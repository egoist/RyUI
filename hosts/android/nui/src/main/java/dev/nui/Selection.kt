package dev.nui

import android.annotation.SuppressLint
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.graphics.Rect
import android.graphics.RectF
import android.graphics.drawable.Drawable
import android.os.SystemClock
import android.view.ActionMode
import android.view.GestureDetector
import android.view.Gravity
import android.view.HapticFeedbackConstants
import android.view.Menu
import android.view.MenuItem
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.view.ViewTreeObserver
import android.view.WindowManager
import android.widget.ImageView
import android.widget.PopupWindow
import java.text.BreakIterator

/**
 * A Selectable: its RichText descendants, in document order, are one text
 * (joined by newlines) with one selection, as a TextView's: a long press
 * selects a word (and a drag goes on selecting), the theme's handles move
 * its ends, the floating toolbar copies it, a tap clears it.
 */
@SuppressLint("ViewConstructor")
class NuiSelectionArea(private val rt: NuiRuntime) : NuiContainer(rt.themed) {
    private var a = 0
    private var b = 0
    private val active get() = a < b
    private var extending = false // a long press is being dragged
    private var anchor = 0..0 // the word it began on
    private var mode: ActionMode? = null
    private var hidingMenu = false
    private val start = Handle(true)
    private val end = Handle(false)

    private val detector = GestureDetector(context, object : GestureDetector.SimpleOnGestureListener() {
        override fun onLongPress(e: MotionEvent) = beginSelecting(e.x, e.y)
        override fun onSingleTapUp(e: MotionEvent): Boolean {
            if (active) clear()
            return false
        }
    })

    // The texts

    /** The area's rich texts, in document order. */
    private fun segments(): List<NuiRichText> {
        val out = ArrayList<NuiRichText>()
        fun walk(g: ViewGroup) {
            for (i in 0 until g.childCount) {
                when (val c = g.getChildAt(i)) {
                    is NuiRichText -> out.add(c)
                    is ViewGroup -> walk(c)
                }
            }
        }
        walk(this)
        return out
    }

    /** Each segment with its offset in the text. */
    private fun layout(): List<Pair<NuiRichText, Int>> {
        val out = ArrayList<Pair<NuiRichText, Int>>()
        var at = 0
        for (s in segments()) {
            out.add(s to at)
            at += s.text.length + 1
        }
        return out
    }

    private fun string() = segments().joinToString("\n") { it.text }

    /** Where a descendant's (0, 0) is in the area. */
    private fun origin(v: View): Pair<Float, Float> {
        var x = 0f
        var y = 0f
        var c: View = v
        while (c !== this) {
            val p = c.parent as? View ?: break
            x += c.left + c.translationX - p.scrollX
            y += c.top + c.translationY - p.scrollY
            c = p
        }
        return x to y
    }

    private fun segmentAt(x: Float, y: Float): Pair<NuiRichText, Int>? {
        for ((s, at) in layout()) {
            val (ox, oy) = origin(s)
            if (x >= ox && x < ox + s.width && y >= oy && y < oy + s.height) return s to at
        }
        return null
    }

    /** The offset closest to (x, y), in the area. */
    private fun offsetAt(x: Float, y: Float): Int {
        var best: Pair<NuiRichText, Int>? = null
        var dist = Float.MAX_VALUE
        for ((s, at) in layout()) {
            val (ox, oy) = origin(s)
            val dy = if (y < oy) oy - y else if (y > oy + s.height) y - oy - s.height else 0f
            val dx = if (x < ox) ox - x else if (x > ox + s.width) x - ox - s.width else 0f
            val d = dy * 4 + dx // rows first
            if (d < dist) { dist = d; best = s to at }
        }
        val (s, at) = best ?: return 0
        val (ox, oy) = origin(s)
        return at + s.closestOffset(x - ox, y - oy)
    }

    /** The caret at offset o, in the area. */
    private fun caret(o: Int): RectF? {
        var found: Pair<NuiRichText, Int>? = null
        for (p in layout()) if (o >= p.second) found = p
        val (s, at) = found ?: return null
        val r = s.caret(minOf(o - at, s.text.length))
        val (ox, oy) = origin(s)
        r.offset(ox, oy)
        return r
    }

    /** The selection's bounds, in the area. */
    private fun bounds(): RectF? {
        var out: RectF? = null
        for ((s, at) in layout()) {
            val f = maxOf(a - at, 0)
            val t = minOf(b - at, s.text.length)
            if (f >= t) continue
            val (ox, oy) = origin(s)
            for (r in s.rects(f, t)) {
                r.offset(ox, oy)
                out = out?.apply { union(r) } ?: RectF(r)
            }
        }
        return out
    }

