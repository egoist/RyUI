package dev.asuka

import android.content.ComponentCallbacks2
import android.content.res.Configuration
import android.content.res.Resources
import android.os.Bundle
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import androidx.activity.enableEdgeToEdge
import androidx.appcompat.app.AppCompatActivity
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsAnimationCompat
import androidx.core.view.WindowInsetsCompat

/**
 * Shows the core's root view edge to edge and reports its environment:
 * size, insets (system bars, cutout, keyboard), density, font scale and
 * dark mode. The views belong to the process, so a recreated Activity
 * takes over the existing tree.
 */
open class AsukaActivity : AppCompatActivity() {
    protected lateinit var frame: FrameLayout
    protected lateinit var runtime: AsukaRuntime
    private var safe = IntArray(4)
    private var keyboard = 0
    /** The keyboard's height over the window, as it animates, in px. */
    val keyboardPx get() = keyboard
    private var last: AsukaEnvironment? = null
    // The app's language and direction, where they aren't the Activity's
    // configuration's: what it makes (dialogs, Material's pickers) is in them.
    private var shownResources: Resources? = null
    private var shownTheme: Resources.Theme? = null

    override fun getResources(): Resources = shownResources ?: super.getResources()
    override fun getTheme(): Resources.Theme = shownTheme ?: super.getTheme()

    /** The language the app shows changed, or this Activity's configuration did. */
    fun localeChanged() {
        shownResources = null
        shownTheme = null
        val shown = runtime.shownLocale ?: return
        val own = super.getResources().configuration
        val config = runtime.localized(runtime.schemed(own))
        // en shown on an en-US system is what the Activity has already.
        val same = own.locales[0].let { it.language == shown.language && it.script == shown.script }
        if (!same || config.layoutDirection != own.layoutDirection) {
            val r = createConfigurationContext(config).resources
            shownResources = r
            shownTheme = r.newTheme().apply { applyStyle(themeRes, true) }
        }
        val direction = if (runtime.rtl) View.LAYOUT_DIRECTION_RTL else View.LAYOUT_DIRECTION_LTR
        if (::frame.isInitialized) frame.layoutDirection = direction
        window?.decorView?.layoutDirection = direction
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        runtime = AsukaRuntime.get(this)
        AsukaSplash.theme(this) // asuka.json's splash: shown, then the app's theme
        edgeToEdge()
        super.onCreate(savedInstanceState)
        AsukaDisplay.apply(this, runtime.isNight(resources.configuration)) // asuka/screen's, if it set any
        frame = FrameLayout(this)
        localeChanged()
        AsukaSplash.install(frame) // asuka.json's splash, until the first frame
        setContentView(content(frame))
        onBackPressedDispatcher.addCallback(this, runtime.back)

        ViewCompat.setOnApplyWindowInsetsListener(frame) { _, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
            safe = safeInsets(intArrayOf(bars.top, bars.right, bars.bottom, bars.left))
            keyboard = insets.getInsets(WindowInsetsCompat.Type.ime()).bottom
            sendEnvironment()
            runtime.insetsChanged()
            WindowInsetsCompat.CONSUMED
        }
        // Follow the keyboard frame by frame as it animates.
        ViewCompat.setWindowInsetsAnimationCallback(frame, object : WindowInsetsAnimationCompat.Callback(DISPATCH_MODE_CONTINUE_ON_SUBTREE) {
            override fun onProgress(insets: WindowInsetsCompat, running: MutableList<WindowInsetsAnimationCompat>): WindowInsetsCompat {
                keyboard = insets.getInsets(WindowInsetsCompat.Type.ime()).bottom
                sendEnvironment()
                runtime.reportScreens()
                return insets
            }
        })
        frame.addOnLayoutChangeListener { _, l, t, r, b, ol, ot, or_, ob ->
            if (r - l != or_ - ol || b - t != ob - ot) sendEnvironment()
        }
        frame.post { runtime.attach(this) }
        // Animations turned off (or on) in the system's settings: less motion.
        contentResolver.registerContentObserver(
            android.provider.Settings.Global.getUriFor(android.provider.Settings.Global.ANIMATOR_DURATION_SCALE), false, motion)
        // TalkBack turned on or off: the environment's ScreenReader.
        a11y.addTouchExplorationStateChangeListener(screenReader)
        // Opened with a URL (a scheme's, an app link).
        if (savedInstanceState == null) intent?.let { incoming(it) }
    }

    /** The Activity's theme, for what it makes in another language. */
    protected open val themeRes: Int get() = R.style.Theme_Asuka

    /** The view the Activity shows, which holds the root's frame: the frame itself. */
    protected open fun content(frame: FrameLayout): View = frame

    /** The root's safe area insets (top, right, bottom, left), from the window's. */
    protected open fun safeInsets(bars: IntArray): IntArray = bars

    /** The window's background, behind the root: the theme's surface. */
    protected open fun windowBackground(): android.graphics.drawable.Drawable = android.graphics.drawable.ColorDrawable(
        com.google.android.material.color.MaterialColors.getColor(runtime.themed, com.google.android.material.R.attr.colorSurface, 0))

