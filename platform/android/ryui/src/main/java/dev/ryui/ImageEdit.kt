package dev.ryui

import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Matrix
import android.graphics.Paint
import android.media.MediaMetadataRetriever
import androidx.exifinterface.media.ExifInterface
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.UUID
import kotlin.math.abs
import kotlin.math.ceil
import kotlin.math.cos
import kotlin.math.roundToInt
import kotlin.math.sin

/**
 * ryui/imageedit's module: "edit", "size" and "videoFrame", each with
 * JSON (source, actions, format, quality, at in ms), answer JSON (path,
 * width, height) or "no image". BitmapFactory decodes (JPEG, PNG, WebP;
 * HEIC from Android 9) a power of two smaller where the first resize
 * allows, then turns it upright as its EXIF says; Matrix does the
 * actions; Bitmap.compress writes JPEG or PNG. Off the main thread.
 */
class RyUIImageEdit(private val rt: RyUIRuntime) {
    private class Request(o: JSONObject) {
        val source: String = o.getString("source")
        val actions: List<JSONObject> = o.optJSONArray("actions")?.let { a -> List(a.length()) { a.getJSONObject(it) } } ?: emptyList()
        val png = o.optString("format") == "png"
        val quality = o.optDouble("quality", 0.9)
        val atMillis = o.optLong("at", 0)
    }

    private class NoImage : Exception("no image")

    fun methods(): Map<String, RyUIModuleMethod> {
        fun run(args: ByteArray, reply: (Result<ByteArray>) -> Unit, f: (Request) -> JSONObject) {
            val r = runCatching { Request(JSONObject(String(args, Charsets.UTF_8))) }.getOrNull()
                ?: return reply(Result.failure(Exception("bad args")))
            Thread {
                reply(runCatching { f(r).toString().toByteArray() }.recoverCatching { e ->
                    throw if (e is OutOfMemoryError) Exception("the image is too large to edit") else e
                })
            }.start()
        }
        return mapOf(
            "size" to { args, reply ->
                run(args, reply) { r ->
                    val (f, temp) = local(r.source)
                    try {
                        val (w, h) = bounds(f)
                        val o = orientation(f)
                        if (quarterTurned(o)) JSONObject().put("path", "").put("width", h).put("height", w)
                        else JSONObject().put("path", "").put("width", w).put("height", h)
                    } finally { if (temp) f.delete() }
                }
            },
            "edit" to { args, reply ->
                run(args, reply) { r ->
                    val (f, temp) = local(r.source)
                    try { write(apply(r.actions, decode(f, r.actions.firstOrNull())), r) } finally { if (temp) f.delete() }
                }
            },
            "videoFrame" to { args, reply ->
                run(args, reply) { r -> write(apply(r.actions, frame(r.source, r.atMillis)), r) }
            },
        )
    }

    // Sources: as ui.Image's. The file, and whether it's a temporary copy.

    private fun local(source: String): Pair<File, Boolean> = when {
        source.startsWith("http://") || source.startsWith("https://") -> download(source) to true
        source.startsWith("file://") -> File(android.net.Uri.parse(source).path ?: throw NoImage()) to false
        source.startsWith("/") -> File(source) to false
        // An asset: pushed by ryui dev, else in the APK.
        File(rt.app.filesDir, "ryui/assets/$source").exists() -> File(rt.app.filesDir, "ryui/assets/$source") to false
        else -> {
            val f = temp(source.substringAfterLast('.', "img"))
            try {
                rt.app.assets.open(source).use { i -> f.outputStream().use { o -> i.copyTo(o) } }
            } catch (_: java.io.IOException) {
                f.delete(); throw NoImage()
            }
            f to true
        }
    }.also { (f, _) -> if (!f.isFile) throw NoImage() }

    private fun temp(ext: String) = File(File(rt.app.cacheDir, "ryui-images-in").apply { mkdirs() }, UUID.randomUUID().toString() + "." + ext)

    private fun download(url: String): File {
        val c = URL(url).openConnection() as HttpURLConnection
        try {
            if (c.responseCode !in 200..299) throw Exception("HTTP ${c.responseCode}")
            val f = temp("img")
            c.inputStream.use { i -> f.outputStream().use { o -> i.copyTo(o) } }
            return f
        } finally {
            c.disconnect()
        }
    }

    private fun bounds(f: File): Pair<Int, Int> {
        val o = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeFile(f.path, o)
        if (o.outWidth <= 0 || o.outHeight <= 0) throw NoImage()
        return o.outWidth to o.outHeight
    }

