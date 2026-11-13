package dev.asuka

import android.content.res.ColorStateList
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
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
    AsukaTextStyle.LARGE_TITLE to com.google.android.material.R.style.TextAppearance_Material3_DisplaySmall,
    AsukaTextStyle.TITLE to com.google.android.material.R.style.TextAppearance_Material3_HeadlineMedium,
    AsukaTextStyle.TITLE2 to com.google.android.material.R.style.TextAppearance_Material3_HeadlineSmall,
    AsukaTextStyle.TITLE3 to com.google.android.material.R.style.TextAppearance_Material3_TitleLarge,
    AsukaTextStyle.HEADLINE to com.google.android.material.R.style.TextAppearance_Material3_TitleMedium,
    AsukaTextStyle.SUBHEADLINE to com.google.android.material.R.style.TextAppearance_Material3_TitleSmall,
    AsukaTextStyle.BODY to com.google.android.material.R.style.TextAppearance_Material3_BodyLarge,
    AsukaTextStyle.CALLOUT to com.google.android.material.R.style.TextAppearance_Material3_BodyMedium,
    AsukaTextStyle.FOOTNOTE to com.google.android.material.R.style.TextAppearance_Material3_BodySmall,
    AsukaTextStyle.CAPTION to com.google.android.material.R.style.TextAppearance_Material3_LabelMedium,
    AsukaTextStyle.CAPTION2 to com.google.android.material.R.style.TextAppearance_Material3_LabelSmall,
)

/** A text style's Material text appearance. */
fun textAppearance(style: Int): Int? = textAppearances[style]

fun weightOf(w: Int): Int = when (w) {
    AsukaFontWeight.ULTRALIGHT -> 100
    AsukaFontWeight.THIN -> 200
    AsukaFontWeight.LIGHT -> 300
    AsukaFontWeight.REGULAR -> 400
    AsukaFontWeight.MEDIUM -> 500
    AsukaFontWeight.SEMIBOLD -> 600
    AsukaFontWeight.BOLD -> 700
    AsukaFontWeight.HEAVY -> 800
    AsukaFontWeight.BLACK -> 900
    else -> 0
}

/** Resolves a core color: a semantic one from the Material 3 theme, or RGBA. */
fun AsukaRuntime.color(v: Long, fallback: Int = Color.TRANSPARENT): Int {
    val kind = (v ushr 32).toInt()
    val c = (v and 0xffffffffL).toInt()
    return when (kind) {
        1 -> {
            fun attr(a: Int) = MaterialColors.getColor(themed, a, fallback)
            // Its opacity (Color.Opacity) is in bits 24-31, as transparency.
            val alpha = 255 - ((c ushr 24) and 0xff)
            val resolved = when (c and 0xffff) {
                AsukaSemanticColor.LABEL -> attr(com.google.android.material.R.attr.colorOnSurface)
                AsukaSemanticColor.SECONDARY_LABEL -> attr(com.google.android.material.R.attr.colorOnSurfaceVariant)
                AsukaSemanticColor.TERTIARY_LABEL -> attr(com.google.android.material.R.attr.colorOutline)
                AsukaSemanticColor.BACKGROUND -> attr(com.google.android.material.R.attr.colorSurface)
                AsukaSemanticColor.SECONDARY_BACKGROUND -> attr(com.google.android.material.R.attr.colorSurfaceContainer)
                AsukaSemanticColor.GROUPED_BACKGROUND -> attr(com.google.android.material.R.attr.colorSurfaceContainerLow)
                AsukaSemanticColor.SEPARATOR -> attr(com.google.android.material.R.attr.colorOutlineVariant)
                AsukaSemanticColor.ACCENT -> accentColor()
                AsukaSemanticColor.ON_ACCENT -> attr(com.google.android.material.R.attr.colorOnPrimary)
                AsukaSemanticColor.RED -> attr(androidx.appcompat.R.attr.colorError)
                AsukaSemanticColor.ORANGE -> 0xFFF57C00.toInt()
                AsukaSemanticColor.YELLOW -> 0xFFFBC02D.toInt()
                AsukaSemanticColor.GREEN -> 0xFF388E3C.toInt()
                AsukaSemanticColor.BLUE -> 0xFF1976D2.toInt()
                AsukaSemanticColor.PURPLE -> 0xFF7B1FA2.toInt()
                AsukaSemanticColor.PINK -> 0xFFC2185B.toInt()
                AsukaSemanticColor.GRAY -> 0xFF757575.toInt()
                AsukaSemanticColor.CLEAR -> Color.TRANSPARENT
                AsukaSemanticColor.SECONDARY_GROUPED_BACKGROUND -> attr(com.google.android.material.R.attr.colorSurfaceContainer)
                else -> fallback
            }
            if (alpha == 255) resolved
            else androidx.core.graphics.ColorUtils.setAlphaComponent(resolved, Color.alpha(resolved) * alpha / 255)
        }
        2 -> Color.argb(c and 0xff, (c ushr 24) and 0xff, (c ushr 16) and 0xff, (c ushr 8) and 0xff)
        else -> fallback
    }
}

