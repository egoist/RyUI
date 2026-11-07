#if canImport(UIKit) && RYUI_PRINT // an app that calls ryui/print (see the CLI's iosFeatures)
import UIKit
#if RYUI_PRINTHTML
import WebKit
#endif

/// ryui/print's module, each with JSON: "file" (source, type), "html"
/// (html) and "webView" (view, a WebView's node id), with the dialog's
/// job, landscape and from (the node id the iPad's popover points at),
/// show the print sheet and answer u8: 0 cancelled, 1 printed; "pdf"
/// (html, title, width, height, top, right, bottom, left in points)
/// answers the path of the PDF it wrote, named after the title.
@MainActor enum RyUIPrint {
    static func methods(_ rt: RyUIRuntime) -> [String: RyUIModuleMethod] {
        var m: [String: RyUIModuleMethod] = [
            "file": { [weak rt] args, reply in
                let o = json(args)
                RyUILocalFile.resolve(o["source"] as? String ?? "", type: o["type"] as? String ?? "") { r in
                    switch r {
                    case .failure(let e): reply(.failure(e))
                    case .success(let url):
                        guard UIPrintInteractionController.canPrint(url) else { return reply(.failure(RyUIModuleError("unsupported"))) }
                        let photo = UIImage(contentsOfFile: url.path) != nil
                        present(rt, o, job: url.lastPathComponent, photo: photo, reply) { $0.printingItem = url }
                    }
                }
            },
            "webView": { [weak rt] args, reply in
                let o = json(args)
                let id = (o["view"] as? NSNumber)?.uint32Value ?? 0
                #if RYUI_WEBVIEW
                guard let web = rt?.nodes[id]?.view as? RyUIWebView else { return reply(.failure(RyUIModuleError("not a WebView"))) }
                present(rt, o, job: web.title ?? "", photo: false, reply) { $0.printFormatter = web.viewPrintFormatter() }
                #else
                reply(.failure(RyUIModuleError("not a WebView")))
                #endif
            },
        ]
        #if RYUI_PRINTHTML
        m["html"] = { [weak rt] args, reply in
            let o = json(args)
            RyUIPrintPage.load(o["html"] as? String ?? "") { r in
                switch r {
                case .failure(let e): reply(.failure(e))
                case .success(let page):
                    present(rt, o, job: page.web.title ?? "", photo: false, { r in
                        reply(r)
                        page.done()
                    }) { $0.printFormatter = page.web.viewPrintFormatter() }
                }
            }
        }
        m["pdf"] = { args, reply in
            let o = json(args)
            func pt(_ k: String) -> CGFloat { CGFloat((o[k] as? NSNumber)?.doubleValue ?? 0) }
            let paper = CGRect(x: 0, y: 0, width: pt("width"), height: pt("height"))
            let printable = paper.inset(by: UIEdgeInsets(top: pt("top"), left: pt("left"), bottom: pt("bottom"), right: pt("right")))
            guard paper.width > 0, paper.height > 0, printable.width > 0, printable.height > 0 else {
                return reply(.failure(RyUIModuleError("the margins leave no room on the page")))
            }
            RyUIPrintPage.load(o["html"] as? String ?? "") { r in
                switch r {
                case .failure(let e): reply(.failure(e))
                case .success(let page):
                    reply(Result { try page.pdf(paper, printable, title: o["title"] as? String ?? "") })
                    page.done()
                }
            }
        }
        #endif
        return m
    }

    private static func json(_ d: Data) -> [String: Any] {
        (try? JSONSerialization.jsonObject(with: d)) as? [String: Any] ?? [:]
    }

    /// Shows the print sheet for what set gives it, from the view o's
    /// from names on the iPad; answers once it's gone.
    private static func present(_ rt: RyUIRuntime?, _ o: [String: Any], job: String, photo: Bool,
                                _ reply: @escaping (Result<Data, Error>) -> Void, _ set: (UIPrintInteractionController) -> Void) {
        guard UIPrintInteractionController.isPrintingAvailable else { return reply(.failure(RyUIModuleError("unavailable"))) }
        guard let rt, let top = rt.topController else { return reply(.failure(RyUIModuleError("no screen"))) }
        let c = UIPrintInteractionController.shared
        let info = UIPrintInfo(dictionary: nil)
        let name = o["job"] as? String ?? ""
        info.jobName = !name.isEmpty ? name : !job.isEmpty ? job : RyUILocalFile.appName
        info.outputType = photo ? .photo : .general
        info.orientation = o["landscape"] as? Bool == true ? .landscape : .portrait
        c.printInfo = info
        c.printingItem = nil
        c.printFormatter = nil
        c.showsNumberOfCopies = true
        c.showsPaperOrientation = true
        set(c)
        let done: UIPrintInteractionController.CompletionHandler = { _, completed, error in
            if let error { reply(.failure(error)); return }
            reply(.success(Data([completed ? 1 : 0])))
        }
        if UIDevice.current.userInterfaceIdiom == .pad {
            let (v, r, _) = RyUILocalFile.anchor(rt, (o["from"] as? NSNumber)?.uint32Value ?? 0, top)
            c.present(from: r, in: v, animated: true, completionHandler: done)
        } else {
            c.present(animated: true, completionHandler: done)
        }
    }
}

