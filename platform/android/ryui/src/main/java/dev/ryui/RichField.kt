package dev.ryui

import android.graphics.Typeface
import android.text.Editable
import android.text.Spannable
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.style.BackgroundColorSpan
import android.text.style.ForegroundColorSpan
import android.text.style.StrikethroughSpan
import android.text.style.StyleSpan
import android.text.style.TypefaceSpan
import android.text.style.URLSpan
import android.text.style.UnderlineSpan
import android.widget.EditText
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * A Rich TextField: its EditText's spans, from and to the core's styled
 * text (RichText's Content encoding). A run's style is one RyUIRunSpan,
 * which keeps the style as the core sent it, and the platform's spans
 * that draw it: bold and italic, underline, strikethrough, monospace, a
 * link, colors. Only these count: a keyboard's composing underline isn't
 * a style. Spans are exclusive-inclusive, so typing at a run's end
 * continues it, as editors do.
 */

/** A span's style (ops RichText.Content). */
data class RyUIFieldStyle(
    val textStyle: Int = 0, val size: Float = 0f, val weight: Int = 0, val flags: Int = 0,
    val color: Long = 0, val background: Long = 0, val link: String = "",
) {
    val bold get() = weight >= SEMIBOLD
    val italic get() = flags and 1 != 0
    val mono get() = flags and 2 != 0
    val underline get() = flags and 4 != 0
    val strike get() = flags and 8 != 0
    val inline get() = flags and 16 != 0

    /** TextFormat bits: 1 bold, 2 italic, 4 underline, 8 strikethrough, 16 monospaced. */
    val format: Int get() = (if (bold) 1 else 0) or (if (italic) 2 else 0) or (if (underline) 4 else 0) or
        (if (strike) 8 else 0) or (if (mono) 16 else 0)

    fun formatted(bits: Int, on: Boolean): RyUIFieldStyle {
        var w = weight
        if (bits and 1 != 0) w = if (on) maxOf(w, BOLD) else if (w >= SEMIBOLD) 0 else w
        var f = flags
        for ((bit, flag) in listOf(2 to 1, 16 to 2, 4 to 4, 8 to 8)) if (bits and bit != 0) f = if (on) f or flag else f and flag.inv()
        return copy(weight = w, flags = f)
    }

    companion object {
        const val SEMIBOLD = 6
        const val BOLD = 7
    }
}

/** A run's style, on its text. */
class RyUIRunSpan(val style: RyUIFieldStyle)
private class FieldFace(s: Int) : StyleSpan(s)
private class FieldUnderline : UnderlineSpan()
private class FieldStrike : StrikethroughSpan()
private class FieldMono : TypefaceSpan("monospace")
private class FieldLink(url: String) : URLSpan(url)
private class FieldColor(c: Int) : ForegroundColorSpan(c)
private class FieldBackground(c: Int) : BackgroundColorSpan(c)

private fun isOurs(o: Any) = o is RyUIRunSpan || o is FieldFace || o is FieldUnderline || o is FieldStrike ||
    o is FieldMono || o is FieldLink || o is FieldColor || o is FieldBackground

/** Marks text [start, end) with style s. */
private fun RyUIRuntime.mark(sp: Spannable, s: RyUIFieldStyle, start: Int, end: Int) {
    if (end <= start) return
    val f = Spanned.SPAN_EXCLUSIVE_INCLUSIVE
    sp.setSpan(RyUIRunSpan(s), start, end, f)
    val face = (if (s.bold) Typeface.BOLD else 0) or (if (s.italic) Typeface.ITALIC else 0)
    if (face != 0) sp.setSpan(FieldFace(face), start, end, f)
    if (s.underline) sp.setSpan(FieldUnderline(), start, end, f)
    if (s.strike) sp.setSpan(FieldStrike(), start, end, f)
    if (s.mono) sp.setSpan(FieldMono(), start, end, f)
    if (s.link.isNotEmpty()) sp.setSpan(FieldLink(s.link), start, end, f)
    if (s.color != 0L) sp.setSpan(FieldColor(color(s.color)), start, end, f)
    if (s.background != 0L) sp.setSpan(FieldBackground(color(s.background)), start, end, f)
}

/** The style of the text at i: its run's. */
private fun styleAt(sp: Spanned, i: Int): RyUIFieldStyle {
    val runs = sp.getSpans(i, minOf(i + 1, sp.length), RyUIRunSpan::class.java)
    return runs.lastOrNull { sp.getSpanStart(it) <= i && sp.getSpanEnd(it) > i }?.style ?: RyUIFieldStyle()
}

/** The Rich prop: the field's text, styled. */
fun RyUIRuntime.setRich(n: RyUINode, v: ByteBuffer) {
    val e = n.view as? EditText ?: return
    val r = e as? RyUIEditText
    if (r != null && r.rich && r.text != null) {
        val now = encodeRich(r.text!!)
        val sent = ByteArray(v.remaining()).also { v.duplicate().get(it) }
        if (now.contentEquals(sent)) return
    }
    r?.rich = true
    val b = v.duplicate().order(ByteOrder.LITTLE_ENDIAN)
    val out = SpannableStringBuilder()
    runCatching {
        fun str(): String { val a = ByteArray(b.int); b.get(a); return String(a, Charsets.UTF_8) }
        val styles = List(b.int) {
            RyUIFieldStyle(b.get().toInt() and 0xff, b.float, b.get().toInt() and 0xff, b.get().toInt() and 0xff, b.long, b.long, str())
        }
        val paragraphs = b.int
        var last = RyUIFieldStyle()
        // The text first, then its runs' spans: inclusive at their end,
        // spans set as it grew would spread over what came after.
        val runs = ArrayList<Triple<Int, Int, RyUIFieldStyle>>()
        for (p in 0 until paragraphs) {
            if (p > 0) {
                runs.add(Triple(out.length, out.length + 1, last))
                out.append("\n")
            }
            repeat(b.int) {
                val i = b.int
                val text = str()
                val s = styles.getOrNull(i) ?: return@repeat
                if (s.inline) return@repeat
                last = s
                runs.add(Triple(out.length, out.length + text.length, s))
                out.append(text)
            }
        }
        for ((a, z, s) in runs) mark(out, s, a, z)
    }
    // The app's content, not an edit: what came before it can't be undone to.
    r?.history?.clear()
    val s = e.selectionStart.coerceAtLeast(0)
    val end = e.selectionEnd.coerceAtLeast(0)
    settingProp = true
    r?.settingSelection = true
    e.setText(out)
    e.setSelection(minOf(s, out.length), minOf(end, out.length))
    r?.settingSelection = false
    settingProp = false
    r?.pending = null
    reportFormats(n)
}

/** The field's styled text as the core encodes it: styles in the order runs first use them, a paragraph per line, runs of one style merged. */
fun encodeRich(sp: Spanned): ByteArray {
    val text = sp.toString()
    val styles = ArrayList<RyUIFieldStyle>()
    val paragraphs = ArrayList<ArrayList<Pair<Int, String>>>()
    var at = 0
    while (true) {
        val nl = text.indexOf('\n', at)
        val end = if (nl < 0) text.length else nl
        val runs = ArrayList<Pair<Int, String>>()
        var i = at
        while (i < end) {
            var next = sp.nextSpanTransition(i, end, RyUIRunSpan::class.java)
            if (next <= i) next = end
            val s = styleAt(sp, i)
            var k = styles.indexOf(s)
            if (k < 0) { styles.add(s); k = styles.size - 1 }
            val part = text.substring(i, next)
            if (runs.isNotEmpty() && runs.last().first == k) runs[runs.size - 1] = k to runs.last().second + part
            else runs.add(k to part)
            i = next
        }
        paragraphs.add(runs)
        if (nl < 0) break
        at = nl + 1
    }
    val w = RyUIWriter()
    w.u32(styles.size)
    for (s in styles) {
        w.u8(s.textStyle); w.f32(s.size); w.u8(s.weight); w.u8(s.flags)
        w.u64(s.color); w.u64(s.background); w.string(s.link)
    }
    w.u32(paragraphs.size)
    for (p in paragraphs) {
        w.u32(p.size)
        for ((k, t) in p) { w.u32(k); w.string(t) }
    }
    return w.bytes()
}

/** The user changed the text: its styles, and the styles at the cursor. */
fun RyUIRuntime.richChanged(n: RyUINode) {
    val e = n.view as? RyUIEditText ?: return
    if (!e.rich) return
    val b = encodeRich(e.text ?: return)
    val id = n.id
    emit(id, RyUIEventKind.RICH_CHANGED) { RyUIEvents.richChanged(id, b) }
    reportFormats(n)
}

/** The styles at the cursor (what's typed next) or all the selection has, and its link, if they changed. */
fun RyUIRuntime.reportFormats(n: RyUINode) {
    val e = n.view as? RyUIEditText ?: return
    if (!e.rich || !n.listens(RyUIEventKind.FORMATS)) return
    val (f, link) = formats(e)
    if (f == e.lastFormats && link == e.lastLink) return
    e.lastFormats = f
    e.lastLink = link
    val w = RyUIWriter()
    w.u8(f)
    w.string(link)
    val id = n.id
    val b = w.bytes()
    emit(id, RyUIEventKind.FORMATS) { RyUIEvents.formats(id, b) }
}

private fun formats(e: RyUIEditText): Pair<Int, String> {
    val sp = e.text ?: return 0 to ""
    val s = minOf(e.selectionStart, e.selectionEnd).coerceAtLeast(0)
    val end = maxOf(e.selectionStart, e.selectionEnd).coerceAtLeast(0)
    if (s == end) {
        // What's typed next: the run before the cursor's, as typing continues it.
        val st = e.pending?.takeIf { it.at == s }?.style ?: if (s > 0) styleAt(sp, s - 1) else styleAt(sp, 0)
        return st.format to st.link
    }
    var all = 31
    var link: String? = null
    var i = s
    while (i < end) {
        val st = styleAt(sp, i)
        all = all and st.format
        link = if (link == null) st.link else if (link != st.link) "" else link
        val next = sp.nextSpanTransition(i, end, RyUIRunSpan::class.java)
        i = if (next <= i) end else next
    }
    return all to (link ?: "")
}

/** Restyles [start, end): each run's style through f, its spans replaced; runs outside keep theirs. */
private fun RyUIRuntime.restyle(sp: Editable, start: Int, end: Int, f: (RyUIFieldStyle) -> RyUIFieldStyle) {
    if (end <= start) return
    val runs = ArrayList<Triple<Int, Int, RyUIFieldStyle>>()
    var i = start
    while (i < end) {
        val next = sp.nextSpanTransition(i, end, RyUIRunSpan::class.java).let { if (it <= i) end else it }
        runs.add(Triple(i, next, f(styleAt(sp, i))))
        i = next
    }
    // Our spans over the range give it up; what they cover outside it keeps its style.
    for (o in sp.getSpans(start, end, Any::class.java)) {
        if (!isOurs(o)) continue
        val a = sp.getSpanStart(o)
        val b = sp.getSpanEnd(o)
        if (b <= start || a >= end) continue
        val run = o as? RyUIRunSpan
        sp.removeSpan(o)
        if (run != null) {
            if (a < start) mark(sp, run.style, a, start)
            if (b > end) mark(sp, run.style, end, b)
        }
    }
    for ((a, b, s) in runs) mark(sp, s, a, b)
}

/** The Format command: styles on, off or toggled, for the selection; at the cursor, for what's typed next. */
fun RyUIRuntime.format(n: RyUINode, bits: Int, how: Int) {
    val e = n.view as? RyUIEditText ?: return
    if (!e.rich) return
    val sp = e.text ?: return
    val (has, _) = formats(e)
    val on = if (how == 2) has and bits != bits else how == 1
    val s = minOf(e.selectionStart, e.selectionEnd).coerceAtLeast(0)
    val end = maxOf(e.selectionStart, e.selectionEnd).coerceAtLeast(0)
    if (s == end) {
        val cur = e.pending?.takeIf { it.at == s }?.style ?: if (s > 0) styleAt(sp, s - 1) else RyUIFieldStyle()
        e.pending = RyUIEditText.Pending(s, cur.formatted(bits, on))
        reportFormats(n)
        return
    }
    e.history.push(e.richState())
    e.history.typingAt = -1
    restyle(sp, s, end) { it.formatted(bits, on) }
    richChanged(n)
}

/** The SetLink command: the selection links to url ("" none); at the cursor, the link it's in. */
fun RyUIRuntime.setLink(n: RyUINode, url: String) {
    val e = n.view as? RyUIEditText ?: return
    if (!e.rich) return
    val sp = e.text ?: return
    var s = minOf(e.selectionStart, e.selectionEnd).coerceAtLeast(0)
    var end = maxOf(e.selectionStart, e.selectionEnd).coerceAtLeast(0)
    if (s == end) {
        val at = (if (s > 0) s - 1 else s).coerceAtMost(sp.length - 1)
        if (at < 0) return
        val link = styleAt(sp, at).link
        if (link.isEmpty()) return
        // The run of text with that link around the cursor.
        while (s > 0 && styleAt(sp, s - 1).link == link) s--
        while (end < sp.length && styleAt(sp, end).link == link) end++
    }
    if (s == end) return
    e.history.push(e.richState())
    e.history.typingAt = -1
    restyle(sp, s, end) { it.copy(link = url) }
    richChanged(n)
}

/** Text typed where a pending style was set (Format at the cursor) takes it. */
fun RyUIRuntime.typedRich(n: RyUINode, start: Int, count: Int) {
    val e = n.view as? RyUIEditText ?: return
    val p = e.pending ?: return
    val sp = e.text ?: return
    if (count <= 0 || start != p.at) return
    restyle(sp, start, start + count) { p.style }
    e.pending = RyUIEditText.Pending(start + count, p.style)
}

/**
 * A Rich field's undo history. Android's undo keeps a field's text only
 * (it puts deleted text back unstyled, and formatting isn't in it), so a
 * rich field keeps its own, as UIKit's and AppKit's text views do: the
 * text with its runs and the selection before each change, typing
 * grouped (a keyboard's composing included) until the cursor moves, a
 * line ends or the text is formatted. Ctrl+Z, Ctrl+Shift+Z or Ctrl+Y,
 * and the undo and redo menu items go to it.
 */
class RyUIRichHistory {
    class State(val text: SpannableStringBuilder, val start: Int, val end: Int)
    val undo = ArrayDeque<State>()
    val redo = ArrayDeque<State>()
    /** Where the typing under way continues: a change that ends there joins its group; -1: none. */
    var typingAt = -1
    /** Between a change's beforeTextChanged and afterTextChanged: the selection moves with it. */
    var changing = false
    /** Undo or redo is putting a state back: not a change to record. */
    var restoring = false

    fun push(s: State) {
        undo.addLast(s)
        if (undo.size > LIMIT) undo.removeFirst()
        redo.clear()
    }

    fun clear() {
        undo.clear()
        redo.clear()
        typingAt = -1
    }

    companion object { const val LIMIT = 200 }
}

/** The field's text with only its runs' spans (not the keyboard's or the spell checker's), and its selection. */
private fun RyUIEditText.richState(): RyUIRichHistory.State {
    val sp = text ?: SpannableStringBuilder()
    val copy = SpannableStringBuilder(sp.toString())
    for (o in sp.getSpans(0, sp.length, Any::class.java)) {
        if (isOurs(o)) copy.setSpan(o, sp.getSpanStart(o), sp.getSpanEnd(o), sp.getSpanFlags(o))
    }
    return RyUIRichHistory.State(copy, selectionStart.coerceAtLeast(0), selectionEnd.coerceAtLeast(0))
}

/** The user is about to change [start, start+count) to after characters (a TextWatcher's beforeTextChanged): remembers the field as it is, unless it continues the typing under way. */
fun RyUIEditText.richWillChange(s: CharSequence?, start: Int, count: Int, after: Int) {
    val h = history
    if (!rich || h.restoring) return
    h.changing = true
    val newline = s != null && (start until minOf(start + count, s.length)).any { s[it] == '\n' }
    // Typing, backspacing and a keyboard's composing each end where the last left off.
    if (h.typingAt < 0 || start + count != h.typingAt || newline) h.push(richState())
    h.typingAt = start + after
}

/** The change is in: a typed line break ends the group it was in. */
fun RyUIEditText.richDidChange(start: Int, count: Int) {
    val h = history
    if (!h.changing) return
    h.changing = false
    val t = text ?: return
    if ((start until minOf(start + count, t.length)).any { t[it] == '\n' }) h.typingAt = -1
}

/** The cursor moved, not by typing: what's typed next is a change of its own. */
fun RyUIEditText.richSelectionMoved(s: Int, e: Int) {
    val h = history
    if (h.changing || h.restoring) return
    if (s != e || s != h.typingAt) h.typingAt = -1
}

/** Undo (or redo): the field as it was, reported as the user's change. False if there's nothing to. */
fun RyUIRuntime.undoRich(n: RyUINode, redo: Boolean): Boolean {
    val e = n.view as? RyUIEditText ?: return false
    if (!e.rich) return false
    val h = e.history
    val to = (if (redo) h.redo else h.undo).removeLastOrNull() ?: return false
    (if (redo) h.undo else h.redo).addLast(e.richState())
    val ed = e.text ?: return false
    h.typingAt = -1
    e.pending = null
    h.restoring = true
    for (o in ed.getSpans(0, ed.length, Any::class.java)) if (isOurs(o)) ed.removeSpan(o)
    ed.replace(0, ed.length, to.text.toString())
    for (o in ed.getSpans(0, ed.length, Any::class.java)) if (isOurs(o)) ed.removeSpan(o)
    for (o in to.text.getSpans(0, to.text.length, Any::class.java)) {
        ed.setSpan(o, to.text.getSpanStart(o), to.text.getSpanEnd(o), to.text.getSpanFlags(o))
    }
    e.setSelection(to.start.coerceIn(0, ed.length), to.end.coerceIn(0, ed.length))
    h.restoring = false
    richChanged(n)
    return true
}
