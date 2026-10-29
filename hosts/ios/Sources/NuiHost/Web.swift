#if canImport(UIKit)
import UIKit
import WebKit
#if canImport(NuiOps)
import NuiOps
#endif

/// A WebView: a WKWebView that reports the pages it loads and what its
/// pages post (window.nui.postMessage), and runs the core's script.
@MainActor final class NuiWebView: WKWebView, WKNavigationDelegate {
    let nuiID: UInt32
    weak var runtime: NuiRuntime?

    init(_ id: UInt32, _ rt: NuiRuntime) {
        nuiID = id
        runtime = rt
        let config = WKWebViewConfiguration()
        let content = WKUserContentController()
        // Before the page's own scripts: the same call as on Android.
        content.addUserScript(WKUserScript(source: """
            window.nui = { postMessage: function (m) { window.webkit.messageHandlers.nui.postMessage(String(m)); } };
            """, injectionTime: .atDocumentStart, forMainFrameOnly: false))
        let handler = NuiWebMessages()
        content.add(handler, name: "nui")
        config.userContentController = content
        config.allowsInlineMediaPlayback = true
        super.init(frame: .zero, configuration: config)
        handler.view = self
        navigationDelegate = self
        allowsBackForwardNavigationGestures = true
        isOpaque = false // no white flash before a dark page draws
        backgroundColor = .systemBackground
        #if DEBUG
        if #available(iOS 16.4, *) { isInspectable = true } // Safari's Web Inspector
        #endif
    }

    required init?(coder: NSCoder) { fatalError() }

    /// Loads source: an http(s) URL, a file (an absolute path, file://),
    /// or one of the app's assets.
    func load(source: String) {
        if source.isEmpty { return }
        if source.hasPrefix("http://") || source.hasPrefix("https://"), let u = URL(string: source) {
            load(URLRequest(url: u))
            return
        }
        let file: URL? = source.hasPrefix("file://") ? URL(string: source)
            : source.hasPrefix("/") ? URL(fileURLWithPath: source)
            : Bundle.main.resourceURL?.appendingPathComponent("assets").appendingPathComponent(source)
        guard let file else { return }
        loadFileURL(file, allowingReadAccessTo: file.deletingLastPathComponent())
    }

    func webView(_ w: WKWebView, didFinish navigation: WKNavigation!) {
        let id = nuiID, url = w.url?.absoluteString ?? ""
        runtime?.emit(id, .navigated) { NuiEvents.navigated(id, url) }
    }

    func webView(_ w: WKWebView, didFail navigation: WKNavigation!, withError error: Error) { failed(error) }
    func webView(_ w: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) { failed(error) }

    private func failed(_ error: Error) {
        if (error as NSError).code == NSURLErrorCancelled { return } // a load another replaced
        let id = nuiID, m = error.localizedDescription
        runtime?.emit(id, .failed) { NuiEvents.failed(id, m) }
    }

    func posted(_ m: String) {
        let id = nuiID
        runtime?.emit(id, .message) { NuiEvents.message(id, m) }
    }
}

/// The page's messages, to the view (the content controller keeps its
/// handler: a weak link back, so the view can go).
final class NuiWebMessages: NSObject, WKScriptMessageHandler {
    weak var view: NuiWebView?

    func userContentController(_ c: WKUserContentController, didReceive message: WKScriptMessage) {
        let m = message.body as? String ?? "\(message.body)"
        MainActor.assumeIsolated { view?.posted(m) }
    }
}
#endif
