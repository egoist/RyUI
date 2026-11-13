package dev.asuka

import android.media.session.PlaybackState
import org.json.JSONObject
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.ByteBuffer
import java.nio.ByteOrder

// asuka/nowplaying's wire, as the Go package writes and reads it: the
// session's actions are the commands enabled; "next" answers u8 command,
// f64 value each.
class NowPlayingTest {
    private fun bits(vararg cs: Int) = cs.fold(0L) { m, c -> m or (1L shl c) }

    @Test
    fun actions() {
        val a = AsukaNowPlaying.actions(bits(AsukaNowPlaying.PLAY, AsukaNowPlaying.PAUSE, AsukaNowPlaying.TOGGLE, AsukaNowPlaying.NEXT, AsukaNowPlaying.SEEK))
        assertEquals(
            PlaybackState.ACTION_PLAY or PlaybackState.ACTION_PAUSE or PlaybackState.ACTION_PLAY_PAUSE or
                PlaybackState.ACTION_SKIP_TO_NEXT or PlaybackState.ACTION_SEEK_TO,
            a,
        )
        val skips = AsukaNowPlaying.actions(bits(AsukaNowPlaying.FORWARD, AsukaNowPlaying.BACKWARD, AsukaNowPlaying.STOP, AsukaNowPlaying.PREVIOUS))
        assertEquals(
            PlaybackState.ACTION_FAST_FORWARD or PlaybackState.ACTION_REWIND or PlaybackState.ACTION_STOP or PlaybackState.ACTION_SKIP_TO_PREVIOUS,
            skips,
        )
        assertEquals(0L, AsukaNowPlaying.actions(0))
    }

    @Test
    fun info() {
        val i = AsukaNowPlaying.Info(JSONObject("""{"title":"Spring","duration":200,"position":12.5,"rate":1.5,"state":"playing","commands":${bits(1, 2, 7)},"skip":30}"""))
        assertEquals("Spring", i.title)
        assertEquals("", i.artist)
        assertEquals(200.0, i.duration, 0.0)
        assertEquals(12.5, i.position, 0.0)
        assertEquals(1.5, i.rate, 0.0)
        assertEquals("playing", i.state)
        assertTrue(i.has(AsukaNowPlaying.SEEK))
        assertFalse(i.has(AsukaNowPlaying.NEXT))
        assertEquals(30.0, i.skip, 0.0)
    }

    @Test
    fun encode() {
        val want = ByteBuffer.allocate(9).order(ByteOrder.LITTLE_ENDIAN).put(7).putDouble(42.0).array()
        assertArrayEquals(want, AsukaNowPlaying.encode(AsukaNowPlaying.SEEK, 42.0))
    }
}
