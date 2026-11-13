// The segmented control on Android: Material's segmented buttons.
package com.github.egoist.asuka.examples.media.segmented

import android.view.View
import android.widget.LinearLayout
import androidx.appcompat.view.ContextThemeWrapper
import com.google.android.material.button.MaterialButton
import com.google.android.material.button.MaterialButtonToggleGroup

private class KotlinSegmented(private val events: SegmentedEvents) : Segmented {
    private val group = MaterialButtonToggleGroup(events.context).apply {
        isSingleSelection = true
        isSelectionRequired = true
    }
    override val view: View get() = group
    private var selected = 0
    private var setting = false

    init {
        group.addOnButtonCheckedListener { g, id, checked ->
            if (checked && !setting) events.changed(g.indexOfChild(g.findViewById(id)).toLong())
        }
    }

    override fun setItems(value: List<String>) {
        setting = true
        group.removeAllViews()
        for (t in value) {
            val b = MaterialButton(ContextThemeWrapper(events.context, dev.asuka.R.style.ThemeOverlay_Asuka_OutlinedButton))
            b.id = View.generateViewId()
            b.text = t
            b.isAllCaps = false
            group.addView(b, LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
        }
        select()
        setting = false
    }

    override fun setSelected(value: Long) {
        setting = true
        selected = value.toInt()
        select()
        setting = false
    }

    // Props come in no set order: the items may follow the selection.
    private fun select() {
        group.getChildAt(selected)?.let { group.check(it.id) }
    }
}

fun makeSegmented(events: SegmentedEvents): Segmented = KotlinSegmented(events)
