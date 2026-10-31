package dev.ryui

import android.content.ComponentCallbacks2
import android.content.res.Configuration
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
open class RyUIActivity : AppCompatActivity() {
    private lateinit var frame: FrameLayout
    private lateinit var runtime: RyUIRuntime
    private var safe = IntArray(4)
    private var keyboard = 0
    /** The keyboard's height over the window, as it animates, in px. */
    val keyboardPx get() = keyboard
    private var last: RyUIEnvironment? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        runtime = RyUIRuntime.get(this)
        edgeToEdge()
        super.onCreate(savedInstanceState)
        frame = FrameLayout(this)
        setContentView(frame)
        onBackPressedDispatcher.addCallback(this, runtime.back)

        ViewCompat.setOnApplyWindowInsetsListener(frame) { _, insets ->
            val bars = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
            safe = intArrayOf(bars.top, bars.right, bars.bottom, bars.left)
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
        // Opened with a URL (a scheme's, an app link).
        if (savedInstanceState == null) intent?.let { incoming(it) }
    }

    /** What the app was opened with: a URL (a scheme's, an app link), or text another app shared (ACTION_SEND). */
    private fun incoming(intent: android.content.Intent) {
        if (intent.action == android.content.Intent.ACTION_SEND) {
            intent.getStringExtra(android.content.Intent.EXTRA_TEXT)?.let { runtime.shared(it) }
            return
        }
        intent.data?.let { runtime.openURL(it.toString()) }
    }

    fun mount(root: View) {
        (root.parent as? ViewGroup)?.removeView(root)
        frame.addView(root, FrameLayout.LayoutParams(FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT))
    }

    fun sendEnvironment(force: Boolean = false) {
        if (!::runtime.isInitialized || frame.width == 0) return
        val d = resources.displayMetrics.density
        val c = resources.configuration
        val e = RyUIEnvironment(
            width = frame.width / d, height = frame.height / d,
            safeTop = safe[0] / d, safeRight = safe[1] / d, safeBottom = safe[2] / d, safeLeft = safe[3] / d,
            keyboard = keyboard / d,
            scale = d,
            fontScale = c.fontScale,
            dark = runtime.isNight(c),
            reduceMotion = !android.animation.ValueAnimator.areAnimatorsEnabled(),
            widthClass = widthClass(frame.width / d), heightClass = heightClass(frame.height / d),
            locale = c.locales[0].toLanguageTag(),
        )
        if (!force && e == last) return
        last = e
        if (runtime.activity === this) runtime.send(RyUIEvents.environment(runtime.rootID, e))
    }

    override fun onConfigurationChanged(newConfig: Configuration) {
        super.onConfigurationChanged(newConfig)
        configured(newConfig)
    }

    /** The app's color scheme changed (ui.SetColorScheme). */
    fun schemeChanged() = configured(resources.configuration)

    private fun configured(config: Configuration) {
        edgeToEdge() // status bar icons follow dark mode
        runtime.configurationChanged(config)
        window.setBackgroundDrawable(android.graphics.drawable.ColorDrawable(
            com.google.android.material.color.MaterialColors.getColor(runtime.themed, com.google.android.material.R.attr.colorSurface, 0)))
        frame.post { sendEnvironment() }
    }

    /** Edge to edge, the system bars' icons light or dark as the app is (its color scheme, or the system's). */
    private fun edgeToEdge() {
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
        runtime.lifecycle(RyUILifecycle.FOREGROUND)
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
        runtime.lifecycle(RyUILifecycle.BACKGROUND)
    }

    override fun onTrimMemory(level: Int) {
        super.onTrimMemory(level)
        if (level >= ComponentCallbacks2.TRIM_MEMORY_RUNNING_LOW) runtime.lifecycle(RyUILifecycle.MEMORY_WARNING)
    }

    private val motion = object : android.database.ContentObserver(android.os.Handler(android.os.Looper.getMainLooper())) {
        override fun onChange(selfChange: Boolean) {
            sendEnvironment()
            runtime.reportScreens()
        }
    }

    override fun onDestroy() {
        contentResolver.unregisterContentObserver(motion)
        runtime.detach(this)
        super.onDestroy()
    }
}
