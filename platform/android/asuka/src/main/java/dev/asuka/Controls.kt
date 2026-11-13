package dev.asuka

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
class AsukaSegmented(private val rt: AsukaRuntime, private val id: Int) : MaterialButtonToggleGroup(rt.themed) {
    private var setting = false

    init {
        isSingleSelection = true
        isSelectionRequired = true
        // One control, as Material 3's segmented buttons are: the group's ends
        // round and its segments share their edges. The theme's Expressive
        // default is separate pills, with an inverse color for the checked one.
        spacing = 0
        innerCornerSize = com.google.android.material.shape.AbsoluteCornerSize(0f)
        shapeAppearance = com.google.android.material.shape.ShapeAppearanceModel.builder()
            .setAllCornerSizes(com.google.android.material.shape.RelativeCornerSize(0.5f)).build()
        addOnButtonCheckedListener { _, buttonId, checked ->
            if (checked && !setting) {
                val i = indexOfChild(findViewById(buttonId)).toLong()
                rt.emit(id, AsukaEventKind.SELECTED) { AsukaEvents.selected(id, i) }
            }
        }
    }

    private var titles = emptyList<String>()

    fun setSegments(b: ByteBuffer) {
        val list = ArrayList<String>()
        if (b.remaining() >= 4) {
            repeat(b.int) {
                val a = ByteArray(b.int)
                b.get(a)
                list.add(String(a, Charsets.UTF_8))
            }
        }
        titles = list
        makeButtons()
    }

    /** Makes the buttons again with the current theme (dark mode, the accent), keeping the selection. */
    fun restyle() = makeButtons()

