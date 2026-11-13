package dev.asuka

import android.graphics.Color
import android.view.MenuItem
import android.view.View
import android.view.ViewGroup
import android.view.ViewTreeObserver
import android.widget.FrameLayout
import android.widget.PopupMenu
import androidx.core.view.ViewCompat
import androidx.recyclerview.widget.RecyclerView
import com.google.android.material.bottomappbar.BottomAppBar
import com.google.android.material.color.MaterialColors
import com.google.android.material.floatingactionbutton.ExtendedFloatingActionButton
import com.google.android.material.floatingactionbutton.FloatingActionButton

/** A screen's Toolbar's Action events: TOOLBAR_BASE + the item's number (ops.ScreenToolbarActions). */
const val TOOLBAR_BASE = 1L shl 32
/** A screen's Primary's Action events: PRIMARY_BASE + the item's number (ops.ScreenPrimaryAction). */
const val PRIMARY_BASE = 2L shl 32

/**
 * A screen's bottom app bar and floating action button (ScreenOptions.Toolbar
 * and Primary), as Material 3's Scaffold has them: the Toolbar's actions are
 * icon buttons in a BottomAppBar at the bottom of the screen, those that
 * don't fit in its overflow menu; the Primary action is a
 * FloatingActionButton at the bottom end, docked in the bar when there is
 * one, or with PrimaryExtended an ExtendedFloatingActionButton (over the
 * bar), which shrinks to its icon as the screen's content scrolls down and
 * extends as it scrolls up. Both are the screen's own views, so they go with
 * it in a push, a pop and predictive back. The bar takes the navigation bar
 * under it into its height, and the screen's safe area takes the bar.
 */
class AsukaScreenBar(private val screen: AsukaScreen, private val rt: AsukaRuntime) {
    var toolbar: List<AsukaAction> = emptyList()
    var primary: AsukaAction? = null
    var extended = false

    // The bar is in a frame of its own: a BottomAppBar lets its parent draw
    // outside itself (for a cradled FAB), and a screen clips.
    private var holder: FrameLayout? = null
    private var bar: BottomAppBar? = null
    private var fab: FloatingActionButton? = null
    private var efab: ExtendedFloatingActionButton? = null
    private var scrolls: ScrollWatch? = null
    /** How many of the toolbar's buttons show in the bar, the others in its overflow; -1: not worked out. */
    private var fitted = -1
    private var fitWidth = 0

    private val d get() = rt.density
    private fun dp(v: Int) = Math.round(v * d)

    /** The bar's height over the screen's bottom, in px (0: none). */
    val barHeight: Int get() = holder?.takeIf { it.visibility == View.VISIBLE }?.measuredHeight ?: 0

    /** Shows the toolbar and the primary action as they are now. */
    fun refresh() {
        refreshBar()
        refreshFab()
        screen.requestLayout()
        screen.coveringTabs()
    }

    /** Takes the theme's colors: made again. */
    fun restyle() {
        holder?.let { screen.removeView(it) }
        holder = null
        bar = null
        dropFab()
        refresh()
    }

    private fun refreshBar() {
        if (toolbar.isEmpty()) {
            holder?.visibility = View.GONE
            return
        }
        val b = bar ?: makeBar().also { bar = it }
        holder?.visibility = View.VISIBLE
        val id = screen.nodeID
        b.menu.clear()
        for ((i, a) in toolbar.withIndex()) {
            // A button with children opens them as a menu.
            val item = if (a.submenu) {
                val sub = b.menu.addSubMenu(0, a.index.toInt(), i, rt.styled(a))
                rt.fillMenu(sub, a.items, 1000 * (i + 1))
                sub.item
            } else b.menu.add(0, a.index.toInt(), i, rt.styled(a))
            item.setShowAsAction(MenuItem.SHOW_AS_ACTION_ALWAYS)
            item.isEnabled = !a.disabled
            // An icon stands for the title, which TalkBack reads (and a long press shows).
            if (a.icon.isNotEmpty()) item.icon = rt.symbol(a.icon)?.apply {
                setTint(MaterialColors.getColor(rt.themed, com.google.android.material.R.attr.colorOnSurfaceVariant, Color.DKGRAY))
                if (a.disabled) alpha = 97 // Material's disabled content: 38%
            }
        }
        fitted = -1 // what fits is worked out as it's measured
        b.setOnMenuItemClickListener { item ->
            if (item.hasSubMenu() || item.itemId < 0) return@setOnMenuItemClickListener false // it opens, or is a header
            val i = TOOLBAR_BASE + item.itemId
            rt.emit(id, AsukaEventKind.ACTION) { AsukaEvents.action(id, i) }
            true
        }
    }

