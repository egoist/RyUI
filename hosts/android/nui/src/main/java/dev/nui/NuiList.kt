package dev.nui

import android.view.ViewGroup
import androidx.recyclerview.widget.DiffUtil
import androidx.recyclerview.widget.LinearLayoutManager
import androidx.recyclerview.widget.RecyclerView
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
    }


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
