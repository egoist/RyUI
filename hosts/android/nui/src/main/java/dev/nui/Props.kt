package dev.nui

import android.content.res.ColorStateList
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.os.Build
import android.text.InputType
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.view.inputmethod.EditorInfo
import android.widget.EditText
import android.widget.TextView
import androidx.core.view.AccessibilityDelegateCompat
import androidx.core.view.ViewCompat
import androidx.core.view.accessibility.AccessibilityNodeInfoCompat
import androidx.core.widget.TextViewCompat
import com.google.android.material.button.MaterialButton
import com.google.android.material.color.MaterialColors
import com.google.android.material.materialswitch.MaterialSwitch
import com.google.android.material.progressindicator.CircularProgressIndicator
import com.google.android.material.slider.Slider
import kotlin.math.ceil

private val textAppearances = mapOf(
    NuiTextStyle.LARGE_TITLE to com.google.android.material.R.style.TextAppearance_Material3_DisplaySmall,
    NuiTextStyle.TITLE to com.google.android.material.R.style.TextAppearance_Material3_HeadlineMedium,
    NuiTextStyle.TITLE2 to com.google.android.material.R.style.TextAppearance_Material3_HeadlineSmall,
    NuiTextStyle.TITLE3 to com.google.android.material.R.style.TextAppearance_Material3_TitleLarge,
    NuiTextStyle.HEADLINE to com.google.android.material.R.style.TextAppearance_Material3_TitleMedium,
    NuiTextStyle.SUBHEADLINE to com.google.android.material.R.style.TextAppearance_Material3_TitleSmall,
    NuiTextStyle.BODY to com.google.android.material.R.style.TextAppearance_Material3_BodyLarge,
    NuiTextStyle.CALLOUT to com.google.android.material.R.style.TextAppearance_Material3_BodyMedium,
    NuiTextStyle.FOOTNOTE to com.google.android.material.R.style.TextAppearance_Material3_BodySmall,
    NuiTextStyle.CAPTION to com.google.android.material.R.style.TextAppearance_Material3_LabelMedium,
    NuiTextStyle.CAPTION2 to com.google.android.material.R.style.TextAppearance_Material3_LabelSmall,
)

/** A text style's Material text appearance. */
fun textAppearance(style: Int): Int? = textAppearances[style]

fun weightOf(w: Int): Int = when (w) {
    NuiFontWeight.ULTRALIGHT -> 100
    NuiFontWeight.THIN -> 200
    NuiFontWeight.LIGHT -> 300
    NuiFontWeight.REGULAR -> 400
    NuiFontWeight.MEDIUM -> 500
    NuiFontWeight.SEMIBOLD -> 600
    NuiFontWeight.BOLD -> 700
    NuiFontWeight.HEAVY -> 800
    NuiFontWeight.BLACK -> 900
    else -> 0
}

/** Resolves a core color: a semantic one from the Material 3 theme, or RGBA. */
fun NuiRuntime.color(v: Long, fallback: Int = Color.TRANSPARENT): Int {
    val kind = (v ushr 32).toInt()
    val c = (v and 0xffffffffL).toInt()
    return when (kind) {
        1 -> {
            fun attr(a: Int) = MaterialColors.getColor(themed, a, fallback)
            when (c) {
                NuiSemanticColor.LABEL -> attr(com.google.android.material.R.attr.colorOnSurface)
                NuiSemanticColor.SECONDARY_LABEL -> attr(com.google.android.material.R.attr.colorOnSurfaceVariant)
                NuiSemanticColor.TERTIARY_LABEL -> attr(com.google.android.material.R.attr.colorOutline)
                NuiSemanticColor.BACKGROUND -> attr(com.google.android.material.R.attr.colorSurface)
                NuiSemanticColor.SECONDARY_BACKGROUND -> attr(com.google.android.material.R.attr.colorSurfaceContainer)
                NuiSemanticColor.GROUPED_BACKGROUND -> attr(com.google.android.material.R.attr.colorSurfaceContainerLow)
                NuiSemanticColor.SEPARATOR -> attr(com.google.android.material.R.attr.colorOutlineVariant)
                NuiSemanticColor.ACCENT -> accentColor()
                NuiSemanticColor.RED -> attr(androidx.appcompat.R.attr.colorError)
                NuiSemanticColor.ORANGE -> 0xFFF57C00.toInt()
                NuiSemanticColor.YELLOW -> 0xFFFBC02D.toInt()
                NuiSemanticColor.GREEN -> 0xFF388E3C.toInt()
                NuiSemanticColor.BLUE -> 0xFF1976D2.toInt()
                NuiSemanticColor.PURPLE -> 0xFF7B1FA2.toInt()
                NuiSemanticColor.PINK -> 0xFFC2185B.toInt()
                NuiSemanticColor.GRAY -> 0xFF757575.toInt()
                NuiSemanticColor.CLEAR -> Color.TRANSPARENT
                else -> fallback
            }
        }
        2 -> Color.argb(c and 0xff, (c ushr 24) and 0xff, (c ushr 16) and 0xff, (c ushr 8) and 0xff)
        else -> fallback
    }
}

