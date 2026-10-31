// The benchmarks' meter on iOS, the same file in every app that's
// measured (bench/ryui, bench/swiftui): what it measures is the main
// thread's work, as each framework does it, frame by frame.
//
// A frame's cost is the main thread's busy time between two vsyncs: the
// run loop's time awake (from a CFRunLoopObserver's AfterWaiting to its
// BeforeWaiting), which holds the framework's update, layout, and Core
// Animation's commit. The render server's work is the system's, the same
// for every framework, and isn't counted. A dropped frame is a vsync the
// display link missed: an interval half again as long as the display's.
import UIKit
import QuartzCore
import Darwin

final class BenchMeter: NSObject {
    static let shared = BenchMeter()

    private var link: CADisplayLink?
    private var observer: CFRunLoopObserver?
    private var awake: CFTimeInterval = 0 // when the run loop last woke
    private var busy: CFTimeInterval = 0 // since the last vsync
    private var lastVsync: CFTimeInterval = 0
    private var interval: CFTimeInterval = 1.0 / 60

    private var name = ""
    private var recording = false
    private var frames: [Double] = [] // ms busy, per frame
    private var gaps: [Double] = [] // ms between vsyncs

    /// Starts watching the main thread; call at launch.
    func install() {
        guard observer == nil else { return }
        let o = CFRunLoopObserverCreateWithHandler(nil, CFRunLoopActivity.afterWaiting.rawValue | CFRunLoopActivity.beforeWaiting.rawValue, true, 0) { [unowned self] _, activity in
            let now = CACurrentMediaTime()
            if activity == .afterWaiting {
                self.awake = now
            } else {
                self.busy += now - self.awake
                self.awake = now
            }
        }
        CFRunLoopAddObserver(CFRunLoopGetMain(), o, .commonModes)
        observer = o
        awake = CACurrentMediaTime()
        let l = CADisplayLink(target: self, selector: #selector(tick(_:)))
        l.add(to: .main, forMode: .common)
        link = l
    }

    @objc private func tick(_ l: CADisplayLink) {
        // Inside a busy stretch: what's run of it so far is the frame's.
        let now = CACurrentMediaTime()
        busy += now - awake
        awake = now
        if l.duration > 0 { interval = l.duration }
        if recording, lastVsync > 0 {
            frames.append(busy * 1000)
            gaps.append((l.timestamp - lastVsync) * 1000)
        }
        busy = 0
        lastVsync = l.timestamp
    }

    /// Starts recording a scenario's frames.
    func begin(_ scenario: String) {
        name = scenario
        frames = []
        gaps = []
        recording = true
    }

    /// Stops recording and reports the scenario: its frames' busy time
    /// (mean, median, 95th percentile, worst, total) and dropped frames.
    func end() {
        recording = false
        let sorted = frames.sorted()
        func pct(_ p: Double) -> Double { sorted.isEmpty ? 0 : sorted[min(sorted.count - 1, Int(Double(sorted.count) * p))] }
        let total = frames.reduce(0, +)
        let dropped = gaps.filter { $0 > interval * 1000 * 1.5 }.count
        report([
            "scenario": name, "frames": frames.count,
            "mean": frames.isEmpty ? 0 : total / Double(frames.count),
            "p50": pct(0.5), "p95": pct(0.95), "max": sorted.last ?? 0, "total": total,
            "dropped": dropped,
        ])
    }

    /// Reports the time from the process's start to now (call it at the
    /// first frame that shows the app's content).
    func startup() {
        var info = kinfo_proc()
        var size = MemoryLayout<kinfo_proc>.size
        var mib: [Int32] = [CTL_KERN, KERN_PROC, KERN_PROC_PID, getpid()]
        sysctl(&mib, 4, &info, &size, nil, 0)
        let start = info.kp_proc.p_un.__p_starttime
        let startS = Double(start.tv_sec) + Double(start.tv_usec) / 1e6
        let ms = (Date().timeIntervalSince1970 - startS) * 1000
        report(["scenario": "startup", "total": ms])
    }

    /// Reports the app's memory footprint (what iOS counts against it,
    /// and limits it by), now and its peak since launch, in MB.
    func memory(_ scenario: String) {
        var info = task_vm_info_data_t()
        var count = mach_msg_type_number_t(MemoryLayout<task_vm_info_data_t>.size / MemoryLayout<integer_t>.size)
        let kr = withUnsafeMutablePointer(to: &info) {
            $0.withMemoryRebound(to: integer_t.self, capacity: Int(count)) { task_info(mach_task_self_, task_flavor_t(TASK_VM_INFO), $0, &count) }
        }
        let ok = kr == KERN_SUCCESS
        report(["scenario": scenario,
                "total": ok ? Double(info.phys_footprint) / 1_048_576 : -1,
                "peak": ok ? Double(info.ledger_phys_footprint_peak) / 1_048_576 : -1])
    }

    func done() { report(["scenario": "done"]) }

    private func report(_ fields: [String: Any]) {
        guard let d = try? JSONSerialization.data(withJSONObject: fields, options: [.sortedKeys]),
              let s = String(data: d, encoding: .utf8) else { return }
        print("RYUIBENCH " + s)
        fflush(stdout)
    }
}