    private fun makeButtons() {
        val selected = checkedButtonId.let { if (it == View.NO_ID) -1 else indexOfChild(findViewById(it)) }
        setting = true
        removeAllViews()
        val c = ContextThemeWrapper(rt.themed, R.style.ThemeOverlay_Asuka_SegmentedButton)
        fun role(attr: Int) = com.google.android.material.color.MaterialColors.getColor(c, attr, 0)
        val checkedState = intArrayOf(android.R.attr.state_checked)
        fun colors(on: Int, off: Int) = android.content.res.ColorStateList(arrayOf(checkedState, intArrayOf()), intArrayOf(on, off))
        val background = colors(role(com.google.android.material.R.attr.colorSecondaryContainer), android.graphics.Color.TRANSPARENT)
        val foreground = colors(role(com.google.android.material.R.attr.colorOnSecondaryContainer), role(com.google.android.material.R.attr.colorOnSurface))
        val outline = android.content.res.ColorStateList.valueOf(role(com.google.android.material.R.attr.colorOutline))
        val d = resources.displayMetrics.density
        for (t in titles) {
            addView(MaterialButton(c).apply {
                id = View.generateViewId()
                text = t
                isAllCaps = false
                maxLines = 1
                ellipsize = android.text.TextUtils.TruncateAt.END
                backgroundTintList = background
                setTextColor(foreground)
                strokeColor = outline
                strokeWidth = Math.round(d)
                insetTop = 0
                insetBottom = 0
                minimumWidth = 0
                minWidth = 0
                setPadding(Math.round(8 * d), paddingTop, Math.round(8 * d), paddingBottom)
            })
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
fun AsukaRuntime.makeProgress() = LinearProgressIndicator(themed).apply {
    max = 1000
    isIndeterminate = false
}

fun AsukaRuntime.setProgress(p: LinearProgressIndicator, v: Double) {
    p.setProgressCompat(Math.round(v * 1000).toInt(), p.isAttachedToWindow)
}

/**
 * A DatePicker: Material's pattern, buttons showing the date and the time
 * (in the device's format) that open Material's date and time pickers.
 */
@SuppressLint("ViewConstructor")
class AsukaDatePicker(private val rt: AsukaRuntime, private val id: Int) : android.widget.LinearLayout(rt.themed) {
    var value = System.currentTimeMillis()
        set(v) { field = v; refresh() }
    var mode = AsukaDateMode.DATE
        set(v) { field = v; refresh() }
    var min = 0L
    var max = 0L
    private val date = button { pickDate() }
    private val time = button { pickTime() }

    init {
        orientation = HORIZONTAL
        addView(date)
        addView(time, LayoutParams(LayoutParams.WRAP_CONTENT, LayoutParams.WRAP_CONTENT).apply { marginStart = Math.round(8 * rt.density) })
        refresh()
    }

    private fun button(click: () -> Unit) = MaterialButton(ContextThemeWrapper(rt.themed, R.style.ThemeOverlay_Asuka_TonalButton)).apply {
        isAllCaps = false
        setOnClickListener { click() }
    }

    override fun setEnabled(enabled: Boolean) {
        super.setEnabled(enabled)
        date.isEnabled = enabled
        time.isEnabled = enabled
    }

    /** Shows the value, in the language. */
    fun refresh() {
        val d = java.util.Date(value)
        date.text = java.text.DateFormat.getDateInstance(java.text.DateFormat.MEDIUM).format(d)
        time.text = android.text.format.DateFormat.getTimeFormat(context).format(d)
        date.visibility = if (mode == AsukaDateMode.TIME) GONE else VISIBLE
        time.visibility = if (mode == AsukaDateMode.DATE) GONE else VISIBLE
        (time.layoutParams as? LayoutParams)?.marginStart = if (mode == AsukaDateMode.DATE_TIME) Math.round(8 * rt.density) else 0
    }

    private fun local() = java.util.Calendar.getInstance().apply { timeInMillis = value }

    private fun choose(ms: Long) {
        var v = ms
        if (min != 0L && v < min) v = min
        if (max != 0L && v > max) v = max
        value = v
        val sent = v.toDouble()
        rt.emit(id, AsukaEventKind.VALUE_CHANGED) { AsukaEvents.valueChanged(id, sent) }
    }

    private fun pickDate() {
        val act = rt.activity ?: return
        val c = local()
        // Material's picker selects UTC midnights: the local date's.
        val utc = java.util.Calendar.getInstance(java.util.TimeZone.getTimeZone("UTC")).apply {
            clear()
            set(c.get(java.util.Calendar.YEAR), c.get(java.util.Calendar.MONTH), c.get(java.util.Calendar.DAY_OF_MONTH))
        }
        val constraints = com.google.android.material.datepicker.CalendarConstraints.Builder()
        constraints.setFirstDayOfWeek(rt.region.firstWeekday)
        val validators = ArrayList<com.google.android.material.datepicker.CalendarConstraints.DateValidator>()
        if (min != 0L) validators.add(com.google.android.material.datepicker.DateValidatorPointForward.from(utcDay(min)))
        if (max != 0L) validators.add(com.google.android.material.datepicker.DateValidatorPointBackward.before(utcDay(max) + 1))
        if (validators.isNotEmpty()) constraints.setValidator(com.google.android.material.datepicker.CompositeDateValidator.allOf(validators))
        val p = com.google.android.material.datepicker.MaterialDatePicker.Builder.datePicker()
            .setSelection(utc.timeInMillis)
            .setCalendarConstraints(constraints.build())
            .build()
        p.addOnPositiveButtonClickListener { sel ->
            val picked = java.util.Calendar.getInstance(java.util.TimeZone.getTimeZone("UTC")).apply { timeInMillis = sel }
            val out = local().apply {
                set(picked.get(java.util.Calendar.YEAR), picked.get(java.util.Calendar.MONTH), picked.get(java.util.Calendar.DAY_OF_MONTH))
            }
            choose(out.timeInMillis)
        }
        p.show(act.supportFragmentManager, "asuka-date-$id")
        act.supportFragmentManager.executePendingTransactions()
        p.dialog?.let { rt.directDialog(it) }
    }

    /** The UTC midnight of ms's local date. */
    private fun utcDay(ms: Long): Long {
        val c = java.util.Calendar.getInstance().apply { timeInMillis = ms }
        return java.util.Calendar.getInstance(java.util.TimeZone.getTimeZone("UTC")).apply {
            clear()
            set(c.get(java.util.Calendar.YEAR), c.get(java.util.Calendar.MONTH), c.get(java.util.Calendar.DAY_OF_MONTH))
        }.timeInMillis
    }

    private fun pickTime() {
        val act = rt.activity ?: return
        val c = local()
        val p = com.google.android.material.timepicker.MaterialTimePicker.Builder()
            .setTimeFormat(if (android.text.format.DateFormat.is24HourFormat(context)) com.google.android.material.timepicker.TimeFormat.CLOCK_24H
                else com.google.android.material.timepicker.TimeFormat.CLOCK_12H)
            .setHour(c.get(java.util.Calendar.HOUR_OF_DAY))
            .setMinute(c.get(java.util.Calendar.MINUTE))
            .build()
        p.addOnPositiveButtonClickListener {
            choose(local().apply {
                set(java.util.Calendar.HOUR_OF_DAY, p.hour)
                set(java.util.Calendar.MINUTE, p.minute)
                set(java.util.Calendar.SECOND, 0)
                set(java.util.Calendar.MILLISECOND, 0)
            }.timeInMillis)
        }
        p.show(act.supportFragmentManager, "asuka-time-$id")
        act.supportFragmentManager.executePendingTransactions()
        p.dialog?.let { rt.directDialog(it) }
    }
}
