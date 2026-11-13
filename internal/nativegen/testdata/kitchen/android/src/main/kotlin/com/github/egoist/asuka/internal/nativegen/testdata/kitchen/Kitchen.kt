// The kitchen package's Kotlin on Android: what its Go declares,
// implemented (the GUI tests' plugin app calls it).
package com.github.egoist.asuka.internal.nativegen.testdata.kitchen

import android.view.View
import android.widget.SeekBar
import dev.asuka.AsukaModules
import dev.asuka.AsukaUnsupportedException
import java.time.Instant
import kotlin.time.Duration.Companion.seconds
import kotlinx.coroutines.delay

private class KotlinKitchen : Kitchen {
    override fun name() = "home"
    override fun items(filter: String) = listOf(
        Item(name = filter, weight = 1.5, unit = Unit.GRAM, tags = listOf("a", "b"), notes = mapOf("k" to "v"),
            best = Instant.ofEpochSecond(1_700_000_000, 250_000_000), next = Item(name = "inner"),
            data = byteArrayOf(1, 2, 3), took = 2.5.seconds, id = ULong.MAX_VALUE, count = -3, ratio = 0.5f, fresh = true),
    )
    override fun add(item: Item) {
        if (item.name.isEmpty()) throw KitchenException(KitchenError.BUSY)
    }
    override suspend fun cook(recipe: String, progress: (Double) -> kotlin.Unit): Item {
        if (recipe == "slow") delay(10_000)
        progress(0.5)
        progress(1.0)
        return Item(name = "$recipe done")
    }
    override fun ask(question: (String) -> Boolean) = question("ok?")
    override fun open(name: String): Oven {
        if (name == "none") throw IllegalStateException("no oven")
        return AsukaModules.onMain { KotlinOven() }
    }
    override fun show() {}
    override fun level() = Level.HIGH
    override fun counts() = mapOf("a" to 1L, "b" to -2L)
    override fun `when`(at: Instant, every: kotlin.time.Duration): Instant? =
        if (every == kotlin.time.Duration.ZERO) null else at.plusNanos(every.inWholeNanoseconds)
}

private class KotlinOven : Oven {
    override fun close() {}
    override fun heat(to: Float, unit: Unit) {
        if (unit == Unit.OUNCE) throw AsukaUnsupportedException("no ounces")
    }
}

fun makeKitchen(): Kitchen = KotlinKitchen()
fun makeOven(): Oven = KotlinOven()

private class KotlinDial(private val events: DialEvents) : Dial {
    private val bar = SeekBar(events.context).apply { max = 100 }
    override val view: View get() = bar
    override fun setValue(value: Double) {
        bar.progress = (value * 100).toInt()
        events.changed(value * 2)
    }
    override fun setLabel(value: String) { bar.contentDescription = value }
    override fun setItems(value: List<Item>) {
        value.lastOrNull()?.let { events.picked(it, (value.size - 1).toLong()) }
    }
}

fun makeDial(events: DialEvents): Dial = KotlinDial(events)
