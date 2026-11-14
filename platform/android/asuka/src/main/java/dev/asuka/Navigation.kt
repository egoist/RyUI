package dev.asuka

import android.content.Context
import android.graphics.Color
import android.text.SpannableString
import android.text.Spanned
import android.text.style.ForegroundColorSpan
import android.util.TypedValue
import android.view.HapticFeedbackConstants
import android.view.MenuItem
import android.view.View
import android.view.ViewGroup
import android.view.animation.PathInterpolator
import android.widget.LinearLayout
import android.widget.PopupMenu
import androidx.activity.BackEventCompat
import androidx.activity.OnBackPressedCallback
import androidx.core.graphics.Insets
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsAnimationCompat
import androidx.core.view.WindowInsetsCompat
import androidx.swiperefreshlayout.widget.SwipeRefreshLayout
import com.google.android.material.appbar.MaterialToolbar
import com.google.android.material.bottomnavigation.BottomNavigationView
import com.google.android.material.bottomsheet.BottomSheetBehavior
import com.google.android.material.bottomsheet.BottomSheetDialog
import com.google.android.material.bottomsheet.BottomSheetDragHandleView
import com.google.android.material.color.MaterialColors
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.navigation.NavigationBarView
import java.nio.ByteBuffer

// Material 3 motion: emphasized easing, medium durations.
private val emphasized = PathInterpolator(0.2f, 0f, 0f, 1f)
private const val pushMillis = 350L
private const val popMillis = 300L

/**
 * A screen of a Stack, a page of Tabs or a sheet's content: sized by its
 * container, it reports its environment (size, the system bars and
 * keyboard over it) to the core, which lays it out.
 */
class AsukaScreen(context: Context, val nodeID: Int, private val rt: AsukaRuntime) : AsukaContainer(context) {
    var title = ""
    var icon = ""
    /** A tab's badge (Screen's Badge; empty: none). */
    var badge = ""
    var actions: List<AsukaAction> = emptyList()
    var backGesture = true
    /** ScreenOptions.ZoomFrom: the Match key of the view below it zooms out of; empty: none. */
    var zoomFrom = ""
    /** A search field's placeholder (ScreenOptions.Search); empty: none. */
    var search = ""
    var searchText = ""
    /** ScreenOptions.BarColor, as the core sends it; 0: the surface. */
    var barColor = 0L
    private var searching = false // the text is being set from code

    // A screen keeps its content to itself (over the bar, in a transition).
    init { clipChildren = true }

    /** ScreenOptions.Toolbar and Primary: its BottomAppBar and FAB (BottomBar.kt). */
    val bottom = AsukaScreenBar(this, rt)

    override fun onMeasure(widthSpec: Int, heightSpec: Int) {
        super.onMeasure(widthSpec, heightSpec)
        bottom.measure(measuredWidth, measuredHeight)
    }

    override fun onAttachedToWindow() {
        super.onAttachedToWindow()
        bottom.attached()
    }

    override fun onDetachedFromWindow() {
        super.onDetachedFromWindow()
        bottom.detached()
    }
    private var searchView: androidx.appcompat.widget.SearchView? = null

    /** The toolbar's search field, made once: typing and submits go to the screen's node. */
    fun searchView(ctx: Context): androidx.appcompat.widget.SearchView {
        searchView?.let { return it }
        val v = androidx.appcompat.widget.SearchView(ctx)
        v.maxWidth = Int.MAX_VALUE
        v.setOnQueryTextListener(object : androidx.appcompat.widget.SearchView.OnQueryTextListener {
            override fun onQueryTextChange(t: String): Boolean {
                if (!searching) {
                    searchText = t
                    rt.emit(nodeID, AsukaEventKind.TEXT_CHANGED) { AsukaEvents.textChanged(nodeID, t) }
                }
                return true
            }
            override fun onQueryTextSubmit(t: String): Boolean {
                rt.emit(nodeID, AsukaEventKind.SUBMIT) { AsukaEvents.submit(nodeID) }
                v.clearFocus()
                return true
            }
        })
        searchView = v
        return v
    }

    fun showSearchText(t: String) {
        searchText = t
        val v = searchView ?: return
        searching = true
        v.setQuery(t, false)
        searching = false
    }
    /** Its tabs hide their navigation bar while it's a pushed screen with a Toolbar on top (AsukaTabs.updateBarHidden). */
    fun coveringTabs() {
        var p = rt.nodes[nodeID]?.parent
        while (p != null) {
            (p.view as? AsukaTabs)?.let { it.updateBarHidden(); return }
            p = p.parent
        }
    }

    /** The keyboard's height over the window as it animates, in px; -1: as the window's insets say. */
    var ime = -1
    private var lastEnv: AsukaEnvironment? = null

    override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
        super.onLayout(changed, l, t, r, b)
        bottom.layout(r - l, b - t)
        report()
    }

    /**
     * A sheet's or popover's window (a dialog's, a popup's): its keys don't
     * pass the activity's dispatchKeyEvent, so they're routed here, on
     * their way to the focused view inside, from its node with nothing
     * focused (Keys.kt).
     */
    override fun dispatchKeyEvent(event: android.view.KeyEvent): Boolean {
        if (rootView !== rt.activity?.window?.decorView && rt.routeKey(event, findFocus(), rt.nodes[nodeID])) return true
        return super.dispatchKeyEvent(event)
    }

    // With nothing focused in its window, a key passes no view: it comes
    // back unhandled, and starts at the sheet's or popover's node.
    init {
        addOnUnhandledKeyEventListener { _, e ->
            val n = rt.nodes[nodeID]
            (n?.kind == AsukaKind.SHEET || n?.kind == AsukaKind.POPOVER) &&
                rootView !== rt.activity?.window?.decorView && findFocus() == null && rt.routeKey(e, null, n)
        }
    }

    fun report() {
        val n = rt.nodes[nodeID] ?: return
        if (width == 0 || height == 0 || n.dying) return
        var e = rt.environment(this, if (ime >= 0) ime else null)
        // A sheet never reaches under the status bar, and pads itself above
        // the navigation bar; it moves without a layout, so its position
        // says nothing. A full-screen one is the screen's, its insets too.
        if (n.kind == AsukaKind.SHEET && !n.fullScreen) e = e.copy(safeTop = 0f, safeBottom = 0f)
        if (n.kind == AsukaKind.POPOVER) e = e.copy(safeTop = 0f, safeRight = 0f, safeBottom = 0f, safeLeft = 0f, keyboard = 0f)
        // Its bottom app bar covers the system's navigation bar and more.
        if (bottom.barHeight > 0) e = e.copy(safeBottom = maxOf(e.safeBottom, bottom.barHeight / rt.density))
        if (e == lastEnv) return
        lastEnv = e
        rt.send(AsukaEvents.environment(nodeID, e))
    }
}

private fun Context.actionBarSize(): Int {
    val tv = TypedValue()
    if (!theme.resolveAttribute(androidx.appcompat.R.attr.actionBarSize, tv, true)) return Math.round(64 * resources.displayMetrics.density)
    return TypedValue.complexToDimensionPixelSize(tv.data, resources.displayMetrics)
}

/**
 * A Stack: a Material top app bar over its screens. An Insert pushes, a
 * Destroy pops; the user's pops (back, the bar's up button) are reported
 * as Popped.
 */
class AsukaStack(context: Context, val nodeID: Int, private val rt: AsukaRuntime) : ViewGroup(context) {
    var bar = makeBar(context)
        private set
    /** The screens, top last; one being popped has left already. */
    val screens = ArrayList<AsukaNode>()
    val top: AsukaNode? get() = screens.lastOrNull()
    private var backing = false
    /** Replace: the screens destroyed next, and the one inserted next, change at once. */
    var instant = false

    init {
        addView(bar)
    }

    private fun makeBar(ctx: Context) = MaterialToolbar(ctx).apply {
        setBackgroundColor(MaterialColors.getColor(ctx, com.google.android.material.R.attr.colorSurface, Color.WHITE))
        setNavigationOnClickListener {
            val split = rt.nodes[nodeID]?.parent?.view as? AsukaSplit
            if (screens.size < 2 && split != null) split.openDrawer() else rt.userPop(this@AsukaStack)
        }
    }

    /** Takes the bar's colors from the current theme. */
    fun restyle() {
        removeView(bar)
        bar = makeBar(rt.themed)
        addView(bar)
        for (s in screens) s.view.setBackgroundColor(surface())
        refreshBar()
        requestLayout()
    }

    private fun surface() = MaterialColors.getColor(rt.themed, com.google.android.material.R.attr.colorSurface, Color.WHITE)

