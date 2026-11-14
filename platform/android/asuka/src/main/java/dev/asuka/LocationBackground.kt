package dev.asuka

import android.annotation.SuppressLint
import android.app.ActivityManager
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.location.Location
import android.location.LocationManager
import android.location.LocationRequest
import android.net.Uri
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.UUID

/**
 * What delivers the background updates and the geofences: Google Play
 * services' Fused Location Provider and GeofencingClient (the asuka-location
 * module's AsukaFusedProvider), or, on a device without them (or an app
 * built without the module), the location manager's updates and
 * proximity alerts. Both deliver to PendingIntents for
 * [AsukaLocationReceiver], so they come with the app not running.
 */
interface AsukaLocationProvider {
    /** The most geofences at once (0: none stated); whether dwells are reported. */
    val limit: Int
    val dwell: Boolean
    fun usable(ctx: Context): Boolean
    fun requestUpdates(ctx: Context, u: JSONObject, pi: PendingIntent)
    fun removeUpdates(ctx: Context, pi: PendingIntent)
    /** Adds the regions (locationspec's JSON); initial: an Enter if the device is inside now. */
    fun addRegions(ctx: Context, rs: List<JSONObject>, initial: Boolean, done: (Exception?) -> Unit)
    /** Removes the regions ids (null: all). */
    fun removeRegions(ctx: Context, ids: List<String>?)
    /** The positions and crossings in a broadcast, or null if it has none of this provider's. */
    fun read(ctx: Context, intent: Intent): AsukaLocationRead?
}

/** What a broadcast brought: positions, crossings (region, 1 enter 2 exit 4 dwell, where), whether the geofences were lost (location turned off). */
class AsukaLocationRead(
    val positions: List<JSONObject> = emptyList(),
    val crossings: List<Triple<String, Int, JSONObject?>> = emptyList(),
    val lost: Boolean = false,
)

object AsukaLocationProviders {
    private var chosen: AsukaLocationProvider? = null
    val manager = AsukaManagerProvider()

    /** Play services' if the app has the module and the device the services, else the location manager's. */
    fun get(ctx: Context): AsukaLocationProvider = chosen ?: run {
        val fused = try {
            Class.forName("dev.asuka.location.AsukaFusedProvider").getDeclaredConstructor().newInstance() as AsukaLocationProvider
        } catch (_: ReflectiveOperationException) { null } catch (_: LinkageError) { null }
        (if (fused != null && fused.usable(ctx)) fused else manager).also { chosen = it }
    }

    /** Reads a broadcast with either provider (one sent by the location manager's has its extras). */
    fun read(ctx: Context, intent: Intent): AsukaLocationRead? = manager.read(ctx, intent) ?: get(ctx).read(ctx, intent)
}

object AsukaLocationIntents {
    const val UPDATES = "dev.asuka.location.UPDATES"
    const val REGIONS = "dev.asuka.location.REGIONS"
    private const val FLAGS = PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE

    fun updates(ctx: Context): PendingIntent =
        PendingIntent.getBroadcast(ctx, 1, Intent(ctx, AsukaLocationReceiver::class.java).setAction(UPDATES), FLAGS)

    fun regions(ctx: Context): PendingIntent =
        PendingIntent.getBroadcast(ctx, 2, Intent(ctx, AsukaLocationReceiver::class.java).setAction(REGIONS), FLAGS)

    /** A region's own (the location manager's proximity alerts take one each). */
    fun region(ctx: Context, id: String): PendingIntent =
        PendingIntent.getBroadcast(ctx, 3, Intent(ctx, AsukaLocationReceiver::class.java).setAction(REGIONS)
            .setData(Uri.fromParts("asuka-region", id, null)), FLAGS)
}

/** The location manager's: LocationRequest updates (Android 12's, batched with a max delay) and proximity alerts. */
@SuppressLint("MissingPermission") // checked first: the module's start and monitor
class AsukaManagerProvider : AsukaLocationProvider {
    override val limit = 0
    override val dwell = false
    override fun usable(ctx: Context) = true

    private fun lm(ctx: Context) = ctx.getSystemService(Context.LOCATION_SERVICE) as LocationManager

