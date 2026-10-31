package dev.ryui

import android.annotation.SuppressLint
import android.app.Application
import android.content.Context
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.text.Editable
import android.text.TextWatcher
import android.util.TypedValue
import android.view.View
import android.view.ViewGroup
import android.view.inputmethod.EditorInfo
import android.widget.FrameLayout
import android.widget.HorizontalScrollView
import android.widget.TextView
import androidx.annotation.Keep
import androidx.appcompat.view.ContextThemeWrapper
import androidx.appcompat.widget.AppCompatEditText
import androidx.core.widget.NestedScrollView
import com.google.android.material.color.MaterialColors
import com.google.android.material.button.MaterialButton
import com.google.android.material.materialswitch.MaterialSwitch
import com.google.android.material.progressindicator.CircularProgressIndicator
import com.google.android.material.slider.Slider
import com.google.android.material.textview.MaterialTextView
import java.nio.ByteBuffer

/**
 * The Android host: applies the core's ops to Android Views and reports
 * events back. One per process: the views outlive Activities, which
 * borrow them (a recreated Activity takes over the existing tree).
 * Everything runs on the main thread.
 */
class RyUIRuntime private constructor(val app: Application) : RyUIOpHandler {
    companion object {
        @SuppressLint("StaticFieldLeak")
        private var instance: RyUIRuntime? = null

        fun get(context: Context): RyUIRuntime =
            instance ?: RyUIRuntime(context.applicationContext as Application).also {
                instance = it
                RyUINative.preload(context)
            }
    }

    /** Views are made with the app's Material theme, not an Activity's. */
    var themed: Context = ContextThemeWrapper(app, R.style.Theme_RyUI)
        private set
    /** The context the theme is made on: the app's, in the current configuration. */
    private var themeBase: Context = app.createConfigurationContext(app.resources.configuration)
    /** The app's appearance (the ColorScheme command's), over the system's. */
    private var colorScheme = RyUIColorScheme.SYSTEM
    /** The app's accent color (ryui.json's), the seed of its color scheme; null: Material's. */
    var accent: Int? = null
        private set
    /** Whether the colors are the wallpaper's (ryui.json's android.dynamicColor). */
    private var wallpaperColors = false
    /** Changes when the theme does: what caches theme colors keys by it. */
    var themeVersion = 0
        private set

    /**
     * The app's Material theme on base: with a color scheme from the
     * wallpaper or the accent where the system can make one (Android 12
     * and later), as Material's DynamicColors does.
     */
    private fun theme(base: Context): Context {
        val t = ContextThemeWrapper(base, R.style.Theme_RyUI)
        if (!com.google.android.material.color.DynamicColors.isDynamicColorAvailable()) return t
        val seed = accent
        return when {
            wallpaperColors -> com.google.android.material.color.DynamicColors.wrapContextIfAvailable(t)
            seed != null -> com.google.android.material.color.DynamicColors.wrapContextIfAvailable(t,
                com.google.android.material.color.DynamicColorsOptions.Builder().setContentBasedSource(seed).build()).also {
                // The scheme's context resolves the colors but loses the
                // theme's other attributes (a toolbar's homeAsUpIndicator):
                // fill them in, not over the colors.
                if (it !== t) it.theme.applyStyle(R.style.Theme_RyUI, false)
            }
            else -> t
        }
    }

    /** The accent color: the scheme's primary, or the accent itself where there's no scheme from it. */
    fun accentColor(): Int {
        val seed = accent
        if (seed != null && !wallpaperColors && !com.google.android.material.color.DynamicColors.isDynamicColorAvailable()) return seed
        return MaterialColors.getColor(themed, androidx.appcompat.R.attr.colorPrimary, android.graphics.Color.BLUE)
    }

    /** Sets the accent (the Accent command) and makes the views the theme decided again. */
    private fun setAccent(c: Int?, wallpaper: Boolean) {
        if (c == accent && wallpaper == wallpaperColors) return
        accent = c
        wallpaperColors = wallpaper
        themeBase = app.createConfigurationContext(schemed(activity?.resources?.configuration ?: app.resources.configuration))
        themed = theme(themeBase)
        themeVersion++
        restyleAll(true)
    }

    /** config in the app's color scheme: night or not, as the app asked, or as the system is. */
    fun schemed(config: android.content.res.Configuration): android.content.res.Configuration {
        val night = when (colorScheme) {
            RyUIColorScheme.LIGHT -> android.content.res.Configuration.UI_MODE_NIGHT_NO
            RyUIColorScheme.DARK -> android.content.res.Configuration.UI_MODE_NIGHT_YES
            else -> return config
        }
        return android.content.res.Configuration(config).apply {
            uiMode = (uiMode and android.content.res.Configuration.UI_MODE_NIGHT_MASK.inv()) or night
        }
    }