/** Applies a node's text style, size, weight and (since a text appearance carries one) color. */
fun NuiRuntime.applyText(n: NuiNode) {
    (n.view as? NuiSymbol)?.let {
        it.textStyle = n.textStyle
        it.size = n.fontSize
        it.tint = n.color
        it.applyTint()
        return
    }
    (n.view as? NuiRichText)?.let {
        it.textStyle = n.textStyle
        it.size = n.fontSize
        it.weight = n.fontWeight
        it.family = n.fontFamily
        it.color = n.color
        it.invalidateText()
        return
    }
    val tv = n.view as? TextView ?: return
    if (tv is MaterialButton && n.fontSize == 0f && n.fontWeight == 0 && n.textStyle == 0 && n.fontFamily.isEmpty()) return
    applyTextStyle(tv, n.textStyle, n.fontSize, n.fontWeight, n.fontFamily)
    applyColor(n)
}

/**
 * A Material text appearance's values, resolved once for each theme, font
 * scale and density. Setting them is setting a view's fields, where
 * setTextAppearance reads the theme again for every view: a third of
 * making a thousand texts.
 */
private class NuiAppearance(
    val size: Float, // px
    val typeface: Typeface?,
    val letterSpacing: Float,
    val spacingExtra: Float,
    val spacingMult: Float,
    val colors: ColorStateList?,
    val hints: ColorStateList?,
    val features: String?,
)

private val appearances = HashMap<Int, NuiAppearance>()
private var appearancesFor = ""

private fun NuiRuntime.appearance(style: Int): NuiAppearance {
    val cfg = themed.resources.configuration
    val key = "$themeVersion|${cfg.fontScale}|${cfg.densityDpi}|${cfg.uiMode}"
    if (key != appearancesFor) {
        appearances.clear()
        appearancesFor = key
    }
    return appearances.getOrPut(style) {
        // Material's view, which reads lineHeight from an appearance too.
        val tv = com.google.android.material.textview.MaterialTextView(themed)
        TextViewCompat.setTextAppearance(tv, textAppearances[style] ?: com.google.android.material.R.style.TextAppearance_Material3_BodyLarge)
        NuiAppearance(tv.textSize, tv.typeface, tv.letterSpacing, tv.lineSpacingExtra, tv.lineSpacingMultiplier,
            tv.textColors, tv.hintTextColors, tv.fontFeatureSettings)
    }
}

/** Applies a text style, size, weight and font family (an app's font, or the system's). */
fun NuiRuntime.applyTextStyle(tv: TextView, style: Int, size: Float = 0f, weight: Int = 0, family: String = "") {
    if (tv !is MaterialButton) {
        val a = appearance(style)
        tv.setTextSize(TypedValue.COMPLEX_UNIT_PX, a.size)
        tv.typeface = a.typeface
        tv.letterSpacing = a.letterSpacing
        tv.setLineSpacing(a.spacingExtra, a.spacingMult)
        a.colors?.let { tv.setTextColor(it) }
        a.hints?.let { tv.setHintTextColor(it) }
        tv.fontFeatureSettings = a.features
    }
    if (size > 0) tv.setTextSize(TypedValue.COMPLEX_UNIT_SP, size)
    val w = weightOf(weight)
    val custom = if (family.isNotEmpty()) NuiFonts.typeface(app, family, w) else null
    if (custom != null) {
        tv.typeface = custom
    } else if (w > 0) {
        tv.typeface = if (Build.VERSION.SDK_INT >= 28) Typeface.create(tv.typeface, w, false)
        else Typeface.create(tv.typeface, if (w >= 600) Typeface.BOLD else Typeface.NORMAL)
    }
}

