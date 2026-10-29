package dev.nui

import android.annotation.SuppressLint
import android.content.res.ColorStateList
import android.graphics.Color
import android.graphics.drawable.Drawable
import androidx.appcompat.content.res.AppCompatResources
import com.google.android.material.color.MaterialColors
import com.google.android.material.textview.MaterialTextView

/** The symbol for an SF Symbol name (ui.Icon): its Material Symbol, or null. */
fun NuiRuntime.symbol(name: String): Drawable? {
    val res = symbolDrawables[name] ?: symbolDrawables[name.removeSuffix(".fill")]
    if (res == null) {
        if (name.isNotEmpty()) android.util.Log.w("nui", "no Android symbol for \"$name\" (see hosts/android/gen_symbols.go)")
        return null
    }
    return AppCompatResources.getDrawable(themed, res)?.mutate()
}

/**
 * A Symbol: a Material Symbol sized by its text props, as Material sizes
 * icons beside text (24dp at the body's 16sp), and tinted by its color
 * (the theme's onSurface by default).
 */
@SuppressLint("ViewConstructor")
class NuiSymbol(private val rt: NuiRuntime) : androidx.appcompat.widget.AppCompatImageView(rt.themed) {
    var name = ""
    var textStyle = 0
    var size = 0f
    var tint = 0L

    init {
        scaleType = ScaleType.FIT_CENTER
        importantForAccessibility = IMPORTANT_FOR_ACCESSIBILITY_NO // decoration, unless the app labels it
    }

    fun refresh() {
        setImageDrawable(rt.symbol(name))
        applyTint()
    }

    fun applyTint() {
        imageTintList = ColorStateList.valueOf(if (tint != 0L) rt.color(tint)
            else MaterialColors.getColor(rt.themed, com.google.android.material.R.attr.colorOnSurface, Color.BLACK))
    }

    /** Its size in px: 1.5 times its text's size. */
    fun sizePx(): Int {
        val tv = MaterialTextView(rt.themed)
        rt.applyTextStyle(tv, textStyle, size, 0)
        return Math.round(tv.textSize * 1.5f)
    }
}
