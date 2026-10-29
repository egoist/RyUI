package dev.nui

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

/** An item of a menu, an alert or a toolbar. */
data class NuiAction(val title: String, val role: Int)

/** Decodes actions: u32 count, then per item u8 role and a u32-length title. */
fun decodeActions(b: ByteBuffer): List<NuiAction> {
    if (b.remaining() < 4) return emptyList()
    val n = b.int
    return List(n) {
        val role = b.get().toInt() and 0xff
        val a = ByteArray(b.int)
        b.get(a)
        NuiAction(String(a, Charsets.UTF_8), role)
    }
}

// Material 3 motion: emphasized easing, medium durations.
private val emphasized = PathInterpolator(0.2f, 0f, 0f, 1f)
private const val pushMillis = 350L
private const val popMillis = 300L

/**
 * A screen of a Stack, a page of Tabs or a sheet's content: sized by its
 * container, it reports its environment (size, the system bars and
 * keyboard over it) to the core, which lays it out.
 */
class NuiScreen(context: Context, val nodeID: Int, private val rt: NuiRuntime) : NuiContainer(context) {
    var title = ""
    var icon = ""
    var actions: List<NuiAction> = emptyList()
    var backGesture = true
    /** The keyboard's height over the window as it animates, in px; -1: as the window's insets say. */
    var ime = -1
    private var lastEnv: NuiEnvironment? = null

