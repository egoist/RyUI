package dev.asuka

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.UUID

// The bluetooth module's wire (internal/bluetoothspec), apart from Android's classes.
class BluetoothTest {
    private val hr = UUID.fromString("0000180d-0000-1000-8000-00805f9b34fb")
    private val battery = UUID.fromString("0000180f-0000-1000-8000-00805f9b34fb")

    @Test
    fun uuids() {
        // As bluetoothspec.ParseUUID reads them.
        for (s in listOf("180d", "180D", "0x180d", "0000180d", "0000180d-0000-1000-8000-00805f9b34fb", "{0000180D-0000-1000-8000-00805F9B34FB}", "0000180d000010008000 00805f9b34fb".replace(" ", ""))) {
            assertEquals(s, hr, AsukaBluetoothWire.uuid(s))
        }
        assertEquals("0000180d-0000-1000-8000-00805f9b34fb", AsukaBluetoothWire.string(hr))
        val big = AsukaBluetoothWire.uuid("f000aa00-0451-4000-b000-000000000000")!!
        assertEquals("f000aa00-0451-4000-b000-000000000000", AsukaBluetoothWire.string(big))
        assertEquals(UUID.fromString("12345678-0000-1000-8000-00805f9b34fb"), AsukaBluetoothWire.uuid("12345678"))
        for (s in listOf("", "18", "zzzz", "0000180d-0000-1000-8000_00805f9b34fb")) assertNull(s, AsukaBluetoothWire.uuid(s))
    }

    @Test
    fun scansMerged() {
        val w = AsukaBluetoothWire
        assertNull(w.merge(emptyList()))
        // The union of the services, the most aggressive mode.
        assertEquals(AsukaBluetoothWire.ScanConfig(setOf(hr, battery), 2), w.merge(listOf(listOf(hr) to 0, listOf(battery) to 2)))
        // One scan for every device: no filter.
        assertEquals(AsukaBluetoothWire.ScanConfig(null, 1), w.merge(listOf(listOf(hr) to 1, emptyList<UUID>() to -1)))
        // Running for more, as hard: no restart; for less, or slower: one.
        assertTrue(w.covers(AsukaBluetoothWire.ScanConfig(null, 2), AsukaBluetoothWire.ScanConfig(setOf(hr), 1)))
        assertTrue(w.covers(AsukaBluetoothWire.ScanConfig(setOf(hr, battery), 2), AsukaBluetoothWire.ScanConfig(setOf(hr), 2)))
        assertFalse(w.covers(AsukaBluetoothWire.ScanConfig(setOf(hr), 2), AsukaBluetoothWire.ScanConfig(setOf(hr, battery), 2)))
        assertFalse(w.covers(AsukaBluetoothWire.ScanConfig(setOf(hr), 2), AsukaBluetoothWire.ScanConfig(null, 2)))
        assertFalse(w.covers(AsukaBluetoothWire.ScanConfig(null, 0), AsukaBluetoothWire.ScanConfig(null, 2)))
    }

    @Test
    fun throttle() {
        val w = AsukaBluetoothWire
        assertEquals(0, w.throttleDelay(listOf(0, 1000, 2000, 3000), 4000))
        // A sixth start within 30 seconds waits for the first to leave the window.
        assertEquals(30_000L + 250 - 4000, w.throttleDelay(listOf(0, 1000, 2000, 3000, 3500), 4000))
        assertEquals(1000L + 30_000 + 250 - 31_000, w.throttleDelay(listOf(0, 1000, 2000, 3000, 3500, 4000), 31_000))
        assertEquals(0, w.throttleDelay(listOf(0, 1000, 2000, 3000, 3500), 40_000))
    }

    @Test
    fun statuses() {
        val w = AsukaBluetoothWire
        assertEquals("timeout", w.reason(8))
        assertEquals("remote", w.reason(19))
        assertEquals("local", w.reason(22))
        assertEquals("failed", w.reason(62))
        assertEquals("failed", w.reason(133))
        assertEquals("timeout", w.connectError(147))
        assertTrue(w.connectError(133).startsWith("failed"))
    }

