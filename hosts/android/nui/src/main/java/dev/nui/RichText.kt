package dev.nui

import android.annotation.SuppressLint
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.RectF
import android.graphics.Typeface
import android.os.Build
import android.text.Layout
import android.text.SpannableStringBuilder
import android.text.Spanned
import android.text.StaticLayout
import android.text.TextPaint
import android.text.TextUtils
import android.text.style.AbsoluteSizeSpan
import android.text.style.ForegroundColorSpan
import android.text.style.RelativeSizeSpan
import android.text.style.StrikethroughSpan
import android.text.style.StyleSpan
import android.text.style.TextAppearanceSpan
import android.text.style.TypefaceSpan
import android.text.style.UnderlineSpan
import android.util.LruCache
import android.util.TypedValue
import android.view.MotionEvent
import android.view.View
import com.google.android.material.color.MaterialColors
import com.google.android.material.textview.MaterialTextView
import java.nio.ByteBuffer
import java.text.BreakIterator

/** A span's look (see ops RichText.Content); zero fields are the view's. */
class NuiRichStyle(
    val textStyle: Int, val size: Float, val weight: Int, val flags: Int,
    val color: Long, val background: Long, val link: String,
) {
    val italic get() = flags and 1 != 0
    val mono get() = flags and 2 != 0
    val underline get() = flags and 4 != 0
    val strike get() = flags and 8 != 0
    val inline get() = flags and 16 != 0
}

/**
 * A paragraph's runs, and a key for its layouts (its runs and their styles,
 * as bytes); its inline components, as indexes into the view's.
 */
class NuiRichParagraph(val runs: List<Pair<Int, String>>, val key: String, val inlines: List<Int> = emptyList())

/** An inline component's placeholder: its size, on the baseline; drawn as nothing. */
private class NuiInlineSpan(val index: Int, val w: Int, val h: Int) : android.text.style.ReplacementSpan() {
    override fun getSize(paint: Paint, text: CharSequence?, start: Int, end: Int, fm: Paint.FontMetricsInt?): Int {
        if (fm != null) {
            paint.getFontMetricsInt(fm)
            fm.ascent = minOf(fm.ascent, -h)
            fm.top = minOf(fm.top, -h)
        }
        return w
    }

    override fun draw(canvas: Canvas, text: CharSequence?, start: Int, end: Int, x: Float, top: Int, y: Int, bottom: Int, paint: Paint) {}
}

/** Marks a span's background, which the view draws per line. */
private class NuiBackgroundSpan(val color: Int)

/** Marks a link. */
private class NuiLinkSpan(val url: String)

/**
 * RichText's view: paragraphs typeset with StaticLayout and drawn by it,
 * so what's measured is what's drawn. Layouts are cached by paragraph
 * content, the view's text props, the width and the configuration.
 */
@SuppressLint("ViewConstructor")
class NuiRichText(private val rt: NuiRuntime, private val id2: Int) : NuiContainer(rt.themed) {
    var styles: List<NuiRichStyle> = emptyList()
    var paragraphs: List<NuiRichParagraph> = emptyList()
    var textStyle = 0
    var size = 0f
    var weight = 0
    var family = ""
    var color = 0L
    var align = NuiTextAlign.START
    var lines = 0
    private var laid: List<StaticLayout> = emptyList()
    private var laidWidth = -1
    private var downLink: String? = null
    /** The plain text: the paragraphs joined by newlines. */
    var text = ""
        private set
    /** The part of the area's selection in this text, drawn as a highlight. */
    var selected: IntRange? = null
        set(v) { if (field != v) { field = v; invalidate() } }
    private val selPaint = Paint().apply {
        color = MaterialColors.getColor(rt.themed, android.R.attr.textColorHighlight, 0x6633B5E5)
    }
    private val a11y = LinkAccessibility()

    /** The inline components (children): node id and size in px, in run order. */
    private var inlines: List<Triple<Int, Int, Int>> = emptyList()

    init {
        setWillNotDraw(false)
        androidx.core.view.ViewCompat.setAccessibilityDelegate(this, a11y)
    }

    /** Sets the inline components' ids and sizes (the Inlines prop). */
    fun setInlines(b: ByteBuffer) {
        val out = ArrayList<Triple<Int, Int, Int>>()
        if (b.remaining() >= 4) {
            repeat(b.int) {
                val id = b.int
                val w = Math.round(b.float * rt.density)
                val h = Math.round(b.float * rt.density)
                out.add(Triple(id, w, h))
            }
        }
        inlines = out
        invalidateText()
    }
    private val bgPaint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val rect = RectF()

