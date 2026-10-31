package dev.ryui

import android.content.Context

/**
 * Reads codes for ryui/scan. It's ML Kit's, in the ryui-scan module, which
 * only the apps that import ryui/scan are built with (the CLI's
 * featurePermissions); found by name.
 */
interface RyUICodeReader {
    /** An analyzer of the camera view's frames: found is called on the main thread with each code's kind and value. */
    fun analyzer(kinds: Set<String>, found: (String, String) -> Unit): RyUICodeAnalyzer

    /** The codes (kind, value) in the image file at path, given on the main thread. */
    fun file(context: Context, path: String, kinds: Set<String>, done: (Result<List<Pair<String, String>>>) -> Unit)
}

/** An analyzer of frames that holds a reader, until it's closed. */
interface RyUICodeAnalyzer : androidx.camera.core.ImageAnalysis.Analyzer, AutoCloseable

object RyUIScan {
    val reader: RyUICodeReader? by lazy {
        try {
            Class.forName("dev.ryui.scan.RyUIMLKitReader").getDeclaredConstructor().newInstance() as RyUICodeReader
        } catch (_: ReflectiveOperationException) {
            null
        }
    }

    fun kinds(s: String): Set<String> = s.split(',').filter { it.isNotEmpty() }.toSet()
}

/** Repeats of a code in view, dropped: a code is reported as it comes into view, and again once it's been out of it for a second. */
class RyUICodeSeen {
    private val last = HashMap<String, Long>()

    fun isNew(kind: String, value: String, now: Long = android.os.SystemClock.uptimeMillis()): Boolean {
        val key = kind + "\u0000" + value
        val new = last[key]?.let { now - it > 1000 } ?: true
        last[key] = now
        if (last.size > 32) last.entries.removeAll { now - it.value > 1000 }
        return new
    }
}

/**
 * ryui/scan's module: "file" reads the codes in an image file. args: the
 * kinds, comma-separated, a newline, the path. The reply: a u32 count,
 * then each code's kind and value as strings.
 */
fun RyUIRuntime.scanMethods(): Map<String, RyUIModuleMethod> = mapOf(
    "file" to { args, reply ->
        val s = String(args, Charsets.UTF_8)
        val nl = s.indexOf('\n')
        val reader = RyUIScan.reader
        if (nl < 0) reply(Result.failure(Exception("bad arguments")))
        else if (reader == null) reply(Result.failure(Exception("ryui/scan isn't in this build")))
        else reader.file(app, s.substring(nl + 1), RyUIScan.kinds(s.substring(0, nl))) { r ->
            reply(r.map { codes ->
                val w = RyUIWriter()
                w.u32(codes.size)
                for ((k, v) in codes) { w.string(k); w.string(v) }
                w.bytes()
            })
        }
    },
)