    @Test
    fun preparedWrite() {
        val (off, v) = AsukaBluetoothWire.assemble(listOf(0 to byteArrayOf(1, 2, 3), 3 to byteArrayOf(4, 5), 5 to byteArrayOf(6)))
        assertEquals(0, off)
        assertArrayEquals(byteArrayOf(1, 2, 3, 4, 5, 6), v)
        val (off2, v2) = AsukaBluetoothWire.assemble(listOf(4 to byteArrayOf(9), 2 to byteArrayOf(7, 8)))
        assertEquals(2, off2)
        assertArrayEquals(byteArrayOf(7, 8, 9), v2)
    }

    @Test
    fun cccd() {
        val notify = 0x10; val indicate = 0x20
        assertArrayEquals(byteArrayOf(1, 0), AsukaBluetoothWire.cccdOn(notify or indicate, false))
        assertArrayEquals(byteArrayOf(2, 0), AsukaBluetoothWire.cccdOn(notify or indicate, true))
        assertArrayEquals(byteArrayOf(2, 0), AsukaBluetoothWire.cccdOn(indicate, false))
        assertArrayEquals(byteArrayOf(1, 0), AsukaBluetoothWire.cccdOn(notify, true))
    }

    @Test
    fun advertisement() {
        val a = AsukaBluetoothWire.advertisement("C4:7C:8D:6A:1F:20", null, -60, "Polar H10", listOf(hr), emptyList(),
            mapOf(battery to byteArrayOf(80)), mapOf(76 to byteArrayOf(2, 21)), -4, true, 1_700_000_000_000)
        assertEquals("C4:7C:8D:6A:1F:20", a.getJSONObject("device").getString("id"))
        assertEquals("Polar H10", a.getJSONObject("device").getString("name")) // the advertised one, without the system's
        assertEquals(-60, a.getInt("rssi"))
        assertEquals("0000180d-0000-1000-8000-00805f9b34fb", a.getJSONArray("services").getString(0))
        assertFalse(a.has("solicited"))
        assertEquals("UA==", a.getJSONObject("serviceData").getString("0000180f-0000-1000-8000-00805f9b34fb"))
        assertEquals("AhU=", a.getJSONObject("manufacturerData").getString("76"))
        assertEquals(-4, a.getInt("txPower"))
        assertTrue(a.getBoolean("connectable"))
        assertEquals(1_700_000_000_000, a.getLong("time"))
        val bare = AsukaBluetoothWire.advertisement("AA:BB:CC:DD:EE:FF", null, -90, null, emptyList(), emptyList(), emptyMap(), emptyMap(), null, null, 0)
        assertEquals(setOf("device", "rssi", "time"), bare.keys().asSequence().toSet())
    }

    @Test
    fun args() {
        val o = JSONObject("""{"value":null,"ids":null,"v":"AQI=","services":["180d"]}""")
        assertArrayEquals(ByteArray(0), AsukaBluetoothWire.bytes(o, "value")) // a nil []byte
        assertArrayEquals(ByteArray(0), AsukaBluetoothWire.bytes(o, "missing"))
        assertArrayEquals(byteArrayOf(1, 2), AsukaBluetoothWire.bytes(o, "v"))
        assertEquals(emptyList<String>(), AsukaBluetoothWire.strings(o, "ids"))
        assertEquals(listOf(hr), AsukaBluetoothWire.uuids(o, "services"))
    }

    @Test
    fun stored() {
        fun ad(id: String, rssi: Int) = AsukaBluetoothWire.advertisement(id, null, rssi, null, emptyList(), emptyList(), emptyMap(), emptyMap(), null, null, 0)
        // Once per device (the newest), unless duplicates; the newest kept.
        val once = AsukaBluetoothWire.storeAdvertisements(JSONArray().put(ad("A", -50)), listOf(ad("B", -60), ad("A", -40)), false, 10)
        assertEquals(listOf("B" to -60, "A" to -40), (0 until once.length()).map { once.getJSONObject(it).let { a -> a.getJSONObject("device").getString("id") to a.getInt("rssi") } })
        val all = AsukaBluetoothWire.storeAdvertisements(JSONArray().put(ad("A", -50)), listOf(ad("A", -40), ad("A", -30)), true, 2)
        assertEquals(listOf(-40, -30), (0 until all.length()).map { all.getJSONObject(it).getInt("rssi") })
    }
}
