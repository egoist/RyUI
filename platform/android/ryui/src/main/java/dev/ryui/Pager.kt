package dev.ryui

import android.annotation.SuppressLint
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.view.MotionEvent
import android.view.VelocityTracker
import android.view.View
import android.view.ViewConfiguration
import android.widget.FrameLayout
import android.widget.HorizontalScrollView
import androidx.core.view.AccessibilityDelegateCompat
import androidx.core.view.ViewCompat
import androidx.core.view.accessibility.AccessibilityNodeInfoCompat
import androidx.core.widget.NestedScrollView
import com.google.android.material.color.MaterialColors
import kotlin.math.abs
import kotlin.math.ceil
import kotlin.math.roundToInt

/**
 * A paging Scroll (ScrollProps.Paging): it rests only at whole pages, each
 * its own size along its axis. Android's scroll views have no paging, so
 * as the finger lifts the scroll view is told the touch was cancelled (no
 * fling), and it's scrolled to the next page in the swipe's direction when
 * it was a fling, else to the nearest: at most one page a swipe, as iOS's.
 * The page it settles on is reported as Paged, from the content's physical
 * start, as soon as it's known.
 */
@SuppressLint("ClickableViewAccessibility")
class RyUIPager(private val rt: RyUIRuntime, private val id: Int, private val v: FrameLayout) : View.OnTouchListener {
    private var tracker: VelocityTracker? = null
    private var from = 0 // the page the swipe began on
    private val minFling = ViewConfiguration.get(v.context).scaledMinimumFlingVelocity

    private val horizontal get() = v is HorizontalScrollView
    private val size get() = if (horizontal) v.width - v.paddingLeft - v.paddingRight else v.height - v.paddingTop - v.paddingBottom
    private val offset get() = if (horizontal) v.scrollX else v.scrollY
    private val pages: Int get() {
        val c = v.getChildAt(0) ?: return 1
        val s = size
        return if (s <= 0) 1 else maxOf(1, ceil((if (horizontal) c.width else c.height).toFloat() / s).toInt())
    }

    override fun onTouch(view: View, e: MotionEvent): Boolean {
        val t = tracker ?: VelocityTracker.obtain().also {
            tracker = it
            // Its first event here may come late (the touch began on a child
            // the scroll view took it from), but it rests on a page until then.
            from = if (size > 0) (offset.toFloat() / size).roundToInt() else 0
        }
        t.addMovement(e)
        val action = e.actionMasked
        if (action != MotionEvent.ACTION_UP && action != MotionEvent.ACTION_CANCEL) return false
        t.computeCurrentVelocity(1000)
        val velocity = if (horizontal) t.xVelocity else t.yVelocity
        t.recycle()
        tracker = null
        // The scroll view lets go, with no fling of its own.
        val cancel = MotionEvent.obtain(e).apply { this.action = MotionEvent.ACTION_CANCEL }
        view.onTouchEvent(cancel)
        cancel.recycle()
        settle(if (action == MotionEvent.ACTION_UP) velocity else 0f)
        return true
    }

    private fun settle(velocity: Float) {
        val s = size
        if (s <= 0) return
        var page = if (abs(velocity) >= minFling) from + (if (velocity < 0) 1 else -1)
            else (offset.toFloat() / s).roundToInt()
        page = page.coerceIn(maxOf(0, from - 1), minOf(pages - 1, from + 1))
        val to = page * s
        when (v) {
            is HorizontalScrollView -> v.smoothScrollTo(to, 0)
            is NestedScrollView -> v.smoothScrollTo(0, to)
        }
        val p = page.toLong()
        rt.emit(id, RyUIEventKind.PAGED) { RyUIEvents.paged(id, p) }
    }

    companion object {
        /** Makes a Scroll page, or not; its view is made again when it turns horizontal, and this again with it. */
        fun apply(rt: RyUIRuntime, n: RyUINode) {
            val v = n.view as? FrameLayout ?: return
            val on = n.props[RyUIProp.Scroll.PAGING] == true
            v.setOnTouchListener(if (on) RyUIPager(rt, n.id, v) else null)
            v.isHorizontalScrollBarEnabled = !on
            v.isVerticalScrollBarEnabled = !on
        }
    }
}

