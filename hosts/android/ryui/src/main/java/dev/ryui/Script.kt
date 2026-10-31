package dev.ryui

import android.annotation.SuppressLint
import android.os.Handler
import android.os.Looper
import android.webkit.JavascriptInterface
import android.webkit.WebView
import android.webkit.WebViewClient
import org.json.JSONObject

/**
 * ryui/js's module: "eval" runs a script (args: u32 timeout ms, then the
 * script) in a WebView that's never shown, made for it and destroyed
 * after, and answers with its value as JSON: the last statement's, or
 * what its promise resolves to. A thrown error fails with its message and
 * stack.
 */
@SuppressLint("SetJavaScriptEnabled", "JavascriptInterface")
fun RyUIRuntime.scriptMethods(): Map<String, RyUIModuleMethod> = mapOf(
    "eval" to { args, reply ->
        if (args.size < 4) reply(Result.failure(Exception("bad arguments")))
        else {
            val ms = java.nio.ByteBuffer.wrap(args, 0, 4).order(java.nio.ByteOrder.LITTLE_ENDIAN).int.toLong() and 0xffffffffL
            val script = String(args, 4, args.size - 4, Charsets.UTF_8)
            val main = Handler(Looper.getMainLooper())
            val web = WebView(app)
            var done = false
            fun finish(r: Result<ByteArray>) {
                if (done) return
                done = true
                reply(r)
                // Not while its bridge's call is on the stack.
                main.post { web.destroy() }
            }
            web.settings.javaScriptEnabled = true
            web.addJavascriptInterface(object {
                // On the WebView's own thread: posted to the main one.
                @JavascriptInterface fun done(json: String) { main.post { finish(Result.success(json.toByteArray())) } }
                @JavascriptInterface fun fail(message: String) { main.post { finish(Result.failure(Exception(message))) } }
            }, "__ryuiScript")
            web.webViewClient = object : WebViewClient() {
                override fun onPageFinished(view: WebView, url: String?) {
                    if (done || view.tag == true) return
                    view.tag = true
                    // Indirect eval: the global scope, and the last statement's value.
                    val wrapped = "(function(){var s=" + JSONObject.quote(script) + ";" +
                        "Promise.resolve().then(function(){return (0,eval)(s)}).then(" +
                        "function(v){__ryuiScript.done(JSON.stringify(v===undefined?null:v))}," +
                        "function(e){__ryuiScript.fail(e&&e.stack?String(e.stack):String(e))})})()"
                    view.evaluateJavascript(wrapped, null)
                }
            }
            web.loadDataWithBaseURL("about:blank", "<!doctype html><html></html>", "text/html", "utf-8", null)
            main.postDelayed({ finish(Result.failure(Exception("timeout"))) }, ms)
        }
    },
)
