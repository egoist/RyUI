package dev.nui

import android.view.ViewGroup
import androidx.recyclerview.widget.DiffUtil
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import android.view.View
import com.google.android.material.divider.MaterialDividerItemDecoration

/**
 * A List: a RecyclerView whose cells bind their rows through
 * NuiNative.listBind when they're about to show. Item IDs come from the
 * core; changes are diffed here and dispatched as item ranges and moves.
 */
class NuiList(val id: Int, val runtime: NuiRuntime) {
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
        val reporter = NuiScrollReporter(runtime, id, recycler)
        recycler.addOnScrollListener(object : RecyclerView.OnScrollListener() {
            override fun onScrolled(rv: RecyclerView, dx: Int, dy: Int) {
                reporter.scrolled(rv.computeHorizontalScrollOffset(), rv.computeVerticalScrollOffset())
            }
        })
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

    private fun swipeActionsOf(h: RecyclerView.ViewHolder): List<NuiAction> =
        runtime.nodes[(h as Holder).cell.nuiID]?.swipeActions ?: emptyList()

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
            val cell = (h as Holder).cell.nuiID
            runtime.emit(cell, NuiEventKind.ACTION) { NuiEvents.action(cell, 0) }
            val p = h.bindingAdapterPosition
            // Back, if the action didn't remove it (a removal is diffed in).
            recycler.post { if (p in ids.indices) adapter.notifyItemChanged(p) }
        }

        override fun onChildDraw(c: android.graphics.Canvas, rv: RecyclerView, h: RecyclerView.ViewHolder, dx: Float, dy: Float, state: Int, active: Boolean) {
            val a = swipeActionsOf(h).firstOrNull()
            val v = h.itemView
            if (a != null && dx != 0f) {
                val destructive = a.role == NuiAlertButtonRole.DESTRUCTIVE
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
        DiffUtil.calculateDiff(object : DiffUtil.Callback() {
            override fun getOldListSize() = old.size
            override fun getNewListSize() = next.size
            override fun areItemsTheSame(o: Int, n: Int) = old[o] == next[n]
            // The core updates bound rows itself.
            override fun areContentsTheSame(o: Int, n: Int) = true
        }, false).dispatchUpdatesTo(adapter)
    }

    class Holder(val cell: NuiCell) : RecyclerView.ViewHolder(cell)

    inner class Adapter : RecyclerView.Adapter<Holder>() {
        override fun getItemCount() = ids.size
        override fun getItemId(position: Int) = ids[position]

        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): Holder {
            val cell = NuiCell(runtime.themed)
            cell.layoutParams = RecyclerView.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            runtime.makeCell(cell)
            return Holder(cell)
        }

        override fun onBindViewHolder(holder: Holder, position: Int) {
            holder.cell.binding = true
            runtime.enter { NuiNative.listBind(id, position.toLong(), holder.cell.nuiID) }
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
class NuiScrollReporter(private val rt: NuiRuntime, private val id: Int, private val view: View) {
    private var x = 0
    private var y = 0
    private var posted = false
    private val send = Runnable {
        posted = false
        // Not a view on its way out, whose ID may be another view's by now.
        if (!view.isAttachedToWindow || rt.nodes[id]?.view !== view) return@Runnable
        val d = rt.density
        rt.emit(id, NuiEventKind.SCROLLED) { NuiEvents.scrolled(id, (x / d).toDouble(), (y / d).toDouble()) }
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