    /** Whether config, in the app's color scheme, is dark. */
    fun isNight(config: android.content.res.Configuration) =
        (schemed(config).uiMode and android.content.res.Configuration.UI_MODE_NIGHT_MASK) ==
            android.content.res.Configuration.UI_MODE_NIGHT_YES

    /**
     * Shows the app light or dark, or as the system is (the ColorScheme
     * command): AppCompat's night mode, which the Activity (its
     * resources, system bars) follows, and the theme the views are made
     * with, which is the process's.
     */
    private fun setColorScheme(s: Int) {
        if (s == colorScheme) return
        colorScheme = s
        androidx.appcompat.app.AppCompatDelegate.setDefaultNightMode(when (s) {
            RyUIColorScheme.LIGHT -> androidx.appcompat.app.AppCompatDelegate.MODE_NIGHT_NO
            RyUIColorScheme.DARK -> androidx.appcompat.app.AppCompatDelegate.MODE_NIGHT_YES
            else -> androidx.appcompat.app.AppCompatDelegate.MODE_NIGHT_FOLLOW_SYSTEM
        })
        // AppCompat tells the Activity (it handles uiMode) when it's
        // shown; the views follow now either way.
        val a = activity
        if (a != null) a.schemeChanged() else configurationChanged(app.resources.configuration)
    }
    val nodes = HashMap<Int, RyUINode>()
    var rootID = 0
    var activity: RyUIActivity? = null
    private var started = false
    private val main = Handler(Looper.getMainLooper())

    /** Runs f on the main thread, later (a module's reply from any thread). */
    fun onMain(f: () -> Unit) { main.post(f) }
    private var depth = 0
    private val pendingLists = ArrayList<RyUIList>()
    private val pendingEvents = ArrayList<ByteArray>()
    private var draining = false
    /** Every Stack, to find the one back pops. */
    val stacks = LinkedHashSet<RyUINode>()
    /** Back pops the active stack's top screen, following the predictive back gesture. */
    val back = makeBackCallback()
    val nativeFactories = HashMap<String, (RyUINativeContext) -> RyUINativeView>()
    /** Between AnimateBegin and AnimateEnd. */
    var animation: RyUIAnimation? = null
    /** Views coming in with a transition in the batch being applied. */
    val entering = ArrayList<Pair<RyUINode, RyUIAnimation>>()
    /** Views going out with a transition, still shown. */
    val exiting = HashSet<View>()
    /** Style.Match: the views with keys, where they were as an animated batch began destroying, and those going and coming in it. */
    val matchViews = HashMap<String, RyUINode>()
    var matchFrames: HashMap<String, android.graphics.Rect>? = null
    val matchOut = HashMap<String, Pair<View, android.graphics.Rect>>()
    val matchIn = ArrayList<RyUINode>()
    var matchAnimation: RyUIAnimation? = null
    var framesApplied = false
    /** Native modules' methods, by module and method name (ui.Call). */
    val modules = HashMap<String, Map<String, RyUIModuleMethod>>()
    var builtinModules = false

    /** Registers a native view kind for ui.Native. */
    fun registerNative(type: String, make: (RyUINativeContext) -> RyUINativeView) {
        nativeFactories[type] = make
    }
    private var nextCellID = 0
    var density = app.resources.displayMetrics.density
        private set

    // Calling into Go

    /**
     * Every call into Go goes through here, so that work Android might do
     * re-entrantly (a list update binding cells, a screen reporting its
     * size while Go lays out) happens once Go returned.
     */
    fun enter(f: () -> Unit) {
        depth++
        try {
            f()
        } finally {
            depth--
            if (depth == 0) drain()
        }
    }

    private fun drain() {
        if (draining) return
        draining = true
        try {
            while (pendingLists.isNotEmpty() || pendingEvents.isNotEmpty() || pendingCulls.isNotEmpty()) {
                if (pendingCulls.isNotEmpty()) {
                    val ss = ArrayList(pendingCulls)
                    pendingCulls.clear()
                    for (s in ss) if (nodes[s.id] === s) cull(s)
                } else if (pendingLists.isNotEmpty()) {
                    val lists = ArrayList(pendingLists)
                    pendingLists.clear()
                    for (l in lists) l.applyPending()
                } else {
                    val e = pendingEvents.removeAt(0)
                    enter { RyUINative.event(e) }
                }
            }
            updateBack()
        } finally {
            draining = false
        }
    }

    /** Sends an event to Go, or queues it while Go is applying ops. */
    fun send(bytes: ByteArray) {
        if (depth > 0) {
            pendingEvents.add(bytes)
            return
        }
        enter { RyUINative.event(bytes) }
    }