/**
 * Makes a field lines of text: return is a new line (no editor action,
 * so no Submit), and it grows with its text up to its line limit (none:
 * no limit), then scrolls.
 */
fun NuiRuntime.setMultiline(n: NuiNode, e: EditText, on: Boolean) {
    e.isSingleLine = !on
    if (!on) return
    e.setHorizontallyScrolling(false)
    e.minLines = 1
    val limit = (n.props[NuiProp.LINE_LIMIT] as? Long)?.toInt() ?: 0
    e.maxLines = if (limit > 0) limit else Int.MAX_VALUE
    e.isVerticalScrollBarEnabled = limit > 0
    e.gravity = android.view.Gravity.TOP or android.view.Gravity.START
    keepMultiline(n, e)
}

/** Keeps a multiline field's input type multiline when another prop sets it. */
fun keepMultiline(n: NuiNode, e: EditText) {
    if (n.props[NuiProp.TextField.MULTILINE] != true) return
    if (e.inputType and InputType.TYPE_TEXT_FLAG_MULTI_LINE == 0) e.inputType = e.inputType or InputType.TYPE_TEXT_FLAG_MULTI_LINE
    e.imeOptions = e.imeOptions or EditorInfo.IME_FLAG_NO_ENTER_ACTION
}

/** A button's icon, before its title (or alone), in the button's text color. */
fun NuiRuntime.setButtonIcon(n: NuiNode, b: MaterialButton, name: String) {
    // A start drawable counts only once the view has a layout direction,
    // which it inherits when attached: the core measures it before.
    b.layoutDirection = android.text.TextUtils.getLayoutDirectionFromLocale(java.util.Locale.getDefault())
    b.icon = if (name.isEmpty()) null else symbol(name)
    b.iconGravity = MaterialButton.ICON_GRAVITY_TEXT_START
    b.iconTint = b.textColors
    iconPadding(n, b)
}

/**
 * An icon alone has no room after it, and a label (the app's, or the
 * symbol's); one before a title has Material's room.
 */
fun NuiRuntime.iconPadding(n: NuiNode, b: MaterialButton) {
    if (b.icon == null) return
    val alone = b.text.isNullOrEmpty()
    b.iconPadding = if (alone) 0 else Math.round(8 * density)
    if (alone) {
        // An icon button: as wide as tall, not a text button's minimum.
        b.minWidth = 0
        b.minimumWidth = 0
        val pad = Math.round(10 * density)
        b.setPaddingRelative(pad, b.paddingTop, pad, b.paddingBottom)
    }
    if (n.props[NuiProp.A11Y_LABEL] == null) {
        b.contentDescription = if (alone) symbolLabels[n.props[NuiProp.Button.ICON] as? String ?: ""] else null
    }
}

/** A material's stand-in: the theme's surface, as opaque as the material is thick. */
fun NuiRuntime.materialColor(m: Int): Int {
    val surface = MaterialColors.getColor(themed, com.google.android.material.R.attr.colorSurfaceContainer, Color.WHITE)
    val alpha = when (m) {
        NuiMaterial.ULTRA_THIN -> 0.6f
        NuiMaterial.THIN -> 0.72f
        NuiMaterial.REGULAR -> 0.82f
        NuiMaterial.THICK -> 0.9f
        else -> 0.94f
    }
    return (Math.round(alpha * 255) shl 24) or (surface and 0xffffff)
}

/** Reapplies what came from the theme, after a configuration change. */
fun NuiRuntime.restyle(n: NuiNode) {
    applyText(n)
    applyColor(n)
    applyBackground(n)
}

