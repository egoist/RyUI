package dev.asuka

import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * An item of a menu, an alert or a toolbar (ops/actions.go). [index] is
 * its number, depth first (a submenu or section before its own items),
 * which Action events carry; [items] are a submenu's or a section's.
 */
data class AsukaAction(
    val title: String,
    val role: Int,
    val icon: String = "",
    val index: Long = 0,
    val flags: Int = 0,
    val items: List<AsukaAction> = emptyList(),
) {
    val disabled get() = flags and DISABLED != 0
    val checked get() = flags and CHECKED != 0
    /** It opens a submenu (or, inline, is a section). */
    val submenu get() = flags and SUBMENU != 0
    /** A section of its menu, shown in place. */
    val inline get() = submenu && flags and INLINE != 0

    companion object {
        const val DISABLED = 2
        const val CHECKED = 4
        const val SUBMENU = 16
        const val INLINE = 32
    }
}

/**
 * Decodes actions: u32 count, then per item u8 role, a u32-length title,
 * a u32-length icon and u8 flags, then a submenu's items; numbered depth
 * first. What's cut short is left out.
 */
fun decodeActions(buf: ByteBuffer): List<AsukaAction> {
    val b = buf.duplicate().order(ByteOrder.LITTLE_ENDIAN)
    var next = 0L
    var cut = false
    fun u32(): Int? {
        if (b.remaining() < 4) { cut = true; return null }
        return b.int
    }
    fun u8(): Int? {
        if (!b.hasRemaining()) { cut = true; return null }
        return b.get().toInt() and 0xff
    }
    fun str(): String? {
        val n = u32() ?: return null
        if (n < 0 || n > b.remaining()) { cut = true; return null }
        val a = ByteArray(n)
        b.get(a)
        return String(a, Charsets.UTF_8)
    }
    fun menu(depth: Int): List<AsukaAction> {
        val n = u32() ?: return emptyList()
        val out = ArrayList<AsukaAction>()
        for (k in 0 until n) {
            val role = u8() ?: break
            val title = str() ?: break
            val icon = str() ?: break
            val flags = u8() ?: break
            val index = next++
            var items = emptyList<AsukaAction>()
            if (flags and AsukaAction.SUBMENU != 0) {
                if (depth >= 16) { cut = true; break }
                items = menu(depth + 1)
            }
            out.add(AsukaAction(title, role, icon, index, flags, items))
            if (cut) break
        }
        return out
    }
    return menu(0)
}