    fun updateBack() {
        back.isEnabled = activeStack() != null
    }

    private var framesOn = false
    private val frame = object : android.view.Choreographer.FrameCallback {
        override fun doFrame(nanos: Long) {
            if (!framesOn) return
            android.view.Choreographer.getInstance().postFrameCallback(this)
            send(RyUIEvents.frame(rootID, nanos / 1e9))
        }
    }

    /** Sends Frame at each vsync, while the core has OnFrame callbacks. */
    fun setFrames(on: Boolean) {
        if (on == framesOn) return
        framesOn = on
        if (on) android.view.Choreographer.getInstance().postFrameCallback(frame)
        else android.view.Choreographer.getInstance().removeFrameCallback(frame)
    }

    /** Reports a Scroll's offset, in points, as Scrolled (when the core listens). */
    private fun reportScrolls(v: View, id: Int) {
        val r = RyUIScrollReporter(this, id, v)
        v.setOnScrollChangeListener { _, x, y, _, _ ->
            r.scrolled(x, y)
            nodes[id]?.let { cull(it) }
        }
    }

    private val pendingCulls = LinkedHashSet<RyUINode>()

    /**
     * Takes the rows of a Scroll's content more than a screen outside what
     * it shows out of the view tree, as RecyclerView does its items, and
     * puts them back as they come near: each one drawn (recorded) and
     * measured is work for every frame it changes in, on screen or not. A
     * row out of the tree keeps its frame and props. While TalkBack
     * explores by touch it takes nothing out: TalkBack moves through every
     * row.
     */
    fun cull(scroll: RyUINode) {
        val sv = scroll.view
        val content = scroll.children.firstOrNull() ?: return
        val c = content.view as? RyUIContainer ?: return
        val w = sv.width
        val h = sv.height
        if (w == 0 || h == 0) return
        val all = (app.getSystemService(Context.ACCESSIBILITY_SERVICE) as android.view.accessibility.AccessibilityManager).isTouchExplorationEnabled
        val l = sv.scrollX - w
        val t = sv.scrollY - h
        val r = sv.scrollX + 2 * w
        val b = sv.scrollY + 2 * h
        var before = 0 // rows there before this one
        for (row in content.children) {
            val v = row.outer
            val near = all || (v.right > l && v.left < r && v.bottom > t && v.top < b)
            if (near && v.parent == null) c.putBack(v, before)
            else if (!near && v.parent === c) c.takeOut(v)
            if (v.parent === c) before++
        }
    }

    fun emit(id: Int, event: Int, make: () -> ByteArray) {
        val n = nodes[id] ?: return
        if (n.listens(event)) send(make())
    }

    fun attach(activity: RyUIActivity) {
        this.activity = activity
        density = activity.resources.displayMetrics.density
        if (!started) {
            started = true
            RyUIAutomation.start(this)
            RyUINative.load(app)
            registerNatives()
            activity.sendEnvironment(force = true)
            enter { RyUINative.start(this) }
            val urls = ArrayList(pendingURLs)
            pendingURLs.clear()
            for (u in urls) openURL(u)
            val shares = ArrayList(pendingShares)
            pendingShares.clear()
            for (s in shares) shared(s)
        } else {
            activity.sendEnvironment(force = true)
            enter { RyUINative.attach() }
        }
    }

    private val pendingURLs = ArrayList<String>()
    private val pendingShares = ArrayList<String>()

    /** Another app shared text with this one (ACTION_SEND): ui.OnShare gets it, once the core started. */
    fun shared(text: String) {
        if (!started) { pendingShares.add(text); return }
        send(RyUIEvents.shared(rootID, text))
    }

    /** The app was opened with url (its scheme, an app link): ui.OnOpenURL gets it, once the core started. */
    fun openURL(url: String) {
        // A sign-in's callback (webAuth): its answer, not the app's link.
        val a = auth
        if (a != null && a.scheme.isNotEmpty() && Uri.parse(url).scheme.equals(a.scheme, ignoreCase = true)) {
            auth = null
            a.reply(Result.success(url.toByteArray()))
            return
        }
        if (!started) { pendingURLs.add(url); return }
        send(RyUIEvents.incomingURL(rootID, url))
    }

    fun detach(activity: RyUIActivity) {
        if (this.activity === activity) {
            nodes[rootID]?.view?.let { (it.parent as? ViewGroup)?.removeView(it) }
            this.activity = null
        }
    }

    fun lifecycle(state: Int) {
        if (started) send(RyUIEvents.lifecycle(rootID, state.toLong()))
    }

    /** The webAuth module's sign-in under way. */
    var auth: RyUIAuth? = null

    /** The Activity was paused: a sign-in's tab may be over it. */
    fun paused() { auth?.away = true }

