package dev.asuka

import android.annotation.SuppressLint
import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import android.widget.TextView
import androidx.activity.OnBackPressedCallback
import androidx.drawerlayout.widget.DrawerLayout
import com.google.android.material.navigation.NavigationView
import java.nio.ByteBuffer
import java.nio.ByteOrder

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

    /** The sections and destinations (Split's Items). */
    fun setItems(b: ByteBuffer) {
        val r = b.duplicate().order(ByteOrder.LITTLE_ENDIAN)
        fun str(): String = ByteArray(r.int).also { r.get(it) }.toString(Charsets.UTF_8)
        val menu = nav.menu
        menu.clear()
        if (r.remaining() < 4) return
        var flat = 0
        for (s in 0 until r.int) {
            val title = str()
            val group = if (title.isEmpty()) menu else menu.addSubMenu(s, 0, s, title)
            for (i in 0 until r.int) {
                val item = group.add(s, flat, flat, str())
                val icon = str()
                if (icon.isNotEmpty()) item.icon = rt.symbol(icon)
                item.isCheckable = true
                flat++
            }
        }
        check(selected)
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
