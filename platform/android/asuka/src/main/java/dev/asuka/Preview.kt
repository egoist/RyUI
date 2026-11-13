package dev.asuka

import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.Intent
import android.net.Uri
import androidx.activity.result.ActivityResultLauncher
import androidx.activity.result.contract.ActivityResultContracts
import org.json.JSONObject

private var launches = 0

/**
 * Starts intent and calls back once the user is back in the app; failed
 * if it couldn't start (an ActivityNotFoundException: no app takes it).
 */
internal fun AsukaRuntime.launchAndWait(intent: Intent, back: () -> Unit, failed: (Exception) -> Unit) {
    val act = activity ?: return failed(Exception("no screen"))
    var launcher: ActivityResultLauncher<Intent>? = null
    launcher = act.activityResultRegistry.register("asuka-launch-${launches++}", ActivityResultContracts.StartActivityForResult()) {
        launcher?.unregister()
        back()
    }
    try {
        launcher.launch(intent)
    } catch (e: Exception) {
        launcher.unregister()
        failed(e)
    }
}

/**
 * asuka/sdk/preview's module: "show" (JSON: files, each a source and a MIME
 * type; start) opens the start file in the app the user picks for its
 * type (ACTION_VIEW), through the app's FileProvider, readable by that
 * app alone, and answers once the user is back; "no viewer" if no app
 * opens it. Android has no system viewer, nor one that pages through
 * several files: as Android's mail apps open an attachment.
 */
fun AsukaRuntime.previewMethods(): Map<String, AsukaModuleMethod> = mapOf(
    "show" to { args, reply ->
        val o = JSONObject(String(args, Charsets.UTF_8))
        val files = o.optJSONArray("files")
        val f = files?.optJSONObject(o.optInt("start").coerceIn(0, maxOf(files.length() - 1, 0)))
        if (f == null) reply(Result.failure(Exception("bad arguments")))
        else localFiles(listOf(AsukaSource(f.optString("source"), f.optString("type")))) { r ->
            r.onFailure { reply(Result.failure(it)) }.onSuccess { (file) ->
                val uri: Uri = providerURI(file.path)
                val view = Intent(Intent.ACTION_VIEW).apply {
                    setDataAndType(uri, fileMIME(file, f.optString("type")))
                    clipData = ClipData.newRawUri(file.name, uri)
                    addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                }
                launchAndWait(view, { reply(Result.success(ByteArray(0))) }) { e ->
                    reply(Result.failure(if (e is ActivityNotFoundException) Exception("no viewer") else e))
                }
            }
        }
    },
)
