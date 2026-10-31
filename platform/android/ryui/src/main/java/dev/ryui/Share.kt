package dev.ryui

import android.content.ClipData
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.OpenableColumns
import android.webkit.MimeTypeMap
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.FileProvider
import java.io.File
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.UUID
import java.util.concurrent.Executors

/*
 * Sharing (expo-sharing's): the "share" module's "files", the share sheet
 * for files (ui.ShareFiles); and what other apps share with this one
 * (ACTION_SEND, ACTION_SEND_MULTIPLE: ui.OnShare), its files copied to the
 * app's caches.
 */

private val shareCopier = Executors.newSingleThreadExecutor()
private var shareSheets = 0

/** ui.ShareKind. */
private object Kind { const val TEXT = 0; const val LINK = 1; const val IMAGE = 2; const val VIDEO = 3; const val AUDIO = 4; const val FILE = 5 }

/**
 * What another app shared, as the Shared event says it: its texts (a
 * link alone is a link; a title in EXTRA_SUBJECT comes first, as Safari's
 * on iOS), then its files, copied to the app's caches (ryui-shared/) off
 * the main thread, where their grant still holds. Then ui.OnShare gets it.
 */
fun RyUIRuntime.receive(intent: Intent) {
    val texts = ArrayList<CharSequence>()
    if (intent.action == Intent.ACTION_SEND_MULTIPLE) {
        intent.getCharSequenceArrayListExtra(Intent.EXTRA_TEXT)?.let { texts.addAll(it) }
    }
    if (texts.isEmpty()) intent.getCharSequenceExtra(Intent.EXTRA_TEXT)?.let { texts.add(it) }
    val subject = intent.getStringExtra(Intent.EXTRA_SUBJECT)?.trim().orEmpty()
    val uris = ArrayList<Uri>()
    @Suppress("DEPRECATION")
    if (intent.action == Intent.ACTION_SEND_MULTIPLE) {
        val list: List<Uri>? = if (Build.VERSION.SDK_INT >= 33) intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM, Uri::class.java)
            else intent.getParcelableArrayListExtra(Intent.EXTRA_STREAM)
        list?.let { uris.addAll(it) }
    } else {
        val u: Uri? = if (Build.VERSION.SDK_INT >= 33) intent.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
            else intent.getParcelableExtra(Intent.EXTRA_STREAM)
        u?.let { uris.add(it) }
    }
    // Some apps give the files only in the clip.
    if (uris.isEmpty()) intent.clipData?.let { c -> for (i in 0 until c.itemCount) c.getItemAt(i).uri?.let { uris.add(it) } }
    val type = intent.type
    shareCopier.execute {
        val w = RyUIWriter()
        var n = 0
        fun item(kind: Int, text: String, path: String = "", name: String = "", mime: String = "text/plain", size: Long = -1) {
            w.u8(kind); w.string(text); w.string(path); w.string(name); w.string(mime); w.u64(size)
            n++
        }
        for (t in texts) {
            val s = t.toString().trim()
            if (s.isEmpty()) continue
            val link = Regex("""^https?://\S+$""").matches(s)
            if (link && subject.isNotEmpty() && !s.contains(subject)) item(Kind.TEXT, subject)
            item(if (link) Kind.LINK else Kind.TEXT, s)
        }
        val dir = File(app.cacheDir, "ryui-shared")
        val files = ArrayList<String>()
        for (u in uris) {
            runCatching {
                val mime = app.contentResolver.getType(u) ?: type?.takeIf { !it.contains('*') }
                    ?: MimeTypeMap.getSingleton().getMimeTypeFromExtension(MimeTypeMap.getFileExtensionFromUrl(u.toString())) ?: "application/octet-stream"
                var name = ""
                var size = -1L
                if (u.scheme == "content") {
                    app.contentResolver.query(u, arrayOf(OpenableColumns.DISPLAY_NAME, OpenableColumns.SIZE), null, null, null)?.use { c ->
                        if (c.moveToFirst()) {
                            c.getColumnIndex(OpenableColumns.DISPLAY_NAME).takeIf { it >= 0 && !c.isNull(it) }?.let { name = c.getString(it) }
                            c.getColumnIndex(OpenableColumns.SIZE).takeIf { it >= 0 && !c.isNull(it) }?.let { size = c.getLong(it) }
                        }
                    }
                }
                if (name.isEmpty()) name = u.lastPathSegment?.substringAfterLast('/').orEmpty()
                name = name.replace('/', '_').replace('\u0000', '_').trim().trimStart('.')
                if (name.isEmpty()) name = "file"
                if (!name.contains('.')) MimeTypeMap.getSingleton().getExtensionFromMimeType(mime)?.let { name += ".$it" }
                val f = File(File(dir, UUID.randomUUID().toString()).apply { mkdirs() }, name)
                app.contentResolver.openInputStream(u)!!.use { i -> f.outputStream().use { o -> i.copyTo(o) } }
                val kind = when {
                    mime.startsWith("image/") -> Kind.IMAGE
                    mime.startsWith("video/") -> Kind.VIDEO
                    mime.startsWith("audio/") -> Kind.AUDIO
                    else -> Kind.FILE
                }
                item(kind, "", f.absolutePath, name, mime, f.length())
                files.add(f.absolutePath)
            }
        }
        if (n == 0) return@execute
        val out = RyUIWriter()
        out.u32(n)
        val bytes = out.bytes() + w.bytes()
        Handler(Looper.getMainLooper()).post {
            lastSharedFiles.clear()
            lastSharedFiles.addAll(files)
            shared(bytes)
        }
    }
}

