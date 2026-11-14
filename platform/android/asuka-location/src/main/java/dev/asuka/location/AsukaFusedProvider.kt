package dev.asuka.location

import android.annotation.SuppressLint
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import com.google.android.gms.common.ConnectionResult
import com.google.android.gms.common.GoogleApiAvailability
import com.google.android.gms.location.Geofence
import com.google.android.gms.location.GeofenceStatusCodes
import com.google.android.gms.location.GeofencingEvent
import com.google.android.gms.location.GeofencingRequest
import com.google.android.gms.location.LocationRequest
import com.google.android.gms.location.LocationResult
import com.google.android.gms.location.LocationServices
import com.google.android.gms.location.Priority
import dev.asuka.AsukaLocation
import dev.asuka.AsukaLocationIntents
import dev.asuka.AsukaLocationProvider
import dev.asuka.AsukaLocationRead
import org.json.JSONObject

/**
 * Google Play services' Fused Location Provider (batched with a max
 * delay) and GeofencingClient (100 geofences an app, dwells reported),
 * both delivering to the host's PendingIntents. Found by name
 * (AsukaLocationProviders), used where Play services are.
 */
@SuppressLint("MissingPermission") // checked first: the module's start and monitor
class AsukaFusedProvider : AsukaLocationProvider {
    override val limit = 100
    override val dwell = true

    override fun usable(ctx: Context) =
        GoogleApiAvailability.getInstance().isGooglePlayServicesAvailable(ctx) == ConnectionResult.SUCCESS

    override fun requestUpdates(ctx: Context, u: JSONObject, pi: PendingIntent) {
        val significant = u.optBoolean("significant")
        val priority = when (if (significant) 2 else u.optInt("accuracy")) {
            0, 1 -> Priority.PRIORITY_HIGH_ACCURACY
            2 -> Priority.PRIORITY_BALANCED_POWER_ACCURACY
            3 -> Priority.PRIORITY_LOW_POWER
            else -> Priority.PRIORITY_PASSIVE
        }
        val interval = u.optLong("interval").takeIf { it > 0 } ?: if (significant) 5 * 60_000L else 10_000L
        val distance = if (significant) maxOf(u.optDouble("distance", 0.0), 500.0) else u.optDouble("distance", 0.0)
        val req = LocationRequest.Builder(priority, interval)
            .setMinUpdateDistanceMeters(distance.toFloat())
            .setMaxUpdateDelayMillis(u.optLong("defer"))
            .setWaitForAccurateLocation(u.optInt("accuracy") == 1)
            .build()
        LocationServices.getFusedLocationProviderClient(ctx).requestLocationUpdates(req, pi)
    }

    override fun removeUpdates(ctx: Context, pi: PendingIntent) {
        LocationServices.getFusedLocationProviderClient(ctx).removeLocationUpdates(pi)
    }

    override fun addRegions(ctx: Context, rs: List<JSONObject>, initial: Boolean, done: (Exception?) -> Unit) {
        if (rs.isEmpty()) { done(null); return }
        var trigger = 0
        val b = GeofencingRequest.Builder()
        for (r in rs) {
            val events = r.optInt("events", 3)
            var types = Geofence.GEOFENCE_TRANSITION_ENTER or Geofence.GEOFENCE_TRANSITION_EXIT // both, to know where it is
            if (events and 4 != 0) types = types or Geofence.GEOFENCE_TRANSITION_DWELL
            val g = Geofence.Builder()
                .setRequestId(r.getString("id"))
                .setCircularRegion(r.getDouble("lat"), r.getDouble("lon"), r.getDouble("radius").toFloat())
                .setExpirationDuration(Geofence.NEVER_EXPIRE)
                .setTransitionTypes(types)
                .setLoiteringDelay(r.optLong("dwell", 300_000L).coerceIn(0, Int.MAX_VALUE.toLong()).toInt())
                .build()
            b.addGeofence(g)
            if (initial) trigger = trigger or GeofencingRequest.INITIAL_TRIGGER_ENTER or
                (if (events and 4 != 0) GeofencingRequest.INITIAL_TRIGGER_DWELL else 0)
        }
        b.setInitialTrigger(trigger)
        LocationServices.getGeofencingClient(ctx).addGeofences(b.build(), AsukaLocationIntents.regions(ctx))
            .addOnSuccessListener { done(null) }
            .addOnFailureListener { done(it) }
    }

    override fun removeRegions(ctx: Context, ids: List<String>?) {
        val c = LocationServices.getGeofencingClient(ctx)
        if (ids == null) c.removeGeofences(AsukaLocationIntents.regions(ctx)) else c.removeGeofences(ids)
    }

    override fun read(ctx: Context, intent: Intent): AsukaLocationRead? {
        if (LocationResult.hasResult(intent)) {
            val r = LocationResult.extractResult(intent) ?: return null
            return AsukaLocationRead(positions = r.locations.map { AsukaLocation.position(it) })
        }
        val e = GeofencingEvent.fromIntent(intent) ?: return null
        if (e.hasError()) return AsukaLocationRead(lost = e.errorCode == GeofenceStatusCodes.GEOFENCE_NOT_AVAILABLE)
        val at = e.triggeringLocation?.let { AsukaLocation.position(it) }
        val kind = when (e.geofenceTransition) {
            Geofence.GEOFENCE_TRANSITION_ENTER -> 1
            Geofence.GEOFENCE_TRANSITION_EXIT -> 2
            Geofence.GEOFENCE_TRANSITION_DWELL -> 4
            else -> return null
        }
        return AsukaLocationRead(crossings = (e.triggeringGeofences ?: emptyList()).map { Triple(it.requestId, kind, at) })
    }
}
