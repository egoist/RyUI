package dev.ryui

import android.Manifest
import android.app.AlarmManager
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationChannelGroup
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.Looper
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.app.Person
import androidx.core.app.RemoteInput
import androidx.core.content.LocusIdCompat
import androidx.core.content.pm.ShortcutInfoCompat
import androidx.core.content.pm.ShortcutManagerCompat
import androidx.core.graphics.drawable.IconCompat
import androidx.lifecycle.Lifecycle
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.UUID
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/**
 * ryui/notify's module (and ryui/push's notifications, which go through
 * the same builder):
 * - "request" (u8 allowed); "schedule" (a notifyspec.Spec; seconds 0
 *   posts it now, else an inexact alarm does, allowed while idle: no
 *   exact-alarm permission); "cancel" (the id).
 * - "categories", "channels", "channelGroups" (JSON), kept: a push that
 *   comes with the app closed is built with them.
 * - "badge": Android has none of its own (launchers count notifications).
 * - "showing" (JSON list), "remove" (JSON ids), "removeThread", "removeAll".
 * - "present", "nextPresent", "presented"; "nextAction", "actionDone";
 *   "stop" (the method's name): as iOS's (RyUINotifyHub).
 */
fun RyUIRuntime.notifyMethods(): Map<String, RyUIModuleMethod> = mapOf(
    "request" to { _, reply ->
        if (Build.VERSION.SDK_INT >= 33) withPermission(arrayOf(Manifest.permission.POST_NOTIFICATIONS)) { ok ->
            reply(Result.success(byteArrayOf(if (ok) 1 else 0)))
        } else reply(Result.success(byteArrayOf(if (NotificationManagerCompat.from(app).areNotificationsEnabled()) 1 else 0)))
    },
    "schedule" to { args, reply ->
        try {
            val spec = JSONObject(String(args, Charsets.UTF_8))
            if (spec.optString("id").isEmpty()) spec.put("id", UUID.randomUUID().toString())
            val seconds = spec.optDouble("seconds", 0.0)
            if (seconds <= 0) {
                RyUINotifications.worker.execute {
                    RyUINotifications.show(app, spec, remote = false)
                    RyUINotifications.main.post { reply(Result.success(ByteArray(0))) }
                }
            } else {
                val at = System.currentTimeMillis() + (maxOf(1.0, seconds) * 1000).toLong()
                val i = Intent(app, RyUINotifyReceiver::class.java).putExtra(RyUINotifyReceiver.SPEC, spec.toString())
                val alarms = app.getSystemService(Context.ALARM_SERVICE) as AlarmManager
                alarms.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, at, RyUINotifyReceiver.alarm(app, spec.getString("id"), i))
                reply(Result.success(ByteArray(0)))
            }
        } catch (e: Exception) {
            reply(Result.failure(e))
        }
    },
    "cancel" to { args, reply ->
        val id = String(args, Charsets.UTF_8)
        val alarms = app.getSystemService(Context.ALARM_SERVICE) as AlarmManager
        alarms.cancel(RyUINotifyReceiver.alarm(app, id, Intent(app, RyUINotifyReceiver::class.java)))
        RyUINotifications.worker.execute { RyUINotifications.remove(app, listOf(id)) }
        reply(Result.success(ByteArray(0)))
    },
    "categories" to { args, reply ->
        RyUINotifications.prefs(app).edit().putString("categories", String(args, Charsets.UTF_8)).apply()
        reply(Result.success(ByteArray(0)))
    },
    "channels" to { args, reply ->
        RyUINotifications.setChannels(app, JSONArray(String(args, Charsets.UTF_8)))
        reply(Result.success(ByteArray(0)))
    },
    "channelGroups" to { args, reply ->
        RyUINotifications.setGroups(app, JSONArray(String(args, Charsets.UTF_8)))
        reply(Result.success(ByteArray(0)))
    },
    // Launchers that badge count the app's notifications: nothing to set.
    "badge" to { _, reply -> reply(Result.success(ByteArray(0))) },
    "showing" to { _, reply ->
        RyUINotifications.worker.execute {
            val list = RyUINotifications.showing(app)
            RyUINotifications.main.post { reply(Result.success(list.toString().toByteArray())) }
        }
    },
    "remove" to { args, reply ->
        val a = JSONArray(String(args, Charsets.UTF_8))
        val ids = (0 until a.length()).map { a.getString(it) }
        RyUINotifications.worker.execute { RyUINotifications.remove(app, ids) }
        reply(Result.success(ByteArray(0)))
    },
    "removeThread" to { args, reply ->
        val thread = String(args, Charsets.UTF_8)
        RyUINotifications.worker.execute { RyUINotifications.removeThread(app, thread) }
        reply(Result.success(ByteArray(0)))
    },
    "removeAll" to { _, reply ->
        RyUINotifications.worker.execute {
            NotificationManagerCompat.from(app).cancelAll()
            RyUINotifications.prune(app)
        }
        reply(Result.success(ByteArray(0)))
    },
    "present" to { args, reply -> RyUINotifyHub.setPresenting(args.firstOrNull() == 1.toByte()); reply(Result.success(ByteArray(0))) },
    "nextPresent" to { _, reply -> RyUINotifyHub.presents.next(reply) },
    "presented" to { args, reply ->
        if (args.isNotEmpty()) RyUINotifyHub.presented(String(args, 1, args.size - 1, Charsets.UTF_8), args[0].toInt())
        reply(Result.success(ByteArray(0)))
    },
    "nextAction" to { _, reply -> RyUINotifyHub.actions.next(reply) },
    "actionDone" to { args, reply -> RyUINotifyHub.done(String(args, Charsets.UTF_8)); reply(Result.success(ByteArray(0))) },
    "stop" to { args, reply ->
        when (String(args, Charsets.UTF_8)) {
            "nextPresent" -> RyUINotifyHub.presents.stop()
            "nextAction" -> RyUINotifyHub.actions.stop()
        }
        reply(Result.success(ByteArray(0)))
    },
)

