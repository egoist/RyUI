// Typed native modules (//asuka:native) and views (//asuka:view): what
// the Swift that asuka native generates for a Go package calls, and what
// the host's transport calls (Go's calls, from any thread). The same file
// is in the iOS host and the Mac's runtime (platform/macos), which a test
// keeps equal.
//
// The wire format is package native's: values in declaration order,
// little-endian.

import Foundation
#if canImport(UIKit)
import UIKit
/// The platform's view: UIView on iOS, NSView on the Mac.
public typealias AsukaPlatformView = UIView
#else
import AppKit
/// The platform's view: UIView on iOS, NSView on the Mac.
public typealias AsukaPlatformView = NSView
#endif

// Where the host has views: iOS (UIKit), and the Mac's runtime (which has
// no AsukaOps); not the iOS host's package built for the Mac (swift test,
// for its decoder's tests: AsukaOps is a module of its own there).
#if canImport(UIKit) || !canImport(AsukaOps)

/// Encodes values (package native's Writer).
public struct AsukaNativeWriter {
    public private(set) var bytes: [UInt8]

    public init() {
        bytes = []
        bytes.reserveCapacity(64)
    }

    /// An answer's writer: its status, ok, first.
    public static func ok() -> AsukaNativeWriter {
        var w = AsukaNativeWriter()
        w.bytes.append(0)
        return w
    }

    @inline(__always) mutating func put<T: FixedWidthInteger>(_ v: T) {
        withUnsafeBytes(of: v.littleEndian) { bytes.append(contentsOf: $0) }
    }

    public mutating func bool(_ v: Bool) { bytes.append(v ? 1 : 0) }
    public mutating func int8(_ v: Int8) { bytes.append(UInt8(bitPattern: v)) }
    public mutating func int16(_ v: Int16) { put(v) }
    public mutating func int32(_ v: Int32) { put(v) }
    public mutating func int64(_ v: Int64) { put(v) }
    public mutating func int(_ v: Int) { put(Int64(v)) }
    public mutating func uint8(_ v: UInt8) { bytes.append(v) }
    public mutating func uint16(_ v: UInt16) { put(v) }
    public mutating func uint32(_ v: UInt32) { put(v) }
    public mutating func uint64(_ v: UInt64) { put(v) }
    public mutating func uint(_ v: UInt) { put(UInt64(v)) }
    public mutating func float32(_ v: Float) { put(v.bitPattern) }
    public mutating func float64(_ v: Double) { put(v.bitPattern) }

    public mutating func string(_ v: String) {
        var v = v
        v.withUTF8 { b in
            put(UInt32(b.count))
            bytes.append(contentsOf: b)
        }
    }

    public mutating func data(_ v: Data) {
        put(UInt32(v.count))
        bytes.append(contentsOf: v)
    }

    /// A time: seconds and nanoseconds since 1970.
    public mutating func time(_ v: Date) {
        let t = v.timeIntervalSince1970
        var s = t.rounded(.down)
        var ns = ((t - s) * 1e9).rounded()
        if ns >= 1e9 {
            s += 1
            ns -= 1e9
        }
        put(Int64(s))
        put(Int32(ns))
    }

    /// A duration, in seconds (Go's: nanoseconds).
    public mutating func duration(_ v: TimeInterval) { put(Int64((v * 1e9).rounded())) }
    public mutating func len(_ n: Int) { put(UInt32(n)) }
    public mutating func present(_ v: Bool) { bool(v) }
}

/// Decodes values (package native's Reader). After a short read every
/// read returns its zero value, and failed is true.
public struct AsukaNativeReader {
    let bytes: [UInt8]
    var p = 0
    public private(set) var failed = false

    public init(_ bytes: [UInt8]) { self.bytes = bytes }
    public init(_ data: Data) { self.bytes = [UInt8](data) }

