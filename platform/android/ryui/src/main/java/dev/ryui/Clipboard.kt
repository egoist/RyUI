package dev.ryui

import android.content.ClipData
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.os.Build
import android.view.textclassifier.TextClassifier
import android.webkit.MimeTypeMap
import androidx.core.content.FileProvider
import java.io.File
import java.util.UUID

/**
 * ryui/device's clipboard module: "has" (u8 text, image, URL, from the
 * clip's description: no "pasted" notice), "image" (the clip's image as a
 * file in the caches, in its own format; empty: none), "copyImage" (a
 * file's path, through the app's FileProvider), and, while watching,
 * "next" (answered at the next change) and "stop".
 */
class RyUIClipboard(private val rt: RyUIRuntime) {
    private val cm get() = rt.app.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
    private val waiting = ArrayList<(Result<ByteArray>) -> Unit>()
    private var listening = false
    private val changed = ClipboardManager.OnPrimaryClipChangedListener {
        val ws = ArrayList(waiting)
        waiting.clear()
        for (w in ws) w(Result.success(ByteArray(0)))
    }

    fun methods(): Map<String, RyUIModuleMethod> = mapOf(
        "has" to { _, reply ->
            val d = cm.primaryClipDescription
            val text = d != null && (d.hasMimeType(ClipDescription.MIMETYPE_TEXT_PLAIN) || d.hasMimeType(ClipDescription.MIMETYPE_TEXT_HTML))
            val image = d != null && d.hasMimeType("image/*")
            // A link: a URI list, or text the system classified as a URL (Android 12).
            val url = d != null && (d.hasMimeType(ClipDescription.MIMETYPE_TEXT_URILIST) ||
                Build.VERSION.SDK_INT >= 31 && d.classificationStatus == ClipDescription.CLASSIFICATION_COMPLETE &&
                d.getConfidenceScore(TextClassifier.TYPE_URL) >= 0.5f)
            reply(Result.success(byteArrayOf(if (text) 1 else 0, if (image) 1 else 0, if (url) 1 else 0)))
        },
        "image" to { _, reply ->
            val clip = cm.primaryClip
            val item = clip?.takeIf { it.description.hasMimeType("image/*") }?.let { c -> (0 until c.itemCount).map { c.getItemAt(it) }.firstOrNull { it.uri != null } }
            if (item == null) reply(Result.success(ByteArray(0)))
            else Thread {
                reply(runCatching {
                    val cr = rt.app.contentResolver
                    val type = cr.getType(item.uri) ?: clip.description.getMimeType(0)
                    val ext = MimeTypeMap.getSingleton().getExtensionFromMimeType(type) ?: "png"
                    val f = File(File(rt.app.cacheDir, "ryui-clipboard").apply { mkdirs() }, UUID.randomUUID().toString() + "." + ext)
                    cr.openInputStream(item.uri)!!.use { i -> f.outputStream().use { o -> i.copyTo(o) } }
                    f.absolutePath.toByteArray()
                })
            }.start()
        },
        "copyImage" to { args, reply ->
            val f = File(String(args, Charsets.UTF_8))
            if (!f.isFile) reply(Result.failure(Exception("no file at ${f.path}")))
            else try {
                val authority = rt.app.packageName + ".ryui.files"
                // A file outside the app's folders goes to its caches first.
                val shared = runCatching { FileProvider.getUriForFile(rt.app, authority, f) }.getOrNull()
                    ?: File(File(rt.app.cacheDir, "ryui-clipboard/" + UUID.randomUUID()).apply { mkdirs() }, f.name).let { c ->
                        f.copyTo(c, overwrite = true)
                        FileProvider.getUriForFile(rt.app, authority, c)
                    }
                cm.setPrimaryClip(ClipData.newUri(rt.app.contentResolver, "image", shared))
                reply(Result.success(ByteArray(0)))
            } catch (e: Exception) {
                reply(Result.failure(e))
            }
        },
        "next" to { _, reply ->
            if (!listening) { listening = true; cm.addPrimaryClipChangedListener(changed) }
            waiting.add(reply)
        },
        "stop" to { _, reply ->
            val ws = ArrayList(waiting)
            waiting.clear()
            if (listening) { listening = false; cm.removePrimaryClipChangedListener(changed) }
            for (w in ws) w(Result.failure(Exception("stopped")))
            reply(Result.success(ByteArray(0)))
        },
    )
}
