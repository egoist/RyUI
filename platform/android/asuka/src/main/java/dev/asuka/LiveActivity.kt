package dev.asuka

import android.Manifest
import android.app.AlarmManager
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.graphics.drawable.Icon
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import org.json.JSONArray
import org.json.JSONObject

/**
 * asuka/sdk/liveactivity's live activities: an ongoing notification each, which
 * Android 16 promotes to a Live Update (requested with
 * EXTRA_REQUEST_PROMOTED_ONGOING; the user can turn promotion off for the
 * app): at the top of the shade and the lock screen, with Value as the
 * status bar's chip (setShortCriticalText) and the bar as a ProgressStyle.
 * Before Android 16, an ongoing notification of the same.
 *
 * The running ones are kept in the app's preferences (their content, for
 * Running and for a server's updates while the app is away). Main thread.
 */
object AsukaLiveActivities {
    const val CHANNEL = "asuka.liveActivity"
    private const val PREFS = "asuka.liveActivities"
    private const val TAG = "asuka.liveActivity"
    /** An ended activity stays this long at most, as on iOS. */
    private const val ENDED_MS = 4 * 60 * 60 * 1000L
    private val main = Handler(Looper.getMainLooper())

    /** What happened, for Go's "next" (the module's events). */
    val events = AsukaWaitQueue()
    /** Whether the module is in use: then an end is told at once, else at the next launch. */
    private var installed = false

    private fun prefs(ctx: Context) = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    private fun nm(ctx: Context) = ctx.getSystemService(NotificationManager::class.java)

    /** The running activities' records: {content, relevance, push, ended}. */
    private fun record(ctx: Context, id: String): JSONObject? =
        prefs(ctx).getString(id, null)?.let { runCatching { JSONObject(it) }.getOrNull() }

    private fun keep(ctx: Context, id: String, r: JSONObject?) {
        val e = prefs(ctx).edit()
        if (r == null) e.remove(id) else e.putString(id, r.toString())
        e.apply()
    }

    fun channel(ctx: Context): String {
        val nm = nm(ctx)
        if (nm.getNotificationChannel(CHANNEL) == null) {
            nm.createNotificationChannel(NotificationChannel(CHANNEL, "Live updates", NotificationManager.IMPORTANCE_DEFAULT).apply {
                setShowBadge(false)
            })
        }
        return CHANNEL
    }

    /** Notifications allowed, and the channel's not turned off. */
    fun enabled(ctx: Context): Boolean {
        val nm = nm(ctx)
        if (!nm.areNotificationsEnabled()) return false
        val ch = nm.getNotificationChannel(CHANNEL)
        return ch == null || ch.importance != NotificationManager.IMPORTANCE_NONE
    }

    fun deliver(type: String, id: String? = null, token: String? = null) {
        val o = JSONObject().put("type", type)
        if (id != null) o.put("id", id)
        if (token != null) o.put("token", token)
        events.deliver(o.toString().toByteArray())
    }

    /**
     * The module's first use: what ended while it wasn't (dismissed with
     * the app away, cleared, the app stopped) is told now; a push-to-start
     * token, with Firebase.
     */
    fun install(ctx: Context) {
        if (installed) return
        installed = true
        val active = nm(ctx).activeNotifications.filter { it.tag == TAG }.map { it.id }.toSet()
        for (id in prefs(ctx).all.keys.toList()) {
            val r = record(ctx, id)
            if (r == null || r.optBoolean("ended") || id.hashCode() !in active) {
                keep(ctx, id, null)
                deliver("ended", id)
            }
        }
        AsukaPushHub.token?.invoke { t -> if (t != null) main.post { deliver("pushToStartToken", token = t) } }
    }

    /** Firebase's token changed: every pushed activity's, and the push-to-start one. */
    fun newToken(ctx: Context, token: String) = main.post {
        if (!installed) return@post
        for (id in prefs(ctx).all.keys) if (record(ctx, id)?.optBoolean("push") == true) deliver("pushToken", id, token)
        deliver("pushToStartToken", token = token)
    }

    fun start(ctx: Context, id: String, content: JSONObject, relevance: Double, push: Boolean, remote: Boolean = false) {
        keep(ctx, id, JSONObject().put("content", content).put("relevance", relevance).put("push", push))
        post(ctx, id, content, relevance, null, ongoing = true, timeoutMs = 0)
        if (remote) deliver("started", id)
    }

