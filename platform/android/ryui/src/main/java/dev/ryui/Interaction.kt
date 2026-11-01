package dev.ryui

import android.content.ClipData
import android.content.ClipDescription
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.view.DragEvent
import android.view.View
import java.io.File
import java.nio.ByteBuffer
import java.nio.ByteOrder

/*
 * Interaction states and drag and drop (DESIGN.md, "Interaction states,
 * drag and drop"): Pressed and Hover are RyUIContainer's (its setPressed
 * and dispatchHoverEvent, wired in wirePresses); here, drop targets
 * (DropAccept: an OnDragListener) and drag sources (DragItems: a long
 * click starting startDragAndDrop).
 */

/** An item of a view's DragItems (ops.Item). */
class RyUIDragItem(val kind: Int, val text: String, val path: String, val name: String, val mime: String, val value: Boolean)

/** A drag the app started: its local state, which a drop in the app reads its source from. */
class RyUIDragState(val source: Int, val mimes: List<String>) {
    var ended = false
}

/** Decodes DragItems (ops.AppendDragItems); a short list ends where it's cut. */
fun decodeDragItems(v: ByteBuffer): List<RyUIDragItem> {
    val b = v.duplicate().order(ByteOrder.LITTLE_ENDIAN)
    val out = ArrayList<RyUIDragItem>()
    runCatching {
        val n = b.int
        for (i in 0 until n) {
            val kind = b.get().toInt() and 0xff
            val text = b.str()
            val path = b.str()
            val name = b.str()
            val mime = b.str()
            val flags = b.get().toInt()
            out.add(RyUIDragItem(kind, text, path, name, mime, flags and 1 != 0))
        }
    }
    return out
}

/** Decodes a list of strings (ops.AppendStrings: DropAccept). */
fun decodeStrings(v: ByteBuffer): List<String> {
    val b = v.duplicate().order(ByteOrder.LITTLE_ENDIAN)
    val out = ArrayList<String>()
    runCatching { repeat(b.int) { out.add(b.str()) } }
    return out
}

private fun ByteBuffer.str(): String {
    val n = int
    if (n < 0 || n > remaining()) throw IndexOutOfBoundsException()
    val a = ByteArray(n)
    get(a)
    return String(a, Charsets.UTF_8)
}

/**
 * Whether a MIME type matches any of the patterns, as ops.MatchMIME: an
 * exact type, a family (image/star) or anything (star/star, star), without
 * regard to case or parameters.
 */
fun matchMIME(patterns: List<String>, mime: String): Boolean {
    val m = baseMIME(mime)
    for (pat in patterns) {
        val p = baseMIME(pat)
        when {
            p == "*" || p == "*/*" -> return true
            p.endsWith("/*") -> {
                val k = p.length - 1
                if (m.length > k && m.regionMatches(0, p, 0, k, ignoreCase = true)) return true
            }
            p.equals(m, ignoreCase = true) -> return true
        }
    }
    return false
}

/** A MIME type without its parameters and spaces. */
private fun baseMIME(m: String): String {
    val i = m.indexOf(';')
    return (if (i >= 0) m.substring(0, i) else m).trim()
}

/** DropAccept: n takes drops of these types, or none (empty). */
fun RyUIRuntime.setDropAccept(n: RyUINode, v: ByteBuffer) {
    n.dropAccept = decodeStrings(v)
    n.view.setOnDragListener(if (n.dropAccept.isEmpty()) null else RyUIDropListener(this, n.id))
}

/** DragItems: n is dragged with these items (a long click), or isn't (empty). */
fun RyUIRuntime.setDragItems(n: RyUINode, v: ByteBuffer) {
    n.dragItems = decodeDragItems(v)
    wirePresses(n)
}

/** Unpresses n's view at once: a long press or a drag took the touch. */
fun cancelPress(n: RyUINode) {
    val v = n.view
    if (v is RyUIContainer) v.cancelPress() else v.isPressed = false
}

/**
 * Starts dragging n (its long click): the view's image under the finger,
 * carrying its items' texts, links and files to other apps (files through
 * the FileProvider, readable by the app they're dropped in); a drag of
 * only values stays in the app. Its local state is its source view, which
 * the app's drops send. Reports Dragging Began, then Dropped or Cancelled
 * as it ends (to the window's root, which hears the end even if the view
 * is gone by then).
 */
