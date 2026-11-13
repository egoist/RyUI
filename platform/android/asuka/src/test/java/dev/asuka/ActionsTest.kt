package dev.asuka

import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.File
import java.nio.ByteBuffer

// The Kotlin reader of action menus agrees with Go's (ops/actions.go): it
// decodes ops/testdata/actions.bin to ops/testdata/actions.txt.
class ActionsTest {
    private val testdata = File("../../../ops/testdata")

    @Test
    fun decodesTheGoldenMenu() {
        val bin = File(testdata, "actions.bin").readBytes()
        val out = StringBuilder()
        fun walk(items: List<AsukaAction>, depth: Int) {
            for (a in items) {
                out.append("${a.index}\t$depth\t${a.role}\t${a.flags}\t${a.title}\t${a.icon}\n")
                walk(a.items, depth + 1)
            }
        }
        walk(decodeActions(ByteBuffer.wrap(bin)), 0)
        assertEquals(File(testdata, "actions.txt").readText(), out.toString())
        // Cut short, it ends where it's cut.
        fun count(items: List<AsukaAction>): Int = items.sumOf { 1 + count(it.items) }
        assertEquals(11, count(decodeActions(ByteBuffer.wrap(bin, 0, bin.size - 3))))
        assertEquals(0, decodeActions(ByteBuffer.allocate(0)).size)
    }
}
