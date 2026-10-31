package dev.ryui

import android.view.ViewGroup
import androidx.recyclerview.widget.DiffUtil
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import android.view.View
import com.google.android.material.color.MaterialColors
import com.google.android.material.divider.MaterialDividerItemDecoration

/**
 * A List: a RecyclerView whose cells bind their rows through
 * RyUINative.listBind when they're about to show. Item IDs come from the
 * core; changes are diffed here and dispatched as item ranges and moves.
 */
class RyUIList(val id: Int, val runtime: RyUIRuntime) {
    val recycler = object : RecyclerView(runtime.themed) {
        override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
            super.onLayout(changed, l, t, r, b)
            // Rows inserted or removed change their neighbors' places.
            if (grouped) segments()
        }
    }
    private var ids = LongArray(0)
    private var pending: LongArray? = null
    private val adapter = Adapter()
    var appearance = RyUIListAppearance.PLAIN
        private set
    var inset = 0f
        private set
    /** Material 3 Expressive's segmented list: an inset grouped list's look. */
    private val grouped get() = appearance == RyUIListAppearance.INSET_GROUPED && columns == 1
    private var separators = makeSeparators()

    /** A list's dividers; a segmented list's gaps, 2dp (Material's catalog). */
    private fun makeSeparators(): RecyclerView.ItemDecoration {
        if (!grouped) return MaterialDividerItemDecoration(runtime.themed, LinearLayoutManager.VERTICAL).apply {
            isLastItemDecorated = false
        }
        val g = Math.round(2 * runtime.density)
        return object : RecyclerView.ItemDecoration() {
            override fun getItemOffsets(out: android.graphics.Rect, view: View, parent: RecyclerView, state: RecyclerView.State) {
                if (parent.getChildAdapterPosition(view) > 0) out.top = g
            }
        }
    }

    init {
        recycler.tag = this
        recycler.layoutManager = LinearLayoutManager(runtime.themed)
        adapter.setHasStableIds(true)
        recycler.adapter = adapter
        recycler.itemAnimator = null
        recycler.addItemDecoration(separators)
        val reporter = RyUIScrollReporter(runtime, id, recycler)
        recycler.addOnScrollListener(object : RecyclerView.OnScrollListener() {
            override fun onScrolled(rv: RecyclerView, dx: Int, dy: Int) {
                reporter.scrolled(rv.computeHorizontalScrollOffset(), rv.computeVerticalScrollOffset())
            }
        })
    }

    var columns = 1
        private set
    var gap = 0f
        private set
    private var gapDecoration: RecyclerView.ItemDecoration? = null

    /**
     * A grid of columns, gap points apart (a GridLayoutManager): each
     * cell an equal share of the width, a row as tall as its tallest; or,
     * columns 1, the list again, with its dividers.
     */
    fun setGrid(columns: Int, gap: Float) {
        val c = maxOf(columns, 1)
        if (c == this.columns && gap == this.gap) return
        this.columns = c
        this.gap = gap
        gapDecoration?.let { recycler.removeItemDecoration(it) }
        gapDecoration = null
        recycler.removeItemDecoration(separators)
        applyAppearance()
        if (c == 1) {
            recycler.layoutManager = LinearLayoutManager(runtime.themed)
            separators = makeSeparators()
            recycler.addItemDecoration(separators)
            return
        }
        recycler.layoutManager = androidx.recyclerview.widget.GridLayoutManager(runtime.themed, c)
        val g = gap * runtime.density
        if (g > 0) {
            // Equal shares: column i's cell gives up i*g/c on its start and
            // g - (i+1)*g/c on its end, so each is (width - g*(c-1)) / c.
            val d = object : RecyclerView.ItemDecoration() {
                override fun getItemOffsets(out: android.graphics.Rect, view: View, parent: RecyclerView, state: RecyclerView.State) {
                    val pos = parent.getChildAdapterPosition(view)
                    if (pos == RecyclerView.NO_POSITION) return
                    val i = pos % c
                    val start = Math.round(i * g / c)
                    val end = Math.round(g - (i + 1) * g / c)
                    if (parent.layoutDirection == View.LAYOUT_DIRECTION_RTL) out.set(end, 0, start, 0) else out.set(start, 0, end, 0)
                    if (pos >= c) out.top = Math.round(g)
                }
            }
            recycler.addItemDecoration(d)
            gapDecoration = d
        }
    }

    /** Scrolls item i into view, at the top. */
    fun scroll(i: Int, animated: Boolean) {
        applyPending()
        if (i < 0 || i >= ids.size) return
        val lm = recycler.layoutManager as LinearLayoutManager
        if (!animated) { lm.scrollToPositionWithOffset(i, 0); return }
        val s = object : androidx.recyclerview.widget.LinearSmoothScroller(runtime.themed) {
            override fun getVerticalSnapPreference() = SNAP_TO_START
        }
        s.targetPosition = i
        lm.startSmoothScroll(s)
    }

    /**
     * The list's look: inset grouped is Material 3 Expressive's segmented
     * list, its rows inset points from each side (the core lays them out
     * at the list's width less that), 8dp from its top and bottom and 2dp
     * apart (Material's catalog), on the surface container; each row's
     * container is rounded as its place in the group is.
     */
    fun setAppearance(appearance: Int, inset: Float) {
        if (appearance == this.appearance && inset == this.inset) return
        this.appearance = appearance
        this.inset = inset
        applyAppearance()
        if (columns == 1) {
            recycler.removeItemDecoration(separators)
            separators = makeSeparators()
            recycler.addItemDecoration(separators)
        }
    }

    private fun applyAppearance() {
        val side = if (grouped) Math.round(inset * runtime.density) else 0
        val ends = if (grouped) Math.round(8 * runtime.density) else 0
        recycler.setPaddingRelative(side, ends, side, ends)
        recycler.clipToPadding = !grouped
        // The app's background, if it has one.
        val n = runtime.nodes[id]
        if (n == null || n.background == 0L && n.gradient == null && n.material == 0) {
            if (grouped) recycler.setBackgroundColor(MaterialColors.getColor(runtime.themed,
                com.google.android.material.R.attr.colorSurfaceContainerHigh, 0))
            else recycler.background = null
        }
        segments()
    }

    /** Rounds each row's container as its place in the group is. */
    private fun segments() {
        for (i in 0 until recycler.childCount) {
            val row = recycler.getChildAt(i) as? RyUISwipeRow ?: continue
            row.segment(if (grouped) recycler.getChildAdapterPosition(row) else RecyclerView.NO_POSITION, ids.size)
        }
    }

    /** Takes the theme's colors again (after a dark mode change). */
    fun restyle() {
        recycler.removeItemDecoration(separators)
        separators = makeSeparators()
        recycler.addItemDecoration(separators)
        applyAppearance()
        if (grouped) for (i in 0 until recycler.childCount) (recycler.getChildAt(i) as? RyUISwipeRow)?.restyle()
    }

    fun update(next: LongArray) {
        pending = next
    }

    fun applyPending() {
        val next = pending ?: return
        pending = null
        val old = ids
        ids = next
        if (old.isEmpty()) {
            adapter.notifyDataSetChanged()
            return
        }
        // At the top, a list stays at the top: rows inserted above the first
        // show, as on iOS (RecyclerView keeps its first row where it is,
        // and the new ones go above, out of sight).
        val atTop = !recycler.canScrollVertically(-1)
        DiffUtil.calculateDiff(object : DiffUtil.Callback() {
            override fun getOldListSize() = old.size
            override fun getNewListSize() = next.size
            override fun areItemsTheSame(o: Int, n: Int) = old[o] == next[n]
            // The core updates bound rows itself.
            override fun areContentsTheSame(o: Int, n: Int) = true
        }, false).dispatchUpdatesTo(adapter)
        if (atTop && next.isNotEmpty()) recycler.scrollToPosition(0)
    }

    class Holder(val row: RyUISwipeRow) : RecyclerView.ViewHolder(row) {
        val cell get() = row.cell
    }

    inner class Adapter : RecyclerView.Adapter<Holder>() {
        override fun getItemCount() = ids.size
        override fun getItemId(position: Int) = ids[position]

        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): Holder {
            val row = RyUISwipeRow(runtime, RyUICell(runtime.themed))
            row.layoutParams = RecyclerView.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            runtime.makeCell(row.cell)
            return Holder(row)
        }

        override fun onBindViewHolder(holder: Holder, position: Int) {
            holder.row.close(animated = false)
            holder.row.segment(if (grouped) position else RecyclerView.NO_POSITION, ids.size)
            holder.cell.binding = true
            runtime.enter { RyUINative.listBind(id, position.toLong(), holder.cell.ryuiID) }
            holder.cell.binding = false
        }
    }
}

