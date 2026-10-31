package dev.ryui

import android.view.ViewGroup
import androidx.recyclerview.widget.DiffUtil
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import android.view.View
import com.google.android.material.divider.MaterialDividerItemDecoration

/**
 * A List: a RecyclerView whose cells bind their rows through
 * RyUINative.listBind when they're about to show. Item IDs come from the
 * core; changes are diffed here and dispatched as item ranges and moves.
 */
class RyUIList(val id: Int, val runtime: RyUIRuntime) {
    val recycler = RecyclerView(runtime.themed)
    private var ids = LongArray(0)
    private var pending: LongArray? = null
    private val adapter = Adapter()
    private var divider = makeDivider()

    private fun makeDivider() = MaterialDividerItemDecoration(runtime.themed, LinearLayoutManager.VERTICAL).apply {
        isLastItemDecorated = false
    }

    init {
        recycler.tag = this
        recycler.layoutManager = LinearLayoutManager(runtime.themed)
        adapter.setHasStableIds(true)
        recycler.adapter = adapter
        recycler.itemAnimator = null
        recycler.addItemDecoration(divider)
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
        recycler.removeItemDecoration(divider)
        if (c == 1) {
            recycler.layoutManager = LinearLayoutManager(runtime.themed)
            recycler.addItemDecoration(divider)
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

    private fun swipeActionsOf(h: RecyclerView.ViewHolder): List<RyUIAction> =
        runtime.nodes[(h as Holder).cell.ryuiID]?.swipeActions ?: emptyList()

    /**
     * Swipe actions, as Material's swipe to dismiss: a row with them swipes
     * toward its start, over the first action's color and icon, and a full
     * swipe runs it. The row comes back unless the core removes it.
     */
    private val swipes = swipeHelper()

    init {
        swipes.attachToRecyclerView(recycler)
    }

    private fun swipeHelper() = androidx.recyclerview.widget.ItemTouchHelper(object : androidx.recyclerview.widget.ItemTouchHelper.SimpleCallback(0, 0) {
        private val paint = android.graphics.Paint()

        override fun getSwipeDirs(rv: RecyclerView, h: RecyclerView.ViewHolder) =
            if (swipeActionsOf(h).isEmpty()) 0 else androidx.recyclerview.widget.ItemTouchHelper.START

        override fun onMove(rv: RecyclerView, a: RecyclerView.ViewHolder, b: RecyclerView.ViewHolder) = false

        override fun onSwiped(h: RecyclerView.ViewHolder, direction: Int) {
            val cell = (h as Holder).cell.ryuiID
            runtime.emit(cell, RyUIEventKind.ACTION) { RyUIEvents.action(cell, 0) }
            val p = h.bindingAdapterPosition
            // Back, if the action didn't remove it (a removal is diffed in).
            recycler.post { if (p in ids.indices) adapter.notifyItemChanged(p) }
        }

        override fun onChildDraw(c: android.graphics.Canvas, rv: RecyclerView, h: RecyclerView.ViewHolder, dx: Float, dy: Float, state: Int, active: Boolean) {
            val a = swipeActionsOf(h).firstOrNull()
            val v = h.itemView
            if (a != null && dx != 0f) {
                val destructive = a.role == RyUIAlertButtonRole.DESTRUCTIVE
                val bg = com.google.android.material.color.MaterialColors.getColor(runtime.themed,
                    if (destructive) androidx.appcompat.R.attr.colorError else com.google.android.material.R.attr.colorSecondaryContainer, 0)
                val fg = com.google.android.material.color.MaterialColors.getColor(runtime.themed,
                    if (destructive) com.google.android.material.R.attr.colorOnError else com.google.android.material.R.attr.colorOnSecondaryContainer, 0)
                paint.color = bg
                val rtl = v.layoutDirection == android.view.View.LAYOUT_DIRECTION_RTL
                val left = if (rtl) v.left.toFloat() else v.right + dx
                val right = if (rtl) v.left + dx else v.right.toFloat()
                c.drawRect(left, v.top.toFloat(), right, v.bottom.toFloat(), paint)
                runtime.symbol(a.icon.ifEmpty { "trash" })?.let { d ->
                    val s = Math.round(24 * runtime.density)
                    val m = Math.round(20 * runtime.density)
                    val top = v.top + (v.height - s) / 2
                    val x = if (rtl) v.left + m else v.right - m - s
                    d.setTint(fg)
                    d.setBounds(x, top, x + s, top + s)
                    d.draw(c)
                }
            }
            super.onChildDraw(c, rv, h, dx, dy, state, active)
        }
    })


    /** Takes the divider color from the current theme (after a dark mode change). */
    fun restyle() {
        recycler.removeItemDecoration(divider)
        divider = makeDivider()
        recycler.addItemDecoration(divider)
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

    class Holder(val cell: RyUICell) : RecyclerView.ViewHolder(cell)

    inner class Adapter : RecyclerView.Adapter<Holder>() {
        override fun getItemCount() = ids.size
        override fun getItemId(position: Int) = ids[position]

        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): Holder {
            val cell = RyUICell(runtime.themed)
            cell.layoutParams = RecyclerView.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            runtime.makeCell(cell)
            return Holder(cell)
        }

        override fun onBindViewHolder(holder: Holder, position: Int) {
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