fun NuiRuntime.applyColor(n: NuiNode) {
    when (val v = n.view) {
        is NuiSymbol -> { v.tint = n.color; v.applyTint() }
        is com.google.android.material.progressindicator.LinearProgressIndicator -> if (n.color != 0L) v.setIndicatorColor(color(n.color))
        is MaterialButton -> if (n.color != 0L) v.setTextColor(color(n.color))
        is TextView -> v.setTextColor(if (n.color != 0L) color(n.color) else MaterialColors.getColor(themed, com.google.android.material.R.attr.colorOnSurface, Color.BLACK))
        is MaterialSwitch -> if (n.color != 0L) v.trackTintList = ColorStateList(
            arrayOf(intArrayOf(android.R.attr.state_checked), intArrayOf()),
            intArrayOf(color(n.color), MaterialColors.getColor(themed, com.google.android.material.R.attr.colorSurfaceContainerHighest, Color.LTGRAY)))
        is Slider -> if (n.color != 0L) { v.trackActiveTintList = ColorStateList.valueOf(color(n.color)); v.thumbTintList = ColorStateList.valueOf(color(n.color)) }
        is CircularProgressIndicator -> if (n.color != 0L) v.setIndicatorColor(color(n.color))
        is android.widget.ImageView -> v.imageTintList = if (n.color != 0L) ColorStateList.valueOf(color(n.color)) else null
        is NuiRichText -> { v.color = n.color; v.invalidateText() }
    }
}

fun NuiRuntime.applyBackground(n: NuiNode) {
    if (n.background == 0L && n.cornerRadius == 0f && n.borderWidth == 0f && n.gradient == null && n.material == 0) return
    if (n.view is MaterialButton) {
        val b = n.view as MaterialButton
        if (n.background != 0L) b.backgroundTintList = ColorStateList.valueOf(color(n.background))
        if (n.cornerRadius > 0) b.cornerRadius = Math.round(n.cornerRadius * density)
        return
    }
    // A material, which Android can't blur behind: the surface, translucent.
    val toColor = if (n.background == 0L && n.material != 0) materialColor(n.material) else color(n.background)
    val toRadius = n.cornerRadius * density
    // Media (pictures, videos) clips to its corners, as on iOS.
    if (n.view is android.widget.ImageView || n.view is NuiVideo) n.view.clipToOutline = n.cornerRadius > 0
    val old = n.view.background as? GradientDrawable
    val g = n.gradient
    if (g != null) {
        // Over the background color: a GradientDrawable fills one or the other.
        n.backgroundAnimator?.cancel()
        val d = GradientDrawable(g.orientation(), g.colors)
        if (Build.VERSION.SDK_INT >= 29) d.setColors(g.colors, g.offsets)
        d.cornerRadius = toRadius
        if (n.borderWidth > 0) d.setStroke(Math.round(n.borderWidth * density), color(n.borderColor, Color.GRAY))
        n.view.background = d
        return
    }
    if (old != null && animates(n.view)) {
        if (n.borderWidth > 0) old.setStroke(Math.round(n.borderWidth * density), color(n.borderColor, Color.GRAY))
        animateBackground(n, old, old.color?.defaultColor ?: toColor, toColor, old.cornerRadius, toRadius)
        return
    }
    n.backgroundAnimator?.cancel()
    val d = GradientDrawable()
    d.setColor(toColor)
    d.cornerRadius = toRadius
    if (n.borderWidth > 0) d.setStroke(Math.round(n.borderWidth * density), color(n.borderColor, Color.GRAY))
    n.view.background = d
}

private fun NuiRuntime.node(id: Int) = nodes[id]

fun NuiRuntime.propBool(id: Int, prop: Int, v: Boolean) {
    val n = node(id) ?: return
    if (n.kind == NuiKind.COLOR_PICKER && setWheelProp(n, prop, v)) return
    n.props[prop] = v
    val view = n.view
    when (prop) {
        NuiProp.HIDDEN -> view.visibility = if (v) View.INVISIBLE else View.VISIBLE
        NuiProp.CLIP -> { view.clipToOutline = v; (view as? android.view.ViewGroup)?.clipChildren = v }
        NuiProp.DISABLED -> view.isEnabled = !v
        NuiProp.A11Y_ADJUSTABLE -> if (v) setAdjustable(n)
        NuiProp.A11Y_HIDDEN -> view.importantForAccessibility =
            if (v) View.IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS else View.IMPORTANT_FOR_ACCESSIBILITY_AUTO
        else -> if (!setNavProp(n, prop, v)) when (n.kind) {
            NuiKind.VIDEO -> (view as NuiVideo).let {
                when (prop) {
                    NuiProp.Video.HIDE_CONTROLS -> it.setControls(!v)
                    NuiProp.Video.AUTO_PLAY -> it.autoPlay = v
                    NuiProp.Video.LOOP -> it.loop = v
                    NuiProp.Video.MUTED -> it.player.volume = if (v) 0f else 1f
                }
            }
            NuiKind.TEXT_FIELD -> when (prop) {
                NuiProp.TextField.SECURE -> {
                    val e = view as EditText
                    e.inputType = if (v) InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD else InputType.TYPE_CLASS_TEXT
                    keepMultiline(n, e)
                }
                NuiProp.TextField.MULTILINE -> setMultiline(n, view as EditText, v)
            }
            NuiKind.PROGRESS -> if (prop == NuiProp.Progress.INDETERMINATE) {
                (view as com.google.android.material.progressindicator.LinearProgressIndicator).isIndeterminate = v
            }
            NuiKind.TOGGLE -> if (prop == NuiProp.Toggle.ON) {
                settingProp = true
                (view as MaterialSwitch).isChecked = v
                settingProp = false
            }
            // Material starts and stops its animation in show and hide.
            NuiKind.ACTIVITY -> if (prop == NuiProp.Activity.ANIMATING) (view as CircularProgressIndicator).let {
                if (v) { it.visibility = View.VISIBLE; it.show() } else it.hide()
            }
        }
    }
}

