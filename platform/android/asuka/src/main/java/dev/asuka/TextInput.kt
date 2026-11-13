package dev.asuka

import android.content.ClipDescription
import android.content.Context
import android.os.Handler
import android.os.Looper
import android.view.KeyEvent
import android.view.View
import android.widget.EditText
import androidx.appcompat.widget.AppCompatEditText
import androidx.core.view.ContentInfoCompat
import androidx.core.view.OnReceiveContentListener
import androidx.core.view.ViewCompat
import java.io.File
import java.io.InputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.UUID

/**
 * A TextField's EditText: it reports its selection as it moves (after
 * the text change that moved it), unless the core is setting it.
 */
class AsukaEditText(context: Context, private val rt: AsukaRuntime, private val id: Int) : AppCompatEditText(context) {
    var settingSelection = false
    /** TextFieldProps.Rich: its text is styled (RichField.kt). */
    var rich = false
    /** A style set at the cursor (Format with no selection): what's typed there next takes it. */
    class Pending(val at: Int, val style: AsukaFieldStyle)
    var pending: Pending? = null
    var lastFormats = -1
    var lastLink = ""
    // The text change under way: where, and how much was put in.
    var changeStart = 0
    var changeCount = 0
    val history = AsukaRichHistory()
    private var queued = false
    private var last = -1L
    // EditText's constructor moves the selection before this class's
    // fields are set: until then there's nothing to report it to.
    private var ready = false

    init { ready = true }

    override fun onSelectionChanged(selStart: Int, selEnd: Int) {
        super.onSelectionChanged(selStart, selEnd)
        if (!ready) return
        if (rich) {
            richSelectionMoved(selStart, selEnd)
            // A pending style is the cursor's where it was set: moved, it's gone.
            pending?.let { if (selStart != it.at || selEnd != it.at) pending = null }
            rt.nodes[id]?.let { rt.reportFormats(it) }
        }
        if (settingSelection) {
            last = pack(selStart, selEnd)
            return
        }
        if (queued || rt.nodes[id]?.listens(AsukaEventKind.SELECTION_CHANGED) != true) return
        queued = true
        // After the text change that moved it (its watcher reports first).
        post {
            queued = false
            val s = selectionStart.coerceAtLeast(0)
            val e = selectionEnd.coerceAtLeast(0)
            val p = pack(s, e)
            if (p == last) return@post
            last = p
            val w = AsukaWriter()
            w.u32(s)
            w.u32(e)
            val b = w.bytes()
            rt.emit(id, AsukaEventKind.SELECTION_CHANGED) { AsukaEvents.selectionChanged(id, b) }
        }
    }

    // A rich field's undo and redo are its own history's (AsukaRichHistory).
    override fun onTextContextMenuItem(item: Int): Boolean {
        if (rich && (item == android.R.id.undo || item == android.R.id.redo)) {
            rt.nodes[id]?.let { rt.undoRich(it, item == android.R.id.redo) }
            return true
        }
        return super.onTextContextMenuItem(item)
    }

    override fun onKeyShortcut(keyCode: Int, event: KeyEvent): Boolean {
        if (rich) {
            val undo = keyCode == KeyEvent.KEYCODE_Z && event.hasModifiers(KeyEvent.META_CTRL_ON)
            val redo = keyCode == KeyEvent.KEYCODE_Z && event.hasModifiers(KeyEvent.META_CTRL_ON or KeyEvent.META_SHIFT_ON) ||
                keyCode == KeyEvent.KEYCODE_Y && event.hasModifiers(KeyEvent.META_CTRL_ON)
            if (undo || redo) return onTextContextMenuItem(if (redo) android.R.id.redo else android.R.id.undo)
            // A hardware keyboard's bold, italic and underline, as other platforms' editors take them.
            val bits = when (keyCode) {
                KeyEvent.KEYCODE_B -> 1
                KeyEvent.KEYCODE_I -> 2
                KeyEvent.KEYCODE_U -> 4
                else -> 0
            }
            if (bits != 0 && event.hasModifiers(KeyEvent.META_CTRL_ON)) {
                rt.nodes[id]?.let { rt.format(it, bits, 2) }
                return true
            }
        }
        return super.onKeyShortcut(keyCode, event)
    }

    private fun pack(s: Int, e: Int) = (s.toLong() shl 32) or (e.toLong() and 0xffffffffL)
}

/** TextFieldProps.Selection: the cursor (or the selection) at UTF-16 offsets, clamped to the text. */
fun AsukaRuntime.setFieldSelection(n: AsukaNode, v: ByteBuffer) {
    val e = n.view as? EditText ?: return
    val b = v.duplicate().order(ByteOrder.LITTLE_ENDIAN)
    if (b.remaining() < 8) return
    val len = e.text?.length ?: 0
    val s = b.int.coerceIn(0, len)
    val end = b.int.coerceIn(0, len)
    val r = e as? AsukaEditText
    r?.settingSelection = true
    e.setSelection(s, end)
    r?.settingSelection = false
}