    // The selection

    private fun beginSelecting(x: Float, y: Float) {
        if (segmentAt(x, y) == null) return
        val o = offsetAt(x, y)
        anchor = word(o)
        performHapticFeedback(HapticFeedbackConstants.LONG_PRESS)
        parent?.requestDisallowInterceptTouchEvent(true)
        // The texts (their links) and scroll views within don't get this gesture.
        val now = SystemClock.uptimeMillis()
        MotionEvent.obtain(now, now, MotionEvent.ACTION_CANCEL, x, y, 0).also { super.dispatchTouchEvent(it); it.recycle() }
        extending = true
        select(anchor.first, anchor.last)
    }

    /** The word at offset o, as first..end (exclusive end). */
    private fun word(o: Int): IntRange {
        val s = string()
        if (s.isEmpty()) return 0..0
        val it = BreakIterator.getWordInstance()
        it.setText(s)
        var at = o.coerceIn(0, s.length)
        // An offset after a word (the touch was on its last letter) is that word's.
        if (at > 0 && Character.isLetterOrDigit(s[at - 1]) && (at == s.length || !Character.isLetterOrDigit(s[at]))) at--
        var f = if (at < s.length && it.isBoundary(at)) at else it.preceding(at).coerceAtLeast(0)
        var t = it.following(minOf(at, s.length - 1)).let { e -> if (e == BreakIterator.DONE) s.length else e }
        if (f == t) { f = maxOf(0, at - 1); t = minOf(s.length, f + 1) }
        return f..t
    }

    private fun select(from: Int, to: Int) {
        val n = layout().lastOrNull()?.let { it.second + it.first.text.length } ?: 0
        a = from.coerceIn(0, n)
        b = to.coerceIn(a, n)
        for ((s, at) in layout()) {
            val f = maxOf(a - at, 0)
            val t = minOf(b - at, s.text.length)
            s.selected = if (f < t) f until t else null
        }
        if (active) {
            showHandles()
            if (!extending && !start.dragging && !end.dragging) showMenu() else hideMenu()
        } else {
            start.hide(); end.hide(); hideMenu()
        }
    }

    private fun clear() {
        extending = false
        select(0, 0)
    }

    /** The text changed (streaming): keep the selection within it. */
    fun textChanged() {
        if (!active) return
        post { if (active) select(a, b) }
    }