/** What Go waits for ("next..." methods): items kept until asked, calls until an item comes. Main thread. */
class RyUIWaitQueue {
    private val items = ArrayDeque<ByteArray>()
    private val waiting = ArrayDeque<(Result<ByteArray>) -> Unit>()

    fun next(reply: (Result<ByteArray>) -> Unit) {
        val i = items.removeFirstOrNull()
        if (i != null) reply(Result.success(i)) else waiting.addLast(reply)
    }

    fun deliver(item: ByteArray) {
        val w = waiting.removeFirstOrNull()
        if (w != null) w(Result.success(item)) else items.addLast(item)
    }

    fun stop() {
        val ws = waiting.toList()
        waiting.clear()
        for (w in ws) w(Result.failure(Exception("stopped")))
    }
}

/**
 * The presentation decisions and the responses waiting for Go, on the
 * main thread. A notification that comes while the app is in front, with
 * a presenter (notify.Present), waits for Go's answer a second at most; a
 * background action keeps its receiver (goAsync) until Go is done, or
 * [ACTION_SECONDS].
 */
object RyUINotifyHub {
    const val ACTION_SECONDS = 9
    private val main = Handler(Looper.getMainLooper())
    val presents = RyUIWaitQueue()
    val actions = RyUIWaitQueue()
    private var presenting = false
    private val pendingPresent = HashMap<String, (Int) -> Unit>()
    private val pendingActions = HashMap<String, () -> Unit>()

    fun setPresenting(on: Boolean) {
        presenting = on
        if (!on) {
            val ps = pendingPresent.values.toList()
            pendingPresent.clear()
            for (p in ps) p(0)
        }
    }

    /** How a notification shows (0 banner, 1 quiet, 2 hide): Go's answer if the app is in front with a presenter. Not on the main thread. */
    fun ask(ctx: Context, shown: JSONObject): Int {
        var how = 0
        val answered = CountDownLatch(1)
        val token = UUID.randomUUID().toString()
        main.post {
            val act = RyUIRuntime.get(ctx).activity
            if (!presenting || act == null || !act.lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) {
                answered.countDown()
                return@post
            }
            pendingPresent[token] = { how = it; answered.countDown() }
            presents.deliver(JSONObject().put("token", token).put("notification", shown).toString().toByteArray())
        }
        answered.await(1, TimeUnit.SECONDS)
        main.post { pendingPresent.remove(token) }
        return how
    }

    fun presented(token: String, how: Int) {
        pendingPresent.remove(token)?.invoke(how)
    }

    /**
     * Hands Go the user's response (starting the core without a screen if
     * it hasn't): done runs when Go is done with it, or in [ACTION_SECONDS].
     * Any thread.
     */
    fun respond(ctx: Context, action: String, reply: String, shown: JSONObject, done: (() -> Unit)?) = main.post {
        RyUIRuntime.get(ctx).startHeadless()
        val token = UUID.randomUUID().toString()
        if (done != null) {
            pendingActions[token] = done
            main.postDelayed({ pendingActions.remove(token)?.invoke() }, ACTION_SECONDS * 1000L)
        }
        actions.deliver(JSONObject().put("token", token).put("action", action).put("reply", reply)
            .put("seconds", ACTION_SECONDS).put("notification", shown).toString().toByteArray())
    }

    fun done(token: String) {
        pendingActions.remove(token)?.invoke()
    }

