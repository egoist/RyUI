package dev.nui

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.PowerManager
import android.os.SystemClock
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import org.json.JSONObject

/**
 * nui/background's module: "start", "update" and "end" (args: JSON id,
 * title, text, progress, actions) keep the app running in a foreground
 * service (data sync) while a task lives, each task with its ongoing
 * notification; "next" answers with an action tapped (JSON id, action),
 * or fails "ended" when the last task ends.
 */
class NuiBackground(private val rt: NuiRuntime) {
    private class Task(var opts: JSONObject, var shown: Long = 0, var queued: Boolean = false)

    private val tasks = LinkedHashMap<Int, Task>()
    private val next = ArrayList<(Result<ByteArray>) -> Unit>()
    private val taps = ArrayList<ByteArray>()
    private val main = Handler(Looper.getMainLooper())
    private val nm = rt.app.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
    private var wake: PowerManager.WakeLock? = null

    companion object {
        const val CHANNEL = "nui.background"
        var current: NuiBackground? = null
        fun notificationID(task: Int) = 0x6e75_0000 + task
    }

    init { current = this }

    fun methods(): Map<String, NuiModuleMethod> = mapOf(
        "start" to { args, reply ->
            val o = JSONObject(String(args, Charsets.UTF_8))
            val id = o.getInt("id")
            tasks[id] = Task(o)
            channel()
            // Its notification shows (Android 13 asks first); the work goes on either way.
            if (Build.VERSION.SDK_INT >= 33) rt.withPermission(arrayOf(android.Manifest.permission.POST_NOTIFICATIONS)) {}
            if (tasks.size == 1) {
                wake = (rt.app.getSystemService(Context.POWER_SERVICE) as PowerManager)
                    // Six hours: as long as Android 15 lets a data sync service run a day.
                    .newWakeLock(PowerManager.PARTIAL_WAKE_LOCK, "nui:background").apply { acquire(6 * 60 * 60 * 1000L) }
                androidx.core.content.ContextCompat.startForegroundService(rt.app, Intent(rt.app, NuiWorkService::class.java))
            } else {
                show(id)
            }
            reply(Result.success(ByteArray(0)))
        },
        "update" to { args, reply ->
            val o = JSONObject(String(args, Charsets.UTF_8))
            val t = tasks[o.getInt("id")]
            if (t != null) { t.opts = o; show(o.getInt("id")) }
            reply(Result.success(ByteArray(0)))
        },
        "end" to { args, reply ->
            val id = JSONObject(String(args, Charsets.UTF_8)).getInt("id")
            if (tasks.remove(id) != null) {
                nm.cancel(notificationID(id))
                if (tasks.isEmpty()) {
                    wake?.let { if (it.isHeld) it.release() }
                    wake = null
                    NuiWorkService.running?.stop()
                    val ws = ArrayList(next)
                    next.clear()
                    for (w in ws) w(Result.failure(Exception("ended")))
                } else {
                    // The service's notification is its first task's.
                    NuiWorkService.running?.foreground()
                }
            }
            reply(Result.success(ByteArray(0)))
        },
        "next" to { _, reply ->
            if (taps.isNotEmpty()) reply(Result.success(taps.removeAt(0)))
            else if (tasks.isEmpty()) reply(Result.failure(Exception("ended")))
            else next.add(reply)
        },
    )

    /** An action tapped on a task's notification. */
    fun tapped(task: Int, action: String) {
        val b = JSONObject().put("id", task).put("action", action).toString().toByteArray()
        if (next.isNotEmpty()) next.removeAt(0)(Result.success(b)) else taps.add(b)
    }

    private fun channel() {
        if (nm.getNotificationChannel(CHANNEL) == null) {
            nm.createNotificationChannel(NotificationChannel(CHANNEL, "Background work", NotificationManager.IMPORTANCE_LOW))
        }
    }

    /** The service's notification: the first task's. */
    fun first(): Pair<Int, Notification>? = tasks.keys.firstOrNull()?.let { it to notification(it) }

    /** Shows a task's notification, at most twice a second (Android drops faster updates). */
    private fun show(id: Int) {
        val t = tasks[id] ?: return
        val wait = t.shown + 500 - SystemClock.uptimeMillis()
        if (wait > 0) {
            if (!t.queued) { t.queued = true; main.postDelayed({ t.queued = false; show(id) }, wait) }
            return
        }
        t.shown = SystemClock.uptimeMillis()
        nm.notify(notificationID(id), notification(id))
    }

    private fun notification(id: Int): Notification {
        val o = tasks[id]?.opts ?: JSONObject()
        val app = rt.app
        val open = app.packageManager.getLaunchIntentForPackage(app.packageName)
        val b = NotificationCompat.Builder(app, CHANNEL)
            .setSmallIcon(app.applicationInfo.icon)
            .setContentTitle(o.optString("title"))
            .setContentText(o.optString("text").ifEmpty { null })
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setSilent(true)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .setCategory(NotificationCompat.CATEGORY_PROGRESS)
        if (open != null) {
            b.setContentIntent(PendingIntent.getActivity(app, id, open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
        }
        val p = o.optDouble("progress", -1.0)
        b.setProgress(1000, if (p < 0) 0 else (p.coerceIn(0.0, 1.0) * 1000).toInt(), p < 0)
        val actions = o.optJSONArray("actions")
        if (actions != null) for (i in 0 until minOf(actions.length(), 3)) {
            val a = actions.getJSONObject(i)
            val tap = Intent(app, NuiWorkService::class.java).setAction("dev.nui.background.action")
                .putExtra("task", id).putExtra("action", a.optString("id"))
            val pi = PendingIntent.getService(app, id * 4 + i, tap, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
            b.addAction(0, a.optString("title"), pi)
        }
        return b.build()
    }
}

/** The foreground service nui/background's tasks keep the app running in. */
class NuiWorkService : Service() {
    companion object {
        var running: NuiWorkService? = null
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        running = this
        val bg = NuiBackground.current
        if (intent?.action == "dev.nui.background.action" && bg != null) {
            bg.tapped(intent.getIntExtra("task", 0), intent.getStringExtra("action") ?: "")
        }
        if (!foreground()) stop()
        // A task's process gone, the work is: nothing to start again.
        return START_NOT_STICKY
    }

    /** Shows the first task's notification as the service's; false if none. */
    fun foreground(): Boolean {
        val (id, n) = NuiBackground.current?.first() ?: return false
        val type = if (Build.VERSION.SDK_INT >= 29) ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC else 0
        ServiceCompat.startForeground(this, NuiBackground.notificationID(id), n, type)
        return true
    }

    fun stop() {
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        stopSelf()
        running = null
    }

    // Android 15 limits a data sync service to six hours a day.
    override fun onTimeout(startId: Int, fgsType: Int) = stop()
}