    override fun requestUpdates(ctx: Context, u: JSONObject, pi: PendingIntent) {
        val lm = lm(ctx)
        val significant = u.optBoolean("significant")
        val accuracy = if (significant) 2 else u.optInt("accuracy")
        val interval = u.optLong("interval").takeIf { it > 0 } ?: if (significant) 5 * 60_000L else 10_000L
        val provider = when {
            accuracy == 4 -> LocationManager.PASSIVE_PROVIDER
            lm.hasProvider(LocationManager.FUSED_PROVIDER) -> LocationManager.FUSED_PROVIDER
            accuracy <= 1 && lm.hasProvider(LocationManager.GPS_PROVIDER) -> LocationManager.GPS_PROVIDER
            else -> LocationManager.NETWORK_PROVIDER
        }
        val quality = when (accuracy) {
            0, 1 -> LocationRequest.QUALITY_HIGH_ACCURACY
            2 -> LocationRequest.QUALITY_BALANCED_POWER_ACCURACY
            else -> LocationRequest.QUALITY_LOW_POWER
        }
        val distance = if (significant) maxOf(u.optDouble("distance", 0.0), 500.0) else u.optDouble("distance", 0.0)
        val req = LocationRequest.Builder(interval).setQuality(quality).setMinUpdateDistanceMeters(distance.toFloat())
            .setMaxUpdateDelayMillis(u.optLong("defer")).build()
        lm.requestLocationUpdates(provider, req, pi)
    }

    override fun removeUpdates(ctx: Context, pi: PendingIntent) = lm(ctx).removeUpdates(pi)

    override fun addRegions(ctx: Context, rs: List<JSONObject>, initial: Boolean, done: (Exception?) -> Unit) {
        try {
            for (r in rs) {
                lm(ctx).addProximityAlert(r.getDouble("lat"), r.getDouble("lon"), r.getDouble("radius").toFloat(), -1, AsukaLocationIntents.region(ctx, r.getString("id")))
            }
            done(null)
        } catch (e: Exception) { done(e) }
    }

    override fun removeRegions(ctx: Context, ids: List<String>?) {
        for (id in ids ?: AsukaLocationBackground.regions(ctx).map { it.getString("id") }) lm(ctx).removeProximityAlert(AsukaLocationIntents.region(ctx, id))
    }

    override fun read(ctx: Context, intent: Intent): AsukaLocationRead? {
        val x = intent.extras ?: return null
        if (x.containsKey(LocationManager.KEY_PROXIMITY_ENTERING)) {
            val id = intent.data?.schemeSpecificPart ?: return null
            return AsukaLocationRead(crossings = listOf(Triple(id, if (x.getBoolean(LocationManager.KEY_PROXIMITY_ENTERING)) 1 else 2, null)))
        }
        @Suppress("DEPRECATION")
        val ls: List<Location> = x.getParcelableArrayList<Location>(LocationManager.KEY_LOCATIONS)
            ?: listOfNotNull(x.getParcelable<Location>(LocationManager.KEY_LOCATION_CHANGED))
        if (ls.isEmpty()) return null
        return AsukaLocationRead(positions = ls.map { AsukaLocation.position(it) })
    }
}

/**
 * The events (locationspec's), kept in a file until Go reports each done:
 * one that came as the process ended is handed over at the next start. An
 * event starts the core (without an Activity if there's none), and keeps
 * the broadcast that brought it (goAsync) until the events are done, or
 * [SECONDS]. Main thread.
 */
object AsukaLocationEvents {
    const val SECONDS = 9
    private val main = Handler(Looper.getMainLooper())
    private var file: File? = null
    private val events = ArrayList<JSONObject>()
    private val handed = HashSet<String>()
    private val waiting = ArrayDeque<(Result<ByteArray>) -> Unit>()
    private val holds = ArrayList<BroadcastReceiver.PendingResult>()

    fun load(ctx: Context) {
        if (file != null) return
        val f = File(ctx.filesDir, "asuka-location-events.json")
        file = f
        runCatching { val a = JSONArray(f.readText()); for (i in 0 until a.length()) events.add(a.getJSONObject(i)) }
    }

    private fun save() {
        val f = file ?: return
        val tmp = File(f.path + ".tmp")
        tmp.writeText(JSONArray(events).toString())
        tmp.renameTo(f)
    }

