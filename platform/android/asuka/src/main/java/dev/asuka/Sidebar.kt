package dev.asuka

import java.nio.ByteBuffer
import java.nio.ByteOrder

// A SplitView's sidebar (ops/sidebar.go): Split's Items. Written by hand,
// beside the generated decoder.

/** A destination in a sidebar; badge is shown at its end (empty: none). */
data class AsukaSidebarItem(val title: String, val icon: String = "", val badge: String = "")

/** A titled group of a sidebar's destinations. */
data class AsukaSidebarSection(val title: String, val items: List<AsukaSidebarItem>)

/**
 * The sections (ops.DecodeSidebar): u32 count, per section a u32-length
 * title and u32 item count, per item a u32-length title, icon and badge.
 * What's cut short is left out, items whole.
 */
fun decodeSidebar(b: ByteBuffer): List<AsukaSidebarSection> {
    val r = b.duplicate().order(ByteOrder.LITTLE_ENDIAN)
    fun u32(): Int? = if (r.remaining() >= 4) r.int else null
    fun str(): String? {
        val n = u32() ?: return null
        if (n < 0 || n > r.remaining()) { r.position(r.limit()); return null }
        return ByteArray(n).also { r.get(it) }.toString(Charsets.UTF_8)
    }
    val count = u32() ?: return emptyList()
    val out = ArrayList<AsukaSidebarSection>()
    for (s in 0 until minOf(count, r.remaining())) {
        val title = str() ?: break
        val n = u32() ?: break
        val items = ArrayList<AsukaSidebarItem>()
        var cut = false
        for (i in 0 until minOf(n, r.remaining())) {
            val t = str(); val icon = str(); val badge = str()
            if (t == null || icon == null || badge == null) { cut = true; break }
            items.add(AsukaSidebarItem(t, icon, badge))
        }
        out.add(AsukaSidebarSection(title, items))
        if (cut) break
    }
    return out
}

/** Whether a and b have as many sections, each as many items (ops.SameSidebarShape). */
fun sameSidebarShape(a: List<AsukaSidebarSection>, b: List<AsukaSidebarSection>): Boolean =
    a.size == b.size && a.indices.all { a[it].items.size == b[it].items.size }
