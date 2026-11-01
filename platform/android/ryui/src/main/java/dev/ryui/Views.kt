package dev.ryui

import android.content.Context
import android.view.MotionEvent
import android.view.View
import android.view.ViewConfiguration
import android.view.ViewGroup
import android.view.accessibility.AccessibilityNodeInfo
import android.widget.FrameLayout

/** A native view the core made, or a list cell the host made. */
class RyUINode(val id: Int, val kind: Int, var view: View) {
    val children = ArrayList<RyUINode>()
    var parent: RyUINode? = null
    /** A bit per event kind the core listens for (kinds are u16: any number of words). */
    private var listenBits = LongArray(1)

    var textStyle = 0
    var fontSize = 0f
    var fontWeight = 0
    var fontFamily = ""
    var color = 0L
    var background = 0L
    var cornerRadius = 0f
    var borderWidth = 0f
    var borderColor = 0L
    var list: RyUIList? = null
    /** The last value of each prop, to make the view again (see RyUIRuntime.rebuild). */
    val props = HashMap<Int, Any>()

    /** A view around this one (a SwipeRefreshLayout); the parent holds it. */
    var wrapper: View? = null
    /** The view the parent holds and frames. */
    val outer: View get() = wrapper ?: view

    /** Being popped or dismissed: its views leave with it, animated. */
    var dying = false
    val underDying: Boolean
        get() {
            var p = parent
            while (p != null) {
                if (p.dying) return true
                p = p.parent
            }
            return false
        }

    /** A menu, an alert's buttons. */
    var actions: List<RyUIAction> = emptyList()
    var swipeActions: List<RyUIAction> = emptyList() // a list cell's
    var swipeA11y: Pair<View, List<Int>>? = null // the TalkBack actions made from them: the view, their IDs
    var menuTrigger = RyUIMenuTrigger.TAP
    var dialog: android.app.Dialog? = null
    var alertTitle = ""
    var alertMessage = ""
    var alertStyle = RyUIAlertStyle.ALERT
    var detents = 0L
    var grabber = false
    var imageSource: String? = null
    val a11yActions = ArrayList<Int>() // TalkBack actions' IDs (A11yActions)
    var imageLoaded = false
    var imagePlaceholder = ""
    var gestures: RyUIGestures? = null
    var frameAnimator: android.animation.Animator? = null
    var backgroundAnimator: android.animation.Animator? = null
    var gradient: RyUIGradient? = null
    var material = 0 // Style.Material
    var transition: RyUITransition? = null
    var match: String? = null // Style.Match's key
    var targetX = Int.MIN_VALUE // the frame's place set last (px), which an animation may still be on its way to
    var targetY = 0
    var frame: FloatArray? = null // the frame set last, in points: applied again if the density changes
    val base = RyUIBase()

    fun listens(event: Int): Boolean {
        val w = event ushr 6
        return w < listenBits.size && listenBits[w] and (1L shl (event and 63)) != 0L
    }

    fun setListening(event: Int, on: Boolean) {
        val w = event ushr 6
        if (w >= listenBits.size) {
            if (!on) return
            listenBits = listenBits.copyOf(w + 1)
        }
        listenBits[w] = if (on) listenBits[w] or (1L shl (event and 63)) else listenBits[w] and (1L shl (event and 63)).inv()
    }

    /** The ripple foreground wirePresses set, if it's on. */
    var ripple: android.graphics.drawable.Drawable? = null
    /** DropAccept: the MIME type patterns it takes drops of. */
    var dropAccept: List<String> = emptyList()
    /** DragItems: what dragging it carries. */
    var dragItems: List<RyUIDragItem> = emptyList()

    /** Where children's views go. */
    val container: ViewGroup? get() = view as? ViewGroup

    fun insert(child: RyUINode, index: Int) {
        val i = minOf(index, children.size)
        children.add(i, child)
        child.parent = this
        val c = container ?: return
        val lp = if (c is RyUIContainer) RyUIParams() else FrameLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT)
        val v = child.outer
        (v.parent as? ViewGroup)?.removeView(v)
        // Among the views there: a Scroll takes rows out of sight out.
        c.addView(v, if (c is RyUIContainer) children.subList(0, i).count { it.outer.parent === c } else -1, lp)
    }

    fun remove(child: RyUINode) {
        children.remove(child)
        child.parent = null
        (child.outer.parent as? ViewGroup)?.removeView(child.outer)
    }
}

/**
 * A child's frame in its RyUIContainer, in pixels. The standard width and
 * height stay WRAP_CONTENT: a TextView whose width looks fixed re-wraps
 * changed text within its old width and then measures that.
 */
class RyUIParams : ViewGroup.LayoutParams(WRAP_CONTENT, WRAP_CONTENT) {
    var x = 0
    var y = 0
    var w = 0
    var h = 0
}

/**
 * A container: the core positions its children, so measuring and laying
 * out just apply their frames. Its own size is its frame.
 */
open class RyUIContainer(context: Context) : ViewGroup(context) {
    var frameWidth = -1
    var frameHeight = -1

    // What overflows shows, as on iOS (and React Native's views): only
    // Style.Clip clips.
    init { clipChildren = false }