    /** What a broadcast brought, as events; hold is its goAsync. Any thread. */
    fun received(ctx: Context, read: AsukaLocationRead, hold: BroadcastReceiver.PendingResult?) = main.post {
        load(ctx)
        val out = ArrayList<JSONObject>()
        if (read.positions.isNotEmpty() && AsukaLocationBackground.updates(ctx) != null) {
            out.add(JSONObject().put("positions", JSONArray(read.positions)))
        }
        for ((id, kind, at) in read.crossings) crossing(ctx, id, kind, at)?.let { out.add(it) }
        if (out.isEmpty()) { hold?.finish(); return@post }
        for (e in out) events.add(e.put("id", UUID.randomUUID().toString()))
        save()
        if (hold != null) {
            holds.add(hold)
            main.postDelayed({ if (holds.remove(hold)) hold.finish() }, SECONDS * 1000L)
        }
        AsukaRuntime.get(ctx).startHeadless()
        hand(ctx)
    }

    /** A crossing as an event, if it's one the region reports: from unknown to outside is none (the region was just added with the device out of it). */
    private fun crossing(ctx: Context, id: String, kind: Int, at: JSONObject?): JSONObject? {
        val r = AsukaLocationBackground.regions(ctx).firstOrNull { it.getString("id") == id } ?: return null
        val inside = AsukaLocationBackground.inside(ctx)
        val was = if (inside.has(id)) inside.getBoolean(id) else null
        if (kind != 4) {
            AsukaLocationBackground.setInside(ctx, inside.put(id, kind == 1))
            if (was == null && kind == 2 || was == (kind == 1)) return null
        }
        if (r.optInt("events", 3) and kind == 0) return null
        val e = JSONObject().put("region", id).put("kind", kind).put("t", System.currentTimeMillis())
        if (at != null) e.put("at", at)
        return e
    }

    private fun hand(ctx: Context) {
        while (waiting.isNotEmpty()) {
            val e = events.firstOrNull { !handed.contains(it.getString("id")) } ?: break
            handed.add(e.getString("id"))
            val out = JSONObject(e.toString()).put("seconds", budget(ctx))
            waiting.removeFirst()(Result.success(out.toString().toByteArray()))
        }
    }

    /** How long the app may run for an event: no limit in front, or with the service running. */
    private fun budget(ctx: Context): Int {
        val info = ActivityManager.RunningAppProcessInfo()
        ActivityManager.getMyMemoryState(info)
        return if (info.importance <= ActivityManager.RunningAppProcessInfo.IMPORTANCE_FOREGROUND_SERVICE) 0 else SECONDS
    }

    fun next(ctx: Context, reply: (Result<ByteArray>) -> Unit) {
        load(ctx)
        waiting.addLast(reply)
        hand(ctx)
    }

    fun done(id: String) {
        events.removeAll { it.optString("id") == id }
        handed.remove(id)
        save()
        if (events.isEmpty()) {
            val hs = ArrayList(holds)
            holds.clear()
            for (h in hs) h.finish()
        }
    }
}

/**
 * asuka/sdk/location's background part (locationspec's "locationBackground"):
 * "start" (JSON Updates; with a title, in [AsukaLocationService]), "stop",
 * "updating"; "monitor" (JSON Region; Always and precise location needed),
 * "unmonitor" (an ID; empty: all), "regions"; "next" and "done" (the
 * events, [AsukaLocationEvents]); "capabilities". What runs is kept in
 * SharedPreferences, and asked for again after a reboot or an update
 * (which clear them), and once location is back on for geofences it lost.
 */