fun NuiRuntime.propInt(id: Int, prop: Int, v: Long) {
    val n = node(id) ?: return
    if ((n.kind == NuiKind.WHEEL || n.kind == NuiKind.STEPPER) && setWheelProp(n, prop, v)) return
    n.props[prop] = v
    if (setNavProp(n, prop, v)) return
    if (n.kind == NuiKind.SEGMENTED && prop == NuiProp.Segmented.SELECTED) (n.view as NuiSegmented).select(v.toInt())
    if (n.kind == NuiKind.DATE_PICKER) (n.view as NuiDatePicker).let {
        when (prop) {
            NuiProp.DatePicker.VALUE -> it.value = v
            NuiProp.DatePicker.MIN -> it.min = v
            NuiProp.DatePicker.MAX -> it.max = v
        }
    }
    if (prop == NuiProp.LINE_LIMIT) (n.view as? NuiRichText)?.let { it.lines = v.toInt(); it.invalidateText() }
    if (prop == NuiProp.LINE_LIMIT && n.props[NuiProp.TextField.MULTILINE] == true) {
        (n.view as? EditText)?.let { setMultiline(n, it, true) }
        return
    }
    if (prop == NuiProp.LINE_LIMIT) (n.view as? TextView)?.let {
        it.maxLines = if (v > 0) v.toInt() else Int.MAX_VALUE
        it.ellipsize = if (v > 0) android.text.TextUtils.TruncateAt.END else null
    }
}

fun NuiRuntime.propFloat(id: Int, prop: Int, v: Double) {
    val n = node(id) ?: return
    if ((n.kind == NuiKind.WHEEL || n.kind == NuiKind.STEPPER) && setWheelProp(n, prop, v)) return
    n.props[prop] = v
    if (n.kind == NuiKind.SHAPE && setShapeProp(n, prop, v)) return
    when (prop) {
        NuiProp.CORNER_RADIUS -> { n.cornerRadius = v.toFloat(); applyBackground(n) }
        NuiProp.BORDER_WIDTH -> { n.borderWidth = v.toFloat(); applyBackground(n) }
        NuiProp.OPACITY -> {
            n.base.alpha = v.toFloat()
            val a = animation
            if (a != null && animates(n.view)) animateProps(n.view, a, View.ALPHA to v.toFloat())
            else { stopRepeating(n.view, View.ALPHA); n.view.alpha = v.toFloat() }
        }
        NuiProp.FONT_SIZE -> { n.fontSize = v.toFloat(); applyText(n) }
        else -> if (n.kind == NuiKind.LIST && prop == NuiProp.List.GAP) {
            n.list?.let { it.setGrid(it.columns, v.toFloat()) }
        } else if (n.kind == NuiKind.PROGRESS && prop == NuiProp.Progress.VALUE) {
            setProgress(n.view as com.google.android.material.progressindicator.LinearProgressIndicator, v)
        } else if (n.kind == NuiKind.SLIDER) {
            val s = n.view as Slider
            val f = v.toFloat()
            when (prop) {
                NuiProp.Slider.MIN -> { s.valueFrom = f; if (s.value < f) s.value = f }
                NuiProp.Slider.MAX -> { if (s.value > f) s.value = f; s.valueTo = f }
                NuiProp.Slider.VALUE -> s.value = f.coerceIn(s.valueFrom, s.valueTo)
            }
        }
    }
}

