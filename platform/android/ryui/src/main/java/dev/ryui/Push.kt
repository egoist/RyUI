package dev.ryui

import android.content.Intent
import android.os.Handler
import android.os.Looper
import org.json.JSONObject

/**
 * ryui/push's messages, kept until the Go side asks for them: those that
 * come while the app runs ("nextReceived") and those the user taps
 * ("nextOpened"), from any thread (Firebase's service). Its "register"
 * is the ryui-push module's (Firebase Cloud Messaging), set as it loads.
 */
object RyUIPushHub {
    private val main = Handler(Looper.getMainLooper())
    private val queues = mapOf("nextReceived" to ArrayDeque<ByteArray>(), "nextOpened" to ArrayDeque<ByteArray>())
    private val waiting = mapOf("nextReceived" to ArrayDeque<(Result<ByteArray>) -> Unit>(), "nextOpened" to ArrayDeque<(Result<ByteArray>) -> Unit>())

    /** The module's: asks for the token, then answers JSON (token, service, allowed). Main thread. */
    var register: ((RyUIRuntime, (Result<ByteArray>) -> Unit) -> Unit)? = null

    fun message(id: String, title: String, body: String, data: Map<String, String>): ByteArray =
        JSONObject().put("id", id).put("title", title).put("body", body).put("data", JSONObject(data as Map<*, *>)).toString().toByteArray()

    /** A message for kind, from any thread. */
    fun deliver(kind: String, m: ByteArray) = main.post {
        val w = waiting[kind]?.removeFirstOrNull()
        if (w != null) w(Result.success(m))
        else queues[kind]?.let { q -> q.addLast(m); if (q.size > 64) q.removeFirst() }
    }

    fun next(kind: String, reply: (Result<ByteArray>) -> Unit) {
        val m = queues[kind]?.removeFirstOrNull()
        if (m != null) reply(Result.success(m)) else waiting[kind]?.addLast(reply)
    }

    fun stop(kind: String) {
        val ws = waiting[kind]?.toList() ?: return
        waiting[kind]?.clear()
        for (w in ws) w(Result.failure(Exception("stopped")))
    }

    /**
     * A tapped push's Intent (Firebase's, from the tray, or the one the
     * module posted in the foreground): its data are extras, with
     * google.message_id. Returns its "url", if any.
     */
    fun opened(intent: Intent): String? {
        val x = intent.extras ?: return null
        val id = x.getString("google.message_id") ?: return null
        val data = HashMap<String, String>()
        for (k in x.keySet()) {
            if (k.startsWith("google.") || k.startsWith("gcm.") || k == "from" || k == "collapse_key" || k.startsWith("ryui.")) continue
            x.getString(k)?.let { data[k] = it }
        }
        deliver("nextOpened", message(id, x.getString("ryui.title") ?: "", x.getString("ryui.body") ?: "", data))
        return data["url"]
    }
}

/** ryui/push's module: "register", "nextReceived", "nextOpened", "stop" (the method's name). */
fun RyUIRuntime.pushMethods(): Map<String, RyUIModuleMethod> = mapOf(
    "register" to { _, reply ->
        val r = RyUIPushHub.register
        if (r == null) reply(Result.failure(Exception("ryui/push isn't in this build")))
        else r(this, reply)
    },
    "nextReceived" to { _, reply -> RyUIPushHub.next("nextReceived", reply) },
    "nextOpened" to { _, reply -> RyUIPushHub.next("nextOpened", reply) },
    "stop" to { args, reply ->
        RyUIPushHub.stop(String(args, Charsets.UTF_8))
        reply(Result.success(ByteArray(0)))
    },
)
