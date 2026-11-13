package dev.asuka

import android.app.UiModeManager
import android.content.Context
import android.content.res.Configuration
import android.database.ContentObserver
import android.os.Build
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import androidx.core.content.ContextCompat

/**
 * asuka/appearance's module: "current" (u8 has accent, red, green, blue,
 * increase contrast, reduce transparency), and, while watching, "next"
 * and "stop". The accent is the wallpaper's color Material You takes
 * (the system's accent1 at tone 40 in light mode, 80 in dark, as the
 * system's dynamic primary); more contrast is the contrast level above
 * standard (Android 14) or high contrast text. Android has no Reduce
 * Transparency.
 */
class AsukaSystemAppearance(private val rt: AsukaRuntime) {
    private val ui = rt.app.getSystemService(Context.UI_MODE_SERVICE) as UiModeManager
    private val main = Handler(Looper.getMainLooper())
    private val highText = Settings.Secure.getUriFor("high_text_contrast_enabled")
    private val observer = object : ContentObserver(main) {
        override fun onChange(selfChange: Boolean) = watch.changed()
    }
    private var contrastListener: Any? = null // UiModeManager.ContrastChangeListener, on Android 14 and later

    private val watch: AsukaWatch = AsukaWatch(::state, {
        current = this
        rt.app.contentResolver.registerContentObserver(highText, false, observer)
        if (Build.VERSION.SDK_INT >= 34) {
            val l = UiModeManager.ContrastChangeListener { watch.changed() }
            ui.addContrastChangeListener(ContextCompat.getMainExecutor(rt.app), l)
            contrastListener = l
        }
    }, {
        rt.app.contentResolver.unregisterContentObserver(observer)
        if (Build.VERSION.SDK_INT >= 34) (contrastListener as? UiModeManager.ContrastChangeListener)?.let { ui.removeContrastChangeListener(it) }
        contrastListener = null
        if (current === this) current = null
    })

    private fun state(): ByteArray {
        // The system's own colors, whatever the app's scheme.
        val night = (rt.app.resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK) == Configuration.UI_MODE_NIGHT_YES
        val c = rt.app.getColor(if (night) android.R.color.system_accent1_200 else android.R.color.system_accent1_600)
        val contrast = if (Build.VERSION.SDK_INT >= 34 && ui.contrast > 0f) true
        else Settings.Secure.getInt(rt.app.contentResolver, "high_text_contrast_enabled", 0) == 1
        return byteArrayOf(1, (c shr 16).toByte(), (c shr 8).toByte(), c.toByte(), if (contrast) 1 else 0, 0)
    }

    fun methods() = watch.methods()

    companion object {
        private var current: AsukaSystemAppearance? = null

        /** The configuration changed (dark mode, the wallpaper's colors): the accent may have. */
        fun configurationChanged() = current?.watch?.changed()
    }
}
