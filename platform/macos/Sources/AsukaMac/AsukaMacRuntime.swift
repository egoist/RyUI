// An app's native code on the Mac, in Swift: what its macos/*.swift
// builds on. The Asuka CLI compiles this file with them, in one module,
// into a library in the app's bundle (Contents/Frameworks/
// AsukaAppNatives.dylib); the Mac host (host/darwin, Go) loads it as it
// starts and calls the functions at the end, through purego, so the app
// and its host still build with no cgo. The names are the iOS host's:
// AsukaAppNatives registers views and modules on AsukaRuntime.shared, and
// a view is a AsukaNativeView, here holding an NSView.
import AppKit

/// A native module's method: the call's bytes, and a reply (once, from
/// any thread).
public typealias AsukaModuleMethod = (_ args: Data, _ reply: @escaping (Result<Data, Error>) -> Void) -> Void

/// A value of a native view's prop or event.
public enum AsukaNativeValue {
    case bool(Bool), int(Int64), float(Double), string(String)

    public var bool: Bool { if case .bool(let v) = self { return v }; return false }
    public var int: Int64 {
        switch self { case .int(let v): return v; case .float(let v): return Int64(v); default: return 0 }
    }
    public var float: Double {
        switch self { case .float(let v): return v; case .int(let v): return Double(v); default: return 0 }
    }
    public var string: String { if case .string(let v) = self { return v }; return "" }
}

/// A view an app registers for ui.Native.
@MainActor public protocol AsukaNativeView: AnyObject {
    /// The AppKit view shown. The host's own view holds it and sizes it
    /// to fill.
    var view: NSView { get }
    /// Sets one of its props, by name, as it's set or changes.
    func set(_ name: String, _ value: AsukaNativeValue)
    /// Its size for what the layout offers (.greatestFiniteMagnitude for
    /// either: as wide or tall as it likes). By default, its view's
    /// fittingSize.
    func sizeThatFits(_ size: CGSize) -> CGSize
    /// Frees what it holds, as the core destroys it. By default, nothing.
    func destroy()
}

extension AsukaNativeView {
    public func sizeThatFits(_ size: CGSize) -> CGSize { view.fittingSize }
    public func destroy() {}
}

/// What a native view is given: a way to send its events.
@MainActor public final class AsukaNativeContext {
    let id: UInt64
    init(id: UInt64) { self.id = id }

    /// Sends an event to the view's ui.NativeProps.On handler.
    public func emit(_ name: String, _ values: [String: AsukaNativeValue] = [:]) {
        var w = AsukaMacWriter()
        w.string(name)
        w.u32(UInt32(values.count))
        for (k, v) in values {
            w.string(k)
            switch v {
            case .bool(let b): w.u8(1); w.u8(b ? 1 : 0)
            case .int(let i): w.u8(2); w.u64(UInt64(bitPattern: i))
            case .float(let f): w.u8(3); w.u64(f.bitPattern)
            case .string(let s): w.u8(4); w.string(s)
            }
        }
        guard let host = AsukaRuntime.shared.host else { return }
        w.bytes.withUnsafeBufferPointer { host.emit(id, $0.baseAddress, $0.count) }
    }
}

/// Apps register their views and modules from a class named
/// AsukaAppNatives that conforms to this, as on iOS.
@objc public protocol AsukaNativeModule {
    @MainActor static func register()
}

/// Where an app registers its native views and modules.
@MainActor public final class AsukaRuntime {
    public static let shared = AsukaRuntime()

    var factories: [String: (AsukaNativeContext) -> AsukaNativeView] = [:]
    var modules: [String: [String: AsukaModuleMethod]] = [:]
    var views: [UInt64: AsukaNativeView] = [:]
    var host: AsukaMacHost?

    /// Registers a native view kind for ui.Native.
    public func registerNative(_ type: String, _ make: @escaping (AsukaNativeContext) -> AsukaNativeView) {
        factories[type] = make
    }

