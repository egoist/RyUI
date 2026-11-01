package dev.ryui

import android.content.Context
import org.json.JSONObject

/**
 * Reads images for ryui/vision. It's ML Kit's, in the ryui-vision module,
 * which only the apps that import ryui/vision are built with (the CLI's
 * featurePermissions); found by name.
 */
interface RyUIVisionReader {
    /** What's in the image at path, as ryui/vision's Result. Blocking: off the main thread. */
    fun analyze(context: Context, path: String, text: Boolean, faces: Boolean, codes: Boolean): JSONObject
}

private val visionReader: RyUIVisionReader? by lazy {
    try {
        Class.forName("dev.ryui.vision.RyUIMLKitVision").getDeclaredConstructor().newInstance() as RyUIVisionReader
    } catch (_: ReflectiveOperationException) {
        null
    }
}

/** ryui/vision's module: "analyze" takes JSON (path, text, faces, codes) and answers its Result as JSON. */
fun RyUIRuntime.visionMethods(): Map<String, RyUIModuleMethod> = mapOf(
    "analyze" to { args, reply ->
        val reader = visionReader
        val o = runCatching { JSONObject(String(args, Charsets.UTF_8)) }.getOrNull()
        if (reader == null) reply(Result.failure(Exception("ryui/vision isn't in this build")))
        else if (o == null) reply(Result.failure(Exception("bad args")))
        else Thread {
            reply(runCatching {
                val path = o.getString("path").removePrefix("file://")
                reader.analyze(app, path, o.optBoolean("text", true), o.optBoolean("faces", true), o.optBoolean("codes", true))
                    .toString().toByteArray()
            }.recoverCatching { e ->
                throw if (e is OutOfMemoryError) Exception("the image is too large to read") else e
            })
        }.start()
    },
)
