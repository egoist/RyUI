package dev.asuka

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.File
import java.nio.ByteBuffer

// The Kotlin reader of a sidebar agrees with Go's (ops/sidebar.go): it
// decodes ops/testdata/sidebar.bin to ops/testdata/sidebar.txt.
class SidebarTest {
    private val testdata = File("../../../ops/testdata")

    @Test
    fun decodesTheGoldenSidebar() {
        val bin = File(testdata, "sidebar.bin").readBytes()
        val sections = decodeSidebar(ByteBuffer.wrap(bin))
        val out = StringBuilder()
        for ((i, s) in sections.withIndex()) {
            out.append("#\t${s.title}\n")
            for (it in s.items) out.append("$i\t${it.title}\t${it.icon}\t${it.badge}\n")
        }
        assertEquals(File(testdata, "sidebar.txt").readText(), out.toString())
        // Cut short, it ends where it's cut, with whole items.
        val cut = decodeSidebar(ByteBuffer.wrap(bin, 0, bin.size - 18).slice())
        assertEquals(2, cut.size)
        assertEquals(listOf("Archiv"), cut[1].items.map { it.title })
        assertEquals(0, decodeSidebar(ByteBuffer.allocate(0)).size)
        val more = sections.toMutableList()
        more[0] = more[0].copy(items = more[0].items.mapIndexed { i, it -> if (i == 0) it.copy(badge = "4") else it })
        assertTrue(sameSidebarShape(sections, more))
        more[2] = more[2].copy(items = more[2].items + AsukaSidebarItem("Spam"))
        assertFalse(sameSidebarShape(sections, more))
    }

    @Test
    fun readsBadgeNumbers() {
        assertEquals(3, badgeNumber("3"))
        assertEquals(1234, badgeNumber("1234"))
        assertNull(badgeNumber(""))
        assertNull(badgeNumber("New"))
        assertNull(badgeNumber("99+"))
        assertNull(badgeNumber("٣"))
        assertNull(badgeNumber("1234567890"))
    }
}