fun NuiRuntime.propString(id: Int, prop: Int, v: String) {
    val n = node(id) ?: return
    if (prop == NuiProp.MATCH) {
        n.match?.let { if (matchViews[it] === n) matchViews.remove(it) }
        n.match = v.ifEmpty { null }
        n.match?.let { matchViews[it] = n }
        return
    }
    if (n.kind == NuiKind.COLOR_PICKER && setWheelProp(n, prop, v)) return
    n.props[prop] = v
    val view = n.view
    when (prop) {
        NuiProp.A11Y_LABEL -> view.contentDescription = v
        NuiProp.A11Y_HINT -> ViewCompat.setStateDescription(view, v)
        NuiProp.FONT_FAMILY -> {
            n.fontFamily = v
            if ("://" in v && NuiFonts.loaded == null) NuiFonts.loaded = { url ->
                for (m in nodes.values) if (m.fontFamily == url) { applyText(m); m.view.requestLayout() }
                send(NuiEvents.fontLoaded(rootID, url))
            }
            applyText(n)
        }
        else -> if (!setNavProp(n, prop, v)) when (n.kind) {
            NuiKind.TEXT -> if (prop == NuiProp.Text.TEXT) (view as TextView).text = v
            NuiKind.BUTTON -> when (prop) {
                NuiProp.Button.TITLE -> { (view as TextView).text = v; (view as? MaterialButton)?.let { iconPadding(n, it) } }
                NuiProp.Button.ICON -> setButtonIcon(n, view as MaterialButton, v)
            }
            NuiKind.VIDEO -> if (prop == NuiProp.Video.SOURCE) (view as NuiVideo).load(v)
            NuiKind.WEB_VIEW -> when (prop) {
                NuiProp.WebView.URL -> (view as NuiWebView).load(v)
                NuiProp.WebView.HTML -> if (v.isNotEmpty()) (view as NuiWebView).loadDataWithBaseURL(null, v, "text/html", "utf-8", null)
            }
            NuiKind.SYMBOL -> if (prop == NuiProp.Symbol.NAME) (view as NuiSymbol).apply { name = v; refresh() }
            NuiKind.TEXT_FIELD -> {
                val e = view as EditText
                when (prop) {
                    NuiProp.TextField.VALUE -> {
                        settingProp = true
                        e.setText(v)
                        e.setSelection(e.text?.length ?: 0)
                        settingProp = false
                    }
                    NuiProp.TextField.PLACEHOLDER -> e.hint = v
                    NuiProp.TextField.CONTENT_TYPE -> if (Build.VERSION.SDK_INT >= 26) e.setAutofillHints(v)
                }
            }
        }
    }
}

fun NuiRuntime.propColor(id: Int, prop: Int, v: Long) {
    val n = node(id) ?: return
    if (n.kind == NuiKind.COLOR_PICKER && setWheelProp(n, prop, v)) return
    n.props[prop] = v
    if (n.kind == NuiKind.SHAPE && setShapeProp(n, prop, v)) return
    when (prop) {
        NuiProp.BACKGROUND -> { n.background = v; applyBackground(n) }
        NuiProp.BORDER_COLOR -> { n.borderColor = v; applyBackground(n) }
        NuiProp.COLOR -> { n.color = v; applyColor(n) }
    }
}

