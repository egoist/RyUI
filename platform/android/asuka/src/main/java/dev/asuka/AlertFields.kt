package dev.asuka

import java.nio.ByteBuffer
import java.nio.ByteOrder

/** A text field of an alert (ops/alert.go); [text] is its starting text, [keyboard] an AsukaKeyboard. */
data class AsukaAlertField(
    val placeholder: String,
    val text: String,
    val flags: Int,
    val keyboard: Int,
    val contentType: String,
) {
    /** It hides what's typed: a password. */
    val secure get() = flags and SECURE != 0

    companion object {
        const val SECURE = 1
    }
}

/**
 * Decodes an alert's fields: u32 count, then per field a u32-length
 * placeholder, a u32-length text, u8 flags, u8 keyboard and a u32-length
 * content type. What's cut short is left out.
 */
fun decodeAlertFields(buf: ByteBuffer): List<AsukaAlertField> {
    val b = buf.duplicate().order(ByteOrder.LITTLE_ENDIAN)
    fun u32(): Int? = if (b.remaining() < 4) null else b.int
    fun u8(): Int? = if (!b.hasRemaining()) null else b.get().toInt() and 0xff
    fun str(): String? {
        val n = u32() ?: return null
        if (n < 0 || n > b.remaining()) return null
        val a = ByteArray(n)
        b.get(a)
        return String(a, Charsets.UTF_8)
    }
    val n = u32() ?: return emptyList()
    val out = ArrayList<AsukaAlertField>()
    for (k in 0 until n) {
        val placeholder = str() ?: break
        val text = str() ?: break
        val flags = u8() ?: break
        val keyboard = u8() ?: break
        val contentType = str() ?: break
        out.add(AsukaAlertField(placeholder, text, flags, keyboard, contentType))
    }
    return out
}

/** FieldChanged's payload (ops.AppendFieldChanged): u32 the field's index, then its text. */
fun fieldChangedPayload(index: Int, text: String): ByteArray = AsukaWriter().apply { u32(index); string(text) }.bytes()
