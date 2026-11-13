#if canImport(UIKit) && ASUKA_WEBVIEW // an app that has a WebView (see the CLI's iosFeatures)
import UIKit
import WebKit
#if canImport(AsukaOps)
import AsukaOps
#endif

/// A WebView: a WKWebView that reports the pages it loads, what its pages
/// post (window.asuka.postMessage), its state (URL, title, progress,
/// history), the files it leads to and the windows it would open, blocks
/// the requests its Block names, and runs the core's script.
@MainActor final class AsukaWebView: WKWebView, WKNavigationDelegate, WKUIDelegate {
    let asukaID: UInt32
    weak var runtime: AsukaRuntime?
    private var observations: [NSKeyValueObservation] = []
    private var stateQueued = false

    init(_ id: UInt32, _ rt: AsukaRuntime) {
        asukaID = id
        runtime = rt
        let config = WKWebViewConfiguration()
        let content = WKUserContentController()
        // Before the page's own scripts: the same call as on Android.
        content.addUserScript(WKUserScript(source: """
            window.asuka = { postMessage: function (m) { window.webkit.messageHandlers.asuka.postMessage(String(m)); } };
            """, injectionTime: .atDocumentStart, forMainFrameOnly: false))
        let handler = AsukaWebMessages()
        content.add(handler, name: "asuka")
        config.userContentController = content
        config.allowsInlineMediaPlayback = true
        super.init(frame: .zero, configuration: config)
        handler.view = self
        navigationDelegate = self
        uiDelegate = self
        observations = [
            observe(\.url) { w, _ in MainActor.assumeIsolated { w.state() } },
            observe(\.title) { w, _ in MainActor.assumeIsolated { w.state() } },
            observe(\.isLoading) { w, _ in MainActor.assumeIsolated { w.state() } },
            observe(\.estimatedProgress) { w, _ in MainActor.assumeIsolated { w.state() } },
            observe(\.canGoBack) { w, _ in MainActor.assumeIsolated { w.state() } },
            observe(\.canGoForward) { w, _ in MainActor.assumeIsolated { w.state() } },
        ]
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
            : asukaAssets?.appendingPathComponent(source)
        guard let file else { return }
        loadFileURL(file, allowingReadAccessTo: file.deletingLastPathComponent())
    }

    func webView(_ w: WKWebView, didFinish navigation: WKNavigation!) {
        let id = asukaID, url = w.url?.absoluteString ?? ""
        runtime?.emit(id, .navigated) { AsukaEvents.navigated(id, url) }
    }

    func webView(_ w: WKWebView, didFail navigation: WKNavigation!, withError error: Error) { failed(error) }
    func webView(_ w: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) { failed(error) }

    private func failed(_ error: Error) {
        if (error as NSError).code == NSURLErrorCancelled { return } // a load another replaced
        let id = asukaID, m = error.localizedDescription
        runtime?.emit(id, .failed) { AsukaEvents.failed(id, m) }
    }

