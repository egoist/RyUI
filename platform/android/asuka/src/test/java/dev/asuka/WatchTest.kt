package dev.asuka

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test

// A watched state: "next" answers once the state isn't the one given, or
// with the events queued since; "stop" answers the waiting ones.
class WatchTest {
    @Test
    fun nextAndEvents() {
        var state = byteArrayOf(1, 2)
        var starts = 0
        var ends = 0
        val w = AsukaWatch({ state }, { starts++ }, { ends++ })
        val got = ArrayList<Result<ByteArray>>()
        w.next(ByteArray(0)) { got.add(it) } // none given: at once
        assertArrayEquals(byteArrayOf(1, 2), got[0].getOrThrow())
        w.next(byteArrayOf(1, 2)) { got.add(it) } // the same: waits
        assertEquals(1, got.size)
        state = byteArrayOf(1, 3)
        w.changed()
        assertArrayEquals(byteArrayOf(1, 3), got[1].getOrThrow())
        w.event(3) // no one waits: queued
        w.next(byteArrayOf(1, 3)) { got.add(it) }
        assertArrayEquals(byteArrayOf(1, 3, 3), got[2].getOrThrow())
        w.next(byteArrayOf(1, 3)) { got.add(it) }
        w.stop {}
        assertEquals("stopped", got[3].exceptionOrNull()?.message)
        w.event(4) // not watched: dropped
        w.next(byteArrayOf(1, 3)) { got.add(it) }
        assertEquals(4, got.size)
        assertEquals(2, starts)
        assertEquals(1, ends)
    }

    @Test
    fun numbers() {
        assertEquals("390", jsonNumber(390f))
        assertEquals("2.625", jsonNumber(2.625f))
        assertEquals("59.94", jsonNumber(59.94f))
        assertEquals("0", jsonNumber(Float.NaN))
    }
}