@SuppressLint("MissingPermission") // checked first
class AsukaLocationBackground(private val rt: AsukaRuntime) {
    companion object {
        private const val PREFS = "asuka.location.background"
        private fun prefs(ctx: Context) = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

        fun updates(ctx: Context): JSONObject? = prefs(ctx).getString("updates", null)?.let { runCatching { JSONObject(it) }.getOrNull() }
        fun regions(ctx: Context): List<JSONObject> = prefs(ctx).getString("regions", null)?.let { s ->
            runCatching { val a = JSONArray(s); (0 until a.length()).map { a.getJSONObject(it) } }.getOrNull()
        } ?: emptyList()
        private fun setRegions(ctx: Context, rs: List<JSONObject>) = prefs(ctx).edit().putString("regions", JSONArray(rs).toString()).apply()
        fun inside(ctx: Context): JSONObject = runCatching { JSONObject(prefs(ctx).getString("inside", "{}")!!) }.getOrDefault(JSONObject())
        fun setInside(ctx: Context, o: JSONObject) = prefs(ctx).edit().putString("inside", o.toString()).apply()
        fun lost(ctx: Context) = prefs(ctx).edit().putBoolean("lost", true).apply()

        /**
         * Asks again for what runs: the updates (the same PendingIntent
         * replaces them), and the geofences after a reboot or an update,
         * or once they were lost. Main thread.
         */
        fun restore(ctx: Context, cleared: Boolean) {
            val u = updates(ctx)
            val rs = regions(ctx)
            if (u == null && rs.isEmpty()) return
            val p = AsukaLocationProviders.get(ctx)
            if (u != null && AsukaLocationAccess.foreground(ctx)) {
                runCatching { p.requestUpdates(ctx, u, AsukaLocationIntents.updates(ctx)) }
                if (u.optString("title").isNotEmpty()) startService(ctx)
            }
            if (rs.isNotEmpty() && (cleared || prefs(ctx).getBoolean("lost", false)) && AsukaLocationAccess.background(ctx)) {
                p.addRegions(ctx, rs, false) { e -> if (e == null) prefs(ctx).edit().putBoolean("lost", false).apply() }
            }
        }

        fun startService(ctx: Context) {
            runCatching { androidx.core.content.ContextCompat.startForegroundService(ctx, Intent(ctx, AsukaLocationService::class.java)) }
        }
    }

    private val ctx = rt.app
    private val provider get() = AsukaLocationProviders.get(ctx)

    private var watchingMode = false

    init {
        restore(ctx, false)
        if (regions(ctx).isNotEmpty()) watchMode()
    }

    /** Geofences lost as location was turned off: added again once it's on. */
    private fun watchMode() {
        if (watchingMode) return
        watchingMode = true
        androidx.core.content.ContextCompat.registerReceiver(ctx, object : BroadcastReceiver() {
            override fun onReceive(c: Context, i: Intent) {
                if (androidx.core.location.LocationManagerCompat.isLocationEnabled(c.getSystemService(Context.LOCATION_SERVICE) as LocationManager)) restore(c, false)
            }
        }, android.content.IntentFilter(LocationManager.MODE_CHANGED_ACTION), androidx.core.content.ContextCompat.RECEIVER_NOT_EXPORTED)
    }

    private fun ok(reply: (Result<ByteArray>) -> Unit) = reply(Result.success(ByteArray(0)))

    fun methods(): Map<String, AsukaModuleMethod> = mapOf(
        "capabilities" to { _, reply ->
            val p = provider
            reply(Result.success(JSONObject().put("updates", true).put("regions", true).put("limit", p.limit)
                .put("dwell", p.dwell).put("relaunch", true).toString().toByteArray()))
        },
        "start" to { args, reply ->
            val u = JSONObject(String(args, Charsets.UTF_8))
            val access = AsukaLocationAccess.current
            val go = {
                if (!AsukaLocationAccess.foreground(ctx)) reply(Result.failure(Exception("denied")))
                else try {
                    prefs(ctx).edit().putString("updates", u.toString()).apply()
                    provider.requestUpdates(ctx, u, AsukaLocationIntents.updates(ctx))
                    if (u.optString("title").isNotEmpty()) {
                        // Its notification shows (the system asks first); the updates go on either way.
                        rt.withPermission(arrayOf(android.Manifest.permission.POST_NOTIFICATIONS)) {}
                        startService(ctx)
                        AsukaLocationService.running?.show()
                    } else {
                        AsukaLocationService.running?.stop()
                    }
                    ok(reply)
                } catch (e: Exception) {
                    prefs(ctx).edit().remove("updates").apply()
                    reply(Result.failure(e))
                }
            }
            if (access != null) access.request(3) { go() } else go()
        },
        "stop" to { _, reply ->
            prefs(ctx).edit().remove("updates").apply()
            runCatching { provider.removeUpdates(ctx, AsukaLocationIntents.updates(ctx)) }
            AsukaLocationService.running?.stop()
            ok(reply)
        },
        "updating" to { _, reply -> reply(Result.success((updates(ctx)?.toString() ?: "null").toByteArray())) },
        "monitor" to { args, reply ->
            val r = JSONObject(String(args, Charsets.UTF_8))
            val id = r.getString("id")
            val rs = regions(ctx).filter { it.getString("id") != id }
            val limit = provider.limit
            when {
                !AsukaLocationAccess.background(ctx) -> reply(Result.failure(Exception("always")))
                !AsukaLocationAccess.granted(ctx, android.Manifest.permission.ACCESS_FINE_LOCATION) ->
                    reply(Result.failure(Exception("geofences need precise location (location.RequestPrecise)")))
                limit > 0 && rs.size >= limit -> reply(Result.failure(Exception("too many regions ($limit at most on Android)")))
                else -> {
                    watchMode()
                    setInside(ctx, inside(ctx).apply { remove(id) })
                    setRegions(ctx, rs + r)
                    provider.addRegions(ctx, listOf(r), true) { e ->
                        if (e == null) ok(reply)
                        else {
                            setRegions(ctx, rs)
                            reply(Result.failure(Exception("region $id: ${e.message}")))
                        }
                    }
                }
            }
        },
        "unmonitor" to { args, reply ->
            val id = String(args, Charsets.UTF_8)
            runCatching { provider.removeRegions(ctx, if (id.isEmpty()) null else listOf(id)) }
            setRegions(ctx, if (id.isEmpty()) emptyList() else regions(ctx).filter { it.getString("id") != id })
            setInside(ctx, if (id.isEmpty()) JSONObject() else inside(ctx).apply { remove(id) })
            ok(reply)
        },
        "regions" to { _, reply -> reply(Result.success(JSONArray(regions(ctx)).toString().toByteArray())) },
        "next" to { _, reply -> AsukaLocationEvents.next(ctx, reply) },
        "done" to { args, reply -> AsukaLocationEvents.done(String(args, Charsets.UTF_8)); ok(reply) },
    )
}

