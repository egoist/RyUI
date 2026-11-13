package dev.asuka

import android.view.View
import android.view.ViewTreeObserver

/**
 * asuka.json's splash (Theme.Asuka.Splash, which the CLI writes): the
 * system's splash stays until the Activity first draws, which waits for
 * the core's first flush, or, if the app holds it (splash.Hold), for
 * "hide".
 */
object AsukaSplash {
    private var held = false
    private var ready = false
    private var waiting: View? = null

    private var on = false

    /** The app has a splash theme: the Activity takes Theme.Asuka (before its onCreate). */
    fun theme(act: AsukaActivity) {
        on = act.resources.getIdentifier("Theme.Asuka.Splash", "style", act.packageName) != 0
        if (on) act.setTheme(R.style.Theme_Asuka)
    }

    /** Holds the content's first draw until the core's first frame (or "hide"). */
    fun install(content: View) {
        if (!on || ready && !held) return
        waiting = content
        content.viewTreeObserver.addOnPreDrawListener(object : ViewTreeObserver.OnPreDrawListener {
            override fun onPreDraw(): Boolean {
                if (!ready || held) return false
                content.viewTreeObserver.removeOnPreDrawListener(this)
                waiting = null
                return true
            }
        })
    }

    /** The core started: its first frame is ready. */
    fun started() {
        ready = true
        waiting?.invalidate()
    }

    fun methods(): Map<String, AsukaModuleMethod> = mapOf(
        "hold" to { _, reply ->
            if (!ready) held = true
            reply(Result.success(ByteArray(0)))
        },
        "hide" to { _, reply ->
            held = false
            waiting?.invalidate()
            reply(Result.success(ByteArray(0)))
        },
    )
}