/** InsertText: text in place of the selection, as typing (its watcher reports it). */
fun AsukaRuntime.insertFieldText(n: AsukaNode, s: String) {
    val e = n.view as? EditText ?: return
    val ed = e.text ?: return
    val a = minOf(e.selectionStart, e.selectionEnd).let { if (it < 0) ed.length else it }
    val b = maxOf(e.selectionStart, e.selectionEnd).let { if (it < 0) ed.length else it }
    ed.replace(a, b, s)
}

/**
 * TextFieldProps.PasteTypes: what a keyboard puts in (GIFs, stickers:
 * commitContent), a paste or a drop of content of these types goes to
 * the app (Pasted), its files copied into the app's caches; the rest
 * goes into the text as usual. AppCompatEditText tells the keyboard
 * which types it takes.
 */
fun AsukaRuntime.setPasteTypes(n: AsukaNode, v: ByteBuffer) {
    val e = n.view as? EditText ?: return
    val types = decodeStrings(v)
    if (types.isEmpty()) {
        ViewCompat.setOnReceiveContentListener(e, null, null)
        return
    }
    val id = n.id
    ViewCompat.setOnReceiveContentListener(e, types.toTypedArray(), OnReceiveContentListener { _, payload ->
        val desc = payload.clip.description
        var hint: String? = null
        for (i in 0 until desc.mimeTypeCount) {
            val m = desc.getMimeType(i)
            if (!m.contains('*') && !m.startsWith("text/")) { hint = m; break }
        }
        val split = payload.partition { item ->
            val u = item.uri
            u != null && matchMIME(types, uriMIME(u, hint)) ||
                u == null && item.text != null && matchMIME(types, ClipDescription.MIMETYPE_TEXT_PLAIN)
        }
        val mine = split.first ?: return@OnReceiveContentListener payload
        receivePasted(id, mine, hint)
        split.second
    })
}

/**
 * Copies what was pasted into the caches, off the main thread, then
 * sends Pasted. The streams are opened here, while the keyboard's grant
 * to its content holds.
 */
private fun AsukaRuntime.receivePasted(id: Int, c: ContentInfoCompat, hint: String?) {
    class Open(val kind: Int, val text: String, val stream: InputStream?, val name: String, val mime: String)
    val opened = ArrayList<Open>()
    val clip = c.clip
    for (i in 0 until clip.itemCount) {
        val it = clip.getItemAt(i)
        val u = it.uri
        if (u == null) {
            val s = it.text?.toString() ?: continue
            opened.add(Open(AsukaShareKind.TEXT, s, null, "", ClipDescription.MIMETYPE_TEXT_PLAIN))
            continue
        }
        val mime = uriMIME(u, hint)
        val stream = runCatching { app.contentResolver.openInputStream(u) }.getOrNull() ?: continue
        var name = runCatching {
            app.contentResolver.query(u, arrayOf(android.provider.OpenableColumns.DISPLAY_NAME), null, null, null)?.use { q ->
                if (q.moveToFirst() && !q.isNull(0)) q.getString(0) else null
            }
        }.getOrNull() ?: u.lastPathSegment?.substringAfterLast('/').orEmpty()
        name = name.replace('/', '_').trim().trimStart('.').ifEmpty { "pasted" }
        if (!name.contains('.')) android.webkit.MimeTypeMap.getSingleton().getExtensionFromMimeType(mime)?.let { name += ".$it" }
        opened.add(Open(fileKind(mime), "", stream, name, mime))
    }
    if (opened.isEmpty()) return
    shareCopier.execute {
        val w = AsukaWriter()
        var count = 0
        val dir = File(app.cacheDir, "asuka-pasted")
        for (o in opened) {
            val s = o.stream
            if (s == null) {
                w.u8(o.kind); w.string(o.text); w.string(""); w.string(""); w.string(o.mime); w.u64(-1)
                count++
                continue
            }
            val f = runCatching {
                val f = File(File(dir, UUID.randomUUID().toString()).apply { mkdirs() }, o.name)
                s.use { i -> f.outputStream().use { out -> i.copyTo(out) } }
                f
            }.getOrNull() ?: continue
            w.u8(o.kind); w.string(""); w.string(f.absolutePath); w.string(o.name); w.string(o.mime); w.u64(f.length())
            count++
        }
        if (count == 0) return@execute
        val head = AsukaWriter()
        head.u32(count)
        val bytes = head.bytes() + w.bytes()
        Handler(Looper.getMainLooper()).post { emit(id, AsukaEventKind.PASTED) { AsukaEvents.pasted(id, bytes) } }
    }
}