    /** Reports its pressed state as it changes (a View listening for Pressed). */
    var onPressedChange: ((Boolean) -> Unit)? = null
        set(f) {
            field = f
            if (f == null) reportedPressed = false
        }
    /** Reports a pointer coming over it and leaving it, its children included (a View listening for Hover). */
    var onHoverChange: ((Boolean) -> Unit)? = null
        set(f) {
            field = f
            if (f == null) hovered = false
        }
    /** Pressable only to report Pressed: not an action for TalkBack. */
    var pressOnly = false
    private var reportedPressed = false
    private var hovered = false
    // The touch is down and still the view's (no long press or drag took
    // it), and whether it moved off the view, which unpresses it.
    private var tracking = false
    private var movedOff = false
    private val slop = ViewConfiguration.get(context).scaledTouchSlop

    override fun setPressed(pressed: Boolean) {
        super.setPressed(pressed)
        val f = onPressedChange ?: return
        // ViewGroup.dispatchSetPressed passes an ancestor's to views that
        // aren't clickable; a view listening for Pressed always is.
        val now = isPressed
        if (now != reportedPressed) {
            reportedPressed = now
            f(now)
        }
    }

    /** A long press or a drag took the touch: unpressed at once, and not again until the next touch. */
    fun cancelPress() {
        tracking = false
        isPressed = false
        jumpDrawablesToCurrentState()
    }

    override fun onTouchEvent(e: MotionEvent): Boolean {
        val handled = super.onTouchEvent(e)
        if (onPressedChange == null || !isEnabled || !isClickable) return handled
        when (e.actionMasked) {
            MotionEvent.ACTION_DOWN -> { tracking = true; movedOff = false }
            MotionEvent.ACTION_MOVE -> if (tracking) {
                val s = slop.toFloat()
                val inside = e.x >= -s && e.y >= -s && e.x < width + s && e.y < height + s
                if (!inside) movedOff = true
                else if (movedOff && !isPressed) {
                    // Back on the view while still down: pressed again (as
                    // UIControl's tracking), so a release here fires Press.
                    movedOff = false
                    drawableHotspotChanged(e.x, e.y)
                    isPressed = true
                }
            }
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> tracking = false
        }
        return handled
    }

    override fun dispatchHoverEvent(e: MotionEvent): Boolean {
        val handled = super.dispatchHoverEvent(e)
        val f = onHoverChange ?: return handled
        // A finger's are TalkBack exploring by touch, not a pointer.
        if (e.getToolType(0) == MotionEvent.TOOL_TYPE_FINGER) return handled
        val on = when (e.actionMasked) {
            MotionEvent.ACTION_HOVER_ENTER, MotionEvent.ACTION_HOVER_MOVE -> true
            MotionEvent.ACTION_HOVER_EXIT -> false
            else -> return true
        }
        if (on != hovered) {
            hovered = on
            f(on)
        }
        // Handled: the parent keeps it as a hover target and sends it the
        // exit as the pointer leaves its bounds, over its children or not.
        return true
    }

    override fun onDetachedFromWindow() {
        super.onDetachedFromWindow()
        tracking = false
        if (reportedPressed) {
            reportedPressed = false
            onPressedChange?.invoke(false)
        }
        if (hovered) {
            hovered = false
            onHoverChange?.invoke(false)
        }
    }

    override fun onInitializeAccessibilityNodeInfo(info: AccessibilityNodeInfo) {
        super.onInitializeAccessibilityNodeInfo(info)
        if (pressOnly) {
            info.isClickable = false
            info.removeAction(AccessibilityNodeInfo.AccessibilityAction.ACTION_CLICK)
        }
    }

    override fun generateDefaultLayoutParams(): LayoutParams = RyUIParams()
    override fun checkLayoutParams(p: LayoutParams?) = p is RyUIParams
    override fun shouldDelayChildPressedState() = false

    override fun onMeasure(widthSpec: Int, heightSpec: Int) {
        val w = if (frameWidth >= 0) frameWidth else MeasureSpec.getSize(widthSpec)
        val h = if (frameHeight >= 0) frameHeight else MeasureSpec.getSize(heightSpec)
        setMeasuredDimension(w, h)
        measureChildren()
    }

    protected fun measureChildren() {
        for (i in 0 until childCount) {
            val c = getChildAt(i)
            val lp = c.layoutParams as? RyUIParams ?: continue
            c.measure(MeasureSpec.makeMeasureSpec(lp.w, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(lp.h, MeasureSpec.EXACTLY))
        }
    }

    override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
        for (i in 0 until childCount) {
            val c = getChildAt(i)
            val lp = c.layoutParams as? RyUIParams ?: continue
            c.layout(lp.x, lp.y, lp.x + lp.w, lp.y + lp.h)
        }
    }

    /** Takes v out (a Scroll's row out of sight), without asking for a layout: its frame is the core's. */
    fun takeOut(v: View) = removeViewInLayout(v)

    /** Puts v back at index, as it was. */
    fun putBack(v: View, index: Int) {
        addViewInLayout(v, index, v.layoutParams ?: RyUIParams(), true)
        invalidate()
    }
}

/** A list cell: its height is the row's, from the core. */
class RyUICell(context: Context) : RyUIContainer(context) {
    var ryuiID = 0
    var rowHeight = 0
    var binding = false // RecyclerView measures it right after binding

    override fun onMeasure(widthSpec: Int, heightSpec: Int) {
        setMeasuredDimension(MeasureSpec.getSize(widthSpec), rowHeight)
        measureChildren()
    }
}