    /// Reports the page's state, once for the changes of a turn of the main loop.
    func state() {
        guard let rt = runtime, rt.nodes[asukaID]?.listens.contains(AsukaEventKind.webState.rawValue) == true, !stateQueued else { return }
        stateQueued = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.stateQueued = false
            var w = AsukaWriter()
            w.string(self.url?.absoluteString ?? "")
            w.string(self.title ?? "")
            w.u8((self.isLoading ? 1 : 0) | (self.canGoBack ? 2 : 0) | (self.canGoForward ? 4 : 0))
            w.f32(Float(self.isLoading ? self.estimatedProgress : 1))
            let id = self.asukaID, b = w.bytes
            rt.emit(id, .webState) { AsukaEvents.webState(id, b) }
        }
    }

    func setPreload(_ script: String) {
        guard !script.isEmpty else { return }
        configuration.userContentController.addUserScript(WKUserScript(source: script, injectionTime: .atDocumentStart, forMainFrameOnly: false))
    }

    /// Block's patterns (u32 count, then u32-length strings), as a content
    /// rule list: a host blocks it and its subdomains, a part of a URL any
    /// URL with it.
    func setBlock(_ b: UnsafeRawBufferPointer) {
        var r = AsukaReader(b)
        guard b.count >= 4 else { return }
        var rules: [[String: Any]] = []
        for _ in 0..<Int(r.u32()) {
            let p = r.string()
            let esc = NSRegularExpression.escapedPattern(for: p)
            let filter = p.contains("/") ? esc : "^[a-z]+://([^/:]+\\.)?" + esc + "[:/]"
            rules.append(["trigger": ["url-filter": filter], "action": ["type": "block"]])
        }
        guard let data = try? JSONSerialization.data(withJSONObject: rules), let json = String(data: data, encoding: .utf8) else { return }
        let id = "asuka-block-\(asukaID)"
        WKContentRuleListStore.default().compileContentRuleList(forIdentifier: id, encodedContentRuleList: json) { [weak self] list, _ in
            guard let list else { return }
            MainActor.assumeIsolated { self?.configuration.userContentController.add(list) }
        }
    }

    /// WebNavigate: 0 back, 1 forward, 2 reload, 3 stop.
    func navigate(_ to: UInt8) {
        switch to {
        case 0: if canGoBack { goBack() }
        case 1: if canGoForward { goForward() }
        case 2: reload()
        default: stopLoading()
        }
    }

    // A page opening a window: reported, or loaded here.
    func webView(_ w: WKWebView, createWebViewWith c: WKWebViewConfiguration, for action: WKNavigationAction, windowFeatures f: WKWindowFeatures) -> WKWebView? {
        guard let url = action.request.url else { return nil }
        if let rt = runtime, rt.nodes[asukaID]?.listens.contains(AsukaEventKind.newWindow.rawValue) == true {
            let id = asukaID, u = url.absoluteString
            rt.emit(id, .newWindow) { AsukaEvents.newWindow(id, u) }
        } else {
            w.load(action.request)
        }
        return nil
    }

    // A response the page can't show, or an attachment: a download.
    func webView(_ w: WKWebView, decidePolicyFor response: WKNavigationResponse, decisionHandler: @escaping @MainActor (WKNavigationResponsePolicy) -> Void) {
        let r = response.response
        let disposition = (r as? HTTPURLResponse)?.value(forHTTPHeaderField: "Content-Disposition") ?? ""
        guard response.isForMainFrame, !response.canShowMIMEType || disposition.lowercased().hasPrefix("attachment") else {
            decisionHandler(.allow)
            return
        }
        decisionHandler(.cancel)
        let id = asukaID
        var b = AsukaWriter()
        b.string(r.url?.absoluteString ?? "")
        b.string(r.mimeType ?? "")
        b.string(r.suggestedFilename ?? "")
        b.u64(UInt64(bitPattern: r.expectedContentLength))
        let bytes = b.bytes
        runtime?.emit(id, .download) { AsukaEvents.download(id, bytes) }
    }

    /// Runs script, answering with its value as JSON (a promise's, awaited), or what it threw.
    func evalResult(_ script: String, _ reply: @escaping (Result<Data, Error>) -> Void) {
        let body = "const v = await (0, eval)(s); return JSON.stringify(v === undefined ? null : v)"
        callAsyncJavaScript(body, arguments: ["s": script], in: nil, in: .page) { r in
            switch r {
            case .success(let v): reply(.success(Data(((v as? String) ?? "null").utf8)))
            case .failure(let e):
                let m = (e as NSError).userInfo["WKJavaScriptExceptionMessage"] as? String ?? e.localizedDescription
                reply(.failure(AsukaModuleError(m)))
            }
        }
    }

    /// The "web" module: "eval" (args: u32 a WebView's node id, then the
    /// script) answers with the script's value as JSON; "cookies" (args:
    /// a URL) with the cookies the WebViews have for it, as a Cookie header.
    static func methods() -> [String: AsukaModuleMethod] {
        [
            "eval": { args, reply in
                let a = [UInt8](args)
                guard a.count >= 4 else { reply(.failure(AsukaModuleError("bad arguments"))); return }
                let id = UInt32(a[0]) | UInt32(a[1]) << 8 | UInt32(a[2]) << 16 | UInt32(a[3]) << 24
                guard let web = AsukaRuntime.shared.nodes[id]?.view as? AsukaWebView else { reply(.failure(AsukaModuleError("not a WebView"))); return }
                web.evalResult(String(decoding: a[4...], as: UTF8.self), reply)
            },
            "cookies": { args, reply in
                guard let url = URL(string: String(decoding: args, as: UTF8.self)), let host = url.host?.lowercased() else {
                    reply(.success(Data())); return
                }
                WKWebsiteDataStore.default().httpCookieStore.getAllCookies { cookies in
                    let now = Date()
                    let path = url.path.isEmpty ? "/" : url.path
                    let header = cookies.filter { c in
                        let d = c.domain.lowercased()
                        let domainOK = d.hasPrefix(".") ? (host == String(d.dropFirst()) || host.hasSuffix(d)) : host == d
                        return domainOK && path.hasPrefix(c.path) && (!c.isSecure || url.scheme == "https") && (c.expiresDate.map { $0 > now } ?? true)
                    }.map { "\($0.name)=\($0.value)" }.joined(separator: "; ")
                    reply(.success(Data(header.utf8)))
                }
            },
        ]
    }

    func posted(_ m: String) {
        let id = asukaID
        runtime?.emit(id, .message) { AsukaEvents.message(id, m) }
    }
}

