#if canImport(UIKit)
import UIKit
import Darwin
import MachO
// Its own framework, loaded at launch if linked: only in an app that
// reads its crash reports (crash.Reports, crash.OnReport; see the CLI's
// iosFeatures).
#if RYUI_CRASH
import MetricKit
#endif
#if canImport(CRyUI)
@_implementationOnly import CRyUI
#endif

/// ryui/crash's native side. The Go code keeps its own crashes (its crash
/// output) and the run's context; the host writes what only it sees, in
/// the directory's inbox, for the Go code to read into reports:
///
/// - an uncaught Objective-C or Swift exception (NSException), as the
///   app ends: inbox/exception-<UUID>.json, with the stack's return
///   addresses and the binaries they're in, for symbolicating;
/// - MetricKit's diagnostics (crashes of every kind, hangs, CPU and disk
///   exceptions, slow launches), which iOS delivers on a later launch:
///   inbox/metrickit-<UUID>.json, the payload's JSON as it is.
///
/// Nothing's installed if the app doesn't import ryui/crash (the Go code
/// says so).
enum RyUICrash {
    nonisolated(unsafe) private static var inbox: String?
    nonisolated(unsafe) private static var previous: (@convention(c) (NSException) -> Void)?

    /// Starts the run's record and the native handlers, as the host boots,
    /// before the Go code's main.
    static func start() {
        guard inbox == nil else { return }
        guard let p = infoJSON().withCString({ ryui_crash_start($0) }) else { return }
        let dir = String(cString: p)
        free(p)
        guard !dir.isEmpty else { return }
        inbox = dir + "/inbox"
        try? FileManager.default.createDirectory(atPath: dir + "/inbox", withIntermediateDirectories: true)
        // Another handler (an SDK's) keeps getting them, after.
        previous = NSGetUncaughtExceptionHandler()
        NSSetUncaughtExceptionHandler { e in
            RyUICrash.record(e)
            RyUICrash.previous?(e)
        }
        #if RYUI_CRASH
        // The app's process gets the diagnostics, its share extension's too.
        if !Bundle.main.bundlePath.hasSuffix(".appex") {
            MXMetricManager.shared.add(RyUIMetricSubscriber.shared)
        }
        #endif
    }

    /// The device and the app, as JSON for ryui_crash_start.
    private static func infoJSON() -> String {
        let info = Bundle.main.infoDictionary ?? [:]
        let d: [String: String] = [
            "model": modelIdentifier(),
            "system": UIDevice.current.systemName,
            "systemVersion": UIDevice.current.systemVersion,
            "appVersion": info["CFBundleShortVersionString"] as? String ?? "",
            "build": info["CFBundleVersion"] as? String ?? "",
        ]
        guard let b = try? JSONSerialization.data(withJSONObject: d) else { return "{}" }
        return String(decoding: b, as: UTF8.self)
    }

    /// The model's identifier ("iPhone17,1"), the simulated one's on the
    /// simulator.
    private static func modelIdentifier() -> String {
        if let sim = ProcessInfo.processInfo.environment["SIMULATOR_MODEL_IDENTIFIER"] { return sim }
        var u = utsname()
        uname(&u)
        return withUnsafeBytes(of: &u.machine) { String(decoding: $0.prefix(while: { $0 != 0 }), as: UTF8.self) }
    }

    /// Writes an uncaught exception, on the thread that threw it, as the
    /// app ends.
    private static func record(_ e: NSException) {
        var frames: [[String: Any]] = []
        for n in e.callStackReturnAddresses {
            frames.append(frame(n.uintValue))
        }
        let thread = Thread.isMainThread ? "main" : (Thread.current.name.flatMap { $0.isEmpty ? nil : $0 } ?? "Thread")
        let d: [String: Any] = [
            "time": Int64(Date().timeIntervalSince1970 * 1000),
            "pid": Int(getpid()),
            "thread": thread,
            "name": e.name.rawValue,
            "reason": e.reason ?? "",
            "stack": e.callStackSymbols.joined(separator: "\n"),
            "frames": frames,
        ]
        guard let b = try? JSONSerialization.data(withJSONObject: d) else { return }
        write("exception-", b)
    }

    /// A return address's frame: its binary, the binary's UUID, its offset
    /// in it, and the symbol, if the binary has one.
    private static func frame(_ addr: UInt) -> [String: Any] {
        var f: [String: Any] = ["address": addr]
        var info = Dl_info()
        guard let p = UnsafeRawPointer(bitPattern: addr), dladdr(p, &info) != 0, let base = info.dli_fbase else { return f }
        let start = UInt(bitPattern: base)
        f["offset"] = addr - start
        if let name = info.dli_fname { f["binary"] = (String(cString: name) as NSString).lastPathComponent }
        if let sym = info.dli_sname { f["function"] = String(cString: sym) }
        if let uuid = imageUUID(base) { f["binaryId"] = uuid }
        return f
    }

    /// The Mach-O image's UUID (its LC_UUID), as dSYMs and MetricKit say it.
    private static func imageUUID(_ base: UnsafeMutableRawPointer) -> String? {
        let header = base.assumingMemoryBound(to: mach_header_64.self)
        guard header.pointee.magic == MH_MAGIC_64 else { return nil }
        var cmd = UnsafeRawPointer(base) + MemoryLayout<mach_header_64>.size
        for _ in 0..<header.pointee.ncmds {
            let lc = cmd.assumingMemoryBound(to: load_command.self).pointee
            if lc.cmd == UInt32(LC_UUID) {
                let u = cmd.assumingMemoryBound(to: uuid_command.self).pointee.uuid
                return UUID(uuid: u).uuidString
            }
            cmd += Int(lc.cmdsize)
        }
        return nil
    }

    /// Writes b as a new file of the inbox, whole or not at all.
    fileprivate static func write(_ prefix: String, _ b: Data) {
        guard let dir = inbox else { return }
        let path = dir + "/" + prefix + UUID().uuidString + ".json"
        let tmp = path + ".tmp"
        guard FileManager.default.createFile(atPath: tmp, contents: b) else { return }
        rename(tmp, path)
    }
}

#if RYUI_CRASH
/// MetricKit's subscriber: each diagnostic payload iOS delivers (on a
/// launch after what it's of) goes in the inbox, and the Go code hears of
/// it (crash.OnReport).
final class RyUIMetricSubscriber: NSObject, MXMetricManagerSubscriber {
    nonisolated(unsafe) static let shared = RyUIMetricSubscriber()

    func didReceive(_ payloads: [MXDiagnosticPayload]) {
        for p in payloads {
            RyUICrash.write("metrickit-", p.jsonRepresentation())
        }
        ryui_crash_notify()
    }
}
#endif
#endif