    /** A tap, or an action that opens the app (RyUIActivity's intent): its response, if it has one. */
    fun opened(ctx: Context, intent: Intent) {
        val r = intent.getStringExtra(RyUINotifications.RESPONSE) ?: return
        intent.removeExtra(RyUINotifications.RESPONSE)
        val o = try { JSONObject(r) } catch (_: Exception) { return }
        val shown = o.optJSONObject("notification") ?: JSONObject()
        val action = o.optString("action")
        val reply = RemoteInput.getResultsFromIntent(intent)?.getCharSequence(RyUINotifications.REPLY)?.toString() ?: ""
        // An action's notification goes, as a tap's does (auto cancel).
        if (action.isNotEmpty()) RyUINotifications.worker.execute { RyUINotifications.remove(ctx, listOf(shown.optString("id"))) }
        respond(ctx, action, reply, shown, null)
    }
}

/**
 * The one builder of notifications, from a notifyspec.Spec: ryui/notify's
 * and pushes'. What it posts is kept (by id, in SharedPreferences) while
 * it shows: its data for "showing", its messages to add the next one to,
 * the reply to add after an inline reply. Off the main thread ([worker]):
 * it downloads images and waits for Go's presentation.
 */
object RyUINotifications {
    const val CHANNEL = "ryui"
    const val RESPONSE = "ryui.notify.response"
    const val REPLY = "ryui.reply"
    private const val SUMMARY = "ryui.summary:"
    val worker = Executors.newSingleThreadExecutor()
    val main = Handler(Looper.getMainLooper())
    /** Times each id was posted: an action's handler that posts it again replaces the update after a reply. */
    private val posts = HashMap<String, Int>()

    fun prefs(ctx: Context) = ctx.getSharedPreferences("ryui.notify", Context.MODE_PRIVATE)

    private fun manager(ctx: Context) = ctx.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

    fun defaultChannel(ctx: Context) {
        val nm = manager(ctx)
        if (nm.getNotificationChannel(CHANNEL) == null) {
            nm.createNotificationChannel(NotificationChannel(CHANNEL, "Notifications", NotificationManager.IMPORTANCE_DEFAULT))
        }
    }

    private fun importance(i: Int) = when (i) {
        1 -> NotificationManager.IMPORTANCE_HIGH
        2 -> NotificationManager.IMPORTANCE_LOW
        3 -> NotificationManager.IMPORTANCE_MIN
        else -> NotificationManager.IMPORTANCE_DEFAULT
    }

    /** notify.SetChannels: makes and renames them, deletes the ones it made before that aren't listed. */
    fun setChannels(ctx: Context, a: JSONArray) {
        val nm = manager(ctx)
        val ids = HashSet<String>()
        for (i in 0 until a.length()) {
            val o = a.getJSONObject(i)
            val id = o.getString("id")
            if (id == CHANNEL) continue
            ids.add(id)
            val c = NotificationChannel(id, o.optString("name", id), importance(o.optInt("importance")))
            c.description = o.optString("description").ifEmpty { null }
            if (o.optBoolean("silent")) c.setSound(null, null)
            c.enableVibration(!o.optBoolean("noVibration"))
            c.setShowBadge(!o.optBoolean("noBadge"))
            o.optString("group").takeIf { it.isNotEmpty() }?.let { c.group = it }
            nm.createNotificationChannel(c)
        }
        val p = prefs(ctx)
        for (old in p.getStringSet("channels", emptySet())!!) if (old !in ids) nm.deleteNotificationChannel(old)
        p.edit().putStringSet("channels", ids).apply()
    }

    /** notify.SetChannelGroups, likewise. */
    fun setGroups(ctx: Context, a: JSONArray) {
        val nm = manager(ctx)
        val ids = HashSet<String>()
        for (i in 0 until a.length()) {
            val o = a.getJSONObject(i)
            ids.add(o.getString("id"))
            nm.createNotificationChannelGroup(NotificationChannelGroup(o.getString("id"), o.optString("name", o.getString("id"))))
        }
        val p = prefs(ctx)
        for (old in p.getStringSet("channelGroups", emptySet())!!) if (old !in ids) nm.deleteNotificationChannelGroup(old)
        p.edit().putStringSet("channelGroups", ids).apply()
    }

    /** The category's actions (notify.SetCategories), as kept. */
    private fun actions(ctx: Context, category: String): List<JSONObject> {
        if (category.isEmpty()) return emptyList()
        val a = try { JSONArray(prefs(ctx).getString("categories", "[]")) } catch (_: Exception) { return emptyList() }
        for (i in 0 until a.length()) {
            val c = a.getJSONObject(i)
            if (c.optString("id") == category) {
                val acts = c.optJSONArray("actions") ?: return emptyList()
                return (0 until acts.length()).map { acts.getJSONObject(it) }
            }
        }
        return emptyList()
    }

