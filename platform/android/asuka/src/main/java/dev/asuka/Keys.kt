package dev.asuka

import android.view.KeyEvent
import android.view.View
import android.view.inputmethod.BaseInputConnection
import android.widget.TextView
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.IdentityHashMap

// A hardware keyboard's keys (a Chromebook's, a Bluetooth one, the
// emulator's computer keyboard), as the Keys prop routes them: from the
// focused view out through its ancestors, the nearest whose Keys has the
// key takes it, ahead of the focused control. AsukaActivity's
// dispatchKeyEvent, which the window's keys reach before its views (and
// after the input method, which keeps what it composes); a sheet's or a
// popover's window doesn't pass it, so its screen routes them
// (AsukaScreen.dispatchKeyEvent).

/** A key of a view's Keys prop (ops.Key): a lower case character or a named key, and modifiers (1 primary, 2 shift, 4 alt, 8 control). */
data class AsukaKey(val name: String, val mods: Int)

object AsukaKeys {
    /** A Keys prop's keys (ops.DecodeKeys): u32 count, per key a u32-length name and u8 modifiers; what's cut short is left out. */
    fun decode(buf: ByteBuffer): List<AsukaKey> {
        val b = buf.duplicate().order(ByteOrder.LITTLE_ENDIAN)
        if (b.remaining() < 4) return emptyList()
        val n = b.int
        if (n < 0 || n > b.remaining()) return emptyList()
        val keys = ArrayList<AsukaKey>(n)
        for (i in 0 until n) {
            if (b.remaining() < 4) break
            val len = b.int
            if (len < 0 || len >= b.remaining()) break // the name and its modifiers' byte
            val a = ByteArray(len)
            b.get(a)
            keys.add(AsukaKey(String(a, Charsets.UTF_8), b.get().toInt() and 0xff))
        }
        return keys
    }

    /**
     * The index of the key in keys that a key going down is (name as the
     * Keys prop says it, mods as [mods] reads them), or -1: ops.MatchKey,
     * with Control as Ctrl, which is Primary here.
     */
    fun match(keys: List<AsukaKey>, name: String, mods: Int): Int {
        val n = keyName(name)
        for ((i, k) in keys.withIndex()) if (pressed(k.mods) == mods && k.name == n) return i
        return -1
    }

    /** A key's name as the Keys prop has it: a single character lower case, a named key as it is (ops.KeyName). */
    fun keyName(name: String): String = if (name.codePointCount(0, name.length) == 1) name.lowercase() else name

    /** Declared modifiers as a key going down has them here: Control (8) is the Ctrl key, Primary's. */
    fun pressed(mods: Int): Int = if (mods and 8 != 0) (mods and 8.inv()) or 1 else mods

    /** A key event's modifiers as Keys' bits: Ctrl primary, Shift, Alt; Meta (no Keys bit) 16, which no key has. */
    fun mods(meta: Int): Int {
        var m = 0
        if (meta and KeyEvent.META_CTRL_ON != 0) m = m or 1
        if (meta and KeyEvent.META_SHIFT_ON != 0) m = m or 2
        if (meta and KeyEvent.META_ALT_ON != 0) m = m or 4
        if (meta and KeyEvent.META_META_ON != 0) m = m or 16
        return m
    }

    /**
     * The name of a key going down, as the Keys prop says it, from its key
     * code and the character it types without modifiers (0: none), or
     * null for a key no Keys names (Back, D-pad center, a modifier).
     */
    fun name(keyCode: Int, char: Int): String? {
        when (keyCode) {
            KeyEvent.KEYCODE_ENTER, KeyEvent.KEYCODE_NUMPAD_ENTER -> return "enter"
            KeyEvent.KEYCODE_ESCAPE -> return "escape"
            KeyEvent.KEYCODE_TAB -> return "tab"
            KeyEvent.KEYCODE_SPACE -> return "space"
            KeyEvent.KEYCODE_FORWARD_DEL -> return "delete"
            KeyEvent.KEYCODE_DEL -> return "backspace"
            KeyEvent.KEYCODE_DPAD_UP -> return "up"
            KeyEvent.KEYCODE_DPAD_DOWN -> return "down"
            KeyEvent.KEYCODE_DPAD_LEFT -> return "left"
            KeyEvent.KEYCODE_DPAD_RIGHT -> return "right"
            KeyEvent.KEYCODE_MOVE_HOME -> return "home"
            KeyEvent.KEYCODE_MOVE_END -> return "end"
            KeyEvent.KEYCODE_PAGE_UP -> return "pageup"
            KeyEvent.KEYCODE_PAGE_DOWN -> return "pagedown"
            KeyEvent.KEYCODE_BACK, KeyEvent.KEYCODE_DPAD_CENTER -> return null
        }
        if (keyCode in KeyEvent.KEYCODE_F1..KeyEvent.KEYCODE_F12) return "f" + (keyCode - KeyEvent.KEYCODE_F1 + 1)
        // A control character, or a dead key's accent (COMBINING_ACCENT,
        // the sign bit): none.
        if (char <= 0x20 || char == 0x7f) return null
        return keyName(String(Character.toChars(char)))
    }
}

/**
 * Routes a key event from a window: the nearest node from focused out
 * (the root when nothing of the app's has the focus) whose Keys has it
 * gets Key with its index, and the event goes no further (nor its
 * release). Reports whether it was taken.
 */
fun AsukaRuntime.routeKey(e: KeyEvent, focused: View?): Boolean {
    when (e.action) {
        KeyEvent.ACTION_UP -> return takenKeys.remove(e.keyCode) // its down was taken
        KeyEvent.ACTION_DOWN -> {}
        else -> return false
    }
    if (keyed.isEmpty()) return false
    val name = AsukaKeys.name(e.keyCode, e.getUnicodeChar(0)) ?: return false
    // An input method composing text keeps its keys.
    val t = (focused as? TextView)?.text
    if (t is android.text.Spannable && BaseInputConnection.getComposingSpanStart(t) >= 0) return false
    val mods = AsukaKeys.mods(e.metaState)
    val byView = IdentityHashMap<View, AsukaNode>(nodes.size)
    for (n in nodes.values) {
        byView[n.view] = n
        n.wrapper?.let { byView[it] = n }
    }
    fun nodeOf(v: View?): AsukaNode? {
        var x: Any? = v
        while (x is View) {
            byView[x]?.let { return it }
            x = x.parent
        }
        return null
    }
    var n = nodeOf(focused) ?: nodes[rootID]
    while (n != null) {
        val i = AsukaKeys.match(n.keys, name, mods)
        if (i >= 0) {
            takenKeys.add(e.keyCode)
            val id = n.id
            emit(id, AsukaEventKind.KEY) { AsukaEvents.key(id, i.toLong()) }
            return true
        }
        // A list's row: its cell has no parent node, its list's view holds it.
        n = n.parent ?: nodeOf(n.outer.parent as? View)
    }
    return false
}

/** Sets a node's Keys prop. */
fun AsukaRuntime.setKeys(n: AsukaNode, v: ByteBuffer) {
    n.keys = AsukaKeys.decode(v)
    if (n.keys.isEmpty()) keyed.remove(n.id) else keyed.add(n.id)
}
