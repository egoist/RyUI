package dev.asuka

import android.content.res.Configuration
import android.os.LocaleList
import java.util.Locale

/**
 * What the environment says of the user's languages and regional formats.
 * It takes a resource context to work out, so it's kept, and read again as
 * they change.
 */
data class AsukaRegion(val languages: String, val locale: String, val firstWeekday: Int, val hourCycle: Int)

/**
 * The system's locales, ignoring the app's own language: the regional formats
 * are the user's whatever language the app shows.
 */
private fun AsukaRuntime.systemLocales(): LocaleList {
    val list = app.getSystemService(android.app.LocaleManager::class.java)?.systemLocales
    return if (list == null || list.isEmpty) app.resources.configuration.locales else list
}

fun AsukaRuntime.readRegion(): AsukaRegion {
    val tags = LinkedHashSet<String>()
    // The configuration has the app's language first, if it has one.
    val config = app.resources.configuration.locales
    for (i in 0 until config.size()) tags.add(config[i].stripExtensions().toLanguageTag())
    val formats = systemLocales()[0]
    return AsukaRegion(tags.joinToString(","), formats.toLanguageTag(), firstWeekday(formats), hourCycle(formats))
}

/** 1 Sunday to 7 Saturday: what the user set (Android 14's regional preferences), else the locale's. */
private fun firstWeekday(l: Locale): Int =
    when (l.getUnicodeLocaleType("fw")) {
        "sun" -> 1
        "mon" -> 2
        "tue" -> 3
        "wed" -> 4
        "thu" -> 5
        "fri" -> 6
        "sat" -> 7
        else -> android.icu.util.Calendar.getInstance(l).firstDayOfWeek
    }

/** 12 or 24: the system's setting, or where it has none, what's usual in the formats' locale (not the app's language). */
private fun AsukaRuntime.hourCycle(l: Locale): Int {
    val c = app.createConfigurationContext(Configuration(app.resources.configuration).apply { setLocale(l) })
    return if (android.text.format.DateFormat.is24HourFormat(c)) 24 else 12
}