    /** What's reported of a notification (a notifyspec.Shown), from its spec. */
    fun shown(spec: JSONObject, time: Long = System.currentTimeMillis()): JSONObject = JSONObject()
        .put("id", spec.optString("id")).put("title", spec.optString("title")).put("subtitle", spec.optString("subtitle"))
        .put("body", spec.optString("body")).put("url", spec.optString("url"))
        .put("data", spec.optJSONObject("data") ?: JSONObject()).put("category", spec.optString("category"))
        .put("thread", spec.optString("thread")).put("time", time).put("remote", spec.optBoolean("ryui.remote"))

    /**
     * Shows spec now (a notifyspec.Spec; remote: a push's, whose data are
     * the tap's extras as Firebase's are), unless Go hides it. Off the main
     * thread.
     */
    @android.annotation.SuppressLint("MissingPermission")
    fun show(ctx: Context, spec: JSONObject, remote: Boolean) {
        val nm = NotificationManagerCompat.from(ctx)
        if (!nm.areNotificationsEnabled()) return
        defaultChannel(ctx)
        if (spec.optString("id").isEmpty()) spec.put("id", UUID.randomUUID().toString())
        if (remote) spec.put("ryui.remote", true)
        val id = spec.getString("id")
        val how = RyUINotifyHub.ask(ctx, shown(spec))
        if (how == 2) return
        // A kept spec is the notification's only while it shows: one the
        // user cleared (or a reboot took) has nothing to add to.
        if (!showing(ctx, id)) prefs(ctx).edit().remove("spec:$id").commit()
        val n = build(ctx, spec, quiet = how == 1) ?: return
        try {
            nm.notify(id, 0, n)
        } catch (_: SecurityException) {
            return // not allowed (the user said no)
        }
        synchronized(posts) { posts[id] = (posts[id] ?: 0) + 1; postedAt[id] = System.currentTimeMillis() }
        prefs(ctx).edit().putString("spec:$id", spec.toString()).apply()
        spec.optString("thread").takeIf { it.isNotEmpty() }?.let { summarize(ctx, it, spec) }
        prune(ctx, keep = id)
    }

    fun posted(id: String) = synchronized(posts) { posts[id] ?: 0 }

    /** When each id was last posted, in this process. */
    private val postedAt = HashMap<String, Long>()

    /** Whether id shows: the manager lists it, or it was posted a moment ago (the manager posts asynchronously). */
    private fun showing(ctx: Context, id: String): Boolean =
        manager(ctx).activeNotifications.any { it.tag == id } ||
            synchronized(posts) { (postedAt[id] ?: 0L) > System.currentTimeMillis() - 2000 }

    /** The spec kept for id, while it shows. */
    fun spec(ctx: Context, id: String): JSONObject? = prefs(ctx).getString("spec:$id", null)?.let {
        try { JSONObject(it) } catch (_: Exception) { null }
    }

    private fun build(ctx: Context, spec: JSONObject, quiet: Boolean): Notification? {
        val id = spec.getString("id")
        val channel = spec.optString("channel").takeIf { it.isNotEmpty() && manager(ctx).getNotificationChannel(it) != null } ?: CHANNEL
        val b = NotificationCompat.Builder(ctx, channel)
            .setSmallIcon(ctx.applicationInfo.icon)
            .setContentTitle(spec.optString("title"))
            .setContentText(spec.optString("body"))
            .setAutoCancel(true)
            .setContentIntent(activityIntent(ctx, spec, ""))
            .setDeleteIntent(broadcast(ctx, RyUINotifyReceiver.DELETED, id, "", spec, mutable = false))
        spec.optString("subtitle").takeIf { it.isNotEmpty() }?.let { b.setSubText(it) }
        spec.optInt("badge").takeIf { it > 0 }?.let { b.setNumber(it) }
        if (quiet || spec.optBoolean("silent")) b.setSilent(true)
        spec.optString("thread").takeIf { it.isNotEmpty() }?.let {
            b.setGroup(it).setGroupAlertBehavior(NotificationCompat.GROUP_ALERT_CHILDREN)
        }
        for (a in actions(ctx, spec.optString("category"))) {
            val aid = a.optString("id")
            val open = a.optBoolean("open")
            val reply = a.optJSONObject("reply")
            val pi = if (open) activityIntent(ctx, spec, aid) else broadcast(ctx, RyUINotifyReceiver.ACTION, id, aid, spec, mutable = reply != null)
            val ab = NotificationCompat.Action.Builder(0, a.optString("title"), pi)
                .setShowsUserInterface(open)
                .setAuthenticationRequired(a.optBoolean("unlock"))
            if (reply != null) {
                ab.addRemoteInput(RemoteInput.Builder(REPLY).setLabel(reply.optString("placeholder").ifEmpty { a.optString("title") }).build())
                    .setSemanticAction(NotificationCompat.Action.SEMANTIC_ACTION_REPLY)
                    .setAllowGeneratedReplies(true)
            }
            b.addAction(ab.build())
        }
        val image = spec.optString("image").takeIf { it.isNotEmpty() }?.let { bitmap(ctx, it) }
        val sender = spec.optJSONObject("sender")
        val conversation = spec.optJSONObject("conversation")
        if (sender != null && conversation != null) {
            message(ctx, b, spec, sender, conversation)
        } else if (image != null) {
            b.setLargeIcon(image).setStyle(NotificationCompat.BigPictureStyle().bigPicture(image).bigLargeIcon(null as Bitmap?))
        } else if (spec.optString("body").length > 40) {
            b.setStyle(NotificationCompat.BigTextStyle().bigText(spec.optString("body")))
        }
        return b.build()
    }