/**
 * Reports a scroll view's offset (pixels in, points out) as Scrolled: on
 * the main thread's next turn, the latest once. Scrolling happens while
 * the view draws (an animated scroll, a fling), and what the core does
 * with the event (adding, removing views) mustn't happen in the middle
 * of that: it stops the scroll.
 */
class RyUIScrollReporter(private val rt: RyUIRuntime, private val id: Int, private val view: View) {
    private var x = 0
    private var y = 0
    private var posted = false
    private val send = Runnable {
        posted = false
        // Not a view on its way out, whose ID may be another view's by now.
        if (!view.isAttachedToWindow || rt.nodes[id]?.view !== view) return@Runnable
        val d = rt.density
        rt.emit(id, RyUIEventKind.SCROLLED) { RyUIEvents.scrolled(id, (x / d).toDouble(), (y / d).toDouble()) }
    }

    fun scrolled(x: Int, y: Int) {
        this.x = x
        this.y = y
        if (!posted) {
            posted = true
            rt.onMain { send.run() }
        }
    }
}

/**
 * Offers a row's swipe actions to TalkBack, as actions on the row (its
 * pressable content, which TalkBack focuses): swiping isn't something a
 * screen reader's user can do.
 */
fun RyUIRuntime.swipeAccessibility(n: RyUINode) {
    n.swipeA11y?.let { (v, ids) -> for (i in ids) androidx.core.view.ViewCompat.removeAccessibilityAction(v, i) }
    n.swipeA11y = null
    if (n.swipeActions.isEmpty()) return
    var v: View = n.view
    while (!v.isClickable && v is ViewGroup && v.childCount == 1) v = v.getChildAt(0)
    val id = n.id
    val ids = n.swipeActions.mapIndexed { i, a ->
        androidx.core.view.ViewCompat.addAccessibilityAction(v, a.title) { _, _ ->
            emit(id, RyUIEventKind.ACTION) { RyUIEvents.action(id, i.toLong()) }
            true
        }
    }
    n.swipeA11y = v to ids
}
