package dev.ryui.push

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import com.google.firebase.messaging.FirebaseMessaging
import com.google.firebase.messaging.FirebaseMessagingService
import com.google.firebase.messaging.RemoteMessage
import dev.ryui.RyUIPlugin
import dev.ryui.RyUIPushHub
import dev.ryui.RyUIRuntime
import dev.ryui.withPermission
import org.json.JSONObject

/** ryui/push's registration: notifications allowed (Android 13's prompt), then FCM's token. */
class RyUIPushPlugin : RyUIPlugin {
    override fun register(rt: RyUIRuntime) {
        RyUIPushHub.register = { r, reply ->
            val tokenFor = { allowed: Boolean ->
                try {
                    FirebaseMessaging.getInstance().token.addOnCompleteListener { t ->
                        if (!t.isSuccessful) reply(Result.failure(t.exception ?: Exception("no token")))
                        else reply(Result.success(JSONObject().put("token", t.result).put("service", "fcm").put("allowed", allowed).toString().toByteArray()))
                    }
                } catch (e: IllegalStateException) {
                    // FirebaseApp isn't set up: no google-services.json in the build.
                    reply(Result.failure(Exception("Firebase isn't configured: set ryui.json's android.googleServices to the project's google-services.json")))
                }
            }
            if (Build.VERSION.SDK_INT >= 33) r.withPermission(arrayOf(Manifest.permission.POST_NOTIFICATIONS)) { tokenFor(it) }
            else tokenFor(NotificationManagerCompat.from(r.app).areNotificationsEnabled())
        }
    }
}

/**
 * Firebase's messages. In the background, Firebase shows a notification
 * message itself, and a tap opens the app with its data as extras
 * (RyUIActivity hands them to RyUIPushHub); this sees data messages, and
 * every message while the app is in the foreground, which it shows the
 * same way.
 */
class RyUIPushService : FirebaseMessagingService() {
    override fun onMessageReceived(m: RemoteMessage) {
        val n = m.notification
        val id = m.messageId ?: java.util.UUID.randomUUID().toString()
        RyUIPushHub.deliver("nextReceived", RyUIPushHub.message(id, n?.title ?: "", n?.body ?: "", m.data))
        if (n != null) show(this, id, n.title ?: "", n.body ?: "", m.data)
    }

    // A new token: the app sends it on its next Register.
    override fun onNewToken(token: String) {}

    companion object {
        @android.annotation.SuppressLint("MissingPermission")
        fun show(ctx: Context, id: String, title: String, body: String, data: Map<String, String>) {
            val nm = NotificationManagerCompat.from(ctx)
            if (!nm.areNotificationsEnabled()) return
            val sys = ctx.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            if (sys.getNotificationChannel("ryui") == null) {
                sys.createNotificationChannel(NotificationChannel("ryui", "Notifications", NotificationManager.IMPORTANCE_DEFAULT))
            }
            // A tap opens the app as Firebase's tray notifications do: the data as extras.
            val open = (ctx.packageManager.getLaunchIntentForPackage(ctx.packageName) ?: Intent()).apply {
                for ((k, v) in data) putExtra(k, v)
                putExtra("google.message_id", id).putExtra("ryui.title", title).putExtra("ryui.body", body)
                data["url"]?.let { action = Intent.ACTION_VIEW; setData(Uri.parse(it)) }
                flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
            }
            val tap = PendingIntent.getActivity(ctx, id.hashCode(), open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
            val notification = NotificationCompat.Builder(ctx, "ryui")
                .setSmallIcon(ctx.applicationInfo.icon)
                .setContentTitle(title)
                .setContentText(body)
                .setAutoCancel(true)
                .setContentIntent(tap)
                .build()
            try {
                nm.notify(id, 0, notification)
            } catch (_: SecurityException) {
            }
        }
    }
}
