package dev.ryui

import android.app.AlarmManager
import android.app.PendingIntent
import android.appwidget.AppWidgetManager
import android.appwidget.AppWidgetProvider
import android.content.ComponentName
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.ColorStateList
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.SystemClock
import android.util.SizeF
import android.view.View
import android.widget.RemoteViews
import org.json.JSONArray
import org.json.JSONObject

/**
 * The App Widget of a kind ryui.json declares: the CLI makes a subclass
 * for each (dev.ryui.app.widgets), a receiver in the manifest with the
 * kind in its meta-data (dev.ryui.widget.kind). It shows the kind's
 * entries, which ryui/widget's "set" keeps in the app's preferences: the
 * one whose time has come, laid out for the widget's size (small, medium,
 * large; Android 12's responsive layouts, or before, the size the
 * launcher reports), and wakes for the next one, and for a countdown's
 * end, with an alarm.
 */
open class RyUIWidgetProvider(private val kind: String, private val sizes: String) : AppWidgetProvider() {
    override fun onUpdate(ctx: Context, mgr: AppWidgetManager, ids: IntArray) {
        for (id in ids) mgr.updateAppWidget(id, views(ctx, mgr, id))
        schedule(ctx, ids)
    }

    override fun onAppWidgetOptionsChanged(ctx: Context, mgr: AppWidgetManager, id: Int, options: Bundle) {
        mgr.updateAppWidget(id, views(ctx, mgr, id))
    }

    private fun views(ctx: Context, mgr: AppWidgetManager, id: Int): RemoteViews {
        val entry = RyUIWidgets.current(RyUIWidgets.entries(ctx, kind))
        val ss = sizes.split(',')
        fun layout(size: String) = RyUIWidgets.render(ctx, kind, entry, size)
        if (Build.VERSION.SDK_INT >= 31) {
            // Each size it comes in, from the smallest box that takes it.
            val m = LinkedHashMap<SizeF, RemoteViews>()
            if ("small" in ss) m[SizeF(40f, 40f)] = layout("small")
            if ("medium" in ss) m[SizeF(200f, 40f)] = layout("medium")
            if ("large" in ss) m[SizeF(200f, 200f)] = layout("large")
            if (m.isEmpty()) m[SizeF(40f, 40f)] = layout("small")
            return RemoteViews(m)
        }
        val o = mgr.getAppWidgetOptions(id)
        val w = o.getInt(AppWidgetManager.OPTION_APPWIDGET_MIN_WIDTH)
        val h = o.getInt(AppWidgetManager.OPTION_APPWIDGET_MAX_HEIGHT)
        val size = when {
            "large" in ss && w >= 200 && h >= 200 -> "large"
            "medium" in ss && w >= 200 -> "medium"
            "small" in ss -> "small"
            else -> ss.firstOrNull() ?: "small"
        }
        return layout(size)
    }

    /** An alarm for the next entry's time, or a countdown's end: then it updates again. */
    private fun schedule(ctx: Context, ids: IntArray) {
        val now = System.currentTimeMillis()
        val es = RyUIWidgets.entries(ctx, kind)
        val next = listOfNotNull(
            es.map { it.optLong("atMs") }.filter { it > now }.minOrNull(),
            RyUIWidgets.current(es)?.optLong("untilMs")?.takeIf { it > now },
        ).minOrNull()
        val am = ctx.getSystemService(Context.ALARM_SERVICE) as AlarmManager
        val pi = PendingIntent.getBroadcast(ctx, kind.hashCode(),
            Intent(ctx, javaClass).setAction(AppWidgetManager.ACTION_APPWIDGET_UPDATE).putExtra(AppWidgetManager.EXTRA_APPWIDGET_IDS, ids),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        if (next == null) am.cancel(pi) else am.set(AlarmManager.RTC, next, pi)
    }
}

/** ryui/widget's entries, and the template. */
object RyUIWidgets {
    private const val PREFS = "ryui.widgets"

    fun entries(ctx: Context, kind: String): List<JSONObject> {
        val s = ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getString(kind, null) ?: return emptyList()
        val a = runCatching { JSONArray(s) }.getOrNull() ?: return emptyList()
        return List(a.length()) { a.getJSONObject(it) }.sortedBy { it.optLong("atMs") }
    }

    /** The entry whose time has come: the last at or before now (or the first). */
    fun current(es: List<JSONObject>): JSONObject? {
        val now = System.currentTimeMillis()
        return es.lastOrNull { it.optLong("atMs") <= now } ?: es.firstOrNull()
    }

    /** "#rrggbbaa" as an ARGB int. */
    private fun color(s: String): Int? {
        if (s.length != 9 || !s.startsWith("#")) return null
        val v = s.substring(1).toLongOrNull(16) ?: return null
        return ((v and 0xff) shl 24 or (v ushr 8)).toInt()
    }

