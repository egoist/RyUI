package dev.nui

import android.annotation.SuppressLint
import android.graphics.Color
import android.webkit.JavascriptInterface
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import java.io.File

/**
 * A WebView: Android's, reporting the pages it loads and what its pages
 * post (window.nui.postMessage), and running the core's script.
 */
@SuppressLint("SetJavaScriptEnabled", "ViewConstructor")
class NuiWebView(private val rt: NuiRuntime, private val nuiID: Int) : WebView(rt.themed) {
    init {
        settings.javaScriptEnabled = true
        settings.domStorageEnabled = true
        setBackgroundColor(Color.TRANSPARENT) // no white flash before a dark page draws
        // window.nui, before the page's own scripts: the same call as on iOS.
        addJavascriptInterface(Bridge(), "nui")
        webViewClient = object : WebViewClient() {
            override fun onPageFinished(view: WebView, url: String) {
                rt.emit(nuiID, NuiEventKind.NAVIGATED) { NuiEvents.navigated(nuiID, url) }
            }

            override fun onReceivedError(view: WebView, req: WebResourceRequest, err: WebResourceError) {
                if (!req.isForMainFrame) return
                val m = err.description.toString()
                rt.emit(nuiID, NuiEventKind.FAILED) { NuiEvents.failed(nuiID, m) }
            }
        }
        if (0 != (rt.app.applicationInfo.flags and android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE)) {
            setWebContentsDebuggingEnabled(true) // chrome://inspect
        }
    }

    private inner class Bridge {
        @JavascriptInterface
        fun postMessage(m: String) {
            // On the bridge's thread: to the main one.
            rt.onMain { rt.emit(nuiID, NuiEventKind.MESSAGE) { NuiEvents.message(nuiID, m) } }
        }
    }

    /** Loads source: an http(s) URL, a file (an absolute path, file://), or one of the app's assets. */
    fun load(source: String) {
        if (source.isEmpty()) return
        if (source.startsWith("http://") || source.startsWith("https://")) { loadUrl(source); return }
        settings.allowFileAccess = true // the app's own files
        val url = when {
            source.startsWith("file://") -> source
            source.startsWith("/") -> "file://$source"
            // In development `nui dev` pushes the assets into the app's files.
            File(rt.app.filesDir, "nui/assets/$source").exists() -> "file://" + File(rt.app.filesDir, "nui/assets/$source").path
            else -> "file:///android_asset/$source"
        }
        loadUrl(url)
    }
}
