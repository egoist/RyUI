#if canImport(UIKit) && RYUI_PREVIEW // an app that calls ryui/preview (see the CLI's iosFeatures)
import UIKit
import QuickLook

/// ryui/preview's module: "show" (JSON: files, each a source and a MIME
/// type; start; from, the node id of the view it grows from; edit,
/// Markup into the files) presents
/// Quick Look over the top screen, the files to swipe through, and
/// answers once it's dismissed; "no viewer" if Quick Look can read none.
@MainActor final class RyUIPreview: NSObject, QLPreviewControllerDataSource, QLPreviewControllerDelegate {
    private static var showing = Set<RyUIPreview>()
    private let items: [URL]
    private let reply: (Result<Data, Error>) -> Void
    private weak var from: UIView?
    private let edit: Bool

    private init(_ items: [URL], from: UIView?, edit: Bool, _ reply: @escaping (Result<Data, Error>) -> Void) {
        self.items = items
        self.from = from
        self.edit = edit
        self.reply = reply
    }

    static func methods(_ rt: RyUIRuntime) -> [String: RyUIModuleMethod] {
        ["show": { [weak rt] args, reply in
            let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] ?? [:]
            let files = (o["files"] as? [[String: Any]] ?? []).map { (source: $0["source"] as? String ?? "", type: $0["type"] as? String ?? "") }
            let start = o["start"] as? Int ?? 0
            let from = (o["from"] as? NSNumber)?.uint32Value ?? 0
            let edit = o["edit"] as? Bool ?? false
            RyUILocalFile.resolve(files) { r in
                switch r {
                case .failure(let e): reply(.failure(e))
                case .success(let urls):
                    guard urls.contains(where: { QLPreviewController.canPreview($0 as NSURL) }) else {
                        return reply(.failure(RyUIModuleError("no viewer")))
                    }
                    guard let rt, let top = rt.topController else { return reply(.failure(RyUIModuleError("no screen"))) }
                    let v = from == 0 ? nil : rt.nodes[from]?.view
                    let p = RyUIPreview(urls, from: v?.window != nil ? v : nil, edit: edit, reply)
                    showing.insert(p)
                    let vc = QLPreviewController()
                    vc.dataSource = p
                    vc.delegate = p
                    vc.currentPreviewItemIndex = min(max(start, 0), urls.count - 1)
                    top.present(vc, animated: true)
                }
            }
        }]
    }

    nonisolated func numberOfPreviewItems(in c: QLPreviewController) -> Int {
        MainActor.assumeIsolated { items.count }
    }

    nonisolated func previewController(_ c: QLPreviewController, previewItemAt i: Int) -> QLPreviewItem {
        MainActor.assumeIsolated { items[i] as NSURL }
    }

    /// Grows from the view it came from (the file the user tapped).
    nonisolated func previewController(_ c: QLPreviewController, transitionViewFor item: QLPreviewItem) -> UIView? {
        MainActor.assumeIsolated { from }
    }

    /// Markup, saving into the files, if the app asked (Options.Edit).
    nonisolated func previewController(_ c: QLPreviewController, editingModeFor item: QLPreviewItem) -> QLPreviewItemEditingMode {
        MainActor.assumeIsolated { edit ? .updateContents : .disabled }
    }

    nonisolated func previewControllerDidDismiss(_ c: QLPreviewController) {
        MainActor.assumeIsolated {
            reply(.success(Data()))
            Self.showing.remove(self)
        }
    }
}
#endif
