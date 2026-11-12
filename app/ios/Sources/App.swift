// The app: one window showing the NSH app full screen, as `nsh package` builds it; the app keeps clear of the
// notch and home bar itself (safeAreaInsets). TailPocket answers the app's tailPocket.* host calls (src/tunnel.ts);
// this class answers page.pickFiles and page.uploadFile (src/link.ts): NSH 0.3.0 has a picker for photos and none for
// other files; page.copyImage, page.videoFrame and page.openFile (src/page.ts).
import AVFoundation
import NativeShell
import QuickLook
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
/// WKContentView's own method at launch left the bar there (iPhone SE simulator, iOS 17: the content view
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

final class AppController: UIViewController, UIDocumentPickerDelegate, QLPreviewControllerDataSource {
  private let app = NshAppView()
  private var tailPocket: AnyObject?
  private var picking: ((Any?) -> Void)?
  private var pickLimit = 9
  private var previewing: URL?

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
    // A picture on the clipboard (src/page.ts copyPicture), its own bytes under its own type: nothing re-encoded.
    app.handle("page.copyImage") { data, reply in
      reply.resolve(Self.copyImage((data as? [String: Any])?["url"] as? String))
    }
    // A frame of a video at a link and its length (src/page.ts videoFrame); null when it can't be read.
    app.handle("page.videoFrame") { data, reply in
      Task { reply.resolve(await Self.videoFrame((data as? [String: Any])?["url"] as? String) as Any?) }
    }
    // A file from the computer, fetched and shown in Quick Look, which has the share sheet to open it elsewhere.
    app.handle("page.openFile") { [weak self] data, reply in
      let a = data as? [String: Any]
      Task { @MainActor in reply.resolve(await self?.openFile(a?["url"] as? String, name: a?["name"] as? String) ?? "failed") }
    }
    guard let url = Bundle.main.url(forResource: "bundle", withExtension: "nshc", subdirectory: "nsh") else {
      NSLog("nsh: no bundle in the app")
      return
    }
    do { try app.loadBundle(at: url) } catch { NSLog("nsh: cannot load: %@", "\(error)") }
  }

  static func copyImage(_ url: String?) -> Bool {
    let types: [String: UTType] = ["png": .png, "jpeg": .jpeg, "gif": .gif, "webp": .webP]
    guard let url, url.hasPrefix("data:image/"), let comma = url.firstIndex(of: ","),
          let head = url[..<comma].split(separator: "/").last?.split(separator: ";").first,
          url[..<comma].hasSuffix(";base64"), let type = types[String(head)],
          let bytes = Data(base64Encoded: String(url[url.index(after: comma)...])) else { return false }
    UIPasteboard.general.setData(bytes, forPasteboardType: type.identifier)
    return true
  }

  /** A second in, or a third of a shorter video: past a black start. The player reads only the parts it needs. */
  static func videoFrame(_ link: String?) async -> [String: Any]? {
    guard let link, let url = URL(string: link), url.scheme == "http" || url.scheme == "https" else { return nil }
    let asset = AVURLAsset(url: url)
    do {
      let length = try await asset.load(.duration).seconds
      let make = AVAssetImageGenerator(asset: asset)
      make.appliesPreferredTrackTransform = true
      make.maximumSize = CGSize(width: 960, height: 540)
      let at = CMTime(seconds: min(1, length / 3), preferredTimescale: 600)
      let (frame, _) = try await make.image(at: at)
      guard let jpeg = UIImage(cgImage: frame).jpegData(compressionQuality: 0.8) else { return nil }
      return ["image": "data:image/jpeg;base64," + jpeg.base64EncodedString(), "duration": Int((length.isFinite ? length : 0) * 1000)]
    } catch {
      NSLog("a video's frame: %@", "\(error)")
      return nil
    }
  }

  /** The largest file the phone fetches to show. */
  private static let openMax: Int64 = 512 << 20

  /** Fetches it into the temp folder, under its own name in a folder of its own, and shows it: "opened" or "failed". */
  private func openFile(_ link: String?, name: String?) async -> String {
    guard let link, let url = URL(string: link), url.scheme == "http" || url.scheme == "https" else { return "failed" }
    let file = (name ?? "file").split(separator: "/").last.map(String.init) ?? "file"
    do {
      let (got, response) = try await URLSession.shared.download(from: url)
      guard (response as? HTTPURLResponse)?.statusCode == 200, response.expectedContentLength <= Self.openMax else {
        try? FileManager.default.removeItem(at: got)
        return "failed"
      }
      let root = FileManager.default.temporaryDirectory.appendingPathComponent("opened")
      try? FileManager.default.removeItem(at: root) // the last one goes as a new one comes
      let folder = root.appendingPathComponent(UUID().uuidString)
      try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
      let to = folder.appendingPathComponent(file.isEmpty || file == "." || file == ".." ? "file" : file)
      try FileManager.default.moveItem(at: got, to: to)
      previewing = to
      let look = QLPreviewController()
      look.dataSource = self
      present(look, animated: true)
      return "opened"
    } catch {
      NSLog("opening a file: %@", "\(error)")
      return "failed"
    }
  }

  func numberOfPreviewItems(in controller: QLPreviewController) -> Int { previewing == nil ? 0 : 1 }
  func previewController(_ controller: QLPreviewController, previewItemAt index: Int) -> QLPreviewItem { (previewing ?? URL(fileURLWithPath: "/")) as NSURL }

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
