package dev.ryui

import android.Manifest
import android.content.ContentUris
import android.content.pm.PackageManager
import android.database.Cursor
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.provider.MediaStore
import android.util.Size
import androidx.core.content.ContextCompat
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.UUID

/**
 * ryui/photos' module, on MediaStore: "current" and "request" answer what's
 * allowed, "manage" asks again for which photos, "list" answers JSON (id,
 * width, height, created in ms, screenshot), "thumbnail" a JPEG's path and
 * "export" JSON (path, name). Off the main thread where they wait.
 */
class RyUIPhotoLibrary(private val rt: RyUIRuntime) {
    private val all = when {
        Build.VERSION.SDK_INT >= 34 -> arrayOf(Manifest.permission.READ_MEDIA_IMAGES, Manifest.permission.READ_MEDIA_VISUAL_USER_SELECTED)
        Build.VERSION.SDK_INT >= 33 -> arrayOf(Manifest.permission.READ_MEDIA_IMAGES)
        else -> arrayOf(Manifest.permission.READ_EXTERNAL_STORAGE)
    }

    private fun granted(p: String) = ContextCompat.checkSelfPermission(rt.app, p) == PackageManager.PERMISSION_GRANTED

    private fun access(asked: Boolean = false): String = when {
        granted(all[0]) -> "allowed"
        all.size > 1 && granted(all[1]) -> "limited"
        asked -> "denied"
        else -> "notAsked"
    }

    private val collection: Uri get() = MediaStore.Images.Media.EXTERNAL_CONTENT_URI

    private fun off(reply: (Result<ByteArray>) -> Unit, f: () -> ByteArray) {
        Thread { reply(runCatching(f)) }.start()
    }

    fun methods(): Map<String, RyUIModuleMethod> = mapOf(
        "current" to { _, reply -> reply(Result.success(access().toByteArray())) },
        "request" to { _, reply ->
            rt.withPermission(all) { reply(Result.success(access(asked = true).toByteArray())) }
        },
        // Android 14 asks again which photos only while the permission isn't fully given.
        "manage" to { _, reply ->
            val act = rt.activity
            if (act == null) reply(Result.failure(Exception("no screen")))
            else {
                var launcher: androidx.activity.result.ActivityResultLauncher<Array<String>>? = null
                launcher = act.activityResultRegistry.register("ryui-photos-manage-${UUID.randomUUID()}",
                    androidx.activity.result.contract.ActivityResultContracts.RequestMultiplePermissions()) {
                    launcher?.unregister()
                    reply(Result.success(ByteArray(0)))
                }
                launcher.launch(all)
            }
        },
        "list" to { args, reply ->
            val o = JSONObject(String(args, Charsets.UTF_8))
            off(reply) { list(o.optBoolean("screenshots"), o.optInt("offset"), o.optInt("limit", 100)).toString().toByteArray() }
        },
        "thumbnail" to { args, reply ->
            val o = JSONObject(String(args, Charsets.UTF_8))
            off(reply) { thumbnail(o.getString("id"), o.optInt("size", 300)).toByteArray() }
        },
        "export" to { args, reply ->
            val o = JSONObject(String(args, Charsets.UTF_8))
            off(reply) { export(o.getString("id")).toString().toByteArray() }
        },
    )

    private fun pathColumn() = if (Build.VERSION.SDK_INT >= 29) MediaStore.Images.Media.RELATIVE_PATH else MediaStore.Images.Media.DATA

