package dev.ryui

import android.Manifest
import android.app.AlarmManager
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import org.json.JSONObject

/**
 * ryui/notify's module: "request" (u8 allowed), "schedule" (JSON: id,
 * title, body, url, seconds), "cancel" (the id). A notification is posted
 * by an alarm (inexact, allowed while idle: no exact-alarm permission),
 * so it shows whether or not the app runs then; a tap opens the app with
 * its URL (ui.OnOpenURL).
 */
fun RyUIRuntime.notifyMethods(): Map<String, RyUIModuleMethod> = mapOf(
    "request" to { _, reply ->
        if (Build.VERSION.SDK_INT >= 33) withPermission(arrayOf(Manifest.permission.POST_NOTIFICATIONS)) { ok ->
            reply(Result.success(byteArrayOf(if (ok) 1 else 0)))
        } else reply(Result.success(byteArrayOf(if (NotificationManagerCompat.from(app).areNotificationsEnabled()) 1 else 0)))
    },
    "schedule" to { args, reply ->
        try {
            val o = JSONObject(String(args, Charsets.UTF_8))
            val id = o.optString("id").ifEmpty { java.util.UUID.randomUUID().toString() }
            val at = System.currentTimeMillis() + (maxOf(1.0, o.optDouble("seconds", 1.0)) * 1000).toLong()
            val i = Intent(app, RyUINotifyReceiver::class.java)
                .putExtra("id", id).putExtra("title", o.optString("title")).putExtra("body", o.optString("body")).putExtra("url", o.optString("url"))
            val alarms = app.getSystemService(Context.ALARM_SERVICE) as AlarmManager
            alarms.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, at, RyUINotifyReceiver.pending(app, id, i))
            reply(Result.success(ByteArray(0)))
        } catch (e: Exception) {
            reply(Result.failure(e))
        }
    },
    "cancel" to { args, reply ->
        val id = String(args, Charsets.UTF_8)
        val alarms = app.getSystemService(Context.ALARM_SERVICE) as AlarmManager
        alarms.cancel(RyUINotifyReceiver.pending(app, id, Intent(app, RyUINotifyReceiver::class.java)))
        NotificationManagerCompat.from(app).cancel(id, 0)
        reply(Result.success(ByteArray(0)))
    },
)

/** Posts a scheduled notification when its alarm goes off. */
class RyUINotifyReceiver : BroadcastReceiver() {
    // The permission is the app's: the CLI declares it for an app that
    // imports ryui/notify, and the user grants it (Request).
    @android.annotation.SuppressLint("MissingPermission")
    override fun onReceive(ctx: Context, intent: Intent) {
        if (!NotificationManagerCompat.from(ctx).areNotificationsEnabled()) return
        val nm = ctx.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (Build.VERSION.SDK_INT >= 26 && nm.getNotificationChannel(CHANNEL) == null) {
            nm.createNotificationChannel(NotificationChannel(CHANNEL, "Notifications", NotificationManager.IMPORTANCE_DEFAULT))
        }
        val id = intent.getStringExtra("id") ?: return
        // A tap opens the app, with the URL when there's one.
        val open = (ctx.packageManager.getLaunchIntentForPackage(ctx.packageName) ?: Intent()).apply {
            intent.getStringExtra("url")?.takeIf { it.isNotEmpty() }?.let { action = Intent.ACTION_VIEW; data = Uri.parse(it) }
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
        }
        val tap = PendingIntent.getActivity(ctx, id.hashCode(), open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        val n = NotificationCompat.Builder(ctx, CHANNEL)
            .setSmallIcon(ctx.applicationInfo.icon)
            .setContentTitle(intent.getStringExtra("title"))
            .setContentText(intent.getStringExtra("body"))
            .setAutoCancel(true)
            .setContentIntent(tap)
            .build()
        try {
            NotificationManagerCompat.from(ctx).notify(id, 0, n)
        } catch (_: SecurityException) {
            // Not allowed (the user said no): nothing to show.
        }
    }

    companion object {
        const val CHANNEL = "ryui"

        fun pending(ctx: Context, id: String, i: Intent): PendingIntent =
            PendingIntent.getBroadcast(ctx, id.hashCode(), i.setAction("dev.ryui.notify.$id"),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    }
}
