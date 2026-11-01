package dev.ryui

import org.junit.Assert.assertEquals
import org.junit.Test
import java.nio.ByteBuffer

// matchMIME mirrors ops.MatchMIME (ops/item_test.go has the same cases);
// the decoders read what ops.AppendDragItems and AppendStrings write.
class InteractionTest {
    @Test
    fun matchMIME() {
        val cases = listOf(
            Triple(listOf("*/*"), "application/x-card", true),
            Triple(listOf("*"), "image/png", true),
            Triple(listOf("image/*"), "image/png", true),
            Triple(listOf("image/*"), "IMAGE/HEIC", true),
            Triple(listOf("image/*"), "imagery/png", false),
            Triple(listOf("image/*"), "image", false),
            Triple(listOf("image/*"), "image/", false),
            Triple(listOf("text/plain"), "text/plain; charset=utf-8", true),
            Triple(listOf(" text/plain ; q=1"), "text/plain", true),
            Triple(listOf("Text/Plain"), "text/plain", true),
            Triple(listOf("text/plain"), "text/uri-list", false),
            Triple(listOf("video/*", "text/uri-list"), "text/uri-list", true),
            Triple(emptyList<String>(), "text/plain", false),
        )
        for ((patterns, mime, want) in cases) assertEquals("matchMIME($patterns, $mime)", want, matchMIME(patterns, mime))
    }

    @Test
    fun decodeDragItems() {
        val w = RyUIWriter()
        w.u32(2)
        w.u8(RyUIShareKind.LINK); w.string("https://example.com"); w.string(""); w.string(""); w.string("text/uri-list"); w.u8(0)
        w.u8(RyUIShareKind.TEXT); w.string("Card"); w.string(""); w.string(""); w.string("application/x-card"); w.u8(1)
        val items = decodeDragItems(ByteBuffer.wrap(w.bytes()))
        assertEquals(2, items.size)
        assertEquals(RyUIShareKind.LINK, items[0].kind)
        assertEquals("https://example.com", items[0].text)
        assertEquals("text/uri-list", items[0].mime)
        assertEquals(false, items[0].value)
        assertEquals("application/x-card", items[1].mime)
        assertEquals(true, items[1].value)
        // Cut short: the items before the cut.
        val cut = w.bytes().copyOf(w.bytes().size - 3)
        assertEquals(1, decodeDragItems(ByteBuffer.wrap(cut)).size)
    }

    @Test
    fun decodeStrings() {
        val w = RyUIWriter()
        w.u32(2); w.string("image/*"); w.string("text/plain")
        assertEquals(listOf("image/*", "text/plain"), decodeStrings(ByteBuffer.wrap(w.bytes())))
        assertEquals(emptyList<String>(), decodeStrings(ByteBuffer.wrap(ByteArray(0))))
    }
}
