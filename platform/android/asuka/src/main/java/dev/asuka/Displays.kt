package dev.asuka

import android.content.Context
import android.hardware.display.DisplayManager
import android.os.Handler
import android.os.Looper
import android.view.Display
import android.view.WindowManager

/**
 * A module's watching of a state (internal/follow's side): "next" (args:
 * the state last given) is answered once the state isn't that one, or
 * with the events queued since (they follow the state); events are
 * queued from the first "next" until "stop", which answers the waiting
 * ones "stopped". start and end begin and end observing.
 */
internal class AsukaWatch(
    private val state: () -> ByteArray,
    private val start: () -> Unit = {},
    private val end: () -> Unit = {},
) {
    private val waiting = ArrayList<Pair<ByteArray, (Result<ByteArray>) -> Unit>>()
    private val events = ArrayList<Byte>()
    var watching = false
        private set

    fun next(args: ByteArray, reply: (Result<ByteArray>) -> Unit) {
        if (!watching) {
            watching = true
            start()
        }
        waiting.add(args to reply)
        changed()
    }

    /** Queues an event while watched. */
    fun event(e: Byte) {
        if (!watching) return
        events.add(e)
        changed()
    }

    /** Answers the waiting ones the state (and events) are new to. */
    fun changed() {
        if (waiting.isEmpty()) return
        val s = state()
        val answer = waiting.filter { events.isNotEmpty() || !it.first.contentEquals(s) }
        if (answer.isEmpty()) return
        val out = s + events.toByteArray()
        events.clear()
        waiting.removeAll(answer.toSet())
        for ((_, reply) in answer) reply(Result.success(out))
    }

    fun stop(reply: (Result<ByteArray>) -> Unit) {
        if (watching) {
            watching = false
            end()
        }
        events.clear()
        val ws = ArrayList(waiting)
        waiting.clear()
        for ((_, w) in ws) w(Result.failure(Exception("stopped")))
        reply(Result.success(ByteArray(0)))
    }

    fun methods(): Map<String, AsukaModuleMethod> = mapOf(
        "current" to { _, reply -> reply(Result.success(state())) },
        "next" to { args, reply -> next(args, reply) },
        "stop" to { _, reply -> stop(reply) },
    )
}

/** A number as JSON, the same each time: whole, or to three places. */
internal fun jsonNumber(v: Float): String {
    if (v.isNaN() || v.isInfinite()) return "0"
    val r = Math.round(v * 1000.0) / 1000.0
    return if (r == Math.rint(r)) r.toLong().toString() else java.math.BigDecimal(r).setScale(3, java.math.RoundingMode.HALF_UP).stripTrailingZeros().toPlainString()
}

/**
 * asuka/screen's displays: "current" (a JSON list as screen.Display's,
 * the default display first, in dp), and, while watching, "next" and
 * "stop", from DisplayManager and its listener. A display's work area is
 * its bounds: the system bars are the app's insets (ui.UseEnvironment).
 */
class AsukaDisplays(private val rt: AsukaRuntime) {
    private val dm = rt.app.getSystemService(Context.DISPLAY_SERVICE) as DisplayManager
    private val main = Handler(Looper.getMainLooper())

    private val listener = object : DisplayManager.DisplayListener {
        override fun onDisplayAdded(id: Int) = watch.changed()
        override fun onDisplayRemoved(id: Int) = watch.changed()
        override fun onDisplayChanged(id: Int) = watch.changed()
    }
    private val watch: AsukaWatch = AsukaWatch(::state, { dm.registerDisplayListener(listener, main) }, { dm.unregisterDisplayListener(listener) })

    private fun state(): ByteArray {
        val all = dm.displays.sortedBy { if (it.displayId == Display.DEFAULT_DISPLAY) -1 else it.displayId }
        val sb = StringBuilder("[")
        var x = 0f
        for ((i, d) in all.withIndex()) {
            val ctx = rt.app.createDisplayContext(d)
            val density = ctx.resources.displayMetrics.density
            val wm = ctx.getSystemService(WindowManager::class.java)
            val px = wm?.maximumWindowMetrics?.bounds
            val w = (px?.width() ?: ctx.resources.displayMetrics.widthPixels) / density
            val h = (px?.height() ?: ctx.resources.displayMetrics.heightPixels) / density
            // The default display at the origin, the others after it.
            val rect = "{\"x\":${jsonNumber(x)},\"y\":0,\"width\":${jsonNumber(w)},\"height\":${jsonNumber(h)}}"
            x += w
            if (i > 0) sb.append(',')
            sb.append("{\"id\":").append(org.json.JSONObject.quote(d.displayId.toString()))
                .append(",\"name\":").append(org.json.JSONObject.quote(d.name ?: ""))
                .append(",\"bounds\":").append(rect).append(",\"workArea\":").append(rect)
                .append(",\"scale\":").append(jsonNumber(density))
                .append(",\"refreshRate\":").append(jsonNumber(d.refreshRate))
                .append(",\"primary\":").append(d.displayId == Display.DEFAULT_DISPLAY).append('}')
        }
        return sb.append(']').toString().toByteArray()
    }

    fun methods() = watch.methods()
}
