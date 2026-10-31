package dev.ryui

import android.Manifest
import android.annotation.SuppressLint
import android.content.Context
import android.location.Location
import android.location.LocationListener
import android.location.LocationManager
import android.os.Build
import android.os.Looper
import androidx.core.location.LocationManagerCompat
import org.json.JSONObject

/**
 * ryui/location's module: the position now ("current"), and each next one
 * while watching ("next", "stop"), from Android's location manager (no
 * Play services). Replies are JSON: lat, lon, acc (meters), alt, t (Unix
 * ms); "denied" fails when the user hasn't allowed it.
 */
@SuppressLint("MissingPermission") // asked for first: withPermission
class RyUILocation(private val rt: RyUIRuntime) {
    private val lm = rt.app.getSystemService(Context.LOCATION_SERVICE) as LocationManager
    private val perms = arrayOf(Manifest.permission.ACCESS_FINE_LOCATION, Manifest.permission.ACCESS_COARSE_LOCATION)
    private val next = ArrayList<(Result<ByteArray>) -> Unit>()
    private var unseen: Location? = null
    private var updating = false
    private val listener = LocationListener { l -> got(l) }

    private fun provider(): String = when {
        Build.VERSION.SDK_INT >= 31 && lm.hasProvider(LocationManager.FUSED_PROVIDER) -> LocationManager.FUSED_PROVIDER
        lm.isProviderEnabled(LocationManager.GPS_PROVIDER) -> LocationManager.GPS_PROVIDER
        else -> LocationManager.NETWORK_PROVIDER
    }

    /** The newest fix any provider has. */
    private fun newest(): Location? = lm.getProviders(true).mapNotNull { runCatching { lm.getLastKnownLocation(it) }.getOrNull() }.maxByOrNull { it.time }

    private fun encode(l: Location): ByteArray = JSONObject(mapOf(
        "lat" to l.latitude, "lon" to l.longitude, "acc" to l.accuracy.toDouble(),
        "alt" to if (l.hasAltitude()) l.altitude else 0.0, "t" to l.time,
    )).toString().toByteArray()

    private fun got(l: Location) {
        if (!updating) return
        if (next.isEmpty()) { unseen = l; return }
        val ws = ArrayList(next)
        next.clear()
        val data = encode(l)
        for (w in ws) w(Result.success(data))
    }

    fun methods(): Map<String, RyUIModuleMethod> = mapOf(
        "current" to { _, reply ->
            rt.withPermission(perms) { ok ->
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
