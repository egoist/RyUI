// The greeter package's Kotlin: its module and its view, on Android.
package com.github.egoist.asuka.e2e.testdata.greeter

import android.view.View
import android.widget.TextView
import com.google.gson.Gson

private class KotlinGreeter : Greeter {
    override fun hello(name: String): String {
        val json = Gson().toJson(mapOf("greeting" to "hello $name from Android"))
        return Gson().fromJson(json, Map::class.java)["greeting"] as String
    }
}

fun makeGreeter(): Greeter = KotlinGreeter()

/** A label showing its name; it sends shown with what it shows. */
private class KotlinBadge(private val events: BadgeEvents) : Badge {
    private val label = TextView(events.context)
    override val view: View get() = label

    override fun setName(value: String) {
        label.text = "Android badge for $value"
        events.shown(label.text.toString())
    }
}

fun makeBadge(events: BadgeEvents): Badge = KotlinBadge(events)
