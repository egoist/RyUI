package dev.asuka

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Looper
import androidx.core.location.LocationManagerCompat
import org.json.JSONObject

/**
 * asuka/sdk/location's module: the position now ("current"), and each next one
 * while watching ("next", "stop"), from Android's location manager (no
 * Play services). Replies are JSON: lat, lon, acc (meters), alt, t (Unix
 * ms); "denied" fails when the user hasn't allowed it.
 */
@SuppressLint("MissingPermission") // asked for first: withPermission
class AsukaLocation(private val rt: AsukaRuntime) {
    private val lm = rt.app.getSystemService(Context.LOCATION_SERVICE) as LocationManager
    private val perms = arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION)
    private val next = ArrayList<(Result<ByteArray>) -> Unit>()
    private var unseen: Location? = null
    private var updating = false
    private val listener = LocationListener { l -> got(l) }

    private fun provider(): String = when {
        lm.hasProvider(LocationManager.FUSED_PROVIDER) -> LocationManager.FUSED_PROVIDER
        lm.isProviderEnabled(LocationManager.GPS_PROVIDER) -> LocationManager.GPS_PROVIDER
        else -> LocationManager.NETWORK_PROVIDER
    }

    /** The newest fix any provider has. */
    private fun newest(): Location? = lm.getProviders(true).mapNotNull { runCatching { lm.getLastKnownLocation(it) }.getOrNull() }.maxByOrNull { it.time }

    private fun encode(l: Location): ByteArray = position(l).toString().toByteArray()

    companion object {
        /** A position as JSON: lat, lon, acc, alt (0: not known), spd and crs (-1: not known), t (Unix ms). */
        fun position(l: Location): JSONObject = JSONObject(mapOf(
            "lat" to l.latitude, "lon" to l.longitude, "acc" to l.accuracy.toDouble(),
            "alt" to if (l.hasAltitude()) l.altitude else 0.0,
            "spd" to if (l.hasSpeed()) l.speed.toDouble() else -1.0,
            "crs" to if (l.hasBearing()) l.bearing.toDouble() else -1.0, "t" to l.time,
        ))
    }

    private fun got(l: Location) {
        if (!updating) return
        if (next.isEmpty()) { unseen = l; return }
        val ws = ArrayList(next)
        next.clear()
        val data = encode(l)
        for (w in ws) w(Result.success(data))
    }

    fun methods(): Map<String, AsukaModuleMethod> = mapOf(
        "name" to { args, reply ->
            val o = org.json.JSONObject(String(args, Charsets.UTF_8))
            Thread {
                reply(runCatching {
                    @Suppress("DEPRECATION")
                    val a = android.location.Geocoder(rt.app).getFromLocation(o.getDouble("lat"), o.getDouble("lon"), 1)?.firstOrNull()
                        ?: throw Exception("no name for this place")
                    listOfNotNull(a.subLocality ?: a.thoroughfare, a.locality ?: a.adminArea ?: a.countryName).distinct().joinToString(", ").toByteArray()
                })
            }.start()
        },
        "current" to { _, reply ->
            rt.withPermission(perms) { ok ->
                AsukaLocationAccess.asked(rt.app)
                if (!ok) { reply(Result.failure(Exception("denied"))); return@withPermission }
                // A fix from the last minute is where the device is; else a
                // new one, for 10 s at most, then the last there was.
                val last = newest()
                if (last != null && System.currentTimeMillis() - last.time < 60_000) {
                    reply(Result.success(encode(last)))
                    return@withPermission
                }
                val cancel = androidx.core.os.CancellationSignal()
                var answered = false
                fun answer(l: Location?) {
                    if (answered) return
                    answered = true
                    val loc = l ?: newest()
                    reply(if (loc != null) Result.success(encode(loc)) else Result.failure(Exception("no location")))
                }
                LocationManagerCompat.getCurrentLocation(lm, provider(), cancel,
                    androidx.core.content.ContextCompat.getMainExecutor(rt.app), androidx.core.util.Consumer<Location?> { answer(it) })
                android.os.Handler(Looper.getMainLooper()).postDelayed({ cancel.cancel(); answer(null) }, 10_000)
            }
        },
        "next" to { _, reply ->
            rt.withPermission(perms) { ok ->
                AsukaLocationAccess.asked(rt.app)
                if (!ok) { reply(Result.failure(Exception("denied"))); return@withPermission }
                val u = unseen
                if (u != null) { unseen = null; reply(Result.success(encode(u))) } else next.add(reply)
                if (!updating) {
                    updating = true
                    lm.requestLocationUpdates(provider(), 1000L, 0f, listener, Looper.getMainLooper())
                }
            }
        },
        "stop" to { _, reply ->
            updating = false
            unseen = null
            lm.removeUpdates(listener)
            val ws = ArrayList(next)
            next.clear()
            for (w in ws) w(Result.failure(Exception("stopped")))
            reply(Result.success(ByteArray(0)))
        },
    )
}

/**
 * asuka/sdk/location's authorization (locationspec's "locationAccess"):
 * "current", "next" (args: the last state; answered once it differs) and
 * "stop" follow it, three bytes: level (0 not asked, 1 denied, 3 when in
 * use, 4 always; Android has no restricted), precise (fine location),
 * off (location turned off); "request" (u8: 3 when in use, 4 always) asks,
 * the background permission on its own after the foreground ones (Android
 * 11 takes the user to the app's location settings for it); "precise"
 * asks to change approximate to precise; "settings" opens the app's
 * settings. Android has no callback for a permission the user changes in
 * Settings: it's read again as the app comes back (an Activity resumes),
 * and as location is turned on or off.
 */
