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

    /** A node tree: the root, a view with Keys around a List, a cell bound in it with a row's field; a sheet and a popover, each with a view and a field. */
    private class Tree {
        val nodes = HashMap<Int, AsukaNode>()
        fun node(id: Int, kind: Int, parent: AsukaNode? = null, vararg keys: AsukaKey): AsukaNode {
            val n = AsukaNode(id, kind, android.view.View(null))
            n.parent = parent
            n.keys = keys.toList()
            nodes[id] = n
            return n
        }
        val root = node(1, AsukaKind.VIEW, null, AsukaKey("escape", 0), AsukaKey("k", 1))
        val around = node(2, AsukaKind.VIEW, root, AsukaKey("down", 0))
        val list = node(3, AsukaKind.LIST, around)
        val cell = node((0x80000000L or 1L).toInt(), 0).also { it.cellOf = list.id }
        val rowField = node(4, AsukaKind.TEXT_FIELD, cell, AsukaKey("enter", 0))
        val sheet = node(10, AsukaKind.SHEET)
        val sheetView = node(11, AsukaKind.VIEW, sheet, AsukaKey("escape", 0))
        val sheetField = node(12, AsukaKind.TEXT_FIELD, sheetView)
        val pop = node(20, AsukaKind.POPOVER)
        val popField = node(21, AsukaKind.TEXT_FIELD, pop)
        val window = node(30, AsukaKind.WINDOW)
        val windowField = node(31, AsukaKind.TEXT_FIELD, window)

        fun taker(from: AsukaNode?, name: String, mods: Int = 0) = keyTaker(from, nodes, root.id, name, mods)?.first
    }

    // A list's row is a cell the host made, which the core leaves without
    // a parent: a key in it goes on to its List, then the views around.
    @Test
    fun listRow() {
        val t = Tree()
        assertEquals(t.list, keyOut(t.cell, t.nodes, t.root.id))
        assertEquals(t.rowField, t.taker(t.rowField, "enter"))
        assertEquals(t.around, t.taker(t.rowField, "down"))
        assertEquals(t.root, t.taker(t.rowField, "escape"))
        assertNull(t.taker(t.rowField, "up"))
    }

    // A sheet's and a popover's node go on to the root (a phone's: the
    // window they cover), whose keys (UseKeys) come after what their
    // content declared; with nothing focused in their window, a key starts
    // at their node. The root and a Window are ends.
    @Test
    fun sheetPopover() {
        val t = Tree()
        assertEquals(t.root, keyOut(t.sheet, t.nodes, t.root.id))
        assertEquals(t.root, keyOut(t.pop, t.nodes, t.root.id))
        assertNull(keyOut(t.root, t.nodes, t.root.id))
        assertNull(keyOut(t.window, t.nodes, t.root.id))
        assertEquals(t.sheetView, t.taker(t.sheetField, "escape")) // the sheet's first
        assertEquals(t.root, t.taker(t.sheetField, "k", 1))
        assertEquals(t.root, t.taker(t.sheet, "k", 1)) // nothing focused in it
        assertEquals(t.root, t.taker(t.popField, "escape"))
        assertEquals(t.root, t.taker(t.pop, "k", 1))
        assertNull(t.taker(t.sheetField, "down")) // the views around the list aren't on its way
        assertNull(t.taker(t.windowField, "k", 1)) // a Window's keys are its own
    }
}
