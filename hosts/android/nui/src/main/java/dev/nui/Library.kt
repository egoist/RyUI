package dev.nui

import android.content.ContentValues
import android.media.MediaScannerConnection
import android.os.Build
import android.os.Environment
import android.provider.MediaStore
import org.json.JSONObject
import java.io.File
import java.util.concurrent.Executors

private val copier = Executors.newSingleThreadExecutor()

/**
 * nui/library's module: "save" (args: JSON path, collection 0 downloads,
 * 1 movies, 2 music, 3 pictures, name, folder, mime, move) copies a file
 * into a MediaStore collection, off the main thread, and answers with its
 * content:// URI. Before Android 10, into the shared folder itself, after
 * the storage permission, then the media scanner.
 */
fun NuiRuntime.libraryMethods(): Map<String, NuiModuleMethod> = mapOf(
    "save" to { args, reply ->
        val o = JSONObject(String(args, Charsets.UTF_8))
        val src = File(o.getString("path"))
        val collection = o.optInt("collection")
        val name = o.optString("name").ifEmpty { src.name }
        val folder = o.optString("folder")
        val mime = o.optString("mime").ifEmpty { "application/octet-stream" }
        val move = o.optBoolean("move")
        val dir = when (collection) {
            1 -> Environment.DIRECTORY_MOVIES
            2 -> Environment.DIRECTORY_MUSIC
            3 -> Environment.DIRECTORY_PICTURES
            else -> Environment.DIRECTORY_DOWNLOADS
        }
        fun done(r: Result<String>) = onMain { reply(r.map { it.toByteArray() }) }
        if (!src.isFile) reply(Result.failure(Exception("no file at ${src.path}")))
        else if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) copier.execute {
            done(runCatching { saveToMediaStore(src, collection, dir, folder, name, mime) }.onSuccess { if (move) src.delete() })
        }
        else withPermission(arrayOf(android.Manifest.permission.WRITE_EXTERNAL_STORAGE)) { ok ->
            if (!ok) reply(Result.failure(Exception("not allowed")))
            else copier.execute {
                done(runCatching {
                    @Suppress("DEPRECATION")
                    val base = File(Environment.getExternalStoragePublicDirectory(dir), folder)
                    base.mkdirs()
                    var dst = File(base, name)
                    var i = 1
                    while (dst.exists()) dst = File(base, "${name.substringBeforeLast('.')} (${i++})" + name.substringAfterLast('.', "").let { if (it.isEmpty()) "" else ".$it" })
                    src.copyTo(dst)
                    if (move) src.delete()
                    MediaScannerConnection.scanFile(app, arrayOf(dst.path), arrayOf(mime), null)
                    android.net.Uri.fromFile(dst).toString()
                })
            }
        }
    },
)

/** Copies src into a MediaStore collection (Android 10 on), pending until it's whole; its URI. */
@androidx.annotation.RequiresApi(Build.VERSION_CODES.Q)
private fun NuiRuntime.saveToMediaStore(src: File, collection: Int, dir: String, folder: String, name: String, mime: String): String {
    val volume = MediaStore.VOLUME_EXTERNAL_PRIMARY
    val uri = when (collection) {
        1 -> MediaStore.Video.Media.getContentUri(volume)
        2 -> MediaStore.Audio.Media.getContentUri(volume)
        3 -> MediaStore.Images.Media.getContentUri(volume)
        else -> MediaStore.Downloads.getContentUri(volume)
    }
    val values = ContentValues().apply {
        put(MediaStore.MediaColumns.DISPLAY_NAME, name)
        put(MediaStore.MediaColumns.MIME_TYPE, mime)
        put(MediaStore.MediaColumns.RELATIVE_PATH, if (folder.isEmpty()) dir else "$dir/$folder")
        put(MediaStore.MediaColumns.IS_PENDING, 1)
    }
    val resolver = app.contentResolver
    val item = resolver.insert(uri, values) ?: error("couldn't add to the library")
    try {
        resolver.openOutputStream(item)?.use { out -> src.inputStream().use { it.copyTo(out, 1 shl 16) } } ?: error("couldn't write to the library")
        resolver.update(item, ContentValues().apply { put(MediaStore.MediaColumns.IS_PENDING, 0) }, null, null)
    } catch (e: Exception) {
        resolver.delete(item, null, null)
        throw e
    }
    return item.toString()
}
