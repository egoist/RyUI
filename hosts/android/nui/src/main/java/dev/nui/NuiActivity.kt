package dev.nui

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
open class NuiActivity : AppCompatActivity() {
    private lateinit var frame: FrameLayout
    private lateinit var runtime: NuiRuntime
    private var safe = IntArray(4)
    private var keyboard = 0
    /** The keyboard's height over the window, as it animates, in px. */
    val keyboardPx get() = keyboard
    private var last: NuiEnvironment? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        enableEdgeToEdge()
        super.onCreate(savedInstanceState)
        frame = FrameLayout(this)
        setContentView(frame)
        runtime = NuiRuntime.get(this)
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
    }

    fun mount(root: View) {
        (root.parent as? ViewGroup)?.removeView(root)
        frame.addView(root, FrameLayout.LayoutParams(FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT))
    }

    fun sendEnvironment(force: Boolean = false) {
        if (!::runtime.isInitialized || frame.width == 0) return
        val d = resources.displayMetrics.density
        val c = resources.configuration
        val e = NuiEnvironment(
            width = frame.width / d, height = frame.height / d,
            safeTop = safe[0] / d, safeRight = safe[1] / d, safeBottom = safe[2] / d, safeLeft = safe[3] / d,
            keyboard = keyboard / d,
            scale = d,
            fontScale = c.fontScale,
            dark = (c.uiMode and Configuration.UI_MODE_NIGHT_MASK) == Configuration.UI_MODE_NIGHT_YES,
            locale = c.locales[0].toLanguageTag(),
        )
        if (!force && e == last) return
        last = e
        if (runtime.activity === this) runtime.send(NuiEvents.environment(runtime.rootID, e))
    }

    override fun onConfigurationChanged(newConfig: Configuration) {
        super.onConfigurationChanged(newConfig)
        enableEdgeToEdge() // status bar icons follow dark mode
        runtime.configurationChanged(newConfig)
        window.setBackgroundDrawable(android.graphics.drawable.ColorDrawable(
            com.google.android.material.color.MaterialColors.getColor(runtime.themed, com.google.android.material.R.attr.colorSurface, 0)))
        frame.post { sendEnvironment() }
    }

    override fun onStart() {
        super.onStart()
        runtime.lifecycle(NuiLifecycle.FOREGROUND)
    }

    override fun onStop() {
        super.onStop()
        runtime.lifecycle(NuiLifecycle.BACKGROUND)
    }

    override fun onTrimMemory(level: Int) {
        super.onTrimMemory(level)
        if (level >= ComponentCallbacks2.TRIM_MEMORY_RUNNING_LOW) runtime.lifecycle(NuiLifecycle.MEMORY_WARNING)
    }

    override fun onDestroy() {
        runtime.detach(this)
        super.onDestroy()
    }
}