    private fun orientation(f: File): Int =
        runCatching { ExifInterface(f.path).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL) }
            .getOrDefault(ExifInterface.ORIENTATION_NORMAL)

    private fun quarterTurned(o: Int) = o == ExifInterface.ORIENTATION_TRANSPOSE || o == ExifInterface.ORIENTATION_ROTATE_90 ||
        o == ExifInterface.ORIENTATION_TRANSVERSE || o == ExifInterface.ORIENTATION_ROTATE_270

    /** Decodes upright, a power of two smaller where a first resize allows (memory). */
    private fun decode(f: File, first: JSONObject?): Bitmap {
        val (w, h) = bounds(f)
        val o = orientation(f)
        val (uw, uh) = if (quarterTurned(o)) h to w else w to h
        var sample = 1
        if (first?.optString("op") == "resize") {
            val (tw, th) = target(first, uw, uh)
            while (uw / (sample * 2) >= tw && uh / (sample * 2) >= th) sample *= 2
        }
        val bmp = BitmapFactory.decodeFile(f.path, BitmapFactory.Options().apply { inSampleSize = sample; inPreferredConfig = Bitmap.Config.ARGB_8888 })
            ?: throw NoImage()
        val m = Matrix()
        when (o) {
            ExifInterface.ORIENTATION_FLIP_HORIZONTAL -> m.setScale(-1f, 1f)
            ExifInterface.ORIENTATION_ROTATE_180 -> m.setRotate(180f)
            ExifInterface.ORIENTATION_FLIP_VERTICAL -> m.setScale(1f, -1f)
            ExifInterface.ORIENTATION_TRANSPOSE -> { m.setRotate(90f); m.postScale(-1f, 1f) }
            ExifInterface.ORIENTATION_ROTATE_90 -> m.setRotate(90f)
            ExifInterface.ORIENTATION_TRANSVERSE -> { m.setRotate(-90f); m.postScale(-1f, 1f) }
            ExifInterface.ORIENTATION_ROTATE_270 -> m.setRotate(-90f)
            else -> return bmp
        }
        return Bitmap.createBitmap(bmp, 0, 0, bmp.width, bmp.height, m, true).also { if (it !== bmp) bmp.recycle() }
    }

    /** A resize's size: a 0 keeps the aspect ratio from the other. */
    private fun target(a: JSONObject, w: Int, h: Int): Pair<Int, Int> {
        var tw = a.optInt("width"); var th = a.optInt("height")
        if (tw <= 0 && th <= 0) return w to h
        if (tw <= 0) tw = maxOf(1, (w.toDouble() * th / h).roundToInt())
        if (th <= 0) th = maxOf(1, (h.toDouble() * tw / w).roundToInt())
        return tw to th
    }

    private fun apply(actions: List<JSONObject>, image: Bitmap): Bitmap {
        var img = image
        fun replace(next: Bitmap) { if (next !== img) img.recycle(); img = next }
        for (a in actions) {
            val w = img.width; val h = img.height
            when (a.optString("op")) {
                "resize" -> {
                    val (tw, th) = target(a, w, h)
                    if (tw != w || th != h) replace(Bitmap.createScaledBitmap(img, tw, th, true))
                }
                "crop" -> {
                    val x = a.optInt("x").coerceIn(0, w); val y = a.optInt("y").coerceIn(0, h)
                    val cw = minOf(a.optInt("width"), w - x); val ch = minOf(a.optInt("height"), h - y)
                    if (cw < 1 || ch < 1) throw Exception("the crop is outside the ${w}×$h image")
                    replace(Bitmap.createBitmap(img, x, y, cw, ch))
                }
                "rotate" -> {
                    val deg = a.optDouble("degrees") % 360
                    if (deg == 0.0) continue
                    val t = Math.toRadians(deg)
                    val quarter = deg % 90 == 0.0
                    val nw = w * abs(cos(t)) + h * abs(sin(t)); val nh = w * abs(sin(t)) + h * abs(cos(t))
                    val ow = if (quarter) nw.roundToInt() else ceil(nw).toInt()
                    val oh = if (quarter) nh.roundToInt() else ceil(nh).toInt()
                    val out = Bitmap.createBitmap(ow, oh, Bitmap.Config.ARGB_8888)
                    val m = Matrix().apply {
                        postTranslate(-w / 2f, -h / 2f)
                        postRotate(deg.toFloat()) // clockwise: the canvas's y is down
                        postTranslate(ow / 2f, oh / 2f)
                    }
                    Canvas(out).drawBitmap(img, m, Paint(Paint.FILTER_BITMAP_FLAG or Paint.ANTI_ALIAS_FLAG))
                    replace(out)
                }
                "fill", "pixelate" -> {
                    if (!img.isMutable) replace(img.copy(Bitmap.Config.ARGB_8888, true))
                    val x = a.optInt("x").coerceIn(0, w); val y = a.optInt("y").coerceIn(0, h)
                    val x1 = (a.optInt("x") + a.optInt("width")).coerceIn(0, w); val y1 = (a.optInt("y") + a.optInt("height")).coerceIn(0, h)
                    if (x1 <= x || y1 <= y) continue
                    if (a.optString("op") == "fill") {
                        val paint = Paint().apply { color = Color.BLACK or a.optInt("rgb"); isAntiAlias = false }
                        Canvas(img).drawRect(x.toFloat(), y.toFloat(), x1.toFloat(), y1.toFloat(), paint)
                    } else {
                        pixelate(img, x, y, x1, y1, maxOf(a.optInt("block"), 1))
                    }
                }
                "flipH", "flipV" -> {
                    val m = Matrix().apply { if (a.optString("op") == "flipH") setScale(-1f, 1f) else setScale(1f, -1f) }
                    replace(Bitmap.createBitmap(img, 0, 0, w, h, m, true))
                }
                else -> throw Exception("unknown action ${a.optString("op")}")
            }
        }
        return img
    }

    /**
     * Squares of block pixels, aligned to the image, each the average of
     * all it covers; only those within the rectangle change.
     */
    private fun pixelate(img: Bitmap, x0: Int, y0: Int, x1: Int, y1: Int, block: Int) {
        val w = img.width; val h = img.height
        var by = y0 / block * block
        while (by < y1) {
            val by1 = minOf(by + block, h)
            var bx = x0 / block * block
            while (bx < x1) {
                val bx1 = minOf(bx + block, w)
                val bw = bx1 - bx; val bh = by1 - by
                val px = IntArray(bw * bh)
                img.getPixels(px, 0, bw, bx, by, bw, bh)
                var a = 0L; var r = 0L; var g = 0L; var b = 0L
                for (p in px) { a += p ushr 24; r += p shr 16 and 255; g += p shr 8 and 255; b += p and 255 }
                val n = px.size
                val v = Color.argb((a / n).toInt(), (r / n).toInt(), (g / n).toInt(), (b / n).toInt())
                val cx0 = maxOf(bx, x0); val cy0 = maxOf(by, y0)
                val cw = minOf(bx1, x1) - cx0; val ch = minOf(by1, y1) - cy0
                img.setPixels(IntArray(cw * ch) { v }, 0, cw, cx0, cy0, cw, ch)
                bx += block
            }
            by += block
        }
    }

    /** Writes to the caches: PNG, or JPEG on white (it has no transparency). */
    private fun write(image: Bitmap, r: Request): JSONObject {
        var img = image
        if (!r.png && image.hasAlpha()) {
            img = Bitmap.createBitmap(image.width, image.height, Bitmap.Config.ARGB_8888)
            Canvas(img).apply { drawColor(Color.WHITE); drawBitmap(image, 0f, 0f, null) }
            image.recycle()
        }
        val dir = File(rt.app.cacheDir, "ryui-images").apply { mkdirs() }
        val f = File(dir, UUID.randomUUID().toString() + if (r.png) ".png" else ".jpg")
        f.outputStream().use { o ->
            if (!img.compress(if (r.png) Bitmap.CompressFormat.PNG else Bitmap.CompressFormat.JPEG, (r.quality * 100).roundToInt().coerceIn(0, 100), o)) {
                throw Exception("can't write ${f.path}")
            }
        }
        val out = JSONObject().put("path", f.absolutePath).put("width", img.width).put("height", img.height)
        img.recycle()
        return out
    }

    /** The video's frame at the time (exactly), upright (the retriever turns it as the video's rotation says). */
    private fun frame(source: String, atMillis: Long): Bitmap {
        val m = MediaMetadataRetriever()
        try {
            try {
                when {
                    source.startsWith("http://") || source.startsWith("https://") -> m.setDataSource(source, HashMap())
                    source.startsWith("file://") -> m.setDataSource(android.net.Uri.parse(source).path)
                    source.startsWith("/") -> m.setDataSource(source)
                    File(rt.app.filesDir, "ryui/assets/$source").exists() -> m.setDataSource(File(rt.app.filesDir, "ryui/assets/$source").path)
                    else -> rt.app.assets.openFd(source).use { fd -> m.setDataSource(fd.fileDescriptor, fd.startOffset, fd.length) }
                }
            } catch (_: Exception) {
                throw NoImage()
            }
            return m.getFrameAtTime(maxOf(atMillis, 0) * 1000, MediaMetadataRetriever.OPTION_CLOSEST)?.let {
                // A hardware bitmap can't be drawn on: a copy that can.
                if (it.config == Bitmap.Config.ARGB_8888) it else it.copy(Bitmap.Config.ARGB_8888, false).also { _ -> it.recycle() }
            } ?: throw NoImage()
        } finally {
            m.release()
        }
    }
}