    private fun list(screenshots: Boolean, offset: Int, limit: Int): JSONArray {
        val projection = arrayOf(
            MediaStore.Images.Media._ID, MediaStore.Images.Media.WIDTH, MediaStore.Images.Media.HEIGHT,
            MediaStore.Images.Media.DATE_TAKEN, MediaStore.Images.Media.DATE_ADDED, pathColumn(),
        )
        val where = if (screenshots) "${pathColumn()} LIKE ?" else null
        val whereArgs = if (screenshots) arrayOf("%Screenshot%") else null
        val q: Bundle = Bundle().apply {
            putString(android.content.ContentResolver.QUERY_ARG_SQL_SELECTION, where)
            putStringArray(android.content.ContentResolver.QUERY_ARG_SQL_SELECTION_ARGS, whereArgs)
            putString(android.content.ContentResolver.QUERY_ARG_SQL_SORT_ORDER, "${MediaStore.Images.Media.DATE_ADDED} DESC")
            putInt(android.content.ContentResolver.QUERY_ARG_LIMIT, limit)
            putInt(android.content.ContentResolver.QUERY_ARG_OFFSET, offset)
        }
        val out = JSONArray()
        rt.app.contentResolver.query(collection, projection, q, null)?.use { c: Cursor ->
            while (c.moveToNext()) {
                val taken = c.getLong(3)
                out.put(JSONObject()
                    .put("id", c.getLong(0).toString()).put("width", c.getInt(1)).put("height", c.getInt(2))
                    .put("created", if (taken > 0) taken else c.getLong(4) * 1000)
                    .put("screenshot", (c.getString(5) ?: "").contains("Screenshot", ignoreCase = true)))
            }
        }
        return out
    }

    private fun uri(id: String) = ContentUris.withAppendedId(collection, id.toLongOrNull() ?: throw Exception("no such photo"))

    private fun thumbnail(id: String, size: Int): String {
        val dir = File(rt.app.cacheDir, "ryui-photo-thumbs").apply { mkdirs() }
        val f = File(dir, "$id-$size.jpg")
        if (f.isFile) return f.path
        val u = uri(id)
        val bmp: Bitmap = if (Build.VERSION.SDK_INT >= 29) {
            // It fits in the box it's given, and the short side is the one
            // asked for: a long screenshot's box is as long as it is.
            var long = size
            rt.app.contentResolver.query(u, arrayOf(MediaStore.Images.Media.WIDTH, MediaStore.Images.Media.HEIGHT), null, null, null)?.use { c ->
                if (c.moveToFirst() && minOf(c.getInt(0), c.getInt(1)) > 0) {
                    long = minOf(maxOf(c.getInt(0), c.getInt(1)) * size / minOf(c.getInt(0), c.getInt(1)), size * 8)
                }
            }
            rt.app.contentResolver.loadThumbnail(u, Size(long, long), null)
        } else {
            val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
            rt.app.contentResolver.openInputStream(u)?.use { BitmapFactory.decodeStream(it, null, bounds) }
            var sample = 1
            while (minOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= size) sample *= 2
            rt.app.contentResolver.openInputStream(u)?.use {
                BitmapFactory.decodeStream(it, null, BitmapFactory.Options().apply { inSampleSize = sample })
            } ?: throw Exception("no thumbnail")
        }
        val tmp = File(dir, UUID.randomUUID().toString())
        tmp.outputStream().use { bmp.compress(Bitmap.CompressFormat.JPEG, 80, it) }
        bmp.recycle()
        tmp.renameTo(f)
        return f.path
    }

    private fun export(id: String): JSONObject {
        val u = uri(id)
        var name = ""
        rt.app.contentResolver.query(u, arrayOf(MediaStore.Images.Media.DISPLAY_NAME), null, null, null)?.use { c ->
            if (c.moveToFirst()) name = c.getString(0) ?: ""
        }
        val ext = name.substringAfterLast('.', "jpg")
        val dir = File(rt.app.cacheDir, "ryui-photo-exports").apply { mkdirs() }
        val f = File(dir, "${UUID.randomUUID()}.$ext")
        val input = rt.app.contentResolver.openInputStream(u) ?: throw Exception("no such photo")
        input.use { i -> f.outputStream().use { o -> i.copyTo(o) } }
        return JSONObject().put("path", f.path).put("name", name)
    }
}