    /** A message: MessagingStyle with its conversation's messages, and the conversation's long-lived shortcut. */
    private fun message(ctx: Context, b: NotificationCompat.Builder, spec: JSONObject, sender: JSONObject, conversation: JSONObject) {
        val convID = conversation.optString("id")
        val meJ = conversation.optJSONObject("me")
        val me = person(ctx, meJ, me = true)
        val from = person(ctx, sender, me = false)
        val group = conversation.optBoolean("group")
        val style = NotificationCompat.MessagingStyle(me).setGroupConversation(group)
        val title = conversation.optString("title")
        if (group && title.isNotEmpty()) style.setConversationTitle(title)
        val now = spec.optLong("ryui.time").takeIf { it > 0 } ?: System.currentTimeMillis()
        spec.put("ryui.time", now)
        val msgs = RyUIMessages.thread(conversation, spec(ctx, spec.getString("id")), sender, spec.optString("body"), now)
        // Kept with the spec: the next message (and a reply) adds to them.
        conversation.put("earlier", RyUIMessages.toJSON(msgs.dropLast(1)))
        val people = HashMap<String, Person>()
        for (m in msgs) {
            val p = m.sender?.let { s -> people.getOrPut(s.optString("id").ifEmpty { s.optString("name") }) { person(ctx, s, me = false) } }
            style.addMessage(NotificationCompat.MessagingStyle.Message(m.text, m.time, p))
        }
        b.setStyle(style).setCategory(NotificationCompat.CATEGORY_MESSAGE).setShortcutId(convID).setLocusId(LocusIdCompat(convID))
        if (sender.optBoolean("ryui.me")) return // the user's reply: the shortcut is there
        // The conversation's shortcut: the shade's Conversations section shows it.
        val label = if (group && title.isNotEmpty()) title else from.name?.toString() ?: convID
        val icon = conversation.optString("image").takeIf { group && it.isNotEmpty() }?.let { bitmap(ctx, it) }?.let { IconCompat.createWithBitmap(it) }
            ?: from.icon ?: IconCompat.createWithResource(ctx, ctx.applicationInfo.icon)
        val open = (ctx.packageManager.getLaunchIntentForPackage(ctx.packageName) ?: Intent()).setAction(Intent.ACTION_VIEW)
        spec.optString("url").takeIf { it.isNotEmpty() }?.let { open.data = Uri.parse(it) }
        val shortcut = ShortcutInfoCompat.Builder(ctx, convID)
            .setShortLabel(label).setLongLived(true).setIcon(icon).setIntent(open)
            .setLocusId(LocusIdCompat(convID)).setPerson(from)
            .setCategories(setOf("dev.ryui.conversation"))
            .build()
        try {
            ShortcutManagerCompat.pushDynamicShortcut(ctx, shortcut)
        } catch (_: Exception) {
            // Over the launcher's rate limit: the notification shows without it.
        }
    }

    private fun person(ctx: Context, o: JSONObject?, me: Boolean): Person {
        val name = o?.optString("name").orEmpty().ifEmpty { if (me) "You" else "" }
        val p = Person.Builder().setName(name).setBot(o?.optBoolean("bot") == true)
        o?.optString("id")?.takeIf { it.isNotEmpty() }?.let { p.setKey(it) }
        o?.optString("image")?.takeIf { it.isNotEmpty() }?.let { bitmap(ctx, it) }?.let { p.setIcon(IconCompat.createWithBitmap(it)) }
        return p.build()
    }

