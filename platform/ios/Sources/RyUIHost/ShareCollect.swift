#if canImport(UIKit)
import UIKit
import UniformTypeIdentifiers

/// What another app shares with this one through its share extension
/// (ryui.json's shareTarget), collected in the extension: the standalone
/// one (platform/ios/Share), which opens the app with it, and the app's
/// share sheet (app.ShareSheet: RyUIShareSheetController), which shows
/// it. Texts and links as they are; files (when the app takes them)
/// copied into the App Group the app and the extension share (Info.plist's
/// RyUIAppGroup), under RyUIShare/. Compiled into both.
final class RyUIShareCollector {
    private weak var context: NSExtensionContext?

    init(_ context: NSExtensionContext?) {
        self.context = context
        inbox = Self.makeInbox()
    }

    /// One shared item, as the URL carries it: k is its ui.ShareKind
    /// (0 text, 1 link, 2 image, 3 video, 4 audio, 5 file), t a text or a
    /// link's URL, p a file's path in the App Group's container, n its
    /// name, m its MIME type, s its size.
    struct Item {
        var kind: Int
        var text = "", path = "", name = "", mime = "text/plain"
        var size: Int64 = -1
        var json: [String: Any] { ["k": kind, "t": text, "p": path, "n": name, "m": mime, "s": size] }
    }

    /// The App Group's folder for files, if the app takes them.
    private(set) var inbox: URL?

    private static func makeInbox() -> URL? {
        guard let group = Bundle.main.object(forInfoDictionaryKey: "RyUIAppGroup") as? String,
              let c = FileManager.default.containerURL(forSecurityApplicationGroupIdentifier: group) else { return nil }
        let dir = c.appendingPathComponent("RyUIShare", isDirectory: true)
        // What the app never took (it didn't open): gone after a day.
        let fm = FileManager.default
        for d in (try? fm.contentsOfDirectory(at: dir, includingPropertiesForKeys: [.creationDateKey])) ?? [] {
            if let t = try? d.resourceValues(forKeys: [.creationDateKey]).creationDate, t.timeIntervalSinceNow < -86400 {
                try? fm.removeItem(at: d)
            }
        }
        return dir
    }

    /// The shared items, in the sharer's order: texts, links, then (when
    /// the app takes them) files; a link already in a text is left out,
    /// and the item's own text stands in when there's no text (a page's
    /// title from Safari).
    func collect(_ done: @escaping ([Item]) -> Void) {
        let items = context?.inputItems as? [NSExtensionItem] ?? []
        let group = DispatchGroup()
        let lock = NSLock()
        // Keyed by attachment, so the order is the sharer's.
        var found: [(Int, Item)] = []
        func add(_ at: Int, _ it: Item) {
            lock.lock()
            found.append((at, it))
            lock.unlock()
        }
        var n = 0
        for item in items {
            for provider in item.attachments ?? [] {
                let at = n
                n += 1
                group.enter()
                load(provider) { it in
                    if let it { add(at, it) }
                    group.leave()
                }
            }
        }
        group.notify(queue: .main) {
            var out = found.sorted { $0.0 < $1.0 }.map { $0.1 }
            for i in out.indices where out[i].kind <= 1 {
                out[i].text = out[i].text.trimmingCharacters(in: .whitespacesAndNewlines)
            }
            out.removeAll { $0.kind <= 1 && $0.text.isEmpty }
            if !out.contains(where: { $0.kind == 0 }) {
                var titles: [Item] = []
                for item in items {
                    if let s = item.attributedContentText?.string.trimmingCharacters(in: .whitespacesAndNewlines), !s.isEmpty {
                        titles.append(Item(kind: 0, text: s))
                    }
                }
                out.insert(contentsOf: titles, at: 0)
            }
            let texts = out.filter { $0.kind == 0 }.map(\.text)
            out.removeAll { it in it.kind == 1 && texts.contains { $0.contains(it.text) } }
            done(out)
        }
    }

    /// One attachment: a web link, a text, or a file (copied into the
    /// inbox); nil for what it can't take.
    private func load(_ p: NSItemProvider, _ done: @escaping (Item?) -> Void) {
        let types = p.registeredTypeIdentifiers.compactMap { UTType($0) }
        // A file's type, the most faithful first (as registered): data
        // that isn't a URL, nor a plain text.
        let file = inbox == nil ? nil : types.first { $0.conforms(to: .data) && !$0.conforms(to: .url) && !$0.conforms(to: .plainText) }
        if p.hasItemConformingToTypeIdentifier(UTType.url.identifier), file == nil {
            p.loadItem(forTypeIdentifier: UTType.url.identifier, options: nil) { value, _ in
                switch value {
                case let u as URL where u.isFileURL:
                    if self.inbox == nil { return done(nil) }
                    self.copy(u, type: UTType(filenameExtension: u.pathExtension) ?? .data, name: nil, done)
                case let u as URL:
                    done(Item(kind: 1, text: u.absoluteString))
                case let s as String:
                    done(Item(kind: 1, text: s))
                case let d as Data:
                    done(String(data: d, encoding: .utf8).map { Item(kind: 1, text: $0) })
                default:
                    done(nil)
                }
            }
            return
        }
        if let file {
            p.loadFileRepresentation(forTypeIdentifier: file.identifier) { url, _ in
                if let url {
                    self.copy(url, type: file, name: p.suggestedName, done)
                    return
                }
                // No file to be had (an image made in memory, as a markup
                // screenshot): its data, or the image itself.
                p.loadItem(forTypeIdentifier: file.identifier, options: nil) { value, _ in
                    switch value {
                    case let d as Data:
                        self.write(d, type: file, name: p.suggestedName, done)
                    case let u as URL where u.isFileURL:
                        self.copy(u, type: file, name: p.suggestedName, done)
                    case let i as UIImage:
                        if let d = i.pngData() { self.write(d, type: .png, name: p.suggestedName, done) } else { done(nil) }
                    default:
                        done(nil)
                    }
                }
            }
            return
        }
        guard p.hasItemConformingToTypeIdentifier(UTType.plainText.identifier) else { return done(nil) }
        p.loadItem(forTypeIdentifier: UTType.plainText.identifier, options: nil) { value, _ in
            switch value {
            case let s as String:
                done(Item(kind: 0, text: s))
            case let s as NSAttributedString:
                done(Item(kind: 0, text: s.string))
            case let d as Data:
                done(String(data: d, encoding: .utf8).map { Item(kind: 0, text: $0) })
            case let u as URL where u.isFileURL && self.inbox != nil:
                self.copy(u, type: .plainText, name: nil, done)
            default:
                done(nil)
            }
        }
    }

