package dev.nui

import android.annotation.SuppressLint
import android.app.Application
import android.content.Context
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
class NuiRuntime private constructor(val app: Application) : NuiOpHandler {
    companion object {
        @SuppressLint("StaticFieldLeak")
        private var instance: NuiRuntime? = null

        fun get(context: Context): NuiRuntime =
            instance ?: NuiRuntime(context.applicationContext as Application).also { instance = it }
    }

    /** Views are made with the app's Material theme, not an Activity's. */
    var themed: Context = ContextThemeWrapper(app, R.style.Theme_Nui)
        private set
    /** The context the theme is made on: the app's, in the current configuration. */
    private var themeBase: Context = app.createConfigurationContext(app.resources.configuration)
    /** The app's accent color (nui.json's), the seed of its color scheme; null: Material's. */
    var accent: Int? = null
        private set
    /** Whether the colors are the wallpaper's (nui.json's android.dynamicColor). */
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
        val t = ContextThemeWrapper(base, R.style.Theme_Nui)
        if (!com.google.android.material.color.DynamicColors.isDynamicColorAvailable()) return t
        val seed = accent
        return when {
            wallpaperColors -> com.google.android.material.color.DynamicColors.wrapContextIfAvailable(t)
            seed != null -> com.google.android.material.color.DynamicColors.wrapContextIfAvailable(t,
                com.google.android.material.color.DynamicColorsOptions.Builder().setContentBasedSource(seed).build()).also {
                // The scheme's context resolves the colors but loses the
                // theme's other attributes (a toolbar's homeAsUpIndicator):
                // fill them in, not over the colors.
                if (it !== t) it.theme.applyStyle(R.style.Theme_Nui, false)
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
        themeBase = app.createConfigurationContext(app.resources.configuration)
        themed = theme(themeBase)
        themeVersion++
        restyleAll(true)
    }
    val nodes = HashMap<Int, NuiNode>()
    var rootID = 0
    var activity: NuiActivity? = null
    private var started = false
    private val main = Handler(Looper.getMainLooper())

    /** Runs f on the main thread, later (a module's reply from any thread). */
    fun onMain(f: () -> Unit) { main.post(f) }
    private var depth = 0
    private val pendingLists = ArrayList<NuiList>()
    private val pendingEvents = ArrayList<ByteArray>()
    private var draining = false
    /** Every Stack, to find the one back pops. */
    val stacks = LinkedHashSet<NuiNode>()
    /** Back pops the active stack's top screen, following the predictive back gesture. */
    val back = makeBackCallback()
    val nativeFactories = HashMap<String, (NuiNativeContext) -> NuiNativeView>()
    /** Between AnimateBegin and AnimateEnd. */
    var animation: NuiAnimation? = null
    /** Views coming in with a transition in the batch being applied. */
    val entering = ArrayList<Pair<NuiNode, NuiAnimation>>()
    /** Views going out with a transition, still shown. */
    val exiting = HashSet<View>()
    /** Native modules' methods, by module and method name (ui.Call). */
    val modules = HashMap<String, Map<String, NuiModuleMethod>>()
    var builtinModules = false

    /** Registers a native view kind for ui.Native. */
    fun registerNative(type: String, make: (NuiNativeContext) -> NuiNativeView) {
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
            while (pendingLists.isNotEmpty() || pendingEvents.isNotEmpty()) {
                if (pendingLists.isNotEmpty()) {
                    val lists = ArrayList(pendingLists)
                    pendingLists.clear()
                    for (l in lists) l.applyPending()
                } else {
                    val e = pendingEvents.removeAt(0)
                    enter { NuiNative.event(e) }
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
        enter { NuiNative.event(bytes) }
    }

    fun updateBack() {
        back.isEnabled = activeStack() != null
    }

    /** Reports a Scroll's offset, in points, as Scrolled (when the core listens). */
    private fun reportScrolls(v: View, id: Int) {
        val r = NuiScrollReporter(this, id, v)
        v.setOnScrollChangeListener { _, x, y, _, _ -> r.scrolled(x, y) }
    }

    fun emit(id: Int, event: Int, make: () -> ByteArray) {
        val n = nodes[id] ?: return
        if (n.listens(event)) send(make())
    }

    fun attach(activity: NuiActivity) {
        this.activity = activity
        density = activity.resources.displayMetrics.density
        if (!started) {
            started = true
            NuiAutomation.start(this)
            NuiNative.load(app)
            registerNatives()
            activity.sendEnvironment(force = true)
            enter { NuiNative.start(this) }
            val urls = ArrayList(pendingURLs)
            pendingURLs.clear()
            for (u in urls) openURL(u)
        } else {
            activity.sendEnvironment(force = true)
            enter { NuiNative.attach() }
        }
    }

    private val pendingURLs = ArrayList<String>()

    /** The app was opened with url (its scheme, an app link): ui.OnOpenURL gets it, once the core started. */
    fun openURL(url: String) {
        if (!started) { pendingURLs.add(url); return }
        send(NuiEvents.incomingURL(rootID, url))
    }

    fun detach(activity: NuiActivity) {
        if (this.activity === activity) {
            nodes[rootID]?.view?.let { (it.parent as? ViewGroup)?.removeView(it) }
            this.activity = null
        }
    }

    fun lifecycle(state: Int) {
        if (started) send(NuiEvents.lifecycle(rootID, state.toLong()))
    }

    /** The configuration changed (dark mode, font scale): restyle what the theme decided. */
    fun configurationChanged(config: android.content.res.Configuration) {
        val night = (config.uiMode and android.content.res.Configuration.UI_MODE_NIGHT_MASK) ==
            android.content.res.Configuration.UI_MODE_NIGHT_YES
        themeBase = app.createConfigurationContext(config)
        themed = theme(themeBase)
        density = config.densityDpi / 160f
        // Views resolve theme colors and text sizes when they're made:
        // after a dark mode, font scale or density change, make the ones
        // that show text or Material colors again.
        val changed = night != this.night || config.fontScale != fontScale || config.densityDpi != densityDpi
        this.night = night
        fontScale = config.fontScale
        densityDpi = config.densityDpi
        if (changed) themeVersion++
        restyleAll(changed)
    }

    /** Restyles every view; with changed, makes those that show text or Material colors again. */
    private fun restyleAll(changed: Boolean) {
        activity?.window?.setBackgroundDrawable(android.graphics.drawable.ColorDrawable(
            MaterialColors.getColor(themed, com.google.android.material.R.attr.colorSurface, android.graphics.Color.WHITE)))
        for (n in ArrayList(nodes.values)) {
            if (changed && n.kind in rebuilt) rebuild(n) else restyle(n)
            if (changed) {
                n.list?.restyle()
                (n.view as? NuiStack)?.restyle()
                (n.view as? NuiTabs)?.restyle()
            }
        }
    }

    private var night = (app.resources.configuration.uiMode and android.content.res.Configuration.UI_MODE_NIGHT_MASK) ==
        android.content.res.Configuration.UI_MODE_NIGHT_YES
    private var fontScale = app.resources.configuration.fontScale
    private var densityDpi = app.resources.configuration.densityDpi

    private val rebuilt = setOf(NuiKind.TEXT, NuiKind.BUTTON, NuiKind.TEXT_FIELD, NuiKind.TOGGLE, NuiKind.SLIDER, NuiKind.ACTIVITY)

    /** Makes n's view again with the current theme, replaying its props and keeping the user's state. */
    private fun rebuild(n: NuiNode) {
        val old = n.view
        val v = makeView(n.id, n.kind)
        n.view = v
        settingProp = true
        for ((prop, value) in n.props) {
            when (value) {
                is Boolean -> propBool(n.id, prop, value)
                is Long -> if (prop == NuiProp.BACKGROUND || prop == NuiProp.COLOR || prop == NuiProp.BORDER_COLOR) propColor(n.id, prop, value) else propInt(n.id, prop, value)
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
        nuiDecode(buf, this)
        runEnters()
    }

    @Keep
    fun measure(id: Int, w: Float, h: Float, aw: Float, ah: Float): Long {
        val (mw, mh) = measureNode(nodes[id] ?: return 0, w, h, aw, ah)
        return (java.lang.Float.floatToRawIntBits(mw).toLong() shl 32) or
            (java.lang.Float.floatToRawIntBits(mh).toLong() and 0xffffffffL)
    }

    @Keep
    fun post() {
        main.post { enter { NuiNative.runPosted() } }
    }

    // Ops

    override fun create(id: Int, kind: Int) {
        val view = makeView(id, kind)
        val n = NuiNode(id, kind, view)
        if (kind == NuiKind.LIST) n.list = (view.tag as NuiList)
        if (kind == NuiKind.TEXT_FIELD) n.textStyle = NuiTextStyle.BODY
        if (kind == NuiKind.STACK) stacks.add(n)
        nodes[id] = n
        if (kind == NuiKind.TEXT || kind == NuiKind.TEXT_FIELD) applyColor(n)
    }

    private fun makeView(id: Int, kind: Int): View {
        val ctx = themed
        return when (kind) {
            NuiKind.VIEW -> NuiContainer(ctx)
            NuiKind.TEXT -> MaterialTextView(ctx).apply { applyTextStyle(this, 0) }
            NuiKind.BUTTON -> makeButton(id, NuiButtonVariant.PLAIN)
            NuiKind.TEXT_FIELD -> makeTextField(id)
            NuiKind.TOGGLE -> MaterialSwitch(ctx).apply {
                setOnCheckedChangeListener { _, on -> if (!settingProp) emit(id, NuiEventKind.TOGGLED) { NuiEvents.toggled(id, on) } }
            }
            NuiKind.SLIDER -> Slider(ctx).apply {
                addOnChangeListener { _, v, fromUser -> if (fromUser) emit(id, NuiEventKind.VALUE_CHANGED) { NuiEvents.valueChanged(id, v.toDouble()) } }
            }
            NuiKind.SCROLL -> NestedScrollView(ctx).apply { isFillViewport = false; reportScrolls(this, id) }
            NuiKind.LIST -> NuiList(id, this).recycler
            NuiKind.ACTIVITY -> CircularProgressIndicator(ctx).apply { isIndeterminate = true }
            NuiKind.SYMBOL -> NuiSymbol(this)
            NuiKind.SEGMENTED -> NuiSegmented(this, id)
            NuiKind.PROGRESS -> makeProgress()
            NuiKind.DATE_PICKER -> NuiDatePicker(this, id)
            NuiKind.STACK -> NuiStack(ctx, id, this)
            NuiKind.TABS -> NuiTabs(ctx, id, this)
            NuiKind.SCREEN, NuiKind.SHEET -> NuiScreen(ctx, id, this)
            NuiKind.ALERT -> View(ctx)
            NuiKind.IMAGE -> androidx.appcompat.widget.AppCompatImageView(ctx).apply { scaleType = android.widget.ImageView.ScaleType.FIT_CENTER }
            NuiKind.NATIVE -> NuiNativeHost(ctx)
            NuiKind.RICH_TEXT -> NuiRichText(this, id)
            NuiKind.SELECTABLE -> NuiSelectionArea(this)
            else -> error("nui: the Android host can't show ${NuiKind.name(kind)} ($kind) yet")
        }
    }

    var settingProp = false

    private fun makeButton(id: Int, variant: Int): MaterialButton {
        val overlay = when (variant) {
            NuiButtonVariant.FILLED -> R.style.ThemeOverlay_Nui_FilledButton
            NuiButtonVariant.TINTED, NuiButtonVariant.GRAY -> R.style.ThemeOverlay_Nui_TonalButton
            NuiButtonVariant.BORDERED -> R.style.ThemeOverlay_Nui_OutlinedButton
            else -> R.style.ThemeOverlay_Nui_TextButton
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
                    emit(id, NuiEventKind.TEXT_CHANGED) { NuiEvents.textChanged(id, t) }
                }
            }
        })
        setOnEditorActionListener { _, action, _ ->
            if (action == EditorInfo.IME_NULL || action >= EditorInfo.IME_ACTION_GO) {
                emit(id, NuiEventKind.SUBMIT) { NuiEvents.submit(id) }
                true
            } else false
        }
        setOnFocusChangeListener { _, focused ->
            if (focused) emit(id, NuiEventKind.FOCUS) { NuiEvents.focus(id) }
            else emit(id, NuiEventKind.BLUR) { NuiEvents.blur(id) }
        }
    }

    /**
     * Makes a Scroll scroll horizontally, or vertically again: Android's are
     * two views (HorizontalScrollView, NestedScrollView), so the view is
     * replaced, its content moved to the new one.
     */
    fun setHorizontal(n: NuiNode, horizontal: Boolean) {
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
    fun replaceButton(n: NuiNode, variant: Int) {
        val old = n.view as MaterialButton
        val b = makeButton(n.id, variant)
        b.text = old.text
        b.isEnabled = old.isEnabled
        (n.props[NuiProp.Button.ICON] as? String)?.let { setButtonIcon(n, b, it) }
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
        if (prop == NuiProp.MENU) {
            n.actions = decodeActions(v)
            wirePresses(n)
            return
        }
        if (prop == NuiProp.SWIPE_ACTIONS) {
            n.swipeActions = decodeActions(v)
            swipeAccessibility(n)
            return
        }
        when (prop) {
            NuiProp.TRANSFORM -> { setTransform(n, v); return }
            NuiProp.TRANSITION -> { n.transition = NuiTransition(v); return }
            NuiProp.SHADOW -> { setShadow(n, v); return }
            NuiProp.GRADIENT -> { setGradient(n, v); return }
        }
        setNavProp(n, prop, v)
    }

    override fun destroy(id: Int) {
        val n = nodes.remove(id) ?: return
        stacks.remove(n)
        if (n.underDying) return // its views leave with the animating screen or sheet
        if ((n.kind == NuiKind.SCREEN || n.kind == NuiKind.SHEET || n.kind == NuiKind.ALERT) && destroyNav(n)) return
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
    }

    override fun remove(parent: Int, child: Int) {
        val p = nodes[parent] ?: return
        val c = nodes[child] ?: return
        p.remove(c)
    }

    override fun setFrame(id: Int, x: Float, y: Float, width: Float, height: Float) {
        val n = nodes[id] ?: return
        val d = density
        val l = Math.round(x * d)
        val t = Math.round(y * d)
        val r = Math.round((x + width) * d)
        val b = Math.round((y + height) * d)
        val v = n.outer
        if (v is NuiCell) {
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
        if (v is NuiContainer) {
            v.frameWidth = r - l
            v.frameHeight = b - t
        }
        val lp = v.layoutParams
        if (lp is NuiParams) {
            lp.x = l
            lp.y = t
            lp.w = r - l
            lp.h = b - t
        }
        v.measure(View.MeasureSpec.makeMeasureSpec(r - l, View.MeasureSpec.EXACTLY), View.MeasureSpec.makeMeasureSpec(b - t, View.MeasureSpec.EXACTLY))
        v.layout(l, t, r, b)
        val parent = v.parent
        // An inline component: its size is the core's, its place the text's.
        if (parent is NuiRichText) parent.placeInlines()
        if (parent is NestedScrollView || parent is HorizontalScrollView || parent is FrameLayout) {
            (parent as View).requestLayout()
        }
    }

    override fun listen(id: Int, event: Int) {
        val n = nodes[id] ?: return
        n.listens = n.listens or (1 shl event)
        if (event == NuiEventKind.PRESS || event == NuiEventKind.LONG_PRESS) wirePresses(n)
        if (n.kind == NuiKind.VIEW && (event == NuiEventKind.PAN || event == NuiEventKind.PINCH || event == NuiEventKind.SWIPE)) listenGesture(n, event)
    }

    override fun unlisten(id: Int, event: Int) {
        val n = nodes[id] ?: return
        n.listens = n.listens and (1 shl event).inv()
    }

    override fun command(id: Int, command: Int, args: ByteBuffer) {
        val n = nodes[id] ?: return
        when (command) {
            NuiCommand.FOCUS -> {
                n.view.requestFocus()
                val imm = app.getSystemService(android.view.inputmethod.InputMethodManager::class.java)
                imm?.showSoftInput(n.view, 0)
            }
            NuiCommand.BLUR -> {
                n.view.clearFocus()
                val imm = app.getSystemService(android.view.inputmethod.InputMethodManager::class.java)
                imm?.hideSoftInputFromWindow(n.view.windowToken, 0)
            }
            NuiCommand.LIST_UPDATE -> {
                val count = args.int
                val ids = LongArray(count) { args.long }
                val l = n.list ?: return
                l.update(ids)
                if (l !in pendingLists) pendingLists.add(l)
            }
            NuiCommand.PRESENT -> present(n)
            NuiCommand.HAPTIC -> if (args.remaining() > 0) haptic(args.get().toInt() and 0xff)
            NuiCommand.ANIMATE_BEGIN -> beginAnimation(args)
            NuiCommand.ANIMATE_END -> animation = null
            NuiCommand.ACCENT -> {
                if (args.remaining() < 9) return
                val c = args.long
                val flags = args.get().toInt()
                setAccent(if (c != 0L) color(c) else null, flags and 1 != 0)
            }
            NuiCommand.CALL -> call(id, args)
            NuiCommand.ANNOUNCE -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                @Suppress("DEPRECATION") // its replacements (live regions, pane titles) describe views, not events
                activity?.window?.decorView?.announceForAccessibility(String(b, Charsets.UTF_8))
            }
            NuiCommand.COPY -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                (app.getSystemService(Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager)
                    .setPrimaryClip(android.content.ClipData.newPlainText(null, String(b, Charsets.UTF_8)))
            }
            NuiCommand.SHARE -> {
                val b = ByteArray(args.remaining())
                args.get(b)
                val send = android.content.Intent(android.content.Intent.ACTION_SEND).apply {
                    type = "text/plain"
                    putExtra(android.content.Intent.EXTRA_TEXT, String(b, Charsets.UTF_8))
                }
                val i = android.content.Intent.createChooser(send, null)
                runCatching { (activity ?: app).startActivity(if (activity == null) i.addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK) else i) }
            }
            NuiCommand.SCROLL_TO -> {
                if (args.remaining() < 5) return
                val i = args.int
                n.list?.scroll(i, args.get().toInt() != 0)
            }
            NuiCommand.SCROLL_TO_OFFSET -> {
                if (args.remaining() < 9) return
                val x = (args.float * density).coerceAtMost(1e9f).toInt()
                val y = (args.float * density).coerceAtMost(1e9f).toInt()
                val animated = args.get().toInt() != 0
                // Each clamps to what it can scroll.
                when (val v = n.view) {
                    is NestedScrollView -> if (animated) v.smoothScrollTo(x, y) else v.scrollTo(x, y)
                    is HorizontalScrollView -> if (animated) v.smoothScrollTo(x, y) else v.scrollTo(x, y)
                }
            }
            NuiCommand.OPEN_URL -> {
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

    fun makeCell(cell: NuiCell): Int {
        nextCellID++
        val id = (0x80000000L or nextCellID.toLong()).toInt()
        cell.nuiID = id
        nodes[id] = NuiNode(id, 0, cell)
        return id
    }
}
