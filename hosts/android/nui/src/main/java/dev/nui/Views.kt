package dev.nui

import android.content.Context
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout

/** A native view the core made, or a list cell the host made. */
class NuiNode(val id: Int, val kind: Int, var view: View) {
    val children = ArrayList<NuiNode>()
    var parent: NuiNode? = null
    var listens = 0 // bit per event kind

    var textStyle = 0
    var fontSize = 0f
    var fontWeight = 0
    var fontFamily = ""
    var color = 0L
    var background = 0L
    var cornerRadius = 0f
    var borderWidth = 0f
    var borderColor = 0L
    var list: NuiList? = null
    /** The last value of each prop, to make the view again (see NuiRuntime.rebuild). */
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
    var actions: List<NuiAction> = emptyList()
    var swipeActions: List<NuiAction> = emptyList() // a list cell's
    var swipeA11y: Pair<View, List<Int>>? = null // the TalkBack actions made from them: the view, their IDs
    var menuTrigger = NuiMenuTrigger.TAP
    var dialog: android.app.Dialog? = null
    var alertTitle = ""
    var alertMessage = ""
    var alertStyle = NuiAlertStyle.ALERT
    var detents = 0L
    var grabber = false
    var imageSource: String? = null
    val a11yActions = ArrayList<Int>() // TalkBack actions' IDs (A11yActions)
    var imageLoaded = false
    var imagePlaceholder = ""
    var gestures: NuiGestures? = null
    var frameAnimator: android.animation.Animator? = null
    var backgroundAnimator: android.animation.Animator? = null
    var gradient: NuiGradient? = null
    var material = 0 // Style.Material
    var transition: NuiTransition? = null
    val base = NuiBase()

    fun listens(event: Int) = listens and (1 shl event) != 0

    /** Where children's views go. */
    val container: ViewGroup? get() = view as? ViewGroup

    fun insert(child: NuiNode, index: Int) {
        val i = minOf(index, children.size)
        children.add(i, child)
        child.parent = this
        val c = container ?: return
        val lp = if (c is NuiContainer) NuiParams() else FrameLayout.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT)
        val v = child.outer
        (v.parent as? ViewGroup)?.removeView(v)
        // Among the views there: a Scroll takes rows out of sight out.
        c.addView(v, if (c is NuiContainer) children.subList(0, i).count { it.outer.parent === c } else -1, lp)
    }

    fun remove(child: NuiNode) {
        children.remove(child)
        child.parent = null
        (child.outer.parent as? ViewGroup)?.removeView(child.outer)
    }
}

/**
 * A child's frame in its NuiContainer, in pixels. The standard width and
 * height stay WRAP_CONTENT: a TextView whose width looks fixed re-wraps
 * changed text within its old width and then measures that.
 */
class NuiParams : ViewGroup.LayoutParams(WRAP_CONTENT, WRAP_CONTENT) {
    var x = 0
    var y = 0
    var w = 0
    var h = 0
}

/**
 * A container: the core positions its children, so measuring and laying
 * out just apply their frames. Its own size is its frame.
 */
open class NuiContainer(context: Context) : ViewGroup(context) {
    var frameWidth = -1
    var frameHeight = -1

    override fun generateDefaultLayoutParams(): LayoutParams = NuiParams()
    override fun checkLayoutParams(p: LayoutParams?) = p is NuiParams
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
            val lp = c.layoutParams as? NuiParams ?: continue
            c.measure(MeasureSpec.makeMeasureSpec(lp.w, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(lp.h, MeasureSpec.EXACTLY))
        }
    }

    override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
        for (i in 0 until childCount) {
            val c = getChildAt(i)
            val lp = c.layoutParams as? NuiParams ?: continue
            c.layout(lp.x, lp.y, lp.x + lp.w, lp.y + lp.h)
        }
    }

    /** Takes v out (a Scroll's row out of sight), without asking for a layout: its frame is the core's. */
    fun takeOut(v: View) = removeViewInLayout(v)

    /** Puts v back at index, as it was. */
    fun putBack(v: View, index: Int) {
        addViewInLayout(v, index, v.layoutParams ?: NuiParams(), true)
        invalidate()
    }
}

/** A list cell: its height is the row's, from the core. */
class NuiCell(context: Context) : NuiContainer(context) {
    var nuiID = 0
    var rowHeight = 0
    var binding = false // RecyclerView measures it right after binding

    override fun onMeasure(widthSpec: Int, heightSpec: Int) {
        setMeasuredDimension(MeasureSpec.getSize(widthSpec), rowHeight)
        measureChildren()
    }
}