    override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
        super.onLayout(changed, l, t, r, b)
        report()
    }

    fun report() {
        val n = rt.nodes[nodeID] ?: return
        if (width == 0 || height == 0 || n.dying) return
        var e = rt.environment(this, if (ime >= 0) ime else null)
        // A sheet never reaches under the status bar, and pads itself above
        // the navigation bar; it moves without a layout, so its position
        // says nothing.
        if (n.kind == NuiKind.SHEET) e = e.copy(safeTop = 0f, safeBottom = 0f)
        if (e == lastEnv) return
        lastEnv = e
        rt.send(NuiEvents.environment(nodeID, e))
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
class NuiStack(context: Context, val nodeID: Int, private val rt: NuiRuntime) : ViewGroup(context) {
    var bar = makeBar(context)
        private set
    /** The screens, top last; one being popped has left already. */
    val screens = ArrayList<NuiNode>()
    val top: NuiNode? get() = screens.lastOrNull()
    private var backing = false

    init {
        addView(bar)
    }

    private fun makeBar(ctx: Context) = MaterialToolbar(ctx).apply {
        setBackgroundColor(MaterialColors.getColor(ctx, com.google.android.material.R.attr.colorSurface, Color.WHITE))
        setNavigationOnClickListener { rt.userPop(this@NuiStack) }
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

    private val slide get() = 0.08f * width.coerceAtLeast(1)

    fun insert(n: NuiNode, index: Int) {
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
    fun remove(n: NuiNode, animated: Boolean) {
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
            // The predictive back gesture already shows what's below.
            backing = false
            v.animate().alpha(0f).translationX(v.translationX + slide).setDuration(popMillis / 2).setInterpolator(emphasized)
                .withEndAction { removeView(v) }.start()
            return
        }
        b.translationX = -slide
        b.alpha = 0f
        b.animate().translationX(0f).alpha(1f).setDuration(popMillis).setInterpolator(emphasized).start()
        v.animate().translationX(slide).alpha(0f).setDuration(popMillis).setInterpolator(emphasized)
            .withEndAction { removeView(v) }.start()
    }

    // Predictive back: the top screen shrinks and follows the gesture,
    // showing the one below.

    fun backStarted() {
        val t = top ?: return
        if (screens.size < 2 || !(t.view as NuiScreen).backGesture) return
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
        v.translationX = (if (e.swipeEdge == BackEventCompat.EDGE_RIGHT) -1 else 1) * e.progress * slide
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
        val s = t.view as NuiScreen
        bar.title = s.title
        if (screens.size > 1) {
            bar.setNavigationIcon(R.drawable.nui_ic_arrow_back)
            bar.setNavigationIconTint(MaterialColors.getColor(bar, com.google.android.material.R.attr.colorOnSurface))
            bar.setNavigationContentDescription(androidx.appcompat.R.string.abc_action_bar_up_description)
        } else {
            bar.navigationIcon = null
        }
        bar.menu.clear()
        for ((i, a) in s.actions.withIndex()) {
            bar.menu.add(0, i, i, rt.styled(a)).setShowAsAction(MenuItem.SHOW_AS_ACTION_IF_ROOM)
        }
        val id = t.id
        bar.setOnMenuItemClickListener { item ->
            rt.emit(id, NuiEventKind.ACTION) { NuiEvents.action(id, item.itemId.toLong()) }
            true
        }
    }
}

private val tabIcons = mapOf(
    "house" to R.drawable.nui_ic_house,
    "magnifyingglass" to R.drawable.nui_ic_search,
    "gearshape" to R.drawable.nui_ic_settings,
    "person" to R.drawable.nui_ic_person,
    "star" to R.drawable.nui_ic_star,
    "list.bullet" to R.drawable.nui_ic_list,
    "heart" to R.drawable.nui_ic_heart,
    "plus" to R.drawable.nui_ic_add,
)

/** Tabs: a Material navigation bar under its pages. */
class NuiTabs(context: Context, val nodeID: Int, private val rt: NuiRuntime) : ViewGroup(context) {
    var bar = makeBar(context)
        private set
    val pages = ArrayList<NuiNode>()
    var selected = 0
        private set
    private var selecting = false
    val current: NuiNode? get() = pages.getOrNull(selected)

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
                rt.emit(nodeID, NuiEventKind.SELECTED) { NuiEvents.selected(nodeID, i) }
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
    }

    fun insert(n: NuiNode, index: Int) {
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

    fun remove(n: NuiNode) {
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
            val s = p.view as NuiScreen
            val item = bar.menu.add(0, i, i, s.title)
            tabIcons[s.icon.removeSuffix(".fill")]?.let { item.setIcon(it) }
        }
        if (selected < bar.menu.size()) bar.selectedItemId = selected
        selecting = false
    }

    override fun onMeasure(widthSpec: Int, heightSpec: Int) {
        val w = MeasureSpec.getSize(widthSpec)
        val h = MeasureSpec.getSize(heightSpec)
        setMeasuredDimension(w, h)
        val ins = rt.insetsOver(this, w, h)
        bar.setPadding(ins.left, 0, ins.right, ins.bottom)
        bar.measure(MeasureSpec.makeMeasureSpec(w, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(h, MeasureSpec.AT_MOST))
        val ph = maxOf(0, h - bar.measuredHeight)
        for (i in 0 until childCount) {
            val c = getChildAt(i)
            if (c !== bar) c.measure(MeasureSpec.makeMeasureSpec(w, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(ph, MeasureSpec.EXACTLY))
        }
    }

    override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
        val bh = bar.measuredHeight
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
fun NuiRuntime.insetsOver(v: View, w: Int, h: Int): Insets {
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
fun NuiRuntime.environment(v: View, keyboardPx: Int? = null): NuiEnvironment {
    val d = density
    val ins = insetsOver(v, v.width, v.height)
    val root = v.rootView
    val act = activity
    val kb = keyboardPx ?: if (act != null && root === act.window.decorView) act.keyboardPx
        else ViewCompat.getRootWindowInsets(v)?.getInsets(WindowInsetsCompat.Type.ime())?.bottom ?: 0
    val below = maxOf(0, root.height - (offsetInWindow(v)[1] + v.height))
    val c = (act?.resources ?: app.resources).configuration
    return NuiEnvironment(
        width = v.width / d, height = v.height / d,
        safeTop = ins.top / d, safeRight = ins.right / d, safeBottom = ins.bottom / d, safeLeft = ins.left / d,
        keyboard = maxOf(0, kb - below) / d,
        scale = d,
        fontScale = c.fontScale,
        dark = (c.uiMode and android.content.res.Configuration.UI_MODE_NIGHT_MASK) == android.content.res.Configuration.UI_MODE_NIGHT_YES,
        locale = c.locales[0].toLanguageTag(),
    )
}

/** The window's insets changed: bars move, screens report again. */
fun NuiRuntime.insetsChanged() {
    for (n in nodes.values) {
        when (val v = n.view) {
            is NuiStack, is NuiTabs -> v.requestLayout()
            is NuiScreen -> v.report()
        }
    }
}

/** The keyboard moved: screens report again. */
fun NuiRuntime.reportScreens() {
    for (n in nodes.values) (n.view as? NuiScreen)?.report()
}

/** Whether n shows: every stack above it shows its branch, every tabs its page. */
private fun NuiRuntime.shown(n: NuiNode): Boolean {
    var c = n
    var p = n.parent
    while (p != null) {
        if (c.dying) return false
        when (val v = p.view) {
            is NuiStack -> if (v.top !== c) return false
            is NuiTabs -> if (v.current !== c) return false
        }
        c = p
        p = p.parent
    }
    return c.id == rootID
}

/** The stack back pops: the innermost shown one with a screen to go back to. */
fun NuiRuntime.activeStack(): NuiStack? {
    var best: NuiStack? = null
    var bestDepth = -1
    for (n in stacks) {
        val s = n.view as NuiStack
        if (s.screens.size < 2 || !shown(n)) continue
        var depth = 0
        var p = n.parent
        while (p != null) { depth++; p = p.parent }
        if (depth > bestDepth) { best = s; bestDepth = depth }
    }
    return best
}

fun NuiRuntime.makeBackCallback() = object : OnBackPressedCallback(false) {
    private var stack: NuiStack? = null

    override fun handleOnBackStarted(backEvent: BackEventCompat) {
        stack = activeStack()?.also { it.backStarted() }
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
    }
}

/** The user popped a stack's top screen: tell the core, which destroys it. */
fun NuiRuntime.userPop(s: NuiStack) {
    val top = s.top ?: return
    if (s.screens.size < 2) return
    top.dying = true
    nodes[s.nodeID]?.children?.remove(top)
    s.remove(top, animated = true)
    val id = top.id.toLong()
    emit(s.nodeID, NuiEventKind.POPPED) { NuiEvents.popped(s.nodeID, id) }
    updateBack()
}

/** Inserts into a Stack or Tabs; false: an ordinary insert. */
fun NuiRuntime.insertNav(p: NuiNode, c: NuiNode, index: Int): Boolean {
    val v = p.view
    if (v !is NuiStack && v !is NuiTabs) return false
    val i = minOf(index, p.children.size)
    p.children.add(i, c)
    c.parent = p
    when (v) {
        is NuiStack -> v.insert(c, i)
        is NuiTabs -> v.insert(c, i)
    }
    return true
}

/** Destroys a screen, sheet or alert; false: an ordinary destroy. */
fun NuiRuntime.destroyNav(n: NuiNode): Boolean {
    if (n.dying) return true // the user popped or dismissed it
    val p = n.parent
    val alive = p != null && nodes[p.id] === p
    when {
        alive && p!!.view is NuiStack -> {
            n.dying = true
            p.children.remove(n)
            val s = p.view as NuiStack
            s.remove(n, animated = s.top === n && s.isAttachedToWindow)
            return true
        }
        alive && p!!.view is NuiTabs -> {
            n.dying = true
            p.children.remove(n)
            (p.view as NuiTabs).remove(n)
            return true
        }
        n.kind == NuiKind.SHEET || n.kind == NuiKind.ALERT -> {
            n.dying = true
            n.dialog?.dismiss()
            n.dialog = null
            return true
        }
    }
    return false
}

fun NuiRuntime.setNavProp(n: NuiNode, prop: Int, v: Any): Boolean {
    when (n.kind) {
        NuiKind.SCREEN -> {
            val s = n.view as NuiScreen
            when (prop) {
                NuiProp.Screen.TITLE -> s.title = v as String
                NuiProp.Screen.ICON -> s.icon = v as String
                NuiProp.Screen.BACK_GESTURE -> s.backGesture = v as Boolean
                NuiProp.Screen.ACTIONS -> s.actions = decodeActions(v as ByteBuffer)
                else -> return false
            }
            when (val pv = n.parent?.view) {
                is NuiStack -> if (pv.top === n) pv.refreshBar()
                is NuiTabs -> pv.refreshBar()
            }
        }
        NuiKind.TABS -> if (prop == NuiProp.Tabs.SELECTED) (n.view as NuiTabs).select((v as Long).toInt()) else return false
        NuiKind.SHEET -> when (prop) {
            NuiProp.Sheet.DETENTS -> n.detents = v as Long
            NuiProp.Sheet.GRABBER -> n.grabber = v as Boolean
            else -> return false
        }
        NuiKind.ALERT -> when (prop) {
            NuiProp.Alert.TITLE -> n.alertTitle = v as String
            NuiProp.Alert.MESSAGE -> n.alertMessage = v as String
            NuiProp.Alert.BUTTONS -> n.actions = decodeActions(v as ByteBuffer)
            NuiProp.Alert.STYLE -> n.alertStyle = v as Int
            else -> return false
        }
        NuiKind.SCROLL -> when (prop) {
            NuiProp.Scroll.REFRESHABLE -> setRefreshable(n, v as Boolean)
            NuiProp.Scroll.REFRESHING -> (n.wrapper as? SwipeRefreshLayout)?.isRefreshing = v as Boolean
            else -> return false
        }
        NuiKind.LIST -> when (prop) {
            NuiProp.List.REFRESHABLE -> setRefreshable(n, v as Boolean)
            NuiProp.List.REFRESHING -> (n.wrapper as? SwipeRefreshLayout)?.isRefreshing = v as Boolean
            else -> return false
        }
        else -> return false
    }
    return true
}

/** Puts a scroll view or list in a SwipeRefreshLayout, which its parent then holds. */
private fun NuiRuntime.setRefreshable(n: NuiNode, on: Boolean) {
    var srl = n.wrapper as? SwipeRefreshLayout
    if (srl == null && on) {
        val s = SwipeRefreshLayout(themed)
        s.setColorSchemeColors(MaterialColors.getColor(themed, androidx.appcompat.R.attr.colorPrimary, Color.BLUE))
        s.setProgressBackgroundColorSchemeColor(MaterialColors.getColor(themed, com.google.android.material.R.attr.colorSurfaceContainerHigh, Color.WHITE))
        val v = n.view
        val parent = v.parent as? ViewGroup
        val i = parent?.indexOfChild(v) ?: -1
        val lp = v.layoutParams
        parent?.removeViewAt(i)
        s.addView(v, ViewGroup.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        if (parent != null) parent.addView(s, i, lp)
        val id = n.id
        s.setOnRefreshListener { emit(id, NuiEventKind.REFRESH) { NuiEvents.refresh(id) } }
        n.wrapper = s
        srl = s
    }
    srl?.isEnabled = on
}

/** A title, in the error color when it's destructive. */
fun NuiRuntime.styled(a: NuiAction): CharSequence {
    if (a.role != NuiAlertButtonRole.DESTRUCTIVE) return a.title
    val s = SpannableString(a.title)
    s.setSpan(ForegroundColorSpan(MaterialColors.getColor(activity ?: themed, androidx.appcompat.R.attr.colorError, Color.RED)), 0, s.length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
    return s
}

fun NuiRuntime.showMenu(n: NuiNode) {
    val act = activity ?: return
    val m = PopupMenu(act, n.outer)
    for ((i, a) in n.actions.withIndex()) m.menu.add(0, i, i, styled(a))
    val id = n.id
    m.setOnMenuItemClickListener { item ->
        emit(id, NuiEventKind.ACTION) { NuiEvents.action(id, item.itemId.toLong()) }
        true
    }
    m.show()
}

/** A tap on a view or button: its menu, if it opens on a tap, else Press. */
fun NuiRuntime.clicked(id: Int) {
    val n = nodes[id] ?: return
    if (n.actions.isNotEmpty() && (n.kind == NuiKind.BUTTON || n.menuTrigger == NuiMenuTrigger.TAP)) showMenu(n)
    else emit(id, NuiEventKind.PRESS) { NuiEvents.press(id) }
}

fun NuiRuntime.longClicked(id: Int): Boolean {
    val n = nodes[id] ?: return false
    if (n.actions.isNotEmpty() && n.kind != NuiKind.BUTTON && n.menuTrigger == NuiMenuTrigger.LONG_PRESS) {
        showMenu(n)
        return true
    }
    if (!n.listens(NuiEventKind.LONG_PRESS)) return false
    emit(id, NuiEventKind.LONG_PRESS) { NuiEvents.longPress(id) }
    return true
}

/** Wires a view's taps and long presses to its menu or its listeners. */
fun NuiRuntime.wirePresses(n: NuiNode) {
    if (n.kind != NuiKind.VIEW) return
    val v = n.view
    val id = n.id
    val tap = n.listens(NuiEventKind.PRESS) || (n.actions.isNotEmpty() && n.menuTrigger == NuiMenuTrigger.TAP)
    val long = n.listens(NuiEventKind.LONG_PRESS) || (n.actions.isNotEmpty() && n.menuTrigger == NuiMenuTrigger.LONG_PRESS)
    if (tap) v.setOnClickListener { clicked(id) }
    if (long) v.setOnLongClickListener { longClicked(id) }
    if ((tap || long) && v.foreground == null) {
        val tv = TypedValue()
        themed.theme.resolveAttribute(android.R.attr.selectableItemBackground, tv, true)
        v.foreground = androidx.core.content.ContextCompat.getDrawable(themed, tv.resourceId)
    }
}

fun NuiRuntime.present(n: NuiNode) {
    val act = activity ?: return
    when (n.kind) {
        NuiKind.SHEET -> presentSheet(act, n)
        NuiKind.ALERT -> presentAlert(act, n)
    }
}

private fun NuiRuntime.presentSheet(act: NuiActivity, n: NuiNode) {
    val d = BottomSheetDialog(act)
    val screen = n.view as NuiScreen
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
        if (!n.dying) {
            n.dying = true
            emit(id, NuiEventKind.DISMISSED) { NuiEvents.dismissed(id) }
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
    n.dialog = d
    d.show()
}

private fun NuiRuntime.presentAlert(act: NuiActivity, n: NuiNode) {
    val b = MaterialAlertDialogBuilder(act)
    if (n.alertTitle.isNotEmpty()) b.setTitle(n.alertTitle)
    val acts = n.actions
    val id = n.id
    fun acted(i: Int) {
        n.dying = true // a button isn't a dismissal
        emit(id, NuiEventKind.ACTION) { NuiEvents.action(id, i.toLong()) }
    }
    val cancel = acts.indexOfFirst { it.role == NuiAlertButtonRole.CANCEL }
    val others = acts.indices.filter { it != cancel }
    // An action sheet, or more buttons than a dialog has, is a list.
    if (n.alertStyle == NuiAlertStyle.ACTION_SHEET || others.size > 2) {
        b.setItems(others.map { styled(acts[it]) }.toTypedArray()) { _, which -> acted(others[which]) }
    } else {
        if (n.alertMessage.isNotEmpty()) b.setMessage(n.alertMessage)
        // The primary action is last, on the end; another is neutral.
        others.lastOrNull()?.let { i -> b.setPositiveButton(acts[i].title) { _, _ -> acted(i) } }
        if (others.size == 2) b.setNeutralButton(acts[others[0]].title) { _, _ -> acted(others[0]) }
        if (acts.isEmpty()) b.setPositiveButton(android.R.string.ok, null)
    }
    if (cancel >= 0) b.setNegativeButton(acts[cancel].title) { _, _ -> acted(cancel) }
    val d = b.create()
    d.setOnDismissListener {
        if (!n.dying) {
            n.dying = true
            emit(id, NuiEventKind.DISMISSED) { NuiEvents.dismissed(id) }
        }
    }
    n.dialog = d
    d.show()
    val error = MaterialColors.getColor(act, androidx.appcompat.R.attr.colorError, Color.RED)
    others.lastOrNull()?.let { if (acts[it].role == NuiAlertButtonRole.DESTRUCTIVE) d.getButton(android.app.AlertDialog.BUTTON_POSITIVE)?.setTextColor(error) }
    if (others.size == 2 && acts[others[0]].role == NuiAlertButtonRole.DESTRUCTIVE) d.getButton(android.app.AlertDialog.BUTTON_NEUTRAL)?.setTextColor(error)
}

fun NuiRuntime.haptic(kind: Int) {
    val v = activity?.window?.decorView ?: return
    val api30 = android.os.Build.VERSION.SDK_INT >= 30
    val c = when (kind) {
        NuiHaptic.SELECTION -> HapticFeedbackConstants.CLOCK_TICK
        NuiHaptic.LIGHT -> HapticFeedbackConstants.KEYBOARD_TAP
        NuiHaptic.MEDIUM -> HapticFeedbackConstants.VIRTUAL_KEY
        NuiHaptic.HEAVY -> HapticFeedbackConstants.LONG_PRESS
        NuiHaptic.SUCCESS -> if (api30) HapticFeedbackConstants.CONFIRM else HapticFeedbackConstants.VIRTUAL_KEY
        NuiHaptic.WARNING -> if (api30) HapticFeedbackConstants.REJECT else HapticFeedbackConstants.LONG_PRESS
        NuiHaptic.ERROR -> if (api30) HapticFeedbackConstants.REJECT else HapticFeedbackConstants.LONG_PRESS
        else -> return
    }
    v.performHapticFeedback(c)
}
