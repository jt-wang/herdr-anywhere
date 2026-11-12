// The system's notifications, posted by the app itself while it runs (src/page.ts notifyFromPage): an agent's alert,
// or a new version (src/updates.ts). No server sends them, so iOS shows one only while the app still runs in the
// background; once iOS suspends it, nothing is posted. page.front says whether the app is in front, as on Android;
// a tap opens the agent's terminal or home (page.open), held until the page listens.
// Laid out as Push.swift on the push branch, which answers page.front and taps the same way, so the two can merge.
import NativeShell
import UIKit
import UserNotifications

@MainActor
final class Notify: NSObject, UNUserNotificationCenterDelegate {
  static let shared = Notify()
  weak var app: NshAppView?
  private var listening = false
  private var opening: [String: Any]?

  func attach(_ app: NshAppView) {
    self.app = app
    UNUserNotificationCenter.current().delegate = self
    app.handle("page.front") { [weak self] _, reply in
      reply.resolve(UIApplication.shared.applicationState == .active)
      guard let self else { return }
      self.listening = true
      DispatchQueue.main.async { self.flushOpen() }
    }
    app.handle("page.notify") { data, reply in
      Task { @MainActor in reply.resolve(await Self.post(data as? [String: Any] ?? [:])) }
    }
    app.handle("page.notifications") { _, reply in
      Task { reply.resolve(Self.permission(await UNUserNotificationCenter.current().notificationSettings().authorizationStatus)) }
    }
    app.handle("page.allowNotifications") { _, reply in
      Task {
        _ = try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound])
        reply.resolve(Self.permission(await UNUserNotificationCenter.current().notificationSettings().authorizationStatus))
      }
    }
    let c = NotificationCenter.default
    c.addObserver(forName: UIApplication.didBecomeActiveNotification, object: nil, queue: .main) { [weak self] _ in
      MainActor.assumeIsolated { self?.app?.send("page.front", ["front": true]) }
    }
    c.addObserver(forName: UIApplication.didEnterBackgroundNotification, object: nil, queue: .main) { [weak self] _ in
      MainActor.assumeIsolated { self?.app?.send("page.front", ["front": false]) }
    }
  }

  private func flushOpen() {
    guard listening, let o = opening, let app else { return }
    opening = nil
    app.send("page.open", o)
  }

  /** The browser's words (src/page.ts Permission). */
  nonisolated static func permission(_ s: UNAuthorizationStatus) -> String {
    switch s {
    case .authorized, .provisional, .ephemeral: return "granted"
    case .denied: return "denied"
    default: return "default"
    }
  }

  /** Posts one, only while the app is not in front (its toast shows otherwise). One per terminal, and one for a new
   *  version: the newest replaces. `quiet`: new words onto one still shown, without a sound. */
  static func post(_ a: [String: Any]) async -> Bool {
    guard UIApplication.shared.applicationState != .active, let title = a["title"] as? String else { return false }
    let center = UNUserNotificationCenter.current()
    guard permission(await center.notificationSettings().authorizationStatus) == "granted" else { return false }
    let update = a["kind"] as? String == "update"
    let terminal = a["terminal"] as? String ?? ""
    let id = update ? "update" : "terminal:" + terminal
    let quiet = a["quiet"] as? Bool == true
    if quiet, !(await center.deliveredNotifications()).contains(where: { $0.request.identifier == id }) { return false }
    let content = UNMutableNotificationContent()
    content.title = title
    if let body = a["body"] as? String { content.body = body }
    content.userInfo = update ? ["home": true] : ["terminal": terminal]
    if !quiet { content.sound = .default }
    do { try await center.add(UNNotificationRequest(identifier: id, content: content, trigger: nil)) } catch { return false }
    return true
  }

  /** In front the app shows its own alert, so iOS shows nothing. */
  nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
    withCompletionHandler done: @escaping (UNNotificationPresentationOptions) -> Void) {
    done([])
  }

  nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
    withCompletionHandler done: @escaping () -> Void) {
    let info = response.notification.request.content.userInfo
    let home = info["home"] as? Bool == true
    let terminal = info["terminal"] as? String
    Task { @MainActor in
      if home { self.opening = ["home": true] } else if let terminal, !terminal.isEmpty { self.opening = ["terminal": terminal] }
      self.flushOpen()
      done()
    }
  }
}
