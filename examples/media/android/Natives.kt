// The app's native views for ui.Native. The RyUI CLI builds this file into
// the Android app.
package dev.ryui.app

import android.view.View
import androidx.appcompat.view.ContextThemeWrapper
import com.google.android.material.button.MaterialButton
import com.google.android.material.button.MaterialButtonToggleGroup
import dev.ryui.RyUINativeContext
import dev.ryui.RyUINativeModule
import dev.ryui.RyUINativeView
import dev.ryui.RyUIRuntime

object RyUIAppNatives : RyUINativeModule {
    override fun register(runtime: RyUIRuntime) {
        runtime.registerNative("Segmented") { ctx -> Segmented(ctx) }
    }
}

/**
 * Material's segmented buttons: props "items" (comma-separated) and
 * "selected"; event "changed" with "index".
 */
class Segmented(private val ctx: RyUINativeContext) : RyUINativeView {
    private val group = MaterialButtonToggleGroup(ctx.context).apply {
        isSingleSelection = true
        isSelectionRequired = true
    }
    override val view: View get() = group
    private var setting = false

    init {
        group.addOnButtonCheckedListener { g, id, checked ->
            if (checked && !setting) ctx.emit("changed", mapOf("index" to g.indexOfChild(g.findViewById(id)).toLong()))
        }
    }

    override fun set(name: String, value: Any) {
        setting = true
        when (name) {
            "items" -> {
                val sel = group.indexOfChild(group.findViewById(group.checkedButtonId))
                group.removeAllViews()
                for (t in (value as String).split(",")) {
                    val b = MaterialButton(ContextThemeWrapper(ctx.context, dev.ryui.R.style.ThemeOverlay_RyUI_OutlinedButton))
                    b.id = View.generateViewId()
                    b.text = t
                    b.isAllCaps = false
                    group.addView(b, android.widget.LinearLayout.LayoutParams(0, android.widget.LinearLayout.LayoutParams.WRAP_CONTENT, 1f))
                }
                if (sel >= 0) select(sel)
            }
            "selected" -> select((value as Long).toInt())
        }
        setting = false
    }

    private fun select(i: Int) {
        group.getChildAt(i)?.let { group.check(it.id) }
    }
}
