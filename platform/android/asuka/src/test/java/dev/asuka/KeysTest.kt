package dev.asuka

import android.view.KeyEvent
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test
import java.nio.ByteBuffer

// AsukaKeys mirrors ops.DecodeKeys, ops.MatchKey and ops.KeyName, and
// names a KeyEvent's key as the Keys prop does.
class KeysTest {
    /** The Keys prop as ops.AppendKeys writes it. */
    private fun encode(keys: List<AsukaKey>): ByteArray {
        val w = AsukaWriter()
        w.u32(keys.size)
        for (k in keys) {
            w.string(k.name)
            w.u8(k.mods)
        }
        return w.bytes()
    }

    private fun decode(b: ByteArray) = AsukaKeys.decode(ByteBuffer.wrap(b))

    @Test
    fun decode() {
        val keys = listOf(AsukaKey("down", 0), AsukaKey("n", 8), AsukaKey("é", 1 or 2), AsukaKey("f12", 4))
        val b = encode(keys)
        assertEquals(keys, decode(b))
        // Cut short: the keys whole before the cut.
        assertEquals(keys.take(3), decode(b.copyOf(b.size - 1)))
        assertEquals(listOf(AsukaKey("down", 0)), decode(b.copyOf(4 + 4 + 4 + 1)))
        assertEquals(emptyList<AsukaKey>(), decode(b.copyOf(4 + 4 + 4))) // its modifiers cut off
        assertEquals(emptyList<AsukaKey>(), decode(ByteArray(0)))
        assertEquals(emptyList<AsukaKey>(), decode(byteArrayOf(1, 0)))
        // A count larger than the bytes could hold.
        assertEquals(emptyList<AsukaKey>(), decode(byteArrayOf(-1, -1, -1, 0x7f)))
    }

    @Test
    fun match() {
        val keys = listOf(AsukaKey("down", 0), AsukaKey("up", 0), AsukaKey("k", 1), AsukaKey("k", 1 or 2), AsukaKey("n", 8), AsukaKey("x", 4))
        assertEquals(0, AsukaKeys.match(keys, "down", 0))
        assertEquals(1, AsukaKeys.match(keys, "up", 0))
        assertEquals(-1, AsukaKeys.match(keys, "down", 2)) // exactly the modifiers
        assertEquals(2, AsukaKeys.match(keys, "k", 1))
        assertEquals(3, AsukaKeys.match(keys, "K", 3)) // a character, any case
        // Control is Ctrl, as Primary is.
        assertEquals(4, AsukaKeys.match(keys, "n", 1))
        assertEquals(5, AsukaKeys.match(keys, "x", 4))
        assertEquals(-1, AsukaKeys.match(keys, "x", 4 or 16)) // with Meta: none
        assertEquals(-1, AsukaKeys.match(keys, "z", 0))
        assertEquals(-1, AsukaKeys.match(emptyList(), "down", 0))
    }

    @Test
    fun keyName() {
        assertEquals("a", AsukaKeys.keyName("A"))
        assertEquals("é", AsukaKeys.keyName("É"))
        assertEquals("Down", AsukaKeys.keyName("Down")) // a named key as it is
        assertEquals("pageup", AsukaKeys.keyName("pageup"))
    }

    @Test
    fun mods() {
        assertEquals(0, AsukaKeys.mods(0))
        assertEquals(1, AsukaKeys.mods(KeyEvent.META_CTRL_ON or KeyEvent.META_CTRL_LEFT_ON))
        assertEquals(2, AsukaKeys.mods(KeyEvent.META_SHIFT_ON or KeyEvent.META_SHIFT_RIGHT_ON))
        assertEquals(4, AsukaKeys.mods(KeyEvent.META_ALT_ON or KeyEvent.META_ALT_LEFT_ON))
        assertEquals(1 or 2 or 4, AsukaKeys.mods(KeyEvent.META_CTRL_ON or KeyEvent.META_SHIFT_ON or KeyEvent.META_ALT_ON))
        assertEquals(16, AsukaKeys.mods(KeyEvent.META_META_ON))
        // Caps Lock and Num Lock aren't modifiers.
        assertEquals(0, AsukaKeys.mods(KeyEvent.META_CAPS_LOCK_ON or KeyEvent.META_NUM_LOCK_ON))
        assertEquals(1, AsukaKeys.pressed(8))
        assertEquals(1 or 2, AsukaKeys.pressed(8 or 1 or 2))
        assertEquals(4, AsukaKeys.pressed(4))
    }

    @Test
    fun name() {
        val cases = listOf(
            KeyEvent.KEYCODE_ENTER to "enter",
            KeyEvent.KEYCODE_NUMPAD_ENTER to "enter",
            KeyEvent.KEYCODE_ESCAPE to "escape",
            KeyEvent.KEYCODE_TAB to "tab",
            KeyEvent.KEYCODE_SPACE to "space",
            KeyEvent.KEYCODE_FORWARD_DEL to "delete",
            KeyEvent.KEYCODE_DEL to "backspace",
            KeyEvent.KEYCODE_DPAD_UP to "up",
            KeyEvent.KEYCODE_DPAD_DOWN to "down",
            KeyEvent.KEYCODE_DPAD_LEFT to "left",
            KeyEvent.KEYCODE_DPAD_RIGHT to "right",
            KeyEvent.KEYCODE_MOVE_HOME to "home",
            KeyEvent.KEYCODE_MOVE_END to "end",
            KeyEvent.KEYCODE_PAGE_UP to "pageup",
            KeyEvent.KEYCODE_PAGE_DOWN to "pagedown",
            KeyEvent.KEYCODE_F1 to "f1",
            KeyEvent.KEYCODE_F12 to "f12",
        )
        for ((code, want) in cases) assertEquals("key code $code", want, AsukaKeys.name(code, 0))
        // A character as the key types it without modifiers, lower case.
        assertEquals("a", AsukaKeys.name(KeyEvent.KEYCODE_A, 'a'.code))
        assertEquals("a", AsukaKeys.name(KeyEvent.KEYCODE_A, 'A'.code))
        assertEquals("1", AsukaKeys.name(KeyEvent.KEYCODE_1, '1'.code))
        assertEquals(",", AsukaKeys.name(KeyEvent.KEYCODE_COMMA, ','.code))
        assertEquals("é", AsukaKeys.name(KeyEvent.KEYCODE_UNKNOWN, 'é'.code))
        // Back is OnBack's, D-pad center no Keys name; modifiers and dead keys type nothing.
        assertNull(AsukaKeys.name(KeyEvent.KEYCODE_BACK, 0))
        assertNull(AsukaKeys.name(KeyEvent.KEYCODE_DPAD_CENTER, 0))
        assertNull(AsukaKeys.name(KeyEvent.KEYCODE_SHIFT_LEFT, 0))
        assertNull(AsukaKeys.name(KeyEvent.KEYCODE_GRAVE, '`'.code or Int.MIN_VALUE))
    }
}
