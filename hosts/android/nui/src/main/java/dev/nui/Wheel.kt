package dev.nui

import android.annotation.SuppressLint
import android.content.res.ColorStateList
import android.widget.NumberPicker
import com.google.android.material.button.MaterialButton
import com.google.android.material.color.MaterialColors
import java.nio.ByteBuffer
import java.nio.ByteOrder

/** A Wheel: Android's NumberPicker, its values the core's items. */
@SuppressLint("ViewConstructor")
class NuiWheel(rt: NuiRuntime, nuiID: Int) : NumberPicker(rt.themed) {
    private var setting = false

    init {
        wrapSelectorWheel = false
        descendantFocusability = FOCUS_BLOCK_DESCENDANTS // a wheel, not a field to type in
        setOnValueChangedListener { _, _, v ->
            if (!setting) rt.emit(nuiID, NuiEventKind.SELECTED) { NuiEvents.selected(nuiID, v.toLong()) }
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
// nuiID, not id: inside a view's apply block, id is that view's.
class NuiStepper(private val rt: NuiRuntime, private val nuiID: Int) : android.view.ViewGroup(rt.themed) {
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
        minus.layout(0, y, side, y + side)
        plus.layout(side + gap, y, 2 * side + gap, y + side)
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
                    rt.emit(nuiID, NuiEventKind.VALUE_CHANGED) { NuiEvents.valueChanged(nuiID, v) }
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

/** Sets a Wheel's or Stepper's prop; false if prop isn't one. */
fun NuiRuntime.setWheelProp(n: NuiNode, prop: Int, v: Any): Boolean {
    when (val view = n.view) {
        is NuiWheel -> when (prop) {
            NuiProp.Wheel.ITEMS -> view.setItems(v as ByteBuffer)
            NuiProp.Wheel.SELECTED -> view.select((v as Long).toInt())
            else -> return false
        }
        is NuiStepper -> when (prop) {
            NuiProp.Stepper.VALUE -> view.value = v as Double
            NuiProp.Stepper.MIN -> view.min = v as Double
            NuiProp.Stepper.MAX -> view.max = v as Double
            NuiProp.Stepper.STEP -> view.step = v as Double
            else -> return false
        }
        else -> return false
    }
    return true
}