/// The page's messages, to the view (the content controller keeps its
/// handler: a weak link back, so the view can go).
final class AsukaWebMessages: NSObject, WKScriptMessageHandler {
    weak var view: AsukaWebView?

    func userContentController(_ c: WKUserContentController, didReceive message: WKScriptMessage) {
        let m = message.body as? String ?? "\(message.body)"
        MainActor.assumeIsolated { view?.posted(m) }
    }
}
/// asuka/js's module: "eval" runs a script (args: u32 timeout ms, then the
/// script) in a WKWebView that's never shown, made for it and let go
/// after, and answers with its value as JSON: the last statement's, or
/// what its promise resolves to. A thrown error fails with its message.
@MainActor final class AsukaScript: NSObject, WKNavigationDelegate {
    private static var running = Set<AsukaScript>()
    private let web = WKWebView(frame: .zero, configuration: WKWebViewConfiguration())
    private let script: String
    private var reply: ((Result<Data, Error>) -> Void)?

    private init(_ script: String, _ reply: @escaping (Result<Data, Error>) -> Void) {
        self.script = script
        self.reply = reply
    }

    static func methods() -> [String: AsukaModuleMethod] {
        ["eval": { args, reply in
            let a = [UInt8](args)
            guard a.count >= 4 else { reply(.failure(AsukaModuleError("bad arguments"))); return }
            let ms = UInt32(a[0]) | UInt32(a[1]) << 8 | UInt32(a[2]) << 16 | UInt32(a[3]) << 24
            let s = AsukaScript(String(decoding: a[4...], as: UTF8.self), reply)
            running.insert(s)
            s.web.navigationDelegate = s
            s.web.loadHTMLString("<!doctype html><html></html>", baseURL: nil)
            DispatchQueue.main.asyncAfter(deadline: .now() + Double(ms) / 1000) { [weak s] in
                s?.finish(.failure(AsukaModuleError("timeout")))
            }
        }]
    }

    func webView(_ w: WKWebView, didFinish navigation: WKNavigation!) {
        // Indirect eval: the global scope, and the last statement's value;
        // callAsyncJavaScript awaits a promise.
        let body = "const v = await (0, eval)(s); return JSON.stringify(v === undefined ? null : v)"
        w.callAsyncJavaScript(body, arguments: ["s": script], in: nil, in: .page) { [weak self] r in
            switch r {
            case .success(let v): self?.finish(.success(Data(((v as? String) ?? "null").utf8)))
            case .failure(let e):
                let info = (e as NSError).userInfo
                let m = info["WKJavaScriptExceptionMessage"] as? String ?? e.localizedDescription
                self?.finish(.failure(AsukaModuleError(m)))
            }
        }
    }

    private func finish(_ r: Result<Data, Error>) {
        guard let reply else { return }
        self.reply = nil
        reply(r)
        web.stopLoading()
        Self.running.remove(self)
    }
}
#endif