    /**
     * What the app was opened with: a URL (a scheme's, an app link), what
     * another app shared (ACTION_SEND, ACTION_SEND_MULTIPLE), or what the
     * share sheet hands over (AsukaShareActivity: OpenApp).
     */
    private fun incoming(intent: android.content.Intent) {
        if (intent.action == android.content.Intent.ACTION_SEND || intent.action == android.content.Intent.ACTION_SEND_MULTIPLE) {
            runtime.receive(intent)
            return
        }
        if (intent.action == AsukaShareActivity.OPEN_APP) {
            intent.getByteArrayExtra(AsukaShareActivity.ITEMS)?.let { runtime.shared(it) }
            return
        }
        // A notification's tap or action that opens the app (asuka/notify).
        AsukaNotifyHub.opened(this, intent)
        // A push the user tapped (asuka/push): its data's url, if any.
        val pushed = AsukaPushHub.opened(intent)
        (intent.data?.toString() ?: pushed)?.let { runtime.openURL(it) }
    }

    fun mount(root: View) {
        (root.parent as? ViewGroup)?.removeView(root)
        frame.addView(root, FrameLayout.LayoutParams(FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT))
    }

    fun sendEnvironment(force: Boolean = false) {
        if (!::runtime.isInitialized || frame.width == 0) return
        val d = resources.displayMetrics.density
        val c = resources.configuration
        val e = AsukaEnvironment(
            width = frame.width / d, height = frame.height / d,
            safeTop = safe[0] / d, safeRight = safe[1] / d, safeBottom = safe[2] / d, safeLeft = safe[3] / d,
            keyboard = keyboard / d,
            scale = d,
            fontScale = c.fontScale,
            dark = runtime.isNight(c),
            reduceMotion = !android.animation.ValueAnimator.areAnimatorsEnabled(),
            screenReader = a11y.isTouchExplorationEnabled,
            widthClass = widthClass(frame.width / d), heightClass = heightClass(frame.height / d),
            locale = runtime.region.locale,
            languages = runtime.region.languages,
            firstWeekday = runtime.region.firstWeekday,
            hourCycle = runtime.region.hourCycle,
        )
        if (!force && e == last) return
        last = e
        if (runtime.activity === this) runtime.send(AsukaEvents.environment(runtime.rootID, e))
    }

    override fun onConfigurationChanged(newConfig: Configuration) {
        // Its own resources again, for what the Activity updates with it.
        shownResources = null
        shownTheme = null
        super.onConfigurationChanged(newConfig)
        configured(newConfig)
    }

    /** The app's color scheme changed (ui.SetColorScheme). */
    fun schemeChanged() = configured(resources.configuration)

    private fun configured(config: Configuration) {
        edgeToEdge() // status bar icons follow dark mode
        AsukaDisplay.bars(this, runtime.isNight(config)) // unless asuka/screen set them
        runtime.configurationChanged(config)
        AsukaSystemAppearance.configurationChanged()
        localeChanged()
        window.setBackgroundDrawable(windowBackground())
        frame.post { sendEnvironment() }
    }

    /** Edge to edge, the system bars' icons light or dark as the app is (its color scheme, or the system's). */
    protected open fun edgeToEdge() {
        val dark = { r: android.content.res.Resources -> runtime.isNight(r.configuration) }
        val clear = android.graphics.Color.TRANSPARENT
        // androidx.activity's default scrims, for the navigation bar's buttons.
        enableEdgeToEdge(
            androidx.activity.SystemBarStyle.auto(clear, clear, dark),
            androidx.activity.SystemBarStyle.auto(android.graphics.Color.argb(0xe6, 0xff, 0xff, 0xff), android.graphics.Color.argb(0x80, 0x1b, 0x1b, 0x1b), dark),
        )
    }

    // singleTask: a URL that opens the running app comes here.
    override fun onNewIntent(intent: android.content.Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        incoming(intent)
    }

    override fun onStart() {
        super.onStart()
        // Another instance had the views (one launched on top of this
        // one, then closed): take them back.
        if (runtime.activity != null && runtime.activity !== this) frame.post { runtime.attach(this) }
        runtime.regionChanged() // what changed while it was away
        runtime.lifecycle(AsukaLifecycle.FOREGROUND)
        AsukaDisplay.watch(this, true)
    }

    override fun onPause() {
        super.onPause()
        runtime.paused()
    }

    override fun onResume() {
        super.onResume()
        runtime.resumed()
    }

    override fun onStop() {
        super.onStop()
        AsukaDisplay.watch(this, false)
        runtime.lifecycle(AsukaLifecycle.BACKGROUND)
    }

    override fun onTrimMemory(level: Int) {
        super.onTrimMemory(level)
        if (level >= ComponentCallbacks2.TRIM_MEMORY_RUNNING_LOW) runtime.lifecycle(AsukaLifecycle.MEMORY_WARNING)
    }

    private val motion = object : android.database.ContentObserver(android.os.Handler(android.os.Looper.getMainLooper())) {
        override fun onChange(selfChange: Boolean) {
            sendEnvironment()
            runtime.reportScreens()
        }
    }

    private val a11y by lazy { getSystemService(ACCESSIBILITY_SERVICE) as android.view.accessibility.AccessibilityManager }
    private val screenReader = android.view.accessibility.AccessibilityManager.TouchExplorationStateChangeListener {
        sendEnvironment()
        runtime.reportScreens()
    }

    override fun onDestroy() {
        contentResolver.unregisterContentObserver(motion)
        a11y.removeTouchExplorationStateChangeListener(screenReader)
        runtime.detach(this)
        super.onDestroy()
    }
}