    /** A tap's (action "") or an action's that opens the app: RyUIActivity, with the response. */
    private fun activityIntent(ctx: Context, spec: JSONObject, action: String): PendingIntent {
        val id = spec.getString("id")
        val data = spec.optJSONObject("data")
        val open = (ctx.packageManager.getLaunchIntentForPackage(ctx.packageName) ?: Intent()).apply {
            spec.optString("url").takeIf { it.isNotEmpty() && action.isEmpty() }?.let { setAction(Intent.ACTION_VIEW); setData(Uri.parse(it)) }
            putExtra(RESPONSE, JSONObject().put("action", action).put("notification", shown(spec)).toString())
            // A push's: its data as extras, as Firebase's taps (RyUIPushHub.opened).
            if (spec.optBoolean("ryui.remote") && action.isEmpty()) {
                data?.keys()?.forEach { k -> putExtra(k, data.optString(k)) }
                putExtra("google.message_id", spec.optString("ryui.message", id))
                putExtra("ryui.title", spec.optString("title")).putExtra("ryui.body", spec.optString("body"))
            }
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP
        }
        val flags = PendingIntent.FLAG_UPDATE_CURRENT or if (action.isEmpty()) PendingIntent.FLAG_IMMUTABLE else PendingIntent.FLAG_MUTABLE
        return PendingIntent.getActivity(ctx, "$id\u0000$action".hashCode(), open, flags)
    }

    /** A background action's, or the notification's dismissal: RyUINotifyReceiver. */
    private fun broadcast(ctx: Context, kind: String, id: String, action: String, spec: JSONObject, mutable: Boolean): PendingIntent {
        val i = Intent(ctx, RyUINotifyReceiver::class.java)
            .setAction(kind).setData(Uri.fromParts("ryui", "$id/$action", null))
            .putExtra("id", id).putExtra("action", action).putExtra("thread", spec.optString("thread"))
            .putExtra(RESPONSE, shown(spec).toString())
        val flags = PendingIntent.FLAG_UPDATE_CURRENT or if (mutable) PendingIntent.FLAG_MUTABLE else PendingIntent.FLAG_IMMUTABLE
        return PendingIntent.getBroadcast(ctx, "$kind\u0000$id\u0000$action".hashCode(), i, flags)
    }

    /** The image a source names (as ui.Image's: an http(s) URL, a file, an asset), scaled to at most 1024 px. */
    fun bitmap(ctx: Context, src: String): Bitmap? = try {
        val bytes = when {
            src.startsWith("http://") || src.startsWith("https://") -> {
                val c = URL(src).openConnection() as HttpURLConnection
                c.connectTimeout = 10_000
                c.readTimeout = 10_000
                try { if (c.responseCode in 200..299) c.inputStream.use { it.readBytes() } else null } finally { c.disconnect() }
            }
            src.startsWith("file://") -> Uri.parse(src).path?.let { File(it).readBytes() }
            src.startsWith("/") -> File(src).readBytes()
            File(ctx.filesDir, "ryui/assets/$src").exists() -> File(ctx.filesDir, "ryui/assets/$src").readBytes()
            else -> ctx.assets.open(src).use { it.readBytes() }
        }
        bytes?.let { decode(it, 1024) }
    } catch (_: Exception) {
        null
    }

    private fun decode(bytes: ByteArray, max: Int): Bitmap? {
        val o = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(bytes, 0, bytes.size, o)
        var sample = 1
        while (o.outWidth / (sample * 2) >= max || o.outHeight / (sample * 2) >= max) sample *= 2
        return BitmapFactory.decodeByteArray(bytes, 0, bytes.size, BitmapFactory.Options().apply { inSampleSize = sample })
    }

