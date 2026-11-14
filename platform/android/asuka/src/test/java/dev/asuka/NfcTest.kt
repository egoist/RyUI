package dev.asuka

import android.nfc.NfcAdapter
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

// asuka/sdk/nfc's wire, as the host gives it: the technologies' names
// (internal/nfcspec's), reader mode's flags for the Poll bits, FeliCa's
// length byte and NFC-V's UID.
class NfcTest {
    @Test
    fun techs() {
        val ntag = arrayOf("android.nfc.tech.NfcA", "android.nfc.tech.MifareUltralight", "android.nfc.tech.Ndef")
        assertEquals(listOf("ndef", "mifare", "nfcA", "mifareUltralight"), AsukaNfcWire.techs(ntag, true))
        val card = arrayOf("android.nfc.tech.IsoDep", "android.nfc.tech.NfcB")
        assertEquals(listOf("iso7816", "isoDep", "nfcB"), AsukaNfcWire.techs(card, false))
        val felica = arrayOf("android.nfc.tech.NfcF", "android.nfc.tech.NdefFormatable")
        assertEquals(listOf("felica", "nfcF", "ndefFormatable"), AsukaNfcWire.techs(felica, false))
        val vicinity = arrayOf("android.nfc.tech.NfcV", "com.example.Unknown")
        assertEquals(listOf("iso15693", "nfcV"), AsukaNfcWire.techs(vicinity, false))
    }

    @Test
    fun readerFlags() {
        val every = NfcAdapter.FLAG_READER_NFC_A or NfcAdapter.FLAG_READER_NFC_B or NfcAdapter.FLAG_READER_NFC_F or NfcAdapter.FLAG_READER_NFC_V
        assertEquals(every, AsukaNfcWire.readerFlags(0, false, false))
        assertEquals(NfcAdapter.FLAG_READER_NFC_A or NfcAdapter.FLAG_READER_NFC_V or NfcAdapter.FLAG_READER_SKIP_NDEF_CHECK or NfcAdapter.FLAG_READER_NO_PLATFORM_SOUNDS,
            AsukaNfcWire.readerFlags(0b1001, true, true))
        assertEquals(NfcAdapter.FLAG_READER_NFC_F or NfcAdapter.FLAG_READER_NFC_BARCODE, AsukaNfcWire.readerFlags(0b10100, false, false))
        assertEquals(NfcAdapter.FLAG_READER_NFC_A or NfcAdapter.FLAG_READER_NFC_B, AsukaNfcWire.readerFlags(0b100000, false, false))
        // The constants themselves (android.jar's stubs could hide a 0).
        assertEquals(1, NfcAdapter.FLAG_READER_NFC_A)
        assertEquals(0x80, NfcAdapter.FLAG_READER_SKIP_NDEF_CHECK)
    }

    @Test
    fun feliCa() {
        // Polling (00), system code FFFF, request code 01, time slot 0F.
        val polling = byteArrayOf(0x00, 0xff.toByte(), 0xff.toByte(), 0x01, 0x0f)
        assertArrayEquals(byteArrayOf(0x06, 0x00, 0xff.toByte(), 0xff.toByte(), 0x01, 0x0f), AsukaNfcWire.feliCaFrame(polling))
        assertArrayEquals(byteArrayOf(0x01, 0x02), AsukaNfcWire.feliCaAnswer(byteArrayOf(0x03, 0x01, 0x02)))
        assertArrayEquals(ByteArray(0), AsukaNfcWire.feliCaAnswer(ByteArray(0)))
    }

    @Test
    fun iso15693() {
        // NfcV's ID is least significant byte first: E0 (ISO 15693), 04 (NXP), then the serial.
        val id = byteArrayOf(0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x04, 0xe0.toByte())
        val (m, serial) = AsukaNfcWire.iso15693(id)!!
        assertEquals(4, m)
        assertArrayEquals(byteArrayOf(0x66, 0x55, 0x44, 0x33, 0x22, 0x11), serial)
        assertNull(AsukaNfcWire.iso15693(byteArrayOf(1, 2)))
    }

    @Test
    fun base64() {
        val b = byteArrayOf(0xd1.toByte(), 1, 0x0c, 0x55, 4)
        assertArrayEquals(b, AsukaNfcWire.unb64(AsukaNfcWire.b64(b)))
        assertArrayEquals(ByteArray(0), AsukaNfcWire.unb64(""))
        assertEquals("", AsukaNfcWire.b64(null))
    }
}
