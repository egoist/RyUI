package dev.ryui

import android.annotation.SuppressLint
import android.graphics.Bitmap
import android.graphics.Color
import android.os.Message
import android.webkit.CookieManager
import android.webkit.JavascriptInterface
import android.webkit.URLUtil
import android.webkit.WebChromeClient
import android.webkit.WebResourceError
import android.webkit.WebResourceRequest
import android.webkit.WebResourceResponse
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.webkit.WebViewCompat
import androidx.webkit.WebViewFeature
import org.json.JSONObject
import java.io.ByteArrayInputStream
import java.io.File
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * A WebView: Android's, reporting the pages it loads, what its pages post
 * (window.ryui.postMessage), its state (URL, title, progress, history),
 * the files it leads to and the windows it would open, blocking the
 * requests its Block names, and running the core's script.
 */
@SuppressLint("SetJavaScriptEnabled", "ViewConstructor")
class RyUIWebView(private val rt: RyUIRuntime, private val ryuiID: Int) : WebView(rt.themed) {
    /** Block's patterns: hosts (and their subdomains), and parts of URLs. Read on the WebView's IO thread. */
    @Volatile private var blockHosts: List<String> = emptyList()
    @Volatile private var blockParts: List<String> = emptyList()
    @Volatile var reportRequests = false
    private var preload: String? = null
    private var title = ""
    private var loading = false
    private var stateQueued = false

    init {
        settings.javaScriptEnabled = true
        settings.domStorageEnabled = true
        settings.mediaPlaybackRequiresUserGesture = false
        setBackgroundColor(Color.TRANSPARENT) // no white flash before a dark page draws
        CookieManager.getInstance().setAcceptThirdPartyCookies(this, true)
        // window.ryui, before the page's own scripts: the same call as on iOS.
        addJavascriptInterface(Bridge(), "ryui")
        webViewClient = object : WebViewClient() {
            override fun onPageStarted(view: WebView, url: String, favicon: Bitmap?) {
                loading = true
                // Without document-start scripts: as early as there is.
                if (preload != null && !WebViewFeature.isFeatureSupported(WebViewFeature.DOCUMENT_START_SCRIPT)) {
                    view.evaluateJavascript(preload!!, null)
                }
                state()
            }

            override fun onPageFinished(view: WebView, url: String) {
                loading = false
                state()
                rt.emit(ryuiID, RyUIEventKind.NAVIGATED) { RyUIEvents.navigated(ryuiID, url) }
            }

            override fun doUpdateVisitedHistory(view: WebView, url: String, isReload: Boolean) = state()

            override fun onReceivedError(view: WebView, req: WebResourceRequest, err: WebResourceError) {
                if (!req.isForMainFrame) return
                val m = err.description.toString()
                rt.emit(ryuiID, RyUIEventKind.FAILED) { RyUIEvents.failed(ryuiID, m) }
            }

            override fun shouldInterceptRequest(view: WebView, req: WebResourceRequest): WebResourceResponse? {
                val url = req.url.toString()
                if (blocked(req.url.host ?: "", url)) {
                    return WebResourceResponse("text/plain", "utf-8", 403, "Blocked", emptyMap(), ByteArrayInputStream(ByteArray(0)))
                }
                if (reportRequests) rt.onMain { rt.emit(ryuiID, RyUIEventKind.REQUEST) { RyUIEvents.request(ryuiID, url) } }
                return null
            }
        }
        webChromeClient = object : WebChromeClient() {
            override fun onReceivedTitle(view: WebView, t: String?) { title = t ?: ""; state() }
            override fun onProgressChanged(view: WebView, progress: Int) = state()

            // A page opening a window: a WebView of its own, only to learn
            // where it would go; then that's reported (or loaded here).
            override fun onCreateWindow(view: WebView, dialog: Boolean, user: Boolean, msg: Message): Boolean {
                val probe = WebView(view.context)
                probe.webViewClient = object : WebViewClient() {
                    override fun shouldOverrideUrlLoading(v: WebView, r: WebResourceRequest): Boolean {
                        opened(r.url.toString())
                        v.destroy()
                        return true
                    }
                }
                (msg.obj as WebViewTransport).webView = probe
                msg.sendToTarget()
                return true
            }
        }
        setDownloadListener { url, _, disposition, mime, length ->
            val name = URLUtil.guessFileName(url, disposition, mime)
            rt.emit(ryuiID, RyUIEventKind.DOWNLOAD) {
                val w = RyUIWriter()
                w.string(url); w.string(mime ?: ""); w.string(name); w.u64(if (length > 0) length else -1)
                RyUIEvents.download(ryuiID, w.bytes())
            }
        }
        if (0 != (rt.app.applicationInfo.flags and android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE)) {
            setWebContentsDebuggingEnabled(true) // chrome://inspect
        }
    }

    private fun opened(url: String) {
        val n = rt.nodes[ryuiID]
        if (n != null && n.listens(RyUIEventKind.NEW_WINDOW)) {
            rt.emit(ryuiID, RyUIEventKind.NEW_WINDOW) { RyUIEvents.newWindow(ryuiID, url) }
        } else {
            loadUrl(url)
        }
    }

