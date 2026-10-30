package dev.nui

import android.annotation.SuppressLint
import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Path
import android.view.View
import android.view.accessibility.AccessibilityNodeInfo
import android.widget.GridLayout
import android.widget.LinearLayout
import com.google.android.material.color.MaterialColors
import com.google.android.material.dialog.MaterialAlertDialogBuilder
import com.google.android.material.slider.Slider

/**
 * The palette Google's apps offer (Calendar's colors, and grays), with
 * their names, which TalkBack reads.
 */
private val palette = listOf(
    "Tomato" to 0xD50000, "Flamingo" to 0xE67C73, "Tangerine" to 0xF4511E, "Pumpkin" to 0xEF6C00, "Banana" to 0xF6BF26,
    "Citron" to 0xE4C441, "Avocado" to 0xC0CA33, "Sage" to 0x33B679, "Basil" to 0x0B8043, "Eucalyptus" to 0x009688,
    "Peacock" to 0x039BE5, "Cobalt" to 0x4285F4, "Blueberry" to 0x3F51B5, "Lavender" to 0x7986CB, "Wisteria" to 0xB39DDB,
    "Grape" to 0x8E24AA, "Amethyst" to 0x9E69AF, "Cocoa" to 0x795548, "Birch" to 0xA79B8E, "Graphite" to 0x616161,
    "Black" to 0x000000, "Charcoal" to 0x424242, "Gray" to 0x9E9E9E, "Silver" to 0xE0E0E0, "White" to 0xFFFFFF,
)

/** A color as nui's RGBA Color (ops.RGBA). */
fun nuiRGBA(c: Int): Long =
    (2L shl 32) or (Color.red(c).toLong() shl 24) or (Color.green(c).toLong() shl 16) or (Color.blue(c).toLong() shl 8) or Color.alpha(c).toLong()

/** A round swatch of a color, outlined so white and clear show; checked, a check mark on it. */
@SuppressLint("ViewConstructor")
open class NuiSwatch(ctx: Context, private val density: Float, private val diameter: Float) : View(ctx) {
    var color = Color.TRANSPARENT
        set(v) { field = v; invalidate(); colorChanged() }
    var checked = false
        set(v) { field = v; invalidate() }
    private val fill = Paint(Paint.ANTI_ALIAS_FLAG)
    private val ring = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = density
        color = MaterialColors.getColor(ctx, com.google.android.material.R.attr.colorOutline, Color.GRAY)
    }
    private val mark = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = 2.5f * density
        strokeCap = Paint.Cap.ROUND
        strokeJoin = Paint.Join.ROUND
    }
    private val check = Path()

    protected open fun colorChanged() {}

    // Material's touch target.
    override fun onMeasure(widthSpec: Int, heightSpec: Int) {
        val s = Math.round(48 * density)
        setMeasuredDimension(s, s)
    }

    override fun onDraw(canvas: Canvas) {
        val cx = width / 2f
        val cy = height / 2f
        val r = diameter * density / 2
        fill.color = color
        canvas.drawCircle(cx, cy, r, fill)
        canvas.drawCircle(cx, cy, r - ring.strokeWidth / 2, ring)
        if (checked) {
            // A mark that shows on light and dark colors alike.
            val light = Color.alpha(color) < 128 || (0.299 * Color.red(color) + 0.587 * Color.green(color) + 0.114 * Color.blue(color)) > 160
            mark.color = if (light) Color.BLACK else Color.WHITE
            val u = r / 2.2f
            check.reset()
            check.moveTo(cx - u, cy)
            check.lineTo(cx - u / 3, cy + u * 0.7f)
            check.lineTo(cx + u, cy - u * 0.6f)
            canvas.drawPath(check, mark)
        }
    }

    override fun onInitializeAccessibilityNodeInfo(info: AccessibilityNodeInfo) {
        super.onInitializeAccessibilityNodeInfo(info)
        info.isCheckable = true
        info.isChecked = checked
    }
}

/**
 * A ColorPicker: Android has none (nor has Material), so a swatch of the
 * color that opens a Material dialog of a palette, as Google's apps do,
 * and an opacity slider for Alpha.
 */
@SuppressLint("ViewConstructor")
class NuiColorPicker(private val rt: NuiRuntime, private val nuiID: Int) : NuiSwatch(rt.themed, rt.density, 28f) {
    var withAlpha = false
    var title = ""