fun RyUIRuntime.startDrag(n: RyUINode): Boolean {
    val v = n.view
    if (!v.isAttachedToWindow) return false
    val items = n.dragItems
    val mimes = ArrayList<String>()
    val clipItems = ArrayList<ClipData.Item>()
    fun mime(m: String) { if (m !in mimes) mimes.add(m) }
    for (it in items) {
        when {
            it.path.isNotEmpty() -> {
                if (!File(it.path).isFile) continue
                val u = runCatching { providerURI(it.path) }.getOrNull() ?: continue
                clipItems.add(ClipData.Item(u))
                mime(it.mime.ifEmpty { "application/octet-stream" })
            }
            it.kind == RyUIShareKind.LINK && it.text.isNotEmpty() -> {
                // The URL as text too: a text field takes it.
                clipItems.add(ClipData.Item(it.text, null, Uri.parse(it.text)))
                mime(ClipDescription.MIMETYPE_TEXT_URILIST)
                mime(ClipDescription.MIMETYPE_TEXT_PLAIN)
            }
            it.text.isNotEmpty() -> {
                clipItems.add(ClipData.Item(it.text))
                mime(ClipDescription.MIMETYPE_TEXT_PLAIN)
            }
        }
    }
    val global = clipItems.isNotEmpty()
    val clip = if (global) ClipData(ClipDescription("ryui", mimes.toTypedArray()), clipItems[0]).also { c ->
        for (i in 1 until clipItems.size) c.addItem(clipItems[i])
    } else ClipData(ClipDescription("ryui", arrayOf("application/x-ryui-local")), ClipData.Item(""))
    val state = RyUIDragState(n.id, items.map { it.mime })
    val flags = if (global) View.DRAG_FLAG_GLOBAL or View.DRAG_FLAG_GLOBAL_URI_READ else 0
    // The press is over: the view isn't drawn pressed in its image.
    cancelPress(n)
    val root = v.rootView
    root.setOnDragListener(dragEnd(state))
    if (!v.startDragAndDrop(clip, View.DragShadowBuilder(v), state, flags)) {
        root.setOnDragListener(null)
        return false
    }
    val id = n.id
    emit(id, RyUIEventKind.DRAGGING) { RyUIEvents.dragging(id, RyUIDragPhase.BEGAN.toLong()) }
    return true
}

/** The window root's listener for a drag the app started: its end reports Dragging Dropped or Cancelled to its source. */
private fun RyUIRuntime.dragEnd(state: RyUIDragState) = View.OnDragListener { root, e ->
    when (e.action) {
        DragEvent.ACTION_DRAG_STARTED -> e.localState === state
        DragEvent.ACTION_DRAG_ENDED -> {
            if (e.localState === state && !state.ended) {
                state.ended = true
                root.setOnDragListener(null)
                val id = state.source
                val phase = if (e.result) RyUIDragPhase.DROPPED else RyUIDragPhase.CANCELLED
                emit(id, RyUIEventKind.DRAGGING) { RyUIEvents.dragging(id, phase.toLong()) }
            }
            true
        }
        else -> false
    }
}

/**
 * A drop target's listener (DropAccept). It takes a drag with an item it
 * accepts: the app's own by their DragItems types, another app's by its
 * clip's types. Android gives each place to the innermost view taking the
 * drag. DragOver: Began with the first place over the view, Changed as it
 * moves, Cancelled as it leaves, ends or drops.
 */
class RyUIDropListener(private val rt: RyUIRuntime, private val id: Int) : View.OnDragListener {
    private var over = false

    override fun onDrag(v: View, e: DragEvent): Boolean {
        val n = rt.nodes[id] ?: return false
        val d = rt.density
        when (e.action) {
            DragEvent.ACTION_DRAG_STARTED -> {
                over = false
                return takes(n.dropAccept, e)
            }
            DragEvent.ACTION_DRAG_ENTERED -> {} // Began comes with its place, in the location that follows
            DragEvent.ACTION_DRAG_LOCATION -> {
                val state = if (over) RyUIGestureState.CHANGED else RyUIGestureState.BEGAN
                over = true
                val g = RyUIGesture(state, e.x / d, e.y / d)
                rt.emit(id, RyUIEventKind.DRAG_OVER) { RyUIEvents.dragOver(id, g) }
            }
            DragEvent.ACTION_DRAG_EXITED, DragEvent.ACTION_DRAG_ENDED -> leave(0f, 0f)
            DragEvent.ACTION_DROP -> {
                val x = e.x / d
                val y = e.y / d
                val s = e.localState as? RyUIDragState
                if (s != null) {
                    // The app's own: the core has its items, Values and all.
                    val w = RyUIWriter()
                    w.f32(x); w.f32(y); w.u32(s.source); w.u8(0); w.u32(0)
                    val b = w.bytes()
                    rt.emit(id, RyUIEventKind.DROP) { RyUIEvents.drop(id, b) }
                    leave(x, y)
                } else {
                    leave(x, y)
                    rt.receiveDrop(id, n.dropAccept, x, y, e)
                }
                return true
            }
        }
        return true
    }

