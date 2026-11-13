package dev.asuka

import android.net.Uri
import android.provider.OpenableColumns
import androidx.activity.result.contract.ActivityResultContracts
import org.json.JSONArray
import org.json.JSONObject
import java.io.File

private var filePickers = 0

/**
 * device.PickFiles' module: "pick" opens the system's document picker
 * (ACTION_OPEN_DOCUMENT: no permission). args: u8 multiple, then MIME
 * types a line each (none: any file); the reply is a JSON array of copies
 * in the app's cache: path, name, type, size (none: cancelled).
 */
fun AsukaRuntime.filesMethods(): Map<String, AsukaModuleMethod> = mapOf(
    "pick" to { args, reply ->
        val act = activity
        if (act == null) reply(Result.failure(Exception("no screen")))
        else {
            val multiple = args.firstOrNull()?.toInt() == 1
            val types = String(args.copyOfRange(minOf(1, args.size), args.size), Charsets.UTF_8)
                .split('\n').filter { it.isNotEmpty() }.ifEmpty { listOf("*/*") }.toTypedArray()
            val key = "asuka-files-${filePickers++}"
            val done = { uris: List<Uri> -> Thread { reply(Result.success(copyPicked(uris))) }.start() }
            if (multiple) {
                var l: androidx.activity.result.ActivityResultLauncher<Array<String>>? = null
                l = act.activityResultRegistry.register(key, ActivityResultContracts.OpenMultipleDocuments()) { uris ->
                    l?.unregister()
                    done(uris)
                }
                l.launch(types)
            } else {
                var l: androidx.activity.result.ActivityResultLauncher<Array<String>>? = null
                l = act.activityResultRegistry.register(key, ActivityResultContracts.OpenDocument()) { uri ->
                    l?.unregister()
                    done(listOfNotNull(uri))
                }
                l.launch(types)
            }
        }
    },
)

/** Copies the picked documents to the cache, each in a directory of its own (its name stays its own); their JSON. */
private fun AsukaRuntime.copyPicked(uris: List<Uri>): ByteArray {
    if (uris.isEmpty()) return ByteArray(0)
    val out = JSONArray()
    val cr = app.contentResolver
    for (u in uris) {
        runCatching {
            var name = "file"
            cr.query(u, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use { c ->
                if (c.moveToFirst() && !c.isNull(0)) name = c.getString(0)
            }
            name = name.replace('/', '_')
            val dir = File(app.cacheDir, "asuka-files/" + java.util.UUID.randomUUID()).apply { mkdirs() }
            val f = File(dir, name)
            cr.openInputStream(u)!!.use { i -> f.outputStream().use { o -> i.copyTo(o) } }
            out.put(JSONObject().put("path", f.absolutePath).put("name", name).put("type", cr.getType(u) ?: "").put("size", f.length()))
        }
    }
    return out.toString().toByteArray()
}
