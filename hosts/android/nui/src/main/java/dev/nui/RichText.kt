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
}

/** A paragraph's runs, and a key for its layouts (its runs and their styles, as bytes). */
class NuiRichParagraph(val runs: List<Pair<Int, String>>, val key: String)

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
class NuiRichText(private val rt: NuiRuntime, private val id: Int) : View(rt.themed) {
    var styles: List<NuiRichStyle> = emptyList()
    var paragraphs: List<NuiRichParagraph> = emptyList()
    var textStyle = 0
    var size = 0f
    var weight = 0
    var color = 0L
    var align = NuiTextAlign.START
    var lines = 0
    private var laid: List<StaticLayout> = emptyList()
    private var laidWidth = -1
    private var downLink: String? = null
    private val bgPaint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val rect = RectF()

    companion object {
        private val cache = LruCache<String, StaticLayout>(1024)
    }

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
        repeat(pn) { i ->
            val rn = b.int
            val runs = ArrayList<Pair<Int, String>>(rn)
            val key = StringBuilder()
            repeat(rn) {
                val si = b.int
                val t = str(b)
                runs.add(si to t)
                key.append(keyed[si]).append(t).append('\u0000')
                plain.append(t)
            }
            if (i < pn - 1) plain.append('\n')
            ps.add(NuiRichParagraph(runs, key.toString()))
        }
        styles = st
        paragraphs = ps
        contentDescription = plain
        invalidateText()
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

    private var spacingExtra = 0f
    private var spacingMult = 1f

    /** The base paint: the view's text style, as a Text's would be (and its line spacing). */
    private fun basePaint(): TextPaint {
        val tv = MaterialTextView(rt.themed)
        rt.applyTextStyle(tv, textStyle, size, weight)
        spacingExtra = tv.lineSpacingExtra
        spacingMult = tv.lineSpacingMultiplier
        val p = TextPaint(tv.paint)
        p.color = if (color != 0L) rt.color(color) else MaterialColors.getColor(rt.themed, com.google.android.material.R.attr.colorOnSurface, Color.BLACK)
        return p
    }

    private fun spanned(p: NuiRichParagraph): CharSequence {
        val sb = SpannableStringBuilder()
        val dm = resources.displayMetrics
        for ((si, text) in p.runs) {
            val s = styles[si]
            val start = sb.length
            sb.append(text)
            val end = sb.length
            fun span(o: Any) = sb.setSpan(o, start, end, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
            if (s.textStyle != 0) textAppearance(s.textStyle)?.let { span(TextAppearanceSpan(rt.themed, it)) }
            if (s.size > 0) span(AbsoluteSizeSpan(TypedValue.applyDimension(TypedValue.COMPLEX_UNIT_SP, s.size, dm).toInt()))
            if (s.mono) {
                span(TypefaceSpan("monospace"))
                span(RelativeSizeSpan(0.94f))
            }
            val w = weightOf(s.weight)
            if (w > 0 && Build.VERSION.SDK_INT >= 28) {
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
                s.link.isNotEmpty() -> MaterialColors.getColor(rt.themed, androidx.appcompat.R.attr.colorPrimary, Color.BLUE)
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

    /** The paragraphs' layouts at a width in px (cached). */
    fun layouts(width: Int): List<StaticLayout> {
        val paint = basePaint()
        val cfg = resources.configuration
        val out = ArrayList<StaticLayout>(paragraphs.size)
        var left = lines
        val alignment = when (align) {
            NuiTextAlign.CENTER -> Layout.Alignment.ALIGN_CENTER
            NuiTextAlign.END -> Layout.Alignment.ALIGN_OPPOSITE
            else -> Layout.Alignment.ALIGN_NORMAL
        }
        for (p in paragraphs) {
            if (lines > 0 && left <= 0) break
            val key = "${p.key}|$textStyle|$size|$weight|$color|$align|$left|$width|${cfg.fontScale}|${cfg.uiMode}|${cfg.densityDpi}"
            val l = cache.get(key) ?: run {
                val text = spanned(p)
                val b = StaticLayout.Builder.obtain(text, 0, text.length, paint, maxOf(width, 1))
                    .setAlignment(alignment)
                    .setIncludePad(false)
                    .setLineSpacing(spacingExtra, spacingMult)
                if (Build.VERSION.SDK_INT >= 28) b.setUseLineSpacingFromFallbacks(true)
                if (left > 0) b.setMaxLines(left).setEllipsize(TextUtils.TruncateAt.END)
                b.build().also { cache.put(key, it) }
            }
            out.add(l)
            left -= maxOf(l.lineCount, 1)
        }
        return out
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
            l.draw(canvas)
            canvas.restore()
            top += l.height
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
                    rt.emit(id, NuiEventKind.LINK) { NuiEvents.link(id, v) }
                }
                downLink = null
                return true
            }
            MotionEvent.ACTION_CANCEL -> downLink = null
        }
        return downLink != null
    }
}