    private fun leave(x: Float, y: Float) {
        if (!over) return
        over = false
        val g = RyUIGesture(RyUIGestureState.CANCELLED, x, y)
        rt.emit(id, RyUIEventKind.DRAG_OVER) { RyUIEvents.dragOver(id, g) }
    }

    /** Whether a drag starting has an item of a type accept matches. */
    private fun takes(accept: List<String>, e: DragEvent): Boolean {
        val s = e.localState as? RyUIDragState
        if (s != null) return s.mimes.any { matchMIME(accept, it) }
        val desc = e.clipDescription ?: return false
        for (i in 0 until desc.mimeTypeCount) if (matchMIME(accept, desc.getMimeType(i))) return true
        return false
    }
}

/**
 * Another app's drop on view id: with the drop's permissions, loads the
 * items accept takes, and only those, off the main thread (texts; links,
 * as links; files copied into the app's caches, ryui-dropped/), then sends
 * Drop with them (flag 1: the files are the app's copies).
 */
private fun RyUIRuntime.receiveDrop(id: Int, accept: List<String>, x: Float, y: Float, e: DragEvent) {
    val clip = e.clipData ?: return
    val perms = runCatching { activity?.requestDragAndDropPermissions(e) }.getOrNull()
    val desc = e.clipDescription
    // A file's type when its provider doesn't say: the clip's one concrete non-text type.
    var hint: String? = null
    if (desc != null) for (i in 0 until desc.mimeTypeCount) {
        val m = desc.getMimeType(i)
        if (!m.contains('*') && !m.startsWith("text/")) { hint = m; break }
    }
    shareCopier.execute {
        val w = RyUIWriter()
        var count = 0
        fun item(kind: Int, text: String, path: String, name: String, mime: String, size: Long) {
            w.u8(kind); w.string(text); w.string(path); w.string(name); w.string(mime); w.u64(size)
            count++
        }
        fun text(s: String) {
            if (matchMIME(accept, ClipDescription.MIMETYPE_TEXT_PLAIN)) item(RyUIShareKind.TEXT, s, "", "", ClipDescription.MIMETYPE_TEXT_PLAIN, -1)
        }
        fun link(s: String) {
            if (matchMIME(accept, ClipDescription.MIMETYPE_TEXT_URILIST)) item(RyUIShareKind.LINK, s, "", "", ClipDescription.MIMETYPE_TEXT_URILIST, -1)
            else text(s)
        }
        val dir = File(app.cacheDir, "ryui-dropped")
        try {
            for (i in 0 until clip.itemCount) {
                val it = clip.getItemAt(i)
                val u = it.uri
                when {
                    u != null && (u.scheme == "content" || u.scheme == "file") -> {
                        val mime = uriMIME(u, hint)
                        if (!matchMIME(accept, mime)) continue
                        val f = copyToCaches(u, mime, dir) ?: continue
                        item(f.kind, "", f.path, f.name, f.mime, f.size)
                    }
                    u != null -> link(u.toString())
                    else -> {
                        val s = (it.text ?: runCatching { it.coerceToText(app) }.getOrNull())?.toString()?.trim().orEmpty()
                        if (s.isEmpty()) continue
                        if (Regex("""^https?://\S+$""").matches(s)) link(s) else text(s)
                    }
                }
            }
        } finally {
            runCatching { perms?.release() }
        }
        if (count == 0) return@execute
        val head = RyUIWriter()
        head.f32(x); head.f32(y); head.u32(0); head.u8(1); head.u32(count)
        val b = head.bytes() + w.bytes()
        Handler(Looper.getMainLooper()).post { emit(id, RyUIEventKind.DROP) { RyUIEvents.drop(id, b) } }
    }
}