    companion object {
        // Paragraph layouts by their inputs, shared by all rich texts. They're
        // built ahead on these threads when a text's content changes (at the
        // width it was laid out at), so that the main thread, measuring,
        // takes them built or waits for the rest of the work, not all of it.
        private val cache = LruCache<String, StaticLayout>(1024)
        private val pending = java.util.concurrent.ConcurrentHashMap<String, java.util.concurrent.Future<StaticLayout>>()
        private val workers = java.util.concurrent.Executors.newFixedThreadPool(2) { r: Runnable ->
            Thread(r, "nui-richtext").apply { isDaemon = true; priority = Thread.NORM_PRIORITY - 1 }
        }
    }

    /** The width in px it was last laid out at: where new content is built ahead. */
    private var lastWidth = -1

    fun setContent(b: ByteBuffer) {
        val keyed = ArrayList<String>()
        val st = ArrayList<NuiRichStyle>()
        if (b.remaining() < 8) {
            styles = emptyList(); paragraphs = emptyList(); invalidateText(); return
        }
        val n = b.int
        repeat(n) {
            val start = b.position()
            val s = NuiRichStyle(b.get().toInt() and 0xff, b.float, b.get().toInt() and 0xff, b.get().toInt() and 0xff,
                b.long, b.long, str(b))
            val raw = ByteArray(b.position() - start)
            b.duplicate().apply { position(start) }.get(raw)
            keyed.add(String(raw, Charsets.ISO_8859_1))
            st.add(s)
        }
        val pn = b.int
        val ps = ArrayList<NuiRichParagraph>(pn)
        val plain = StringBuilder()
        var inlineCount = 0
        repeat(pn) { i ->
            val rn = b.int
            val runs = ArrayList<Pair<Int, String>>(rn)
            val key = StringBuilder()
            val ins = ArrayList<Int>()
            repeat(rn) {
                val si = b.int
                val t = str(b)
                if (st[si].inline) ins.add(inlineCount++)
                runs.add(si to t)
                key.append(keyed[si]).append(t).append('\u0000')
                plain.append(t)
            }
            if (i < pn - 1) plain.append('\n')
            ps.add(NuiRichParagraph(runs, key.toString(), ins))
        }
        styles = st
        paragraphs = ps
        text = plain.toString()
        // The inline components are views of their own.
        contentDescription = text.replace("\uFFFC", "")
        selected = null
        invalidateText()
        (selectionArea())?.textChanged()
        prepare()
    }

    private fun str(b: ByteBuffer): String {
        val a = ByteArray(b.int)
        b.get(a)
        return String(a, Charsets.UTF_8)
    }

    /** Typesetting inputs changed (props, theme, font scale): lay out again. */
    fun invalidateText() {
        laidWidth = -1
        requestLayout()
        invalidate()
    }

