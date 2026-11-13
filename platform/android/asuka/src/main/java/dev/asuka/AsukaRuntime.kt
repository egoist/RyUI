package dev.asuka

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
class AsukaRuntime private constructor(val app: Application) : AsukaOpHandler {
    companion object {
        @SuppressLint("StaticFieldLeak")
        private var instance: AsukaRuntime? = null

        fun get(context: Context): AsukaRuntime =
            instance ?: AsukaRuntime(context.applicationContext as Application).also {
                instance = it
                AsukaNative.preload(context)
            }
    }

    /** Views are made with the app's Material theme, not an Activity's. */
    var themed: Context = ContextThemeWrapper(app, R.style.Theme_Asuka)
        private set
    /** The context the theme is made on: the app's, in the current configuration. */
    private var themeBase: Context = app.createConfigurationContext(app.resources.configuration)
    /** The app's appearance (the ColorScheme command's), over the system's. */
    private var colorScheme = AsukaColorScheme.SYSTEM
    /** The app's accent color (asuka.json's), the seed of its color scheme; null: Material's. */
    var accent: Int? = null
        private set
    /** Whether the colors are the wallpaper's (asuka.json's android.dynamicColor). */
    private var wallpaperColors = false
    /** Changes when the theme or the language does: what caches theme colors keys by it. */
    var themeVersion = 0
        private set
    /** The language the app shows (the Locale command's), whatever the system's; null until the core says. */
    var shownLocale: java.util.Locale? = null
        private set
    /** Whether the app is laid out right to left: the Locale command's flag, which may disagree with its tag (asuka dev --dir). */
    var rtl = false
        private set
    /** The user's languages and formats, as the environment reports them. */
    var region = readRegion()
        private set

    /** config with the language and direction the app shows: what its views' strings and resources come from. */
    fun localized(config: android.content.res.Configuration): android.content.res.Configuration {
        val l = shownLocale ?: return config
        return android.content.res.Configuration(config).apply {
            // One locale: with more, resources may choose another one as the
            // best match, and the configuration's direction with it.
            setLocales(android.os.LocaleList(l))
            screenLayout = (screenLayout and android.content.res.Configuration.SCREENLAYOUT_LAYOUTDIR_MASK.inv()) or
                (if (rtl) android.content.res.Configuration.SCREENLAYOUT_LAYOUTDIR_RTL else android.content.res.Configuration.SCREENLAYOUT_LAYOUTDIR_LTR)
        }
    }

    /**
     * The direction a view is laid out in: its own (ui.WithDirection), or
     * the app's. Each view has it from when it's made, not through its
     * parent: the core measures views that aren't attached yet.
     */
    fun directionOf(n: AsukaNode): Int = when (n.props[AsukaProp.LAYOUT_DIRECTION] as? Int) {
        AsukaLayoutDirection.LEFT_TO_RIGHT -> View.LAYOUT_DIRECTION_LTR
        AsukaLayoutDirection.RIGHT_TO_LEFT -> View.LAYOUT_DIRECTION_RTL
        AsukaLayoutDirection.INHERIT -> View.LAYOUT_DIRECTION_INHERIT
        else -> if (rtl) View.LAYOUT_DIRECTION_RTL else View.LAYOUT_DIRECTION_LTR
    }

    /** The Locale command: makes the app's views again in the language and direction, keeping the user's state. */
    private fun setLocale(l: java.util.Locale, rtl: Boolean) {
        if (l == shownLocale && rtl == this.rtl) return
        val turned = rtl != this.rtl
        shownLocale = l
        this.rtl = rtl
        keepDefaultLocale()
        themeBase = app.createConfigurationContext(localized(schemed(app.resources.configuration)))
        themed = theme(themeBase)
        themeVersion++
        activity?.localeChanged()
        if (turned) for (n in nodes.values) n.view.layoutDirection = directionOf(n)
        restyleAll(true)
    }

    /**
     * What's made without a context (a text's locale, which picks Han
     * glyphs; formats of dates) goes by the default locale, which the system
     * sets again at every configuration change.
     */
    private fun keepDefaultLocale() {
        val l = shownLocale ?: return
        if (java.util.Locale.getDefault() != l) android.os.LocaleList.setDefault(android.os.LocaleList(l))
    }

    /** Reads the user's languages and formats again; if they changed, the environment says so. */
    fun regionChanged() {
        keepDefaultLocale()
        val next = readRegion()
        if (next == region) return
        region = next
        activity?.sendEnvironment()
        reportScreens()
    }

    private var regionReceiver: android.content.BroadcastReceiver? = null

    /** The system's languages and 24-hour setting can change with no Activity to be told. */
    private fun watchRegion() {
        if (regionReceiver != null) return
        val r = object : android.content.BroadcastReceiver() {
            override fun onReceive(c: Context, i: android.content.Intent) = regionChanged()
        }
        regionReceiver = r
        val f = android.content.IntentFilter(android.content.Intent.ACTION_LOCALE_CHANGED)
        f.addAction(android.content.Intent.ACTION_TIME_CHANGED)
        androidx.core.content.ContextCompat.registerReceiver(app, r, f, androidx.core.content.ContextCompat.RECEIVER_NOT_EXPORTED)
    }

