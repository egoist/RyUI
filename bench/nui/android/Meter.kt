// The benchmarks' meter on Android, the same file in every app that's
// measured (bench/nui, bench/compose): what it measures is the main
// thread's work, as each framework does it, frame by frame.
//
// A frame's cost is what the system's FrameMetrics count on the main
// thread: input, animation (Choreographer callbacks: nui's frame events,
// Compose's recomposition), measure and layout, recording the drawing,
// and the hand-off to the render thread. The render thread's and the
// GPU's work isn't in it; each frame's whole time, start to finish, is
// reported too ("wall"). A dropped frame is one that missed its
// deadline.
package dev.nui.app

import android.app.Activity
import android.os.Debug
import android.os.Handler
import android.os.HandlerThread
import android.os.Looper
import android.os.Process
import android.os.SystemClock
import android.util.Log
import android.view.FrameMetrics
import android.view.Window
import org.json.JSONObject

object BenchMeter {
    private class Frame(val vsync: Long, val busyMs: Double, val totalMs: Double, val late: Boolean)

    private val frames = ArrayList<Frame>() // guarded by itself
    private var installed = false
    private var budgetMs = 1000.0 / 60
    private val main = Handler(Looper.getMainLooper())
    private var name = ""
    private var begun = 0L

    /** Starts watching the activity's frames; call when it's created. */
    fun install(activity: Activity) {
        if (installed) return
        installed = true
        @Suppress("DEPRECATION")
        val hz = activity.windowManager.defaultDisplay.refreshRate
        if (hz > 0) budgetMs = 1000.0 / hz
        val thread = HandlerThread("bench-frames").apply { start() }
        activity.window.addOnFrameMetricsAvailableListener(Window.OnFrameMetricsAvailableListener { _, m, _ ->
            fun ms(id: Int) = m.getMetric(id) / 1e6
            val busy = ms(FrameMetrics.INPUT_HANDLING_DURATION) + ms(FrameMetrics.ANIMATION_DURATION) +
                ms(FrameMetrics.LAYOUT_MEASURE_DURATION) + ms(FrameMetrics.DRAW_DURATION) + ms(FrameMetrics.SYNC_DURATION)
            val total = ms(FrameMetrics.TOTAL_DURATION)
            // Late: past the frame's own deadline (Android 12), or a frame's time.
            val deadline = if (android.os.Build.VERSION.SDK_INT >= 31) ms(FrameMetrics.DEADLINE) else budgetMs
            val f = Frame(m.getMetric(FrameMetrics.INTENDED_VSYNC_TIMESTAMP), busy, total, total > deadline)
            synchronized(frames) { frames.add(f) }
        }, Handler(thread.looper))
    }

    /** Starts recording a scenario's frames: those from the next. */
    fun begin(scenario: String) {
        name = scenario
        begun = System.nanoTime()
    }

    /**
     * Stops recording and reports the scenario: its frames' busy time
     * (mean, median, 95th percentile, worst, total) and dropped frames.
     * The system reports frames a little after they're drawn: this
     * reports half a second later, counting the frames that began in it.
     */
    fun end() {
        val scenario = name
        val from = begun
        val to = System.nanoTime()
        main.postDelayed({
            val fs = synchronized(frames) { frames.filter { it.vsync in from until to }.also { frames.removeAll { f -> f.vsync < to } } }
            val busy = fs.map { it.busyMs }.sorted()
            fun pct(p: Double) = if (busy.isEmpty()) 0.0 else busy[minOf(busy.size - 1, (busy.size * p).toInt())]
            val total = busy.sum()
            report(JSONObject().apply {
                put("scenario", scenario); put("frames", fs.size)
                put("mean", if (fs.isEmpty()) 0.0 else total / fs.size)
                put("p50", pct(0.5)); put("p95", pct(0.95)); put("max", busy.lastOrNull() ?: 0.0); put("total", total)
                put("dropped", fs.count { it.late })
                put("wall", if (fs.isEmpty()) 0.0 else fs.sumOf { it.totalMs } / fs.size) // with the render thread and GPU
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
        Log.i("NUIBENCH", o.toString())
    }
}