    @inline(__always) mutating func take(_ n: Int) -> ArraySlice<UInt8>? {
        guard !failed, n >= 0, bytes.count - p >= n else {
            failed = true
            return nil
        }
        defer { p += n }
        return bytes[p..<p + n]
    }

    @inline(__always) mutating func get<T: FixedWidthInteger>(_: T.Type) -> T {
        guard let s = take(MemoryLayout<T>.size) else { return 0 }
        var v: T = 0
        withUnsafeMutableBytes(of: &v) { $0.copyBytes(from: s) }
        return T(littleEndian: v)
    }

    public mutating func bool() -> Bool { get(UInt8.self) != 0 }
    public mutating func int8() -> Int8 { Int8(bitPattern: get(UInt8.self)) }
    public mutating func int16() -> Int16 { get(Int16.self) }
    public mutating func int32() -> Int32 { get(Int32.self) }
    public mutating func int64() -> Int64 { get(Int64.self) }
    public mutating func int() -> Int { Int(get(Int64.self)) }
    public mutating func uint8() -> UInt8 { get(UInt8.self) }
    public mutating func uint16() -> UInt16 { get(UInt16.self) }
    public mutating func uint32() -> UInt32 { get(UInt32.self) }
    public mutating func uint64() -> UInt64 { get(UInt64.self) }
    public mutating func uint() -> UInt { UInt(get(UInt64.self)) }
    public mutating func float32() -> Float { Float(bitPattern: get(UInt32.self)) }
    public mutating func float64() -> Double { Double(bitPattern: get(UInt64.self)) }

    public mutating func string() -> String {
        let n = Int(get(UInt32.self))
        guard let s = take(n) else { return "" }
        return String(decoding: s, as: UTF8.self)
    }

    public mutating func data() -> Data {
        let n = Int(get(UInt32.self))
        guard let s = take(n) else { return Data() }
        return Data(s)
    }

    public mutating func time() -> Date {
        let s = get(Int64.self)
        let ns = get(Int32.self)
        return Date(timeIntervalSince1970: Double(s) + Double(ns) / 1e9)
    }

    public mutating func duration() -> TimeInterval { Double(get(Int64.self)) / 1e9 }

    /// A slice's or a map's length, bounded by what's left.
    public mutating func len() -> Int {
        let n = Int(get(UInt32.self))
        if n > bytes.count - p {
            failed = true
            return 0
        }
        return n
    }

    public mutating func present() -> Bool { bool() }
    /// A callback's id (0: none).
    public mutating func callback() -> UInt64 { get(UInt64.self) }

    /// Throws when a read went past what's there.
    public func check() throws {
        if failed { throw AsukaNativeError.malformed }
    }
}

/// An error a package declares (native.NewError): its Go gets that value.
public protocol AsukaDeclaredError: Error {
    var asukaIndex: UInt32 { get }
}

/// The runtime's own errors.
public enum AsukaNativeError: Error, LocalizedError {
    /// What the platform doesn't have: Go gets errors.ErrUnsupported.
    case unsupported(String)
    case malformed
    case closed

    public var errorDescription: String? {
        switch self {
        case .unsupported(let m): return m
        case .malformed: return "asuka: a malformed call"
        case .closed: return "asuka: the object is closed"
        }
    }
}

/// A typed view's custom size (on the Mac; on iOS its view's
/// sizeThatFits is it).
public protocol AsukaSizing {
    @MainActor func sizeThatFits(_ size: CGSize) -> CGSize
}

/// A typed view that frees what it holds as the core destroys it.
public protocol AsukaDestroying {
    @MainActor func destroy()
}