class AsukaLocationAccess(private val rt: AsukaRuntime) {
    private val lm = rt.app.getSystemService(Context.LOCATION_SERVICE) as LocationManager
    private val waits = ArrayList<Pair<ByteArray, (Result<ByteArray>) -> Unit>>()
    private var asks = 0

    companion object {
        private const val PREFS = "asuka.location"
        private const val FINE = Manifest.permission.ACCESS_FINE_LOCATION
        private const val COARSE = Manifest.permission.ACCESS_COARSE_LOCATION
        private const val BACKGROUND = Manifest.permission.ACCESS_BACKGROUND_LOCATION
        var current: AsukaLocationAccess? = null

        /** The user was asked: from then on, no permission is Denied, not NotAsked. */
        fun asked(ctx: Context) {
            ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putBoolean("asked", true).apply()
            current?.changed()
        }

        fun granted(ctx: Context, p: String) = androidx.core.content.ContextCompat.checkSelfPermission(ctx, p) == android.content.pm.PackageManager.PERMISSION_GRANTED
        fun foreground(ctx: Context) = granted(ctx, FINE) || granted(ctx, COARSE)
        fun background(ctx: Context) = foreground(ctx) && granted(ctx, BACKGROUND)
    }

    init {
        current = this
        rt.app.registerActivityLifecycleCallbacks(object : android.app.Application.ActivityLifecycleCallbacks {
            override fun onActivityResumed(a: android.app.Activity) = changed()
            override fun onActivityCreated(a: android.app.Activity, b: android.os.Bundle?) {}
            override fun onActivityStarted(a: android.app.Activity) {}
            override fun onActivityPaused(a: android.app.Activity) {}
            override fun onActivityStopped(a: android.app.Activity) {}
            override fun onActivitySaveInstanceState(a: android.app.Activity, b: android.os.Bundle) {}
            override fun onActivityDestroyed(a: android.app.Activity) {}
        })
        androidx.core.content.ContextCompat.registerReceiver(rt.app, object : android.content.BroadcastReceiver() {
            override fun onReceive(c: Context, i: android.content.Intent) = changed()
        }, android.content.IntentFilter(LocationManager.MODE_CHANGED_ACTION), androidx.core.content.ContextCompat.RECEIVER_NOT_EXPORTED)
    }

    fun state(): ByteArray {
        val ctx = rt.app
        val level = when {
            background(ctx) -> 4
            foreground(ctx) -> 3
            ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean("asked", false) -> 1
            else -> 0
        }
        return byteArrayOf(level.toByte(), if (granted(ctx, FINE)) 1 else 0, if (LocationManagerCompat.isLocationEnabled(lm)) 0 else 1)
    }

    /** The state may have changed: the followers whose last state it isn't get it. Main thread. */
    fun changed() {
        val s = state()
        val ws = waits.filter { !it.first.contentEquals(s) }
        waits.removeAll(ws.toSet())
        for (w in ws) w.second(Result.success(s))
    }

    /** Asks for permissions, whether or not one is granted already, then calls done. Main thread. */
    private fun ask(permissions: Array<String>, done: () -> Unit) {
        val act = rt.activity ?: run { done(); return }
        var launcher: androidx.activity.result.ActivityResultLauncher<Array<String>>? = null
        launcher = act.activityResultRegistry.register("asuka-location-${asks++}", androidx.activity.result.contract.ActivityResultContracts.RequestMultiplePermissions()) {
            launcher?.unregister()
            asked(rt.app)
            done()
        }
        launcher.launch(permissions)
    }

    /** Asks for level (3, 4), then gives the state. Main thread. */
    fun request(level: Int, done: (ByteArray) -> Unit) {
        val ctx = rt.app
        when {
            !foreground(ctx) -> ask(arrayOf(FINE, COARSE)) {
                if (level == 4 && foreground(ctx)) request(4, done) else done(state())
            }
            level == 4 && !granted(ctx, BACKGROUND) -> ask(arrayOf(BACKGROUND)) { done(state()) }
            else -> done(state())
        }
    }

    fun methods(): Map<String, AsukaModuleMethod> = mapOf(
        "current" to { _, reply -> reply(Result.success(state())) },
        "next" to { args, reply ->
            val s = state()
            if (args.isEmpty() || !args.contentEquals(s)) reply(Result.success(s)) else waits.add(args to reply)
        },
        "stop" to { _, reply ->
            val ws = ArrayList(waits)
            waits.clear()
            for (w in ws) w.second(Result.failure(Exception("stopped")))
            reply(Result.success(ByteArray(0)))
        },
        "request" to { args, reply -> request(args.firstOrNull()?.toInt() ?: 3) { reply(Result.success(it)) } },
        "precise" to { _, reply ->
            if (foreground(rt.app) && !granted(rt.app, FINE)) ask(arrayOf(FINE, COARSE)) { reply(Result.success(state())) }
            else reply(Result.success(state()))
        },
        "settings" to { _, reply ->
            val i = android.content.Intent(android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS, android.net.Uri.fromParts("package", rt.app.packageName, null))
                .addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK)
            runCatching { (rt.activity ?: rt.app).startActivity(i) }
            reply(Result.success(ByteArray(0)))
        },
    )
}