    override fun onMeasure(widthSpec: Int, heightSpec: Int) {
        val w = MeasureSpec.getSize(widthSpec)
        val h = MeasureSpec.getSize(heightSpec)
        setMeasuredDimension(w, h)
        val ins = rt.insetsOver(this, w, h)
        bar.setPadding(ins.left, ins.top, ins.right, 0)
        val bh = context.actionBarSize() + ins.top
        bar.measure(MeasureSpec.makeMeasureSpec(w, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(bh, MeasureSpec.EXACTLY))
        val ch = maxOf(0, h - bh)
        for (i in 0 until childCount) {
            val c = getChildAt(i)
            if (c !== bar) c.measure(MeasureSpec.makeMeasureSpec(w, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(ch, MeasureSpec.EXACTLY))
        }
    }

    override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
        val bh = bar.measuredHeight
        bar.layout(0, 0, r - l, bh)
        for (i in 0 until childCount) {
            val c = getChildAt(i)
            if (c !== bar) c.layout(0, bh, r - l, b - t)
        }
    }

    /** Sizes a screen at once, so it reports its environment in this flush. */
    private fun layoutNow(v: View) {
        if (width == 0) return
        val bh = bar.measuredHeight
        v.measure(MeasureSpec.makeMeasureSpec(width, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(maxOf(0, height - bh), MeasureSpec.EXACTLY))
        v.layout(0, bh, width, height)
    }

    /** How far screens slide, toward the end they go to: a push comes from the end, which is the left right to left. */
    private val slide get() = (if (layoutDirection == LAYOUT_DIRECTION_RTL) -0.08f else 0.08f) * width.coerceAtLeast(1)

    fun insert(n: AsukaNode, index: Int) {
        instant = false
        val v = n.view
        v.setBackgroundColor(surface())
        (v.parent as? ViewGroup)?.removeView(v)
        if (index >= screens.size) {
            val prev = top
            screens.add(n)
            addView(v, indexOfChild(bar))
            layoutNow(v)
            refreshBar()
            if (prev == null) return
            if (isAttachedToWindow && zoom(n, prev, true)) return
            if (isAttachedToWindow) {
                v.translationX = slide
                v.alpha = 0f
                v.animate().translationX(0f).alpha(1f).setDuration(pushMillis).setInterpolator(emphasized).start()
                prev.view.animate().translationX(-slide).alpha(0f).setDuration(pushMillis).setInterpolator(emphasized).withEndAction {
                    if (prev !== top) prev.view.visibility = INVISIBLE
                    prev.view.translationX = 0f
                    prev.view.alpha = 1f
                }.start()
            } else {
                prev.view.visibility = INVISIBLE
            }
        } else {
            screens.add(index, n)
            v.visibility = INVISIBLE
            addView(v, 0)
            layoutNow(v)
            refreshBar()
        }
    }

    /** Takes n out: the top animates away when animated, others just go. */
    fun remove(n: AsukaNode, animated: Boolean) {
        if (n !== top) {
            screens.remove(n)
            removeView(n.view)
            refreshBar()
            return
        }
        screens.remove(n)
        val below = top
        refreshBar()
        val v = n.view
        if (!animated || below == null) {
            backing = false
            removeView(v)
            below?.view?.visibility = VISIBLE
            return
        }
        val b = below.view
        b.visibility = VISIBLE
        if (backing) {
            // The predictive back gesture already shows what's below; a
            // screen that zoomed out of a view goes back into it.
            backing = false
            if (zoom(n, below, false)) return
            // It goes on the way the gesture took it.
            val away = if (v.translationX == 0f) slide else if (v.translationX < 0) -Math.abs(slide) else Math.abs(slide)
            v.animate().alpha(0f).translationX(v.translationX + away).setDuration(popMillis / 2).setInterpolator(emphasized)
                .withEndAction { removeView(v) }.start()
            return
        }
        if (zoom(n, below, false)) return
        b.translationX = -slide
        b.alpha = 0f
        b.animate().translationX(0f).alpha(1f).setDuration(popMillis).setInterpolator(emphasized).start()
        v.animate().translationX(slide).alpha(0f).setDuration(popMillis).setInterpolator(emphasized)
            .withEndAction { removeView(v) }.start()
    }

    /**
     * ScreenOptions.ZoomFrom: Material's container transform. The screen's
     * container grows from the view with its key on the screen below into
     * its own bounds (or, popping, back into the view), its content scaled
     * to the container's width and clipped to it, rounded; the view is
     * hidden meanwhile. False if there's no such view on screen.
     */
    private fun zoom(n: AsukaNode, below: AsukaNode, push: Boolean): Boolean {
        val key = (n.view as AsukaScreen).zoomFrom
        if (key.isEmpty()) return false
        val src = rt.zoomSource(key, below, n) ?: return false
        val v = n.view
        if (v.width == 0 || v.height == 0 || src.width == 0 || src.height == 0) return false
        val at = IntArray(2)
        val me = IntArray(2)
        src.getLocationInWindow(at)
        getLocationInWindow(me)
        val from = android.graphics.RectF((at[0] - me[0]).toFloat(), (at[1] - me[1]).toFloat(), 0f, 0f)
        from.right = from.left + src.width
        from.bottom = from.top + src.height
        // Popping, it starts where the screen is: a predictive back gesture
        // has shrunk and moved it (around its center).
        val s0 = if (push) 1f else v.scaleX
        val to = android.graphics.RectF(
            v.left + v.translationX + (1 - s0) * v.pivotX, v.top + v.translationY + (1 - s0) * v.pivotY, 0f, 0f)
        to.right = to.left + v.width * s0
        to.bottom = to.top + v.height * s0
        val full = v.width.toFloat()
        val radius = 16 * rt.density
        val shown = src.alpha
        var clip = android.graphics.RectF()
        var corner = 0f
        v.outlineProvider = object : android.view.ViewOutlineProvider() {
            override fun getOutline(view: View, o: android.graphics.Outline) {
                o.setRoundRect(Math.round(clip.left), Math.round(clip.top), Math.round(clip.right), Math.round(clip.bottom), corner)
            }
        }
        v.clipToOutline = true
        fun frame(p: Float) {
            fun lerp(a: Float, b: Float) = a + (b - a) * p
            val l = lerp(from.left, to.left)
            val t = lerp(from.top, to.top)
            val w = lerp(from.width(), to.width())
            val h = lerp(from.height(), to.height())
            val s = w / full // the content fits the container's width
            v.scaleX = s
            v.scaleY = s
            v.translationX = l - v.left
            v.translationY = t - v.top
            clip = android.graphics.RectF(0f, 0f, full, h / s)
            corner = lerp(radius, 0f) / s
            v.invalidateOutline()
            // Near the view, the screen and the view cross-fade.
            v.alpha = (p / 0.3f).coerceIn(0f, 1f)
            src.alpha = shown * (1 - v.alpha)
        }
        v.pivotX = 0f // after reading where the gesture left it
        v.pivotY = 0f
        below.view.visibility = VISIBLE
        below.view.alpha = 1f
        below.view.translationX = 0f
        frame(if (push) 0f else 1f)
        android.animation.ValueAnimator.ofFloat(if (push) 0f else 1f, if (push) 1f else 0f).apply {
            duration = if (push) pushMillis + 100 else popMillis + 100
            interpolator = emphasized
            addUpdateListener { frame(it.animatedValue as Float) }
            addListener(object : android.animation.AnimatorListenerAdapter() {
                override fun onAnimationEnd(a: android.animation.Animator) {
                    src.alpha = shown
                    v.clipToOutline = false
                    v.outlineProvider = android.view.ViewOutlineProvider.BACKGROUND
                    v.scaleX = 1f; v.scaleY = 1f; v.translationX = 0f; v.translationY = 0f; v.alpha = 1f
                    v.pivotX = v.width / 2f; v.pivotY = v.height / 2f
                    if (push) { if (below !== top) below.view.visibility = INVISIBLE } else removeView(v)
                }
            })
            start()
        }
        return true
    }

    // Predictive back: the top screen shrinks and follows the gesture,
    // showing the one below.

    fun backStarted() {
        val t = top ?: return
        if (screens.size < 2 || !(t.view as AsukaScreen).backGesture) return
        backing = true
        val b = screens[screens.size - 2].view
        b.animate().cancel()
        b.visibility = VISIBLE
        b.alpha = 1f
        b.translationX = 0f
    }

    fun backProgressed(e: BackEventCompat) {
        if (!backing) return
        val v = top?.view ?: return
        val s = 1f - 0.1f * e.progress
        v.scaleX = s
        v.scaleY = s
        // Along the finger from either edge; from no edge (buttons), as a pop goes.
        v.translationX = e.progress * when (e.swipeEdge) {
            BackEventCompat.EDGE_LEFT -> Math.abs(slide)
            BackEventCompat.EDGE_RIGHT -> -Math.abs(slide)
            else -> slide
        }
    }

    fun backCancelled() {
        if (!backing) return
        backing = false
        val t = top ?: return
        val below = screens.getOrNull(screens.size - 2)
        t.view.animate().scaleX(1f).scaleY(1f).translationX(0f).setDuration(popMillis / 2).setInterpolator(emphasized).withEndAction {
            if (below != null && below !== top) below.view.visibility = INVISIBLE
        }.start()
    }

    /** Shows the top screen's title, actions and, under it, the up button. */
    fun refreshBar() {
        val t = top ?: return
        val s = t.view as AsukaScreen
        bar.title = s.title
        bar.setBackgroundColor(if (s.barColor != 0L) rt.color(s.barColor) else surface())
        val split = rt.nodes[nodeID]?.parent?.view as? AsukaSplit
        if (screens.size > 1) {
            bar.setNavigationIcon(R.drawable.asuka_ic_arrow_back)
            bar.setNavigationIconTint(MaterialColors.getColor(bar, com.google.android.material.R.attr.colorOnSurface))
            bar.setNavigationContentDescription(androidx.appcompat.R.string.abc_action_bar_up_description)
        } else if (split != null && !split.wide) {
            // A SplitView's destination: its drawer's menu button.
            bar.navigationIcon = rt.symbol("line.3.horizontal")
            bar.setNavigationIconTint(MaterialColors.getColor(bar, com.google.android.material.R.attr.colorOnSurface))
            bar.setNavigationContentDescription(androidx.appcompat.R.string.abc_action_menu_overflow_description)
        } else {
            bar.navigationIcon = null
        }
        bar.menu.clear()
        for ((i, a) in s.actions.withIndex()) {
            // A bar button with children opens them as a menu.
            val item = if (a.submenu) {
                val sub = bar.menu.addSubMenu(0, a.index.toInt(), i, rt.styled(a))
                rt.fillMenu(sub, a.items, 1000 * (i + 1))
                sub.item
            } else bar.menu.add(0, a.index.toInt(), i, rt.styled(a))
            item.setShowAsAction(MenuItem.SHOW_AS_ACTION_IF_ROOM)
            item.isEnabled = !a.disabled
            // An icon stands for the title, which TalkBack reads (and a long press shows).
            if (a.icon.isNotEmpty()) item.icon = rt.symbol(a.icon)?.apply {
                setTint(MaterialColors.getColor(rt.themed, com.google.android.material.R.attr.colorOnSurfaceVariant, Color.DKGRAY))
            }
        }
        if (s.search.isNotEmpty()) {
            // Material's pattern: a search action that expands into the field.
            val sv = s.searchView(bar.context)
            sv.queryHint = s.search
            val item = bar.menu.add(0, SEARCH_ITEM, 0, s.search)
            item.icon = rt.symbol("magnifyingglass")?.apply {
                setTint(MaterialColors.getColor(rt.themed, com.google.android.material.R.attr.colorOnSurfaceVariant, Color.DKGRAY))
            }
            item.actionView = sv
            item.setShowAsAction(MenuItem.SHOW_AS_ACTION_ALWAYS or MenuItem.SHOW_AS_ACTION_COLLAPSE_ACTION_VIEW)
            if (s.searchText.isNotEmpty()) { item.expandActionView(); s.showSearchText(s.searchText) }
        }
        s.coveringTabs()
        val id = t.id
        bar.setOnMenuItemClickListener { item ->
            if (item.itemId == SEARCH_ITEM) return@setOnMenuItemClickListener false // it expands
            if (item.hasSubMenu() || item.itemId < 0) return@setOnMenuItemClickListener false // it opens, or is a header
            rt.emit(id, AsukaEventKind.ACTION) { AsukaEvents.action(id, item.itemId.toLong()) }
            true
        }
    }
}

/**
 * The count a badge says, if it's only ASCII digits (at most nine), as
 * ops.BadgeNumber reads it; null for other text, shown as it is.
 */
fun badgeNumber(s: String): Int? =
    if (s.isEmpty() || s.length > 9 || s.any { it !in '0'..'9' }) null else s.toInt()

/** The toolbar menu item of a screen's search field. */
private const val SEARCH_ITEM = 0x5eac

/** Tabs: a Material navigation bar under its pages. */
class AsukaTabs(context: Context, val nodeID: Int, private val rt: AsukaRuntime) : ViewGroup(context) {
    var bar = makeBar(context)
        private set
    val pages = ArrayList<AsukaNode>()
    var selected = 0
        private set
    private var selecting = false
    val current: AsukaNode? get() = pages.getOrNull(selected)

    init {
        addView(bar)
    }

    private fun makeBar(ctx: Context) = BottomNavigationView(ctx).apply {
        labelVisibilityMode = NavigationBarView.LABEL_VISIBILITY_LABELED
        // The bar's bottom padding is the system bar under it; see onMeasure.
        ViewCompat.setOnApplyWindowInsetsListener(this) { _, insets -> insets }
        setOnItemSelectedListener { item ->
            if (!selecting) {
                show(item.itemId)
                val i = item.itemId.toLong()
                rt.emit(nodeID, AsukaEventKind.SELECTED) { AsukaEvents.selected(nodeID, i) }
            }
            true
        }
    }

    fun restyle() {
        removeView(bar)
        bar = makeBar(rt.themed)
        addView(bar)
        refreshBar()
        requestLayout()
    }

    fun select(i: Int) {
        selected = i
        if (i < pages.size) {
            selecting = true
            bar.selectedItemId = i
            selecting = false
        }
        show(i)
    }

    private fun show(i: Int) {
        selected = i
        for ((j, p) in pages.withIndex()) p.view.visibility = if (j == i) VISIBLE else INVISIBLE
        rt.updateBack()
        updateBarHidden()
    }

    /** The bar's height, 0 while it's hidden. */
    private fun barHeight() = if (bar.visibility == GONE) 0 else bar.measuredHeight

    /**
     * Hides the navigation bar while the selected page's innermost shown
     * stack has a pushed screen with a Toolbar on top, whose bottom app bar
     * takes the bottom edge (Material: the two don't go together); shows it
     * again after.
     */
    fun updateBarHidden() {
        fun covers(n: AsukaNode): Boolean {
            val v = n.view
            if (v is AsukaStack) {
                val t = v.top ?: return false
                if (v.screens.size > 1 && (t.view as AsukaScreen).bottom.toolbar.isNotEmpty()) return true
                return covers(t)
            }
            if (v is AsukaTabs) return false
            return n.children.any { covers(it) }
        }
        val hide = current?.let { covers(it) } ?: false
        val vis = if (hide) GONE else VISIBLE
        if (bar.visibility != vis) {
            bar.visibility = vis
            requestLayout()
        }
    }

    fun insert(n: AsukaNode, index: Int) {
        val i = minOf(index, pages.size)
        pages.add(i, n)
        (n.view.parent as? ViewGroup)?.removeView(n.view)
        addView(n.view, indexOfChild(bar))
        refreshBar()
        show(selected)
        if (width > 0) {
            val bh = bar.measuredHeight
            n.view.measure(MeasureSpec.makeMeasureSpec(width, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(maxOf(0, height - bh), MeasureSpec.EXACTLY))
            n.view.layout(0, 0, width, height - bh)
        }
    }

    fun remove(n: AsukaNode) {
        pages.remove(n)
        removeView(n.view)
        refreshBar()
        show(selected.coerceAtMost(maxOf(0, pages.size - 1)))
    }

    /** Makes the bar's items from the pages' titles and icons. */
    fun refreshBar() {
        selecting = true
        bar.menu.clear()
        for ((i, p) in pages.withIndex()) {
            if (i >= bar.maxItemCount) break
            val s = p.view as AsukaScreen
            val item = bar.menu.add(0, i, i, s.title)
            rt.symbol(s.icon)?.let { item.icon = it }
            showBadge(i, s.badge)
        }
        if (selected < bar.menu.size()) bar.selectedItemId = selected
        selecting = false
    }

    /** Shows page n's badge on its item, and nothing else changes. */
    fun showBadge(n: AsukaNode) {
        val i = pages.indexOf(n)
        if (i >= 0 && i < bar.menu.size()) showBadge(i, (n.view as AsukaScreen).badge)
    }

    /**
     * Item i's badge: Material's BadgeDrawable, a count as its number
     * (999+ past 999, as Material shortens it) and other text as its
     * text; none when empty. TalkBack reads it after the item's title,
     * in Material's words ("3 new notifications").
     */
    private fun showBadge(i: Int, text: String) {
        if (text.isEmpty()) {
            bar.removeBadge(i)
            return
        }
        val b = bar.getOrCreateBadge(i)
        val n = badgeNumber(text)
        if (n != null) {
            b.clearText()
            b.number = n
        } else {
            b.clearNumber()
            b.text = text
        }
        b.isVisible = true
    }

    override fun onMeasure(widthSpec: Int, heightSpec: Int) {
        val w = MeasureSpec.getSize(widthSpec)
        val h = MeasureSpec.getSize(heightSpec)
        setMeasuredDimension(w, h)
        val ins = rt.insetsOver(this, w, h)
        bar.setPadding(ins.left, 0, ins.right, ins.bottom)
        bar.measure(MeasureSpec.makeMeasureSpec(w, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(h, MeasureSpec.AT_MOST))
        val ph = maxOf(0, h - barHeight())
        for (i in 0 until childCount) {
            val c = getChildAt(i)
            if (c !== bar) c.measure(MeasureSpec.makeMeasureSpec(w, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(ph, MeasureSpec.EXACTLY))
        }
    }

    override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
        val bh = barHeight()
        val h = b - t
        bar.layout(0, h - bh, r - l, h)
        for (i in 0 until childCount) {
            val c = getChildAt(i)
            if (c !== bar) c.layout(0, 0, r - l, h - bh)
        }
    }
}

// The runtime's navigation: environments, back, dialogs, menus, refresh
// and haptics.

/** Where v is in its window, by layout (animations' transforms aside). */
private fun offsetInWindow(v: View): IntArray {
    var x = 0
    var y = 0
    var c = v
    while (true) {
        x += c.left
        y += c.top
        val p = c.parent as? View ?: break
        x -= p.scrollX
        y -= p.scrollY
        c = p
    }
    return intArrayOf(x, y)
}

/** How much of the system bars and cutout overlap a w×h view, in px. */
fun AsukaRuntime.insetsOver(v: View, w: Int, h: Int): Insets {
    val ins = ViewCompat.getRootWindowInsets(v) ?: return Insets.NONE
    val bars = ins.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
    val root = v.rootView
    val o = offsetInWindow(v)
    return Insets.of(
        maxOf(0, bars.left - o[0]),
        maxOf(0, bars.top - o[1]),
        maxOf(0, bars.right - maxOf(0, root.width - (o[0] + w))),
        maxOf(0, bars.bottom - maxOf(0, root.height - (o[1] + h))),
    )
}

/** A view's environment: its size and what overlaps it, in dp. */
fun AsukaRuntime.environment(v: View, keyboardPx: Int? = null): AsukaEnvironment {
    val d = density
    val ins = insetsOver(v, v.width, v.height)
    val root = v.rootView
    val act = activity
    val kb = keyboardPx ?: if (act != null && root === act.window.decorView) act.keyboardPx
        else ViewCompat.getRootWindowInsets(v)?.getInsets(WindowInsetsCompat.Type.ime())?.bottom ?: 0
    val below = maxOf(0, root.height - (offsetInWindow(v)[1] + v.height))
    val c = (act?.resources ?: app.resources).configuration
    // The size classes are the app window's, as Material's are, for a
    // screen or a sheet too.
    val win = act?.window?.decorView ?: root
    return AsukaEnvironment(
        width = v.width / d, height = v.height / d,
        safeTop = ins.top / d, safeRight = ins.right / d, safeBottom = ins.bottom / d, safeLeft = ins.left / d,
        keyboard = maxOf(0, kb - below) / d,
        scale = d,
        fontScale = c.fontScale,
        dark = isNight(c),
        reduceMotion = !android.animation.ValueAnimator.areAnimatorsEnabled(),
        screenReader = (app.getSystemService(Context.ACCESSIBILITY_SERVICE) as android.view.accessibility.AccessibilityManager).isTouchExplorationEnabled,
        widthClass = widthClass(win.width / d), heightClass = heightClass(win.height / d),
        locale = region.locale,
        languages = region.languages,
        firstWeekday = region.firstWeekday,
        hourCycle = region.hourCycle,
    )
}

/** Material's window width class (as ops.SizeClass: 1 compact, 2 medium, 3 expanded) of a width in dp. */
fun widthClass(dp: Float) = if (dp < 600f) 1 else if (dp < 840f) 2 else 3

/** Material's window height class (as ops.SizeClass) of a height in dp. */
fun heightClass(dp: Float) = if (dp < 480f) 1 else if (dp < 900f) 2 else 3

/** The window's insets changed: bars move, screens report again. */
fun AsukaRuntime.insetsChanged() {
    for (n in nodes.values) {
        when (val v = n.view) {
            is AsukaStack, is AsukaTabs -> v.requestLayout()
            is AsukaScreen -> v.report()
        }
    }
    AsukaSnackbar.current?.place()
}

/** The keyboard moved: screens report again, and a snackbar stays above it. */
fun AsukaRuntime.reportScreens() {
    for (n in nodes.values) (n.view as? AsukaScreen)?.report()
    AsukaSnackbar.current?.place()
}

/** Whether n shows: every stack above it shows its branch, every tabs its page. */
private fun AsukaRuntime.shown(n: AsukaNode): Boolean {
    var c = n
    var p = n.parent
    while (p != null) {
        if (c.dying) return false
        when (val v = p.view) {
            is AsukaStack -> if (v.top !== c) return false
            is AsukaTabs -> if (v.current !== c) return false
        }
        c = p
        p = p.parent
    }
    return c.id == rootID
}

/** The stack back pops: the innermost shown one with a screen to go back to. */
fun AsukaRuntime.activeStack(): AsukaStack? {
    var best: AsukaStack? = null
    var bestDepth = -1
    for (n in stacks) {
        val s = n.view as AsukaStack
        if (s.screens.size < 2 || !shown(n)) continue
        var depth = 0
        var p = n.parent
        while (p != null) { depth++; p = p.parent }
        if (depth > bestDepth) { best = s; bestDepth = depth }
    }
    return best
}

fun AsukaRuntime.makeBackCallback(): OnBackPressedCallback = object : OnBackPressedCallback(false) {
    private var stack: AsukaStack? = null

    override fun handleOnBackStarted(backEvent: BackEventCompat) {
        // A screen that takes back itself (ui.OnBack) isn't previewed popping.
        stack = activeStack()?.takeIf { it.top?.intercepts != true }?.also { it.backStarted() }
    }

    override fun handleOnBackProgressed(backEvent: BackEventCompat) {
        stack?.backProgressed(backEvent)
    }

    override fun handleOnBackCancelled() {
        stack?.backCancelled()
        stack = null
    }

    override fun handleOnBackPressed() {
        val s = stack ?: activeStack()
        stack = null
        if (s != null) userPop(s)
        else nodes[rootID]?.takeIf { it.intercepts }?.let { emit(it.id, AsukaEventKind.BACK) { AsukaEvents.back(it.id) } }
    }
}

/** The core takes the user's going back from this screen, sheet or root (ui.OnBack). */
val AsukaNode.intercepts: Boolean get() = props[AsukaProp.INTERCEPT_BACK] == true

/** The root's OnBack let Android's back go on: do what the system would have. */
fun AsukaRuntime.backExit() {
    val act = activity ?: return
    back.isEnabled = false
    act.onBackPressedDispatcher.onBackPressed()
    updateBack()
}

/** A sheet's OnBack: Android's back, a tap outside and a swipe down ask the core rather than dismiss it. */
fun AsukaRuntime.sheetIntercepts(n: AsukaNode) {
    val d = n.dialog as? BottomSheetDialog ?: return
    val on = n.intercepts
    d.setCancelable(!on) // not hideable by a swipe either
    sheetBack[n.id]?.isEnabled = on
}

/** The user popped a stack's top screen: tell the core, which destroys it. */
fun AsukaRuntime.userPop(s: AsukaStack) {
    val top = s.top ?: return
    if (s.screens.size < 2) return
    if (top.intercepts) { // ui.OnBack: the core decides, and pops it if it goes
        emit(top.id, AsukaEventKind.BACK) { AsukaEvents.back(top.id) }
        return
    }
    top.dying = true
    nodes[s.nodeID]?.children?.remove(top)
    s.remove(top, animated = true)
    val id = top.id.toLong()
    emit(s.nodeID, AsukaEventKind.POPPED) { AsukaEvents.popped(s.nodeID, id) }
    updateBack()
}

/** Inserts into a Stack or Tabs; false: an ordinary insert. */
fun AsukaRuntime.insertNav(p: AsukaNode, c: AsukaNode, index: Int): Boolean {
    val v = p.view
    if (v is AsukaSplit) {
        p.children.add(minOf(index, p.children.size), c)
        c.parent = p
        v.setDetail(c.view)
        return true
    }
    if (v !is AsukaStack && v !is AsukaTabs) return false
    val i = minOf(index, p.children.size)
    p.children.add(i, c)
    c.parent = p
    when (v) {
        is AsukaStack -> v.insert(c, i)
        is AsukaTabs -> v.insert(c, i)
    }
    return true
}

/** Destroys a screen, sheet or alert; false: an ordinary destroy. */
fun AsukaRuntime.destroyNav(n: AsukaNode): Boolean {
    if (n.dying) return true // the user popped or dismissed it
    val p = n.parent
    val alive = p != null && nodes[p.id] === p
    when {
        alive && p!!.view is AsukaStack -> {
            n.dying = true
            p.children.remove(n)
            val s = p.view as AsukaStack
            s.remove(n, animated = !s.instant && s.top === n && s.isAttachedToWindow)
            return true
        }
        alive && p!!.view is AsukaTabs -> {
            n.dying = true
            p.children.remove(n)
            (p.view as AsukaTabs).remove(n)
            return true
        }
        n.kind == AsukaKind.POPOVER -> {
            n.dying = true
            n.popover?.dismiss()
            return true
        }
        n.kind == AsukaKind.SHEET || n.kind == AsukaKind.ALERT -> {
            n.dying = true
            n.dialog?.dismiss()
            n.dialog = null
            sheetBack.remove(n.id)
            return true
        }
    }
    return false
}

fun AsukaRuntime.setNavProp(n: AsukaNode, prop: Int, v: Any): Boolean {
    when (n.kind) {
        AsukaKind.SCREEN -> {
            val s = n.view as AsukaScreen
            when (prop) {
                AsukaProp.Screen.TITLE -> s.title = v as String
                AsukaProp.Screen.ICON -> s.icon = v as String
                AsukaProp.Screen.BADGE -> {
                    // The badge alone changes: the bar isn't made again.
                    s.badge = v as String
                    (n.parent?.view as? AsukaTabs)?.showBadge(n)
                    return true
                }
                AsukaProp.Screen.BACK_GESTURE -> s.backGesture = v as Boolean
                AsukaProp.Screen.ZOOM_FROM -> { s.zoomFrom = v as String; return true }
                AsukaProp.Screen.ACTIONS -> s.actions = decodeActions(v as ByteBuffer)
                AsukaProp.Screen.SEARCH -> s.search = v as String
                AsukaProp.Screen.SEARCH_TEXT -> { s.showSearchText(v as String); return true }
                AsukaProp.Screen.LARGE_TITLE -> return true // Material's top app bar keeps its look
                AsukaProp.Screen.TOOLBAR -> { s.bottom.toolbar = decodeActions(v as ByteBuffer); s.bottom.refresh(); return true } // and its tabs' bar
                AsukaProp.Screen.PRIMARY -> { s.bottom.primary = decodeActions(v as ByteBuffer).firstOrNull(); s.bottom.refresh(); return true }
                AsukaProp.Screen.PRIMARY_EXTENDED -> { s.bottom.extended = v as Boolean; s.bottom.refresh(); return true }
                else -> return false
            }
            when (val pv = n.parent?.view) {
                is AsukaStack -> if (pv.top === n) pv.refreshBar()
                is AsukaTabs -> pv.refreshBar()
            }
        }
        AsukaKind.TABS -> if (prop == AsukaProp.Tabs.SELECTED) (n.view as AsukaTabs).select((v as Long).toInt()) else return false
        AsukaKind.SHADER -> (n.view as AsukaShaderView).let {
            when (prop) {
                AsukaProp.Shader.CODE -> it.setCode(v as java.nio.ByteBuffer)
                AsukaProp.Shader.ANIMATED -> it.animated = v as Boolean
                AsukaProp.Shader.UNIFORMS -> it.setUniforms(v as java.nio.ByteBuffer)
                else -> return false
            }
        }
        AsukaKind.SPLIT -> (n.view as AsukaSplit).let {
            when (prop) {
                AsukaProp.Split.ITEMS -> it.setItems(v as java.nio.ByteBuffer)
                AsukaProp.Split.SELECTED -> it.select((v as Long).toInt())
                AsukaProp.Split.TITLE -> it.setTitle(v as String)
                else -> return false
            }
        }
        AsukaKind.CANVAS -> if (prop == AsukaProp.Canvas.COMMANDS) (n.view as AsukaCanvas).setCommands(v as java.nio.ByteBuffer) else return false
        AsukaKind.ACCESSORY -> when (prop) {
            AsukaProp.Accessory.HEIGHT -> n.accessory?.height = (v as Double).toFloat()
            AsukaProp.Accessory.WIDTH -> {}
            else -> return false
        }
        AsukaKind.POPOVER -> n.popover?.let {
            when (prop) {
                AsukaProp.Popover.ANCHOR -> it.anchor = (v as Long).toInt()
                AsukaProp.Popover.WIDTH -> { it.width = (v as Double).toFloat(); it.resize() }
                AsukaProp.Popover.HEIGHT -> { it.height = (v as Double).toFloat(); it.resize() }
                else -> return false
            }
        }
        AsukaKind.SHEET -> when (prop) {
            AsukaProp.Sheet.DETENTS -> n.detents = v as Long
            AsukaProp.Sheet.GRABBER -> n.grabber = v as Boolean
            AsukaProp.Sheet.FULL_SCREEN -> n.fullScreen = v as Boolean
            AsukaProp.Sheet.WINDOW -> {} // the window it's declared in: a phone has one
            else -> return false
        }
        AsukaKind.ALERT -> when (prop) {
            AsukaProp.Alert.TITLE -> n.alertTitle = v as String
            AsukaProp.Alert.MESSAGE -> n.alertMessage = v as String
            AsukaProp.Alert.BUTTONS -> {
                n.actions = decodeActions(v as ByteBuffer)
                n.alertRefresh?.invoke()
            }
            AsukaProp.Alert.STYLE -> n.alertStyle = v as Int
            AsukaProp.Alert.WINDOW -> {} // likewise
            AsukaProp.Alert.FIELDS -> n.alertFields = decodeAlertFields(v as ByteBuffer)
            else -> return false
        }
        AsukaKind.SNACKBAR -> return (n.view as AsukaSnackbar).set(prop, v)
        AsukaKind.SCROLL -> when (prop) {
            AsukaProp.Scroll.HORIZONTAL -> setHorizontal(n, v as Boolean)
            AsukaProp.Scroll.PAGING -> AsukaPager.apply(this, n)
            AsukaProp.Scroll.REFRESHABLE -> setRefreshable(n, v as Boolean)
            AsukaProp.Scroll.REFRESHING -> (n.wrapper as? SwipeRefreshLayout)?.isRefreshing = v as Boolean
            else -> return false
        }
        AsukaKind.LIST -> when (prop) {
            AsukaProp.List.REFRESHABLE -> setRefreshable(n, v as Boolean)
            AsukaProp.List.REFRESHING -> (n.wrapper as? SwipeRefreshLayout)?.isRefreshing = v as Boolean
            AsukaProp.List.COLUMNS -> n.list?.let { it.setGrid((v as Long).toInt(), it.gap) }
            AsukaProp.List.SELECTING -> n.list?.setSelecting(v as Boolean)
            else -> return false
        }
        AsukaKind.IMAGE -> when (prop) {
            AsukaProp.Image.SOURCE -> loadImage(n, v as String)
            AsukaProp.Image.PLACEHOLDER -> {
                n.imagePlaceholder = v as String
                showPlaceholder(n)
            }
            AsukaProp.Image.PLACEHOLDER_IMAGE -> {
                n.imageHash = placeholderBitmap(v as ByteBuffer)
                showPlaceholder(n)
            }
            AsukaProp.Image.PLACEHOLDER_MODE -> (n.view as AsukaImageView).placeholderMode = v as Int
            AsukaProp.Image.TRANSITION -> (n.view as AsukaImageView).transition = v as Double
            AsukaProp.Image.PAUSED -> (n.view as AsukaImageView).paused = v as Boolean
            AsukaProp.Image.MODE -> (n.view as android.widget.ImageView).scaleType = imageScaleType(v as Int)
            else -> return false
        }
        AsukaKind.NATIVE -> return setNativeProp(n, prop, v)
        AsukaKind.PAGE_CONTROL -> (n.view as AsukaPageDots).let {
            when (prop) {
                AsukaProp.PageControl.COUNT -> it.count = (v as Long).toInt()
                AsukaProp.PageControl.PAGE -> it.page = (v as Long).toInt()
                else -> return false
            }
        }
        AsukaKind.SEGMENTED -> if (prop == AsukaProp.Segmented.SEGMENTS) (n.view as AsukaSegmented).setSegments(v as ByteBuffer) else return false
        AsukaKind.RICH_TEXT -> when (prop) {
            AsukaProp.RichText.CONTENT -> (n.view as AsukaRichText).setContent(v as ByteBuffer)
            AsukaProp.RichText.INLINES -> (n.view as AsukaRichText).setInlines(v as ByteBuffer)
            else -> return false
        }
        else -> return false
    }
    return true
}

/** Puts a scroll view or list in a SwipeRefreshLayout, which its parent then holds. */
private fun AsukaRuntime.setRefreshable(n: AsukaNode, on: Boolean) {
    var srl = n.wrapper as? SwipeRefreshLayout
    if (srl == null && on) {
        val s = SwipeRefreshLayout(themed)
        s.setColorSchemeColors(accentColor())
        s.setProgressBackgroundColorSchemeColor(MaterialColors.getColor(themed, com.google.android.material.R.attr.colorSurfaceContainerHigh, Color.WHITE))
        val v = n.view
        val parent = v.parent as? ViewGroup
        val i = parent?.indexOfChild(v) ?: -1
        val lp = v.layoutParams
        parent?.removeViewAt(i)
        s.addView(v, ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        if (parent != null) parent.addView(s, i, lp)
        val id = n.id
        s.setOnRefreshListener { emit(id, AsukaEventKind.REFRESH) { AsukaEvents.refresh(id) } }
        // A list's view is a frame around its RecyclerView: that's what scrolls.
        n.list?.let { l -> s.setOnChildScrollUpCallback { _, _ -> l.recycler.canScrollVertically(-1) } }
        n.wrapper = s
        srl = s
    }
    srl?.isEnabled = on
}

/** A title, in the error color when it's destructive (and enabled). */
fun AsukaRuntime.styled(a: AsukaAction): CharSequence {
    // A disabled one is dimmed as the others are.
    if (a.role != AsukaAlertButtonRole.DESTRUCTIVE || a.disabled) return a.title
    val s = SpannableString(a.title)
    s.setSpan(ForegroundColorSpan(MaterialColors.getColor(activity ?: themed, androidx.appcompat.R.attr.colorError, Color.RED)), 0, s.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    return s
}

fun AsukaRuntime.showMenu(n: AsukaNode) {
    val act = activity ?: return
    val m = PopupMenu(act, n.outer)
    if (fillMenu(m.menu, n.actions)) m.setForceShowIcon(true)
    val id = n.id
    m.setOnMenuItemClickListener { item ->
        if (item.hasSubMenu() || item.itemId < 0) return@setOnMenuItemClickListener false
        emit(id, AsukaEventKind.ACTION) { AsukaEvents.action(id, item.itemId.toLong()) }
        true
    }
    m.show()
}

/**
 * Adds actions to a menu, each item's ID its number: disabled items
 * disabled, a checked one checked, a submenu a SubMenu and a section a
 * group set off by dividers, under its title (a disabled item, as
 * Android's menus have no headers). A section with a checked item is a
 * checkable group (radio buttons when exactly one is checked, as a
 * choice); elsewhere only a checked item shows its check. A cancel
 * action is left out: the menu's dismissal. True: an item has an icon.
 */
fun AsukaRuntime.fillMenu(menu: android.view.Menu, actions: List<AsukaAction>, group: Int = 0): Boolean {
    var icons = false
    var groups = group
    var sections = false
    fun add(menu: android.view.Menu, actions: List<AsukaAction>, group: Int) {
        for (a in actions) {
            if (a.role == AsukaAlertButtonRole.CANCEL) continue
            when {
                a.inline -> {
                    val g = ++groups
                    sections = true
                    if (a.title.isNotEmpty()) {
                        val header = SpannableString(a.title)
                        header.setSpan(android.text.style.RelativeSizeSpan(0.875f), 0, header.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
                        menu.add(g, -1 - a.index.toInt(), a.index.toInt(), header).isEnabled = false
                    }
                    add(menu, a.items, g)
                    val checked = a.items.count { it.checked && !it.submenu }
                    if (checked > 0) {
                        // Its choices: radio buttons for one, checkboxes for more.
                        menu.setGroupCheckable(g, true, checked == 1)
                        // Only the checked: checking an exclusive item (even
                        // "unchecking" it) unchecks the others.
                        for (c in a.items) if (!c.submenu && c.checked) menu.findItem(c.index.toInt())?.isChecked = true
                        if (a.title.isNotEmpty()) menu.findItem(-1 - a.index.toInt())?.isCheckable = false
                    }
                }
                a.submenu -> {
                    val sub = menu.addSubMenu(group, a.index.toInt(), a.index.toInt(), styled(a))
                    if (a.icon.isNotEmpty()) { sub.item.icon = icon(a); icons = true }
                    sub.item.isEnabled = !a.disabled
                    sub.setHeaderTitle(a.title)
                    add(sub, a.items, group)
                }
                else -> {
                    val item = menu.add(group, a.index.toInt(), a.index.toInt(), styled(a))
                    if (a.icon.isNotEmpty()) { item.icon = icon(a); icons = true }
                    item.isEnabled = !a.disabled
                    if (a.checked) {
                        item.isCheckable = true
                        item.isChecked = true
                    }
                }
            }
        }
    }
    add(menu, actions, group)
    if (sections) menu.setGroupDividerEnabled(true)
    return icons
}

/** An action's icon, tinted as Material's menus tint theirs (the error color when destructive). */
private fun AsukaRuntime.icon(a: AsukaAction) = symbol(a.icon)?.apply {
    setTint(MaterialColors.getColor(themed, if (a.role == AsukaAlertButtonRole.DESTRUCTIVE && !a.disabled) androidx.appcompat.R.attr.colorError
        else com.google.android.material.R.attr.colorOnSurfaceVariant, Color.DKGRAY))
    if (a.disabled) alpha = 97 // Material's disabled content: 38%
}

/** A tap on a view or button: its menu, if it opens on a tap, else Press. */
fun AsukaRuntime.clicked(id: Int) {
    val n = nodes[id] ?: return
    if (n.actions.isNotEmpty() && (n.kind == AsukaKind.BUTTON || n.menuTrigger == AsukaMenuTrigger.TAP)) showMenu(n)
    else emit(id, AsukaEventKind.PRESS) { AsukaEvents.press(id) }
}

/**
 * A long click on a view: a drag, if it has DragItems; else its menu, if
 * it opens on a long press, or LongPress. Each takes the touch: the view
 * is unpressed at once, and the release fires no Press.
 */
fun AsukaRuntime.longClicked(id: Int): Boolean {
    val n = nodes[id] ?: return false
    if (n.dragItems.isNotEmpty() && startDrag(n)) return true
    if (n.actions.isNotEmpty() && n.kind != AsukaKind.BUTTON && n.menuTrigger == AsukaMenuTrigger.LONG_PRESS) {
        cancelPress(n)
        showMenu(n)
        return true
    }
    if (!n.listens(AsukaEventKind.LONG_PRESS)) return false
    cancelPress(n)
    emit(id, AsukaEventKind.LONG_PRESS) { AsukaEvents.longPress(id) }
    return true
}

/**
 * Wires a view's taps and long presses to its menu or its listeners, and
 * its pressed and hover states to Pressed and Hover. A view listening for
 * Pressed is pressable (for Pressed alone, without being an action to
 * TalkBack); one listening for either draws its own states, so it has
 * no ripple.
 */
fun AsukaRuntime.wirePresses(n: AsukaNode) {
    if (n.kind != AsukaKind.VIEW) return
    val v = n.view
    val id = n.id
    val tap = n.listens(AsukaEventKind.PRESS) || (n.actions.isNotEmpty() && n.menuTrigger == AsukaMenuTrigger.TAP)
    val long = n.listens(AsukaEventKind.LONG_PRESS) || (n.actions.isNotEmpty() && n.menuTrigger == AsukaMenuTrigger.LONG_PRESS) || n.dragItems.isNotEmpty()
    val pressed = n.listens(AsukaEventKind.PRESSED)
    val hover = n.listens(AsukaEventKind.HOVER)
    if (tap) v.setOnClickListener { clicked(id) }
    if (long) v.setOnLongClickListener { longClicked(id) }
    if (pressed) v.isClickable = true
    (v as? AsukaContainer)?.let { c ->
        c.pressOnly = pressed && !tap
        c.onPressedChange = if (pressed) ({ on -> emit(id, AsukaEventKind.PRESSED) { AsukaEvents.pressed(id, on) } }) else null
        c.onHoverChange = if (hover) ({ on -> emit(id, AsukaEventKind.HOVER) { AsukaEvents.hover(id, on) } }) else null
    }
    if (pressed || hover) {
        if (n.ripple != null && v.foreground === n.ripple) v.foreground = null
        n.ripple = null
    } else if ((tap || long) && v.foreground == null) {
        val tv = TypedValue()
        themed.theme.resolveAttribute(android.R.attr.selectableItemBackground, tv, true)
        v.foreground = androidx.core.content.ContextCompat.getDrawable(themed, tv.resourceId)
        n.ripple = v.foreground
    }
}

/** A dialog's window is laid out in the system's direction unless it's told: the app's. */
fun AsukaRuntime.directDialog(d: android.app.Dialog) {
    if (shownLocale != null) d.window?.decorView?.layoutDirection = if (rtl) View.LAYOUT_DIRECTION_RTL else View.LAYOUT_DIRECTION_LTR
}

fun AsukaRuntime.present(n: AsukaNode) {
    val act = activity ?: return
    when (n.kind) {
        AsukaKind.SHEET -> if (n.fullScreen) presentFullScreen(act, n) else presentSheet(act, n)
        AsukaKind.POPOVER -> n.popover?.present()
        AsukaKind.ALERT -> presentAlert(act, n)
        AsukaKind.SNACKBAR -> (n.view as AsukaSnackbar).present(act)
    }
}

private fun AsukaRuntime.presentSheet(act: AsukaActivity, n: AsukaNode) {
    val d = BottomSheetDialog(dialogContext(act))
    directDialog(d)
    val screen = n.view as AsukaScreen
    (screen.parent as? ViewGroup)?.removeView(screen)
    val box = LinearLayout(act).apply { orientation = LinearLayout.VERTICAL }
    var handle = 0
    if (n.grabber) {
        val h = BottomSheetDragHandleView(act)
        box.addView(h)
        h.measure(View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED), View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
        handle = h.measuredHeight
    }
    // The content's height is the sheet's at its largest detent.
    val decor = act.window.decorView
    val bars = ViewCompat.getRootWindowInsets(decor)?.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout()) ?: Insets.NONE
    val full = decor.height - bars.top - bars.bottom - handle
    val medium = n.detents and 1L != 0L
    val large = n.detents and 2L != 0L || !medium
    val h = if (large) full else (decor.height / 2 - bars.bottom - handle)
    box.addView(screen, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, h))
    d.setContentView(box)
    val b = d.behavior
    b.skipCollapsed = true
    if (medium && large) {
        b.isFitToContents = false
        b.halfExpandedRatio = 0.5f
        b.expandedOffset = bars.top
        b.state = BottomSheetBehavior.STATE_HALF_EXPANDED
    } else {
        b.state = BottomSheetBehavior.STATE_EXPANDED
    }
    b.addBottomSheetCallback(object : BottomSheetBehavior.BottomSheetCallback() {
        override fun onStateChanged(sheet: View, state: Int) {
            if (state == BottomSheetBehavior.STATE_EXPANDED || state == BottomSheetBehavior.STATE_HALF_EXPANDED) screen.report()
        }
        override fun onSlide(sheet: View, offset: Float) {}
    })
    // The keyboard: the sheet goes to its largest detent, as on iOS, and
    // its content follows the keyboard frame by frame.
    val sheetDecor = d.window?.decorView ?: box
    ViewCompat.setWindowInsetsAnimationCallback(sheetDecor, object : WindowInsetsAnimationCompat.Callback(DISPATCH_MODE_CONTINUE_ON_SUBTREE) {
        override fun onStart(a: WindowInsetsAnimationCompat, bounds: WindowInsetsAnimationCompat.BoundsCompat): WindowInsetsAnimationCompat.BoundsCompat {
            if (ViewCompat.getRootWindowInsets(box)?.isVisible(WindowInsetsCompat.Type.ime()) == true && large) b.state = BottomSheetBehavior.STATE_EXPANDED
            return bounds
        }
        override fun onProgress(insets: WindowInsetsCompat, running: MutableList<WindowInsetsAnimationCompat>): WindowInsetsCompat {
            screen.ime = insets.getInsets(WindowInsetsCompat.Type.ime()).bottom
            screen.report()
            return insets
        }
        override fun onEnd(a: WindowInsetsAnimationCompat) {
            screen.ime = -1
            screen.report()
        }
    })
    val id = n.id
    d.setOnDismissListener {
        sheetBack.remove(id)
        if (!n.dying) {
            n.dying = true
            emit(id, AsukaEventKind.DISMISSED) { AsukaEvents.dismissed(id) }
        }
    }
    // A keyboard that shows without animating: catch it on layout.
    var imeShown = false
    box.viewTreeObserver.addOnGlobalLayoutListener {
        val shown = ViewCompat.getRootWindowInsets(box)?.isVisible(WindowInsetsCompat.Type.ime()) == true
        if (shown == imeShown) return@addOnGlobalLayoutListener
        imeShown = shown
        if (shown && large) b.state = BottomSheetBehavior.STATE_EXPANDED
        screen.report()
    }
    // ui.OnBack: what would dismiss it asks the core while it intercepts.
    val back = object : androidx.activity.OnBackPressedCallback(false) {
        override fun handleOnBackPressed() { emit(id, AsukaEventKind.BACK) { AsukaEvents.back(id) } }
    }
    d.onBackPressedDispatcher.addCallback(back)
    sheetBack[id] = back
    d.setOnShowListener {
        d.findViewById<View>(com.google.android.material.R.id.touch_outside)?.setOnClickListener {
            if (n.intercepts) emit(id, AsukaEventKind.BACK) { AsukaEvents.back(id) }
            else if (d.isShowing) d.cancel()
        }
    }
    n.dialog = d
    sheetIntercepts(n)
    d.show()
}

/**
 * A full-screen sheet (SheetProps.FullScreen): Material's full-screen
 * dialog. A window as big as the screen in a theme of its own
 * (ThemeOverlay.Asuka.FullScreenDialog), sliding up; drawn edge to edge,
 * so its content reports the screen's insets (the status bar, the
 * cutout, the navigation bar, the keyboard) as the activity's does.
 * Nothing outside it to tap, nor a swipe: back dismisses it, shrinking
 * with the gesture as it goes (predictive back), unless it intercepts
 * (ui.OnBack), when the core decides and it doesn't move.
 */
private fun AsukaRuntime.presentFullScreen(act: AsukaActivity, n: AsukaNode) {
    val ctx = dialogContext(act)
    val d = androidx.activity.ComponentDialog(ctx, R.style.ThemeOverlay_Asuka_FullScreenDialog)
    val screen = n.view as AsukaScreen
    (screen.parent as? ViewGroup)?.removeView(screen)
    d.setContentView(screen, ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
    directDialog(d)
    val w = d.window
    if (w != null) {
        w.setLayout(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT)
        androidx.core.view.WindowCompat.setDecorFitsSystemWindows(w, false)
        @Suppress("DEPRECATION") run {
            w.statusBarColor = Color.TRANSPARENT
            w.navigationBarColor = Color.TRANSPARENT
        }
        w.isNavigationBarContrastEnforced = true // a scrim behind three-button navigation, as the activity's
        w.setSoftInputMode(android.view.WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE)
    }
    n.dialog = d
    styleFullScreen(n, isNight(act.resources.configuration))
    // Its insets change (the keyboard, a rotation): its environment again,
    // the keyboard frame by frame as it moves.
    ViewCompat.setOnApplyWindowInsetsListener(screen) { _, ins -> screen.post { screen.report() }; ins }
    ViewCompat.setWindowInsetsAnimationCallback(screen, object : WindowInsetsAnimationCompat.Callback(DISPATCH_MODE_CONTINUE_ON_SUBTREE) {
        override fun onProgress(insets: WindowInsetsCompat, running: MutableList<WindowInsetsAnimationCompat>): WindowInsetsCompat {
            screen.ime = insets.getInsets(WindowInsetsCompat.Type.ime()).bottom
            screen.report()
            return insets
        }
        override fun onEnd(a: WindowInsetsAnimationCompat) {
            screen.ime = -1
            screen.report()
        }
    })
    val id = n.id
    d.setOnDismissListener {
        if (!n.dying) {
            n.dying = true
            emit(id, AsukaEventKind.DISMISSED) { AsukaEvents.dismissed(id) }
        }
    }
    // Back: predictive, as Material's full-screen surfaces go back, the
    // content shrinking toward the edge it's swiped from, its corners
    // rounding; or, while it intercepts, the core's to decide.
    val dp = density
    val radius = 28f * dp
    var corner = 0f
    screen.outlineProvider = object : android.view.ViewOutlineProvider() {
        override fun getOutline(v: View, o: android.graphics.Outline) = o.setRoundRect(0, 0, v.width, v.height, corner)
    }
    fun shape(p: Float, edge: Int) {
        val s = 1f - 0.1f * p
        screen.scaleX = s
        screen.scaleY = s
        val shift = maxOf(0f, screen.width / 20f - 8f * dp) * p
        screen.translationX = when (edge) {
            BackEventCompat.EDGE_LEFT -> shift
            BackEventCompat.EDGE_RIGHT -> -shift
            else -> 0f
        }
        corner = radius * p
        screen.clipToOutline = p > 0f
        screen.invalidateOutline()
    }
    val back = object : OnBackPressedCallback(true) {
        var shrinking = false
        override fun handleOnBackStarted(e: BackEventCompat) {
            shrinking = !n.intercepts && android.animation.ValueAnimator.areAnimatorsEnabled()
        }
        override fun handleOnBackProgressed(e: BackEventCompat) {
            if (shrinking) shape(e.progress, e.swipeEdge)
        }
        override fun handleOnBackCancelled() {
            if (!shrinking) return
            shrinking = false
            android.animation.ValueAnimator.ofFloat((1f - screen.scaleX) / 0.1f, 0f).apply {
                duration = 150
                interpolator = emphasized
                addUpdateListener { shape(it.animatedValue as Float, if (screen.translationX < 0) BackEventCompat.EDGE_RIGHT else BackEventCompat.EDGE_LEFT) }
            }.start()
        }
        override fun handleOnBackPressed() {
            shrinking = false
            if (n.intercepts) {
                shape(0f, BackEventCompat.EDGE_LEFT)
                emit(id, AsukaEventKind.BACK) { AsukaEvents.back(id) }
            } else if (d.isShowing) {
                d.cancel()
            }
        }
    }
    d.onBackPressedDispatcher.addCallback(back)
    d.setCanceledOnTouchOutside(false)
    d.show()
}

/**
 * A full-screen sheet's look, which follows the app's light or dark
 * (the system's dark mode, or ui.SetColorScheme) while it shows: its
 * surface, and the system bars' icons over it, dark on a light app and
 * light on a dark one, as the activity's.
 */
internal fun AsukaRuntime.styleFullScreen(n: AsukaNode, night: Boolean) {
    val d = n.dialog ?: return
    n.view.setBackgroundColor(MaterialColors.getColor(themed, com.google.android.material.R.attr.colorSurface, Color.WHITE))
    val w = d.window ?: return
    androidx.core.view.WindowCompat.getInsetsController(w, w.decorView).run {
        isAppearanceLightStatusBars = !night
        isAppearanceLightNavigationBars = !night
    }
}

private fun AsukaRuntime.presentAlert(act: AsukaActivity, n: AsukaNode) {
    val ctx = dialogContext(act)
    val b = MaterialAlertDialogBuilder(ctx)
    if (n.alertTitle.isNotEmpty()) b.setTitle(n.alertTitle)
    val acts = n.actions
    val id = n.id
    fun acted(i: Int) {
        n.dying = true // a button isn't a dismissal
        emit(id, AsukaEventKind.ACTION) { AsukaEvents.action(id, acts[i].index) }
    }
    val cancel = acts.indexOfFirst { it.role == AsukaAlertButtonRole.CANCEL }
    val others = acts.indices.filter { it != cancel }
    // Each button's dialog button (or the list's row), for Disabled.
    var list: android.widget.ArrayAdapter<CharSequence>? = null
    val buttons = HashMap<Int, Int>() // action → DialogInterface.BUTTON_*
    // An action sheet, or more buttons than a dialog has, is a list.
    if (n.alertStyle == AsukaAlertStyle.ACTION_SHEET || others.size > 2) {
        // The dialog's own list item layout, its rows dimmed when disabled.
        val ta = ctx.obtainStyledAttributes(null, androidx.appcompat.R.styleable.AlertDialog, androidx.appcompat.R.attr.alertDialogStyle, 0)
        val layout = ta.getResourceId(androidx.appcompat.R.styleable.AlertDialog_listItemLayout, android.R.layout.simple_list_item_1)
        ta.recycle()
        val adapter = object : android.widget.ArrayAdapter<CharSequence>(ctx, layout, android.R.id.text1, others.map { styled(acts[it]) }) {
            override fun areAllItemsEnabled() = others.none { n.actions.getOrNull(it)?.disabled == true }
            override fun isEnabled(position: Int) = n.actions.getOrNull(others[position])?.disabled != true
            override fun getView(position: Int, convertView: View?, parent: ViewGroup): View =
                super.getView(position, convertView, parent).also { it.isEnabled = isEnabled(position) }
        }
        list = adapter
        b.setAdapter(adapter) { _, which -> if (adapter.isEnabled(which)) acted(others[which]) }
    } else {
        if (n.alertMessage.isNotEmpty()) b.setMessage(n.alertMessage)
        // The primary action is last, on the end; another is neutral.
        others.lastOrNull()?.let { i -> b.setPositiveButton(acts[i].title) { _, _ -> acted(i) }; buttons[i] = android.content.DialogInterface.BUTTON_POSITIVE }
        if (others.size == 2) { b.setNeutralButton(acts[others[0]].title) { _, _ -> acted(others[0]) }; buttons[others[0]] = android.content.DialogInterface.BUTTON_NEUTRAL }
        if (acts.isEmpty()) b.setPositiveButton(android.R.string.ok, null)
    }
    if (cancel >= 0) { b.setNegativeButton(acts[cancel].title) { _, _ -> acted(cancel) }; buttons[cancel] = android.content.DialogInterface.BUTTON_NEGATIVE }
    // Text fields, under the message: an IME action goes on to the next,
    // and from the last presses the default button, if it's enabled.
    var dialog: androidx.appcompat.app.AlertDialog? = null
    val fields = if (n.alertStyle == AsukaAlertStyle.ALERT && n.alertFields.isNotEmpty()) alertFieldsView(ctx, n) {
        val d = dialog ?: return@alertFieldsView
        val def = acts.indexOfFirst { it.role == AsukaAlertButtonRole.DEFAULT }
        if (def < 0 || n.actions.getOrNull(def)?.disabled == true) return@alertFieldsView
        val which = buttons[def]
        if (which != null) d.getButton(which)?.takeIf { it.isEnabled }?.performClick()
        else { acted(def); d.dismiss() }
    } else null
    fields?.let { b.setView(it.first) }
    val d = b.create()
    dialog = d
    directDialog(d)
    d.setOnDismissListener {
        n.alertRefresh = null
        if (!n.dying) {
            n.dying = true
            emit(id, AsukaEventKind.DISMISSED) { AsukaEvents.dismissed(id) }
        }
    }
    n.dialog = d
    // Its buttons follow Disabled while it shows (set again with Buttons).
    n.alertRefresh = {
        for ((i, which) in buttons) d.getButton(which)?.isEnabled = n.actions.getOrNull(i)?.disabled != true
        list?.notifyDataSetChanged()
    }
    if (fields != null) {
        // The first field has the focus, with the keyboard up.
        d.window?.setSoftInputMode(android.view.WindowManager.LayoutParams.SOFT_INPUT_STATE_ALWAYS_VISIBLE or
            android.view.WindowManager.LayoutParams.SOFT_INPUT_ADJUST_RESIZE)
        fields.second.firstOrNull()?.requestFocus()
    }
    d.show()
    n.alertRefresh?.invoke()
    val error = MaterialColors.getColor(act, androidx.appcompat.R.attr.colorError, Color.RED)
    others.lastOrNull()?.let { if (acts[it].role == AsukaAlertButtonRole.DESTRUCTIVE) d.getButton(android.app.AlertDialog.BUTTON_POSITIVE)?.setTextColor(error) }
    if (others.size == 2 && acts[others[0]].role == AsukaAlertButtonRole.DESTRUCTIVE) d.getButton(android.app.AlertDialog.BUTTON_NEUTRAL)?.setTextColor(error)
}

/** Content types typed as they are: no capitals, no corrections. */
private val literalContent = setOf("username", "password", "newPassword", "oneTimeCode", "email", "url")

/**
 * An alert's text fields, as Material's dialog with text fields has them:
 * outlined TextInputLayouts in a column, inset as the dialog's content is
 * (a password's with its toggle). Typing reports FieldChanged; the IME's
 * action goes on to the next field, and from the last calls [submit].
 */
private fun AsukaRuntime.alertFieldsView(ctx: Context, n: AsukaNode, submit: () -> Unit): Pair<View, List<android.widget.EditText>> {
    val dp = ctx.resources.displayMetrics.density
    val pad = TypedValue().let { tv ->
        if (ctx.theme.resolveAttribute(androidx.appcompat.R.attr.dialogPreferredPadding, tv, true)) TypedValue.complexToDimensionPixelSize(tv.data, ctx.resources.displayMetrics)
        else (24 * dp).toInt()
    }
    val column = LinearLayout(ctx).apply {
        orientation = LinearLayout.VERTICAL
        // The title or message above has its own space below it.
        setPaddingRelative(pad, 0, pad, 0)
    }
    val id = n.id
    val count = n.alertFields.size
    val edits = ArrayList<android.widget.EditText>()
    for ((i, f) in n.alertFields.withIndex()) {
        val layout = com.google.android.material.textfield.TextInputLayout(ctx, null, com.google.android.material.R.attr.textInputOutlinedStyle)
        val e = com.google.android.material.textfield.TextInputEditText(layout.context)
        e.isSingleLine = true
        e.textAlignment = View.TEXT_ALIGNMENT_VIEW_START
        val typeface = e.typeface
        e.inputType = when {
            f.secure && (f.keyboard == AsukaKeyboard.NUMBER || f.keyboard == AsukaKeyboard.PHONE) ->
                android.text.InputType.TYPE_CLASS_NUMBER or android.text.InputType.TYPE_NUMBER_VARIATION_PASSWORD
            f.secure -> android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_VARIATION_PASSWORD
            else -> when (f.keyboard) {
                AsukaKeyboard.NUMBER -> android.text.InputType.TYPE_CLASS_NUMBER
                AsukaKeyboard.DECIMAL -> android.text.InputType.TYPE_CLASS_NUMBER or android.text.InputType.TYPE_NUMBER_FLAG_DECIMAL
                AsukaKeyboard.EMAIL -> android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS
                AsukaKeyboard.URL -> android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_VARIATION_URI
                AsukaKeyboard.PHONE -> android.text.InputType.TYPE_CLASS_PHONE
                // An account's name, a code: as typed, without suggestions.
                else -> if (f.contentType in literalContent) android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
                    else android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_FLAG_CAP_SENTENCES
            }
        }
        e.typeface = typeface // a password's input type turns it monospaced, placeholder and all
        if (f.contentType.isNotEmpty()) {
            e.setAutofillHints(autofillHint(f.contentType))
            e.importantForAutofill = View.IMPORTANT_FOR_AUTOFILL_YES
        }
        e.setText(f.text)
        e.setSelection(e.length())
        e.imeOptions = if (i < count - 1) android.view.inputmethod.EditorInfo.IME_ACTION_NEXT else android.view.inputmethod.EditorInfo.IME_ACTION_DONE
        e.addTextChangedListener(object : android.text.TextWatcher {
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
            override fun afterTextChanged(s: android.text.Editable?) {
                val t = s?.toString() ?: ""
                emit(id, AsukaEventKind.FIELD_CHANGED) { AsukaEvents.fieldChanged(id, fieldChangedPayload(i, t)) }
            }
        })
        e.setOnEditorActionListener { _, action, _ ->
            if (i < count - 1) return@setOnEditorActionListener false // on to the next field
            if (action == android.view.inputmethod.EditorInfo.IME_ACTION_DONE || action == android.view.inputmethod.EditorInfo.IME_NULL) {
                submit()
                true
            } else false
        }
        layout.hint = f.placeholder
        if (f.secure) layout.endIconMode = com.google.android.material.textfield.TextInputLayout.END_ICON_PASSWORD_TOGGLE
        layout.addView(e, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT))
        column.addView(layout, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT).apply {
            if (i > 0) topMargin = (8 * dp).toInt()
        })
        edits.add(e)
    }
    return column to edits
}

fun AsukaRuntime.haptic(kind: Int) {
    val v = activity?.window?.decorView ?: return
    val c = when (kind) {
        AsukaHaptic.SELECTION -> HapticFeedbackConstants.CLOCK_TICK
        AsukaHaptic.LIGHT -> HapticFeedbackConstants.KEYBOARD_TAP
        AsukaHaptic.MEDIUM -> HapticFeedbackConstants.VIRTUAL_KEY
        AsukaHaptic.HEAVY -> HapticFeedbackConstants.LONG_PRESS
        AsukaHaptic.SUCCESS -> HapticFeedbackConstants.CONFIRM
        AsukaHaptic.WARNING -> HapticFeedbackConstants.REJECT
        AsukaHaptic.ERROR -> HapticFeedbackConstants.REJECT
        else -> return
    }
    v.performHapticFeedback(c)
}

/**
 * The view with Style.Match key on the screen below (a List's cells aren't
 * in the tree: then the one registered with the key), if it's on screen
 * and not the zooming screen's own.
 */
fun AsukaRuntime.zoomSource(key: String, below: AsukaNode, screen: AsukaNode): View? {
    fun find(n: AsukaNode): View? {
        if (n.match == key && n.outer.isAttachedToWindow) return n.outer
        for (c in n.children) find(c)?.let { return it }
        return null
    }
    find(below)?.let { return it }
    val m = matchViews[key] ?: return null
    val v = m.outer
    if (!v.isAttachedToWindow) return null
    var p = v.parent
    while (p != null) {
        if (p === screen.view) return null
        p = p.parent
    }
    return v
}