fun NuiRuntime.propEnum(id: Int, prop: Int, v: Int) {
    val n = node(id) ?: return
    n.props[prop] = v
    if (n.kind == NuiKind.SHAPE && setShapeProp(n, prop, v)) return
    if (prop == NuiProp.MATERIAL) { n.material = v; applyBackground(n); return }
    if (n.kind == NuiKind.DATE_PICKER && prop == NuiProp.DatePicker.MODE) { (n.view as NuiDatePicker).mode = v; return }
    val view = n.view
    when (prop) {
        NuiProp.TEXT_STYLE -> { n.textStyle = v; applyText(n) }
        NuiProp.FONT_WEIGHT -> { n.fontWeight = v; applyText(n) }
        NuiProp.MENU_TRIGGER -> { n.menuTrigger = v; wirePresses(n) }
        NuiProp.TEXT_ALIGN -> if (view is NuiRichText) { view.align = v; view.invalidateText() } else (view as? TextView)?.gravity = when (v) {
            NuiTextAlign.CENTER -> Gravity.CENTER_HORIZONTAL
            NuiTextAlign.END -> Gravity.END
            else -> Gravity.START
        } or Gravity.CENTER_VERTICAL
        NuiProp.A11Y_ROLE -> if (view is NuiRichText) view.heading = v == NuiRole.HEADER
        else ViewCompat.setAccessibilityDelegate(view, object : AccessibilityDelegateCompat() {
            override fun onInitializeAccessibilityNodeInfo(host: View, info: AccessibilityNodeInfoCompat) {
                super.onInitializeAccessibilityNodeInfo(host, info)
                when (v) {
                    NuiRole.BUTTON, NuiRole.LINK -> info.className = "android.widget.Button"
                    NuiRole.HEADER -> info.isHeading = true
                    NuiRole.IMAGE -> info.className = "android.widget.ImageView"
                }
            }
        })
        else -> if (!setNavProp(n, prop, v)) when (n.kind) {
            NuiKind.VIDEO -> if (prop == NuiProp.Video.MODE) (view as NuiVideo).setFill(v == NuiImageMode.FILL)
            NuiKind.BUTTON -> if (prop == NuiProp.Button.VARIANT) replaceButton(n, v)
            NuiKind.TEXT_FIELD -> {
                val e = view as EditText
                when (prop) {
                    NuiProp.TextField.KEYBOARD -> e.inputType = when (v) {
                        NuiKeyboard.NUMBER -> InputType.TYPE_CLASS_NUMBER
                        NuiKeyboard.DECIMAL -> InputType.TYPE_CLASS_NUMBER or InputType.TYPE_NUMBER_FLAG_DECIMAL
                        NuiKeyboard.EMAIL -> InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS
                        NuiKeyboard.URL -> InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI
                        NuiKeyboard.PHONE -> InputType.TYPE_CLASS_PHONE
                        else -> InputType.TYPE_CLASS_TEXT
                    }
                    NuiProp.TextField.RETURN_KEY -> e.imeOptions = when (v) {
                        NuiReturnKey.GO -> EditorInfo.IME_ACTION_GO
                        NuiReturnKey.NEXT -> EditorInfo.IME_ACTION_NEXT
                        NuiReturnKey.SEARCH -> EditorInfo.IME_ACTION_SEARCH
                        NuiReturnKey.SEND -> EditorInfo.IME_ACTION_SEND
                        else -> EditorInfo.IME_ACTION_DONE
                    }
                    NuiProp.TextField.AUTOCAPITALIZE -> e.inputType = (e.inputType and (InputType.TYPE_TEXT_FLAG_CAP_CHARACTERS or
                        InputType.TYPE_TEXT_FLAG_CAP_WORDS or InputType.TYPE_TEXT_FLAG_CAP_SENTENCES).inv()) or when (v) {
                        NuiAutocapitalize.NONE -> 0
                        NuiAutocapitalize.WORDS -> InputType.TYPE_TEXT_FLAG_CAP_WORDS
                        NuiAutocapitalize.CHARACTERS -> InputType.TYPE_TEXT_FLAG_CAP_CHARACTERS
                        else -> InputType.TYPE_TEXT_FLAG_CAP_SENTENCES
                    }
                    NuiProp.TextField.AUTOCORRECT -> if (v == NuiAutocorrect.NO) e.inputType = e.inputType or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
                }
                keepMultiline(n, e)
            }
        }
    }
}

// Measuring