    fun render(ctx: Context, kind: String, e: JSONObject?, size: String): RemoteViews {
        val layout = when (size) { "large" -> R.layout.ryui_widget_large; "medium" -> R.layout.ryui_widget_medium; else -> R.layout.ryui_widget_small }
        val v = RemoteViews(ctx.packageName, layout)
        fun text(id: Int, s: String?) {
            if (s.isNullOrEmpty()) v.setViewVisibility(id, View.GONE)
            else { v.setViewVisibility(id, View.VISIBLE); v.setTextViewText(id, s) }
        }
        if (e == null) {
            // Nothing set yet: the widget's name, as the gallery shows it.
            val label = runCatching {
                AppWidgetManager.getInstance(ctx).getInstalledProvidersForPackage(ctx.packageName, null)
                    .firstOrNull { kindOf(ctx, it.provider) == kind }?.loadLabel(ctx.packageManager)
            }.getOrNull()
            text(R.id.ryui_widget_title, label ?: kind)
            text(R.id.ryui_widget_value, "—")
            for (id in listOf(R.id.ryui_widget_body, R.id.ryui_widget_caption, R.id.ryui_widget_symbol, R.id.ryui_widget_progress, R.id.ryui_widget_until)) {
                v.setViewVisibility(id, View.GONE)
            }
            v.setOnClickPendingIntent(R.id.ryui_widget_root, open(ctx, kind, ""))
            return v
        }
        text(R.id.ryui_widget_title, e.optString("title"))
        text(R.id.ryui_widget_value, e.optString("value"))
        text(R.id.ryui_widget_body, e.optString("body"))
        text(R.id.ryui_widget_caption, e.optString("caption"))
        val tint = color(e.optString("tint")) ?: ctx.getColor(R.color.ryui_widget_accent)
        v.setTextColor(R.id.ryui_widget_title, tint)
        val sym = symbolDrawables[e.optString("symbol")]
        if (sym == null) v.setViewVisibility(R.id.ryui_widget_symbol, View.GONE)
        else {
            v.setViewVisibility(R.id.ryui_widget_symbol, View.VISIBLE)
            v.setImageViewResource(R.id.ryui_widget_symbol, sym)
            v.setInt(R.id.ryui_widget_symbol, "setColorFilter", tint)
        }
        if (e.optBoolean("showProgress")) {
            v.setViewVisibility(R.id.ryui_widget_progress, View.VISIBLE)
            v.setProgressBar(R.id.ryui_widget_progress, 1000, (e.optDouble("progress", 0.0).coerceIn(0.0, 1.0) * 1000).toInt(), false)
            if (Build.VERSION.SDK_INT >= 31) v.setColorStateList(R.id.ryui_widget_progress, "setProgressTintList", ColorStateList.valueOf(tint))
        } else v.setViewVisibility(R.id.ryui_widget_progress, View.GONE)
        val until = e.optLong("untilMs")
        if (until > 0) {
            // Down to it, then up from it (the alarm at its end switches).
            val left = until - System.currentTimeMillis()
            v.setViewVisibility(R.id.ryui_widget_until, View.VISIBLE)
            v.setChronometer(R.id.ryui_widget_until, SystemClock.elapsedRealtime() + left, null, true)
            v.setChronometerCountDown(R.id.ryui_widget_until, left > 0)
        } else v.setViewVisibility(R.id.ryui_widget_until, View.GONE)
        v.setOnClickPendingIntent(R.id.ryui_widget_root, open(ctx, kind, e.optString("url")))
        return v
    }

    /** Opens the app, with the entry's URL (ui.OnOpenURL) if it has one. */
    private fun open(ctx: Context, kind: String, url: String): PendingIntent {
        val i = Intent(ctx, RyUIActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP)
        if (url.isNotEmpty()) i.setAction(Intent.ACTION_VIEW).setData(Uri.parse(url)) else i.setAction(Intent.ACTION_MAIN)
        return PendingIntent.getActivity(ctx, ("ryui.widget." + kind).hashCode(), i, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
    }

    /** A provider's kind: its receiver's dev.ryui.widget.kind. */
    fun kindOf(ctx: Context, provider: ComponentName): String? = runCatching {
        ctx.packageManager.getReceiverInfo(provider, PackageManager.GET_META_DATA).metaData?.getString("dev.ryui.widget.kind")
    }.getOrNull()

    /** Keeps kind's entries and updates its widgets. */
    fun set(ctx: Context, kind: String, entries: JSONArray) {
        ctx.getSharedPreferences(PREFS, Context.MODE_PRIVATE).edit().putString(kind, entries.toString()).apply()
        val mgr = AppWidgetManager.getInstance(ctx)
        for (p in mgr.getInstalledProvidersForPackage(ctx.packageName, null)) {
            if (kindOf(ctx, p.provider) != kind) continue
            val ids = mgr.getAppWidgetIds(p.provider)
            ctx.sendBroadcast(Intent(AppWidgetManager.ACTION_APPWIDGET_UPDATE).setComponent(p.provider)
                .putExtra(AppWidgetManager.EXTRA_APPWIDGET_IDS, ids))
        }
    }
}

/** ryui/widget's module: "set" (JSON: kind, entries). */
fun RyUIRuntime.widgetMethods(): Map<String, RyUIModuleMethod> = mapOf(
    "set" to { args, reply ->
        val o = runCatching { JSONObject(String(args, Charsets.UTF_8)) }.getOrNull()
        if (o == null) reply(Result.failure(Exception("bad args")))
        else {
            RyUIWidgets.set(app, o.getString("kind"), o.optJSONArray("entries") ?: JSONArray())
            reply(Result.success(ByteArray(0)))
        }
    },
)
