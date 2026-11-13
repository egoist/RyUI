package dev.asuka

import android.content.Intent
import android.os.Handler
import android.os.Looper
import org.json.JSONObject

/**
 * asuka/push's messages, kept until the Go side asks for them: those that
 * come while the app runs ("nextReceived") and those the user taps
 * ("nextOpened"), from any thread (Firebase's service). Its "register"
 * is the asuka-push module's (Firebase Cloud Messaging), set as it loads.
 */
object AsukaPushHub {
    private val main = Handler(Looper.getMainLooper())
    private val queues = mapOf("nextReceived" to ArrayDeque<ByteArray>(), "nextOpened" to ArrayDeque<ByteArray>())
    private val waiting = mapOf("nextReceived" to ArrayDeque<(Result<ByteArray>) -> Unit>(), "nextOpened" to ArrayDeque<(Result<ByteArray>) -> Unit>())

    /** The module's: asks for the token, then answers JSON (token, service, allowed). Main thread. */
    var register: ((AsukaRuntime, (Result<ByteArray>) -> Unit) -> Unit)? = null

    fun message(id: String, title: String, body: String, data: Map<String, String>): ByteArray =
        messageJSON(id, title, body, data, null, null).toString().toByteArray()

    /**
     * A push as push.Message's JSON: a notification message's title, body,
     * channel and image, or a data message's keys of those names (and
     * "subtitle", "category", "thread", "summary", "badge").
     */
    fun messageJSON(id: String, title: String, body: String, data: Map<String, String>, channel: String?, image: String?): JSONObject =
        JSONObject().put("id", id).put("title", title).put("body", body).put("data", JSONObject(data as Map<*, *>))
            .put("subtitle", data["subtitle"].orEmpty()).put("category", data["category"].orEmpty())
            .put("thread", data["thread"].orEmpty()).put("summary", data["summary"].orEmpty())
            .put("channel", channel ?: data["channel"].orEmpty()).put("image", image ?: data["image"].orEmpty())
            .put("badge", data["badge"]?.toIntOrNull() ?: 0)

    /** The rewrites (push.Rewrite) waiting for Go. */
    private val rewrites = AsukaWaitQueue()
    private val pendingRewrites = HashMap<String, (JSONObject?) -> Unit>()

    /**
     * Has Go rewrite message (push.Message's JSON), starting the core
     * without a screen if it hasn't: the notification to show, or null to
     * show it as it came (the rewrite failed, or took more than seconds).
     * Not on the main thread.
     */
    fun rewrite(rt: AsukaRuntime, message: JSONObject, seconds: Long): JSONObject? {
        var out: JSONObject? = null
        val done = java.util.concurrent.CountDownLatch(1)
        val token = java.util.UUID.randomUUID().toString()
        main.post {
            rt.startHeadless()
            pendingRewrites[token] = { out = it; done.countDown() }
            rewrites.deliver(JSONObject().put("token", token).put("message", message).toString().toByteArray())
        }
        done.await(seconds, java.util.concurrent.TimeUnit.SECONDS)
        main.post { pendingRewrites.remove(token) }
        return out
    }

    fun nextRewrite(reply: (Result<ByteArray>) -> Unit) = rewrites.next(reply)

    fun rewritten(o: JSONObject) {
        val p = pendingRewrites.remove(o.optString("token")) ?: return
        p(if (o.optBoolean("ok")) o.optJSONObject("spec") else null)
    }

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
            if (k.startsWith("google.") || k.startsWith("gcm.") || k == "from" || k == "collapse_key" || k.startsWith("asuka.")) continue
            x.getString(k)?.let { data[k] = it }
        }
        deliver("nextOpened", message(id, x.getString("asuka.title") ?: "", x.getString("asuka.body") ?: "", data))
        return data["url"]
    }
}

/**
 * asuka/push's module: "register", "nextReceived", "nextOpened", "stop" (the
 * method's name); "nextRewrite" ({token, message}) and "rewritten"
 * ({token, ok, spec}) for push.Rewrite.
 */
fun AsukaRuntime.pushMethods(): Map<String, AsukaModuleMethod> = mapOf(
    "register" to { _, reply ->
        val r = AsukaPushHub.register
        if (r == null) reply(Result.failure(Exception("asuka/push isn't in this build")))
        else r(this, reply)
    },
    "nextReceived" to { _, reply -> AsukaPushHub.next("nextReceived", reply) },
    "nextOpened" to { _, reply -> AsukaPushHub.next("nextOpened", reply) },
    "nextRewrite" to { _, reply -> AsukaPushHub.nextRewrite(reply) },
    "rewritten" to { args, reply ->
        try { AsukaPushHub.rewritten(JSONObject(String(args, Charsets.UTF_8))) } catch (_: Exception) {}
        reply(Result.success(ByteArray(0)))
    },
    "stop" to { args, reply ->
        AsukaPushHub.stop(String(args, Charsets.UTF_8))
        reply(Result.success(ByteArray(0)))
    },
)