/**
 * A PageControl: a dot per page, the current one in the theme's primary
 * color. Material has no page indicator in its views, so it's drawn, as
 * small as iOS's and with room to tap: a tap on a dot selects its page,
 * and TalkBack adjusts it as a seek bar, saying "2 of 5".
 */
@SuppressLint("ViewConstructor")
class RyUIPageDots(private val rt: RyUIRuntime, private val id: Int) : View(rt.themed) {
    var count = 0
        set(v) {
            field = maxOf(v, 0)
            requestLayout()
            invalidate()
            describe()
        }
    var page = 0
        set(v) {
            field = v
            invalidate()
            describe()
        }

    private val d = resources.displayMetrics.density
    private val dot = 8 * d
    private val cell = 16 * d // a dot and its gap
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG)

    init {
        isFocusable = true
        ViewCompat.setAccessibilityDelegate(this, object : AccessibilityDelegateCompat() {
            override fun onInitializeAccessibilityNodeInfo(host: View, info: AccessibilityNodeInfoCompat) {
                super.onInitializeAccessibilityNodeInfo(host, info)
                info.className = "android.widget.SeekBar"
                if (page < count - 1) info.addAction(AccessibilityNodeInfoCompat.AccessibilityActionCompat.ACTION_SCROLL_FORWARD)
                if (page > 0) info.addAction(AccessibilityNodeInfoCompat.AccessibilityActionCompat.ACTION_SCROLL_BACKWARD)
                info.rangeInfo = AccessibilityNodeInfoCompat.RangeInfoCompat.obtain(
                    AccessibilityNodeInfoCompat.RangeInfoCompat.RANGE_TYPE_INT, 0f, maxOf(count - 1, 0).toFloat(), page.toFloat())
            }

            override fun performAccessibilityAction(host: View, action: Int, args: android.os.Bundle?): Boolean {
                val to = when (action) {
                    android.view.accessibility.AccessibilityNodeInfo.ACTION_SCROLL_FORWARD -> page + 1
                    android.view.accessibility.AccessibilityNodeInfo.ACTION_SCROLL_BACKWARD -> page - 1
                    else -> return super.performAccessibilityAction(host, action, args)
                }
                if (to !in 0 until count) return false
                choose(to)
                return true
            }
        })
    }

    private fun describe() {
        ViewCompat.setStateDescription(this, if (count > 0) "${page + 1}/$count" else null)
    }

    private fun choose(i: Int) {
        if (i == page) return
        page = i
        val p = i.toLong()
        rt.emit(id, RyUIEventKind.SELECTED) { RyUIEvents.selected(id, p) }
    }

    override fun onMeasure(widthSpec: Int, heightSpec: Int) {
        val w = Math.round(count * cell + cell)
        val h = Math.round(cell + cell) // 32dp: the dots, with room to tap
        setMeasuredDimension(resolveSize(w, widthSpec), resolveSize(h, heightSpec))
    }

    /** The dot under x, in the reading direction. */
    private fun dotAt(x: Float): Int {
        val start = (width - count * cell) / 2
        var i = ((x - start) / cell).toInt()
        if (layoutDirection == LAYOUT_DIRECTION_RTL) i = count - 1 - i
        return i.coerceIn(0, maxOf(count - 1, 0))
    }

    @SuppressLint("ClickableViewAccessibility")
    override fun onTouchEvent(e: MotionEvent): Boolean {
        if (count == 0 || !isEnabled) return false
        when (e.actionMasked) {
            MotionEvent.ACTION_DOWN -> return true
            MotionEvent.ACTION_UP -> {
                choose(dotAt(e.x))
                performClick()
            }
        }
        return true
    }

    override fun performClick(): Boolean = super.performClick()

    override fun onDraw(canvas: Canvas) {
        if (count == 0) return
        val on = MaterialColors.getColor(this, androidx.appcompat.R.attr.colorPrimary)
        val off = MaterialColors.getColor(this, com.google.android.material.R.attr.colorOnSurfaceVariant, Color.GRAY)
        val start = (width - count * cell) / 2 + cell / 2
        val cy = height / 2f
        val rtl = layoutDirection == LAYOUT_DIRECTION_RTL
        for (i in 0 until count) {
            val slot = if (rtl) count - 1 - i else i
            paint.color = if (i == page) on else (off and 0x00ffffff) or (0x66 shl 24)
            canvas.drawCircle(start + slot * cell, cy, dot / 2, paint)
        }
    }
}