#if RYUI_PRINTHTML
/// Markup in a WKWebView that's never shown, loaded, to print or lay out
/// as a PDF with WebKit's print formatter (paginated, as Safari prints).
@MainActor final class RyUIPrintPage: NSObject, WKNavigationDelegate {
    private static var loading = Set<RyUIPrintPage>()
    let web: WKWebView
    private var loaded: ((Result<RyUIPrintPage, Error>) -> Void)?

    private init(_ done: @escaping (Result<RyUIPrintPage, Error>) -> Void) {
        // A page's width as a printer's, before it's paginated.
        web = WKWebView(frame: CGRect(x: 0, y: 0, width: 612, height: 792), configuration: WKWebViewConfiguration())
        loaded = done
    }

    static func load(_ html: String, _ done: @escaping (Result<RyUIPrintPage, Error>) -> Void) {
        let p = RyUIPrintPage(done)
        loading.insert(p)
        p.web.navigationDelegate = p
        p.web.loadHTMLString(html, baseURL: nil)
    }

    func webView(_ w: WKWebView, didFinish navigation: WKNavigation!) {
        // Its fonts and images, which the load doesn't wait for.
        w.evaluateJavaScript("document.fonts ? document.fonts.ready.then(() => true) : true") { [weak self] _, _ in
            guard let self, let l = self.loaded else { return }
            self.loaded = nil
            l(.success(self))
        }
    }

    func webView(_ w: WKWebView, didFail navigation: WKNavigation!, withError error: Error) { failed(error) }
    func webView(_ w: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) { failed(error) }

    private func failed(_ e: Error) {
        guard let l = loaded else { return }
        loaded = nil
        l(.failure(e))
        done()
    }

    func done() { Self.loading.remove(self) }

    /// The page laid out on sheets of paper, printing in printable, as a
    /// PDF file in the caches (ryui-print/), named after its title.
    func pdf(_ paper: CGRect, _ printable: CGRect, title: String) throws -> Data {
        let r = UIPrintPageRenderer()
        r.addPrintFormatter(web.viewPrintFormatter(), startingAtPageAt: 0)
        // The renderer's paper: read-only properties, set as UIKit's own
        // print sheet sets them (what Expo and others do too).
        r.setValue(NSValue(cgRect: paper), forKey: "paperRect")
        r.setValue(NSValue(cgRect: printable), forKey: "printableRect")
        let data = NSMutableData()
        UIGraphicsBeginPDFContextToData(data, paper, [kCGPDFContextCreator as String: RyUILocalFile.appName])
        r.prepare(forDrawingPages: NSRange(location: 0, length: r.numberOfPages))
        let bounds = UIGraphicsGetPDFContextBounds()
        for i in 0..<max(r.numberOfPages, 1) {
            UIGraphicsBeginPDFPage()
            if i < r.numberOfPages { r.drawPage(at: i, in: bounds) }
        }
        UIGraphicsEndPDFContext()
        let fm = FileManager.default
        // Named after the page's title, as it's shared or mailed, in a folder of its own.
        let dir = fm.urls(for: .cachesDirectory, in: .userDomainMask)[0].appendingPathComponent("ryui-print/" + UUID().uuidString, isDirectory: true)
        try fm.createDirectory(at: dir, withIntermediateDirectories: true)
        let name = title.replacingOccurrences(of: "/", with: "-").replacingOccurrences(of: ":", with: "-").trimmingCharacters(in: .whitespacesAndNewlines)
        let url = dir.appendingPathComponent((name.isEmpty ? "Document" : name) + ".pdf")
        try (data as Data).write(to: url)
        return Data(url.path.utf8)
    }
}
#endif
#endif
