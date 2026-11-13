package dev.asuka

import android.content.Context

/**
 * Reads codes for asuka/scan. It's ML Kit's, in the asuka-scan module, which
 * only the apps that import asuka/scan are built with (the CLI's
 * featurePermissions); found by name.
 */
interface AsukaCodeReader {
    /** An analyzer of the camera view's frames: found is called on the main thread with each code's kind and value. */
    fun analyzer(kinds: Set<String>, found: (String, String) -> Unit): AsukaCodeAnalyzer

    /** The codes (kind, value) in the image file at path, given on the main thread. */
    fun file(context: Context, path: String, kinds: Set<String>, done: (Result<List<Pair<String, String>>>) -> Unit)
}

/** An analyzer of frames that holds a reader, until it's closed. */
interface AsukaCodeAnalyzer : androidx.camera.core.ImageAnalysis.Analyzer, AutoCloseable

object AsukaScan {
    val reader: AsukaCodeReader? by lazy {
        try {
            Class.forName("dev.asuka.scan.AsukaMLKitReader").getDeclaredConstructor().newInstance() as AsukaCodeReader
        } catch (_: ReflectiveOperationException) {
            null
        }
    }

    fun kinds(s: String): Set<String> = s.split(',').filter { it.isNotEmpty() }.toSet()
}

/** Repeats of a code in view, dropped: a code is reported as it comes into view, and again once it's been out of it for a second. */
class AsukaCodeSeen {
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
 * asuka/scan's module: "file" reads the codes in an image file. args: the
 * kinds, comma-separated, a newline, the path. The reply: a u32 count,
 * then each code's kind and value as strings.
 */
fun AsukaRuntime.scanMethods(): Map<String, AsukaModuleMethod> = mapOf(
    "file" to { args, reply ->
        val s = String(args, Charsets.UTF_8)
        val nl = s.indexOf('\n')
        val reader = AsukaScan.reader
        if (nl < 0) reply(Result.failure(Exception("bad arguments")))
        else if (reader == null) reply(Result.failure(Exception("asuka/scan isn't in this build")))
        else reader.file(app, s.substring(nl + 1), AsukaScan.kinds(s.substring(0, nl))) { r ->
            reply(r.map { codes ->
                val w = AsukaWriter()
                w.u32(codes.size)
                for ((k, v) in codes) { w.string(k); w.string(v) }
                w.bytes()
            })
        }
    },
)
