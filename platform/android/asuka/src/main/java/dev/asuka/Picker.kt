package dev.asuka

import android.annotation.SuppressLint
import android.content.Context
import android.content.res.ColorStateList
import android.text.InputType
import android.view.ContextThemeWrapper
import android.view.View
import android.widget.FrameLayout
import android.widget.LinearLayout
import com.google.android.material.textfield.MaterialAutoCompleteTextView
import com.google.android.material.textfield.TextInputLayout
import java.nio.ByteBuffer
import java.nio.ByteOrder
import kotlin.math.ceil

/**
 * A Picker: Material 3's exposed dropdown menu, an outlined
 * TextInputLayout over a MaterialAutoCompleteTextView the user can't type
 * in (its input type none): the chosen item is its text, the Label its
 * floating label, and tapping it opens the menu of items. TalkBack reads
 * it as a drop-down list (Material's end icon makes it one). Its views
 * are made again with the theme (restyle), keeping what it shows.
 */
@SuppressLint("ViewConstructor")
class AsukaPicker(private val rt: AsukaRuntime, private val asukaID: Int) : FrameLayout(rt.themed) {
    private var items: List<String> = emptyList()
    private var chosen = -1
    private var label = ""
    private var tint = 0L
    private lateinit var box: TextInputLayout
    private lateinit var field: Field

    init { make() }

    /**
     * The field: as wide as its widest item (or label), whichever is
     * chosen, so choosing doesn't move it, and the end icon beside it.
     * TextInputLayout makes the icon's room in the field (a drawable as
     * wide) only after it's laid out: endSpace is that room, from the start.
     */
    private inner class Field(context: Context) : MaterialAutoCompleteTextView(context) {
        var endSpace = 0

        override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
            super.onMeasure(widthMeasureSpec, heightMeasureSpec)
            if (MeasureSpec.getMode(widthMeasureSpec) == MeasureSpec.EXACTLY) return
            var widest = paint.measureText(label)
            for (s in items) widest = maxOf(widest, paint.measureText(s))
            var w = ceil(widest).toInt() + compoundPaddingStart + maxOf(compoundPaddingEnd, endSpace)
            if (MeasureSpec.getMode(widthMeasureSpec) == MeasureSpec.AT_MOST) w = minOf(w, MeasureSpec.getSize(widthMeasureSpec))
            if (w > measuredWidth) setMeasuredDimension(w, measuredHeight)
        }
    }

    private fun make() {
        val enabled = if (::box.isInitialized) box.isEnabled else true
        removeAllViews()
        // TextInputLayout's style (its textInputStyle) brings the overlay the
        // field's style comes from: make the field with its context.
        box = TextInputLayout(ContextThemeWrapper(rt.themed, R.style.ThemeOverlay_Asuka_Picker))
        field = Field(box.context).apply {
            inputType = InputType.TYPE_NULL // chosen, not typed: Material's non-editable dropdown
            isSingleLine = true
            setOnItemClickListener { _, _, position, _ ->
                if (position != chosen) {
                    chosen = position
                    rt.emit(asukaID, AsukaEventKind.SELECTED) { AsukaEvents.selected(asukaID, position.toLong()) }
                }
            }
        }
        box.addView(field, LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, LinearLayout.LayoutParams.WRAP_CONTENT))
        addView(box, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT))
        box.hint = label
        field.setSimpleItems(items.toTypedArray())
        show()
        applyTint()
        box.isEnabled = enabled
    }

    /** Shows the chosen item, or none (the label in the field). */
    private fun show() {
        // false: without filtering the menu down to it.
        field.setText(if (chosen in items.indices) items[chosen] else "", false)
        if (chosen in items.indices) field.listSelection = chosen
    }

    fun setItems(b: ByteBuffer) {
        val r = b.duplicate().order(ByteOrder.LITTLE_ENDIAN)
        items = if (r.remaining() < 4) emptyList() else List(r.int) { ByteArray(r.int).also { a -> r.get(a) }.toString(Charsets.UTF_8) }
        field.setSimpleItems(items.toTypedArray())
        show()
        requestLayout()
    }

    fun select(i: Int) {
        chosen = i
        show()
    }

    fun setLabel(s: String) {
        label = s
        box.hint = s
        requestLayout()
    }

    /** Style.Color: the outline and label while it's focused (Material's are the primary color). */
    fun setTint(c: Long) {
        tint = c
        applyTint()
    }

    private fun applyTint() {
        if (tint == 0L) return
        val c = rt.color(tint)
        box.boxStrokeColor = c
        box.hintTextColor = ColorStateList.valueOf(c)
    }

    /** Made again with the current theme (dark mode, the accent, the font scale). */
    fun restyle() = make()

    override fun setEnabled(enabled: Boolean) {
        super.setEnabled(enabled)
        box.isEnabled = enabled // and the field and its icon
    }

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        super.onMeasure(widthMeasureSpec, heightMeasureSpec)
        // The end icon's room, as TextInputLayout will make it (its end
        // layout's width and start margin), measured with the field: the
        // field's own measurement is again with it, the first time.
        val space = endSpace()
        if (space != field.endSpace) {
            field.endSpace = space
            field.requestLayout()
            super.onMeasure(widthMeasureSpec, heightMeasureSpec)
        }
    }

    /** The width of the layout holding the end icon, with its start margin (0 without one). */
    private fun endSpace(): Int {
        var v: View = box.findViewById(com.google.android.material.R.id.text_input_end_icon) ?: return 0
        // Up to the field's sibling: the end layout.
        while (v.parent !== field.parent) v = v.parent as? View ?: return 0
        if (v.visibility == View.GONE) return 0
        val ms = (v.layoutParams as? MarginLayoutParams)?.marginStart ?: 0
        return v.measuredWidth + ms
    }
}

/** Sets a Picker's prop; false if prop isn't one of its own. */
fun AsukaRuntime.setPickerProp(n: AsukaNode, prop: Int, v: Any): Boolean {
    val p = n.view as? AsukaPicker ?: return false
    when (prop) {
        AsukaProp.Picker.ITEMS -> p.setItems(v as ByteBuffer)
        AsukaProp.Picker.SELECTED -> p.select((v as Long).toInt())
        AsukaProp.Picker.LABEL -> p.setLabel(v as String)
        else -> return false
    }
    return true
}
