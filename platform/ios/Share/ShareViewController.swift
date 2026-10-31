import UIKit
import UniformTypeIdentifiers

// The share extension (ryui.json's shareTarget): what another app shares
// from its share sheet, text and links, goes to the app, which it opens,
// as Android's ACTION_SEND opens the app; ui.OnShare gets it. It runs no
// Go: it hands the text over in a URL of the app's share scheme
// (Info.plist's RyUIShareScheme, ryuishare.<app ID>), which the app's host
// turns into the Shared event.

@objc(RyUIShareViewController)
final class ShareViewController: UIViewController {
    private var started = false

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .clear
    }

    // Once on screen: opening the app goes through the responder chain,
    // which reaches the application only from a view in a window.
    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated)
        guard !started else { return }
        started = true
        collect { text in
            guard !text.isEmpty, let url = self.shareURL(text) else { return self.finish() }
            self.openApp(url) { _ in self.finish() }
        }
    }

    private func finish() {
        extensionContext?.completeRequest(returningItems: nil)
    }

    /// The shared text: the items' texts, then their web links that aren't
    /// in a text already, a line each; the item's own text if there's none.
    private func collect(_ done: @escaping (String) -> Void) {
        let items = extensionContext?.inputItems as? [NSExtensionItem] ?? []
        let group = DispatchGroup()
        let lock = NSLock()
        // Keyed by item and attachment, so the order is the sharer's.
        var texts: [(Int, String)] = [], links: [(Int, String)] = []
        var n = 0
        for item in items {
            for provider in item.attachments ?? [] {
                let at = n
                n += 1
                let url = UTType.url.identifier, text = UTType.plainText.identifier
                let type = provider.hasItemConformingToTypeIdentifier(url) ? url
                    : provider.hasItemConformingToTypeIdentifier(text) ? text : nil
                guard let type else { continue }
                group.enter()
                provider.loadItem(forTypeIdentifier: type, options: nil) { value, _ in
                    defer { group.leave() }
                    lock.lock()
                    defer { lock.unlock() }
                    switch value {
                    case let u as URL where !u.isFileURL:
                        links.append((at, u.absoluteString))
                    case let s as String where type == url:
                        links.append((at, s))
                    case let s as String:
                        texts.append((at, s))
                    case let s as NSAttributedString:
                        texts.append((at, s.string))
                    case let d as Data:
                        if let s = String(data: d, encoding: .utf8) {
                            if type == url { links.append((at, s)) } else { texts.append((at, s)) }
                        }
                    default:
                        break
                    }
                }
            }
        }
        group.notify(queue: .main) {
            var lines = texts.sorted { $0.0 < $1.0 }.map { $0.1 }
            if lines.isEmpty {
                for item in items {
                    if let s = item.attributedContentText?.string, !s.isEmpty { lines.append(s) }
                }
            }
            for (_, l) in links.sorted(by: { $0.0 < $1.0 }) where !lines.contains(where: { $0.contains(l) }) {
                lines.append(l)
            }
            done(lines.map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }.filter { !$0.isEmpty }.joined(separator: "\n"))
        }
    }

    /// ryuishare.<app ID>:?<text, percent-encoded>
    private func shareURL(_ text: String) -> URL? {
        guard let scheme = Bundle.main.object(forInfoDictionaryKey: "RyUIShareScheme") as? String,
              let q = text.addingPercentEncoding(withAllowedCharacters: .alphanumerics) else { return nil }
        return URL(string: scheme + ":?" + q)
    }

    /// Opens the app: an extension has no UIApplication.shared, but the
    /// application is at the end of its view's responder chain. Its open
    /// isn't available to extensions at compile time, so it's called
    /// through the runtime.
    private func openApp(_ url: URL, _ done: @escaping (Bool) -> Void) {
        let sel = NSSelectorFromString("openURL:options:completionHandler:")
        var r: UIResponder? = self
        while let x = r {
            if let app = x as? UIApplication, app.responds(to: sel) {
                typealias Open = @convention(c) (AnyObject, Selector, NSURL, NSDictionary, (@convention(block) (Bool) -> Void)?) -> Void
                let open = unsafeBitCast(app.method(for: sel), to: Open.self)
                let completion: @convention(block) (Bool) -> Void = { ok in DispatchQueue.main.async { done(ok) } }
                open(app, sel, url as NSURL, NSDictionary(), completion)
                return
            }
            r = x.next
        }
        done(false)
    }
}
