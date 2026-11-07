package dev.ryui

import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import androidx.recyclerview.widget.DiffUtil
import androidx.recyclerview.widget.GridLayoutManager
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
import com.google.android.material.color.MaterialColors
import com.google.android.material.divider.MaterialDividerItemDecoration

/**
 * A List: a RecyclerView whose cells bind their rows (and its sections'
 * headers) through RyUINative.listBind when they're about to show. Item
 * IDs come from the core; changes are diffed here and dispatched as item
 * ranges and moves. The list's view is a frame around the RecyclerView,
 * so a plain list's section header can stay over its top while its rows
 * scroll under it (UITableView's plain style, which Android's lists don't
 * have): a cell of its own, bound to the section at the top.
 */
class RyUIList(val id: Int, val runtime: RyUIRuntime) {
    val recycler = object : RecyclerView(runtime.themed) {
        override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
            super.onLayout(changed, l, t, r, b)
            // Rows inserted or removed change their neighbors' places.
            if (grouped) segments()
            pin()
        }
    }
    /** The list's view: the RecyclerView, and over its top the pinned header. */
    val frame = FrameLayout(runtime.themed)
    private var pinned: RyUICell? = null
    private var pinnedSection = -1

    // The core's items and sections, and the entries the adapter shows
    // (each section's header, then its rows): their IDs, their indexes as
    // the core counts them (an item's, or -1 - a section's), and sections.
    private var ids = LongArray(0)
    private var starts = IntArray(0)
    private var entries = LongArray(0)
    private var entryIndex = IntArray(0)
    private var entrySection = IntArray(0)
    private var headerPos = IntArray(0)
    private class Update(val ids: LongArray, val starts: IntArray, val sections: LongArray)
    private var pending: Update? = null
    private val adapter = Adapter()
    var appearance = RyUIListAppearance.PLAIN
        private set
    var inset = 0f
        private set
    /** Material 3 Expressive's segmented list: an inset grouped list's look. */
    private val grouped get() = appearance == RyUIListAppearance.INSET_GROUPED && columns == 1
    private val hasSections get() = starts.isNotEmpty()
    private var separators = makeSeparators()

    // Anchored at the bottom, and whether it's at its end (and stays
    // there): only the user's scrolling moves it off, or back.
    private var bottom = false
    private var stuck = true
    private var dragged = false
    private var settling = false // a scroll the core asked for
    // Selecting, and the room the checkbox takes at the rows' end.
    var selecting = false
        private set
    private var rowInsetSent = 0f

    private fun isHeader(position: Int) = position in entryIndex.indices && entryIndex[position] < 0

    /** A list's dividers, none around headers; a segmented list's gaps, 2dp (Material's catalog). */
    private fun makeSeparators(): RecyclerView.ItemDecoration {
        if (appearance == RyUIListAppearance.BARE) return object : RecyclerView.ItemDecoration() {}
        if (!grouped) return object : MaterialDividerItemDecoration(runtime.themed, LinearLayoutManager.VERTICAL) {
            override fun shouldDrawDivider(position: Int, adapter: RecyclerView.Adapter<*>?): Boolean =
                !isHeader(position) && !isHeader(position + 1)
        }.apply { isLastItemDecorated = false }
        val g = Math.round(2 * runtime.density)
        val ends = Math.round(8 * runtime.density)
        return object : RecyclerView.ItemDecoration() {
            override fun getItemOffsets(out: android.graphics.Rect, view: View, parent: RecyclerView, state: RecyclerView.State) {
                val p = parent.getChildAdapterPosition(view)
                if (p == RecyclerView.NO_POSITION) return
                // Each section is a group of its own: its header, then its rows 2dp apart.
                if (isHeader(p)) { if (p > 0) out.top = ends; return }
                if (p > 0 && !isHeader(p - 1)) out.top = g
            }
        }
    }

    init {
        frame.tag = this
        recycler.tag = this
        frame.addView(recycler, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        recycler.layoutManager = LinearLayoutManager(runtime.themed)
        adapter.setHasStableIds(true)
        recycler.adapter = adapter
        recycler.itemAnimator = null
        recycler.addItemDecoration(separators)
        // A row RecyclerView kept aside comes back without being bound
        // again: in the list's selection mode as it is now.
        recycler.addOnChildAttachStateChangeListener(object : RecyclerView.OnChildAttachStateChangeListener {
            override fun onChildViewAttachedToWindow(view: View) { (view as? RyUISwipeRow)?.setSelecting(selecting) }
            override fun onChildViewDetachedFromWindow(view: View) {}
        })
        val reporter = RyUIScrollReporter(runtime, id, recycler)
        recycler.addOnScrollListener(object : RecyclerView.OnScrollListener() {
            override fun onScrolled(rv: RecyclerView, dx: Int, dy: Int) {
                reporter.scrolled(rv.computeHorizontalScrollOffset(), rv.computeVerticalScrollOffset())
                if (dragged) stuck = !rv.canScrollVertically(1)
                pin()
            }

            override fun onScrollStateChanged(rv: RecyclerView, state: Int) {
                if (state == RecyclerView.SCROLL_STATE_DRAGGING) dragged = true
                if (state == RecyclerView.SCROLL_STATE_IDLE && (dragged || settling)) {
                    dragged = false
                    settling = false
                    stuck = !rv.canScrollVertically(1)
                }
            }
        })
    }

    var columns = 1
        private set
    var gap = 0f
        private set
    private var gapDecoration: RecyclerView.ItemDecoration? = null

    private fun linear(): LinearLayoutManager = LinearLayoutManager(runtime.themed).apply { stackFromEnd = bottom }

    /**
     * A grid of columns, gap points apart (a GridLayoutManager): each
     * cell an equal share of the width, a row as tall as its tallest, a
     * section's header across the whole row; or, columns 1, the list
     * again, with its dividers.
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
            recycler.layoutManager = linear()
            separators = makeSeparators()
            recycler.addItemDecoration(separators)
            return
        }
        val lm = GridLayoutManager(runtime.themed, c) // a grid reads from its top (the core's anchor for it)
        lm.spanSizeLookup = object : GridLayoutManager.SpanSizeLookup() {
            override fun getSpanSize(position: Int) = if (isHeader(position)) c else 1
        }.apply { isSpanIndexCacheEnabled = false; isSpanGroupIndexCacheEnabled = false }
        recycler.layoutManager = lm
        val g = gap * runtime.density
        if (g > 0) {
            // Equal shares: column i's cell gives up i*g/c on its start and
            // g - (i+1)*g/c on its end, so each is (width - g*(c-1)) / c.
            val d = object : RecyclerView.ItemDecoration() {
                override fun getItemOffsets(out: android.graphics.Rect, view: View, parent: RecyclerView, state: RecyclerView.State) {
                    val pos = parent.getChildAdapterPosition(view)
                    if (pos == RecyclerView.NO_POSITION) return
                    if (isHeader(pos)) { if (pos > 0) out.top = Math.round(g); return }
                    val i = lm.spanSizeLookup.getSpanIndex(pos, c)
                    val start = Math.round(i * g / c)
                    val end = Math.round(g - (i + 1) * g / c)
                    if (parent.layoutDirection == View.LAYOUT_DIRECTION_RTL) out.set(end, 0, start, 0) else out.set(start, 0, end, 0)
                    // Not above the first row of the list or of a section (its header is).
                    val first = if (hasSections) headerPos[entrySection[pos]] + 1 else 0
                    if (lm.spanSizeLookup.getSpanGroupIndex(pos, c) > lm.spanSizeLookup.getSpanGroupIndex(first, c)) out.top = Math.round(g)
                }
            }
            recycler.addItemDecoration(d)
            gapDecoration = d
        }
    }

    /**
     * Anchored at the bottom (a chat's): it opens at its end, rows that
     * don't fill it sit at its bottom (LinearLayoutManager's stackFromEnd),
     * and while it's at its end it stays there, as rows are added there.
     */
    fun setAnchor(anchor: Int) {
        bottom = anchor == RyUIScrollAnchor.BOTTOM
        stuck = true
        // GridLayoutManager can't stack from its end: a grid isn't anchored.
        (recycler.layoutManager as? LinearLayoutManager)?.takeIf { it !is GridLayoutManager }?.stackFromEnd = bottom
    }

    /** The adapter position of item i. */
    private fun position(i: Int): Int {
        if (!hasSections) return i
        var s = 0
        while (s + 1 < starts.size && starts[s + 1] <= i) s++
        return i + s + 1
    }

    /** Scrolls item i into view, at the top. */
    fun scroll(i: Int, animated: Boolean) {
        applyPending()
        if (i < 0 || i >= ids.size) return
        val p = position(i)
        val lm = recycler.layoutManager as LinearLayoutManager
        if (bottom) stuck = i == ids.size - 1
        if (!animated) {
            // Under a pinned header, a section's first row shows below it.
            lm.scrollToPositionWithOffset(if (hasSections && sticky && p == headerPos[entrySection[p]] + 1) p - 1 else p, 0)
            return
        }
        val s = object : androidx.recyclerview.widget.LinearSmoothScroller(runtime.themed) {
            override fun getVerticalSnapPreference() = SNAP_TO_START
        }
        s.targetPosition = p
        lm.startSmoothScroll(s)
    }

    /** Scrolled to an offset by the core: at the end, a list anchored at its bottom stays there again. */
    fun scrolledTo() {
        if (!bottom) return
        if (recycler.scrollState == RecyclerView.SCROLL_STATE_IDLE) stuck = !recycler.canScrollVertically(1)
        else settling = true
    }

    /**
     * The list's look: inset grouped is Material 3 Expressive's segmented
     * list, its rows inset points from each side (the core lays them out
     * at the list's width less that), 8dp from its top and bottom and 2dp
     * apart (Material's catalog), on the surface container; each row's
     * container is rounded as its place in its group is.
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
        pinned?.let { restylePinned(it) }
        pin()
    }

    /** Rounds each row's container as its place in its group is. */
    private fun segments() {
        for (i in 0 until recycler.childCount) {
            val row = recycler.getChildAt(i) as? RyUISwipeRow ?: continue
            val p = recycler.getChildAdapterPosition(row)
            segment(row, p)
        }
    }

    private fun segment(row: RyUISwipeRow, p: Int) {
        if (!grouped || p == RecyclerView.NO_POSITION || p !in entryIndex.indices) {
            row.segment(RecyclerView.NO_POSITION, 0)
            return
        }
        val i = entryIndex[p]
        if (!hasSections) { row.segment(i, ids.size); return }
        val s = entrySection[p]
        val start = starts[s]
        val end = if (s + 1 < starts.size) starts[s + 1] else ids.size
        row.segment(i - start, end - start)
    }

    /** Takes the theme's colors again (after a dark mode change). */
    fun restyle() {
        recycler.removeItemDecoration(separators)
        separators = makeSeparators()
        recycler.addItemDecoration(separators)
        applyAppearance()
        for (i in 0 until recycler.childCount) (recycler.getChildAt(i) as? RyUISwipeRow)?.restyle()
    }

    fun update(next: LongArray, starts: IntArray, sections: LongArray) {
        pending = Update(next, starts, sections)
    }

    /**
     * Applies what the core last sent. Rows added or removed above the
     * ones on screen leave those where they are (RecyclerView keeps its
     * anchor row in place), unless the list is at the edge it's anchored
     * to: at its top, new rows there show; at its end, anchored at the
     * bottom, it scrolls to show new rows there.
     */
    fun applyPending() {
        val next = pending ?: return
        pending = null
        val old = entries
        val oldLast = ids.lastOrNull()
        val oldCount = ids.size
        ids = next.ids
        starts = next.starts
        build(next.sections)
        if (old.isEmpty()) {
            adapter.notifyDataSetChanged()
            pinnedSection = -1
            return
        }
        val atTop = !recycler.canScrollVertically(-1)
        val now = entries
        DiffUtil.calculateDiff(object : DiffUtil.Callback() {
            override fun getOldListSize() = old.size
            override fun getNewListSize() = now.size
            override fun areItemsTheSame(o: Int, n: Int) = old[o] == now[n]
            // The core updates bound rows itself.
            override fun areContentsTheSame(o: Int, n: Int) = true
        }, false).dispatchUpdatesTo(adapter)
        pinnedSection = -1 // its section's index may have changed
        when {
            !bottom && atTop && now.isNotEmpty() -> recycler.scrollToPosition(0)
            bottom && stuck && ids.size > oldCount && ids.lastOrNull() != oldLast -> {
                val last = now.size - 1
                if (recycler.isAttachedToWindow && recycler.isLaidOut) recycler.smoothScrollToPosition(last) else recycler.scrollToPosition(last)
            }
        }
    }

    /** The entries: each section's header, then its rows. */
    private fun build(sections: LongArray) {
        if (starts.isEmpty()) {
            entries = ids
            entryIndex = IntArray(ids.size) { it }
            entrySection = IntArray(ids.size)
            headerPos = IntArray(0)
            return
        }
        val n = ids.size + starts.size
        val e = LongArray(n)
        val ix = IntArray(n)
        val sec = IntArray(n)
        val hp = IntArray(starts.size)
        var p = 0
        for (s in starts.indices) {
            hp[s] = p
            e[p] = sections[s]
            ix[p] = -1 - s
            sec[p] = s
            p++
            val end = if (s + 1 < starts.size) starts[s + 1] else ids.size
            for (i in starts[s] until end) {
                e[p] = ids[i]
                ix[p] = i
                sec[p] = s
                p++
            }
        }
        entries = e
        entryIndex = ix
        entrySection = sec
        headerPos = hp
    }

    // Pinned headers

    /** A plain or bare list's headers stick to its top; an inset grouped list's scroll with it. */
    private val sticky get() = hasSections && appearance != RyUIListAppearance.INSET_GROUPED

    /**
     * Shows the header of the section at the list's top over it, unless
     * that section's own header is still in view there; the next header
     * pushes it up as it comes.
     */
    private fun pin() {
        val lm = recycler.layoutManager as? LinearLayoutManager
        if (!sticky || lm == null || entries.isEmpty()) {
            pinned?.visibility = View.GONE
            return
        }
        val top = recycler.paddingTop
        var pos = RecyclerView.NO_POSITION
        for (i in 0 until recycler.childCount) {
            val c = recycler.getChildAt(i)
            val p = recycler.getChildAdapterPosition(c)
            if (p != RecyclerView.NO_POSITION && c.bottom > top && (pos == RecyclerView.NO_POSITION || p < pos)) pos = p
        }
        if (pos == RecyclerView.NO_POSITION || pos !in entrySection.indices) {
            pinned?.visibility = View.GONE
            return
        }
        val s = entrySection[pos]
        val own = lm.findViewByPosition(headerPos[s])
        if (own != null && own.top >= top) {
            pinned?.visibility = View.GONE
            return
        }
        val cell = pinned ?: makePinned()
        if (s != pinnedSection) {
            pinnedSection = s
            cell.binding = true
            runtime.enter { RyUINative.listBind(id, (-1 - s).toLong(), cell.ryuiID) }
            cell.binding = false
        }
        cell.visibility = View.VISIBLE
        val h = cell.rowHeight
        // Its size is the row's, the list's width: placed here, at once (a
        // layout asked for inside the list's own would wait a frame).
        val w = recycler.width
        if (cell.width != w || cell.height != h) {
            cell.measure(View.MeasureSpec.makeMeasureSpec(w, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(h, View.MeasureSpec.EXACTLY))
            cell.layout(0, 0, w, h)
        }
        val nextHeader = if (s + 1 < headerPos.size) lm.findViewByPosition(headerPos[s + 1]) else null
        cell.translationY = (top + if (nextHeader != null && nextHeader.top < top + h) nextHeader.top - top - h else 0).toFloat()
    }

    private fun makePinned(): RyUICell {
        val c = RyUICell(runtime.themed)
        runtime.makeCell(c)
        c.importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS // the list's own header row is read
        frame.addView(c, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT, Gravity.TOP))
        restylePinned(c)
        pinned = c
        return c
    }

    private fun restylePinned(c: RyUICell) {
        // A plain list's is opaque, over the rows, on the surface; a bare
        // list's draws its own, as its rows do.
        if (appearance == RyUIListAppearance.BARE) c.background = null
        else c.setBackgroundColor(MaterialColors.getColor(runtime.themed, com.google.android.material.R.attr.colorSurface, 0))
    }

    // Selection

    /**
     * Selecting rows: each shows a Material checkbox at its end, a tap
     * selects it, and rows don't swipe. The core lays the rows out at
     * the width they have (RowInset).
     */
    fun setSelecting(on: Boolean) {
        if (on == selecting) return
        selecting = on
        for (i in 0 until recycler.childCount) (recycler.getChildAt(i) as? RyUISwipeRow)?.setSelecting(on)
        val trail = if (on) RyUISwipeRow.CHECK_WIDTH else 0f
        if (trail != rowInsetSent) {
            rowInsetSent = trail
            runtime.emit(id, RyUIEventKind.ROW_INSET) { RyUIEvents.rowInset(id, 0.0, trail.toDouble()) }
        }
    }

    class Holder(val row: RyUISwipeRow?, val header: RyUICell?) : RecyclerView.ViewHolder(row ?: header!!) {
        val cell: RyUICell get() = row?.cell ?: header!!
    }

    inner class Adapter : RecyclerView.Adapter<Holder>() {
        override fun getItemCount() = entries.size
        override fun getItemId(position: Int) = entries[position]
        override fun getItemViewType(position: Int) = if (entryIndex[position] < 0) 1 else 0

        override fun onCreateViewHolder(parent: ViewGroup, viewType: Int): Holder {
            if (viewType == 1) {
                val h = RyUICell(runtime.themed)
                h.layoutParams = RecyclerView.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
                runtime.makeCell(h)
                androidx.core.view.ViewCompat.setAccessibilityHeading(h, true)
                return Holder(null, h)
            }
            val row = RyUISwipeRow(runtime, RyUICell(runtime.themed))
            row.layoutParams = RecyclerView.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT)
            runtime.makeCell(row.cell)
            return Holder(row, null)
        }

        override fun onBindViewHolder(holder: Holder, position: Int) {
            holder.row?.let {
                it.close(animated = false)
                it.setSelecting(selecting)
                segment(it, position)
            }
            val cell = holder.cell
            cell.binding = true
            runtime.enter { RyUINative.listBind(id, entryIndex[position].toLong(), cell.ryuiID) }
            cell.binding = false
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
        val n = rt.nodes[id]
        if (!view.isAttachedToWindow || n == null || (n.view !== view && n.list?.recycler !== view)) return@Runnable
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
 * screen reader's user can do. Leading actions' events carry -1 - their
 * index, as the core expects.
 */
fun RyUIRuntime.swipeAccessibility(n: RyUINode) {
    n.swipeA11y?.let { (v, ids) -> for (i in ids) androidx.core.view.ViewCompat.removeAccessibilityAction(v, i) }
    n.swipeA11y = null
    if (n.swipeActions.isEmpty() && n.leadingSwipeActions.isEmpty()) return
    var v: View = n.view
    while (!v.isClickable && v is ViewGroup && v.childCount == 1) v = v.getChildAt(0)
    val id = n.id
    val all = n.leadingSwipeActions.mapIndexed { i, a -> a to (-1L - i) } + n.swipeActions.mapIndexed { i, a -> a to i.toLong() }
    val ids = all.map { (a, k) ->
        androidx.core.view.ViewCompat.addAccessibilityAction(v, a.title) { _, _ ->
            emit(id, RyUIEventKind.ACTION) { RyUIEvents.action(id, k) }
            true
        }
    }
    n.swipeA11y = v to ids
}
