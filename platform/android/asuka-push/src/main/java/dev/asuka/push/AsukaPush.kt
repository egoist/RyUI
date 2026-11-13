package dev.asuka.push

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import com.google.firebase.messaging.FirebaseMessaging
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import dev.asuka.AsukaPlugin
import dev.asuka.AsukaNotifications
import dev.asuka.AsukaPushHub
import dev.asuka.AsukaRuntime
import dev.asuka.withPermission
import org.json.JSONObject

/** asuka/sdk/push's registration: notifications allowed (Android 13's prompt), then FCM's token. */
class AsukaPushPlugin : AsukaPlugin {
    override fun register(rt: AsukaRuntime) {
        AsukaPushHub.register = { r, reply ->
            val tokenFor = { allowed: Boolean ->
                try {
                    FirebaseMessaging.getInstance().token.addOnCompleteListener { t ->
                        if (!t.isSuccessful) reply(Result.failure(t.exception ?: Exception("no token")))
                        else reply(Result.success(JSONObject().put("token", t.result).put("service", "fcm").put("allowed", allowed).toString().toByteArray()))
                    }
                } catch (e: IllegalStateException) {
                    // FirebaseApp isn't set up: no google-services.json in the build.
                    reply(Result.failure(Exception("Firebase isn't configured: set asuka.json's android.googleServices to the project's google-services.json")))
                }
            }
            r.withPermission(arrayOf(Manifest.permission.POST_NOTIFICATIONS)) { tokenFor(it) }
        }
    }
}

/**
 * Firebase's messages. In the background, Firebase shows a notification
 * message itself, and a tap opens the app with its data as extras
 * (AsukaActivity hands them to AsukaPushHub); this sees data messages, and
 * every message while the app is in the foreground. It shows them with
 * asuka/sdk/notify's builder: a notification message, and a data message with
 * a "title" or "body" (its "channel", "category", "thread", "image"...).
 * With push.Rewrite (the manifest's dev.asuka.push.rewrite), the Go code
 * rewrites each first, started without a screen if the app isn't running.
 */
class AsukaPushService : FirebaseMessagingService() {
    override fun onMessageReceived(m: RemoteMessage) {
        val n = m.notification
        val id = m.messageId ?: java.util.UUID.randomUUID().toString()
        val message = AsukaPushHub.messageJSON(id, n?.title ?: m.data["title"].orEmpty(), n?.body ?: m.data["body"].orEmpty(), m.data,
            n?.channelId, n?.imageUrl?.toString())
        AsukaPushHub.deliver("nextReceived", message.toString().toByteArray())
        var spec = if (n != null || m.data["title"] != null || m.data["body"] != null) specOf(message) else null
        if (rewrites(this)) AsukaPushHub.rewrite(AsukaRuntime.get(this), message, REWRITE_SECONDS)?.let { spec = it }
        val s = spec ?: return
        if (s.optString("title").isEmpty() && s.optString("body").isEmpty()) return
        // The push's id for its tap (push.OnOpen), whatever the notification's is.
        s.put("asuka.message", id)
        if (s.optString("url").isEmpty()) m.data["url"]?.let { s.put("url", it) }
        AsukaNotifications.show(this, s, remote = true)
    }

    // A new token: the app sends it on its next Register.
    override fun onNewToken(token: String) {}

    companion object {
        /** What Firebase gives onMessageReceived is about 20 seconds: the rewrite has half. */
        const val REWRITE_SECONDS = 10L

        /** The message as it came, as the notification to show (a notifyspec.Spec). */
        fun specOf(message: JSONObject): JSONObject = JSONObject()
            .put("id", message.optString("id")).put("title", message.optString("title")).put("body", message.optString("body"))
            .put("subtitle", message.optString("subtitle")).put("data", message.optJSONObject("data") ?: JSONObject())
            .put("category", message.optString("category")).put("thread", message.optString("thread"))
            .put("summary", message.optString("summary")).put("channel", message.optString("channel"))
            .put("image", message.optString("image")).put("badge", message.optInt("badge"))

        /** Whether the app rewrites its pushes (push.Rewrite): the CLI says so in the manifest. */
        fun rewrites(ctx: Context): Boolean = try {
            val ai = ctx.packageManager.getApplicationInfo(ctx.packageName, PackageManager.GET_META_DATA)
            ai.metaData?.getBoolean("dev.asuka.push.rewrite") == true
        } catch (_: Exception) {
            false
        }
    }
}