/** Applies a node's text style, size, weight and (since a text appearance carries one) color. */
fun AsukaRuntime.applyText(n: AsukaNode) {
    (n.view as? AsukaSymbol)?.let {
        it.textStyle = n.textStyle
        it.size = n.fontSize
        it.tint = n.color
        it.applyTint()
        return
    }
    (n.view as? AsukaRichText)?.let {
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
private class AsukaAppearance(
    val size: Float, // px
    val typeface: Typeface?,
    val letterSpacing: Float,
    val spacingExtra: Float,
    val spacingMult: Float,
    val colors: ColorStateList?,
    val hints: ColorStateList?,
    val features: String?,
)

private val appearances = HashMap<Int, AsukaAppearance>()
private var appearancesFor = ""

private fun AsukaRuntime.appearance(style: Int): AsukaAppearance {
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
        AsukaAppearance(tv.textSize, tv.typeface, tv.letterSpacing, tv.lineSpacingExtra, tv.lineSpacingMultiplier,
            tv.textColors, tv.hintTextColors, tv.fontFeatureSettings)
    }
}

/** Applies a text style, size, weight and font family (an app's font, or the system's). */
fun AsukaRuntime.applyTextStyle(tv: TextView, style: Int, size: Float = 0f, weight: Int = 0, family: String = "") {
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
    val custom = if (family.isNotEmpty()) AsukaFonts.typeface(app, family, w) else null
    if (custom != null) {
        tv.typeface = custom
    } else if (w > 0) {
        tv.typeface = Typeface.create(tv.typeface, w, false)
    }
}

/**
 * Makes a field lines of text: return is a new line (no editor action,
 * so no Submit), and it grows with its text up to its line limit (none:
 * no limit), then scrolls.
 */
fun AsukaRuntime.setMultiline(n: AsukaNode, e: EditText, on: Boolean) {
    e.isSingleLine = !on
    if (!on) return
    e.setHorizontallyScrolling(false)
    e.minLines = 1
    val limit = (n.props[AsukaProp.LINE_LIMIT] as? Long)?.toInt() ?: 0
    e.maxLines = if (limit > 0) limit else Int.MAX_VALUE
    e.isVerticalScrollBarEnabled = limit > 0
    e.gravity = android.view.Gravity.TOP or android.view.Gravity.START
    keepMultiline(n, e)
}

/** Keeps a multiline field's input type multiline when another prop sets it. */
fun keepMultiline(n: AsukaNode, e: EditText) {
    if (n.props[AsukaProp.TextField.MULTILINE] != true) return
    if (e.inputType and InputType.TYPE_TEXT_FLAG_MULTI_LINE == 0) e.inputType = e.inputType or InputType.TYPE_TEXT_FLAG_MULTI_LINE
    e.imeOptions = e.imeOptions or EditorInfo.IME_FLAG_NO_ENTER_ACTION
}

/** A button's icon, before its title (or alone), in the button's text color. */
fun AsukaRuntime.setButtonIcon(n: AsukaNode, b: MaterialButton, name: String) {
    // A start drawable counts only once the view has a layout direction,
    // which it inherits when attached: the core measures it before.
    b.layoutDirection = directionOf(n)
    b.icon = if (name.isEmpty()) null else symbol(name)
    b.iconGravity = MaterialButton.ICON_GRAVITY_TEXT_START
    b.iconTint = b.textColors
    iconPadding(n, b)
}

/**
 * An icon alone has no room after it, and a label (the app's, or the
 * symbol's); one before a title has Material's room.
 */
fun AsukaRuntime.iconPadding(n: AsukaNode, b: MaterialButton) {
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
    if (n.props[AsukaProp.A11Y_LABEL] == null) {
        b.contentDescription = if (alone) symbolLabels[n.props[AsukaProp.Button.ICON] as? String ?: ""] else null
    }
}

/** A material's stand-in: the theme's surface, as opaque as the material is thick. */
fun AsukaRuntime.materialColor(m: Int): Int {
    val surface = MaterialColors.getColor(themed, com.google.android.material.R.attr.colorSurfaceContainer, Color.WHITE)
    val alpha = when (m) {
        AsukaMaterial.ULTRA_THIN -> 0.6f
        AsukaMaterial.THIN -> 0.72f
        AsukaMaterial.REGULAR -> 0.82f
        AsukaMaterial.THICK -> 0.9f
        else -> 0.94f
    }
    return (Math.round(alpha * 255) shl 24) or (surface and 0xffffff)
}

/** Reapplies what came from the theme, after a configuration change. */
fun AsukaRuntime.restyle(n: AsukaNode) {
    applyText(n)
    applyColor(n)
    applyBackground(n)
}

fun AsukaRuntime.applyColor(n: AsukaNode) {
    when (val v = n.view) {
        is AsukaSymbol -> { v.tint = n.color; v.applyTint() }
        is com.google.android.material.progressindicator.LinearProgressIndicator -> if (n.color != 0L) v.setIndicatorColor(color(n.color))
        is MaterialButton -> if (n.color != 0L) v.setTextColor(color(n.color))
        is com.google.android.material.checkbox.MaterialCheckBox -> if (n.color != 0L) v.buttonTintList = ColorStateList(
            arrayOf(intArrayOf(android.R.attr.state_checked), intArrayOf()),
            intArrayOf(color(n.color), MaterialColors.getColor(themed, com.google.android.material.R.attr.colorOnSurfaceVariant, Color.GRAY)))
        is TextView -> v.setTextColor(if (n.color != 0L) color(n.color) else MaterialColors.getColor(themed, com.google.android.material.R.attr.colorOnSurface, Color.BLACK))
        is MaterialSwitch -> if (n.color != 0L) v.trackTintList = ColorStateList(
            arrayOf(intArrayOf(android.R.attr.state_checked), intArrayOf()),
            intArrayOf(color(n.color), MaterialColors.getColor(themed, com.google.android.material.R.attr.colorSurfaceContainerHighest, Color.LTGRAY)))
        is Slider -> if (n.color != 0L) { v.trackActiveTintList = ColorStateList.valueOf(color(n.color)); v.thumbTintList = ColorStateList.valueOf(color(n.color)) }
        is CircularProgressIndicator -> if (n.color != 0L) v.setIndicatorColor(color(n.color))
        is android.widget.ImageView -> v.imageTintList = if (n.color != 0L) ColorStateList.valueOf(color(n.color)) else null
        is AsukaRichText -> { v.color = n.color; v.invalidateText() }
    }
}

fun AsukaRuntime.applyBackground(n: AsukaNode) {
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
    if (n.view is android.widget.ImageView || n.view is AsukaVideo) n.view.clipToOutline = n.cornerRadius > 0
    val old = n.view.background as? GradientDrawable
    val g = n.gradient
    if (g != null) {
        // Over the background color: a GradientDrawable fills one or the other.
        n.backgroundAnimator?.cancel()
        val d = GradientDrawable(g.orientation(), g.colors)
        d.setColors(g.colors, g.offsets)
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

private fun AsukaRuntime.node(id: Int) = nodes[id]

fun AsukaRuntime.propBool(id: Int, prop: Int, v: Boolean) {
    val n = node(id) ?: return
    if (n.kind == AsukaKind.WEB_VIEW && prop == AsukaProp.WebView.REQUESTS) { (n.view as AsukaWebView).reportRequests = v; return }
    if (n.kind == AsukaKind.COLOR_PICKER && setWheelProp(n, prop, v)) return
    n.props[prop] = v
    val view = n.view
    when (prop) {
        AsukaProp.HIDDEN -> view.visibility = if (v) View.INVISIBLE else View.VISIBLE
        AsukaProp.CLIP -> { view.clipToOutline = v; (view as? android.view.ViewGroup)?.clipChildren = v }
        AsukaProp.DISABLED -> view.isEnabled = !v
        AsukaProp.SELECTED -> (view as? AsukaCell)?.row?.setChosen(v)
        AsukaProp.A11Y_ADJUSTABLE -> if (v) setAdjustable(n)
        AsukaProp.INTERCEPT_BACK -> {
            if (n.kind == AsukaKind.SHEET) sheetIntercepts(n)
            updateBack()
        }
        AsukaProp.A11Y_HIDDEN -> view.importantForAccessibility =
            if (v) View.IMPORTANT_FOR_ACCESSIBILITY_NO_HIDE_DESCENDANTS else View.IMPORTANT_FOR_ACCESSIBILITY_AUTO
        else -> if (!setNavProp(n, prop, v)) when (n.kind) {
            AsukaKind.VIDEO -> (view as AsukaVideo).let {
                when (prop) {
                    AsukaProp.Video.HIDE_CONTROLS -> it.setControls(!v)
                    AsukaProp.Video.AUTO_PLAY -> it.autoPlay = v
                    AsukaProp.Video.LOOP -> it.loop = v
                    AsukaProp.Video.MUTED -> it.player.volume = if (v) 0f else 1f
                }
            }
            AsukaKind.TEXT_FIELD -> when (prop) {
                AsukaProp.TextField.SECURE -> {
                    val e = view as EditText
                    val typeface = e.typeface
                    e.inputType = if (v) InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD else InputType.TYPE_CLASS_TEXT
                    e.typeface = typeface // a password's input type turns it monospaced, placeholder and all
                    keepMultiline(n, e)
                }
                AsukaProp.TextField.MULTILINE -> setMultiline(n, view as EditText, v)
                // No box nor underline: the text alone (set once, before it's shown).
                AsukaProp.TextField.PLAIN -> if (v) (view as EditText).apply {
                    background = null
                    setPadding(0, 0, 0, 0)
                }
            }
            AsukaKind.PROGRESS -> if (prop == AsukaProp.Progress.INDETERMINATE) {
                (view as com.google.android.material.progressindicator.LinearProgressIndicator).isIndeterminate = v
            }
            AsukaKind.TOGGLE -> when (prop) {
                AsukaProp.Toggle.ON -> {
                    settingProp = true
                    (view as android.widget.CompoundButton).isChecked = v
                    settingProp = false
                }
                AsukaProp.Toggle.CHECKBOX -> if (v) makeCheckbox(n)
            }
            // Material starts and stops its animation in show and hide.
            AsukaKind.ACTIVITY -> if (prop == AsukaProp.Activity.ANIMATING) (view as CircularProgressIndicator).let {
                if (v) { it.visibility = View.VISIBLE; it.show() } else it.hide()
            }
        }
    }
}

fun AsukaRuntime.propInt(id: Int, prop: Int, v: Long) {
    val n = node(id) ?: return
    if ((n.kind == AsukaKind.WHEEL || n.kind == AsukaKind.STEPPER) && setWheelProp(n, prop, v)) return
    n.props[prop] = v
    if (setNavProp(n, prop, v)) return
    if (n.kind == AsukaKind.SEGMENTED && prop == AsukaProp.Segmented.SELECTED) (n.view as AsukaSegmented).select(v.toInt())
    if (n.kind == AsukaKind.DATE_PICKER) (n.view as AsukaDatePicker).let {
        when (prop) {
            AsukaProp.DatePicker.VALUE -> it.value = v
            AsukaProp.DatePicker.MIN -> it.min = v
            AsukaProp.DatePicker.MAX -> it.max = v
        }
    }
    if (prop == AsukaProp.LINE_LIMIT) (n.view as? AsukaRichText)?.let { it.lines = v.toInt(); it.invalidateText() }
    if (prop == AsukaProp.LINE_LIMIT && n.props[AsukaProp.TextField.MULTILINE] == true) {
        (n.view as? EditText)?.let { setMultiline(n, it, true) }
        return
    }
    if (prop == AsukaProp.LINE_LIMIT) (n.view as? TextView)?.let {
        it.maxLines = if (v > 0) v.toInt() else Int.MAX_VALUE
        it.ellipsize = if (v > 0) android.text.TextUtils.TruncateAt.END else null
    }
}

fun AsukaRuntime.propFloat(id: Int, prop: Int, v: Double) {
    val n = node(id) ?: return
    if ((n.kind == AsukaKind.WHEEL || n.kind == AsukaKind.STEPPER) && setWheelProp(n, prop, v)) return
    n.props[prop] = v
    if (n.kind == AsukaKind.SHAPE && setShapeProp(n, prop, v)) return
    when (prop) {
        AsukaProp.CORNER_RADIUS -> { n.cornerRadius = v.toFloat(); applyBackground(n) }
        AsukaProp.BORDER_WIDTH -> { n.borderWidth = v.toFloat(); applyBackground(n) }
        AsukaProp.OPACITY -> {
            n.base.alpha = v.toFloat()
            val a = animation
            if (a != null && animates(n.view)) animateProps(n.view, a, View.ALPHA to v.toFloat())
            else { stopRepeating(n.view, View.ALPHA); n.view.alpha = v.toFloat() }
        }
        AsukaProp.FONT_SIZE -> { n.fontSize = v.toFloat(); applyText(n) }
        else -> if (setNavProp(n, prop, v)) {
            // A popover's or a keyboard bar's size.
        } else if (n.kind == AsukaKind.LIST && prop == AsukaProp.List.GAP) {
            n.list?.let { it.setGrid(it.columns, v.toFloat()) }
        } else if (n.kind == AsukaKind.LIST && prop == AsukaProp.List.INSET) {
            n.list?.let { it.setAppearance(it.appearance, v.toFloat()) }
        } else if (n.kind == AsukaKind.PROGRESS && prop == AsukaProp.Progress.VALUE) {
            setProgress(n.view as com.google.android.material.progressindicator.LinearProgressIndicator, v)
        } else if (n.kind == AsukaKind.SLIDER) {
            val s = n.view as Slider
            val f = v.toFloat()
            when (prop) {
                AsukaProp.Slider.MIN -> { s.valueFrom = f; if (s.value < f) s.value = f }
                AsukaProp.Slider.MAX -> { if (s.value > f) s.value = f; s.valueTo = f }
                AsukaProp.Slider.VALUE -> s.value = f.coerceIn(s.valueFrom, s.valueTo)
            }
        }
    }
}

fun AsukaRuntime.propString(id: Int, prop: Int, v: String) {
    val n = node(id) ?: return
    if (prop == AsukaProp.MATCH) {
        n.match?.let { if (matchViews[it] === n) matchViews.remove(it) }
        n.match = v.ifEmpty { null }
        n.match?.let { matchViews[it] = n }
        return
    }
    if (n.kind == AsukaKind.COLOR_PICKER && setWheelProp(n, prop, v)) return
    n.props[prop] = v
    val view = n.view
    when (prop) {
        AsukaProp.A11Y_LABEL -> view.contentDescription = v
        // A mouse's hover, or a long press when the view has none of its own.
        AsukaProp.TOOLTIP -> view.tooltipText = v.ifEmpty { null }
        AsukaProp.A11Y_HINT -> ViewCompat.setStateDescription(view, v)
        AsukaProp.FONT_FAMILY -> {
            n.fontFamily = v
            if ("://" in v && AsukaFonts.loaded == null) AsukaFonts.loaded = { url ->
                for (m in nodes.values) if (m.fontFamily == url) { applyText(m); m.view.requestLayout() }
                send(AsukaEvents.fontLoaded(rootID, url))
            }
            applyText(n)
        }
        else -> if (!setNavProp(n, prop, v)) when (n.kind) {
            AsukaKind.TEXT -> if (prop == AsukaProp.Text.TEXT) (view as TextView).text = v
            AsukaKind.BUTTON -> when (prop) {
                AsukaProp.Button.TITLE -> { (view as TextView).text = v; (view as? MaterialButton)?.let { iconPadding(n, it) } }
                AsukaProp.Button.ICON -> setButtonIcon(n, view as MaterialButton, v)
            }
            AsukaKind.VIDEO -> if (prop == AsukaProp.Video.SOURCE) (view as AsukaVideo).load(v)
            AsukaKind.WEB_VIEW -> when (prop) {
                AsukaProp.WebView.URL -> (view as AsukaWebView).load(v)
                AsukaProp.WebView.HTML -> if (v.isNotEmpty()) (view as AsukaWebView).loadDataWithBaseURL(null, v, "text/html", "utf-8", null)
                AsukaProp.WebView.PRELOAD -> (view as AsukaWebView).setPreload(v)
                AsukaProp.WebView.USER_AGENT -> (view as AsukaWebView).setUserAgent(v)
            }
            AsukaKind.SYMBOL -> if (prop == AsukaProp.Symbol.NAME) (view as AsukaSymbol).apply { name = v; refresh() }
            AsukaKind.TEXT_FIELD -> {
                val e = view as EditText
                when (prop) {
                    AsukaProp.TextField.VALUE -> {
                        settingProp = true
                        e.setText(v)
                        e.setSelection(e.text?.length ?: 0)
                        settingProp = false
                    }
                    AsukaProp.TextField.PLACEHOLDER -> e.hint = v
                    AsukaProp.TextField.CONTENT_TYPE -> e.setAutofillHints(autofillHint(v))
                }
            }
        }
    }
}

fun AsukaRuntime.propColor(id: Int, prop: Int, v: Long) {
    val n = node(id) ?: return
    if (n.kind == AsukaKind.COLOR_PICKER && setWheelProp(n, prop, v)) return
    n.props[prop] = v
    if (n.kind == AsukaKind.SHAPE && setShapeProp(n, prop, v)) return
    if (n.kind == AsukaKind.SCREEN && prop == AsukaProp.Screen.BAR_COLOR) {
        (n.view as AsukaScreen).barColor = v
        (n.parent?.view as? AsukaStack)?.let { if (it.top === n) it.refreshBar() }
        return
    }
    when (prop) {
        AsukaProp.BACKGROUND -> { n.background = v; applyBackground(n) }
        AsukaProp.BORDER_COLOR -> { n.borderColor = v; applyBackground(n) }
        AsukaProp.COLOR -> { n.color = v; applyColor(n) }
    }
}

fun AsukaRuntime.propEnum(id: Int, prop: Int, v: Int) {
    val n = node(id) ?: return
    n.props[prop] = v
    if (n.kind == AsukaKind.SHAPE && setShapeProp(n, prop, v)) return
    if (prop == AsukaProp.MATERIAL) { n.material = v; applyBackground(n); return }
    if (n.kind == AsukaKind.LIST && prop == AsukaProp.List.APPEARANCE) { n.list?.let { it.setAppearance(v, it.inset) }; return }
    if (n.kind == AsukaKind.LIST && prop == AsukaProp.List.ANCHOR) { n.list?.setAnchor(v); return }
    if (n.kind == AsukaKind.DATE_PICKER && prop == AsukaProp.DatePicker.MODE) { (n.view as AsukaDatePicker).mode = v; return }
    val view = n.view
    when (prop) {
        AsukaProp.TEXT_STYLE -> { n.textStyle = v; applyText(n) }
        AsukaProp.FONT_WEIGHT -> { n.fontWeight = v; applyText(n) }
        AsukaProp.MENU_TRIGGER -> { n.menuTrigger = v; wirePresses(n) }
        AsukaProp.LAYOUT_DIRECTION -> view.layoutDirection = directionOf(n)
        AsukaProp.TEXT_ALIGN -> if (view is AsukaRichText) { view.align = v; view.invalidateText() } else (view as? TextView)?.let {
            it.gravity = when (v) {
                AsukaTextAlign.CENTER -> Gravity.CENTER_HORIZONTAL
                AsukaTextAlign.END -> Gravity.END
                else -> Gravity.START
            } or Gravity.CENTER_VERTICAL
            // The side the view starts on, whichever way its text runs: a
            // Latin string in a right to left app is on the right. A button
            // places its icon by its gravity.
            if (it !is MaterialButton) it.textAlignment = when (v) {
                AsukaTextAlign.CENTER -> View.TEXT_ALIGNMENT_CENTER
                AsukaTextAlign.END -> View.TEXT_ALIGNMENT_VIEW_END
                else -> View.TEXT_ALIGNMENT_VIEW_START
            }
        }
        AsukaProp.A11Y_ROLE -> if (view is AsukaRichText) view.heading = v == AsukaRole.HEADER
        else ViewCompat.setAccessibilityDelegate(view, object : AccessibilityDelegateCompat() {
            override fun onInitializeAccessibilityNodeInfo(host: View, info: AccessibilityNodeInfoCompat) {
                super.onInitializeAccessibilityNodeInfo(host, info)
                when (v) {
                    AsukaRole.BUTTON, AsukaRole.LINK -> info.className = "android.widget.Button"
                    AsukaRole.HEADER -> info.isHeading = true
                    AsukaRole.IMAGE -> info.className = "android.widget.ImageView"
                }
            }
        })
        else -> if (!setNavProp(n, prop, v)) when (n.kind) {
            AsukaKind.VIDEO -> if (prop == AsukaProp.Video.MODE) (view as AsukaVideo).setFill(v == AsukaImageMode.FILL)
            AsukaKind.BUTTON -> if (prop == AsukaProp.Button.VARIANT) replaceButton(n, v)
            AsukaKind.TEXT_FIELD -> {
                val e = view as EditText
                when (prop) {
                    AsukaProp.TextField.KEYBOARD -> e.inputType = when (v) {
                        AsukaKeyboard.NUMBER -> InputType.TYPE_CLASS_NUMBER
                        AsukaKeyboard.DECIMAL -> InputType.TYPE_CLASS_NUMBER or InputType.TYPE_NUMBER_FLAG_DECIMAL
                        AsukaKeyboard.EMAIL -> InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_EMAIL_ADDRESS
                        AsukaKeyboard.URL -> InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_URI
                        AsukaKeyboard.PHONE -> InputType.TYPE_CLASS_PHONE
                        else -> InputType.TYPE_CLASS_TEXT
                    }
                    AsukaProp.TextField.RETURN_KEY -> e.imeOptions = when (v) {
                        AsukaReturnKey.GO -> EditorInfo.IME_ACTION_GO
                        AsukaReturnKey.NEXT -> EditorInfo.IME_ACTION_NEXT
                        AsukaReturnKey.SEARCH -> EditorInfo.IME_ACTION_SEARCH
                        AsukaReturnKey.SEND -> EditorInfo.IME_ACTION_SEND
                        else -> EditorInfo.IME_ACTION_DONE
                    }
                    AsukaProp.TextField.AUTOCAPITALIZE -> e.inputType = (e.inputType and (InputType.TYPE_TEXT_FLAG_CAP_CHARACTERS or
                        InputType.TYPE_TEXT_FLAG_CAP_WORDS or InputType.TYPE_TEXT_FLAG_CAP_SENTENCES).inv()) or when (v) {
                        AsukaAutocapitalize.NONE -> 0
                        AsukaAutocapitalize.WORDS -> InputType.TYPE_TEXT_FLAG_CAP_WORDS
                        AsukaAutocapitalize.CHARACTERS -> InputType.TYPE_TEXT_FLAG_CAP_CHARACTERS
                        else -> InputType.TYPE_TEXT_FLAG_CAP_SENTENCES
                    }
                    AsukaProp.TextField.AUTOCORRECT -> if (v == AsukaAutocorrect.NO) e.inputType = e.inputType or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
                }
                keepMultiline(n, e)
            }
        }
    }
}

// Padding

/**
 * ContentInset: a text, a field or an image draws its content inside its
 * padding and border (the view's padding, on top of its own: a field's
 * background's). Measurements are of the content alone (measureNode).
 * Centered content (a button's title, a symbol) needs none.
 */
fun AsukaRuntime.applyContentInset(n: AsukaNode) {
    val ci = n.contentInset ?: return
    val d = density
    val t = Math.round(ci[0] * d)
    val r = Math.round(ci[1] * d)
    val b = Math.round(ci[2] * d)
    val l = Math.round(ci[3] * d)
    when (val v = n.view) {
        is AsukaRichText -> v.setInset(l, t, r)
        is com.google.android.material.button.MaterialButton -> {}
        is TextView, is android.widget.ImageView -> {
            val base = n.basePadding ?: intArrayOf(v.paddingLeft, v.paddingTop, v.paddingRight, v.paddingBottom).also { n.basePadding = it }
            v.setPadding(base[0] + l, base[1] + t, base[2] + r, base[3] + b)
        }
    }
}

/** The padding ContentInset added to a view, in px (horizontal, vertical): what its measurement leaves out. */
private fun AsukaRuntime.insetPx(n: AsukaNode): Pair<Int, Int> {
    val ci = n.contentInset ?: return 0 to 0
    if (n.basePadding == null) return 0 to 0
    val d = density
    return (Math.round(ci[1] * d) + Math.round(ci[3] * d)) to (Math.round(ci[0] * d) + Math.round(ci[2] * d))
}

// Measuring

/** Measures a view's content in dp: known sizes (NaN: free) and the space available. */
fun AsukaRuntime.measureNode(n: AsukaNode, w: Float, h: Float, aw: Float, ah: Float): Pair<Float, Float> {
    val d = density
    val v = n.view
    fun spec(known: Float, avail: Float, minContent: Int): Int = when {
        !known.isNaN() -> View.MeasureSpec.makeMeasureSpec(Math.round(known * d), View.MeasureSpec.EXACTLY)
        avail == Float.POSITIVE_INFINITY -> View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED)
        avail == Float.NEGATIVE_INFINITY -> if (minContent >= 0) View.MeasureSpec.makeMeasureSpec(minContent, View.MeasureSpec.AT_MOST)
            else View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED)
        else -> View.MeasureSpec.makeMeasureSpec(Math.round(maxOf(avail, 0f) * d), View.MeasureSpec.AT_MOST)
    }
    if (v is AsukaRichText) {
        val (mw, mh) = v.measureRich(w, aw)
        return mw to (if (!h.isNaN()) h else mh)
    }
    if (v is com.google.android.material.progressindicator.LinearProgressIndicator) {
        // As wide as it's let be; as tall as Material makes it.
        v.measure(View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED), View.MeasureSpec.makeMeasureSpec(0, View.MeasureSpec.UNSPECIFIED))
        val mw = if (!w.isNaN()) w else if (aw.isFinite()) maxOf(aw, 0f) else 100f
        return mw to (if (!h.isNaN()) h else v.measuredHeight / d)
    }
    if (v is AsukaSymbol) {
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
    // Its padding (ContentInset) is the core's to add: measured with room
    // for it, then without it.
    val (px, py) = insetPx(n)
    fun grow(spec: Int, by: Int): Int = if (by == 0 || View.MeasureSpec.getMode(spec) == View.MeasureSpec.UNSPECIFIED) spec
        else View.MeasureSpec.makeMeasureSpec(View.MeasureSpec.getSize(spec) + by, View.MeasureSpec.getMode(spec))
    var ws = grow(spec(w, aw, if (minW >= 0) minW - px else minW), px)
    if (v is Slider && w.isNaN()) {
        ws = View.MeasureSpec.makeMeasureSpec(Math.round((if (aw.isFinite()) maxOf(aw, 0f) else 200f) * d), View.MeasureSpec.EXACTLY)
    }
    v.measure(ws, grow(spec(h, ah, -1), py))
    var mw = maxOf(v.measuredWidth - px, 0) / d
    var mh = maxOf(v.measuredHeight - py, 0) / d
    if (v is EditText && aw == Float.NEGATIVE_INFINITY && w.isNaN()) mw = 80f
    if (!w.isNaN()) mw = w
    if (!h.isNaN()) mh = h
    return mw to mh
}

/** A screen reader's actions on the view (TalkBack's actions menu): u32 count, then each name. */
fun AsukaRuntime.setA11yActions(n: AsukaNode, b: java.nio.ByteBuffer) {
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
            emit(id, AsukaEventKind.A11Y_ACTION) { AsukaEvents.a11yAction(id, i.toLong()) }
            true
        })
    }
}