    private fun makeBar(): BottomAppBar {
        val b = BottomAppBar(rt.themed)
        // The icons at the start, the FAB (if any) at the end, as Material 3 has them.
        b.menuAlignmentMode = BottomAppBar.MENU_ALIGNMENT_MODE_START
        b.fabAlignmentMode = BottomAppBar.FAB_ALIGNMENT_MODE_END
        // The navigation bar under it is padded for in measure, as the screen sees it.
        ViewCompat.setOnApplyWindowInsetsListener(b) { _, insets -> insets }
        val h = FrameLayout(rt.themed)
        h.addView(b, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        // Over the content (its children come before), under the FAB.
        screen.addView(h, ViewGroup.LayoutParams(0, 0))
        holder = h
        fab?.bringToFront()
        efab?.bringToFront()
        return b
    }

    private fun dropFab() {
        fab?.let { screen.removeView(it) }
        efab?.let { screen.removeView(it) }
        fab = null
        efab = null
        scrolls?.stop()
        scrolls = null
    }

    private fun refreshFab() {
        val p = primary
        if (p == null) {
            dropFab()
            return
        }
        // Extended: the title beside the icon, as asked, or when there's no icon to show alone.
        val wantExtended = extended || p.icon.isEmpty()
        if (wantExtended && fab != null || !wantExtended && efab != null) dropFab()
        val id = screen.nodeID
        val v: View = if (wantExtended) {
            val e = efab ?: ExtendedFloatingActionButton(rt.themed).also {
                efab = it
                screen.addView(it, ViewGroup.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT))
            }
            e.text = p.title
            e.icon = rt.symbol(p.icon)
            e.isEnabled = !p.disabled
            if (p.icon.isNotEmpty() && extended) {
                if (scrolls == null) scrolls = ScrollWatch(screen, e).also { if (screen.isAttachedToWindow) it.start() }
            } else {
                scrolls?.stop()
                scrolls = null
                if (!e.isExtended) e.extend()
            }
            e
        } else {
            val f = fab ?: FloatingActionButton(rt.themed).also {
                fab = it
                screen.addView(it, ViewGroup.LayoutParams(ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT))
            }
            f.setImageDrawable(rt.symbol(p.icon))
            f.isEnabled = !p.disabled
            f
        }
        // TalkBack says the title, for an icon alone too.
        v.contentDescription = p.title
        androidx.appcompat.widget.TooltipCompat.setTooltipText(v, p.title)
        v.setOnClickListener {
            val cur = primary ?: return@setOnClickListener
            if (cur.submenu) {
                // With children, it opens them as a menu.
                val act = rt.activity ?: return@setOnClickListener
                val m = PopupMenu(act, v)
                if (rt.fillMenu(m.menu, cur.items)) m.setForceShowIcon(true)
                m.setOnMenuItemClickListener { item ->
                    if (item.hasSubMenu() || item.itemId < 0) return@setOnMenuItemClickListener false
                    val i = PRIMARY_BASE + item.itemId
                    rt.emit(id, AsukaEventKind.ACTION) { AsukaEvents.action(id, i) }
                    true
                }
                m.show()
                return@setOnClickListener
            }
            rt.emit(id, AsukaEventKind.ACTION) { AsukaEvents.action(id, PRIMARY_BASE) }
        }
    }

    fun attached() { scrolls?.start() }
    fun detached() { scrolls?.stop() }

