package dev.asuka

import android.annotation.SuppressLint
import android.content.res.ColorStateList
import android.view.Gravity
import android.view.MotionEvent
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import com.google.android.material.button.MaterialButton
import com.google.android.material.checkbox.MaterialCheckBox
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
 * the first until a full swipe runs it. Its leading actions are revealed
 * the same way from its start edge. The row comes back unless the core
 * removes it. A row without actions doesn't swipe.
 *
 * In a selecting list, a Material checkbox shows at its end, a tap
 * selects or deselects it, and a selected row is on the secondary
 * container color (Material's selected list item). A long press on a row
 * of a list that isn't selecting can begin selecting (RowSelectBegin).
 */
@SuppressLint("ViewConstructor")
class AsukaSwipeRow(private val rt: AsukaRuntime, val cell: AsukaCell) : FrameLayout(rt.themed) {
    private val content = Content()
    private val reveal = ListItemRevealLayout(rt.themed)
    private val leadingReveal = ListItemRevealLayout(rt.themed)
    private val layout = object : ListItemLayout(rt.themed) {
        // Without actions, the row leaves touches alone (ListItemLayout's
        // gesture detector would otherwise claim them).
        override fun onInterceptTouchEvent(ev: MotionEvent) = content.isSwipeEnabled && super.onInterceptTouchEvent(ev)
        override fun onTouchEvent(ev: MotionEvent) = content.isSwipeEnabled && super.onTouchEvent(ev)
    }
    private var actions: List<AsukaAction> = emptyList()
    private var leading: List<AsukaAction> = emptyList()
    private val check = MaterialCheckBox(rt.themed).apply {
        isClickable = false
        isFocusable = false
        importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO // the row is the checkbox for TalkBack
        visibility = View.GONE
    }
    private var selecting = false
    /** Selected, in a selecting list (View's own selected state is left alone). */
    private var chosenState = false
    // A long press to begin selecting: where it went down, and the wait.
    private var downX = 0f
    private var downY = 0f
    private var pressing = false
    private var downTime = 0L
    private val slop = android.view.ViewConfiguration.get(rt.themed).scaledTouchSlop
    private val longPress = Runnable {
        if (!pressing || selecting) return@Runnable
        pressing = false
        // The touch is the selection's now: what's in the row doesn't get it.
        val now = android.os.SystemClock.uptimeMillis()
        spentDown = downTime
        val cancel = MotionEvent.obtain(now, now, MotionEvent.ACTION_CANCEL, downX, downY, 0)
        super.dispatchTouchEvent(cancel)
        cancel.recycle()
        performHapticFeedback(android.view.HapticFeedbackConstants.LONG_PRESS)
        val id = cell.asukaID
        rt.emit(id, AsukaEventKind.ROW_SELECT_BEGIN) { AsukaEvents.rowSelectBegin(id) }
    }

    init {
        cell.row = this
        clipChildren = false // what overflows a row shows, as before
        layout.clipChildren = false
        content.clipChildren = false
        content.addView(cell, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT))
        layout.addView(content, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT))
        // The actions are the row's accessibility actions (swipeAccessibility).
        reveal.importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS
        layout.addView(reveal, LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.MATCH_PARENT, Gravity.END))
        leadingReveal.importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS
        layout.addView(leadingReveal, LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.MATCH_PARENT, Gravity.START))
        addView(layout, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT))
        val d = rt.density
        addView(check, LayoutParams(Math.round(48 * d), Math.round(48 * d), Gravity.END or Gravity.CENTER_VERTICAL).apply {
            marginEnd = Math.round((CHECK_WIDTH - 48) * d)
        })
        setOnClickListener {
            if (!selecting) return@setOnClickListener
            setChosen(!chosenState)
            val id = cell.asukaID
            val v = chosenState
            rt.emit(id, AsukaEventKind.ROW_SELECTED) { AsukaEvents.rowSelected(id, v) }
        }
        isClickable = false
        androidx.core.view.ViewCompat.setAccessibilityDelegate(this, object : androidx.core.view.AccessibilityDelegateCompat() {
            override fun onInitializeAccessibilityNodeInfo(host: View, info: androidx.core.view.accessibility.AccessibilityNodeInfoCompat) {
                super.onInitializeAccessibilityNodeInfo(host, info)
                if (!selecting) return
                info.className = android.widget.CheckBox::class.java.name
                info.isCheckable = true
                info.isChecked = chosenState
            }
        })
    }

    /**
     * Selecting: the checkbox at the end (the core lays the row out
     * narrower by its room), taps select, nothing in the row takes them,
     * and the row doesn't swipe.
     */
    fun setSelecting(on: Boolean) {
        if (on == selecting) return
        selecting = on
        if (on) close(animated = false)
        check.visibility = if (on) View.VISIBLE else View.GONE
        isClickable = on
        importantForAccessibility = if (on) View.IMPORTANT_FOR_ACCESSIBILITY_YES else View.IMPORTANT_FOR_ACCESSIBILITY_AUTO
        // What's in the row is read as the checkbox's label.
        cell.importantForAccessibility = if (on) View.IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS else View.IMPORTANT_FOR_ACCESSIBILITY_AUTO
        if (on) contentDescription = rowText() else contentDescription = null
        content.selected(on && chosenState)
    }

    /** The core's (or the user's) choice: checked, and the row highlighted. */
    fun setChosen(on: Boolean) {
        chosenState = on
        check.isChecked = on
        content.selected(selecting && on)
        if (selecting) contentDescription = rowText()
    }

    private fun rowText(): CharSequence {
        val out = StringBuilder()
        fun walk(v: View) {
            if (v is android.widget.TextView && v.text.isNotEmpty()) {
                if (out.isNotEmpty()) out.append(", ")
                out.append(v.text)
            } else v.contentDescription?.let { if (it.isNotEmpty()) { if (out.isNotEmpty()) out.append(", "); out.append(it) } }
            if (v is ViewGroup) for (i in 0 until v.childCount) walk(v.getChildAt(i))
        }
        walk(cell)
        return out
    }

    override fun onInterceptTouchEvent(ev: MotionEvent): Boolean = selecting || super.onInterceptTouchEvent(ev)

    /** Closes the row (a recycled row, a swipe done). */
    fun close(animated: Boolean) {
        if (layout.swipeState != SwipeableListItem.STATE_CLOSED) layout.setSwipeState(SwipeableListItem.STATE_CLOSED, openGravity, animated)
    }

    /** The side the row is open on. */
    private var openGravity = Gravity.END

    override fun dispatchTouchEvent(ev: MotionEvent): Boolean {
        // A touch on a closed row: its actions as they are now.
        if (ev.actionMasked == MotionEvent.ACTION_DOWN && layout.swipeState == SwipeableListItem.STATE_CLOSED) {
            val n = rt.nodes[cell.asukaID]
            val next = if (selecting) emptyList() else n?.swipeActions ?: emptyList()
            val lead = if (selecting) emptyList() else n?.leadingSwipeActions ?: emptyList()
            if (next != actions || reveal.childCount != next.size || lead != leading || leadingReveal.childCount != lead.size) setActions(next, lead)
        }
        // A long press begins selecting, if the core asks to hear of it.
        when (ev.actionMasked) {
            MotionEvent.ACTION_DOWN -> if (!selecting && rt.nodes[cell.asukaID]?.listens(AsukaEventKind.ROW_SELECT_BEGIN) == true) {
                pressing = true
                downX = ev.x
                downY = ev.y
                downTime = ev.downTime
                postDelayed(longPress, android.view.ViewConfiguration.getLongPressTimeout().toLong())
            }
            MotionEvent.ACTION_MOVE -> if (pressing && (Math.abs(ev.x - downX) > slop || Math.abs(ev.y - downY) > slop)) {
                pressing = false
                removeCallbacks(longPress)
            }
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                pressing = false
                removeCallbacks(longPress)
            }
        }
        if (!pressing && ev.actionMasked != MotionEvent.ACTION_DOWN && wasLongPressed(ev)) return true
        return super.dispatchTouchEvent(ev)
    }

    // After a long press began selecting, the rest of that touch is spent.
    private var spentDown = -1L
    private fun wasLongPressed(ev: MotionEvent): Boolean {
        if (ev.downTime != spentDown) return false
        if (ev.actionMasked == MotionEvent.ACTION_UP || ev.actionMasked == MotionEvent.ACTION_CANCEL) spentDown = -1L
        return true
    }

    /**
     * ListItemLayout keeps the list from scrolling once a touch moves at
     * all, even mostly up or down; only a swipe that has started may.
     */
    override fun requestDisallowInterceptTouchEvent(disallow: Boolean) {
        if (disallow && layout.swipeState != SwipeableListItem.STATE_DRAGGING) return
        super.requestDisallowInterceptTouchEvent(disallow)
    }

    private fun setActions(next: List<AsukaAction>, lead: List<AsukaAction>) {
        actions = next
        leading = lead
        content.isSwipeEnabled = next.isNotEmpty() || lead.isNotEmpty()
        reveal.removeAllViews()
        // One action is the full swipe's; with more, a swipe stops at them first.
        reveal.primaryActionSwipeMode = if (next.size == 1) RevealableListItem.PRIMARY_ACTION_SWIPE_DIRECT
            else RevealableListItem.PRIMARY_ACTION_SWIPE_INDIRECT
        // The last child grows on a full swipe: the first action, at the end.
        for (i in next.indices.reversed()) reveal.addView(button(reveal, i.toLong(), next[i]))
        reveal.resetIntrinsicWidth()
        leadingReveal.removeAllViews()
        leadingReveal.primaryActionSwipeMode = if (lead.size == 1) RevealableListItem.PRIMARY_ACTION_SWIPE_DIRECT
            else RevealableListItem.PRIMARY_ACTION_SWIPE_INDIRECT
        // From the start edge, the first child grows: the first action, at the start.
        for (i in lead.indices) leadingReveal.addView(button(leadingReveal, -1L - i, lead[i]))
        leadingReveal.resetIntrinsicWidth()
    }

    private fun button(parent: ViewGroup, i: Long, a: AsukaAction): MaterialButton {
        val ctx = parent.context // with the reveal layout's theme overlay
        val destructive = a.role == AsukaAlertButtonRole.DESTRUCTIVE
        val icon = rt.symbol(a.icon.ifEmpty { if (destructive) "trash" else "" })
        // A leading action is filled in the accent (Gmail's archive), a trailing one tonal.
        val style = if (destructive || i < 0) com.google.android.material.R.attr.materialIconButtonFilledStyle
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

    private fun run(i: Long) {
        val id = cell.asukaID
        rt.emit(id, AsukaEventKind.ACTION) { AsukaEvents.action(id, i) }
        // Back, if the action didn't remove it (a removal is diffed in).
        post { if (isAttachedToWindow) close(animated = true) }
    }

    /**
     * The row's place in a segmented list (position of count; NO_POSITION
     * in a plain list): its container is rounded as the group's ends are.
     */
    fun segment(position: Int, count: Int) {
        content.place(when {
            position < 0 || count <= 0 -> NONE
            count == 1 -> ListItemLayout.POSITION_SINGLE
            position == 0 -> ListItemLayout.POSITION_FIRST
            position == count - 1 -> ListItemLayout.POSITION_LAST
            else -> ListItemLayout.POSITION_MIDDLE
        })
    }

    /** Takes the theme's colors again (after a dark mode change). */
    fun restyle() = content.restyle()

    /**
     * The row's content: what slides. In a segmented list it's always on
     * its container; in a plain list, only while it's out.
     */
    private inner class Content : FrameLayout(rt.themed), SwipeableListItem {
        private var enabled = false
        private var swiped = false
        private var place = NONE
        private var color = 0 // the container's, as drawn
        private var chosen = false // selected, in a selecting list

        /** Selected: on the secondary container color. */
        fun selected(on: Boolean) {
            if (on == chosen) return
            chosen = on
            restyle()
        }

        /** Its place in the group; the container again if that or the theme's color changed. */
        fun place(p: Int) {
            if (p == place && (p == NONE || surface() == color)) return
            place = p
            restyle()
        }

        override fun getSwipeMaxOvershoot() = Math.round(16 * rt.density)
        override fun isSwipeEnabled() = enabled
        override fun setSwipeEnabled(swipeEnabled: Boolean) { enabled = swipeEnabled }
        override fun onSwipe(swipeOffset: Int) {}

        override fun <T> onSwipeStateChanged(newState: Int, revealableListItem: T, revealGravity: Int) where T : View, T : RevealableListItem {
            val out = newState != SwipeableListItem.STATE_CLOSED
            if (out != swiped) {
                swiped = out
                restyle()
            }
            if (newState != SwipeableListItem.STATE_CLOSED) openGravity = revealGravity
            // The primary action: the first at whichever edge was swiped from.
            if (newState == SwipeableListItem.STATE_SWIPE_PRIMARY_ACTION) run(if (revealableListItem === leadingReveal) -1L else 0L)
        }

        private fun surface() = MaterialColors.getColor(context,
            if (chosen) com.google.android.material.R.attr.colorSecondaryContainer else com.google.android.material.R.attr.colorSurface, 0)

        fun restyle() {
            color = surface()
            background = when {
                place != NONE -> container(when (place) {
                    ListItemLayout.POSITION_SINGLE -> com.google.android.material.R.attr.listItemShapeAppearanceSingle
                    ListItemLayout.POSITION_FIRST -> com.google.android.material.R.attr.listItemShapeAppearanceFirst
                    ListItemLayout.POSITION_LAST -> com.google.android.material.R.attr.listItemShapeAppearanceLast
                    else -> com.google.android.material.R.attr.listItemShapeAppearanceMiddle
                })
                swiped || chosen -> container(com.google.android.material.R.attr.listItemShapeAppearanceMiddle)
                else -> null
            }
        }

        /** A list item's container, shaped by the theme's attr (Material's list item tokens). */
        private fun container(shape: Int): MaterialShapeDrawable {
            val tv = android.util.TypedValue()
            val model = if (context.theme.resolveAttribute(shape, tv, true) && tv.resourceId != 0)
                ShapeAppearanceModel.builder(context, tv.resourceId, 0).build()
            else ShapeAppearanceModel.builder().setAllCornerSizes(4 * rt.density).build()
            return MaterialShapeDrawable(model).apply {
                fillColor = ColorStateList.valueOf(color)
            }
        }
    }

    companion object {
        private const val NONE = -1
        /** The room the checkbox takes at a selecting row's end, in dp: a 48dp target, its box 16dp from the edge (Material's list item end padding). */
        const val CHECK_WIDTH = 52f
    }
}
