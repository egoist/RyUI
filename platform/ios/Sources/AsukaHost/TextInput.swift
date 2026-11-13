#if canImport(UIKit)
import UIKit
import UniformTypeIdentifiers
#if canImport(AsukaOps)
import AsukaOps
#endif

/// A TextField's selection, insertions and pastes of content the app
/// takes (TextFieldProps.Selection, InsertText, PasteTypes). Offsets are
/// UTF-16, as UITextInput's are.
@MainActor extension AsukaRuntime {
    /// The field's text input: a one-line field's UITextField, a
    /// multiline one's UITextView.
    private func input(_ n: AsukaNode) -> (UIView & UITextInput)? {
        (n.view as? AsukaTextField) ?? (n.view as? AsukaTextView)
    }

    /// Puts the cursor (start == end) or the selection at UTF-16 offsets
    /// of the field's text, clamped to it.
    func setSelection(_ n: AsukaNode, _ start: Int, _ end: Int) {
        guard let t = input(n) else { return }
        let begin = t.beginningOfDocument
        let length = t.offset(from: begin, to: t.endOfDocument)
        guard let a = t.position(from: begin, offset: min(max(start, 0), length)),
              let b = t.position(from: begin, offset: min(max(end, 0), length)) else { return }
        settingSelection = true
        t.selectedTextRange = t.textRange(from: a, to: b)
        settingSelection = false
        lastSelection[n.id] = (min(max(start, 0), length), min(max(end, 0), length))
    }

    /// Reports a field's selection, once, after what changed it (typing
    /// reports its text first), if it's not where the core put it.
    func reportSelection(_ id: UInt32) {
        guard !settingSelection, nodes[id]?.listens.contains(AsukaEventKind.selectionChanged.rawValue) == true,
              selectionQueued.insert(id).inserted else { return }
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.selectionQueued.remove(id)
            guard let n = self.nodes[id], let t = self.input(n), let r = t.selectedTextRange else { return }
            let s = t.offset(from: t.beginningOfDocument, to: r.start)
            let e = t.offset(from: t.beginningOfDocument, to: r.end)
            if let last = self.lastSelection[id], last == (s, e) { return }
            self.lastSelection[id] = (s, e)
            var w = AsukaWriter()
            w.u32(UInt32(s))
            w.u32(UInt32(e))
            let b = w.bytes
            self.emit(id, .selectionChanged) { AsukaEvents.selectionChanged(id, b) }
        }
    }

    /// Puts text in place of the selection as typing does: undoably, and
    /// reported as typing is.
    func insertText(_ n: AsukaNode, _ s: String) {
        switch n.view {
        case let t as AsukaTextField:
            if !t.isFirstResponder, t.selectedTextRange == nil {
                t.selectedTextRange = t.textRange(from: t.endOfDocument, to: t.endOfDocument)
            }
            let before = t.text ?? ""
            t.insertText(s)
            // A field that isn't editing doesn't send its own change.
            if (t.text ?? "") != before, !t.isFirstResponder { t.sendActions(for: .editingChanged) }
            reportSelection(t.asukaID)
        case let t as AsukaTextView:
            t.insertText(s)
            if !t.isFirstResponder { textViewDidChange(t) }
            reportSelection(t.asukaID)
        default:
            break
        }
    }
}

/// Pastes of the content a field takes (PasteTypes): images, GIFs and
/// files from the pasteboard (a GIF keyboard copies to it), loaded into
/// the caches as a drop's are, and reported with Pasted.
@MainActor enum AsukaPaste {
    /// Whether the pasteboard has content of the types, from its types
    /// alone (which doesn't ask the user to allow the paste).
    static func available(_ types: [String]) -> Bool {
        guard !types.isEmpty else { return false }
        for id in UIPasteboard.general.types {
            guard let t = UTType(id) else { continue }
            if matches(types, t) { return true }
        }
        return false
    }

    private static func matches(_ patterns: [String], _ t: UTType) -> Bool {
        if let m = t.preferredMIMEType, AsukaInteraction.matchMIME(patterns, m) { return true }
        // A type with no MIME type of its own, by what it is.
        let kind = AsukaInteraction.fileKind(t)
        let generic: String
        switch kind {
        case .image: generic = "image/x-unknown"
        case .video: generic = "video/x-unknown"
        case .audio: generic = "audio/x-unknown"
        default: return false
        }
        return AsukaInteraction.matchMIME(patterns, generic)
    }

    /// Takes the pasteboard's items of the types, if it has any: they're
    /// loaded (files copied into the caches) and reported with Pasted.
    /// False: none, the field pastes as it would.
    static func take(_ id: UInt32, _ types: [String]) -> Bool {
        guard !types.isEmpty else { return false }
        let providers = UIPasteboard.general.itemProviders.filter {
            AsukaInteraction.matchMIME(types, AsukaInteraction.describe($0).mime)
        }
        guard !providers.isEmpty else { return false }
        let group = DispatchGroup()
        let loaded = AsukaLoads(providers.count)
        for (k, p) in providers.enumerated() {
            group.enter()
            AsukaInteraction.load(p) { it in
                loaded.set(k, it)
                group.leave()
            }
        }
        group.notify(queue: .main) {
            MainActor.assumeIsolated {
                let items = loaded.items
                guard !items.isEmpty else { return }
                var w = AsukaWriter()
                w.u32(UInt32(items.count))
                for it in items {
                    w.u8(UInt8(clamping: it.kind.rawValue))
                    w.string(it.text)
                    w.string(it.path)
                    w.string(it.name)
                    w.string(it.mime)
                    w.u64(UInt64(bitPattern: it.size))
                }
                let b = w.bytes
                AsukaRuntime.shared.emit(id, .pasted) { AsukaEvents.pasted(id, b) }
            }
        }
        return true
    }
}

/// Items loading on other queues, in their order.
final class AsukaLoads: @unchecked Sendable {
    private let lock = NSLock()
    private var all: [AsukaInteraction.Loaded?]
    init(_ n: Int) { all = Array(repeating: nil, count: n) }
    func set(_ i: Int, _ it: AsukaInteraction.Loaded?) {
        lock.lock()
        all[i] = it
        lock.unlock()
    }
    var items: [AsukaInteraction.Loaded] {
        lock.lock()
        defer { lock.unlock() }
        return all.compactMap { $0 }
    }
}
#endif