    /// A new folder in the inbox for one file.
    private func slot() -> URL? {
        guard let inbox else { return nil }
        let d = inbox.appendingPathComponent(UUID().uuidString, isDirectory: true)
        return (try? FileManager.default.createDirectory(at: d, withIntermediateDirectories: true)) != nil ? d : nil
    }

    /// The file's name: its own, or the provider's with the type's
    /// extension.
    private func fileName(_ url: URL?, _ type: UTType, _ suggested: String?) -> String {
        if let n = url?.lastPathComponent, !n.isEmpty, !n.hasPrefix(".") { return n }
        var n = suggested.flatMap { $0.isEmpty ? nil : $0 } ?? "file"
        if let ext = type.preferredFilenameExtension, (n as NSString).pathExtension.isEmpty { n += "." + ext }
        return n
    }

    private func item(_ dst: URL, _ type: UTType) -> Item {
        let t = UTType(filenameExtension: dst.pathExtension).flatMap { $0.conforms(to: type) ? $0 : nil } ?? type
        let kind = t.conforms(to: .image) ? 2 : t.conforms(to: .movie) || t.conforms(to: .video) ? 3 : t.conforms(to: .audio) ? 4 : 5
        let size = (try? FileManager.default.attributesOfItem(atPath: dst.path)[.size] as? NSNumber)?.int64Value ?? -1
        let path = String(dst.path.dropFirst(inbox!.deletingLastPathComponent().path.count).drop { $0 == "/" })
        return Item(kind: kind, path: path, name: dst.lastPathComponent, mime: t.preferredMIMEType ?? "application/octet-stream", size: size)
    }

    /// Copies a file into the inbox. Within the provider's completion: its
    /// file is gone after.
    private func copy(_ src: URL, type: UTType, name: String?, _ done: (Item?) -> Void) {
        guard let d = slot() else { return done(nil) }
        let dst = d.appendingPathComponent(fileName(src, type, name))
        let scoped = src.startAccessingSecurityScopedResource()
        defer { if scoped { src.stopAccessingSecurityScopedResource() } }
        do {
            try FileManager.default.copyItem(at: src, to: dst)
            done(item(dst, type))
        } catch {
            done(nil)
        }
    }

    private func write(_ data: Data, type: UTType, name: String?, _ done: (Item?) -> Void) {
        guard let d = slot() else { return done(nil) }
        let dst = d.appendingPathComponent(fileName(nil, type, name))
        do {
            try data.write(to: dst)
            done(item(dst, type))
        } catch {
            done(nil)
        }
    }

    /// ryuishare.<app ID>:?<the items as JSON, percent-encoded>
    func shareURL(_ items: [Item]) -> URL? {
        guard let scheme = Bundle.main.object(forInfoDictionaryKey: "RyUIShareScheme") as? String,
              let json = try? JSONSerialization.data(withJSONObject: items.map(\.json)),
              let q = String(data: json, encoding: .utf8)?.addingPercentEncoding(withAllowedCharacters: .alphanumerics) else { return nil }
        return URL(string: scheme + ":?" + q)
    }

    /// A file's path, from the App Group container's.
    func absolute(_ item: Item) -> String {
        guard let inbox, !item.path.isEmpty else { return "" }
        return inbox.deletingLastPathComponent().appendingPathComponent(item.path).path
    }

    /// Removes the files of items (the sheet's, once it closes).
    func remove(_ items: [Item]) {
        for it in items where !it.path.isEmpty {
            try? FileManager.default.removeItem(at: URL(fileURLWithPath: absolute(it)).deletingLastPathComponent())
        }
    }

    /// Opens the app: an extension has no UIApplication.shared, but the
    /// application is at the end of its view's responder chain. Its open
    /// isn't available to extensions at compile time, so it's called
    /// through the runtime.
    static func openApp(_ url: URL, from responder: UIResponder, _ done: @escaping (Bool) -> Void) {
        let sel = NSSelectorFromString("openURL:options:completionHandler:")
        var r: UIResponder? = responder
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
#endif