    /** Whether a request is one Block names. */
    private fun blocked(host: String, url: String): Boolean {
        for (h in blockHosts) if (host == h || host.endsWith(".$h")) return true
        for (p in blockParts) if (url.contains(p)) return true
        return false
    }

    /** Reports the page's state, once for the changes of a turn of the main loop. */
    private fun state() {
        val n = rt.nodes[ryuiID] ?: return
        if (!n.listens(RyUIEventKind.WEB_STATE) || stateQueued) return
        stateQueued = true
        post {
            stateQueued = false
            val w = RyUIWriter()
            w.string(url ?: "")
            w.string(title)
            w.u8((if (loading) 1 else 0) or (if (canGoBack()) 2 else 0) or (if (canGoForward()) 4 else 0))
            w.f32(if (loading) progress / 100f else 1f)
            rt.emit(ryuiID, RyUIEventKind.WEB_STATE) { RyUIEvents.webState(ryuiID, w.bytes()) }
        }
    }

    fun setPreload(script: String) {
        preload = script
        if (WebViewFeature.isFeatureSupported(WebViewFeature.DOCUMENT_START_SCRIPT)) {
            WebViewCompat.addDocumentStartJavaScript(this, script, setOf("*"))
        }
    }

    fun setUserAgent(ua: String) {
        settings.userAgentString = ua.ifEmpty { null }
    }

    /** Block's patterns: u32 count, then u32-length strings. */
    fun setBlock(b: ByteBuffer) {
        val r = b.duplicate().order(ByteOrder.LITTLE_ENDIAN)
        val hosts = ArrayList<String>()
        val parts = ArrayList<String>()
        if (r.remaining() >= 4) repeat(r.int) {
            val s = ByteArray(r.int).also { r.get(it) }.toString(Charsets.UTF_8)
            if (s.contains('/')) parts.add(s) else hosts.add(s.lowercase())
        }
        blockHosts = hosts
        blockParts = parts
    }

    /** WebNavigate: 0 back, 1 forward, 2 reload, 3 stop. */
    fun navigate(to: Int) {
        when (to) {
            0 -> if (canGoBack()) goBack()
            1 -> if (canGoForward()) goForward()
            2 -> reload()
            3 -> stopLoading()
        }
    }

    /** Runs script, answering with its value as JSON (a promise's, awaited), or what it threw. */
    fun evalResult(script: String, reply: (Result<ByteArray>) -> Unit) {
        val id = nextEval++
        evals[id] = reply
        val wrapped = "(function(){var s=" + JSONObject.quote(script) + ";" +
            "Promise.resolve().then(function(){return (0,eval)(s)}).then(" +
            "function(v){ryui.evalDone($id,JSON.stringify(v===undefined?null:v))}," +
            "function(e){ryui.evalFail($id,e&&e.stack?String(e.stack):String(e))})})()"
        evaluateJavascript(wrapped, null)
    }

    private val evals = HashMap<Int, (Result<ByteArray>) -> Unit>()
    private var nextEval = 1

    private inner class Bridge {
        @JavascriptInterface
        fun postMessage(m: String) {
            // On the bridge's thread: to the main one.
            rt.onMain { rt.emit(ryuiID, RyUIEventKind.MESSAGE) { RyUIEvents.message(ryuiID, m) } }
        }

        @JavascriptInterface
        fun evalDone(id: Int, json: String) { rt.onMain { evals.remove(id)?.invoke(Result.success(json.toByteArray())) } }

        @JavascriptInterface
        fun evalFail(id: Int, message: String) { rt.onMain { evals.remove(id)?.invoke(Result.failure(Exception(message))) } }
    }

    /** Loads source: an http(s) URL, a file (an absolute path, file://), or one of the app's assets. */
    fun load(source: String) {
        if (source.isEmpty()) return
        if (source.startsWith("http://") || source.startsWith("https://")) { loadUrl(source); return }
        settings.allowFileAccess = true // the app's own files
        val url = when {
            source.startsWith("file://") -> source
            source.startsWith("/") -> "file://$source"
            // In development `ryui dev` pushes the assets into the app's files.
            File(rt.app.filesDir, "ryui/assets/$source").exists() -> "file://" + File(rt.app.filesDir, "ryui/assets/$source").path
            else -> "file:///android_asset/$source"
        }
        loadUrl(url)
    }
}

/**
 * The "web" module: "eval" (args: u32 a WebView's node id, then the
 * script) answers with the script's value as JSON; "cookies" (args: a
 * URL) with the cookies the WebViews have for it, as a Cookie header.
 */
fun RyUIRuntime.webMethods(): Map<String, RyUIModuleMethod> = mapOf(
    "eval" to { args, reply ->
        val id = if (args.size >= 4) ByteBuffer.wrap(args, 0, 4).order(ByteOrder.LITTLE_ENDIAN).int else -1
        val web = nodes[id]?.view as? RyUIWebView
        if (web == null) reply(Result.failure(Exception("not a WebView")))
        else web.evalResult(String(args, 4, args.size - 4, Charsets.UTF_8), reply)
    },
    "cookies" to { args, reply ->
        val c = CookieManager.getInstance().getCookie(String(args, Charsets.UTF_8)) ?: ""
        reply(Result.success(c.toByteArray()))
    },
)
