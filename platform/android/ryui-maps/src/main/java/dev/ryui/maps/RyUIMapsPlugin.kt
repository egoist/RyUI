package dev.ryui.maps

import android.content.Context
import android.graphics.Color
import android.view.View
import android.widget.FrameLayout
import com.google.android.gms.maps.CameraUpdateFactory
import com.google.android.gms.maps.GoogleMap
import com.google.android.gms.maps.MapView
import com.google.android.gms.maps.model.BitmapDescriptorFactory
import com.google.android.gms.maps.model.LatLng
import com.google.android.gms.maps.model.LatLngBounds
import com.google.android.gms.maps.model.Marker
import com.google.android.gms.maps.model.MarkerOptions
import dev.ryui.RyUINativeContext
import dev.ryui.RyUINativeView
import dev.ryui.RyUIPlugin
import dev.ryui.RyUIRuntime
import org.json.JSONArray
import org.json.JSONObject

/** maps.View on Android: Google Maps. */
class RyUIMapsPlugin : RyUIPlugin {
    override fun register(rt: RyUIRuntime) {
        rt.registerNative("ryui.map") { ctx -> RyUIMapView(ctx.context, ctx) }
    }
}

/**
 * A Google Maps MapView, its lifecycle following its window. Props:
 * region and markers (JSON), type (standard, satellite, hybrid, muted),
 * showsUser, interactive. Events: region (lat, lng, latDelta, lngDelta,
 * once the user moved it), marker (id), press (lat, lng).
 */
class RyUIMapView(context: Context, private val ctx: RyUINativeContext) : RyUINativeView {
    private val map = MapView(context)
    private val box = object : FrameLayout(context) {}
    /** The Maps SDK's key, which it throws without (ryui.json's android.mapsKey). */
    private val hasKey = runCatching {
        context.packageManager.getApplicationInfo(context.packageName, android.content.pm.PackageManager.GET_META_DATA)
            .metaData?.getString("com.google.android.geo.API_KEY").isNullOrEmpty().not()
    }.getOrDefault(false)
    override val view: View get() = box
    private var google: GoogleMap? = null
    private var region: JSONObject? = null
    private var markers = JSONArray()
    private var type = 0
    private var showsUser = false
    private var interactive = true
    private var userMoved = false
    private val ids = HashMap<Marker, String>()

    init {
        if (hasKey) start() else {
            // No key: say so where the map would be.
            box.addView(android.widget.TextView(context).apply {
                text = "Google Maps needs an API key: set ryui.json's android.mapsKey"
                gravity = android.view.Gravity.CENTER
                setPadding(32, 32, 32, 32)
            }, FrameLayout.LayoutParams(FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT))
            box.post { ctx.emit("error", mapOf("message" to "no Google Maps API key (ryui.json's android.mapsKey)")) }
        }
    }

    private fun start() {
        map.onCreate(null)
        box.addView(map, FrameLayout.LayoutParams(FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT))
        box.addOnAttachStateChangeListener(object : View.OnAttachStateChangeListener {
            override fun onViewAttachedToWindow(v: View) { map.onStart(); map.onResume() }
            override fun onViewDetachedFromWindow(v: View) { map.onPause(); map.onStop() }
        })
        map.getMapAsync { g ->
            google = g
            g.setOnCameraMoveStartedListener { reason -> userMoved = reason == GoogleMap.OnCameraMoveStartedListener.REASON_GESTURE }
            g.setOnCameraIdleListener {
                if (!userMoved) return@setOnCameraIdleListener
                userMoved = false
                val b = g.projection.visibleRegion.latLngBounds
                val c = g.cameraPosition.target
                ctx.emit("region", mapOf("lat" to c.latitude, "lng" to c.longitude,
                    "latDelta" to (b.northeast.latitude - b.southwest.latitude), "lngDelta" to (b.northeast.longitude - b.southwest.longitude)))
            }
            g.setOnMarkerClickListener { m -> ids[m]?.let { ctx.emit("marker", mapOf("id" to it)) }; false }
            g.setOnMapClickListener { p -> ctx.emit("press", mapOf("lat" to p.latitude, "lng" to p.longitude)) }
            apply(g, moveCamera = true)
        }
    }

    override fun set(name: String, value: Any) {
        when (name) {
            "region" -> { region = runCatching { JSONObject(value as String) }.getOrNull(); google?.let { moveTo(it, animated = true) } }
            "markers" -> { markers = runCatching { JSONArray(value as String) }.getOrDefault(JSONArray()); google?.let { placeMarkers(it) } }
            "type" -> { type = (value as Number).toInt(); google?.let { apply(it, false) } }
            "showsUser" -> { showsUser = value as Boolean; google?.let { apply(it, false) } }
            "interactive" -> { interactive = value as Boolean; google?.let { apply(it, false) } }
        }
    }

    private fun apply(g: GoogleMap, moveCamera: Boolean) {
        g.mapType = when (type) { 1 -> GoogleMap.MAP_TYPE_SATELLITE; 2 -> GoogleMap.MAP_TYPE_HYBRID; else -> GoogleMap.MAP_TYPE_NORMAL }
        g.uiSettings.setAllGesturesEnabled(interactive)
        // The user's location needs the permission (ryui/location's); without it, none.
        try { g.isMyLocationEnabled = showsUser } catch (_: SecurityException) {}
        placeMarkers(g)
        if (moveCamera) moveTo(g, animated = false)
    }

    private fun moveTo(g: GoogleMap, animated: Boolean) {
        val r = region ?: return
        val c = r.optJSONObject("center") ?: return
        val lat = c.optDouble("lat"); val lng = c.optDouble("lng")
        val dLat = maxOf(r.optDouble("latDelta", 0.05), 0.0005) / 2; val dLng = maxOf(r.optDouble("lngDelta", 0.05), 0.0005) / 2
        val bounds = LatLngBounds(LatLng(lat - dLat, lng - dLng), LatLng(lat + dLat, lng + dLng))
        // The region the user moved to, given back: already there.
        val now = g.cameraPosition.target
        if (Math.abs(now.latitude - lat) < 1e-6 && Math.abs(now.longitude - lng) < 1e-6) return
        val move = { if (animated) g.animateCamera(CameraUpdateFactory.newLatLngBounds(bounds, 0)) else g.moveCamera(CameraUpdateFactory.newLatLngBounds(bounds, 0)) }
        // Bounds need the map's size: once it's laid out.
        if (map.width > 0 && map.height > 0) move() else map.post { if (map.width > 0) move() else g.moveCamera(CameraUpdateFactory.newLatLngZoom(LatLng(lat, lng), 12f)) }
    }

    private fun placeMarkers(g: GoogleMap) {
        for (m in ids.keys) m.remove()
        ids.clear()
        for (i in 0 until markers.length()) {
            val o = markers.optJSONObject(i) ?: continue
            val at = o.optJSONObject("at") ?: continue
            val opts = MarkerOptions().position(LatLng(at.optDouble("lat"), at.optDouble("lng")))
            o.optString("title").takeIf { it.isNotEmpty() }?.let { opts.title(it) }
            o.optString("subtitle").takeIf { it.isNotEmpty() }?.let { opts.snippet(it) }
            o.optString("color").takeIf { it.length == 9 }?.let { hex ->
                // #rrggbbaa: its hue.
                val c = Color.parseColor("#" + hex.substring(1, 7))
                val hsv = FloatArray(3)
                Color.colorToHSV(c, hsv)
                opts.icon(BitmapDescriptorFactory.defaultMarker(hsv[0]))
            }
            g.addMarker(opts)?.let { ids[it] = o.optString("id") }
        }
    }
}
