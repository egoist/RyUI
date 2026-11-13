// The Swift that packages' native code (//asuka:native, //asuka:view)
// builds on, on the Mac: with NativeModules.swift, the module Asuka the
// packages' Swift imports. The Asuka CLI builds it with them into a
// library in the app's bundle (Contents/Frameworks/AsukaNatives.dylib);
// the Mac host (host/darwin, Go) loads it as it starts and calls the
// functions at the end, through purego, so the app and its host still
// build with no cgo. The names are the iOS host's: a view is an
// AsukaNativeView, here holding an NSView.
import AppKit

/// A value of a native view's prop or event.
public enum AsukaNativeValue {
    case bool(Bool), int(Int64), float(Double), string(String)
    /// A typed view's prop or event (//asuka:view): its values, encoded.
    case bytes(Data)

    public var bool: Bool { if case .bool(let v) = self { return v }; return false }
    public var int: Int64 {
        switch self { case .int(let v): return v; case .float(let v): return Int64(v); default: return 0 }
    }
    public var float: Double {
        switch self { case .float(let v): return v; case .int(let v): return Double(v); default: return 0 }
    }
    public var string: String { if case .string(let v) = self { return v }; return "" }
    public var bytes: Data { if case .bytes(let v) = self { return v }; return Data() }
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
            case .bytes(let d): w.u8(5); w.u32(UInt32(d.count)); w.bytes.append(contentsOf: d)
            }
        }
        guard let host = AsukaRuntime.shared.host else { return }
        w.bytes.withUnsafeBufferPointer { host.emit(id, $0.baseAddress, $0.count) }
    }
}

/// The views the host shows, by its ids.
@MainActor final class AsukaRuntime {
    static let shared = AsukaRuntime()

    var views: [UInt64: AsukaNativeView] = [:]
    var host: AsukaMacHost?
}

// What the host gives the library: functions of its own (purego
// callbacks). emit is called on the main thread; done and invoke from
// any (a typed call's answer; a Go callback's call, its answer into
// memory the library frees).
public typealias AsukaMacEmit = @convention(c) (UInt64, UnsafePointer<UInt8>?, Int) -> Void
public typealias AsukaMacDone = @convention(c) (UInt64, UnsafePointer<UInt8>?, Int) -> Void
public typealias AsukaMacInvoke = @convention(c) (UInt64, UnsafePointer<UInt8>?, Int, UnsafeMutablePointer<UnsafeMutablePointer<UInt8>?>?) -> Int

struct AsukaMacHost {
    let emit: AsukaMacEmit
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
    mutating func data() -> Data {
        let n = Int(u32())
        return take(n).map { Data($0) } ?? Data()
    }
}

// The library's functions, for the host. Each is called on the main
// thread.

/// Keeps the host's functions: emit for views' events, done and invoke
/// for typed modules' answers and callbacks.
@_cdecl("asuka_natives_boot")
public func asukaNativesBoot(_ emit: AsukaMacEmit, _ done: AsukaMacDone, _ invoke: AsukaMacInvoke) {
    MainActor.assumeIsolated {
        AsukaRuntime.shared.host = AsukaMacHost(emit: emit)
    }
    AsukaNative.done = { call, b in b.withUnsafeBufferPointer { done(call, $0.baseAddress, $0.count) } }
    AsukaNative.invoke = { id, b in
        var out: UnsafeMutablePointer<UInt8>? = nil
        let n = b.withUnsafeBufferPointer { invoke(id, $0.baseAddress, $0.count, &out) }
        guard let out else { return nil }
        defer { free(out) }
        return Array(UnsafeBufferPointer(start: out, count: n))
    }
}

@_cdecl("asuka_natives_free")
public func asukaNativesFree(_ p: UnsafeMutableRawPointer?) { free(p) }

/// Makes a view of a kind, as the host's view id: its NSView (0: none).
@_cdecl("asuka_natives_make")
public func asukaNativesMake(_ kind: UnsafePointer<CChar>, _ id: UInt64) -> UInt {
    MainActor.assumeIsolated {
        let rt = AsukaRuntime.shared
        // A package's typed view: its module's class # its name.
        guard let v = AsukaNative.makeView(String(cString: kind), AsukaNativeContext(id: id)) else { return 0 }
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
        case 5: value = .bytes(r.data())
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

// Typed modules (NativeModules.swift), from Go's calls on any thread.

/// Makes an object of a module's interface; its answer into out (cap
/// bytes), else into *big, which asuka_natives_free frees.
@_cdecl("asuka_native_new")
public func asukaNativeNew(_ cls: UnsafePointer<UInt8>?, _ clsLen: Int, _ iface: UInt32, _ out: UnsafeMutablePointer<UInt8>?, _ cap: Int, _ big: UnsafeMutablePointer<UnsafeMutablePointer<UInt8>?>?) -> Int {
    let name = String(decoding: UnsafeBufferPointer(start: cls, count: clsLen), as: UTF8.self)
    return AsukaNative.write(AsukaNative.new(name, iface), out, cap, big)
}

/// Calls an object's method; its answer as asuka_native_new's.
@_cdecl("asuka_native_call")
public func asukaNativeCall(_ h: UInt64, _ method: UInt32, _ p: UnsafePointer<UInt8>?, _ n: Int, _ out: UnsafeMutablePointer<UInt8>?, _ cap: Int, _ big: UnsafeMutablePointer<UnsafeMutablePointer<UInt8>?>?) -> Int {
    let input = Array(UnsafeBufferPointer(start: p, count: n))
    return AsukaNative.write(AsukaNative.call(h, method, input), out, cap, big)
}

/// Starts a method that takes a context; the host's done answers it.
@_cdecl("asuka_native_start")
public func asukaNativeStart(_ h: UInt64, _ method: UInt32, _ p: UnsafePointer<UInt8>?, _ n: Int, _ call: UInt64) {
    AsukaNative.start(h, method, Array(UnsafeBufferPointer(start: p, count: n)), call)
}

@_cdecl("asuka_native_cancel")
public func asukaNativeCancel(_ call: UInt64) { AsukaNative.cancel(call) }

@_cdecl("asuka_native_release")
public func asukaNativeRelease(_ h: UInt64) { AsukaNative.release(h) }
