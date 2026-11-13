package dev.asuka

import android.annotation.SuppressLint
import android.content.res.ColorStateList
import android.widget.NumberPicker
import com.google.android.material.button.MaterialButton
import com.google.android.material.color.MaterialColors
import java.nio.ByteBuffer
import java.nio.ByteOrder

/** A Wheel: Android's NumberPicker, its values the core's items. */
@SuppressLint("ViewConstructor")
class AsukaWheel(rt: AsukaRuntime, asukaID: Int) : NumberPicker(rt.themed) {
    private var setting = false

    init {
        wrapSelectorWheel = false
        descendantFocusability = FOCUS_BLOCK_DESCENDANTS // a wheel, not a field to type in
        setOnValueChangedListener { _, _, v ->
            if (!setting) rt.emit(asukaID, AsukaEventKind.SELECTED) { AsukaEvents.selected(asukaID, v.toLong()) }
        }
    }

    fun setItems(b: ByteBuffer) {
        val r = b.duplicate().order(ByteOrder.LITTLE_ENDIAN)
        val items = if (r.remaining() < 4) emptyList() else List(r.int) { ByteArray(r.int).also { a -> r.get(a) }.toString(Charsets.UTF_8) }
        setting = true
        displayedValues = null // before the range changes, which it must fit
        minValue = 0
        maxValue = maxOf(items.size - 1, 0)
        displayedValues = if (items.isEmpty()) arrayOf("") else items.toTypedArray()
        setting = false
    }

    fun select(i: Int) {
        setting = true
        value = i.coerceIn(minValue, maxValue)
        setting = false
    }
}

/**
 * A Stepper: Android has none (nor has Material, nor Compose), so a pair
 * of Material icon buttons, minus and plus, which step a value between a
 * minimum and maximum, each off at its end.
 */
@SuppressLint("ViewConstructor")
// asukaID, not id: inside a view's apply block, id is that view's.
class AsukaStepper(private val rt: AsukaRuntime, private val asukaID: Int) : android.view.ViewGroup(rt.themed) {
    var value = 0.0
        set(v) { field = v; ends() }
    var min = 0.0
        set(v) { field = v; ends() }
    var max = 100.0
        set(v) { field = v; ends() }
    var step = 1.0
    private val minus = button("minus", "Decrement") { -step }
    private val plus = button("plus", "Increment") { step }

    init {
        addView(minus)
        addView(plus)
        ends()
    }

    // Two square icon buttons, each Material's touch target (48dp), a
    // little apart.
    private val side get() = Math.round(48 * rt.density)
    private val gap get() = Math.round(4 * rt.density)

    override fun onMeasure(widthSpec: Int, heightSpec: Int) {
        val s = MeasureSpec.makeMeasureSpec(side, MeasureSpec.EXACTLY)
        minus.measure(s, s)
        plus.measure(s, s)
        setMeasuredDimension(2 * side + gap, side)
    }

    override fun onLayout(changed: Boolean, l: Int, t: Int, r: Int, b: Int) {
        val y = maxOf(0, (b - t - side) / 2)
        // Minus first, in the direction it reads.
        val rtl = layoutDirection == LAYOUT_DIRECTION_RTL
        val (first, second) = if (rtl) plus to minus else minus to plus
        first.layout(0, y, side, y + side)
        second.layout(side + gap, y, 2 * side + gap, y + side)
    }

    private fun button(symbol: String, label: String, by: () -> Double) =
        MaterialButton(rt.themed, null, com.google.android.material.R.attr.materialIconButtonOutlinedStyle).apply {
            icon = rt.symbol(symbol)
            iconPadding = 0
            iconGravity = MaterialButton.ICON_GRAVITY_TEXT_START
            setPadding(0, 0, 0, 0)
            insetTop = 0
            insetBottom = 0
            iconTint = ColorStateList.valueOf(MaterialColors.getColor(this, androidx.appcompat.R.attr.colorPrimary))
            contentDescription = label
            setOnClickListener {
                val v = (value + by()).coerceIn(min, max)
                if (v != value) {
                    value = v
                    rt.emit(asukaID, AsukaEventKind.VALUE_CHANGED) { AsukaEvents.valueChanged(asukaID, v) }
                }
            }
        }

    private fun ends() {
        minus.isEnabled = isEnabled && value > min
        plus.isEnabled = isEnabled && value < max
    }

    override fun setEnabled(enabled: Boolean) {
        super.setEnabled(enabled)
        ends()
    }
}

/** Sets a Wheel's, Stepper's or ColorPicker's prop; false if prop isn't one. */
fun AsukaRuntime.setWheelProp(n: AsukaNode, prop: Int, v: Any): Boolean {
    when (val view = n.view) {
        is AsukaWheel -> when (prop) {
            AsukaProp.Wheel.ITEMS -> view.setItems(v as ByteBuffer)
            AsukaProp.Wheel.SELECTED -> view.select((v as Long).toInt())
            else -> return false
        }
        is AsukaStepper -> when (prop) {
            AsukaProp.Stepper.VALUE -> view.value = v as Double
            AsukaProp.Stepper.MIN -> view.min = v as Double
            AsukaProp.Stepper.MAX -> view.max = v as Double
            AsukaProp.Stepper.STEP -> view.step = v as Double
            else -> return false
        }
        is AsukaColorPicker -> when (prop) {
            AsukaProp.ColorPicker.VALUE -> view.color = color(v as Long)
            AsukaProp.ColorPicker.ALPHA -> view.withAlpha = v as Boolean
            AsukaProp.ColorPicker.TITLE -> view.title = v as String
            else -> return false
        }
        else -> return false
    }
    return true
}