    fun update(ctx: Context, id: String, content: JSONObject, alert: JSONObject?) {
        val r = record(ctx, id) ?: return
        if (r.optBoolean("ended")) return
        keep(ctx, id, r.put("content", content))
        post(ctx, id, content, r.optDouble("relevance", 0.0), alert, ongoing = true, timeoutMs = 0)
    }

    /** dismiss: "now", "at" (atMs), or else it stays, no longer ongoing, 4 hours at most. */
    fun end(ctx: Context, id: String, content: JSONObject?, dismiss: String, atMs: Long) {
        val r = record(ctx, id) ?: return
        keep(ctx, id, null)
        val relevance = r.optDouble("relevance", 0.0)
        val c = content ?: r.optJSONObject("content") ?: JSONObject()
        val left = if (dismiss == "at") (atMs - System.currentTimeMillis()).coerceAtMost(ENDED_MS) else ENDED_MS
        cancelRefresh(ctx, id)
        if (dismiss == "now" || left <= 0) nm(ctx).cancel(TAG, id.hashCode())
        else post(ctx, id, c, relevance, null, ongoing = false, timeoutMs = left)
        if (installed) deliver("ended", id)
    }

    /** The user dismissed it (its delete intent). */
    fun dismissed(ctx: Context, id: String) {
        val r = record(ctx, id) ?: return
        cancelRefresh(ctx, id)
        if (installed) {
            keep(ctx, id, null)
            deliver("ended", id)
        } else keep(ctx, id, r.put("ended", true))
    }

    /** A refresh's alarm: a bar that fills by itself, a countdown that turns to count up. */
    fun refresh(ctx: Context, id: String) {
        val r = record(ctx, id) ?: return
        if (r.optBoolean("ended")) return
        post(ctx, id, r.optJSONObject("content") ?: JSONObject(), r.optDouble("relevance", 0.0), null, ongoing = true, timeoutMs = 0)
    }

    fun running(ctx: Context): JSONArray {
        val out = JSONArray()
        val active = nm(ctx).activeNotifications.filter { it.tag == TAG }.map { it.id }.toSet()
        for (id in prefs(ctx).all.keys) {
            val r = record(ctx, id) ?: continue
            if (r.optBoolean("ended") || id.hashCode() !in active) continue
            out.put(JSONObject().put("id", id).put("content", r.optJSONObject("content") ?: JSONObject()))
        }
        return out
    }

    /**
     * A server's push (Firebase's data message, AsukaPushService): "start",
     * "update" or "end" (asuka.liveActivity) the activity asuka.id, with
     * asuka.content (Content's JSON), asuka.alertTitle and asuka.alertBody,
     * asuka.dismiss ("now", "at" with asuka.dismissAt in ms). Any thread.
     */
    fun remote(ctx: Context, data: Map<String, String>) = main.post {
        val id = data["asuka.id"] ?: return@post
        val content = data["asuka.content"]?.let { runCatching { JSONObject(it) }.getOrNull() }
        val alert = if (data["asuka.alertTitle"] != null || data["asuka.alertBody"] != null)
            JSONObject().put("title", data["asuka.alertTitle"].orEmpty()).put("body", data["asuka.alertBody"].orEmpty()) else null
        when (data["asuka.liveActivity"]) {
            "start" -> if (record(ctx, id) == null) start(ctx, id, content ?: JSONObject(), data["asuka.relevance"]?.toDoubleOrNull() ?: 0.0, push = true, remote = true)
            "update" -> update(ctx, id, content ?: return@post, alert)
            "end" -> end(ctx, id, content, data["asuka.dismiss"].orEmpty(), data["asuka.dismissAt"]?.toLongOrNull() ?: 0)
        }
    }

    /** "#rrggbbaa" as an ARGB int. */
    private fun color(s: String): Int? {
        if (s.length != 9 || !s.startsWith("#")) return null
        val v = s.substring(1).toLongOrNull(16) ?: return null
        return ((v and 0xff) shl 24 or (v ushr 8)).toInt()
    }

