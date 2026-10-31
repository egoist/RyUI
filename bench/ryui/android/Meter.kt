// The benchmarks' meter on Android, the same file in every app that's
// measured (bench/ryui, bench/compose): what it measures is the main
// thread's work, as each framework does it, frame by frame, as the iOS
// meter does.
//
// A frame's cost is the main thread's busy time between two vsyncs: the
// time its looper spends dispatching messages (from the looper's message
// logging), which holds the Choreographer's frame (input, animation:
// RyUI's frame events, Compose's recomposition; measure and layout;
// recording the drawing; the hand-off to the render thread) and anything
// else the framework posts. A frame that draws nothing (an update out of
// sight) counts too. The render thread's and the GPU's work isn't in it:
// FrameMetrics give each drawn frame's whole time ("wall") and whether it
// missed its deadline ("dropped").
package dev.ryui.app

import android.app.Activity
import android.os.Debug
import android.os.Handler
import android.os.HandlerThread
import android.os.Looper
import android.os.Process
import android.os.SystemClock
import android.util.Log
import android.view.Choreographer
import android.view.FrameMetrics
import android.view.Window
import org.json.JSONObject

object BenchMeter {
    private class Drawn(val vsync: Long, val totalMs: Double, val late: Boolean)

    private val drawn = ArrayList<Drawn>() // guarded by itself
    private var installed = false
    private var budgetMs = 1000.0 / 60
    private val main = Handler(Looper.getMainLooper())

    // The main thread's busy time, by vsync.
    private var dispatching = 0L // when the message in progress began, or 0
    private var busy = 0L // ns since the last vsync
    private var recording = false
    private val frames = ArrayList<Double>() // ms busy, per frame
    private var name = ""
    private var begun = 0L

    private val tick = object : Choreographer.FrameCallback {
        override fun doFrame(nanos: Long) {
            // Inside a message: what's run of it so far is the last frame's.
            val now = System.nanoTime()
            if (dispatching != 0L) {
                busy += now - dispatching
                dispatching = now
            }
            if (recording) frames.add(busy / 1e6)
            busy = 0
            if (recording) Choreographer.getInstance().postFrameCallback(this)
        }
    }

    /** Starts watching the activity's frames; call when it's created. */
    fun install(activity: Activity) {
        if (installed) return
        installed = true
        @Suppress("DEPRECATION")
        val hz = activity.windowManager.defaultDisplay.refreshRate
        if (hz > 0) budgetMs = 1000.0 / hz
        // The looper logs ">>>>> Dispatching ..." before each message and
        // "<<<<< Finished ..." after it.
        Looper.getMainLooper().setMessageLogging { s ->
            val now = System.nanoTime()
            if (s.startsWith(">")) {
                dispatching = now
            } else if (dispatching != 0L) {
                busy += now - dispatching
                dispatching = 0
            }
        }
        val thread = HandlerThread("bench-frames").apply { start() }
        activity.window.addOnFrameMetricsAvailableListener(Window.OnFrameMetricsAvailableListener { _, m, _ ->
            val total = m.getMetric(FrameMetrics.TOTAL_DURATION) / 1e6
            // Late: past the frame's own deadline (Android 12), or a frame's time.
            val deadline = if (android.os.Build.VERSION.SDK_INT >= 31) m.getMetric(FrameMetrics.DEADLINE) / 1e6 else budgetMs
            val f = Drawn(m.getMetric(FrameMetrics.INTENDED_VSYNC_TIMESTAMP), total, total > deadline)
            synchronized(drawn) { drawn.add(f) }
        }, Handler(thread.looper))
    }

    /** Starts recording a scenario's frames: those from the next vsync. */
    fun begin(scenario: String) {
        name = scenario
        begun = System.nanoTime()
        frames.clear()
        recording = true
        Choreographer.getInstance().postFrameCallback(tick)
    }

    /**
     * Stops recording and reports the scenario: its frames' busy time
     * (mean, median, 95th percentile, worst, total), dropped frames and
     * the drawn frames' whole time. The system reports drawn frames a
     * little after they're drawn: this reports half a second later.
     */
    fun end() {
        recording = false
        Choreographer.getInstance().removeFrameCallback(tick)
        val scenario = name
        val from = begun
        val to = System.nanoTime()
        val busyMs = frames.sorted()
        main.postDelayed({
            val fs = synchronized(drawn) { drawn.filter { it.vsync in from until to }.also { drawn.removeAll { f -> f.vsync < to } } }
            fun pct(p: Double) = if (busyMs.isEmpty()) 0.0 else busyMs[minOf(busyMs.size - 1, (busyMs.size * p).toInt())]
            val total = busyMs.sum()
            report(JSONObject().apply {
                put("scenario", scenario); put("frames", busyMs.size)
                put("mean", if (busyMs.isEmpty()) 0.0 else total / busyMs.size)
                put("p50", pct(0.5)); put("p95", pct(0.95)); put("max", busyMs.lastOrNull() ?: 0.0); put("total", total)
                put("dropped", fs.count { it.late })
                put("wall", if (fs.isEmpty()) 0.0 else fs.sumOf { it.totalMs } / fs.size)
            })
        }, 500)
    }

    /** Reports the time from the process's start to now (call it at the first frame that shows the app's content). */
    fun startup() {
        val ms = SystemClock.uptimeMillis() - Process.getStartUptimeMillis()
        report(JSONObject().apply { put("scenario", "startup"); put("total", ms.toDouble()) })
    }

    /**
     * Reports the app's memory, in MB: its proportional set size now (what
     * Android accounts it), and its peak resident set since launch (the
     * kernel's high-water mark, VmHWM).
     */
    fun memory(scenario: String) {
        val mi = Debug.MemoryInfo()
        Debug.getMemoryInfo(mi)
        val hwm = try {
            java.io.File("/proc/self/status").readLines().first { it.startsWith("VmHWM:") }
                .split(Regex("\\s+"))[1].toDouble() / 1024
        } catch (_: Exception) { -1.0 }
        report(JSONObject().apply { put("scenario", scenario); put("total", mi.totalPss / 1024.0); put("peak", hwm) })
    }

    /** Reports the end of the run, after what end has yet to report. */
    fun done() = main.postDelayed({ report(JSONObject().apply { put("scenario", "done") }) }, 700)

    private fun report(o: JSONObject) {
        Log.i("RYUIBENCH", o.toString())
    }
}