    /** The SaveLocale command: the app's language for its next launches, or "" to follow the system's. */
    private fun saveLocale(tag: String) {
        // The system tells the process with a configuration change, which
        // the Activity handles (configChanges): the language has changed by then.
        app.getSystemService(android.app.LocaleManager::class.java)?.applicationLocales =
            if (tag.isEmpty()) android.os.LocaleList.getEmptyLocaleList() else android.os.LocaleList.forLanguageTags(tag)
    }

    /**
     * The app's Material theme on base: with a color scheme from the
     * wallpaper or the accent where the system can make one, as Material's
     * DynamicColors does.
     */
    private fun theme(base: Context): Context {
        val t = ContextThemeWrapper(base, R.style.Theme_Asuka)
        if (!com.google.android.material.color.DynamicColors.isDynamicColorAvailable()) return t
        val seed = accent
        return when {
            wallpaperColors -> com.google.android.material.color.DynamicColors.wrapContextIfAvailable(t)
            seed != null -> com.google.android.material.color.DynamicColors.wrapContextIfAvailable(t,
                com.google.android.material.color.DynamicColorsOptions.Builder().setContentBasedSource(seed).build()).also {
                // The scheme's context resolves the colors but loses the
                // theme's other attributes (a toolbar's homeAsUpIndicator):
                // fill them in, not over the colors.
                if (it !== t) it.theme.applyStyle(R.style.Theme_Asuka, false)
            }
            else -> t
        }
    }

