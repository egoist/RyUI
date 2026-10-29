package dev.nui

import android.annotation.SuppressLint
import android.view.ContextThemeWrapper
import android.view.View
import com.google.android.material.button.MaterialButton
import com.google.android.material.button.MaterialButtonToggleGroup
import com.google.android.material.progressindicator.LinearProgressIndicator
import java.nio.ByteBuffer

/**
 * A Segmented control: Material 3's segmented buttons, outlined buttons in
 * a toggle group with one always selected. The user's choice is reported
 * with Selected.
 */
@SuppressLint("ViewConstructor")
class NuiSegmented(private val rt: NuiRuntime, private val id: Int) : MaterialButtonToggleGroup(rt.themed) {
    private var setting = false

    init {
        isSingleSelection = true
        isSelectionRequired = true
        addOnButtonCheckedListener { _, buttonId, checked ->
            if (checked && !setting) {
                val i = indexOfChild(findViewById(buttonId)).toLong()
                rt.emit(id, NuiEventKind.SELECTED) { NuiEvents.selected(id, i) }
            }
        }
    }

    fun setSegments(b: ByteBuffer) {
        val selected = checkedButtonId.let { if (it == View.NO_ID) -1 else indexOfChild(findViewById(it)) }
        setting = true
        removeAllViews()
        if (b.remaining() >= 4) {
            repeat(b.int) {
                val a = ByteArray(b.int)
                b.get(a)
                addView(MaterialButton(ContextThemeWrapper(rt.themed, R.style.ThemeOverlay_Nui_OutlinedButton)).apply {
                    id = View.generateViewId()
                    text = String(a, Charsets.UTF_8)
                    isAllCaps = false
                })
            }
        }
        setting = false
        if (selected >= 0) select(selected)
    }

    override fun onMeasure(widthSpec: Int, heightSpec: Int) {
        // Given a width (stretched, sized): the segments share it, as
        // UIKit's do; otherwise each is as wide as its title.
        val exact = MeasureSpec.getMode(widthSpec) == MeasureSpec.EXACTLY
        for (i in 0 until childCount) {
            val lp = getChildAt(i).layoutParams as LayoutParams
            lp.width = if (exact) 0 else LayoutParams.WRAP_CONTENT
            lp.weight = if (exact) 1f else 0f
        }
        super.onMeasure(widthSpec, heightSpec)
    }

    fun select(i: Int) {
        val b = getChildAt(i) ?: return
        setting = true
        check(b.id)
        setting = false
    }
}

/** A Progress bar: Material's linear indicator, in thousandths. */
fun NuiRuntime.makeProgress() = LinearProgressIndicator(themed).apply {
    max = 1000
    isIndeterminate = false
}

fun NuiRuntime.setProgress(p: LinearProgressIndicator, v: Double) {
    p.setProgressCompat(Math.round(v * 1000).toInt(), p.isAttachedToWindow)
}