/**
 * The "share" module. "files" (args: u32 view, string MIME, string UTI,
 * string title, u32 count, then the paths) shows the chooser for the
 * files, through the app's FileProvider (a file outside its folders is
 * copied to its caches first), and answers once the user is back.
 */
fun RyUIRuntime.shareMethods(): Map<String, RyUIModuleMethod> = mapOf(
    "files" to { args, reply ->
        val act = activity
        val b = ByteBuffer.wrap(args).order(ByteOrder.LITTLE_ENDIAN)
        fun str(): String { val a = ByteArray(b.int); b.get(a); return String(a, Charsets.UTF_8) }
        val parsed = runCatching {
            b.int // the view: a popover's, which Android hasn't
            val mime = str()
            str() // UTI: iOS's
            val title = str()
            val paths = List(b.int.coerceIn(0, 10_000)) { str() }
            Triple(mime, title, paths)
        }.getOrNull()
        val missing = parsed?.third?.firstOrNull { !File(it).isFile }
        when {
            parsed == null || parsed.third.isEmpty() -> reply(Result.failure(Exception("bad arguments")))
            missing != null -> reply(Result.failure(Exception("no file at $missing")))
            act == null -> reply(Result.failure(Exception("no screen")))
            else -> {
                val (mime, title, paths) = parsed
                val authority = app.packageName + ".ryui.files"
                val uris = ArrayList(paths.map { p ->
                    val f = File(p)
                    try {
                        FileProvider.getUriForFile(app, authority, f)
                    } catch (_: IllegalArgumentException) {
                        val c = File(File(app.cacheDir, "ryui-share/" + UUID.randomUUID()).apply { mkdirs() }, f.name)
                        f.copyTo(c, overwrite = true)
                        FileProvider.getUriForFile(app, authority, c)
                    }
                })
                val send = Intent(if (uris.size == 1) Intent.ACTION_SEND else Intent.ACTION_SEND_MULTIPLE).apply {
                    type = mime.ifEmpty { if (uris.size == 1) app.contentResolver.getType(uris[0]) ?: "*/*" else "*/*" }
                    if (uris.size == 1) putExtra(Intent.EXTRA_STREAM, uris[0]) else putParcelableArrayListExtra(Intent.EXTRA_STREAM, uris)
                    if (title.isNotEmpty()) putExtra(Intent.EXTRA_TITLE, title)
                    // The clip: the grant to every file, and the sheet's preview.
                    clipData = ClipData.newRawUri(null, uris[0]).also { c -> for (u in uris.drop(1)) c.addItem(ClipData.Item(u)) }
                    addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                }
                val chooser = Intent.createChooser(send, title.ifEmpty { null })
                var launcher: androidx.activity.result.ActivityResultLauncher<Intent>? = null
                launcher = act.activityResultRegistry.register("ryui-share-${shareSheets++}", ActivityResultContracts.StartActivityForResult()) {
                    launcher?.unregister()
                    reply(Result.success(ByteArray(0)))
                }
                try {
                    launcher.launch(chooser)
                } catch (e: Exception) {
                    launcher.unregister()
                    reply(Result.failure(e))
                }
            }
        }
    },
)