/**
 * The broadcasts: the updates' and the geofences' PendingIntents (with the
 * app not running, they start its process), and a reboot or an update,
 * after which what ran is asked for again.
 */
class AsukaLocationReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        when (intent.action) {
            Intent.ACTION_BOOT_COMPLETED, Intent.ACTION_MY_PACKAGE_REPLACED ->
                Handler(Looper.getMainLooper()).post { AsukaLocationBackground.restore(ctx, true) }
            else -> {
                val read = AsukaLocationProviders.read(ctx, intent) ?: return
                if (read.lost) AsukaLocationBackground.lost(ctx)
                if (read.positions.isEmpty() && read.crossings.isEmpty()) return
                AsukaLocationEvents.received(ctx, read, goAsync())
            }
        }
    }
}

/**
 * The foreground service of the location type that keeps the background
 * updates at their rate with the app out of sight, its notification the
 * updates' title and text (tapping it opens the app). Started again by
 * Android if the process is ended (START_STICKY), while updates run.
 */
class AsukaLocationService : Service() {
    companion object {
        const val CHANNEL = "asuka.location"
        const val ID = 0x6c6f_6361
        var running: AsukaLocationService? = null
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        running = this
        if (!show()) { stop(); return START_NOT_STICKY }
        return START_STICKY
    }

    /** Shows the updates' notification as the service's; false if none (or Android won't). */
    fun show(): Boolean {
        val u = AsukaLocationBackground.updates(this) ?: return false
        if (u.optString("title").isEmpty()) return false
        val nm = getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (nm.getNotificationChannel(CHANNEL) == null) {
            nm.createNotificationChannel(NotificationChannel(CHANNEL, "Location", NotificationManager.IMPORTANCE_LOW))
        }
        val open = packageManager.getLaunchIntentForPackage(packageName)
        val b = NotificationCompat.Builder(this, CHANNEL)
            .setSmallIcon(applicationInfo.icon)
            .setContentTitle(u.optString("title"))
            .setContentText(u.optString("text").ifEmpty { null })
            .setOngoing(true)
            .setSilent(true)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .setCategory(NotificationCompat.CATEGORY_NAVIGATION)
        if (open != null) b.setContentIntent(PendingIntent.getActivity(this, ID, open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
        val n: Notification = b.build()
        return try {
            ServiceCompat.startForeground(this, ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
            true
        } catch (e: Exception) {
            // Not allowed now (started from the background without Always,
            // the permission revoked): the updates go on as Android lets them.
            android.util.Log.w("asuka", "location: the foreground service can't start: $e")
            false
        }
    }

    fun stop() {
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        stopSelf()
        running = null
    }

    override fun onDestroy() {
        if (running === this) running = null
        super.onDestroy()
    }
}