    /// Registers a module's methods for ui.Call(module, method, ...). The
    /// host's own modules of a name win over an app's.
    public func registerModule(_ name: String, _ methods: [String: AsukaModuleMethod]) {
        modules[name] = methods
    }
}

/// An error a module replies with when it has no message of its own.
struct AsukaMacError: LocalizedError {
    let message: String
    var errorDescription: String? { message }
}

// What the host gives the library: functions of its own (purego
// callbacks), called on the main thread.
public typealias AsukaMacEmit = @convention(c) (UInt64, UnsafePointer<UInt8>?, Int) -> Void
public typealias AsukaMacReply = @convention(c) (UInt64, Int32, UnsafePointer<UInt8>?, Int) -> Void

struct AsukaMacHost {
    let emit: AsukaMacEmit
    let reply: AsukaMacReply
}

struct AsukaMacWriter {
    var bytes: [UInt8] = []
    mutating func u8(_ v: UInt8) { bytes.append(v) }
    mutating func u32(_ v: UInt32) { withUnsafeBytes(of: v.littleEndian) { bytes.append(contentsOf: $0) } }
    mutating func u64(_ v: UInt64) { withUnsafeBytes(of: v.littleEndian) { bytes.append(contentsOf: $0) } }
    mutating func string(_ s: String) {
        let u = Array(s.utf8)
        u32(UInt32(u.count))
        bytes.append(contentsOf: u)
    }
}

struct AsukaMacReader {
    var p: UnsafeRawBufferPointer
    var bad = false
    mutating func take(_ n: Int) -> UnsafeRawBufferPointer? {
        guard !bad, n >= 0, p.count >= n else { bad = true; return nil }
        let v = UnsafeRawBufferPointer(rebasing: p[..<n])
        p = UnsafeRawBufferPointer(rebasing: p[n...])
        return v
    }
    mutating func u8() -> UInt8 { take(1)?[0] ?? 0 }
    mutating func u32() -> UInt32 { take(4).map { UInt32(littleEndian: $0.loadUnaligned(as: UInt32.self)) } ?? 0 }
    mutating func u64() -> UInt64 { take(8).map { UInt64(littleEndian: $0.loadUnaligned(as: UInt64.self)) } ?? 0 }
    mutating func string() -> String {
        let n = Int(u32())
        return take(n).map { String(decoding: $0, as: UTF8.self) } ?? ""
    }
}

// The library's functions, for the host. Each is called on the main
// thread.

/// Registers the app's views and modules (its AsukaAppNatives), and keeps
/// the host's functions. Returns what was registered, for the host to
/// list with package native: a buffer it frees with asuka_natives_free,
/// its u32 length first, then u32 count, and for each a u8 (1: a view
/// kind, 2: a module method) and its names (kind; module, method).
@_cdecl("asuka_natives_boot")
public func asukaNativesBoot(_ emit: AsukaMacEmit, _ reply: AsukaMacReply) -> UInt {
    MainActor.assumeIsolated {
        let rt = AsukaRuntime.shared
        rt.host = AsukaMacHost(emit: emit, reply: reply)
        (NSClassFromString("AsukaAppNatives") as? AsukaNativeModule.Type)?.register()
        var w = AsukaMacWriter()
        var entries = 0
        var body = AsukaMacWriter()
        for kind in rt.factories.keys.sorted() {
            body.u8(1); body.string(kind)
            entries += 1
        }
        for (name, methods) in rt.modules.sorted(by: { $0.key < $1.key }) {
            for m in methods.keys.sorted() {
                body.u8(2); body.string(name); body.string(m)
                entries += 1
            }
        }
        w.u32(UInt32(4 + 4 + body.bytes.count))
        w.u32(UInt32(entries))
        w.bytes.append(contentsOf: body.bytes)
        let out = malloc(w.bytes.count)!
        w.bytes.withUnsafeBytes { out.copyMemory(from: $0.baseAddress!, byteCount: $0.count) }
        return UInt(bitPattern: out)
    }
}