    override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
        super.onLayout(changed, l, t, r, b)
        placeInlines()
    }

    /**
     * Puts each inline component at its placeholder (the core sizes it); one
     * in a line cut off is hidden.
     */
    fun placeInlines() {
        if (inlines.isEmpty()) return
        val at = HashMap<Int, Pair<Int, Int>>()
        var top = 0f
        for (l in current()) {
            val text = l.text as? Spanned
            text?.getSpans(0, text.length, NuiInlineSpan::class.java)?.forEach { s ->
                val o = text.getSpanStart(s)
                val line = l.getLineForOffset(o)
                if (o >= l.getLineEnd(line) || s.index < 0) return@forEach
                if (line == l.lineCount - 1 && l.getEllipsisCount(line) > 0 && o >= l.getLineStart(line) + l.getEllipsisStart(line)) return@forEach
                val x = l.getPrimaryHorizontal(o)
                val base = top + l.getLineBaseline(line)
                at[s.index] = Math.round(x) to Math.round(base - s.h)
            }
            top += l.height
        }
        for ((i, c) in inlines.withIndex()) {
            val v = rt.nodes[c.first]?.outer ?: continue
            if (v.parent !== this) continue
            val p = at[i]
            if (p == null) { v.visibility = INVISIBLE; continue }
            v.visibility = VISIBLE
            v.layout(p.first, p.second, p.first + v.width, p.second + v.height)
        }
    }

    private var spacingExtra = 0f
    private var spacingMult = 1f

    private var paint: TextPaint? = null
    private var paintKey = ""

    /** The base paint: the view's text style, as a Text's would be (and its line spacing). */
    private fun basePaint(): TextPaint {
        val cfg = resources.configuration
        val key = "$textStyle|$size|$weight|$family|${NuiFonts.version}|$color|${cfg.fontScale}|${cfg.uiMode}|${cfg.densityDpi}|${rt.themeVersion}"
        paint?.let { if (key == paintKey) return it }
        val tv = MaterialTextView(rt.themed)
        rt.applyTextStyle(tv, textStyle, size, weight, family)
        spacingExtra = tv.lineSpacingExtra
        spacingMult = tv.lineSpacingMultiplier
        val p = TextPaint(tv.paint)
        p.color = if (color != 0L) rt.color(color) else MaterialColors.getColor(rt.themed, com.google.android.material.R.attr.colorOnSurface, Color.BLACK)
        paint = p
        paintKey = key
        return p
    }

    private fun spanned(p: NuiRichParagraph): CharSequence {
        val sb = SpannableStringBuilder()
        val dm = resources.displayMetrics
        var next = 0
        for ((si, text) in p.runs) {
            val s = styles[si]
            val start = sb.length
            sb.append(text)
            val end = sb.length
            fun span(o: Any) = sb.setSpan(o, start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
            if (s.inline) {
                val i = p.inlines.getOrElse(next++) { -1 }
                val size = inlines.getOrNull(i)
                span(NuiInlineSpan(i, size?.second ?: 0, size?.third ?: 0))
                continue
            }
            if (s.textStyle != 0) textAppearance(s.textStyle)?.let { span(TextAppearanceSpan(rt.themed, it)) }
            if (s.size > 0) span(AbsoluteSizeSpan(TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP, s.size, dm).toInt()))
            if (s.mono) {
                span(TypefaceSpan("monospace"))
                span(RelativeSizeSpan(0.94f))
            }
            val w = weightOf(s.weight)
            val custom = if (family.isNotEmpty() && !s.mono && (w > 0 || s.italic)) NuiFonts.typeface(rt.app, family, w, s.italic) else null
            if (custom != null && Build.VERSION.SDK_INT >= 28) {
                span(TypefaceSpan(custom))
            } else if (w > 0 && Build.VERSION.SDK_INT >= 28) {
                span(TypefaceSpan(Typeface.create(if (s.mono) Typeface.MONOSPACE else Typeface.DEFAULT, w, s.italic)))
            } else if (w >= 600 || s.italic) {
                span(StyleSpan(when {
                    w >= 600 && s.italic -> Typeface.BOLD_ITALIC
                    w >= 600 -> Typeface.BOLD
                    else -> Typeface.ITALIC
                }))
            }
            val c = when {
                s.color != 0L -> rt.color(s.color)
                s.link.isNotEmpty() -> rt.accentColor()
                else -> null
            }
            c?.let { span(ForegroundColorSpan(it)) }
            if (s.underline) span(UnderlineSpan())
            if (s.strike) span(StrikethroughSpan())
            if (s.background != 0L) span(NuiBackgroundSpan(rt.color(s.background)))
            if (s.link.isNotEmpty()) span(NuiLinkSpan(s.link))
        }
        return sb
    }

    private fun alignment() = when (align) {
        NuiTextAlign.CENTER -> Layout.Alignment.ALIGN_CENTER
        NuiTextAlign.END -> Layout.Alignment.ALIGN_OPPOSITE
        else -> Layout.Alignment.ALIGN_NORMAL
    }

    private fun key(p: NuiRichParagraph, left: Int, width: Int): String {
        val cfg = resources.configuration
        // With its inline components' sizes.
        val ins = if (p.inlines.isEmpty()) "" else p.inlines.joinToString(",") { i -> inlines.getOrNull(i)?.let { "${it.second}x${it.third}" } ?: "0x0" }
        return "${p.key}|$ins|$textStyle|$size|$weight|$family|${NuiFonts.version}|$color|$align|$left|$width|${cfg.fontScale}|${cfg.uiMode}|${cfg.densityDpi}|${rt.themeVersion}"
    }

    /** The paragraphs' layouts at a width in px (cached). */
    fun layouts(width: Int): List<StaticLayout> {
        val paint = basePaint()
        val out = ArrayList<StaticLayout>(paragraphs.size)
        var left = lines
        val alignment = alignment()
        if (width < (1 shl 22)) lastWidth = width
        for (p in paragraphs) {
            if (lines > 0 && left <= 0) break
            val key = key(p, left, width)
            val l = cache.get(key)
                ?: pending[key]?.let { runCatching { it.get() }.getOrNull() }
                ?: build(spanned(p), paint, width, alignment, spacingExtra, spacingMult, left).also { cache.put(key, it) }
            out.add(l)
            left -= maxOf(l.lineCount, 1)
        }
        return out
    }

    private fun build(text: CharSequence, paint: TextPaint, width: Int, alignment: Layout.Alignment, extra: Float, mult: Float, left: Int): StaticLayout {
        val b = StaticLayout.Builder.obtain(text, 0, text.length, paint, maxOf(width, 1))
            .setAlignment(alignment)
            .setIncludePad(false)
            .setLineSpacing(extra, mult)
        if (Build.VERSION.SDK_INT >= 28) b.setUseLineSpacingFromFallbacks(true)
        if (left > 0) b.setMaxLines(left).setEllipsize(TextUtils.TruncateAt.END)
        return b.build()
    }

    /**
     * Builds the paragraphs' layouts at the last width, off the main thread,
     * for the measuring to come. The spanned texts are made here (they read
     * the theme); the line breaking, the work, there.
     */
    private fun prepare() {
        if (lastWidth <= 0 || lines > 0) return
        val width = lastWidth
        val paint = basePaint()
        val alignment = alignment()
        val extra = spacingExtra
        val mult = spacingMult
        for (p in paragraphs) {
            val key = key(p, 0, width)
            if (p.inlines.isNotEmpty() || cache.get(key) != null || pending.containsKey(key)) continue
            val text = spanned(p)
            // Its own paint: a layout keeps the one it's built with.
            val own = TextPaint(paint)
            val task = java.util.concurrent.FutureTask {
                build(text, own, width, alignment, extra, mult, 0).also {
                    cache.put(key, it)
                    pending.remove(key)
                }
            }
            pending[key] = task // before it runs, so that it's gone when it's done
            workers.execute(task)
        }
    }

    /** The size the text wants within the core's constraints, in dp. */
    fun measureRich(w: Float, aw: Float): Pair<Float, Float> {
        val d = rt.density
        if (aw == Float.NEGATIVE_INFINITY && w.isNaN()) {
            // Min-content: the widest word.
            val paint = basePaint()
            var widest = 0f
            for (p in paragraphs) {
                val text = spanned(p)
                val it = BreakIterator.getWordInstance()
                it.setText(text.toString())
                var s = it.first()
                var e = it.next()
                while (e != BreakIterator.DONE) {
                    widest = maxOf(widest, Layout.getDesiredWidth(text, s, e, paint))
                    s = e
                    e = it.next()
                }
            }
            val ls = layouts(Math.ceil(widest.toDouble()).toInt())
            return Math.ceil(widest.toDouble()).toFloat() / d to ls.sumOf { it.height } / d
        }
        val width = when {
            !w.isNaN() -> Math.round(w * d)
            aw.isFinite() -> Math.round(maxOf(aw, 0f) * d)
            else -> 1 shl 22
        }
        val ls = layouts(width)
        var wide = 0f
        for (l in ls) for (i in 0 until l.lineCount) wide = maxOf(wide, l.getLineMax(i))
        val mw = if (!w.isNaN()) w else Math.ceil(wide.toDouble()).toFloat() / d
        return mw to ls.sumOf { it.height } / d
    }

    private fun current(): List<StaticLayout> {
        if (laidWidth != width) {
            laid = layouts(width)
            laidWidth = width
        }
        return laid
    }

    override fun onDraw(canvas: Canvas) {
        val pad = 3 * rt.density
        val radius = 5 * rt.density
        var top = 0f
        for (l in current()) {
            canvas.save()
            canvas.translate(0f, top)
            val text = l.text as? Spanned
            // Backgrounds first, each span's on each line it covers, rounded.
            text?.getSpans(0, text.length, NuiBackgroundSpan::class.java)?.forEach { bg ->
                val s = text.getSpanStart(bg)
                val e = text.getSpanEnd(bg)
                bgPaint.color = bg.color
                for (line in l.getLineForOffset(s)..l.getLineForOffset(maxOf(e - 1, s))) {
                    val ls = maxOf(s, l.getLineStart(line))
                    val le = minOf(e, l.getLineVisibleEnd(line))
                    if (le <= ls) continue
                    val x0 = l.getPrimaryHorizontal(ls)
                    val x1 = if (le >= l.getLineVisibleEnd(line)) l.getLineLeft(line) + l.getLineMax(line) else l.getPrimaryHorizontal(le)
                    // The text's height, not the line's (its spacing).
                    val fm = l.paint.fontMetrics
                    val base = l.getLineBaseline(line).toFloat()
                    rect.set(minOf(x0, x1) - pad, base + fm.ascent - pad / 3, maxOf(x0, x1) + pad, base + fm.descent + pad / 3)
                    canvas.drawRoundRect(rect, radius, radius, bgPaint)
                }
            }
            selectionUnder(canvas, l, top)
            l.draw(canvas)
            canvas.restore()
            top += l.height
        }
    }

    // Selection: offsets are into the plain text (UTF-16), as the area's.

    private class Line(val layout: StaticLayout, val line: Int, val top: Float, val start: Int)

    /** Each laid-out line, with its paragraph's top and offset in the text. */
    private fun lines(): List<Line> {
        val out = ArrayList<Line>()
        var top = 0f
        var start = 0
        val ls = current()
        for ((i, l) in ls.withIndex()) {
            for (k in 0 until l.lineCount) out.add(Line(l, k, top, start))
            top += l.height
            start += (if (i < paragraphs.size) paragraphs[i].runs.sumOf { it.second.length } else 0) + 1
        }
        return out
    }

    private fun lineOf(offset: Int, ls: List<Line>): Line? {
        var best: Line? = null
        for (g in ls) {
            val a = g.start + g.layout.getLineStart(g.line)
            val b = g.start + g.layout.getLineEnd(g.line)
            if (offset in a..b) { best = g; if (offset < b) break }
            if (a > offset) break
        }
        return best ?: ls.lastOrNull()
    }

    /** Where offset is: x, the line's top and bottom, in px. */
    fun caret(offset: Int): RectF {
        val g = lineOf(offset, lines()) ?: return RectF(0f, 0f, 0f, 0f)
        val local = (offset - g.start).coerceIn(0, g.layout.text.length)
        val x = g.layout.getPrimaryHorizontal(local)
        return RectF(x, g.top + g.layout.getLineTop(g.line), x, g.top + g.layout.getLineBottom(g.line))
    }

    /** The rectangles covering [from, to), a line each. */
    fun rects(from: Int, to: Int): List<RectF> {
        val out = ArrayList<RectF>()
        for (g in lines()) {
            val a = g.start + g.layout.getLineStart(g.line)
            val b = g.start + g.layout.getLineVisibleEnd(g.line)
            val s = maxOf(from, a)
            val e = minOf(to, b)
            if (s >= e) continue
            val x0 = g.layout.getPrimaryHorizontal(s - g.start)
            val x1 = if (e >= b) g.layout.getLineLeft(g.line) + g.layout.getLineMax(g.line) else g.layout.getPrimaryHorizontal(e - g.start)
            out.add(RectF(minOf(x0, x1), g.top + g.layout.getLineTop(g.line), maxOf(x0, x1), g.top + g.layout.getLineBottom(g.line)))
        }
        return out
    }

    /** The offset closest to (x, y). */
    fun closestOffset(x: Float, y: Float): Int {
        val ls = lines()
        if (ls.isEmpty()) return 0
        var best = ls[0]
        var dist = Float.MAX_VALUE
        for (g in ls) {
            val t = g.top + g.layout.getLineTop(g.line)
            val b = g.top + g.layout.getLineBottom(g.line)
            val d = if (y < t) t - y else if (y > b) y - b else 0f
            if (d < dist) { dist = d; best = g }
        }
        val o = best.layout.getOffsetForHorizontal(best.line, x)
        return best.start + o.coerceIn(best.layout.getLineStart(best.line), best.layout.getLineEnd(best.line))
    }

    private fun selectionUnder(canvas: Canvas, l: StaticLayout, top: Float) {
        val sel = selected ?: return
        for (r in rects(sel.first, sel.last + 1)) {
            // rects() are in the view's coordinates; the canvas is at the paragraph.
            if (r.bottom <= top || r.top >= top + l.height) continue
            canvas.drawRect(r.left, r.top - top, r.right, r.bottom - top, selPaint)
        }
    }

    private fun selectionArea(): NuiSelectionArea? {
        var p = parent
        while (p != null && p !is NuiSelectionArea) p = p.parent
        return p as? NuiSelectionArea
    }

    // Links, for TalkBack: each a virtual view it can find and activate.

    private class LinkRange(val start: Int, val end: Int, val url: String, val text: String)

    private fun linkRanges(): List<LinkRange> {
        val out = ArrayList<LinkRange>()
        var at = 0
        for (p in paragraphs) {
            for ((si, t) in p.runs) {
                val link = styles[si].link
                val last = out.lastOrNull()
                if (link.isNotEmpty()) {
                    if (last != null && last.url == link && last.end == at) {
                        out[out.size - 1] = LinkRange(last.start, at + t.length, link, last.text + t)
                    } else {
                        out.add(LinkRange(at, at + t.length, link, t))
                    }
                }
                at += t.length
            }
            at++
        }
        return out
    }

    override fun dispatchHoverEvent(event: MotionEvent): Boolean = a11y.dispatchHoverEvent(event) || super.dispatchHoverEvent(event)

    private inner class LinkAccessibility : androidx.customview.widget.ExploreByTouchHelper(this@NuiRichText) {
        override fun getVirtualViewAt(x: Float, y: Float): Int {
            for ((i, l) in linkRanges().withIndex()) {
                if (rects(l.start, l.end).any { it.contains(x, y) }) return i
            }
            return HOST_ID
        }

        // A node provider hides the view's children: the inline components.
        override fun onPopulateNodeForHost(node: androidx.core.view.accessibility.AccessibilityNodeInfoCompat) {
            for (i in 0 until childCount) node.addChild(getChildAt(i))
        }

        override fun getVisibleVirtualViews(ids: MutableList<Int>) {
            for (i in linkRanges().indices) ids.add(i)
        }

        override fun onPopulateNodeForVirtualView(id: Int, node: androidx.core.view.accessibility.AccessibilityNodeInfoCompat) {
            val l = linkRanges().getOrNull(id)
            if (l == null) {
                node.contentDescription = ""
                node.setBoundsInParent(android.graphics.Rect(0, 0, 1, 1))
                return
            }
            node.contentDescription = l.text
            node.roleDescription = "link"
            node.className = "android.widget.Button"
            node.isClickable = true
            node.addAction(androidx.core.view.accessibility.AccessibilityNodeInfoCompat.ACTION_CLICK)
            val rs = rects(l.start, l.end)
            val b = RectF(rs.firstOrNull() ?: RectF())
            for (r in rs) b.union(r)
            node.setBoundsInParent(android.graphics.Rect(b.left.toInt(), b.top.toInt(), Math.ceil(b.right.toDouble()).toInt(), Math.ceil(b.bottom.toDouble()).toInt()))
        }

        override fun onPerformActionForVirtualView(id: Int, action: Int, args: android.os.Bundle?): Boolean {
            if (action != androidx.core.view.accessibility.AccessibilityNodeInfoCompat.ACTION_CLICK) return false
            val l = linkRanges().getOrNull(id) ?: return false
            val url = l.url
            rt.emit(this@NuiRichText.id2, NuiEventKind.LINK) { NuiEvents.link(this@NuiRichText.id2, url) }
            return true
        }
    }

    private fun linkAt(x: Float, y: Float): String? {
        var top = 0f
        for (l in current()) {
            if (y >= top && y < top + l.height) {
                val line = l.getLineForVertical((y - top).toInt())
                if (x < l.getLineLeft(line) - 8 || x > l.getLineRight(line) + 8) return null
                val off = l.getOffsetForHorizontal(line, x)
                val text = l.text as? Spanned ?: return null
                return text.getSpans(maxOf(off - 1, 0), minOf(off + 1, text.length), NuiLinkSpan::class.java)
                    .firstOrNull { text.getSpanStart(it) <= off && off <= text.getSpanEnd(it) }?.url
            }
            top += l.height
        }
        return null
    }

    @SuppressLint("ClickableViewAccessibility")
    override fun onTouchEvent(e: MotionEvent): Boolean {
        when (e.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                downLink = linkAt(e.x, e.y)
                return downLink != null
            }
            MotionEvent.ACTION_UP -> {
                val link = linkAt(e.x, e.y)
                if (link != null && link == downLink) {
                    val v = link
                    rt.emit(id2, NuiEventKind.LINK) { NuiEvents.link(id2, v) }
                }
                downLink = null
                return true
            }
            MotionEvent.ACTION_CANCEL -> downLink = null
        }
        return downLink != null
    }
}
