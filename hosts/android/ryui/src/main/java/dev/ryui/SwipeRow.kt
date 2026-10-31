package dev.ryui

import android.annotation.SuppressLint
import android.content.res.ColorStateList
import android.view.Gravity
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import com.google.android.material.button.MaterialButton
import com.google.android.material.color.MaterialColors
import com.google.android.material.listitem.ListItemLayout
import com.google.android.material.listitem.ListItemRevealLayout
import com.google.android.material.listitem.RevealableListItem
import com.google.android.material.listitem.SwipeableListItem
import com.google.android.material.shape.MaterialShapeDrawable
import com.google.android.material.shape.ShapeAppearanceModel

/**
 * A List row, with its swipe actions as Material 3 Expressive's swipe
 * to reveal (ListItemLayout): the row slides toward its start over its
 * actions, the first at the end edge, as on iOS; swiping further grows
 * the first until a full swipe runs it. The row comes back unless the
 * core removes it. A row without actions doesn't swipe.
 */
@SuppressLint("ViewConstructor")
class RyUISwipeRow(private val rt: RyUIRuntime, val cell: RyUICell) : FrameLayout(rt.themed) {
    private val content = Content()
    private val reveal = ListItemRevealLayout(rt.themed)
    private val layout = object : ListItemLayout(rt.themed) {
        // Without actions, the row leaves touches alone (ListItemLayout's
        // gesture detector would otherwise claim them).
        override fun onInterceptTouchEvent(ev: MotionEvent) = content.isSwipeEnabled && super.onInterceptTouchEvent(ev)
        override fun onTouchEvent(ev: MotionEvent) = content.isSwipeEnabled && super.onTouchEvent(ev)
    }
    private var actions: List<RyUIAction> = emptyList()

    init {
        clipChildren = false // what overflows a row shows, as before
        layout.clipChildren = false
        content.clipChildren = false
        content.addView(cell, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT))
        layout.addView(content, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT))
        // The actions are the row's accessibility actions (swipeAccessibility).
        reveal.importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS
        layout.addView(reveal, LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.MATCH_PARENT, Gravity.END))
        addView(layout, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT))
    }

    /** Closes the row (a recycled row, a swipe done). */
    fun close(animated: Boolean) {
        if (layout.swipeState != SwipeableListItem.STATE_CLOSED) layout.setSwipeState(SwipeableListItem.STATE_CLOSED, Gravity.END, animated)
    }

    override fun dispatchTouchEvent(ev: MotionEvent): Boolean {
        // A touch on a closed row: its actions as they are now.
        if (ev.actionMasked == MotionEvent.ACTION_DOWN && layout.swipeState == SwipeableListItem.STATE_CLOSED) {
            val next = rt.nodes[cell.ryuiID]?.swipeActions ?: emptyList()
            if (next != actions || reveal.childCount != next.size) setActions(next)
        }
        return super.dispatchTouchEvent(ev)
    }

    /**
     * ListItemLayout keeps the list from scrolling once a touch moves at
     * all, even mostly up or down; only a swipe that has started may.
     */
    override fun requestDisallowInterceptTouchEvent(disallow: Boolean) {
        if (disallow && layout.swipeState != SwipeableListItem.STATE_DRAGGING) return
        super.requestDisallowInterceptTouchEvent(disallow)
    }

    private fun setActions(next: List<RyUIAction>) {
        actions = next
        content.isSwipeEnabled = next.isNotEmpty()
        reveal.removeAllViews()
        // One action is the full swipe's; with more, a swipe stops at them first.
        reveal.primaryActionSwipeMode = if (next.size == 1) RevealableListItem.PRIMARY_ACTION_SWIPE_DIRECT
            else RevealableListItem.PRIMARY_ACTION_SWIPE_INDIRECT
        // The last child grows on a full swipe: the first action, at the end.
        for (i in next.indices.reversed()) reveal.addView(button(i, next[i]))
        reveal.resetIntrinsicWidth()
    }

    private fun button(i: Int, a: RyUIAction): MaterialButton {
        val ctx = reveal.context // with the reveal layout's theme overlay
        val destructive = a.role == RyUIAlertButtonRole.DESTRUCTIVE
        val icon = rt.symbol(a.icon.ifEmpty { if (destructive) "trash" else "" })
        val style = if (destructive) com.google.android.material.R.attr.materialIconButtonFilledStyle
            else com.google.android.material.R.attr.materialIconButtonFilledTonalStyle
        val d = rt.density
        return MaterialButton(ctx, null, style).apply {
            if (icon != null) this.icon = icon else text = a.title
            if (destructive) {
                backgroundTintList = ColorStateList.valueOf(MaterialColors.getColor(ctx, androidx.appcompat.R.attr.colorError, 0))
                val on = ColorStateList.valueOf(MaterialColors.getColor(ctx, com.google.android.material.R.attr.colorOnError, 0))
                iconTint = on
                setTextColor(on)
            }
            layoutParams = ViewGroup.MarginLayoutParams(if (icon != null) Math.round(64 * d) else ViewGroup.LayoutParams.WRAP_CONTENT,
                ViewGroup.LayoutParams.MATCH_PARENT).apply {
                marginStart = Math.round(2 * d)
                marginEnd = Math.round(2 * d)
            }
            setOnClickListener { run(i) }
        }
    }

    private fun run(i: Int) {
        val id = cell.ryuiID
        rt.emit(id, RyUIEventKind.ACTION) { RyUIEvents.action(id, i.toLong()) }
        // Back, if the action didn't remove it (a removal is diffed in).
        post { if (isAttachedToWindow) close(animated = true) }
    }

    /** The row's content: what slides, over the surface while it's out. */
    private inner class Content : FrameLayout(rt.themed), SwipeableListItem {
        private var enabled = false

        override fun getSwipeMaxOvershoot() = Math.round(16 * rt.density)
        override fun isSwipeEnabled() = enabled
        override fun setSwipeEnabled(swipeEnabled: Boolean) { enabled = swipeEnabled }
        override fun onSwipe(swipeOffset: Int) {}

        override fun <T> onSwipeStateChanged(newState: Int, revealableListItem: T, revealGravity: Int) where T : View, T : RevealableListItem {
            background = if (newState == SwipeableListItem.STATE_CLOSED) null else surface()
            if (newState == SwipeableListItem.STATE_SWIPE_PRIMARY_ACTION) run(0)
        }

        /** A swiped list item's container (Material's list item tokens). */
        private fun surface(): MaterialShapeDrawable {
            val tv = android.util.TypedValue()
            val model = if (context.theme.resolveAttribute(com.google.android.material.R.attr.shapeAppearanceCornerExtraSmall, tv, true))
                ShapeAppearanceModel.builder(context, tv.resourceId, 0).build()
            else ShapeAppearanceModel.builder().setAllCornerSizes(4 * rt.density).build()
            return MaterialShapeDrawable(model).apply {
                fillColor = ColorStateList.valueOf(MaterialColors.getColor(context, com.google.android.material.R.attr.colorSurface, 0))
            }
        }
    }
}