    override fun dispatchTouchEvent(e: MotionEvent): Boolean {
        // A new gesture: whatever the last one left (an end it never got).
        if (e.actionMasked == MotionEvent.ACTION_DOWN && extending) {
            extending = false
            select(a, b)
        }
        detector.onTouchEvent(e)
        if (extending) {
            when (e.actionMasked) {
                MotionEvent.ACTION_MOVE -> {
                    // Word by word, from the word it began on, as a TextView's.
                    val w = word(offsetAt(e.x, e.y))
                    select(minOf(anchor.first, w.first), maxOf(anchor.last, w.last))
                }
                MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                    extending = false
                    select(a, b)
                }
            }
            return true
        }
        val handled = super.dispatchTouchEvent(e)
        // The gesture is the area's from its start (a text doesn't take it),
        // so it sees it through: a scroll view's taking it over cancels the
        // long press, as a drag does.
        if (e.actionMasked == MotionEvent.ACTION_DOWN && (active || segmentAt(e.x, e.y) != null)) return true
        return handled
    }

    // The floating toolbar

    private fun showMenu() {
        val m = mode
        if (m != null) { m.invalidateContentRect(); return }
        mode = startActionMode(object : ActionMode.Callback2() {
            override fun onCreateActionMode(mode: ActionMode, menu: Menu): Boolean {
                menu.add(Menu.NONE, android.R.id.copy, 0, android.R.string.copy).setShowAsAction(MenuItem.SHOW_AS_ACTION_ALWAYS)
                menu.add(Menu.NONE, android.R.id.selectAll, 1, android.R.string.selectAll).setShowAsAction(MenuItem.SHOW_AS_ACTION_ALWAYS)
                return true
            }

            override fun onPrepareActionMode(mode: ActionMode, menu: Menu): Boolean {
                val n = string().length
                menu.findItem(android.R.id.selectAll)?.isVisible = a != 0 || b != n
                return true
            }

            override fun onActionItemClicked(mode: ActionMode, item: MenuItem): Boolean {
                when (item.itemId) {
                    android.R.id.copy -> {
                        val s = string()
                        // Not the inline components' placeholders.
                        val text = s.substring(a.coerceIn(0, s.length), b.coerceIn(0, s.length)).replace("\uFFFC", "")
                        (context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager)
                            .setPrimaryClip(ClipData.newPlainText(null, text))
                        clear()
                    }
                    android.R.id.selectAll -> {
                        select(0, string().length)
                        mode.invalidate()
                    }
                    else -> return false
                }
                return true
            }

            override fun onDestroyActionMode(m: ActionMode) {
                if (mode === m) mode = null
                // Dismissed (back): the selection goes with it, as a TextView's.
                if (!hidingMenu) clear()
            }

            override fun onGetContentRect(mode: ActionMode, view: View, out: Rect) {
                val r = bounds() ?: RectF()
                // Only the part of it that's shown.
                val vis = Rect()
                if (getLocalVisibleRect(vis)) r.intersect(RectF(vis))
                out.set(r.left.toInt(), r.top.toInt(), Math.ceil(r.right.toDouble()).toInt(), Math.ceil(r.bottom.toDouble()).toInt())
            }
        }, ActionMode.TYPE_FLOATING)
    }

    private fun hideMenu() {
        val m = mode ?: return
        hidingMenu = true
        mode = null
        m.finish()
        hidingMenu = false
    }

    // The handles: the theme's, in popups over the window, as a TextView's.

    private fun showHandles() {
        start.show(caret(a))
        end.show(caret(b))
    }

    private val reposition = ViewTreeObserver.OnPreDrawListener {
        if (active && isShown) {
            showHandles()
            mode?.invalidateContentRect()
        }
        true
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        viewTreeObserver.addOnPreDrawListener(reposition)
    }

    override fun onDetachedFromWindow() {
        viewTreeObserver.removeOnPreDrawListener(reposition)
        start.hide(); end.hide(); hideMenu()
        super.onDetachedFromWindow()
    }

    private inner class Handle(val isStart: Boolean) {
        private val image: Drawable? = context.obtainStyledAttributes(
            intArrayOf(if (isStart) android.R.attr.textSelectHandleLeft else android.R.attr.textSelectHandleRight)
        ).let { t -> t.getDrawable(0).also { t.recycle() } }
        private val w = image?.intrinsicWidth ?: 0
        private val h = image?.intrinsicHeight ?: 0
        // The point of the handle that sits at the caret's bottom.
        private val hotspot = if (isStart) w * 3 / 4 else w / 4
        var dragging = false
        private var caretHeight = 0f
        private var touchX = 0f
        private var touchY = 0f

        @SuppressLint("ClickableViewAccessibility")
        private val view = ImageView(context).apply {
            setImageDrawable(image)
            setOnTouchListener { _, e -> touch(e) }
        }

        private val popup = PopupWindow(context, null, android.R.attr.textSelectHandleWindowStyle).apply {
            contentView = view
            width = w
            height = h
            isSplitTouchEnabled = true
            isClippingEnabled = false
            windowLayoutType = WindowManager.LayoutParams.TYPE_APPLICATION_SUB_PANEL
            setBackgroundDrawable(null)
        }

        fun show(caret: RectF?) {
            if (caret == null || image == null) { hide(); return }
            caretHeight = caret.height()
            // Hidden when its end of the selection is scrolled out of view.
            val vis = Rect()
            if (!getLocalVisibleRect(vis) || !vis.contains(caret.left.toInt(), (caret.top + caret.bottom).toInt() / 2)) { hide(); return }
            val loc = IntArray(2)
            getLocationInWindow(loc)
            val x = loc[0] + caret.left.toInt() - hotspot
            val y = loc[1] + caret.bottom.toInt()
            if (popup.isShowing) popup.update(x, y, w, h) else popup.showAtLocation(this@NuiSelectionArea, Gravity.NO_GRAVITY, x, y)
        }

        fun hide() {
            if (popup.isShowing && !dragging) popup.dismiss()
        }

        private fun touch(e: MotionEvent): Boolean {
            val loc = IntArray(2)
            this@NuiSelectionArea.getLocationOnScreen(loc)
            when (e.actionMasked) {
                MotionEvent.ACTION_DOWN -> {
                    dragging = true
                    // Where in the handle it's held, from the caret's bottom.
                    touchX = e.x - hotspot
                    touchY = e.y
                    hideMenu()
                }
                MotionEvent.ACTION_MOVE -> {
                    val x = e.rawX - loc[0] - touchX
                    val y = e.rawY - loc[1] - touchY - caretHeight / 2
                    val o = offsetAt(x, y)
                    // The ends never cross, as a TextView's.
                    if (isStart) select(minOf(o, b - 1), b) else select(a, maxOf(o, a + 1))
                }
                MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                    dragging = false
                    select(a, b)
                }
            }
            return true
        }
    }
}
