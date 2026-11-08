package dev.ryui

import android.app.ActivityManager
import android.app.ApplicationExitInfo
import android.content.Context
import android.os.Build
import android.os.Process
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.UUID

/**
 * ryui/crash's native side. The Go code keeps its own crashes (its crash
 * output) and the run's context; the host writes what only it sees, in
 * the directory's inbox, for the Go code to read into reports:
 *
 * - an uncaught JVM exception, as the app ends (the handler before it,
 *   the system's, still runs): inbox/exception-<UUID>.json, with every
 *   thread's stack;
 * - on the next launch, how the last runs ended, from
 *   ActivityManager.getHistoricalProcessExitReasons: native crashes
 *   with their tombstone, ANRs with their traces, low-memory kills,
 *   excessive resource use: inbox/exit-<time>-<pid>.json, and its trace
 *   in a .trace beside it.
 *   The last one written is remembered (exits, in the directory), so
 *   each is written once.
 *
 * Nothing's installed if the app doesn't import ryui/crash (the Go code
 * says so).
 */
object RyUICrash {
    @Volatile private var inbox: File? = null

    /** Starts the run's record and the native handlers, as the library loads, before the Go code's main. Any thread. */
    fun start(context: Context) {
        if (inbox != null) return
        val dir = runCatching { RyUINative.crashStart(info(context)) }.getOrNull() ?: return
        if (dir.isEmpty()) return
        val root = File(dir)
        val box = File(root, "inbox").apply { mkdirs() }
        inbox = box
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { t, e ->
            runCatching { record(t, e) }
            if (previous != null) previous.uncaughtException(t, e)
            else { Process.killProcess(Process.myPid()); System.exit(10) }
        }
        val app = context.applicationContext
        Thread({ runCatching { exits(app, root, box) } }, "ryui-crash").start()
    }

    /**
     * The device and the app, as JSON for ryui_crash_start, and whether
     * the app starts in the foreground (an Activity's launch: the
     * lifecycle's first event comes before the core starts).
     */
    private fun info(context: Context): String {
        val state = ActivityManager.RunningAppProcessInfo()
        ActivityManager.getMyMemoryState(state)
        val p = runCatching { context.packageManager.getPackageInfo(context.packageName, 0) }.getOrNull()
        val build = p?.longVersionCode?.toString() ?: ""
        return JSONObject(mapOf(
            "model" to Build.MODEL, "system" to "Android", "systemVersion" to Build.VERSION.RELEASE,
            "appVersion" to (p?.versionName ?: ""), "build" to build,
            "foreground" to (state.importance <= ActivityManager.RunningAppProcessInfo.IMPORTANCE_FOREGROUND),
        )).toString()
    }

    /** Writes an uncaught exception, on the thread that threw it, as the app ends. */
    private fun record(thread: Thread, e: Throwable) {
        val others = JSONArray()
        for ((t, stack) in Thread.getAllStackTraces()) {
            if (t === thread) continue
            others.put(JSONObject().put("name", t.name).put("stack", stack.joinToString("\n") { "\tat $it" }))
        }
        val o = JSONObject()
            .put("time", System.currentTimeMillis())
            .put("pid", Process.myPid())
            .put("thread", thread.name)
            .put("name", e.javaClass.name)
            .put("reason", e.message ?: "")
            .put("stack", e.stackTraceToString())
            .put("threads", others)
        write("exception-" + UUID.randomUUID() + ".json", o.toString().toByteArray())
    }

    /** Writes how the runs since the last launch ended, those that crashed. */
    private fun exits(context: Context, root: File, box: File) {
        val am = context.getSystemService(ActivityManager::class.java) ?: return
        val seen = File(root, "exits")
        val last = runCatching { seen.readText().trim().toLong() }.getOrDefault(0L)
        var newest = last
        var wrote = false
        for (x in am.getHistoricalProcessExitReasons(context.packageName, 0, 0)) {
            if (x.timestamp <= last) continue
            newest = maxOf(newest, x.timestamp)
            if (x.reason !in reported) continue
            val name = "exit-${x.timestamp}-${x.pid}"
            // The trace first: the record is what the Go code looks for.
            val trace = runCatching { x.traceInputStream?.use { it.readBytes() } }.getOrNull()
            if (trace != null && trace.isNotEmpty()) write("$name.trace", trace)
            val o = JSONObject()
                .put("time", x.timestamp)
                .put("pid", x.pid)
                .put("reason", x.reason)
                .put("status", x.status)
                .put("importance", x.importance)
                .put("description", x.description ?: "")
                .put("pss", x.pss)
                .put("rss", x.rss)
                .put("process", x.processName)
            write("$name.json", o.toString().toByteArray())
            wrote = true
        }
        if (newest > last) {
            val tmp = File(root, "exits.tmp")
            tmp.writeText(newest.toString())
            tmp.renameTo(seen)
        }
        if (wrote) runCatching { RyUINative.crashNotify() }
    }

    /** The exits reported: crashes (native, and the JVM's this handler missed), ANRs, kills for memory or resources. */
    private val reported = setOf(
        ApplicationExitInfo.REASON_CRASH_NATIVE, ApplicationExitInfo.REASON_CRASH, ApplicationExitInfo.REASON_ANR,
        ApplicationExitInfo.REASON_LOW_MEMORY, ApplicationExitInfo.REASON_EXCESSIVE_RESOURCE_USAGE,
        ApplicationExitInfo.REASON_INITIALIZATION_FAILURE,
    )

    /** Writes b as a file of the inbox, whole or not at all. */
    private fun write(name: String, b: ByteArray) {
        val dir = inbox ?: return
        val tmp = File(dir, "$name.tmp")
        tmp.writeBytes(b)
        tmp.renameTo(File(dir, name))
    }
}
