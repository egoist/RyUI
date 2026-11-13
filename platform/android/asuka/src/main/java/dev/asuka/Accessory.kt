package dev.asuka

import android.view.Gravity
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsAnimationCompat
import androidx.core.view.WindowInsetsCompat

/**
 * A TextField's Accessory: Android has no bar over the keyboard of its
 * own, so while its field is focused the bar is put in the field's
 * window (the activity's, or a sheet's), at its bottom, and lifted by the
 * keyboard's height as the keyboard moves, frame by frame. It shows only
 * while the keyboard does.
 */
class AsukaAccessory(private val rt: AsukaRuntime, private val n: AsukaNode) {
    var height = 0f
        set(v) {
            field = v
            n.view.layoutParams?.let { it.height = Math.round(v * rt.density); n.view.layoutParams = it }
        }

    init {
        val v = n.view
        ViewCompat.setWindowInsetsAnimationCallback(v, object : WindowInsetsAnimationCompat.Callback(DISPATCH_MODE_CONTINUE_ON_SUBTREE) {
            override fun onProgress(insets: WindowInsetsCompat, running: MutableList<WindowInsetsAnimationCompat>): WindowInsetsCompat {
                place(insets.getInsets(WindowInsetsCompat.Type.ime()).bottom)
                return insets
            }
        })
        ViewCompat.setOnApplyWindowInsetsListener(v) { _, insets ->
            place(insets.getInsets(WindowInsetsCompat.Type.ime()).bottom)
            insets
        }
    }

    /** Its field got focus: the bar goes in the field's window. */
    fun show(field: View) {
        val decor = field.rootView as? ViewGroup ?: return
        val v = n.view
        if (v.parent !== decor) {
            (v.parent as? ViewGroup)?.removeView(v)
            decor.addView(v, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, Math.round(height * rt.density), Gravity.BOTTOM))
        }
        place(ViewCompat.getRootWindowInsets(field)?.getInsets(WindowInsetsCompat.Type.ime())?.bottom ?: 0)
        ViewCompat.requestApplyInsets(v)
    }

    fun hide() {
        (n.view.parent as? ViewGroup)?.removeView(n.view)
    }

    private fun place(ime: Int) {
        val v = n.view
        v.translationY = -ime.toFloat()
        v.visibility = if (ime > 0) View.VISIBLE else View.INVISIBLE
    }
}