    private fun post(ctx: Context, id: String, c: JSONObject, relevance: Double, alert: JSONObject?, ongoing: Boolean, timeoutMs: Long) {
        val now = System.currentTimeMillis()
        val title = c.optString("title")
        val subtitle = c.optString("subtitle")
        val value = c.optString("value")
        val tint = color(c.optString("tint")) ?: ctx.getColor(R.color.asuka_widget_accent)
        val since = c.optLong("sinceMs")
        val until = c.optLong("untilMs")
        val b = Notification.Builder(ctx, channel(ctx))
            .setContentTitle(title.ifEmpty { ctx.applicationInfo.loadLabel(ctx.packageManager) })
            .setContentText(subtitle.ifEmpty { null })
            .setOngoing(ongoing)
            .setAutoCancel(false)
            // Silent but for an alert: its own group, alerting as its
            // summary would (it has none), as NotificationCompat's setSilent.
            .setGroup("$TAG.$id")
            .setGroupAlertBehavior(if (alert == null) Notification.GROUP_ALERT_SUMMARY else Notification.GROUP_ALERT_ALL)
            .setColor(tint)
            .setCategory(if (c.optBoolean("showProgress")) Notification.CATEGORY_PROGRESS else Notification.CATEGORY_STATUS)
            .setVisibility(Notification.VISIBILITY_PUBLIC)
            // The more relevant first (a sort key's order).
            .setSortKey(String.format(java.util.Locale.ROOT, "%012.3f", 1e8 - relevance.coerceIn(0.0, 1e8)))
            .setContentIntent(open(ctx, id, c.optString("url")))
            .setDeleteIntent(PendingIntent.getBroadcast(ctx, id.hashCode(),
                Intent(ctx, AsukaLiveActivityReceiver::class.java).setAction(AsukaLiveActivityReceiver.DISMISSED).putExtra("id", id),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
        val sym = symbolDrawables[c.optString("symbol")]
        if (sym != null) b.setSmallIcon(Icon.createWithResource(ctx, sym)) else b.setSmallIcon(ctx.applicationInfo.icon)
        if (alert != null) {
            val text = listOf(alert.optString("title"), alert.optString("body")).filter { it.isNotEmpty() }.joinToString(": ")
            if (text.isNotEmpty()) b.setTicker(text)
        }
        if (timeoutMs > 0) b.setTimeoutAfter(timeoutMs)
        // The value: Android 16's status bar chip; before, the header's text.
        if (value.isNotEmpty()) {
            if (Build.VERSION.SDK_INT >= 36) b.setShortCriticalText(value) else b.setSubText(value)
        }
        // The countdown: down to until, then up from it (a refresh at until turns it).
        if (until > 0) {
            b.setWhen(until).setShowWhen(true).setUsesChronometer(true).setChronometerCountDown(until > now)
        } else b.setShowWhen(false)
        var nextRefresh = if (until > now) until else 0L
        if (c.optBoolean("showProgress")) {
            // A bar from since to until fills by itself: a refresh a minute.
            val timed = since > 0 && until > since
            val p = if (timed) ((now - since).toDouble() / (until - since)).coerceIn(0.0, 1.0) else c.optDouble("progress", 0.0).coerceIn(0.0, 1.0)
            if (timed && now < until) nextRefresh = minOf(nextRefresh, now + 60_000)
            if (Build.VERSION.SDK_INT >= 36) {
                b.setStyle(Notification.ProgressStyle()
                    .setStyledByProgress(true)
                    .setProgressSegments(listOf(Notification.ProgressStyle.Segment(1000).setColor(tint)))
                    .setProgress((p * 1000).toInt()))
            } else b.setProgress(1000, (p * 1000).toInt(), false)
        } else if (subtitle.isNotEmpty()) {
            b.setStyle(Notification.BigTextStyle().bigText(subtitle))
        }
        // Android 16: promoted to a Live Update while it's ongoing.
        b.extras.putBoolean("android.requestPromotedOngoing", ongoing)
        nm(ctx).notify(TAG, id.hashCode(), b.build())
        if (ongoing && nextRefresh > 0) scheduleRefresh(ctx, id, nextRefresh) else cancelRefresh(ctx, id)
    }

    private fun refreshIntent(ctx: Context, id: String) = PendingIntent.getBroadcast(ctx, id.hashCode(),
        Intent(ctx, AsukaLiveActivityReceiver::class.java).setAction(AsukaLiveActivityReceiver.REFRESH).putExtra("id", id),
        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)

    private fun scheduleRefresh(ctx: Context, id: String, at: Long) {
        // Inexact and not waking the device: the shade's shown when it's on.
        ctx.getSystemService(AlarmManager::class.java).set(AlarmManager.RTC, at, refreshIntent(ctx, id))
    }

    private fun cancelRefresh(ctx: Context, id: String) {
        ctx.getSystemService(AlarmManager::class.java).cancel(refreshIntent(ctx, id))
    }

    /** Opens the app, with the content's URL (ui.OnOpenURL) if it has one. */
    private fun open(ctx: Context, id: String, url: String): PendingIntent {
        val i = Intent(ctx, AsukaActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        if (url.isNotEmpty()) i.setAction(Intent.ACTION_VIEW).setData(Uri.parse(url)) else i.setAction(Intent.ACTION_MAIN)
        return PendingIntent.getActivity(ctx, (TAG + id).hashCode(), i, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    }
}

/** An activity dismissed by the user, and the refreshes' alarms. */
class AsukaLiveActivityReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        val id = intent.getStringExtra("id") ?: return
        when (intent.action) {
            DISMISSED -> AsukaLiveActivities.dismissed(ctx, id)
            REFRESH -> AsukaLiveActivities.refresh(ctx, id)
        }
    }

    companion object {
        const val DISMISSED = "dev.asuka.liveActivity.dismissed"
        const val REFRESH = "dev.asuka.liveActivity.refresh"
    }
}

/**
 * asuka/sdk/liveactivity's module: "enabled" (u8), "start" (JSON: id, content,
 * push, relevance; asks for notifications the first time), "update" (JSON:
 * id, content, alert), "end" (JSON: id, content, dismiss, atMs), "running"
 * (JSON), "next" (an event: type, id, token), "stop".
 */
fun AsukaRuntime.liveActivityMethods(): Map<String, AsukaModuleMethod> {
    val la = AsukaLiveActivities
    fun json(args: ByteArray) = runCatching { JSONObject(String(args, Charsets.UTF_8)) }.getOrNull()
    return mapOf(
        "enabled" to { _, reply -> reply(Result.success(byteArrayOf(if (la.enabled(app)) 1 else 0))) },
        "start" to { args, reply ->
            la.install(app)
            val o = json(args)
            if (o == null) reply(Result.failure(Exception("bad args")))
            else withPermission(arrayOf(Manifest.permission.POST_NOTIFICATIONS)) { ok ->
                val push = o.optBoolean("push")
                val token = AsukaPushHub.token
                when {
                    !ok || !la.enabled(app) -> reply(Result.failure(Exception("not available: notifications are off for the app")))
                    push && token == null -> reply(Result.failure(Exception("Options.Push needs asuka/sdk/push (Firebase): import it")))
                    else -> {
                        val id = o.getString("id")
                        la.start(app, id, o.optJSONObject("content") ?: JSONObject(), o.optDouble("relevance", 0.0), push)
                        reply(Result.success(ByteArray(0)))
                        if (push) token?.invoke { t -> if (t != null) Handler(Looper.getMainLooper()).post { la.deliver("pushToken", id, t) } }
                    }
                }
            }
        },
        "update" to { args, reply ->
            json(args)?.let { la.update(app, it.getString("id"), it.optJSONObject("content") ?: JSONObject(), it.optJSONObject("alert")) }
            reply(Result.success(ByteArray(0)))
        },
        "end" to { args, reply ->
            json(args)?.let { la.end(app, it.getString("id"), it.optJSONObject("content"), it.optString("dismiss"), it.optLong("atMs")) }
            reply(Result.success(ByteArray(0)))
        },
        "running" to { _, reply ->
            la.install(app)
            reply(Result.success(la.running(app).toString().toByteArray()))
        },
        "next" to { _, reply ->
            la.install(app)
            la.events.next(reply)
        },
        "stop" to { _, reply ->
            la.events.stop()
            reply(Result.success(ByteArray(0)))
        },
    )
}