    /** The Activity is back: a sign-in's tab closed without a callback is canceled. */
    fun resumed() {
        val a = auth ?: return
        if (!a.away) return
        auth = null
        a.reply(Result.failure(Exception("canceled")))
    }

    /** The configuration changed (dark mode, font scale): restyle what the theme decided. */
    fun configurationChanged(system: android.content.res.Configuration) {
        val config = schemed(system)
        val night = isNight(config)
        themeBase = app.createConfigurationContext(config)
        themed = theme(themeBase)
        density = config.densityDpi / 160f
        // Views resolve theme colors and text sizes when they're made:
        // after a dark mode, font scale or density change, make the ones
        // that show text or Material colors again.
        val dense = config.densityDpi != densityDpi
        val changed = night != this.night || config.fontScale != fontScale || dense
        this.night = night
        fontScale = config.fontScale
        densityDpi = config.densityDpi
        if (changed) themeVersion++
        restyleAll(changed)
        if (dense) reframe()
    }

    /**
     * The core's frames are in points: after a density change (the
     * user's display size) each view's is set again, in its pixels. The
     * core sends none, its layout being the same.
     */
    private fun reframe() {
        val a = animation
        animation = null
        for (n in ArrayList(nodes.values)) {
            val f = n.frame ?: continue
            setFrame(n.id, f[0], f[1], f[2], f[3])
        }
        animation = a
        framesApplied = false
    }

    /** Restyles every view; with changed, makes those that show text or Material colors again. */
    private fun restyleAll(changed: Boolean) {
        activity?.window?.setBackgroundDrawable(android.graphics.drawable.ColorDrawable(
            MaterialColors.getColor(themed, com.google.android.material.R.attr.colorSurface, android.graphics.Color.WHITE)))
        for (n in ArrayList(nodes.values)) {
            if (changed && n.kind in rebuilt) rebuild(n) else restyle(n)
            if (changed) {
                n.list?.restyle()
                (n.view as? RyUIStack)?.restyle()
                (n.view as? RyUITabs)?.restyle()
                (n.view as? RyUISegmented)?.restyle()
            }
        }
    }

    private var night = (app.resources.configuration.uiMode and android.content.res.Configuration.UI_MODE_NIGHT_MASK) ==
        android.content.res.Configuration.UI_MODE_NIGHT_YES
    private var fontScale = app.resources.configuration.fontScale
    private var densityDpi = app.resources.configuration.densityDpi

    private val rebuilt = setOf(RyUIKind.TEXT, RyUIKind.BUTTON, RyUIKind.TEXT_FIELD, RyUIKind.TOGGLE, RyUIKind.SLIDER, RyUIKind.ACTIVITY)

