package dev.asuka

import android.annotation.SuppressLint
import android.view.MenuItem
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import android.widget.TextView
import androidx.activity.OnBackPressedCallback
import androidx.drawerlayout.widget.DrawerLayout
import com.google.android.material.color.MaterialColors
import com.google.android.material.navigation.NavigationView
import java.nio.ByteBuffer

/**
 * A SplitView: Material's navigation drawer, a NavigationView in a
 * DrawerLayout. On a phone it's modal, opened by the menu button of the
 * detail's first screen (or a swipe from the edge) and closed by choosing
 * a destination or Android's back; on a wide window (840dp and more) it
 * stands open beside the content, a standard drawer. Its detail, a Stack,
 * fills the rest: the host sizes it, not the core.
 */
@SuppressLint("ViewConstructor")
class AsukaSplit(private val rt: AsukaRuntime, private val id: Int) : DrawerLayout(rt.themed) {
    val content = FrameLayout(rt.themed)
    private val nav = NavigationView(rt.themed)
    private val header = TextView(rt.themed)
    /** The drawer stands open beside the content. */
    var wide = false
        private set
    private var measured = false
    private val back = object : OnBackPressedCallback(false) {
        override fun handleOnBackPressed() { closeDrawer(nav) }
    }

    init {
        addView(content, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
        val d = rt.density
        header.setTextAppearance(com.google.android.material.R.style.TextAppearance_Material3_TitleSmall)
        header.setPadding(Math.round(28 * d), Math.round(24 * d), Math.round(28 * d), Math.round(12 * d))
        nav.addHeaderView(header)
        addView(nav, LayoutParams(Math.round(360 * d), LayoutParams.MATCH_PARENT, Gravity.START))
        nav.setNavigationItemSelectedListener { item ->
            val i = item.itemId.toLong()
            rt.emit(id, AsukaEventKind.SELECTED) { AsukaEvents.selected(id, i) }
            if (!wide) closeDrawer(nav)
            true
        }
        addDrawerListener(object : SimpleDrawerListener() {
            override fun onDrawerOpened(v: View) { back.isEnabled = !wide }
            override fun onDrawerClosed(v: View) { back.isEnabled = false }
        })
        rt.activity?.onBackPressedDispatcher?.addCallback(back)
    }

    fun openDrawer() = openDrawer(nav)

    fun setTitle(t: String) {
        header.text = t
        header.visibility = if (t.isEmpty()) View.GONE else View.VISIBLE
    }

    private var sections = emptyList<AsukaSidebarSection>()
    /** The destinations' items, in order (their ids are their indexes). */
    private val items = ArrayList<MenuItem>()

    /**
     * The sections and destinations (Split's Items). With as many
     * sections and items as before (a badge counting, a title changing),
     * the items change in place, a counter alone without touching the
     * menu; otherwise the menu is made again.
     */
    fun setItems(b: ByteBuffer) {
        val new = decodeSidebar(b)
        val old = sections
        sections = new
        if (sameSidebarShape(old, new) && items.size == new.sumOf { it.items.size }) {
            var flat = 0
            for ((s, sec) in new.withIndex()) {
                if (sec.title != old[s].title) {
                    if (sec.title.isEmpty() != old[s].title.isEmpty()) return rebuild()
                    nav.menu.findItem(SECTION_ID + s)?.title = sec.title
                }
                for ((i, it) in sec.items.withIndex()) {
                    val was = old[s].items[i]
                    val item = items[flat++]
                    if (it.title != was.title) item.title = it.title
                    if (it.icon != was.icon) item.icon = if (it.icon.isEmpty()) null else rt.symbol(it.icon)
                    if (it.badge != was.badge) showCounter(item, it.badge)
                }
            }
            return
        }
        rebuild()
    }

    private fun rebuild() {
        val menu = nav.menu
        menu.clear()
        items.clear()
        var flat = 0
        for ((s, sec) in sections.withIndex()) {
            val group = if (sec.title.isEmpty()) menu else menu.addSubMenu(s, SECTION_ID + s, s, sec.title)
            for (it in sec.items) {
                val item = group.add(s, flat, flat, it.title)
                if (it.icon.isNotEmpty()) item.icon = rt.symbol(it.icon)
                item.isCheckable = true
                showCounter(item, it.badge)
                items.add(item)
                flat++
            }
        }
        check(selected)
    }

    /**
     * An item's badge: a counter at its end, as Material's navigation
     * drawer and Gmail show one (label large, on surface variant). TalkBack
     * reads it after the title, a count in Material's badge words ("3 new
     * notifications"), as the navigation bar's badges.
     */
    private fun showCounter(item: MenuItem, badge: String) {
        if (badge.isEmpty()) {
            if (item.actionView != null) item.actionView = null
            return
        }
        val t = (item.actionView as? TextView) ?: TextView(rt.themed).also {
            it.setTextAppearance(com.google.android.material.R.style.TextAppearance_Material3_LabelLarge)
            it.setTextColor(MaterialColors.getColor(nav, com.google.android.material.R.attr.colorOnSurfaceVariant))
            it.gravity = Gravity.CENTER_VERTICAL or Gravity.END
            it.fontFeatureSettings = "tnum"
            item.actionView = it
        }
        t.text = badge
        val n = badgeNumber(badge)
        t.contentDescription = if (n != null) {
            rt.themed.resources.getQuantityString(com.google.android.material.R.plurals.mtrl_badge_content_description, n, n)
        } else null
    }

    private var selected = -1
    fun select(i: Int) {
        selected = i
        check(i)
    }

    /**
     * Checks destination i, and only it: the items themselves, which is
     * what NavigationView's menu keeps when it updates (setCheckedItem
     * alone marks its presenter's), across the sections' groups.
     */
    private fun check(i: Int) {
        val menu = nav.menu
        for (j in 0 until menu.size()) {
            val item = menu.getItem(j)
            val sub = item.subMenu
            if (sub == null) item.isChecked = item.itemId == i
            else for (k in 0 until sub.size()) sub.getItem(k).let { it.isChecked = it.itemId == i }
        }
        if (i >= 0 && menu.findItem(i) != null) nav.setCheckedItem(i)
    }

    override fun onMeasure(widthSpec: Int, heightSpec: Int) {
        val w = MeasureSpec.getSize(widthSpec)
        val d = rt.density
        val isWide = w / d >= 840
        val navWidth = minOf(Math.round(360 * d), w - Math.round(56 * d))
        (nav.layoutParams as LayoutParams).width = navWidth
        // The drawer goes under the system bars, its items between them.
        // (The activity takes the insets, so NavigationView never gets them.)
        val ins = rt.insetsOver(this, w, MeasureSpec.getSize(heightSpec))
        if (nav.paddingTop != ins.top || nav.paddingBottom != ins.bottom) nav.setPadding(0, ins.top, 0, ins.bottom)
        if (isWide != wide || !measured) {
            measured = true
            wide = isWide
            setDrawerLockMode(if (wide) LOCK_MODE_LOCKED_OPEN else LOCK_MODE_UNLOCKED, nav)
            setScrimColor(if (wide) 0 else 0x52000000)
            if (wide) openDrawer(nav, false) else closeDrawer(nav, false)
            back.isEnabled = false
            // The detail's first screen shows the menu button only while the drawer hides.
            refreshDetailBar()
        }
        (content.layoutParams as LayoutParams).marginStart = if (wide) navWidth else 0
        super.onMeasure(widthSpec, heightSpec)
    }

    /** The detail (a Stack) goes in the content, filling it. */
    fun setDetail(v: View) {
        (v.parent as? ViewGroup)?.removeView(v)
        content.removeAllViews()
        content.addView(v, FrameLayout.LayoutParams(FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT))
        refreshDetailBar() // its bar drew before it was the detail: no menu button yet
    }

    private fun refreshDetailBar() = post { (content.getChildAt(0) as? AsukaStack)?.refreshBar() }

    override fun onDetachedFromWindow() {
        super.onDetachedFromWindow()
        back.remove()
    }
}

/** A section's submenu item's id: past any destination's. */
private const val SECTION_ID = 0x40000000