/** OnAdjust: a seek bar to TalkBack, whose swipe up and down (and the volume keys) step it. */
fun AsukaRuntime.setAdjustable(n: AsukaNode) {
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
            emit(id, AsukaEventKind.ADJUST) { AsukaEvents.adjust(id, by) }
            return true
        }
    })
    n.view.isFocusable = true
}

/** A TextField's ContentType (ui.Content...) as Android's autofill hints name it; another an autofill hint itself. */
fun autofillHint(s: String): String = when (s) {
    "username" -> android.view.View.AUTOFILL_HINT_USERNAME
    "password" -> android.view.View.AUTOFILL_HINT_PASSWORD
    "newPassword" -> "newPassword" // androidx.autofill's HintConstants
    "oneTimeCode" -> "smsOTPCode"
    "email" -> android.view.View.AUTOFILL_HINT_EMAIL_ADDRESS
    "phone" -> android.view.View.AUTOFILL_HINT_PHONE
    "name" -> android.view.View.AUTOFILL_HINT_NAME
    "givenName" -> "personGivenName"
    "familyName" -> "personFamilyName"
    "address" -> "streetAddress"
    "postalCode" -> android.view.View.AUTOFILL_HINT_POSTAL_CODE
    "city" -> "addressLocality"
    "country" -> "addressCountry"
    "cardNumber" -> android.view.View.AUTOFILL_HINT_CREDIT_CARD_NUMBER
    "url" -> "url"
    else -> s
}
