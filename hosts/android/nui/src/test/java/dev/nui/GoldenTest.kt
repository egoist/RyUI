package dev.nui

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.File
import java.nio.ByteBuffer
import java.nio.ByteOrder

// The Go side writes ops/testdata (go test ./ops -update). These tests
// check the generated Kotlin decoder and event encoders agree with it.
class GoldenTest {
    private val testdata = File("../../../ops/testdata")

    private fun hex(b: ByteArray) = b.joinToString("") { "%02x".format(it) }
    private fun f32(f: Float) = "%08x".format(java.lang.Float.floatToRawIntBits(f))

    private class Dumper : NuiOpHandler {
        val out = StringBuilder()
        private fun hex(b: ByteArray) = b.joinToString("") { "%02x".format(it) }
        private fun f32(f: Float) = "%08x".format(java.lang.Float.floatToRawIntBits(f))
        private fun set(id: Int, prop: Int, t: Int, v: String) { out.append("set ${id.toUInt()} $prop $t $v\n") }
        override fun create(id: Int, kind: Int) { out.append("create ${id.toUInt()} $kind\n") }
        override fun destroy(id: Int) { out.append("destroy ${id.toUInt()}\n") }
        override fun insert(parent: Int, child: Int, index: Int) { out.append("insert ${parent.toUInt()} ${child.toUInt()} $index\n") }
        override fun remove(parent: Int, child: Int) { out.append("remove ${parent.toUInt()} ${child.toUInt()}\n") }
        override fun setBool(id: Int, prop: Int, v: Boolean) = set(id, prop, 1, v.toString())
        override fun setInt(id: Int, prop: Int, v: Long) = set(id, prop, 2, v.toString())
        override fun setFloat(id: Int, prop: Int, v: Double) = set(id, prop, 3, "%016x".format(java.lang.Double.doubleToRawLongBits(v)))
        override fun setString(id: Int, prop: Int, v: String) = set(id, prop, 4, hex(v.toByteArray(Charsets.UTF_8)))
        override fun setColor(id: Int, prop: Int, v: Long) = set(id, prop, 5, "%016x".format(v))
        override fun setEdges(id: Int, prop: Int, top: Float, right: Float, bottom: Float, left: Float) =
            set(id, prop, 6, "${f32(top)} ${f32(right)} ${f32(bottom)} ${f32(left)}")
        override fun setEnum(id: Int, prop: Int, v: Int) = set(id, prop, 7, v.toString())
        override fun setBytes(id: Int, prop: Int, v: ByteBuffer) {
            val a = ByteArray(v.remaining()); v.get(a); set(id, prop, 8, hex(a))
        }
        override fun setFrame(id: Int, x: Float, y: Float, width: Float, height: Float) {
            out.append("frame ${id.toUInt()} ${f32(x)} ${f32(y)} ${f32(width)} ${f32(height)}\n")
        }
        override fun listen(id: Int, event: Int) { out.append("listen ${id.toUInt()} $event\n") }
        override fun unlisten(id: Int, event: Int) { out.append("unlisten ${id.toUInt()} $event\n") }
        override fun command(id: Int, command: Int, args: ByteBuffer) {
            val a = ByteArray(args.remaining()); args.get(a); out.append("command ${id.toUInt()} $command ${hex(a)}\n")
        }
        override fun setRoot(id: Int) { out.append("root ${id.toUInt()}\n") }
    }

    @Test fun decodesGoldenBatch() {
        val bin = File(testdata, "golden.bin").readBytes()
        val want = File(testdata, "golden.txt").readText()
        val d = Dumper()
        nuiDecode(ByteBuffer.wrap(bin), d)
        assertEquals(want, d.out.toString())
    }

    @Test fun encodesGoldenEvents() {
        val env = NuiEnvironment(width = 402f, height = 874f, safeTop = 62f, safeBottom = 34f, keyboard = 336f,
            scale = 3f, fontScale = 1.25f, dark = true, locale = "en_US")
        val events = listOf(
            NuiEvents.press(7),
            NuiEvents.textChanged(7, "hé"),
            NuiEvents.toggled(8, true),
            NuiEvents.valueChanged(9, 0.25),
            NuiEvents.scrolled(10, 1.5, -2.0),
            NuiEvents.lifecycle(1, NuiLifecycle.BACKGROUND.toLong()),
            NuiEvents.environment(1, env),
        )
        val out = ByteBuffer.allocate(4096).order(ByteOrder.LITTLE_ENDIAN)
        for (e in events) { out.putInt(e.size); out.put(e) }
        assertArrayEquals(File(testdata, "events.bin").readBytes(), out.array().copyOf(out.position()))
    }
}