    /** A context for a dialog over act: the activity's window, the app's colors. */
    fun dialogContext(act: Context): Context = theme(act)

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
        themeBase = app.createConfigurationContext(localized(schemed(activity?.resources?.configuration ?: app.resources.configuration)))
        themed = theme(themeBase)
        themeVersion++
        restyleAll(true)
    }

    /** config in the app's color scheme: night or not, as the app asked, or as the system is. */
    fun schemed(config: android.content.res.Configuration): android.content.res.Configuration {
        val night = when (colorScheme) {
            AsukaColorScheme.LIGHT -> android.content.res.Configuration.UI_MODE_NIGHT_NO
            AsukaColorScheme.DARK -> android.content.res.Configuration.UI_MODE_NIGHT_YES
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
            AsukaColorScheme.LIGHT -> androidx.appcompat.app.AppCompatDelegate.MODE_NIGHT_NO
            AsukaColorScheme.DARK -> androidx.appcompat.app.AppCompatDelegate.MODE_NIGHT_YES
            else -> androidx.appcompat.app.AppCompatDelegate.MODE_NIGHT_FOLLOW_SYSTEM
        })
        // AppCompat tells the Activity (it handles uiMode) when it's
        // shown; the views follow now either way.
        val a = activity
        if (a != null) a.schemeChanged() else configurationChanged(app.resources.configuration)
    }
    val nodes = HashMap<Int, AsukaNode>()
    /** The nodes with Keys, and the keys (codes) whose going down a node's Keys took: their release goes no further either. */
    val keyed = HashSet<Int>()
    val takenKeys = HashSet<Int>()
    var rootID = 0
    var activity: AsukaActivity? = null
    private var started = false
    private val main = Handler(Looper.getMainLooper())

    /** Runs f on the main thread, later (a module's reply from any thread). */
    fun onMain(f: () -> Unit) { main.post(f) }
    private var depth = 0
    private val pendingLists = ArrayList<AsukaList>()
    private val pendingEvents = ArrayList<ByteArray>()
    private var draining = false
    /** Every Stack, to find the one back pops. */
    val stacks = LinkedHashSet<AsukaNode>()
    /** Back pops the active stack's top screen, following the predictive back gesture. */
    val back = makeBackCallback()
    val nativeFactories = HashMap<String, (AsukaNativeContext) -> AsukaNativeView>()
    /** Between AnimateBegin and AnimateEnd. */
    var animation: AsukaAnimation? = null
    /** Views coming in with a transition in the batch being applied. */
    val entering = ArrayList<Pair<AsukaNode, AsukaAnimation>>()
    /** Views going out with a transition, still shown. */
    val exiting = HashSet<View>()
    /** Style.Match: the views with keys, where they were as an animated batch began destroying, and those going and coming in it. */
    val matchViews = HashMap<String, AsukaNode>()
    var matchFrames: HashMap<String, android.graphics.Rect>? = null
    val matchOut = HashMap<String, Pair<View, android.graphics.Rect>>()
    val matchIn = ArrayList<AsukaNode>()
    var matchAnimation: AsukaAnimation? = null
    var framesApplied = false
    /** Native modules' methods, by module and method name (ui.Call). */
    val modules = HashMap<String, Map<String, AsukaModuleMethod>>()
    var builtinModules = false

    /** Registers a native view kind for ui.Native. */
    fun registerNative(type: String, make: (AsukaNativeContext) -> AsukaNativeView) {
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
                    enter { AsukaNative.event(e) }
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
        enter { AsukaNative.event(bytes) }
    }

    fun updateBack() {
        back.isEnabled = activeStack() != null || nodes[rootID]?.intercepts == true
    }

    /** Sheets' back callbacks, on while their OnBack intercepts. */
    val sheetBack = HashMap<Int, androidx.activity.OnBackPressedCallback>()

    private var framesOn = false
    private val frame = object : android.view.Choreographer.FrameCallback {
        override fun doFrame(nanos: Long) {
            if (!framesOn) return
            android.view.Choreographer.getInstance().postFrameCallback(this)
            send(AsukaEvents.frame(rootID, nanos / 1e9))
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
    /**
     * A scroll view shows its content only inside its frame, as UIKit's does:
     * Asuka's containers let their children draw outside them, and its parent
     * would show the content it scrolled out of sight (the next page of a
     * carousel). Clipped to its background's outline (its corners) if any.
     */
    private fun clipsToFrame(v: View) {
        v.outlineProvider = object : android.view.ViewOutlineProvider() {
            override fun getOutline(view: View, outline: android.graphics.Outline) {
                view.background?.getOutline(outline)
                if (outline.isEmpty) outline.setRect(0, 0, view.width, view.height)
            }
        }
        v.clipToOutline = true
    }

    private fun reportScrolls(v: View, id: Int) {
        val r = AsukaScrollReporter(this, id, v)
        v.setOnScrollChangeListener { _, x, y, _, _ ->
            r.scrolled(x, y)
            nodes[id]?.let { cull(it) }
        }
    }

    private val pendingCulls = LinkedHashSet<AsukaNode>()

    /**
     * Takes the rows of a Scroll's content more than a screen outside what
     * it shows out of the view tree, as RecyclerView does its items, and
     * puts them back as they come near: each one drawn (recorded) and
     * measured is work for every frame it changes in, on screen or not. A
     * row out of the tree keeps its frame and props. While TalkBack
     * explores by touch it takes nothing out: TalkBack moves through every
     * row.
     */
    fun cull(scroll: AsukaNode) {
        val sv = scroll.view
        val content = scroll.children.firstOrNull() ?: return
        val c = content.view as? AsukaContainer ?: return
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

    fun attach(activity: AsukaActivity) {
        this.activity = activity
        density = activity.resources.displayMetrics.density
        if (!started) {
            started = true
            AsukaAutomation.start(this)
            watchRegion()
            AsukaNative.load(app)
            // A share sheet's process: the Go code's root is the app's sheet.
            if (activity is AsukaShareActivity) runCatching { AsukaNative.setenv("ASUKA_SHARE_SHEET", "1") }
            registerNatives()
            activity.sendEnvironment(force = true)
            enter { AsukaNative.start(this) }
            AsukaSplash.started()
            val urls = ArrayList(pendingURLs)
            pendingURLs.clear()
            for (u in urls) openURL(u)
            val shares = ArrayList(pendingShares)
            pendingShares.clear()
            for (s in shares) shared(s)
        } else {
            activity.sendEnvironment(force = true)
            enter { AsukaNative.attach() }
        }
    }

    /**
     * Starts the core without an Activity, if it hasn't started: the
     * system runs a task (asuka/periodic) with the app in the background.
     * The root's views are made; an Activity that comes takes them (attach).
     */
    fun startHeadless() {
        if (started) return
        started = true
        watchRegion()
        AsukaNative.load(app)
        registerNatives()
        enter { AsukaNative.start(this) }
        AsukaSplash.started()
    }

    private val pendingURLs = ArrayList<String>()
    private val pendingShares = ArrayList<ByteArray>()

    /** What was last shared (the Shared event's payload), and its files: a share sheet's, to hand to the app or remove. */
    var lastShared: ByteArray? = null
    val lastSharedFiles = ArrayList<String>()

    /** Another app shared with this one (ACTION_SEND, ACTION_SEND_MULTIPLE: receive): ui.OnShare gets the items, once the core started. */
    fun shared(items: ByteArray) {
        lastShared = items
        if (!started) { pendingShares.add(items); return }
        send(AsukaEvents.shared(rootID, items))
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
        send(AsukaEvents.incomingURL(rootID, url))
    }

    fun detach(activity: AsukaActivity) {
        if (this.activity === activity) {
            nodes[rootID]?.view?.let { (it.parent as? ViewGroup)?.removeView(it) }
            this.activity = null
        }
    }

    fun lifecycle(state: Int) {
        if (started) send(AsukaEvents.lifecycle(rootID, state.toLong()))
    }

    /** The webAuth module's sign-in under way. */
    var auth: AsukaAuth? = null

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
        val config = localized(schemed(system))
        val night = isNight(config)
        themeBase = app.createConfigurationContext(config)
        themed = theme(themeBase)
        regionChanged() // the system's languages, and the default locale it just set
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
                (n.view as? AsukaStack)?.restyle()
                (n.view as? AsukaTabs)?.restyle()
                (n.view as? AsukaSegmented)?.restyle()
                (n.view as? AsukaDatePicker)?.refresh() // says the date in the language
            }
        }
    }

    private var night = (app.resources.configuration.uiMode and android.content.res.Configuration.UI_MODE_NIGHT_MASK) ==
        android.content.res.Configuration.UI_MODE_NIGHT_YES
    private var fontScale = app.resources.configuration.fontScale
    private var densityDpi = app.resources.configuration.densityDpi

    private val rebuilt = setOf(AsukaKind.TEXT, AsukaKind.BUTTON, AsukaKind.TEXT_FIELD, AsukaKind.TOGGLE, AsukaKind.SLIDER, AsukaKind.ACTIVITY)

    /** Makes n's view again with the current theme, replaying its props and keeping the user's state. */
    private fun rebuild(n: AsukaNode) {
        val old = n.view
        val v = makeView(n.id, n.kind)
        n.view = v
        settingProp = true
        for ((prop, value) in n.props) {
            when (value) {
                is Boolean -> propBool(n.id, prop, value)
                is Long -> if (prop == AsukaProp.BACKGROUND || prop == AsukaProp.COLOR || prop == AsukaProp.BORDER_COLOR) propColor(n.id, prop, value) else propInt(n.id, prop, value)
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
                n.pasteTypes?.let { setPasteTypes(n, ByteBuffer.wrap(it)) }
                // A rich field's spans, as they are now (the user's edits too).
                if ((old as? AsukaEditText)?.rich == true) {
                    old.text?.let { n.richBytes = encodeRich(it) }
                    n.richBytes?.let { setRich(n, ByteBuffer.wrap(it).order(java.nio.ByteOrder.LITTLE_ENDIAN)) }
                    cur.setSelection(old.selectionStart.coerceAtLeast(0), old.selectionEnd.coerceAtLeast(0))
                }
            }
            old is android.widget.CompoundButton && cur is android.widget.CompoundButton -> cur.isChecked = old.isChecked
            old is Slider && cur is Slider -> { cur.valueFrom = old.valueFrom; cur.valueTo = old.valueTo; cur.value = old.value }
        }
        settingProp = false
        val now = n.view // replaying a button's variant replaced it again
        restyle(n)
        n.basePadding = null // the new view's own
        applyContentInset(n)
        val lp = old.layoutParams
        val parent = old.parent as? ViewGroup
        if (parent == null) {
            // A Scroll's row out of sight: it keeps its frame to come back with.
            if (lp != null) now.layoutParams = lp
            now.measure(View.MeasureSpec.makeMeasureSpec(old.width, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(old.height, View.MeasureSpec.EXACTLY))
            now.layout(old.left, old.top, old.right, old.bottom)
            return
        }
        val i = parent.indexOfChild(old)
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
        asukaDecode(buf, this)
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
        main.post { enter { AsukaNative.runPosted() } }
    }

    // Ops

    override fun create(id: Int, kind: Int) {
        val view = makeView(id, kind)
        val n = AsukaNode(id, kind, view)
        if (kind == AsukaKind.LIST) n.list = (view.tag as AsukaList)
        if (kind == AsukaKind.TEXT_FIELD) n.textStyle = AsukaTextStyle.BODY
        if (kind == AsukaKind.STACK) stacks.add(n)
        if (kind == AsukaKind.POPOVER) n.popover = AsukaPopover(this, n)
        if (kind == AsukaKind.ACCESSORY) n.accessory = AsukaAccessory(this, n)
        nodes[id] = n
        if (kind == AsukaKind.TEXT || kind == AsukaKind.TEXT_FIELD) applyColor(n)
    }

    private fun makeView(id: Int, kind: Int): View {
        val ctx = themed
        return when (kind) {
            AsukaKind.VIEW -> AsukaContainer(ctx)
            // The platform's TextView: AppCompat's (Material's) posts a message
            // to load the theme's font for each one made, and needs nothing
            // else here; the Material text appearance is applied all the same.
            AsukaKind.TEXT -> TextView(ctx).apply { applyTextStyle(this, 0); textAlignment = View.TEXT_ALIGNMENT_VIEW_START }
            AsukaKind.BUTTON -> makeButton(id, AsukaButtonVariant.PLAIN)
            AsukaKind.TEXT_FIELD -> makeTextField(id)
            AsukaKind.TOGGLE -> MaterialSwitch(ctx).apply {
                setOnCheckedChangeListener { _, on -> if (!settingProp) emit(id, AsukaEventKind.TOGGLED) { AsukaEvents.toggled(id, on) } }
            }
            AsukaKind.SLIDER -> Slider(ctx).apply {
                addOnChangeListener { _, v, fromUser -> if (fromUser) emit(id, AsukaEventKind.VALUE_CHANGED) { AsukaEvents.valueChanged(id, v.toDouble()) } }
            }
            AsukaKind.WEB_VIEW -> AsukaWebView(this, id)
            AsukaKind.SHAPE -> AsukaShape(this)
            AsukaKind.VIDEO -> AsukaVideo(this, id)
            AsukaKind.SCROLL -> NestedScrollView(ctx).apply { isFillViewport = false; clipsToFrame(this); reportScrolls(this, id) }
            AsukaKind.LIST -> AsukaList(id, this).frame
            AsukaKind.ACTIVITY -> CircularProgressIndicator(ctx).apply { isIndeterminate = true }
            AsukaKind.SYMBOL -> AsukaSymbol(this)
            AsukaKind.SEGMENTED -> AsukaSegmented(this, id)
            AsukaKind.WHEEL -> AsukaWheel(this, id)
            AsukaKind.STEPPER -> AsukaStepper(this, id)
            AsukaKind.COLOR_PICKER -> AsukaColorPicker(this, id)
            AsukaKind.PROGRESS -> makeProgress()
            AsukaKind.PAGE_CONTROL -> AsukaPageDots(this, id)
            AsukaKind.ACCESSORY -> AsukaContainer(ctx)
            AsukaKind.CANVAS -> AsukaCanvas(this)
            AsukaKind.SPLIT -> AsukaSplit(this, id)
            AsukaKind.SHADER -> AsukaShaderView(this)
            AsukaKind.DATE_PICKER -> AsukaDatePicker(this, id)
            AsukaKind.STACK -> AsukaStack(ctx, id, this)
            AsukaKind.TABS -> AsukaTabs(ctx, id, this)
            AsukaKind.SCREEN, AsukaKind.SHEET, AsukaKind.POPOVER -> AsukaScreen(ctx, id, this)
            AsukaKind.ALERT -> View(ctx)
            AsukaKind.IMAGE -> androidx.appcompat.widget.AppCompatImageView(ctx).apply { scaleType = android.widget.ImageView.ScaleType.FIT_CENTER }
            AsukaKind.NATIVE -> AsukaNativeHost(ctx)
            AsukaKind.RICH_TEXT -> AsukaRichText(this, id)
            AsukaKind.SELECTABLE -> AsukaSelectionArea(this)
            else -> error("asuka: the Android host can't show ${AsukaKind.name(kind)} ($kind) yet")
        }.also { if (rtl) it.layoutDirection = View.LAYOUT_DIRECTION_RTL }
    }

    var settingProp = false

    private fun makeButton(id: Int, variant: Int): MaterialButton {
        val overlay = when (variant) {
            // Android has no Liquid Glass: Material's tonal and filled buttons.
            AsukaButtonVariant.FILLED, AsukaButtonVariant.GLASS_PROMINENT -> R.style.ThemeOverlay_Asuka_FilledButton
            AsukaButtonVariant.TINTED, AsukaButtonVariant.GRAY, AsukaButtonVariant.GLASS -> R.style.ThemeOverlay_Asuka_TonalButton
            AsukaButtonVariant.BORDERED -> R.style.ThemeOverlay_Asuka_OutlinedButton
            else -> R.style.ThemeOverlay_Asuka_TextButton
        }
        return MaterialButton(ContextThemeWrapper(themed, overlay)).apply {
            isAllCaps = false
            setOnClickListener { clicked(id) }
        }
    }

    private var heightCheckQueued = false

    private fun makeTextField(id: Int) = AsukaEditText(themed, this, id).apply {
        isSingleLine = true
        textAlignment = View.TEXT_ALIGNMENT_VIEW_START
        imeOptions = EditorInfo.IME_ACTION_DONE
        addTextChangedListener(object : TextWatcher {
            override fun beforeTextChanged(s: CharSequence?, start: Int, count: Int, after: Int) {
                if (!settingProp) richWillChange(s, start, count, after)
            }
            override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {
                changeStart = start
                changeCount = count
                richDidChange(start, count)
            }
            override fun afterTextChanged(s: Editable?) {
                if (!settingProp) {
                    val t = s?.toString() ?: ""
                    emit(id, AsukaEventKind.TEXT_CHANGED) { AsukaEvents.textChanged(id, t) }
                    nodes[id]?.let { n ->
                        if (rich && !history.restoring) {
                            typedRich(n, changeStart, changeCount)
                            richChanged(n)
                        }
                    }
                    // A paste's lines aren't laid out yet when it's reported: report
                    // it again once they are, so a multiline field grows to fit.
                    if (maxLines > 1 && !heightCheckQueued) {
                        heightCheckQueued = true
                        post {
                            heightCheckQueued = false
                            val now = text?.toString() ?: ""
                            emit(id, AsukaEventKind.TEXT_CHANGED) { AsukaEvents.textChanged(id, now) }
                        }
                    }
                }
            }
        })
        setOnEditorActionListener { _, action, _ ->
            // A multiline field's return is a new line: TextView asks this
            // listener first about Enter's key (IME_NULL), a hardware
            // keyboard's or a soft one's, so it mustn't take it.
            if (action == EditorInfo.IME_NULL && nodes[id]?.props?.get(AsukaProp.TextField.MULTILINE) == true) return@setOnEditorActionListener false
            if (action == EditorInfo.IME_NULL || action >= EditorInfo.IME_ACTION_GO) {
                emit(id, AsukaEventKind.SUBMIT) { AsukaEvents.submit(id) }
                true
            } else false
        }
        setOnFocusChangeListener { v, focused ->
            // Its Accessory, if it has one, over the keyboard while it's focused.
            (nodes[id]?.props?.get(AsukaProp.TextField.ACCESSORY) as? Long)?.let { a ->
                nodes[a.toInt()]?.accessory?.let { if (focused) it.show(v) else it.hide() }
            }
            if (focused) emit(id, AsukaEventKind.FOCUS) { AsukaEvents.focus(id) }
            else emit(id, AsukaEventKind.BLUR) { AsukaEvents.blur(id) }
        }
    }

    /**
     * Makes a Scroll scroll horizontally, or vertically again: Android's are
     * two views (HorizontalScrollView, NestedScrollView), so the view is
     * replaced, its content moved to the new one.
     */
    fun setHorizontal(n: AsukaNode, horizontal: Boolean) {
        val old = n.view as? android.widget.FrameLayout ?: return
        if ((old is HorizontalScrollView) == horizontal) return
        val v: android.widget.FrameLayout = if (horizontal) AsukaHorizontalScroll(themed).apply { isFillViewport = false }
            else NestedScrollView(themed).apply { isFillViewport = false }
        v.layoutDirection = directionOf(n)
        clipsToFrame(v)
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
        AsukaPager.apply(this, n)
    }

    /** Makes a Toggle a checkbox: Material's, in place of its switch. */
    fun makeCheckbox(n: AsukaNode) {
        val old = n.view as? MaterialSwitch ?: return
        val id = n.id
        val c = com.google.android.material.checkbox.MaterialCheckBox(themed).apply {
            isChecked = old.isChecked
            isEnabled = old.isEnabled
            layoutDirection = directionOf(n)
            setOnCheckedChangeListener { _, on -> if (!settingProp) emit(id, AsukaEventKind.TOGGLED) { AsukaEvents.toggled(id, on) } }
        }
        val parent = old.parent as? ViewGroup
        if (parent != null) {
            val i = parent.indexOfChild(old)
            val lp = old.layoutParams
            parent.removeViewAt(i)
            parent.addView(c, i, lp)
        }
        n.view = c
        restyle(n)
    }

    /** Replaces a button with one of another variant (a style is fixed when a view is made). */
    fun replaceButton(n: AsukaNode, variant: Int) {
        val old = n.view as MaterialButton
        val b = makeButton(n.id, variant)
        b.layoutDirection = directionOf(n)
        b.text = old.text
        b.isEnabled = old.isEnabled
        (n.props[AsukaProp.Button.ICON] as? String)?.let { setButtonIcon(n, b, it) }
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
    override fun setEdges(id: Int, prop: Int, top: Float, right: Float, bottom: Float, left: Float) {
        val n = nodes[id] ?: return
        if (prop == AsukaProp.CONTENT_INSET) {
            n.contentInset = floatArrayOf(top, right, bottom, left)
            applyContentInset(n)
        }
    }
    override fun setEnum(id: Int, prop: Int, v: Int) = propEnum(id, prop, v)
    override fun setBytes(id: Int, prop: Int, v: ByteBuffer) {
        val n = nodes[id] ?: return
        if (n.kind == AsukaKind.SHAPE && setShapeProp(n, prop, v)) return
        if (n.kind == AsukaKind.WHEEL && setWheelProp(n, prop, v)) return
        if (n.kind == AsukaKind.WEB_VIEW && prop == AsukaProp.WebView.BLOCK) { (n.view as AsukaWebView).setBlock(v); return }
        if (n.kind == AsukaKind.TEXT_FIELD && prop == AsukaProp.TextField.SELECTION) { setFieldSelection(n, v); return }
        if (n.kind == AsukaKind.TEXT_FIELD && prop == AsukaProp.TextField.RICH) {
            val a = ByteArray(v.remaining())
            v.duplicate().get(a)
            n.richBytes = a
            setRich(n, v)
            return
        }
        if (n.kind == AsukaKind.TEXT_FIELD && prop == AsukaProp.TextField.PASTE_TYPES) {
            val a = ByteArray(v.remaining())
            v.duplicate().get(a)
            n.pasteTypes = a
            setPasteTypes(n, v)
            return
        }
        if (prop == AsukaProp.KEYS) {
            setKeys(n, v)
            return
        }
        if (prop == AsukaProp.MENU) {
            n.actions = decodeActions(v)
            wirePresses(n)
            return
        }
        if (n.kind == AsukaKind.VIEW && prop == AsukaProp.View.DROP_ACCEPT) {
            setDropAccept(n, v)
            return
        }
        if (n.kind == AsukaKind.VIEW && prop == AsukaProp.View.DRAG_ITEMS) {
            setDragItems(n, v)
            return
        }
        if (prop == AsukaProp.A11Y_ACTIONS) {
            setA11yActions(n, v)
            return
        }
        if (prop == AsukaProp.SWIPE_ACTIONS) {
            n.swipeActions = decodeActions(v)
            swipeAccessibility(n)
            return
        }
        if (prop == AsukaProp.LEADING_SWIPE_ACTIONS) {
            n.leadingSwipeActions = decodeActions(v)
            swipeAccessibility(n)
            return
        }
        when (prop) {
            AsukaProp.TRANSFORM -> { setTransform(n, v); return }
            AsukaProp.TRANSITION -> { n.transition = AsukaTransition(v); return }
            AsukaProp.SHADOW -> { setShadow(n, v); return }
            AsukaProp.GRADIENT -> { setGradient(n, v); return }
        }
        setNavProp(n, prop, v)
    }

    override fun destroy(id: Int) {
        val n = nodes.remove(id) ?: return
        keyed.remove(id)
        (n.view as? AsukaVideo)?.release()
        stacks.remove(n)
        if (n.underDying) return // its views leave with the animating screen or sheet
        if ((n.kind == AsukaKind.SCREEN || n.kind == AsukaKind.SHEET || n.kind == AsukaKind.POPOVER || n.kind == AsukaKind.ALERT) && destroyNav(n)) return
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
        if (n.parent?.view is AsukaSplit) return // a SplitView's detail: it fills what the drawer leaves
        // A Scroll, or a row of its content, moved: what it takes out is redone.
        if (n.kind == AsukaKind.SCROLL) pendingCulls.add(n)
        n.parent?.parent?.let { if (it.kind == AsukaKind.SCROLL) pendingCulls.add(it) }
        val d = density
        val l = Math.round(x * d)
        val t = Math.round(y * d)
        val r = Math.round((x + width) * d)
        val b = Math.round((y + height) * d)
        n.targetX = l
        n.targetY = t
        val v = n.outer
        if (v is AsukaCell) {
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
        if (v is AsukaContainer) {
            v.frameWidth = r - l
            v.frameHeight = b - t
        }
        val lp = v.layoutParams
        if (lp is AsukaParams) {
            lp.x = l
            lp.y = t
            lp.w = r - l
            lp.h = b - t
        }
        v.measure(View.MeasureSpec.makeMeasureSpec(r - l, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(b - t, View.MeasureSpec.EXACTLY))
        v.layout(l, t, r, b)
        val parent = v.parent
        // An inline component: its size is the core's, its place the text's.
        if (parent is AsukaRichText) parent.placeInlines()
        if (parent is AsukaHorizontalScroll) parent.keep()
        if (parent is NestedScrollView || parent is HorizontalScrollView || parent is FrameLayout) {
            // A scroll view measures its content in specs of its own, which
            // the view would answer from its measure cache, in its old size.
            v.forceLayout()
            (parent as View).requestLayout()
        }
    }

    override fun listen(id: Int, event: Int) {
        val n = nodes[id] ?: return
        n.setListening(event, true)
        if (event == AsukaEventKind.PRESS || event == AsukaEventKind.LONG_PRESS || event == AsukaEventKind.PRESSED || event == AsukaEventKind.HOVER) wirePresses(n)
        if (n.kind == AsukaKind.VIEW && (event == AsukaEventKind.PAN || event == AsukaEventKind.PINCH || event == AsukaEventKind.SWIPE || event == AsukaEventKind.ROTATE)) listenGesture(n, event)
    }

    override fun unlisten(id: Int, event: Int) {
        val n = nodes[id] ?: return
        n.setListening(event, false)
        if (event == AsukaEventKind.PRESSED || event == AsukaEventKind.HOVER) wirePresses(n)
    }

    override fun command(id: Int, command: Int, args: ByteBuffer) {
        val n = nodes[id] ?: return
        when (command) {
            AsukaCommand.FOCUS -> {
                n.view.requestFocus()
                val imm = app.getSystemService(android.view.inputmethod.InputMethodManager::class.java)
                imm?.showSoftInput(n.view, 0)
            }
            AsukaCommand.BLUR -> {
                n.view.clearFocus()
                val imm = app.getSystemService(android.view.inputmethod.InputMethodManager::class.java)
                imm?.hideSoftInputFromWindow(n.view.windowToken, 0)
            }
            AsukaCommand.LIST_UPDATE -> {
                val count = args.int
                val ids = LongArray(count) { args.long }
                val ns = if (args.remaining() >= 4) args.int else 0
                val starts = IntArray(ns)
                val sections = LongArray(ns)
                for (s in 0 until ns) {
                    starts[s] = args.int
                    sections[s] = args.long
                }
                val l = n.list ?: return
                l.update(ids, starts, sections)
                if (l !in pendingLists) pendingLists.add(l)
            }
            AsukaCommand.FORMAT -> if (args.remaining() >= 2) format(n, args.get().toInt() and 0xff, args.get().toInt() and 0xff)
            AsukaCommand.SET_LINK -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                setLink(n, String(b, Charsets.UTF_8))
            }
            AsukaCommand.INSERT_TEXT -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                insertFieldText(n, String(b, Charsets.UTF_8))
            }
            AsukaCommand.PRESENT -> present(n)
            AsukaCommand.HAPTIC -> if (args.remaining() > 0) haptic(args.get().toInt() and 0xff)
            AsukaCommand.ANIMATE_BEGIN -> beginAnimation(args)
            AsukaCommand.ANIMATE_END -> animation = null
            AsukaCommand.ACCENT -> {
                if (args.remaining() < 9) return
                val c = args.long
                val flags = args.get().toInt()
                setAccent(if (c != 0L) color(c) else null, flags and 1 != 0)
            }
            AsukaCommand.COLOR_SCHEME -> if (args.remaining() > 0) setColorScheme(args.get().toInt())
            AsukaCommand.LOCALE -> {
                if (args.remaining() < 1) return
                val flags = args.get().toInt()
                val b = ByteArray(args.remaining())
                args.get(b)
                setLocale(java.util.Locale.forLanguageTag(String(b, Charsets.UTF_8)), flags and 1 != 0)
            }
            AsukaCommand.SAVE_LOCALE -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                saveLocale(String(b, Charsets.UTF_8))
            }
            AsukaCommand.CALL -> call(id, args)
            AsukaCommand.FRAMES -> setFrames(args.remaining() > 0 && args.get().toInt() == 1)
            AsukaCommand.PLAY -> (n.view as? AsukaVideo)?.player?.play()
            AsukaCommand.PAUSE -> (n.view as? AsukaVideo)?.player?.pause()
            AsukaCommand.SEEK -> if (args.remaining() >= 8) (n.view as? AsukaVideo)?.player?.seekTo((args.double * 1000).toLong())
            AsukaCommand.WEB_NAVIGATE -> if (args.remaining() > 0) (n.view as? AsukaWebView)?.navigate(args.get().toInt())
            AsukaCommand.EVAL -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                (n.view as? AsukaWebView)?.evaluateJavascript(String(b, Charsets.UTF_8), null)
            }
            AsukaCommand.ANNOUNCE -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                @Suppress("DEPRECATION") // its replacements (live regions, pane titles) describe views, not events
                activity?.window?.decorView?.announceForAccessibility(String(b, Charsets.UTF_8))
            }
            AsukaCommand.BACK_EXIT -> backExit()
            AsukaCommand.REPLACE -> (n.view as? AsukaStack)?.instant = true
            AsukaCommand.A11Y_FOCUS -> n.view?.let { v ->
                // After this batch's frames, so TalkBack finds the view where it shows.
                v.post { if (v.isAttachedToWindow) v.performAccessibilityAction(android.view.accessibility.AccessibilityNodeInfo.ACTION_ACCESSIBILITY_FOCUS, null) }
            }
            AsukaCommand.SHARE_CLOSE -> (activity as? AsukaShareActivity)?.close(if (args.hasRemaining()) args.get().toInt() else 0)
            AsukaCommand.COPY -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                (app.getSystemService(Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager)
                    .setPrimaryClip(android.content.ClipData.newPlainText(null, String(b, Charsets.UTF_8)))
            }
            AsukaCommand.SHARE -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                val send = android.content.Intent(android.content.Intent.ACTION_SEND).apply {
                    type = "text/plain"
                    putExtra(android.content.Intent.EXTRA_TEXT, String(b, Charsets.UTF_8))
                }
                val i = android.content.Intent.createChooser(send, null)
                runCatching { (activity ?: app).startActivity(if (activity == null) i.addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK) else i) }
            }
            AsukaCommand.SCROLL_TO -> {
                if (args.remaining() < 5) return
                val i = args.int
                n.list?.scroll(i, args.get().toInt() != 0)
            }
            AsukaCommand.SCROLL_TO_OFFSET -> {
                if (args.remaining() < 9) return
                val x = (args.float * density).coerceAtMost(1e9f).toInt()
                val y = (args.float * density).coerceAtMost(1e9f).toInt()
                val animated = args.get().toInt() != 0
                // Each clamps to what it can scroll.
                when (val v = n.view) {
                    is NestedScrollView -> if (animated) v.smoothScrollTo(x, y) else v.scrollTo(x, y)
                    is HorizontalScrollView -> if (animated) v.smoothScrollTo(x, y) else v.scrollTo(x, y)
                    else -> n.list?.let { l ->
                        val v = l.recycler
                        l.applyPending()
                        // By the difference: a list knows where it is, not where an offset is.
                        val dx = x - v.computeHorizontalScrollOffset()
                        val dy = y - v.computeVerticalScrollOffset()
                        if (animated) v.smoothScrollBy(dx, dy) else v.scrollBy(dx, dy)
                        l.scrolledTo()
                    }
                }
            }
            AsukaCommand.OPEN_URL -> {
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

    fun makeCell(cell: AsukaCell, list: Int): Int {
        nextCellID++
        val id = (0x80000000L or nextCellID.toLong()).toInt()
        cell.asukaID = id
        if (rtl) cell.layoutDirection = View.LAYOUT_DIRECTION_RTL
        nodes[id] = AsukaNode(id, 0, cell).also { it.cellOf = list }
        return id
    }
}
