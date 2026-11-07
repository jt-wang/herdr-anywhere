// The app: one window showing the NSH app full screen, as `nsh package` builds it; the app keeps clear of the
// notch and home bar itself (safeAreaInsets). TailPocket answers the app's tailPocket.* host calls (src/tunnel.ts);
// this class answers page.pickFiles and page.uploadFile (src/link.ts): NSH 0.3.0 has a picker for photos and none for
// other files.
import NativeShell
import TailPocketNSH
import UIKit
import UniformTypeIdentifiers

@main
final class AppDelegate: UIResponder, UIApplicationDelegate {
  var window: UIWindow?

  func application(_ application: UIApplication, didFinishLaunchingWithOptions options: [UIApplication.LaunchOptionsKey: Any]?) -> Bool {
    hideWebKeyboardBar()
    window = UIWindow(frame: UIScreen.main.bounds)
    window?.rootViewController = AppController()
    window?.makeKeyAndVisible()
    return true
  }
}

/// WKWebView puts a bar (˄ ˅ Done) over the keyboard while a field in a page has the typing focus: between the key row
/// and the keyboard when typing straight into the terminal, whose page has one hidden field and nothing to step to.
/// When a web view's content view is about to show the keyboard, it becomes a subclass of its class that answers no
/// accessory view, as react-native-webview's hideKeyboardAccessoryView does; the app's own inputs keep theirs. Replacing
/// WKContentView's own method at launch left the bar there (iPhone SE simulator, iOS 17, 2026-11-07: the content view
/// still answered UIWebFormAccessory); the subclass took it away, 44 pt less over the keyboard.
private func hideWebKeyboardBar() {
  NotificationCenter.default.addObserver(forName: UIResponder.keyboardWillShowNotification, object: nil, queue: .main) { _ in
    func focused(_ v: UIView) -> UIView? { if v.isFirstResponder { return v }; for s in v.subviews { if let f = focused(s) { return f } }; return nil }
    let windows = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.flatMap { $0.windows }
    guard let view = windows.lazy.compactMap({ focused($0) }).first, let base: AnyClass = object_getClass(view) else { return }
    let name = NSStringFromClass(base)
    guard name.hasPrefix("WK"), !name.hasSuffix("_NoBar") else { return }
    var noBar: AnyClass? = NSClassFromString(name + "_NoBar")
    if noBar == nil, let c = objc_allocateClassPair(base, name + "_NoBar", 0) {
      let none: @convention(block) (AnyObject) -> UIView? = { _ in nil }
      class_addMethod(c, #selector(getter: UIResponder.inputAccessoryView), imp_implementationWithBlock(none), "@@:")
      objc_registerClassPair(c)
      noBar = c
    }
    guard let c = noBar else { return }
    object_setClass(view, c)
    view.reloadInputViews()
  }
}

final class AppController: UIViewController, UIDocumentPickerDelegate {
  private let app = NshAppView()
  private var tailPocket: AnyObject?
  private var picking: ((Any?) -> Void)?
  private var pickLimit = 9

  override func viewDidLoad() {
    super.viewDidLoad()
    app.frame = view.bounds
    app.autoresizingMask = [.flexibleWidth, .flexibleHeight]
    app.onError = { message in NSLog("nsh error: %@", message) }
    view.addSubview(app)
    tailPocket = TailPocketNsh.attach(app)
    app.handle("page.pickFiles") { [weak self] data, reply in
      let limit = (data as? [String: Any])?["limit"] as? Int
      Task { @MainActor in self?.pickFiles(limit: limit) { reply.resolve($0) } }
    }
    app.handle("page.uploadFile") { data, reply in
      let a = data as? [String: Any]
      Task {
        do { reply.resolve(try await Self.uploadFile(a?["id"] as? String, url: a?["url"] as? String, token: a?["token"] as? String)) }
        catch { reply.reject(error.localizedDescription) }
      }
    }
    guard let url = Bundle.main.url(forResource: "bundle", withExtension: "nshc", subdirectory: "nsh") else {
      NSLog("nsh: no bundle in the app")
      return
    }
    do { try app.loadBundle(at: url) } catch { NSLog("nsh: cannot load: %@", "\(error)") }
  }

  private func pickFiles(limit: Int?, answer: @escaping (Any?) -> Void) {
    picking?([])
    picking = answer
    pickLimit = limit ?? pickLimit
    let picker = UIDocumentPickerViewController(forOpeningContentTypes: [.item], asCopy: true)
    picker.allowsMultipleSelection = true
    picker.delegate = self
    present(picker, animated: true)
  }

  func documentPickerWasCancelled(_ controller: UIDocumentPickerViewController) {
    picking?([])
    picking = nil
  }

  /** Copies in the app's temp folder (asCopy); page.uploadFile sends one by its path and removes it. */
  func documentPicker(_ controller: UIDocumentPickerViewController, didPickDocumentsAt urls: [URL]) {
    let files: [[String: Any]] = urls.prefix(pickLimit).map { url in
      ["id": url.path, "name": url.lastPathComponent, "size": (try? url.resourceValues(forKeys: [.fileSizeKey]))?.fileSize ?? -1]
    }
    picking?(files)
    picking = nil
  }

  /** Streams it from disk: it may be far bigger than the app's memory. */
  private static func uploadFile(_ id: String?, url: String?, token: String?) async throws -> [String: Any] {
    guard let id, let url, let target = URL(string: url) else { throw URLError(.badURL) }
    let file = URL(fileURLWithPath: id)
    defer { try? FileManager.default.removeItem(at: file) }
    var request = URLRequest(url: target)
    request.httpMethod = "POST"
    request.timeoutInterval = 3600
    request.setValue("Bearer \(token ?? "")", forHTTPHeaderField: "Authorization")
    request.setValue("application/octet-stream", forHTTPHeaderField: "Content-Type")
    let (body, response) = try await URLSession.shared.upload(for: request, fromFile: file)
    return ["status": (response as? HTTPURLResponse)?.statusCode ?? 0, "body": String(decoding: body, as: UTF8.self)]
  }

  override var preferredStatusBarStyle: UIStatusBarStyle { NshAppView.preferredStatusBarStyle }
  override var prefersStatusBarHidden: Bool { NshAppView.prefersStatusBarHidden }
  override var preferredStatusBarUpdateAnimation: UIStatusBarAnimation { NshAppView.preferredStatusBarUpdateAnimation }
}