    /**
     * The group's summary notification: its children's lines, as they are
     * now (latest: one just posted, which the manager may not list yet);
     * none without children.
     */
    @android.annotation.SuppressLint("MissingPermission")
    fun summarize(ctx: Context, thread: String, latest: JSONObject? = null) {
        val nm = NotificationManagerCompat.from(ctx)
        val lid = latest?.optString("id")
        val kids = manager(ctx).activeNotifications.filter {
            it.notification.group == thread && it.tag?.startsWith(SUMMARY) != true && it.tag != lid
        }.sortedByDescending { it.postTime }
        if (kids.isEmpty() && latest == null) {
            nm.cancel(SUMMARY + thread, 0)
            return
        }
        val top = latest ?: kids.first().tag?.let { spec(ctx, it) } ?: JSONObject()
        val style = NotificationCompat.InboxStyle()
        if (latest != null) style.addLine("${latest.optString("title")}  ${latest.optString("body")}".trim())
        for (k in kids) {
            val x = k.notification.extras
            style.addLine("${x.getCharSequence(Notification.EXTRA_TITLE) ?: ""}  ${x.getCharSequence(Notification.EXTRA_TEXT) ?: ""}".trim())
        }
        top.optString("summary").takeIf { it.isNotEmpty() }?.let { style.setSummaryText(it) }
        val channel = latest?.optString("channel")?.takeIf { it.isNotEmpty() && manager(ctx).getNotificationChannel(it) != null }
            ?: kids.firstOrNull()?.notification?.channelId ?: CHANNEL
        val open = (ctx.packageManager.getLaunchIntentForPackage(ctx.packageName) ?: Intent()).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        val n = NotificationCompat.Builder(ctx, channel)
            .setSmallIcon(ctx.applicationInfo.icon)
            .setContentTitle(top.optString("title"))
            .setContentText(top.optString("summary").ifEmpty { top.optString("body") })
            .setStyle(style)
            .setGroup(thread).setGroupSummary(true)
            .setGroupAlertBehavior(NotificationCompat.GROUP_ALERT_CHILDREN)
            .setSilent(true).setAutoCancel(true)
            .setContentIntent(PendingIntent.getActivity(ctx, (SUMMARY + thread).hashCode(), open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
            .build()
        try {
            nm.notify(SUMMARY + thread, 0, n)
        } catch (_: SecurityException) {
        }
    }

    /** The notifications showing (notifyspec.Shown), newest first, without the summaries. */
    fun showing(ctx: Context): JSONArray {
        val out = JSONArray()
        for (s in manager(ctx).activeNotifications.sortedByDescending { it.postTime }) {
            val tag = s.tag
            if (tag?.startsWith(SUMMARY) == true) continue
            val id = tag ?: s.id.toString()
            val spec = spec(ctx, id)
            if (spec != null) {
                out.put(shown(spec, s.postTime))
                continue
            }
            // One Firebase showed (a notification message, the app in the background).
            val x = s.notification.extras
            out.put(JSONObject().put("id", id).put("title", x.getCharSequence(Notification.EXTRA_TITLE)?.toString() ?: "")
                .put("subtitle", x.getCharSequence(Notification.EXTRA_SUB_TEXT)?.toString() ?: "")
                .put("body", x.getCharSequence(Notification.EXTRA_TEXT)?.toString() ?: "").put("url", "").put("data", JSONObject())
                .put("category", "").put("thread", s.notification.group ?: "").put("time", s.postTime)
                .put("remote", tag?.startsWith("FCM-Notification") == true))
        }
        return out
    }

    /** Takes away the notifications ids, keeping their groups' summaries as they are. */
    fun remove(ctx: Context, ids: List<String>) {
        val nm = NotificationManagerCompat.from(ctx)
        val threads = HashSet<String>()
        for (s in manager(ctx).activeNotifications) {
            val id = s.tag ?: s.id.toString()
            if (id in ids) {
                s.notification.group?.let { threads.add(it) }
                nm.cancel(s.tag, s.id)
            }
        }
        for (id in ids) nm.cancel(id, 0)
        for (t in threads) summarize(ctx, t)
        prune(ctx)
    }

    /** Takes away thread's notifications and its summary. */
    fun removeThread(ctx: Context, thread: String) {
        val nm = NotificationManagerCompat.from(ctx)
        for (s in manager(ctx).activeNotifications) if (s.notification.group == thread) nm.cancel(s.tag, s.id)
        nm.cancel(SUMMARY + thread, 0)
        prune(ctx)
    }

    /**
     * Forgets the specs of notifications that no longer show, but keep's:
     * one just posted, which the manager (posting asynchronously) may not
     * list yet.
     */
    fun prune(ctx: Context, keep: String? = null) {
        val active = manager(ctx).activeNotifications.mapNotNull { it.tag }.toSet() + listOfNotNull(keep)
        val p = prefs(ctx)
        val e = p.edit()
        for (k in p.all.keys) if (k.startsWith("spec:") && k.removePrefix("spec:") !in active) e.remove(k)
        e.apply()
    }

    /**
     * After an inline reply, once Go is done with it: Android leaves the
     * reply's spinner until the notification is posted again. Unless the
     * app showed it again, a conversation shows the reply under the user's
     * name; another is taken away.
     */
    fun replied(ctx: Context, id: String, text: String, postsBefore: Int) {
        if (posted(id) != postsBefore) return
        val spec = spec(ctx, id)
        val conversation = spec?.optJSONObject("conversation")
        if (spec == null || conversation == null || spec.optJSONObject("sender") == null) {
            remove(ctx, listOf(id))
            return
        }
        // The reply, as the user's message; quietly.
        conversation.put("earlier", RyUIMessages.toJSON(RyUIMessages.withLatest(spec)))
        spec.put("sender", JSONObject().put("name", conversation.optJSONObject("me")?.optString("name").orEmpty().ifEmpty { "You" }).put("ryui.me", true))
        spec.put("body", text).put("silent", true).put("ryui.time", System.currentTimeMillis())
        val n = build(ctx, spec, quiet = true) ?: return
        try {
            NotificationManagerCompat.from(ctx).notify(id, 0, n)
            prefs(ctx).edit().putString("spec:$id", spec.toString()).apply()
        } catch (_: SecurityException) {
        }
    }
}

/**
 * A conversation's messages, as MessagingStyle shows them: those the spec
 * says came before (or, without, those of the notification showing), and
 * the new one. Pure, for the tests.
 */
object RyUIMessages {
    data class Message(val sender: JSONObject?, val text: String, val time: Long)

    /** earlier's (a notifyspec.Conversation's), or else shown's (the spec kept for the notification showing), then text from sender. */
    fun thread(conversation: JSONObject, shown: JSONObject?, sender: JSONObject, text: String, now: Long): List<Message> {
        val out = ArrayList<Message>()
        val earlier = conversation.optJSONArray("earlier")
        if (earlier != null && earlier.length() > 0) {
            out.addAll(fromJSON(earlier))
        } else if (shown != null && shown.optJSONObject("conversation")?.optString("id") == conversation.optString("id")) {
            out.addAll(withLatest(shown))
        }
        out.add(Message(if (sender.optBoolean("ryui.me")) null else sender, text, now))
        return out
    }

    /** The spec's messages: its conversation's earlier ones and its own (a sent spec, as kept). */
    fun withLatest(spec: JSONObject): List<Message> {
        val earlier = spec.optJSONObject("conversation")?.optJSONArray("earlier")?.let { fromJSON(it) } ?: emptyList()
        val s = spec.optJSONObject("sender") ?: JSONObject()
        return earlier + Message(if (s.optBoolean("ryui.me")) null else s, spec.optString("body"), spec.optLong("ryui.time"))
    }

    fun fromJSON(a: JSONArray): List<Message> = (0 until a.length()).map {
        val m = a.getJSONObject(it)
        Message(m.optJSONObject("sender"), m.optString("text"), m.optLong("time"))
    }

    fun toJSON(ms: List<Message>): JSONArray {
        val a = JSONArray()
        for (m in ms) {
            val o = JSONObject().put("text", m.text).put("time", m.time)
            m.sender?.let { o.put("sender", it) }
            a.put(o)
        }
        return a
    }
}

/**
 * The notifications' broadcasts: a scheduled one's alarm (posts it), a
 * background action (hands Go the response, keeping the process until
 * it's done; after a reply, updates the notification as Android wants),
 * and a dismissal (keeps the group's summary as it is).
 */
class RyUINotifyReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        val pending = goAsync()
        when (intent.action) {
            ACTION -> {
                val id = intent.getStringExtra("id") ?: return pending.finish()
                val action = intent.getStringExtra("action").orEmpty()
                val reply = RemoteInput.getResultsFromIntent(intent)?.getCharSequence(RyUINotifications.REPLY)?.toString()
                val shown = try { JSONObject(intent.getStringExtra(RyUINotifications.RESPONSE) ?: "{}") } catch (_: Exception) { JSONObject() }
                val before = RyUINotifications.posted(id)
                RyUINotifyHub.respond(ctx, action, reply.orEmpty(), shown) {
                    RyUINotifications.worker.execute {
                        // As iOS: a background action's notification goes;
                        // after a reply, a conversation shows it.
                        if (reply != null) RyUINotifications.replied(ctx, id, reply, before)
                        else if (RyUINotifications.posted(id) == before) RyUINotifications.remove(ctx, listOf(id))
                        pending.finish()
                    }
                }
            }
            DELETED -> RyUINotifications.worker.execute {
                intent.getStringExtra("thread")?.takeIf { it.isNotEmpty() }?.let { RyUINotifications.summarize(ctx, it) }
                RyUINotifications.prune(ctx)
                pending.finish()
            }
            else -> {
                val spec = intent.getStringExtra(SPEC)?.let { try { JSONObject(it) } catch (_: Exception) { null } }
                    // Scheduled by an earlier version: its extras.
                    ?: intent.getStringExtra("id")?.let { id ->
                        JSONObject().put("id", id).put("title", intent.getStringExtra("title")).put("body", intent.getStringExtra("body")).put("url", intent.getStringExtra("url"))
                    }
                if (spec == null) return pending.finish()
                RyUINotifications.worker.execute {
                    RyUINotifications.show(ctx, spec, remote = false)
                    pending.finish()
                }
            }
        }
    }

    companion object {
        const val ACTION = "dev.ryui.notify.ACTION"
        const val DELETED = "dev.ryui.notify.DELETED"
        const val SPEC = "spec"

        fun alarm(ctx: Context, id: String, i: Intent): PendingIntent =
            PendingIntent.getBroadcast(ctx, id.hashCode(), i.setAction("dev.ryui.notify.$id"),
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    }
}
