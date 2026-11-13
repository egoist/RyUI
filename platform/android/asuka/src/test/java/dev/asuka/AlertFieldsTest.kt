package dev.asuka

import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Test
import java.io.File
import java.nio.ByteBuffer

// The Kotlin reader of an alert's fields agrees with Go's (ops/alert.go):
// it decodes ops/testdata/alertfields.bin to ops/testdata/alertfields.txt,
// and writes FieldChanged's payload as ops/testdata/fieldchanged.bin.
class AlertFieldsTest {
    private val testdata = File("../../../ops/testdata")

    @Test
    fun decodesTheGoldenFields() {
        val bin = File(testdata, "alertfields.bin").readBytes()
        val out = StringBuilder()
        for (f in decodeAlertFields(ByteBuffer.wrap(bin))) {
            out.append("${f.placeholder}\t${f.text}\t${f.flags}\t${f.keyboard}\t${f.contentType}\n")
        }
        assertEquals(File(testdata, "alertfields.txt").readText(), out.toString())
        assertEquals(3, decodeAlertFields(ByteBuffer.wrap(bin, 0, bin.size - 3).slice()).size)
        assertEquals(0, decodeAlertFields(ByteBuffer.allocate(0)).size)
        assertArrayEquals(File(testdata, "fieldchanged.bin").readBytes(), fieldChangedPayload(2, "Grüße"))
    }
}
