package dev.ryui

import android.Manifest
import android.content.ContentUris
import android.content.Intent
import android.content.pm.PackageManager
import android.provider.CalendarContract
import androidx.activity.result.ActivityResultLauncher
import androidx.activity.result.contract.ActivityResultContracts
import org.json.JSONArray
import org.json.JSONObject

private var calendarLaunches = 0

/**
 * ryui/calendar's module, with CalendarContract: "request" (u8 access: 1
 * denied, 3 full), "calendars" and "events" (JSON: from, to in ms,
 * calendars; each occurrence, from the Instances table) answer JSON;
 * "add" (an event's JSON) opens the calendar app's editor and answers u8
 * 0 once the user is back (calendar apps don't say whether they saved).
 */
fun RyUIRuntime.calendarMethods(): Map<String, RyUIModuleMethod> {
    fun allowed() = app.checkSelfPermission(Manifest.permission.READ_CALENDAR) == PackageManager.PERMISSION_GRANTED
    return mapOf(
        "request" to { _, reply ->
            withPermission(arrayOf(Manifest.permission.READ_CALENDAR)) { ok -> reply(Result.success(byteArrayOf(if (ok) 3 else 1))) }
        },
        "calendars" to { _, reply ->
            if (!allowed()) reply(Result.failure(Exception("not allowed")))
            else Thread {
                reply(runCatching {
                    val out = JSONArray()
                    app.contentResolver.query(CalendarContract.Calendars.CONTENT_URI, arrayOf(CalendarContract.Calendars._ID,
                        CalendarContract.Calendars.CALENDAR_DISPLAY_NAME, CalendarContract.Calendars.CALENDAR_COLOR,
                        CalendarContract.Calendars.ACCOUNT_NAME, CalendarContract.Calendars.CALENDAR_ACCESS_LEVEL),
                        "${CalendarContract.Calendars.VISIBLE} = 1", null, null)?.use { c ->
                        while (c.moveToNext()) {
                            out.put(JSONObject().put("id", c.getLong(0).toString()).put("title", c.getString(1) ?: "")
                                .put("color", String.format("#%06x", c.getInt(2) and 0xffffff)).put("source", c.getString(3) ?: "")
                                .put("writable", c.getInt(4) >= CalendarContract.Calendars.CAL_ACCESS_CONTRIBUTOR))
                        }
                    }
                    out.toString().toByteArray()
                })
            }.start()
        },
        "events" to { args, reply ->
            val o = runCatching { JSONObject(String(args, Charsets.UTF_8)) }.getOrDefault(JSONObject())
            if (!allowed()) reply(Result.failure(Exception("not allowed")))
            else Thread {
                reply(runCatching {
                    val ids = o.optJSONArray("calendars")?.let { a -> (0 until a.length()).map { a.getString(it) }.toSet() } ?: emptySet()
                    val out = JSONArray()
                    val uri = CalendarContract.Instances.CONTENT_URI.buildUpon().also {
                        ContentUris.appendId(it, o.optLong("from")); ContentUris.appendId(it, o.optLong("to"))
                    }.build()
                    app.contentResolver.query(uri, arrayOf(CalendarContract.Instances.EVENT_ID, CalendarContract.Instances.CALENDAR_ID,
                        CalendarContract.Instances.TITLE, CalendarContract.Instances.EVENT_LOCATION, CalendarContract.Instances.DESCRIPTION,
                        CalendarContract.Instances.BEGIN, CalendarContract.Instances.END, CalendarContract.Instances.ALL_DAY),
                        null, null, "${CalendarContract.Instances.BEGIN} ASC")?.use { c ->
                        while (c.moveToNext()) {
                            val cal = c.getLong(1).toString()
                            if (ids.isNotEmpty() && cal !in ids) continue
                            out.put(JSONObject().put("id", c.getLong(0).toString()).put("calendarId", cal).put("title", c.getString(2) ?: "")
                                .put("location", c.getString(3) ?: "").put("notes", c.getString(4) ?: "").put("url", "")
                                .put("startMs", c.getLong(5)).put("endMs", c.getLong(6)).put("allDay", c.getInt(7) == 1))
                        }
                    }
                    out.toString().toByteArray()
                })
            }.start()
        },
        "add" to { args, reply ->
            val act = activity
            val o = runCatching { JSONObject(String(args, Charsets.UTF_8)) }.getOrNull()
            if (act == null || o == null) reply(Result.failure(Exception(if (o == null) "bad args" else "no screen")))
            else {
                val i = Intent(Intent.ACTION_INSERT, CalendarContract.Events.CONTENT_URI)
                o.optString("title").takeIf { it.isNotEmpty() }?.let { i.putExtra(CalendarContract.Events.TITLE, it) }
                o.optString("location").takeIf { it.isNotEmpty() }?.let { i.putExtra(CalendarContract.Events.EVENT_LOCATION, it) }
                // Android has no URL field: with the notes.
                val notes = listOf(o.optString("notes"), o.optString("url")).filter { it.isNotEmpty() }.joinToString("\n\n")
                if (notes.isNotEmpty()) i.putExtra(CalendarContract.Events.DESCRIPTION, notes)
                val start = o.optLong("startMs"); val end = o.optLong("endMs")
                if (start > 0) {
                    i.putExtra(CalendarContract.EXTRA_EVENT_BEGIN_TIME, start)
                    i.putExtra(CalendarContract.EXTRA_EVENT_END_TIME, if (end > start) end else start + 3_600_000)
                }
                if (o.optBoolean("allDay")) i.putExtra(CalendarContract.EXTRA_EVENT_ALL_DAY, true)
                o.optString("calendarId").toLongOrNull()?.let { i.putExtra(CalendarContract.Events.CALENDAR_ID, it) }
                var l: ActivityResultLauncher<Intent>? = null
                l = act.activityResultRegistry.register("ryui-calendar-${calendarLaunches++}", ActivityResultContracts.StartActivityForResult()) { _ ->
                    l?.unregister()
                    reply(Result.success(byteArrayOf(0)))
                }
                try { l.launch(i) } catch (e: android.content.ActivityNotFoundException) { l.unregister(); reply(Result.failure(Exception("no calendar app"))) }
            }
        },
    )
}