    /** Makes n's view again with the current theme, replaying its props and keeping the user's state. */
    private fun rebuild(n: RyUINode) {
        val old = n.view
        val v = makeView(n.id, n.kind)
        n.view = v
        settingProp = true
        for ((prop, value) in n.props) {
            when (value) {
                is Boolean -> propBool(n.id, prop, value)
                is Long -> if (prop == RyUIProp.BACKGROUND || prop == RyUIProp.COLOR || prop == RyUIProp.BORDER_COLOR) propColor(n.id, prop, value) else propInt(n.id, prop, value)
                is Double -> propFloat(n.id, prop, value)
                is String -> propString(n.id, prop, value)
                is Int -> propEnum(n.id, prop, value)
            }
        }
        val cur = n.view
        when {
            old is android.widget.EditText && cur is android.widget.EditText -> {
                cur.setText(old.text)
                cur.setSelection(old.selectionStart.coerceAtLeast(0), old.selectionEnd.coerceAtLeast(0))
            }
            old is MaterialSwitch && cur is MaterialSwitch -> cur.isChecked = old.isChecked
            old is Slider && cur is Slider -> { cur.valueFrom = old.valueFrom; cur.valueTo = old.valueTo; cur.value = old.value }
        }
        settingProp = false
        val now = n.view // replaying a button's variant replaced it again
        restyle(n)
        val parent = old.parent as? ViewGroup ?: return
        val i = parent.indexOfChild(old)
        val lp = old.layoutParams
        val focused = old.hasFocus()
        parent.removeViewAt(i)
        parent.addView(now, i, lp)
        now.measure(View.MeasureSpec.makeMeasureSpec(old.width, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(old.height, View.MeasureSpec.EXACTLY))
        now.layout(old.left, old.top, old.right, old.bottom)
        if (focused) now.requestFocus()
    }

    // JNI callbacks

    @Keep
    fun apply(buf: ByteBuffer) {
        ryuiDecode(buf, this)
        runEnters()
        runMatches()
    }

    @Keep
    fun measure(id: Int, w: Float, h: Float, aw: Float, ah: Float): Long {
        val (mw, mh) = measureNode(nodes[id] ?: return 0, w, h, aw, ah)
        return (java.lang.Float.floatToRawIntBits(mw).toLong() shl 32) or
            (java.lang.Float.floatToRawIntBits(mh).toLong() and 0xffffffffL)
    }

    @Keep
    fun post() {
        main.post { enter { RyUINative.runPosted() } }
    }

    // Ops

    override fun create(id: Int, kind: Int) {
        val view = makeView(id, kind)
        val n = RyUINode(id, kind, view)
        if (kind == RyUIKind.LIST) n.list = (view.tag as RyUIList)
        if (kind == RyUIKind.TEXT_FIELD) n.textStyle = RyUITextStyle.BODY
        if (kind == RyUIKind.STACK) stacks.add(n)
        nodes[id] = n
        if (kind == RyUIKind.TEXT || kind == RyUIKind.TEXT_FIELD) applyColor(n)
    }

    private fun makeView(id: Int, kind: Int): View {
        val ctx = themed
        return when (kind) {
            RyUIKind.VIEW -> RyUIContainer(ctx)
            // The platform's TextView: AppCompat's (Material's) posts a message
            // to load the theme's font for each one made, and needs nothing
            // else here; the Material text appearance is applied all the same.
            RyUIKind.TEXT -> TextView(ctx).apply { applyTextStyle(this, 0) }
            RyUIKind.BUTTON -> makeButton(id, RyUIButtonVariant.PLAIN)
            RyUIKind.TEXT_FIELD -> makeTextField(id)
            RyUIKind.TOGGLE -> MaterialSwitch(ctx).apply {
                setOnCheckedChangeListener { _, on -> if (!settingProp) emit(id, RyUIEventKind.TOGGLED) { RyUIEvents.toggled(id, on) } }
            }
            RyUIKind.SLIDER -> Slider(ctx).apply {
                addOnChangeListener { _, v, fromUser -> if (fromUser) emit(id, RyUIEventKind.VALUE_CHANGED) { RyUIEvents.valueChanged(id, v.toDouble()) } }
            }
            RyUIKind.WEB_VIEW -> RyUIWebView(this, id)
            RyUIKind.SHAPE -> RyUIShape(this)
            RyUIKind.VIDEO -> RyUIVideo(this, id)
            RyUIKind.SCROLL -> NestedScrollView(ctx).apply { isFillViewport = false; reportScrolls(this, id) }
            RyUIKind.LIST -> RyUIList(id, this).recycler
            RyUIKind.ACTIVITY -> CircularProgressIndicator(ctx).apply { isIndeterminate = true }
            RyUIKind.SYMBOL -> RyUISymbol(this)
            RyUIKind.SEGMENTED -> RyUISegmented(this, id)
            RyUIKind.WHEEL -> RyUIWheel(this, id)
            RyUIKind.STEPPER -> RyUIStepper(this, id)
            RyUIKind.COLOR_PICKER -> RyUIColorPicker(this, id)
            RyUIKind.PROGRESS -> makeProgress()
            RyUIKind.DATE_PICKER -> RyUIDatePicker(this, id)
            RyUIKind.STACK -> RyUIStack(ctx, id, this)
            RyUIKind.TABS -> RyUITabs(ctx, id, this)
            RyUIKind.SCREEN, RyUIKind.SHEET -> RyUIScreen(ctx, id, this)
            RyUIKind.ALERT -> View(ctx)
            RyUIKind.IMAGE -> androidx.appcompat.widget.AppCompatImageView(ctx).apply { scaleType = android.widget.ImageView.ScaleType.FIT_CENTER }
            RyUIKind.NATIVE -> RyUINativeHost(ctx)
            RyUIKind.RICH_TEXT -> RyUIRichText(this, id)
            RyUIKind.SELECTABLE -> RyUISelectionArea(this)
            else -> error("ryui: the Android host can't show ${RyUIKind.name(kind)} ($kind) yet")
        }
    }

    var settingProp = false

    private fun makeButton(id: Int, variant: Int): MaterialButton {
        val overlay = when (variant) {
            RyUIButtonVariant.FILLED -> R.style.ThemeOverlay_RyUI_FilledButton
            RyUIButtonVariant.TINTED, RyUIButtonVariant.GRAY -> R.style.ThemeOverlay_RyUI_TonalButton
            RyUIButtonVariant.BORDERED -> R.style.ThemeOverlay_RyUI_OutlinedButton
            else -> R.style.ThemeOverlay_RyUI_TextButton
        }
        return MaterialButton(ContextThemeWrapper(themed, overlay)).apply {
            isAllCaps = false
            setOnClickListener { clicked(id) }
        }
    }

    private fun makeTextField(id: Int) = AppCompatEditText(themed).apply {
        isSingleLine = true
        imeOptions = EditorInfo.IME_ACTION_DONE
        addTextChangedListener(object : TextWatcher {
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {}
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {}
            override fun afterTextChanged(s: Editable?) {
                if (!settingProp) {
                    val t = s?.toString() ?: ""
                    emit(id, RyUIEventKind.TEXT_CHANGED) { RyUIEvents.textChanged(id, t) }
                }
            }
        })
        setOnEditorActionListener { _, action, _ ->
            if (action == EditorInfo.IME_NULL || action >= EditorInfo.IME_ACTION_GO) {
                emit(id, RyUIEventKind.SUBMIT) { RyUIEvents.submit(id) }
                true
            } else false
        }
        setOnFocusChangeListener { _, focused ->
            if (focused) emit(id, RyUIEventKind.FOCUS) { RyUIEvents.focus(id) }
            else emit(id, RyUIEventKind.BLUR) { RyUIEvents.blur(id) }
        }
    }

    /**
     * Makes a Scroll scroll horizontally, or vertically again: Android's are
     * two views (HorizontalScrollView, NestedScrollView), so the view is
     * replaced, its content moved to the new one.
     */
    fun setHorizontal(n: RyUINode, horizontal: Boolean) {
        val old = n.view as? android.widget.FrameLayout ?: return
        if ((old is HorizontalScrollView) == horizontal) return
        val v: android.widget.FrameLayout = if (horizontal) HorizontalScrollView(themed).apply { isFillViewport = false }
            else NestedScrollView(themed).apply { isFillViewport = false }
        reportScrolls(v, n.id)
        while (old.childCount > 0) {
            val c = old.getChildAt(0)
            val lp = c.layoutParams
            old.removeViewAt(0)
            v.addView(c, lp)
        }
        val parent = old.parent as? ViewGroup
        if (parent != null) {
            val i = parent.indexOfChild(old)
            val lp = old.layoutParams
            parent.removeViewAt(i)
            parent.addView(v, i, lp)
            v.measure(View.MeasureSpec.makeMeasureSpec(old.width, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(old.height, View.MeasureSpec.EXACTLY))
            v.layout(old.left, old.top, old.right, old.bottom)
        }
        n.view = v
        restyle(n)
    }

    /** Replaces a button with one of another variant (a style is fixed when a view is made). */
    fun replaceButton(n: RyUINode, variant: Int) {
        val old = n.view as MaterialButton
        val b = makeButton(n.id, variant)
        b.text = old.text
        b.isEnabled = old.isEnabled
        (n.props[RyUIProp.Button.ICON] as? String)?.let { setButtonIcon(n, b, it) }
        val parent = old.parent as? ViewGroup
        if (parent != null) {
            val i = parent.indexOfChild(old)
            val lp = old.layoutParams
            parent.removeViewAt(i)
            parent.addView(b, i, lp)
            b.measure(View.MeasureSpec.makeMeasureSpec(old.width, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(old.height, View.MeasureSpec.EXACTLY))
            b.layout(old.left, old.top, old.right, old.bottom)
        }
        n.view = b
        restyle(n)
    }

    override fun setBool(id: Int, prop: Int, v: Boolean) = propBool(id, prop, v)
    override fun setInt(id: Int, prop: Int, v: Long) = propInt(id, prop, v)
    override fun setFloat(id: Int, prop: Int, v: Double) = propFloat(id, prop, v)
    override fun setString(id: Int, prop: Int, v: String) = propString(id, prop, v)
    override fun setColor(id: Int, prop: Int, v: Long) = propColor(id, prop, v)
    override fun setEdges(id: Int, prop: Int, top: Float, right: Float, bottom: Float, left: Float) {}
    override fun setEnum(id: Int, prop: Int, v: Int) = propEnum(id, prop, v)
    override fun setBytes(id: Int, prop: Int, v: ByteBuffer) {
        val n = nodes[id] ?: return
        if (n.kind == RyUIKind.SHAPE && setShapeProp(n, prop, v)) return
        if (n.kind == RyUIKind.WHEEL && setWheelProp(n, prop, v)) return
        if (n.kind == RyUIKind.WEB_VIEW && prop == RyUIProp.WebView.BLOCK) { (n.view as RyUIWebView).setBlock(v); return }
        if (prop == RyUIProp.MENU) {
            n.actions = decodeActions(v)
            wirePresses(n)
            return
        }
        if (prop == RyUIProp.A11Y_ACTIONS) {
            setA11yActions(n, v)
            return
        }
        if (prop == RyUIProp.SWIPE_ACTIONS) {
            n.swipeActions = decodeActions(v)
            swipeAccessibility(n)
            return
        }
        when (prop) {
            RyUIProp.TRANSFORM -> { setTransform(n, v); return }
            RyUIProp.TRANSITION -> { n.transition = RyUITransition(v); return }
            RyUIProp.SHADOW -> { setShadow(n, v); return }
            RyUIProp.GRADIENT -> { setGradient(n, v); return }
        }
        setNavProp(n, prop, v)
    }

    override fun destroy(id: Int) {
        val n = nodes.remove(id) ?: return
        (n.view as? RyUIVideo)?.release()
        stacks.remove(n)
        if (n.underDying) return // its views leave with the animating screen or sheet
        if ((n.kind == RyUIKind.SCREEN || n.kind == RyUIKind.SHEET || n.kind == RyUIKind.ALERT) && destroyNav(n)) return
        if (matchOut(n)) {
            n.parent?.children?.remove(n)
            n.parent = null
            for (c in n.children) c.parent = null
            return
        }
        // Going out with a transition, or inside a view that is: shown
        // until it's gone.
        if (leaving(n.outer) || exit(n)) {
            n.parent?.children?.remove(n)
            n.parent = null
            for (c in n.children) c.parent = null
            return
        }
        n.parent?.remove(n)
        for (c in n.children) c.parent = null
        (n.outer.parent as? ViewGroup)?.removeView(n.outer)
    }

    override fun insert(parent: Int, child: Int, index: Int) {
        val p = nodes[parent] ?: return
        val c = nodes[child] ?: return
        if (insertNav(p, c, index)) return
        p.insert(c, index)
        enter(c)
        matchIn(c)
    }

    override fun remove(parent: Int, child: Int) {
        val p = nodes[parent] ?: return
        val c = nodes[child] ?: return
        p.remove(c)
    }

    override fun setFrame(id: Int, x: Float, y: Float, width: Float, height: Float) {
        val n = nodes[id] ?: return
        framesApplied = true
        val f = n.frame ?: FloatArray(4).also { n.frame = it }
        f[0] = x; f[1] = y; f[2] = width; f[3] = height
        // A Scroll, or a row of its content, moved: what it takes out is redone.
        if (n.kind == RyUIKind.SCROLL) pendingCulls.add(n)
        n.parent?.parent?.let { if (it.kind == RyUIKind.SCROLL) pendingCulls.add(it) }
        val d = density
        val l = Math.round(x * d)
        val t = Math.round(y * d)
        val r = Math.round((x + width) * d)
        val b = Math.round((y + height) * d)
        n.targetX = l
        n.targetY = t
        val v = n.outer
        if (v is RyUICell) {
            if (v.rowHeight != b - t) {
                v.rowHeight = b - t
                if (!v.binding) v.requestLayout()
            }
            return
        }
        if (animates(v)) animateFrame(n, v, l, t, r, b) { l2, t2, r2, b2 -> applyFrame(v, l2, t2, r2, b2) }
        else {
            n.frameAnimator?.cancel()
            applyFrame(v, l, t, r, b)
        }
    }

    private fun applyFrame(v: View, l: Int, t: Int, r: Int, b: Int) {
        if (v is RyUIContainer) {
            v.frameWidth = r - l
            v.frameHeight = b - t
        }
        val lp = v.layoutParams
        if (lp is RyUIParams) {
            lp.x = l
            lp.y = t
            lp.w = r - l
            lp.h = b - t
        }
        v.measure(View.MeasureSpec.makeMeasureSpec(r - l, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(b - t, View.MeasureSpec.EXACTLY))
        v.layout(l, t, r, b)
        val parent = v.parent
        // An inline component: its size is the core's, its place the text's.
        if (parent is RyUIRichText) parent.placeInlines()
        if (parent is NestedScrollView || parent is HorizontalScrollView || parent is FrameLayout) {
            (parent as View).requestLayout()
        }
    }

    override fun listen(id: Int, event: Int) {
        val n = nodes[id] ?: return
        n.listens = n.listens or (1 shl event)
        if (event == RyUIEventKind.PRESS || event == RyUIEventKind.LONG_PRESS) wirePresses(n)
        if (n.kind == RyUIKind.VIEW && (event == RyUIEventKind.PAN || event == RyUIEventKind.PINCH || event == RyUIEventKind.SWIPE || event == RyUIEventKind.ROTATE)) listenGesture(n, event)
    }

    override fun unlisten(id: Int, event: Int) {
        val n = nodes[id] ?: return
        n.listens = n.listens and (1 shl event).inv()
    }

    override fun command(id: Int, command: Int, args: ByteBuffer) {
        val n = nodes[id] ?: return
        when (command) {
            RyUICommand.FOCUS -> {
                n.view.requestFocus()
                val imm = app.getSystemService(android.view.inputmethod.InputMethodManager::class.java)
                imm?.showSoftInput(n.view, 0)
            }
            RyUICommand.BLUR -> {
                n.view.clearFocus()
                val imm = app.getSystemService(android.view.inputmethod.InputMethodManager::class.java)
                imm?.hideSoftInputFromWindow(n.view.windowToken, 0)
            }
            RyUICommand.LIST_UPDATE -> {
                val count = args.int
                val ids = LongArray(count) { args.long }
                val l = n.list ?: return
                l.update(ids)
                if (l !in pendingLists) pendingLists.add(l)
            }
            RyUICommand.PRESENT -> present(n)
            RyUICommand.HAPTIC -> if (args.remaining() > 0) haptic(args.get().toInt() and 0xff)
            RyUICommand.ANIMATE_BEGIN -> beginAnimation(args)
            RyUICommand.ANIMATE_END -> animation = null
            RyUICommand.ACCENT -> {
                if (args.remaining() < 9) return
                val c = args.long
                val flags = args.get().toInt()
                setAccent(if (c != 0L) color(c) else null, flags and 1 != 0)
            }
            RyUICommand.COLOR_SCHEME -> if (args.remaining() > 0) setColorScheme(args.get().toInt())
            RyUICommand.CALL -> call(id, args)
            RyUICommand.FRAMES -> setFrames(args.remaining() > 0 && args.get().toInt() == 1)
            RyUICommand.PLAY -> (n.view as? RyUIVideo)?.player?.play()
            RyUICommand.PAUSE -> (n.view as? RyUIVideo)?.player?.pause()
            RyUICommand.SEEK -> if (args.remaining() >= 8) (n.view as? RyUIVideo)?.player?.seekTo((args.double * 1000).toLong())
            RyUICommand.WEB_NAVIGATE -> if (args.remaining() > 0) (n.view as? RyUIWebView)?.navigate(args.get().toInt())
            RyUICommand.EVAL -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                (n.view as? RyUIWebView)?.evaluateJavascript(String(b, Charsets.UTF_8), null)
            }
            RyUICommand.ANNOUNCE -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                @Suppress("DEPRECATION") // its replacements (live regions, pane titles) describe views, not events
                activity?.window?.decorView?.announceForAccessibility(String(b, Charsets.UTF_8))
            }
            RyUICommand.COPY -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                (app.getSystemService(Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager)
                    .setPrimaryClip(android.content.ClipData.newPlainText(null, String(b, Charsets.UTF_8)))
            }
            RyUICommand.SHARE -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                val send = android.content.Intent(android.content.Intent.ACTION_SEND).apply {
                    type = "text/plain"
                    putExtra(android.content.Intent.EXTRA_TEXT, String(b, Charsets.UTF_8))
                }
                val i = android.content.Intent.createChooser(send, null)
                runCatching { (activity ?: app).startActivity(if (activity == null) i.addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK) else i) }
            }
            RyUICommand.SCROLL_TO -> {
                if (args.remaining() < 5) return
                val i = args.int
                n.list?.scroll(i, args.get().toInt() != 0)
            }
            RyUICommand.SCROLL_TO_OFFSET -> {
                if (args.remaining() < 9) return
                val x = (args.float * density).coerceAtMost(1e9f).toInt()
                val y = (args.float * density).coerceAtMost(1e9f).toInt()
                val animated = args.get().toInt() != 0
                // Each clamps to what it can scroll.
                when (val v = n.view) {
                    is NestedScrollView -> if (animated) v.smoothScrollTo(x, y) else v.scrollTo(x, y)
                    is HorizontalScrollView -> if (animated) v.smoothScrollTo(x, y) else v.scrollTo(x, y)
                    is androidx.recyclerview.widget.RecyclerView -> {
                        n.list?.applyPending()
                        // By the difference: a list knows where it is, not where an offset is.
                        val dx = x - v.computeHorizontalScrollOffset()
                        val dy = y - v.computeVerticalScrollOffset()
                        if (animated) v.smoothScrollBy(dx, dy) else v.scrollBy(dx, dy)
                    }
                }
            }
            RyUICommand.OPEN_URL -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                val i = android.content.Intent(android.content.Intent.ACTION_VIEW, android.net.Uri.parse(String(b, Charsets.UTF_8)))
                runCatching { (activity ?: app).startActivity(if (activity == null) i.addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK) else i) }
            }
        }
    }

    override fun setRoot(id: Int) {
        rootID = id
        val v = nodes[id]?.view ?: return
        activity?.mount(v)
    }

    // Cells

    fun makeCell(cell: RyUICell): Int {
        nextCellID++
        val id = (0x80000000L or nextCellID.toLong()).toInt()
        cell.ryuiID = id
        nodes[id] = RyUINode(id, 0, cell)
        return id
    }
}