/// A Go package's native code, as asuka native generates it: the host
/// finds it by its Objective-C name (the module's Swift, in Go).
public protocol AsukaModuleProvider: AnyObject {
    static func make(_ iface: UInt32) throws -> AnyObject
    static func call(_ object: AnyObject, _ iface: UInt32, _ method: UInt32, _ r: inout AsukaNativeReader, _ w: inout AsukaNativeWriter) throws
    static func start(_ object: AnyObject, _ iface: UInt32, _ method: UInt32, _ input: AsukaNativeReader, _ done: @escaping (Result<AsukaNativeWriter, Error>) -> Void) -> Task<Void, Never>?
    @MainActor static func makeView(_ name: String, _ ctx: AsukaNativeContext) -> AsukaNativeView?
}

/// The typed modules' runtime: the objects Go holds, by handle; the
/// calls it started; and the way back to Go (done, invoke), which the
/// host's transport sets.
public enum AsukaNative {
    struct Entry {
        let object: AnyObject
        let provider: AsukaModuleProvider.Type
        let iface: UInt32
    }

    static let lock = NSLock()
    static var objects: [UInt64: Entry] = [:]
    static var nextHandle: UInt64 = 0
    static var tasks: [UInt64: Task<Void, Never>] = [:]
    static var finished: Set<UInt64> = []
    static var providers: [String: AsukaModuleProvider.Type] = [:]

    /// Answers a started call (the host's: to Go).
    public static var done: (UInt64, [UInt8]) -> Void = { _, _ in }
    /// Calls a Go callback: its answer, nil for a notification (the
    /// host's: to Go).
    public static var invoke: (UInt64, [UInt8]) -> [UInt8]? = { _, _ in nil }

    /// The provider of a class's name, looked up once.
    public static func provider(_ name: String) -> AsukaModuleProvider.Type? {
        lock.lock()
        defer { lock.unlock() }
        if let p = providers[name] { return p }
        guard let p = NSClassFromString(name) as? AsukaModuleProvider.Type else { return nil }
        providers[name] = p
        return p
    }

    /// Keeps an object for Go: its handle.
    public static func retain(_ o: AnyObject, _ p: AsukaModuleProvider.Type, _ iface: UInt32) -> UInt64 {
        lock.lock()
        defer { lock.unlock() }
        nextHandle += 1
        objects[nextHandle] = Entry(object: o, provider: p, iface: iface)
        return nextHandle
    }

    /// The object of a handle Go passed (nil for 0, or one let go).
    public static func object(_ h: UInt64) -> AnyObject? {
        lock.lock()
        defer { lock.unlock() }
        return objects[h]?.object
    }

    static func entry(_ h: UInt64) -> Entry? {
        lock.lock()
        defer { lock.unlock() }
        return objects[h]
    }

    // What Go calls, through the host's transport.

    /// Makes an object of a module's interface.
    public static func new(_ cls: String, _ iface: UInt32) -> [UInt8] {
        guard let p = provider(cls) else {
            return answer(AsukaNativeError.unsupported("no Swift \(cls): build the app with its packages' native code"))
        }
        do {
            let o = try p.make(iface)
            var w = AsukaNativeWriter.ok()
            w.uint64(retain(o, p, iface))
            return w.bytes
        } catch {
            return answer(error)
        }
    }

    /// Calls an object's method, on this thread.
    public static func call(_ h: UInt64, _ method: UInt32, _ input: [UInt8]) -> [UInt8] {
        guard let e = entry(h) else { return answer(AsukaNativeError.closed) }
        var r = AsukaNativeReader(input)
        var w = AsukaNativeWriter.ok()
        do {
            try e.provider.call(e.object, e.iface, method, &r, &w)
            return w.bytes
        } catch {
            return answer(error)
        }
    }

    /// Starts a method that takes a context; done answers it.
    public static func start(_ h: UInt64, _ method: UInt32, _ input: [UInt8], _ call: UInt64) {
        guard let e = entry(h) else {
            done(call, answer(AsukaNativeError.closed))
            return
        }
        let task = e.provider.start(e.object, e.iface, method, AsukaNativeReader(input)) { finish(call, $0) }
        lock.lock()
        if finished.remove(call) == nil, let task { tasks[call] = task }
        lock.unlock()
    }