    init {
        isClickable = true
        isFocusable = true
        contentDescription = "Color"
        setBackgroundResource(android.util.TypedValue().also {
            rt.themed.theme.resolveAttribute(android.R.attr.selectableItemBackgroundBorderless, it, true)
        }.resourceId)
        setOnClickListener { open() }
    }

    override fun onInitializeAccessibilityNodeInfo(info: AccessibilityNodeInfo) {
        super.onInitializeAccessibilityNodeInfo(info)
        info.isCheckable = false
        info.className = android.widget.Button::class.java.name
    }

    // TalkBack says the color: its name in the palette, or its hex.
    override fun colorChanged() {
        androidx.core.view.ViewCompat.setStateDescription(this,
            palette.firstOrNull { it.second == (color and 0xffffff) }?.first ?: String.format("#%06X", color and 0xffffff))
    }

    private fun pick(c: Int) {
        color = c
        rt.emit(nuiID, NuiEventKind.COLOR_CHANGED) { NuiEvents.colorChanged(nuiID, nuiRGBA(c)) }
    }

    private fun open() {
        val act = rt.activity ?: return
        val d = rt.density
        val grid = GridLayout(act).apply { columnCount = 5 }
        val swatches = palette.map { (name, rgb) ->
            NuiSwatch(act, d, 36f).apply {
                color = Color.rgb(rgb shr 16 and 0xff, rgb shr 8 and 0xff, rgb and 0xff)
                checked = rgb == (this@NuiColorPicker.color and 0xffffff)
                contentDescription = name
                isClickable = true
                setBackgroundResource(android.util.TypedValue().also {
                    act.theme.resolveAttribute(android.R.attr.selectableItemBackgroundBorderless, it, true)
                }.resourceId)
            }.also { grid.addView(it) }
        }
        val body = LinearLayout(act).apply {
            orientation = LinearLayout.VERTICAL
            val p = Math.round(20 * d)
            setPadding(p, Math.round(8 * d), p, 0)
            addView(grid, LinearLayout.LayoutParams(LinearLayout.LayoutParams.WRAP_CONTENT, LinearLayout.LayoutParams.WRAP_CONTENT).apply {
                gravity = android.view.Gravity.CENTER_HORIZONTAL
            })
        }
        var opacity: Slider? = null
        if (withAlpha) {
            opacity = Slider(act).apply {
                valueFrom = 0f
                valueTo = 100f // continuous: a step of 1 draws a hundred ticks
                value = Math.round(Color.alpha(this@NuiColorPicker.color) * 100f / 255).toFloat()
                contentDescription = "Opacity"
                setLabelFormatter { "${it.toInt()}%" }
                addOnChangeListener { _, v, fromUser ->
                    if (fromUser) pick((this@NuiColorPicker.color and 0xffffff) or (Math.round(v * 255 / 100) shl 24))
                }
            }
            body.addView(android.widget.TextView(act).apply {
                text = "Opacity"
                setTextAppearance(com.google.android.material.R.style.TextAppearance_Material3_LabelLarge)
                setTextColor(MaterialColors.getColor(this, com.google.android.material.R.attr.colorOnSurfaceVariant))
                importantForAccessibility = View.IMPORTANT_FOR_ACCESSIBILITY_NO // the slider says it
                setPadding(Math.round(4 * d), Math.round(16 * d), 0, 0)
            })
            body.addView(opacity, LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT).apply {
                topMargin = Math.round(8 * d)
            })
        }
        val b = MaterialAlertDialogBuilder(act).setView(body)
        b.setTitle(title.ifEmpty { "Color" })
        if (withAlpha) b.setPositiveButton(android.R.string.ok, null) else b.setNegativeButton(android.R.string.cancel, null)
        val dialog = b.show()
        for ((i, s) in swatches.withIndex()) {
            s.setOnClickListener {
                val a = if (opacity != null) Math.round(opacity.value * 255 / 100) else 255
                pick((palette[i].second and 0xffffff) or (a shl 24))
                swatches.forEach { it.checked = it === s }
                // A tap chooses, as in Google's apps; with an opacity to
                // set too, the dialog stays until OK.
                if (!withAlpha) dialog.dismiss()
            }
        }
    }
}