    /** Sizes the bar and the button for a w×h screen (px). */
    fun measure(w: Int, h: Int) {
        val ins = rt.insetsOver(screen, w, h)
        val hold = holder
        val b = bar
        val docked = fab != null && toolbar.isNotEmpty()
        if (hold != null && b != null && hold.visibility == View.VISIBLE) {
            // Material 3: an 80dp container, the system's navigation bar under it.
            val bh = dp(80) + ins.bottom
            // Room for a docked FAB at the end, so the overflow comes before it.
            val fabRoom = if (docked) dp(16) + dp(56) + dp(8) else 0
            val rtl = screen.layoutDirection == View.LAYOUT_DIRECTION_RTL
            b.setPadding(ins.left + if (rtl) fabRoom else 0, 0, ins.right + if (rtl) 0 else fabRoom, ins.bottom)
            fit(b, w - ins.left - ins.right - fabRoom - b.contentInsetStart - b.contentInsetEnd)
            hold.measure(View.MeasureSpec.makeMeasureSpec(w, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(bh, View.MeasureSpec.EXACTLY))
        }
        val f: View? = fab ?: efab
        // Its own size where it sets one: an extended FAB animates its width as it shrinks and extends.
        fun spec(size: Int, max: Int) = if (size >= 0) View.MeasureSpec.makeMeasureSpec(size, View.MeasureSpec.EXACTLY)
            else View.MeasureSpec.makeMeasureSpec(max, View.MeasureSpec.AT_MOST)
        f?.let { it.measure(spec(it.layoutParams.width, w), spec(it.layoutParams.height, h)) }
        // Docked, it sits in the bar, lowered: Material 3's bottom app bar FAB has no shadow.
        fab?.let {
            val e = if (docked) 0f else dp(6).toFloat()
            if (it.compatElevation != e) it.compatElevation = e
        }
    }

    /**
     * Shows as many of the toolbar's buttons as fit in width px, as Material's
     * icon buttons (48dp; a title alone as wide as it is), and puts the rest
     * in the overflow menu, whose button takes one place. (A Toolbar's own
     * limit is a count by screen width, not the room it has.)
     */
    private fun fit(b: BottomAppBar, width: Int) {
        if (width == fitWidth && fitted >= 0) return
        fitWidth = width
        val paint = android.text.TextPaint().apply { textSize = 14 * rt.themed.resources.displayMetrics.scaledDensity }
        val widths = toolbar.map { if (it.icon.isNotEmpty()) dp(48) else Math.round(paint.measureText(it.title)) + dp(24) }
        var n = toolbar.size
        if (widths.sum() > width) {
            var room = width - dp(48)
            n = 0
            while (n < widths.size && widths[n] <= room) { room -= widths[n]; n++ }
        }
        if (n == fitted) return
        fitted = n
        for (i in 0 until b.menu.size()) {
            b.menu.getItem(i).setShowAsAction(if (i < n) MenuItem.SHOW_AS_ACTION_ALWAYS else MenuItem.SHOW_AS_ACTION_NEVER)
        }
    }

    /** Places them in a w×h screen (px). */
    fun layout(w: Int, h: Int) {
        val ins = rt.insetsOver(screen, w, h)
        val bh = barHeight
        holder?.takeIf { it.visibility == View.VISIBLE }?.layout(0, h - bh, w, h)
        val f: View = fab ?: efab ?: return
        val fw = f.measuredWidth
        val fh = f.measuredHeight
        val rtl = screen.layoutDirection == View.LAYOUT_DIRECTION_RTL
        val x = if (rtl) ins.left + dp(16) else w - ins.right - dp(16) - fw
        val y = when {
            // Docked: centered in the bar's 80dp.
            bh > 0 && fab != null -> h - bh + (dp(80) - fh) / 2
            // Over the bar, or the screen's bottom edge.
            bh > 0 -> h - bh - dp(16) - fh
            else -> h - ins.bottom - dp(16) - fh
        }
        f.layout(x, y, x + fw, y + fh)
    }
}

/**
 * Shrinks an extended FAB as the screen's content scrolls down, and extends
 * it as it scrolls up or reaches the top (Material's behavior). The content
 * is the first scroll view that takes up most of the screen; scrolls are
 * the view tree's, so they're followed whatever else listens to them.
 */
private class ScrollWatch(private val screen: AsukaScreen, private val fab: ExtendedFloatingActionButton) : ViewTreeObserver.OnScrollChangedListener {
    private var target: View? = null
    private var last = 0
    private var on = false
    private val slop = android.view.ViewConfiguration.get(screen.context).scaledTouchSlop

    fun start() {
        if (on) return
        on = true
        screen.viewTreeObserver.addOnScrollChangedListener(this)
    }

    fun stop() {
        if (!on) return
        on = false
        if (screen.viewTreeObserver.isAlive) screen.viewTreeObserver.removeOnScrollChangedListener(this)
        target = null
    }

    private fun offset(v: View) = if (v is RecyclerView) v.computeVerticalScrollOffset() else v.scrollY

    private fun find(): View? {
        target?.let { if (it.isAttachedToWindow && it.isShown) return it }
        val queue = ArrayDeque<View>()
        queue.add(screen)
        while (queue.isNotEmpty()) {
            val v = queue.removeFirst()
            val scrolls = v.canScrollVertically(1) || v.canScrollVertically(-1)
            if (v !== screen && v.isShown && v.height > 0 && v.height >= screen.height / 2 && scrolls) {
                target = v
                last = offset(v)
                return v
            }
            if (v is ViewGroup && v !is RecyclerView) for (i in 0 until v.childCount) queue.add(v.getChildAt(i))
        }
        return null
    }

    override fun onScrollChanged() {
        val v = find() ?: return
        val y = offset(v)
        val dy = y - last
        if (y <= 0) {
            last = y
            if (!fab.isExtended) fab.extend()
            return
        }
        if (Math.abs(dy) < slop) return
        last = y
        if (dy > 0 && fab.isExtended) fab.shrink() else if (dy < 0 && !fab.isExtended) fab.extend()
    }
}