/** Measures a view's content in dp: known sizes (NaN: free) and the space available. */
fun NuiRuntime.measureNode(n: NuiNode, w: Float, h: Float, aw: Float, ah: Float): Pair<Float, Float> {
    val d = density
    val v = n.view
    fun spec(known: Float, avail: Float, minContent: Int): Int = when {
        !known.isNaN() -> View.MeasureSpec.makeMeasureSpec(Math.round(known * d), View.MeasureSpec.EXACTLY)
        avail == Float.POSITIVE_INFINITY -> View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED)
        avail == Float.NEGATIVE_INFINITY -> if (minContent >= 0) View.MeasureSpec.makeMeasureSpec(minContent, View.MeasureSpec.AT_MOST)
            else View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED)
        else -> View.MeasureSpec.makeMeasureSpec(Math.round(maxOf(avail, 0f) * d), View.MeasureSpec.AT_MOST)
    }
    if (v is NuiRichText) {
        val (mw, mh) = v.measureRich(w, aw)
        return mw to (if (!h.isNaN()) h else mh)
    }
    if (v is com.google.android.material.progressindicator.LinearProgressIndicator) {
        // As wide as it's let be; as tall as Material makes it.
        v.measure(View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED), View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
        val mw = if (!w.isNaN()) w else if (aw.isFinite()) maxOf(aw, 0f) else 100f
        return mw to (if (!h.isNaN()) h else v.measuredHeight / d)
    }
    if (v is NuiSymbol) {
        val s = v.sizePx() / d
        return (if (!w.isNaN()) w else s) to (if (!h.isNaN()) h else s)
    }
    var minW = -1
    when {
        v is EditText -> minW = Math.round(80 * d) // it scrolls its text: it can be narrow
        v is TextView && v !is MaterialButton -> {
            // Min-content: the widest word.
            val p = v.paint
            var widest = 0f
            for (word in v.text.split(' ', '\n', '\t')) widest = maxOf(widest, p.measureText(word))
            minW = ceil(widest).toInt() + v.totalPaddingLeft + v.totalPaddingRight
        }
    }
    var ws = spec(w, aw, minW)
    if (v is Slider && w.isNaN()) {
        ws = View.MeasureSpec.makeMeasureSpec(Math.round((if (aw.isFinite()) maxOf(aw, 0f) else 200f) * d), View.MeasureSpec.EXACTLY)
    }
    v.measure(ws, spec(h, ah, -1))
    var mw = v.measuredWidth / d
    var mh = v.measuredHeight / d
    if (v is EditText && aw == Float.NEGATIVE_INFINITY && w.isNaN()) mw = 80f
    if (!w.isNaN()) mw = w
    if (!h.isNaN()) mh = h
    return mw to mh
}

/** A screen reader's actions on the view (TalkBack's actions menu): u32 count, then each name. */
fun NuiRuntime.setA11yActions(n: NuiNode, b: java.nio.ByteBuffer) {
    val r = b.duplicate().order(java.nio.ByteOrder.LITTLE_ENDIAN)
    n.a11yActions.forEach { ViewCompat.removeAccessibilityAction(n.view, it) }
    n.a11yActions.clear()
    if (r.remaining() < 4) return
    val id = n.id
    // One item to TalkBack, which reads its texts, with the actions.
    ViewCompat.setScreenReaderFocusable(n.view, true)
    for (i in 0 until r.int) {
        val name = ByteArray(r.int).also { r.get(it) }.toString(Charsets.UTF_8)
        n.a11yActions.add(ViewCompat.addAccessibilityAction(n.view, name) { _, _ ->
            emit(id, NuiEventKind.A11Y_ACTION) { NuiEvents.a11yAction(id, i.toLong()) }
            true
        })
    }
}

/** OnAdjust: a seek bar to TalkBack, whose swipe up and down (and the volume keys) step it. */
fun NuiRuntime.setAdjustable(n: NuiNode) {
    val id = n.id
    ViewCompat.setAccessibilityDelegate(n.view, object : AccessibilityDelegateCompat() {
        override fun onInitializeAccessibilityNodeInfo(host: View, info: AccessibilityNodeInfoCompat) {
            super.onInitializeAccessibilityNodeInfo(host, info)
            info.className = "android.widget.SeekBar"
            info.addAction(AccessibilityNodeInfoCompat.AccessibilityActionCompat.ACTION_SCROLL_FORWARD)
            info.addAction(AccessibilityNodeInfoCompat.AccessibilityActionCompat.ACTION_SCROLL_BACKWARD)
        }

        override fun performAccessibilityAction(host: View, action: Int, args: android.os.Bundle?): Boolean {
            val by = when (action) {
                android.view.accessibility.AccessibilityNodeInfo.ACTION_SCROLL_FORWARD -> 1L
                android.view.accessibility.AccessibilityNodeInfo.ACTION_SCROLL_BACKWARD -> -1L
                else -> return super.performAccessibilityAction(host, action, args)
            }
            emit(id, NuiEventKind.ADJUST) { NuiEvents.adjust(id, by) }
            return true
        }
    })
    n.view.isFocusable = true
}