@_cdecl("asuka_natives_free")
public func asukaNativesFree(_ p: UnsafeMutableRawPointer?) { free(p) }

/// Makes a view of a kind, as the host's view id: its NSView (0: none).
@_cdecl("asuka_natives_make")
public func asukaNativesMake(_ kind: UnsafePointer<CChar>, _ id: UInt64) -> UInt {
    MainActor.assumeIsolated {
        let rt = AsukaRuntime.shared
        guard let make = rt.factories[String(cString: kind)] else { return 0 }
        let v = make(AsukaNativeContext(id: id))
        rt.views[id] = v
        return UInt(bitPattern: Unmanaged.passUnretained(v.view).toOpaque())
    }
}

/// Sets a prop of a view: its name, a u8 type, and its value, as
/// ui.Native sends it.
@_cdecl("asuka_natives_set")
public func asukaNativesSet(_ id: UInt64, _ p: UnsafeRawPointer?, _ n: Int) {
    MainActor.assumeIsolated {
        guard let v = AsukaRuntime.shared.views[id], let p else { return }
        var r = AsukaMacReader(p: UnsafeRawBufferPointer(start: p, count: n))
        let name = r.string()
        let value: AsukaNativeValue
        switch r.u8() {
        case 1: value = .bool(r.u8() != 0)
        case 2: value = .int(Int64(bitPattern: r.u64()))
        case 3: value = .float(Double(bitPattern: r.u64()))
        case 4: value = .string(r.string())
        default: return
        }
        if !r.bad { v.set(name, value) }
    }
}

/// A view's size for the layout's offer (NaN: unbounded), into out[0]
/// and out[1].
@_cdecl("asuka_natives_measure")
public func asukaNativesMeasure(_ id: UInt64, _ width: Double, _ height: Double, _ out: UnsafeMutablePointer<Double>) {
    MainActor.assumeIsolated {
        guard let v = AsukaRuntime.shared.views[id] else { out[0] = 0; out[1] = 0; return }
        let unbounded = CGFloat.greatestFiniteMagnitude
        let s = v.sizeThatFits(CGSize(width: width.isFinite ? width : unbounded, height: height.isFinite ? height : unbounded))
        out[0] = Double(s.width)
        out[1] = Double(s.height)
    }
}

/// Lets a view go, as the core destroys it.
@_cdecl("asuka_natives_destroy")
public func asukaNativesDestroy(_ id: UInt64) {
    MainActor.assumeIsolated {
        AsukaRuntime.shared.views.removeValue(forKey: id)?.destroy()
    }
}

/// Runs a module's method; its answer comes back with the host's reply,
/// on the main thread, with call.
@_cdecl("asuka_natives_call")
public func asukaNativesCall(_ module: UnsafePointer<CChar>, _ method: UnsafePointer<CChar>, _ p: UnsafeRawPointer?, _ n: Int, _ call: UInt64) {
    MainActor.assumeIsolated {
        let rt = AsukaRuntime.shared
        guard let host = rt.host else { return }
        let args = p.map { Data(bytes: $0, count: n) } ?? Data()
        let lock = NSLock()
        var answered = false
        let reply: (Result<Data, Error>) -> Void = { res in
            lock.lock()
            let first = !answered
            answered = true
            lock.unlock()
            guard first else { return }
            DispatchQueue.main.async {
                switch res {
                case .success(let d):
                    d.withUnsafeBytes { host.reply(call, 1, $0.bindMemory(to: UInt8.self).baseAddress, $0.count) }
                case .failure(let e):
                    let m = Array(e.localizedDescription.utf8)
                    m.withUnsafeBufferPointer { host.reply(call, 0, $0.baseAddress, $0.count) }
                }
            }
        }
        let name = String(cString: module), m = String(cString: method)
        guard let f = rt.modules[name]?[m] else {
            reply(.failure(AsukaMacError(message: "no module method \(name).\(m)")))
            return
        }
        f(args, reply)
    }
}
