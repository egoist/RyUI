package dev.ryui

import android.app.Activity
import android.content.pm.ActivityInfo
import android.os.Build
import android.view.WindowManager
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat
import androidx.core.view.WindowInsetsControllerCompat

/**
 * ryui/screen's settings: the app's, kept here so that an Activity made
 * again (a configuration change) takes them, as RyUIActivity applies them.
 */
object RyUIDisplay {
    /** screen.Orientation; 0: the manifest's. */
    var lock = 0
    /** The manifest's orientation, before the first lock. */
    private var original: Int? = null
    var style = 0 // auto, light, dark
    var hideStatus = false
    var hideNavigation = false
    var secure = false
    val screenshots = ArrayList<(Result<ByteArray>) -> Unit>()
    private var callback: Any? = null // Activity.ScreenCaptureCallback, on Android 14 and later

    /** Applies the settings to the Activity: its orientation, bars and capture. */
    fun apply(act: RyUIActivity, night: Boolean) {
        if (original == null) original = act.requestedOrientation
        act.requestedOrientation = when (lock) {
            1 -> ActivityInfo.SCREEN_ORIENTATION_USER_PORTRAIT
            2 -> ActivityInfo.SCREEN_ORIENTATION_PORTRAIT
            3 -> ActivityInfo.SCREEN_ORIENTATION_USER_LANDSCAPE
            4 -> ActivityInfo.SCREEN_ORIENTATION_REVERSE_LANDSCAPE // its bottom on the left: UIKit's landscapeLeft
            5 -> ActivityInfo.SCREEN_ORIENTATION_LANDSCAPE
            else -> original ?: ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
        }
        bars(act, night)
        if (secure) act.window.addFlags(WindowManager.LayoutParams.FLAG_SECURE)
        else act.window.clearFlags(WindowManager.LayoutParams.FLAG_SECURE)
    }

    /** The bars' icons and whether they show; after edge to edge, which sets the icons by the scheme. */
    fun bars(act: Activity, night: Boolean) {
        val c = WindowCompat.getInsetsController(act.window, act.window.decorView)
        val light = when (style) { 1 -> false; 2 -> true; else -> !night } // light bars: dark icons
        c.isAppearanceLightStatusBars = light
        c.isAppearanceLightNavigationBars = light
        c.systemBarsBehavior = WindowInsetsControllerCompat.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        if (hideStatus) c.hide(WindowInsetsCompat.Type.statusBars()) else c.show(WindowInsetsCompat.Type.statusBars())
        if (hideNavigation) c.hide(WindowInsetsCompat.Type.navigationBars()) else c.show(WindowInsetsCompat.Type.navigationBars())
    }

    /** Follows screenshots while the Activity shows and someone waits for one (Android 14 and later). */
    fun watch(act: Activity, on: Boolean) {
        if (Build.VERSION.SDK_INT < 34) return
        try {
            if (on && callback == null && screenshots.isNotEmpty()) {
                val cb = Activity.ScreenCaptureCallback {
                    val ws = ArrayList(screenshots)
                    screenshots.clear()
                    for (w in ws) w(Result.success(ByteArray(0)))
                }
                act.registerScreenCaptureCallback(act.mainExecutor, cb)
                callback = cb
            } else if (!on && callback != null) {
                act.unregisterScreenCaptureCallback(callback as Activity.ScreenCaptureCallback)
                callback = null
            }
        } catch (_: SecurityException) {
            // No DETECT_SCREEN_CAPTURE: the app doesn't import ryui/screen's package path.
        }
    }
}

/** ryui/screen's module: "lock", "bars", "preventCapture", "screenshot", "stopScreenshots" (see screen.go). */
fun RyUIRuntime.screenMethods(): Map<String, RyUIModuleMethod> {
    fun applied(reply: (Result<ByteArray>) -> Unit) {
        activity?.let { RyUIDisplay.apply(it, isNight(it.resources.configuration)) }
        reply(Result.success(ByteArray(0)))
    }
    return mapOf(
        "lock" to { args, reply ->
            RyUIDisplay.lock = args.firstOrNull()?.toInt() ?: 0
            applied(reply)
        },
        "bars" to { args, reply ->
            RyUIDisplay.style = args.firstOrNull()?.toInt() ?: 0
            val flags = args.getOrNull(1)?.toInt() ?: 0
            RyUIDisplay.hideStatus = flags and 1 != 0
            RyUIDisplay.hideNavigation = flags and 2 != 0
            applied(reply)
        },
        "preventCapture" to { args, reply ->
            RyUIDisplay.secure = args.firstOrNull()?.toInt() == 1
            applied(reply)
        },
        "screenshot" to { _, reply ->
            RyUIDisplay.screenshots.add(reply)
            activity?.let { RyUIDisplay.watch(it, true) }
        },
        "stopScreenshots" to { _, reply ->
            val ws = ArrayList(RyUIDisplay.screenshots)
            RyUIDisplay.screenshots.clear()
            for (w in ws) w(Result.failure(Exception("stopped")))
            activity?.let { RyUIDisplay.watch(it, false) }
            reply(Result.success(ByteArray(0)))
        },
    )
}