    static func finish(_ call: UInt64, _ result: Result<AsukaNativeWriter, Error>) {
        lock.lock()
        if tasks.removeValue(forKey: call) == nil { finished.insert(call) }
        lock.unlock()
        switch result {
        case .success(let w): done(call, w.bytes)
        case .failure(let e): done(call, answer(e))
        }
    }

    /// Cancels a started call (its task), which still answers.
    public static func cancel(_ call: UInt64) {
        lock.lock()
        let t = tasks[call]
        lock.unlock()
        t?.cancel()
    }

    /// Lets an object go.
    public static func release(_ h: UInt64) {
        lock.lock()
        let e = objects.removeValue(forKey: h)
        lock.unlock()
        if let e, !Thread.isMainThread {
            // Its last reference goes on the main thread, where a view or
            // a main-thread object expects to be freed.
            DispatchQueue.main.async { _ = e }
        }
    }

    // What the generated Swift calls.

    /// Calls a Go callback that answers nothing (it runs on Go's main thread).
    public static func notify(_ id: UInt64, _ w: AsukaNativeWriter) {
        if id != 0 { _ = invoke(id, w.bytes) }
    }

    /// Calls a Go callback and reads its answer.
    public static func ask(_ id: UInt64, _ w: AsukaNativeWriter) -> AsukaNativeReader {
        AsukaNativeReader(id == 0 ? [] : (invoke(id, w.bytes) ?? []))
    }

    final class Box<T> { var v: Result<T, Error>? }

    /// Runs body on the main thread: now if this is it, else waiting for it.
    public static func onMain<T>(_ body: @MainActor () throws -> T) throws -> T {
        let box = Box<T>()
        if Thread.isMainThread {
            MainActor.assumeIsolated { box.v = Result { try body() } }
        } else {
            DispatchQueue.main.sync { MainActor.assumeIsolated { box.v = Result { try body() } } }
        }
        return try box.v!.get()
    }

    /// An error's answer: declared (its index), unsupported, canceled, or
    /// its domain, code and message.
    public static func answer(_ e: Error) -> [UInt8] {
        var w = AsukaNativeWriter()
        if let d = e as? AsukaDeclaredError {
            w.uint8(1)
            w.uint32(d.asukaIndex)
        } else if case AsukaNativeError.unsupported(let m) = e {
            w.uint8(3)
            w.string(m)
        } else if e is CancellationError {
            w.uint8(4)
        } else {
            let ns = e as NSError
            w.uint8(2)
            w.string(ns.domain)
            w.int64(Int64(ns.code))
            w.string((e as? LocalizedError)?.errorDescription ?? ns.localizedDescription)
        }
        return w.bytes
    }

    /// A typed view of a kind (its module's class # its name).
    @MainActor public static func makeView(_ kind: String, _ ctx: AsukaNativeContext) -> AsukaNativeView? {
        guard let i = kind.firstIndex(of: "#"), let p = provider(String(kind[..<i])) else { return nil }
        return p.makeView(String(kind[kind.index(after: i)...]), ctx)
    }

    /// Puts an answer where Go's transport reads it: into out when it fits
    /// (its length returned), else into memory of its own (*big, which
    /// asukaNativeFree frees).
    public static func write(_ a: [UInt8], _ out: UnsafeMutablePointer<UInt8>?, _ cap: Int, _ big: UnsafeMutablePointer<UnsafeMutablePointer<UInt8>?>?) -> Int {
        a.withUnsafeBufferPointer { src in
            if a.count <= cap, let out {
                out.update(from: src.baseAddress!, count: a.count)
            } else {
                let m = malloc(max(a.count, 1))!.assumingMemoryBound(to: UInt8.self)
                m.update(from: src.baseAddress!, count: a.count)
                big?.pointee = m
            }
        }
        return a.count
    }
}

#endif
