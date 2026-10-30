package dev.nui

import android.content.Context

/**
 * Reads codes for nui/scan. It's ML Kit's, in the nui-scan module, which
 * only the apps that import nui/scan are built with (the CLI's
 * featurePermissions); found by name.
 */
interface NuiCodeReader {
    /** An analyzer of the camera view's frames: found is called on the main thread with each code's kind and value. */
    fun analyzer(kinds: Set<String>, found: (String, String) -> Unit): NuiCodeAnalyzer

    /** The codes (kind, value) in the image file at path, given on the main thread. */
    fun file(context: Context, path: String, kinds: Set<String>, done: (Result<List<Pair<String, String>>>) -> Unit)
}

/** An analyzer of frames that holds a reader, until it's closed. */
interface NuiCodeAnalyzer : androidx.camera.core.ImageAnalysis.Analyzer, AutoCloseable

object NuiScan {
    val reader: NuiCodeReader? by lazy {
        try {
            Class.forName("dev.nui.scan.NuiMLKitReader").getDeclaredConstructor().newInstance() as NuiCodeReader
        } catch (_: ReflectiveOperationException) {
            null
        }
    }

    fun kinds(s: String): Set<String> = s.split(',').filter { it.isNotEmpty() }.toSet()
}

/** Repeats of a code in view, dropped: a code is reported as it comes into view, and again once it's been out of it for a second. */
class NuiCodeSeen {
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
 * nui/scan's module: "file" reads the codes in an image file. args: the
 * kinds, comma-separated, a newline, the path. The reply: a u32 count,
 * then each code's kind and value as strings.
 */
fun NuiRuntime.scanMethods(): Map<String, NuiModuleMethod> = mapOf(
    "file" to { args, reply ->
        val s = String(args, Charsets.UTF_8)
        val nl = s.indexOf('\n')
        val reader = NuiScan.reader
        if (nl < 0) reply(Result.failure(Exception("bad arguments")))
        else if (reader == null) reply(Result.failure(Exception("nui/scan isn't in this build")))
        else reader.file(app, s.substring(nl + 1), NuiScan.kinds(s.substring(0, nl))) { r ->
            reply(r.map { codes ->
                val w = NuiWriter()
                w.u32(codes.size)
                for ((k, v) in codes) { w.string(k); w.string(v) }
                w.bytes()
            })
        }
    },
)
