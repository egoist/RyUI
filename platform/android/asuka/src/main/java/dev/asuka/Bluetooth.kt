package dev.asuka

import android.Manifest
import android.annotation.SuppressLint
import android.app.Activity
import android.app.Application
import android.app.PendingIntent
import android.bluetooth.BluetoothAdapter
import android.bluetooth.BluetoothDevice
import android.bluetooth.BluetoothGatt
import android.bluetooth.BluetoothGattCallback
import android.bluetooth.BluetoothGattCharacteristic
import android.bluetooth.BluetoothGattDescriptor
import android.bluetooth.BluetoothGattServer
import android.bluetooth.BluetoothGattServerCallback
import android.bluetooth.BluetoothGattService
import android.bluetooth.BluetoothManager
import android.bluetooth.BluetoothProfile
import android.bluetooth.BluetoothStatusCodes
import android.bluetooth.le.AdvertiseCallback
import android.bluetooth.le.AdvertiseData
import android.bluetooth.le.AdvertiseSettings
import android.bluetooth.le.BluetoothLeScanner
import android.bluetooth.le.ScanCallback
import android.bluetooth.le.ScanFilter
import android.bluetooth.le.ScanResult
import android.bluetooth.le.ScanSettings
import android.companion.AssociationInfo
import android.companion.AssociationRequest
import android.companion.BluetoothLeDeviceFilter
import android.companion.CompanionDeviceManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.IntentSender
import android.content.pm.PackageInfo
import android.content.pm.PackageManager
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.ParcelUuid
import android.os.SystemClock
import androidx.activity.result.IntentSenderRequest
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.IdentityHashMap
import java.util.UUID
import java.util.regex.Pattern

/**
 * The wire's pure parts (internal/bluetoothspec), apart from Android's
 * classes so the unit tests run them: UUIDs, scans merged, the scan
 * throttle, statuses, a prepared write put together, an advertisement.
 */
internal object AsukaBluetoothWire {
    private val base = UUID.fromString("00000000-0000-1000-8000-00805f9b34fb")
    val CCCD: UUID = UUID.fromString("00002902-0000-1000-8000-00805f9b34fb")

    /** A UUID as bluetoothspec.ParseUUID reads one: 16-bit, 32-bit or 128-bit. */
    fun uuid(str: String): UUID? {
        var s = str.trim().removePrefix("0x").removePrefix("0X").removePrefix("{").removeSuffix("}")
        if (s.length == 4) s = "0000$s"
        if (s.length == 8) {
            val n = s.toLongOrNull(16) ?: return null
            return UUID(base.mostSignificantBits or (n shl 32), base.leastSignificantBits)
        }
        if (s.length == 36) {
            if (s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-') return null
            s = s.replace("-", "")
        }
        if (s.length != 32 || s.any { Character.digit(it, 16) < 0 }) return null
        return UUID(java.lang.Long.parseUnsignedLong(s.substring(0, 16), 16), java.lang.Long.parseUnsignedLong(s.substring(16), 16))
    }

    /** As the wire has it: 128-bit, lower case. */
    fun string(u: UUID): String = u.toString().lowercase()

    /** What one scanner scans for: the services (null: every device) and its ScanSettings mode. */
    data class ScanConfig(val services: Set<UUID>?, val mode: Int)

    /** The scans' union: their services (none if one has none), the most aggressive mode; null: no scans. */
    fun merge(scans: Collection<Pair<List<UUID>, Int>>): ScanConfig? {
        if (scans.isEmpty()) return null
        val all = if (scans.any { it.first.isEmpty() }) null else scans.flatMap { it.first }.toSet()
        return ScanConfig(all, scans.maxOf { it.second })
    }

    /** Whether a scanner running so hears all that want needs (no restart). */
    fun covers(running: ScanConfig, want: ScanConfig): Boolean =
        (running.services == null || (want.services != null && running.services.containsAll(want.services))) && running.mode >= want.mode

    /** Android lets an app start 5 scans in 30 seconds, then scans for nothing: how long before the next may start. */
    const val THROTTLE_STARTS = 5
    const val THROTTLE_WINDOW = 30_000L
    fun throttleDelay(starts: List<Long>, now: Long): Long {
        // A moment more than the window: the system's clock and ours aren't one.
        val window = THROTTLE_WINDOW + 250
        val recent = starts.filter { now - it < window }
        if (recent.size < THROTTLE_STARTS) return 0
        return recent[recent.size - THROTTLE_STARTS] + window - now
    }

    /** A disconnection's reason, from its GATT (HCI) status. */
    fun reason(status: Int): String = when (status) {
        8 -> "timeout" // supervision timeout
        19, 21 -> "remote" // the remote user ended it; its power off
        22 -> "local" // this host ended it
        0 -> "unknown"
        else -> "failed" // 62 (failed to establish), 133 (GATT_ERROR), ...
    }

    /** A GATT status in words. */
    fun statusText(status: Int): String = when (status) {
        0 -> "success"
        1 -> "invalid handle"
        2 -> "read not permitted"
        3 -> "write not permitted"
        5 -> "insufficient authentication"
        6 -> "request not supported"
        7 -> "invalid offset"
        8 -> "connection timeout"
        13 -> "invalid attribute length"
        15 -> "insufficient encryption"
        19 -> "the device ended the connection"
        22 -> "this device ended the connection"
        34 -> "LMP response timeout"
        62 -> "the connection failed to be established"
        133 -> "GATT error"
        143 -> "connection congested"
        147 -> "connection timeout"
        257 -> "failure"
        else -> "status $status"
    }

    /** A connect that failed: its error, as Go reads it. */
    fun connectError(status: Int): String = when (status) {
        8, 147 -> "timeout"
        else -> "failed to connect: ${statusText(status)} ($status)"
    }

    /** A prepared write's parts, each at its offset, as one value from the first offset. */
    fun assemble(parts: List<Pair<Int, ByteArray>>): Pair<Int, ByteArray> {
        if (parts.isEmpty()) return 0 to ByteArray(0)
        val start = parts.minOf { it.first }
        val end = parts.maxOf { it.first + it.second.size }
        val out = ByteArray(end - start)
        for ((off, v) in parts) v.copyInto(out, off - start)
        return start to out
    }

    /** The CCCD's value that turns on what's asked: indications if asked and there, or if that's all it has. */
    fun cccdOn(properties: Int, indicate: Boolean): ByteArray {
        val notifies = properties and BluetoothGattCharacteristic.PROPERTY_NOTIFY != 0
        val indicates = properties and BluetoothGattCharacteristic.PROPERTY_INDICATE != 0
        return if (indicates && (indicate || !notifies)) byteArrayOf(2, 0) else byteArrayOf(1, 0)
    }

    fun b64(b: ByteArray): String = java.util.Base64.getEncoder().encodeToString(b)
    fun unb64(s: String): ByteArray = java.util.Base64.getDecoder().decode(s)

    /** The []byte at k (null and missing: none). */
    fun bytes(o: JSONObject, k: String): ByteArray = if (o.isNull(k)) ByteArray(0) else unb64(o.getString(k))

    /** The []string at k. */
    fun strings(o: JSONObject, k: String): List<String> {
        val a = o.optJSONArray(k) ?: return emptyList()
        return (0 until a.length()).map { a.getString(it) }
    }

    fun uuids(o: JSONObject, k: String): List<UUID> = strings(o, k).map { uuid(it) ?: throw IllegalArgumentException("bad args: not a UUID: $it") }

    /** A bluetoothspec.Advertisement. */
    fun advertisement(
        id: String, name: String?, rssi: Int, localName: String?, services: List<UUID>, solicited: List<UUID>,
        serviceData: Map<UUID, ByteArray>, manufacturerData: Map<Int, ByteArray>, txPower: Int?, connectable: Boolean?, time: Long,
    ): JSONObject {
        val dev = JSONObject().put("id", id)
        (name ?: localName)?.takeIf { it.isNotEmpty() }?.let { dev.put("name", it) }
        val o = JSONObject().put("device", dev).put("rssi", rssi).put("time", time)
        localName?.takeIf { it.isNotEmpty() }?.let { o.put("localName", it) }
        if (services.isNotEmpty()) o.put("services", JSONArray(services.map { string(it) }))
        if (solicited.isNotEmpty()) o.put("solicited", JSONArray(solicited.map { string(it) }))
        if (serviceData.isNotEmpty()) o.put("serviceData", JSONObject().apply { for ((k, v) in serviceData) put(string(k), b64(v)) })
        if (manufacturerData.isNotEmpty()) o.put("manufacturerData", JSONObject().apply { for ((k, v) in manufacturerData) put(k.toString(), b64(v)) })
        txPower?.let { o.put("txPower", it) }
        connectable?.let { o.put("connectable", it) }
        return o
    }

    /** Adds the advertisements to those stored (one per device, the newest, unless duplicates), the newest [keep]. */
    fun storeAdvertisements(stored: JSONArray, ads: List<JSONObject>, duplicates: Boolean, keep: Int): JSONArray {
        val all = ArrayList<JSONObject>()
        for (i in 0 until stored.length()) stored.optJSONObject(i)?.let { all.add(it) }
        for (a in ads) {
            if (!duplicates) {
                val id = a.optJSONObject("device")?.optString("id")
                all.removeAll { it.optJSONObject("device")?.optString("id") == id }
            }
            all.add(a)
        }
        return JSONArray(all.takeLast(keep))
    }
}

/**
 * asuka/sdk/bluetooth's module (internal/bluetoothspec): Bluetooth LE as a
 * central (BluetoothLeScanner, BluetoothGatt, the companion device
 * chooser) and as a peripheral (BluetoothLeAdvertiser,
 * BluetoothGattServer). Everything on the main thread: the GATT
 * callbacks come on it (connectGatt's Handler), the server's are posted
 * to it. Events wait in a queue for "next".
 */
@SuppressLint("MissingPermission") // each call checks the state, which is "on" only with the permissions granted
class AsukaBluetooth(private val rt: AsukaRuntime) {
    companion object {
        /** The module of this process, for the background scan's receiver. Main thread. */
        internal var live: AsukaBluetooth? = null
        private const val PREFS = "asuka.bluetooth"
        private const val ACTION_SCAN = "dev.asuka.bluetooth.SCAN"
        /** At most this many advertisements wait for "restore". */
        private const val KEEP = 1000

        internal fun restoredFile(c: Context) = File(c.noBackupFilesDir, "asuka-bluetooth-restored.json")

        /** The background scan's PendingIntent: the same each time, so a later run stops what an earlier started. */
        internal fun scanIntent(c: Context): PendingIntent = PendingIntent.getBroadcast(c, 0,
            Intent(c, AsukaBluetoothScanReceiver::class.java).setAction(ACTION_SCAN),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_MUTABLE) // mutable: the system fills the results in

        /** The advertisement a scan result is. */
        internal fun advertisement(c: Context, r: ScanResult): JSONObject {
            val rec = r.scanRecord
            val name = if (ContextCompat.checkSelfPermission(c, Manifest.permission.BLUETOOTH_CONNECT) == PackageManager.PERMISSION_GRANTED)
                runCatching { r.device.name }.getOrNull() else null
            val md = HashMap<Int, ByteArray>()
            rec?.manufacturerSpecificData?.let { s -> for (i in 0 until s.size()) md[s.keyAt(i)] = s.valueAt(i) }
            val time = System.currentTimeMillis() - (SystemClock.elapsedRealtimeNanos() - r.timestampNanos) / 1_000_000
            return AsukaBluetoothWire.advertisement(
                r.device.address, name, r.rssi, rec?.deviceName,
                rec?.serviceUuids?.map { it.uuid } ?: emptyList(),
                rec?.serviceSolicitationUuids?.map { it.uuid } ?: emptyList(),
                rec?.serviceData?.mapKeys { it.key.uuid } ?: emptyMap(), md,
                rec?.txPowerLevel?.takeIf { it != Int.MIN_VALUE }, r.isConnectable, time,
            )
        }

        internal fun results(i: Intent): List<ScanResult> =
            i.getParcelableArrayListExtra(BluetoothLeScanner.EXTRA_LIST_SCAN_RESULT, ScanResult::class.java) ?: emptyList()

        /** Stores what the background scan heard with no scan of Go's to give it to: "restore" answers it. */
        internal fun store(c: Context, rs: List<ScanResult>) {
            if (rs.isEmpty()) return
            val f = restoredFile(c)
            val old = runCatching { JSONArray(f.readText()) }.getOrElse { JSONArray() }
            val dup = c.getSharedPreferences(PREFS, Context.MODE_PRIVATE).getBoolean("backgroundDuplicates", false)
            val all = AsukaBluetoothWire.storeAdvertisements(old, rs.map { advertisement(c, it) }, dup, KEEP)
            runCatching {
                val tmp = File(f.path + ".tmp")
                tmp.writeText(all.toString())
                tmp.renameTo(f)
            }
        }
    }

    private val app = rt.app
    private val main = Handler(Looper.getMainLooper())
    private val manager = app.getSystemService(BluetoothManager::class.java)
    private val adapter: BluetoothAdapter? get() = manager?.adapter
    private val prefs = app.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
    private val info by lazy {
        runCatching {
            app.packageManager.getPackageInfo(app.packageName, PackageManager.PackageInfoFlags.of(PackageManager.GET_PERMISSIONS.toLong()))
        }.getOrNull()
    }
    private val declared: Set<String> by lazy { info?.requestedPermissions?.toSet() ?: emptySet() }

    /** Whether the app's scans may find where the user is: BLUETOOTH_SCAN declared without neverForLocation (asuka.json's bluetooth.location), which Android then gives results only with the location permission. */
    private val scansLocate: Boolean by lazy {
        val i = info ?: return@lazy false
        val names = i.requestedPermissions ?: return@lazy false
        val flags = i.requestedPermissionsFlags ?: return@lazy false
        val n = names.indexOf(Manifest.permission.BLUETOOTH_SCAN)
        n >= 0 && flags[n] and PackageInfo.REQUESTED_PERMISSION_NEVER_FOR_LOCATION == 0
    }

    // The events, waiting for "next".
    private val queue = ArrayList<JSONObject>()
    private var waiting: ((Result<ByteArray>) -> Unit)? = null
    private var following = true
    private var flushPosted = false

    private var lastState = ""
    private var started = false

    /** At the module's first call (every app has it, few use it): the state followed from then on. */
    private fun start() {
        if (started) return
        started = true
        live = this
        lastState = state()
        ContextCompat.registerReceiver(app, object : BroadcastReceiver() {
            override fun onReceive(c: Context, i: Intent) = stateChanged()
        }, IntentFilter(BluetoothAdapter.ACTION_STATE_CHANGED), ContextCompat.RECEIVER_NOT_EXPORTED)
        // Back from Settings: the permission may have changed.
        app.registerActivityLifecycleCallbacks(object : Application.ActivityLifecycleCallbacks {
            override fun onActivityResumed(a: Activity) = stateChanged()
            override fun onActivityCreated(a: Activity, b: Bundle?) {}
            override fun onActivityStarted(a: Activity) {}
            override fun onActivityPaused(a: Activity) {}
            override fun onActivityStopped(a: Activity) {}
            override fun onActivitySaveInstanceState(a: Activity, b: Bundle) {}
            override fun onActivityDestroyed(a: Activity) {}
        })
    }

    private fun ok(reply: (Result<ByteArray>) -> Unit) = reply(Result.success("{}".toByteArray()))
    private fun fail(reply: (Result<ByteArray>) -> Unit, why: String) = reply(Result.failure(Exception(why)))
    private fun answer(reply: (Result<ByteArray>) -> Unit, v: Any) = reply(Result.success(v.toString().toByteArray()))
    private fun args(b: ByteArray) = if (b.isEmpty()) JSONObject() else JSONObject(String(b, Charsets.UTF_8))
    private fun JSONObject.id(k: String) = optLong(k)

    // State and permissions

    /** The runtime permissions the app declares of those Bluetooth needs (Android 12's nearby devices). */
    private fun permissions(): List<String> = (listOf(
        Manifest.permission.BLUETOOTH_SCAN, Manifest.permission.BLUETOOTH_CONNECT, Manifest.permission.BLUETOOTH_ADVERTISE,
    ) + (if (scansLocate) listOf(Manifest.permission.ACCESS_FINE_LOCATION) else emptyList())).filter { it in declared }

    private fun granted(p: String) = ContextCompat.checkSelfPermission(app, p) == PackageManager.PERMISSION_GRANTED

    /** The state (bluetoothspec State*), without asking. */
    private fun state(): String {
        val a = adapter
        if (a == null || !app.packageManager.hasSystemFeature(PackageManager.FEATURE_BLUETOOTH_LE)) return "unsupported"
        val ps = permissions()
        if (ps.isEmpty()) return "denied"
        if (!ps.all { granted(it) }) return if (prefs.getBoolean("asked", false)) "denied" else "notAsked"
        return if (a.state == BluetoothAdapter.STATE_ON) "on" else "off"
    }

    /** A state's error, for what it stops. */
    private fun stateError(s: String) = if (s == "notAsked") "denied" else s

    /** Fails reply unless Bluetooth is on and allowed. */
    private fun on(reply: (Result<ByteArray>) -> Unit): Boolean {
        val s = state()
        if (s == "on") return true
        fail(reply, stateError(s))
        return false
    }

    private fun stateChanged() {
        val s = state()
        if (s == lastState) return
        lastState = s
        emit(JSONObject().put("type", "state").put("state", s))
        if (s != "on") stopped(stateError(s))
    }

    /** Bluetooth went (off, not allowed): what ran ends, as Android ends it. */
    private fun stopped(why: String) {
        for (m in listOf(foreground, background)) m.ended(why)
        for (c in conns.values.toList()) c.ended(0, off = true, why = why)
        for ((id, a) in adverts.toList()) {
            adverts.remove(id)
            runCatching { adapter?.bluetoothLeAdvertiser?.stopAdvertising(a) }
            val r = a.reply
            if (r != null) { a.reply = null; fail(r, why) }
            else emit(JSONObject().put("type", "advertiseStopped").put("id", id).put("error", why))
        }
        for (id in servers.keys.toList()) {
            val r = serving.remove(id)
            if (r != null) fail(r, why) else emit(JSONObject().put("type", "serverStopped").put("server", id).put("error", why))
        }
        closeServer()
    }

    // Events

    private fun emit(e: JSONObject) {
        if (!following && e.optString("type") == "advertisement") return // nothing in Go listens
        queue.add(e)
        if (waiting != null && !flushPosted) {
            // A burst (a batch of results) goes as one answer.
            flushPosted = true
            main.post { flushPosted = false; flush() }
        }
    }

    private fun flush() {
        val w = waiting ?: return
        if (queue.isEmpty()) return
        waiting = null
        val out = JSONArray(queue).toString().toByteArray()
        queue.clear()
        w(Result.success(out))
    }

    // Scans

    private class GoScan(val id: Long, val services: List<UUID>, val duplicates: Boolean, val mode: Int) {
        val seen = HashSet<String>()
        fun matches(uuids: List<UUID>) = services.isEmpty() || services.any { it in uuids }
    }

    private fun mode(s: String, background: Boolean) = when (s) {
        "lowPower" -> ScanSettings.SCAN_MODE_LOW_POWER
        "balanced" -> ScanSettings.SCAN_MODE_BALANCED
        "lowLatency" -> ScanSettings.SCAN_MODE_LOW_LATENCY
        "opportunistic" -> ScanSettings.SCAN_MODE_OPPORTUNISTIC
        else -> if (background) ScanSettings.SCAN_MODE_LOW_POWER else ScanSettings.SCAN_MODE_LOW_LATENCY
    }

    /** The recent scan starts (elapsedRealtime), the throttle's: foreground and background alike. */
    private val starts = ArrayList<Long>()

    private fun filters(c: AsukaBluetoothWire.ScanConfig): List<ScanFilter> =
        c.services?.map { ScanFilter.Builder().setServiceUuid(ParcelUuid(it)).build() } ?: emptyList()

    private fun settings(c: AsukaBluetoothWire.ScanConfig): ScanSettings {
        val b = ScanSettings.Builder().setScanMode(c.mode).setCallbackType(ScanSettings.CALLBACK_TYPE_ALL_MATCHES)
        if (adapter?.isLeExtendedAdvertisingSupported == true) b.setLegacy(false).setPhy(ScanSettings.PHY_LE_ALL_SUPPORTED)
        return b.build()
    }

    /**
     * Go's scans that one scanner serves, restarted as the set changes:
     * the foreground's (a ScanCallback), the background's (a PendingIntent
     * scan, which outlives the process). A restart waits for the throttle;
     * one that would only narrow the scan waits a moment more, for others.
     */
    private inner class Merged(val isBackground: Boolean) {
        val scans = LinkedHashMap<Long, GoScan>()
        var running: AsukaBluetoothWire.ScanConfig? = null
        val pending = ArrayList<(Result<ByteArray>) -> Unit>()
        private var callback: ScanCallback? = null
        private var restartAt = -1L
        private val restart = Runnable { restartAt = -1; apply() }

        fun want() = AsukaBluetoothWire.merge(scans.values.map { it.services to it.mode })

        fun add(s: GoScan, reply: (Result<ByteArray>) -> Unit) {
            scans[s.id] = s
            val want = want()!!
            val run = running
            if (run != null && AsukaBluetoothWire.covers(run, want)) {
                if (isBackground) saveBackground()
                ok(reply)
                return
            }
            pending.add(reply)
            schedule(0)
        }

        fun remove(id: Long): Boolean {
            if (scans.remove(id) == null) return false
            val want = want()
            if (want == null) stop()
            else if (want != running) schedule(2000)
            return true
        }

        private fun schedule(wait: Long) {
            val now = SystemClock.elapsedRealtime()
            val at = now + maxOf(wait, AsukaBluetoothWire.throttleDelay(starts, now))
            if (restartAt in 0..at) return
            main.removeCallbacks(restart)
            restartAt = at
            main.postDelayed(restart, at - now)
        }

        private fun apply() {
            val want = want() ?: return stop()
            if (want == running) { answerPending(); return }
            val now = SystemClock.elapsedRealtime()
            val d = AsukaBluetoothWire.throttleDelay(starts, now)
            if (d > 0) { schedule(d); return }
            val scanner = adapter?.bluetoothLeScanner ?: return ended("off")
            try {
                stopScanner(scanner)
                if (isBackground) {
                    saveBackground()
                    val code = scanner.startScan(filters(want), settings(want), scanIntent(app))
                    if (code != 0) return ended(scanFailure(code))
                } else {
                    val cb = object : ScanCallback() {
                        override fun onScanResult(type: Int, r: ScanResult) { if (callback === this) dispatch(this@Merged, r) }
                        override fun onBatchScanResults(rs: MutableList<ScanResult>) { if (callback === this) for (r in rs) dispatch(this@Merged, r) }
                        override fun onScanFailed(code: Int) { if (callback === this) { callback = null; ended(scanFailure(code)) } }
                    }
                    callback = cb
                    scanner.startScan(filters(want), settings(want), cb)
                }
            } catch (e: SecurityException) {
                return ended("denied")
            } catch (e: Exception) {
                return ended("failed: ${e.message}")
            }
            starts.add(now)
            starts.removeAll { now - it >= 2 * AsukaBluetoothWire.THROTTLE_WINDOW }
            running = want
            answerPending()
        }

        private fun answerPending() {
            val ps = ArrayList(pending)
            pending.clear()
            for (p in ps) ok(p)
        }

        private fun stopScanner(scanner: BluetoothLeScanner) {
            // The background scan's PendingIntent may be scanning still, a previous run's: stopped first.
            if (isBackground) runCatching { scanner.stopScan(scanIntent(app)) }
            else callback?.let { cb -> callback = null; runCatching { scanner.stopScan(cb) } }
        }

        /** No scans left: the scanner stops (a background scan, also after the process). */
        fun stop() {
            main.removeCallbacks(restart)
            restartAt = -1
            adapter?.bluetoothLeScanner?.let { stopScanner(it) }
            callback = null
            running = null
            if (isBackground) prefs.edit().remove("backgroundDuplicates").apply()
        }

        /** The scanner stopped by itself (or couldn't start): every scan ends with why. */
        fun ended(why: String) {
            main.removeCallbacks(restart)
            restartAt = -1
            if (!isBackground) callback?.let { cb -> callback = null; runCatching { adapter?.bluetoothLeScanner?.stopScan(cb) } }
            running = null
            val ps = ArrayList(pending)
            pending.clear()
            for (p in ps) fail(p, why)
            for (id in scans.keys) emit(JSONObject().put("type", "scanStopped").put("scan", id).put("error", why))
            scans.clear()
        }

        private fun saveBackground() {
            prefs.edit().putBoolean("backgroundDuplicates", scans.values.any { it.duplicates }).apply()
        }
    }

    private val foreground = Merged(false)
    private val background = Merged(true)

    private fun scanFailure(code: Int) = when (code) {
        ScanCallback.SCAN_FAILED_ALREADY_STARTED -> "failed: already scanning"
        ScanCallback.SCAN_FAILED_APPLICATION_REGISTRATION_FAILED -> "failed: the app couldn't register to scan"
        ScanCallback.SCAN_FAILED_INTERNAL_ERROR -> "failed: internal error"
        ScanCallback.SCAN_FAILED_FEATURE_UNSUPPORTED -> "not available: this device can't scan so"
        ScanCallback.SCAN_FAILED_OUT_OF_HARDWARE_RESOURCES -> "failed: out of hardware resources"
        ScanCallback.SCAN_FAILED_SCANNING_TOO_FREQUENTLY -> "failed: scanning too frequently"
        else -> "failed: scan error $code"
    }

    /** Gives a result to each scan that wants it. */
    private fun dispatch(m: Merged, r: ScanResult) {
        val uuids = r.scanRecord?.serviceUuids?.map { it.uuid } ?: emptyList()
        val address = r.device.address
        var ad: JSONObject? = null
        for (s in m.scans.values) {
            if (!s.matches(uuids)) continue
            if (!s.duplicates && !s.seen.add(address)) continue
            if (ad == null) ad = advertisement(app, r)
            emit(JSONObject().put("type", "advertisement").put("scan", s.id).put("advertisement", ad))
        }
    }

    /** The background scan's results, while this process runs: Go's background scans', if it has some (else false: stored). */
    internal fun backgroundResults(i: Intent): Boolean {
        if (background.scans.isEmpty()) return false
        val err = i.getIntExtra(BluetoothLeScanner.EXTRA_ERROR_CODE, 0)
        if (err != 0) {
            background.ended(scanFailure(err))
            return true
        }
        for (r in results(i)) dispatch(background, r)
        return true
    }

    /** The receiver stored results: Go, if it follows, asks for them ("restore"). */
    internal fun restoredStored() {
        if (following) emit(JSONObject().put("type", "restored"))
    }

    private fun hasScanReceiver(): Boolean =
        app.packageManager.queryBroadcastReceivers(Intent(app, AsukaBluetoothScanReceiver::class.java), PackageManager.ResolveInfoFlags.of(0)).isNotEmpty()

    // GATT client

    private val conns = HashMap<Long, Conn>()

    private class Op(val kind: Int, val reply: (Result<ByteArray>) -> Unit, val start: (Op) -> Unit)

    private object Kind {
        const val DISCOVER = 1; const val READ = 2; const val WRITE = 3; const val READ_DESC = 4; const val WRITE_DESC = 5
        const val MTU = 6; const val RSSI = 7; const val NOW = 8
    }

    private fun att(status: Int): Result<ByteArray> = Result.failure(Exception("att $status: ${AsukaBluetoothWire.statusText(status)}"))

    /** A connection: its BluetoothGatt, its one GATT operation at a time, its handles. */
    private inner class Conn(val id: Long, val device: BluetoothDevice) : BluetoothGattCallback() {
        var gatt: BluetoothGatt? = null
        var up = false
        var closing = false
        var connecting: ((Result<ByteArray>) -> Unit)? = null
        val disconnecting = ArrayList<(Result<ByteArray>) -> Unit>()
        val ops = ArrayDeque<Op>()
        var current: Op? = null
        private var pumping = false
        var mtu = 23
        // The handles "services" gave, the same until the services change.
        var tree: JSONArray? = null
        val byHandle = HashMap<Int, Any>()
        val handles = IdentityHashMap<Any, Int>()
        private var nextHandle = 0

        fun mine() = conns[id] === this

        fun enqueue(kind: Int, reply: (Result<ByteArray>) -> Unit, start: (Op) -> Unit) {
            if (!up || closing) { fail(reply, "disconnected"); return }
            ops.add(Op(kind, reply, start))
            pump()
        }

        fun pump() {
            if (pumping) return
            pumping = true
            while (current == null && up && ops.isNotEmpty()) {
                val op = ops.removeFirst()
                current = op
                try {
                    op.start(op)
                } catch (e: SecurityException) {
                    done(op, Result.failure(Exception("denied")))
                } catch (e: Exception) {
                    done(op, Result.failure(e))
                }
            }
            pumping = false
        }

        /** Ends op (the one under way) with r; the next goes. */
        fun done(op: Op, r: Result<ByteArray>) {
            if (current !== op) return
            current = null
            op.reply(r)
            pump()
        }

        /** A callback of kind ends the operation under way, if it's of that kind. */
        fun complete(kind: Int, r: Result<ByteArray>) {
            val op = current ?: return
            if (op.kind == kind) done(op, r)
        }

        fun failOps(why: String) {
            val all = listOfNotNull(current) + ops
            current = null
            ops.clear()
            for (op in all) fail(op.reply, why)
        }

        /** The connection's over: status Android's, off when Bluetooth went (why: the error). */
        fun ended(status: Int, off: Boolean = false, why: String = "off") {
            if (!mine()) return
            conns.remove(id)
            failOps("disconnected")
            runCatching { gatt?.close() }
            val c = connecting
            connecting = null
            when {
                c != null -> fail(c, if (off) why else AsukaBluetoothWire.connectError(status))
                closing -> {}
                up -> {
                    val e = JSONObject().put("type", "disconnected").put("conn", id)
                    if (off) e.put("reason", if (why == "off") "off" else "unknown").put("error", "Bluetooth is $why")
                    else e.put("reason", AsukaBluetoothWire.reason(status)).put("error", AsukaBluetoothWire.statusText(status)).put("code", status)
                    emit(e)
                }
            }
            up = false
            val ds = ArrayList(disconnecting)
            disconnecting.clear()
            for (d in ds) ok(d)
        }

        fun handle(o: Any): Int = handles.getOrPut(o) { (++nextHandle).also { byHandle[it] = o } }

        fun buildTree(): JSONArray {
            tree?.let { return it }
            val out = JSONArray()
            for (s in gatt?.services ?: emptyList()) {
                val cs = JSONArray()
                for (c in s.characteristics) {
                    val ds = JSONArray()
                    for (d in c.descriptors) ds.put(JSONObject().put("handle", handle(d)).put("uuid", AsukaBluetoothWire.string(d.uuid)))
                    val jc = JSONObject().put("handle", handle(c)).put("uuid", AsukaBluetoothWire.string(c.uuid)).put("properties", c.properties and 0xff)
                    if (ds.length() > 0) jc.put("descriptors", ds)
                    cs.put(jc)
                }
                val js = JSONObject().put("handle", handle(s)).put("uuid", AsukaBluetoothWire.string(s.uuid))
                    .put("primary", s.type == BluetoothGattService.SERVICE_TYPE_PRIMARY)
                if (cs.length() > 0) js.put("characteristics", cs)
                out.put(js)
            }
            tree = out
            return out
        }

        fun mtuJson() = JSONObject().put("mtu", mtu).put("writeWithResponse", mtu - 3).put("writeWithoutResponse", mtu - 3)

        // Callbacks: on the main thread (connectGatt's Handler).

        override fun onConnectionStateChange(g: BluetoothGatt, status: Int, newState: Int) {
            if (!mine()) return
            if (newState == BluetoothProfile.STATE_CONNECTED && status == BluetoothGatt.GATT_SUCCESS) {
                if (!up) {
                    up = true
                    connecting?.let { connecting = null; ok(it) }
                    pump()
                }
                return
            }
            if (newState == BluetoothProfile.STATE_DISCONNECTED || status != BluetoothGatt.GATT_SUCCESS) ended(status)
        }

        override fun onServicesDiscovered(g: BluetoothGatt, status: Int) {
            if (!mine()) return
            complete(Kind.DISCOVER, if (status == BluetoothGatt.GATT_SUCCESS) Result.success(buildTree().toString().toByteArray()) else att(status))
        }

        override fun onServiceChanged(g: BluetoothGatt) {
            if (!mine()) return
            tree = null
            byHandle.clear()
            handles.clear()
            emit(JSONObject().put("type", "servicesChanged").put("conn", id))
        }

        override fun onCharacteristicRead(g: BluetoothGatt, c: BluetoothGattCharacteristic, value: ByteArray, status: Int) {
            if (mine()) complete(Kind.READ, if (status == BluetoothGatt.GATT_SUCCESS) Result.success(value) else att(status))
        }

        override fun onCharacteristicWrite(g: BluetoothGatt?, c: BluetoothGattCharacteristic?, status: Int) {
            if (mine()) complete(Kind.WRITE, if (status == BluetoothGatt.GATT_SUCCESS) Result.success("{}".toByteArray()) else att(status))
        }

        override fun onDescriptorRead(g: BluetoothGatt, d: BluetoothGattDescriptor, status: Int, value: ByteArray) {
            if (mine()) complete(Kind.READ_DESC, if (status == BluetoothGatt.GATT_SUCCESS) Result.success(value) else att(status))
        }

        override fun onDescriptorWrite(g: BluetoothGatt?, d: BluetoothGattDescriptor?, status: Int) {
            if (mine()) complete(Kind.WRITE_DESC, if (status == BluetoothGatt.GATT_SUCCESS) Result.success("{}".toByteArray()) else att(status))
        }

        override fun onCharacteristicChanged(g: BluetoothGatt, c: BluetoothGattCharacteristic, value: ByteArray) {
            if (!mine()) return
            val h = handles[c] ?: return
            emit(JSONObject().put("type", "value").put("conn", id).put("handle", h).put("value", AsukaBluetoothWire.b64(value)))
        }

        override fun onMtuChanged(g: BluetoothGatt?, mtu: Int, status: Int) {
            if (!mine()) return
            if (status == BluetoothGatt.GATT_SUCCESS) this.mtu = mtu
            complete(Kind.MTU, Result.success(mtuJson().toString().toByteArray()))
        }

        override fun onReadRemoteRssi(g: BluetoothGatt?, rssi: Int, status: Int) {
            if (mine()) complete(Kind.RSSI, if (status == BluetoothGatt.GATT_SUCCESS) Result.success(JSONObject().put("rssi", rssi).toString().toByteArray()) else att(status))
        }
    }

    /** The connection named, up; else reply fails ("disconnected"). */
    private fun conn(a: JSONObject, reply: (Result<ByteArray>) -> Unit): Conn? {
        val c = conns[a.id("conn")]
        if (c == null || !c.up || c.closing) { fail(reply, "disconnected"); return null }
        return c
    }

    private fun Conn.characteristic(a: JSONObject, reply: (Result<ByteArray>) -> Unit): BluetoothGattCharacteristic? =
        byHandle[a.optInt("handle")] as? BluetoothGattCharacteristic ?: run { fail(reply, "not found"); null }

    private fun Conn.descriptor(a: JSONObject, reply: (Result<ByteArray>) -> Unit): BluetoothGattDescriptor? =
        byHandle[a.optInt("handle")] as? BluetoothGattDescriptor ?: run { fail(reply, "not found"); null }

    /** Writes a characteristic: write is Android's call, retried while the stack is busy. */
    private fun Conn.writeOp(op: Op, write: () -> Int) {
        when (val code = write()) {
            BluetoothStatusCodes.SUCCESS -> {}
            BluetoothStatusCodes.ERROR_GATT_WRITE_REQUEST_BUSY -> main.postDelayed({ if (current === op && mine()) runCatching { writeOp(op, write) } }, 10)
            BluetoothStatusCodes.ERROR_GATT_WRITE_NOT_ALLOWED -> done(op, Result.failure(Exception("att 3: write not permitted")))
            BluetoothStatusCodes.ERROR_MISSING_BLUETOOTH_CONNECT_PERMISSION -> done(op, Result.failure(Exception("denied")))
            else -> done(op, Result.failure(Exception("failed: the system refused the write (status $code)")))
        }
    }

    private fun Conn.started(op: Op, ok: Boolean, what: String) {
        if (!ok) done(op, Result.failure(Exception("failed: the system refused the $what")))
    }

    private fun device(d: BluetoothDevice): JSONObject {
        val o = JSONObject().put("id", d.address)
        if (granted(Manifest.permission.BLUETOOTH_CONNECT)) runCatching { d.name }.getOrNull()?.takeIf { it.isNotEmpty() }?.let { o.put("name", it) }
        return o
    }

    // Peripheral: advertising

    private inner class Advert(val id: Long, var reply: ((Result<ByteArray>) -> Unit)?) : AdvertiseCallback() {
        override fun onStartSuccess(settings: AdvertiseSettings?) {
            reply?.let { reply = null; ok(it) }
        }

        override fun onStartFailure(code: Int) {
            if (adverts[id] !== this) return
            adverts.remove(id)
            val why = when (code) {
                ADVERTISE_FAILED_DATA_TOO_LARGE -> "failed: data too large (an advertisement has 31 bytes)"
                ADVERTISE_FAILED_TOO_MANY_ADVERTISERS -> "failed: too many advertisers"
                ADVERTISE_FAILED_ALREADY_STARTED -> "failed: already advertising"
                ADVERTISE_FAILED_FEATURE_UNSUPPORTED -> "not available: this device can't advertise"
                else -> "failed: internal error ($code)"
            }
            val r = reply
            if (r != null) { reply = null; fail(r, why) }
            else emit(JSONObject().put("type", "advertiseStopped").put("id", id).put("error", why))
        }
    }

    private val adverts = HashMap<Long, Advert>()

    // Peripheral: the GATT server (one, for all of Go's servers)

    private class LocalChar(val server: Long, val handle: Int, val char: BluetoothGattCharacteristic, val value: ByteArray?)
    private class Sub(val device: BluetoothDevice, val indicate: Boolean)
    /** Waits for the answers to an executed prepared write's parts. */
    private class Execute(val device: BluetoothDevice, val request: Int, var left: Int) { var error = 0 }
    private class Req(val device: BluetoothDevice, val request: Int, val offset: Int, val execute: Execute?)
    private class Notification(val lc: LocalChar, val value: ByteArray, val indicate: Boolean, val sent: () -> Unit)

    private var gattServer: BluetoothGattServer? = null
    /** Go's servers' services, by server. */
    private val servers = HashMap<Long, MutableList<BluetoothGattService>>()
    /** The "serve" calls not answered yet. */
    private val serving = HashMap<Long, (Result<ByteArray>) -> Unit>()
    private val localChars = IdentityHashMap<BluetoothGattCharacteristic, LocalChar>()
    private val localByHandle = HashMap<Pair<Long, Int>, LocalChar>()
    private val descValues = IdentityHashMap<BluetoothGattDescriptor, ByteArray>()
    /** Each notifying characteristic's subscribers, by address. */
    private val subs = IdentityHashMap<LocalChar, LinkedHashMap<String, Sub>>()
    private val requests = HashMap<Int, Req>()
    private var nextRequest = 0
    /** Prepared writes' parts, by central, then characteristic, until executed. */
    private val prepared = HashMap<String, LinkedHashMap<BluetoothGattCharacteristic, MutableList<Pair<Int, ByteArray>>>>()
    /** Services to add, one at a time (each waits for onServiceAdded). */
    private val adding = ArrayDeque<Pair<Pair<Long, BluetoothGattService>, (Boolean) -> Unit>>()
    private var addingNow: ((Boolean) -> Unit)? = null
    /** Notifications to send, by central, one at a time (each waits for onNotificationSent). */
    private val notifying = HashMap<String, ArrayDeque<Notification>>()
    private val sending = HashMap<String, Notification>()

    private val serverCallback = object : BluetoothGattServerCallback() {
        // Binder threads: each to the main thread.
        override fun onServiceAdded(status: Int, service: BluetoothGattService) {
            main.post { addingNow?.let { addingNow = null; it(status == BluetoothGatt.GATT_SUCCESS) }; addNext() }
        }

        override fun onConnectionStateChange(device: BluetoothDevice, status: Int, newState: Int) {
            if (newState == BluetoothProfile.STATE_DISCONNECTED) main.post { centralGone(device) }
        }

        override fun onCharacteristicReadRequest(device: BluetoothDevice, requestId: Int, offset: Int, c: BluetoothGattCharacteristic) {
            main.post { readRequest(device, requestId, offset, c) }
        }

        override fun onCharacteristicWriteRequest(device: BluetoothDevice, requestId: Int, c: BluetoothGattCharacteristic, preparedWrite: Boolean, responseNeeded: Boolean, offset: Int, value: ByteArray?) {
            main.post { writeRequest(device, requestId, c, preparedWrite, responseNeeded, offset, value ?: ByteArray(0)) }
        }

        override fun onDescriptorReadRequest(device: BluetoothDevice, requestId: Int, offset: Int, d: BluetoothGattDescriptor) {
            main.post { descriptorRead(device, requestId, offset, d) }
        }

        override fun onDescriptorWriteRequest(device: BluetoothDevice, requestId: Int, d: BluetoothGattDescriptor, preparedWrite: Boolean, responseNeeded: Boolean, offset: Int, value: ByteArray?) {
            main.post { descriptorWrite(device, requestId, d, responseNeeded, offset, value ?: ByteArray(0)) }
        }

        override fun onExecuteWrite(device: BluetoothDevice, requestId: Int, execute: Boolean) {
            main.post { executeWrite(device, requestId, execute) }
        }

        override fun onNotificationSent(device: BluetoothDevice, status: Int) {
            main.post {
                sending.remove(device.address)?.sent?.invoke()
                sendNext(device.address)
            }
        }
    }

    private fun respond(device: BluetoothDevice, request: Int, status: Int, offset: Int, value: ByteArray?) {
        runCatching { gattServer?.sendResponse(device, request, status, offset, value) }
    }

    /** A fixed value, from offset. */
    private fun respondFixed(device: BluetoothDevice, request: Int, offset: Int, v: ByteArray) {
        if (offset > v.size) respond(device, request, 7, offset, null) // invalid offset
        else respond(device, request, BluetoothGatt.GATT_SUCCESS, offset, v.copyOfRange(offset, v.size))
    }

    private fun central(d: BluetoothDevice) = device(d)

    private fun readRequest(device: BluetoothDevice, request: Int, offset: Int, c: BluetoothGattCharacteristic) {
        val lc = localChars[c] ?: return respond(device, request, 1, offset, null) // invalid handle
        lc.value?.let { return respondFixed(device, request, offset, it) }
        val req = ++nextRequest
        requests[req] = Req(device, request, offset, null)
        emit(JSONObject().put("type", "read").put("server", lc.server).put("request", req).put("central", central(device))
            .put("handle", lc.handle).put("offset", offset))
    }

    private fun writeRequest(device: BluetoothDevice, request: Int, c: BluetoothGattCharacteristic, preparedWrite: Boolean, responseNeeded: Boolean, offset: Int, value: ByteArray) {
        val lc = localChars[c] ?: return run { if (responseNeeded) respond(device, request, 1, offset, null) }
        if (preparedWrite) {
            // A long write's part: kept until executed (the response echoes it).
            prepared.getOrPut(device.address) { LinkedHashMap() }.getOrPut(c) { ArrayList() }.add(offset to value)
            if (responseNeeded) respond(device, request, BluetoothGatt.GATT_SUCCESS, offset, value)
            return
        }
        val e = JSONObject().put("type", "write").put("server", lc.server).put("central", central(device))
            .put("handle", lc.handle).put("offset", offset).put("value", AsukaBluetoothWire.b64(value)).put("response", responseNeeded)
        if (responseNeeded) {
            val req = ++nextRequest
            requests[req] = Req(device, request, offset, null)
            e.put("request", req)
        }
        emit(e)
    }

    private fun executeWrite(device: BluetoothDevice, request: Int, execute: Boolean) {
        val parts = prepared.remove(device.address)
        if (!execute || parts.isNullOrEmpty()) return respond(device, request, BluetoothGatt.GATT_SUCCESS, 0, null)
        val ex = Execute(device, request, parts.size)
        for ((c, ps) in parts) {
            val lc = localChars[c]
            if (lc == null) { answerExecute(ex, 1); continue }
            val (offset, value) = AsukaBluetoothWire.assemble(ps)
            val req = ++nextRequest
            requests[req] = Req(device, request, 0, ex)
            emit(JSONObject().put("type", "write").put("server", lc.server).put("request", req).put("central", central(device))
                .put("handle", lc.handle).put("offset", offset).put("value", AsukaBluetoothWire.b64(value)).put("response", true))
        }
    }

    private fun answerExecute(ex: Execute, error: Int) {
        if (ex.error == 0) ex.error = error
        if (--ex.left == 0) respond(ex.device, ex.request, ex.error, 0, null)
    }

    private fun descriptorRead(device: BluetoothDevice, request: Int, offset: Int, d: BluetoothGattDescriptor) {
        val lc = d.characteristic?.let { localChars[it] }
        if (d.uuid == AsukaBluetoothWire.CCCD && lc != null) {
            val s = subs[lc]?.get(device.address)
            return respondFixed(device, request, offset, if (s == null) byteArrayOf(0, 0) else if (s.indicate) byteArrayOf(2, 0) else byteArrayOf(1, 0))
        }
        val v = descValues[d] ?: return respond(device, request, 1, offset, null)
        respondFixed(device, request, offset, v)
    }

    private fun descriptorWrite(device: BluetoothDevice, request: Int, d: BluetoothGattDescriptor, responseNeeded: Boolean, offset: Int, value: ByteArray) {
        val lc = d.characteristic?.let { localChars[it] }
        if (d.uuid != AsukaBluetoothWire.CCCD || lc == null) {
            if (responseNeeded) respond(device, request, 3, offset, null) // write not permitted: the app's descriptors are fixed
            return
        }
        val bits = value.firstOrNull()?.toInt() ?: 0
        val on = bits and 3 != 0
        val m = subs.getOrPut(lc) { LinkedHashMap() }
        val was = m.containsKey(device.address)
        if (on) m[device.address] = Sub(device, bits and 2 != 0 && bits and 1 == 0) else m.remove(device.address)
        if (responseNeeded) respond(device, request, BluetoothGatt.GATT_SUCCESS, offset, value)
        if (was != on) emit(JSONObject().put("type", "subscription").put("server", lc.server).put("central", central(device))
            .put("handle", lc.handle).put("on", on))
    }

    /** A central disconnected: its subscriptions end, its writes and notifications go. */
    private fun centralGone(device: BluetoothDevice) {
        val a = device.address
        for ((lc, m) in subs) {
            if (m.remove(a) != null) emit(JSONObject().put("type", "subscription").put("server", lc.server).put("central", central(device))
                .put("handle", lc.handle).put("on", false))
        }
        prepared.remove(a)
        requests.entries.removeAll { it.value.device.address == a }
        sending.remove(a)?.sent?.invoke()
        notifying.remove(a)?.forEach { it.sent() }
    }

    private fun sendNext(address: String) {
        if (sending.containsKey(address)) return
        val q = notifying[address] ?: return
        val n = q.removeFirstOrNull() ?: run { notifying.remove(address); return }
        val sub = subs[n.lc]?.get(address)
        val s = gattServer
        if (sub == null || s == null) { n.sent(); return sendNext(address) }
        val code = runCatching { s.notifyCharacteristicChanged(sub.device, n.lc.char, n.indicate, n.value) }.getOrDefault(BluetoothStatusCodes.ERROR_UNKNOWN)
        when (code) {
            BluetoothStatusCodes.SUCCESS -> sending[address] = n
            BluetoothStatusCodes.ERROR_GATT_WRITE_REQUEST_BUSY -> { q.addFirst(n); main.postDelayed({ sendNext(address) }, 10) }
            else -> { n.sent(); sendNext(address) }
        }
    }

    private fun addNext() {
        if (addingNow != null) return
        while (adding.isNotEmpty()) {
            val (what, done) = adding.removeFirst()
            val (server, svc) = what
            val s = gattServer
            if (s == null || !servers.containsKey(server)) { done(false); continue }
            addingNow = done
            if (runCatching { s.addService(svc) }.getOrDefault(false)) return
            addingNow = null
            done(false)
        }
    }

    private fun closeServer() {
        runCatching { gattServer?.close() }
        gattServer = null
        servers.clear()
        serving.clear()
        localChars.clear()
        localByHandle.clear()
        descValues.clear()
        subs.clear()
        requests.clear()
        prepared.clear()
        val queued = adding.map { it.second }
        adding.clear()
        addingNow = null
        for (d in queued) d(false)
        for (n in sending.values) n.sent()
        for (q in notifying.values) for (n in q) n.sent()
        sending.clear()
        notifying.clear()
    }

    private fun unserve(id: Long) {
        val svcs = servers.remove(id) ?: return
        serving.remove(id)
        for (svc in svcs) runCatching { gattServer?.removeService(svc) }
        val gone = localChars.values.filter { it.server == id }
        for (lc in gone) {
            localChars.remove(lc.char)
            localByHandle.remove(lc.server to lc.handle)
            subs.remove(lc)
            for (d in lc.char.descriptors) descValues.remove(d)
        }
        if (servers.isEmpty()) closeServer()
    }

    // Choose: the companion device chooser

    private var choosers = 0

    private fun choose(a: JSONObject, reply: (Result<ByteArray>) -> Unit) {
        if (!app.packageManager.hasSystemFeature(PackageManager.FEATURE_COMPANION_DEVICE_SETUP)) {
            return fail(reply, "not available: this device has no companion device chooser")
        }
        if (!on(reply)) return
        val act = rt.activity ?: return fail(reply, "no screen")
        val cdm = app.getSystemService(CompanionDeviceManager::class.java) ?: return fail(reply, "not available: no companion device manager")
        val req = AssociationRequest.Builder().setSingleDevice(true)
        val fs = a.optJSONArray("filters")
        if (fs == null || fs.length() == 0) req.addDeviceFilter(BluetoothLeDeviceFilter.Builder().build())
        else for (i in 0 until fs.length()) {
            val f = fs.getJSONObject(i)
            val scan = ScanFilter.Builder()
            // A ScanFilter has one service: the filter's first (Android can't ask for all of them).
            AsukaBluetoothWire.uuids(f, "services").firstOrNull()?.let { scan.setServiceUuid(ParcelUuid(it)) }
            if (!f.isNull("manufacturer") && f.has("manufacturer")) scan.setManufacturerData(f.getInt("manufacturer"), ByteArray(0))
            val b = BluetoothLeDeviceFilter.Builder().setScanFilter(scan.build())
            val name = f.optString("name")
            val prefix = f.optString("namePrefix")
            when {
                name.isNotEmpty() -> b.setNamePattern(Pattern.compile(Pattern.quote(name)))
                prefix.isNotEmpty() -> b.setNamePattern(Pattern.compile(Pattern.quote(prefix) + ".*", Pattern.DOTALL))
            }
            req.addDeviceFilter(b.build())
        }
        var answered = false
        fun done(r: Result<ByteArray>) { if (!answered) { answered = true; reply(r) } }
        fun chosen(info: AssociationInfo): Boolean {
            val mac = info.deviceMacAddress ?: return false
            val o = JSONObject().put("id", mac.toString().uppercase())
            val advertised = if (android.os.Build.VERSION.SDK_INT >= 34) info.associatedDevice?.bleDevice?.scanRecord?.deviceName else null
            (advertised ?: info.displayName?.toString())?.takeIf { it.isNotEmpty() }?.let { o.put("name", it) }
            done(Result.success(o.toString().toByteArray()))
            return true
        }
        cdm.associate(req.build(), ContextCompat.getMainExecutor(app), object : CompanionDeviceManager.Callback() {
            override fun onAssociationPending(sender: IntentSender) {
                val key = "asuka-bluetooth-choose-${choosers++}"
                var l: androidx.activity.result.ActivityResultLauncher<IntentSenderRequest>? = null
                l = act.activityResultRegistry.register(key, ActivityResultContracts.StartIntentSenderForResult()) { res ->
                    l?.unregister()
                    if (res.resultCode != Activity.RESULT_OK) return@register done(Result.failure(Exception("canceled")))
                    val data = res.data ?: return@register
                    data.getParcelableExtra(CompanionDeviceManager.EXTRA_ASSOCIATION, AssociationInfo::class.java)?.let { if (chosen(it)) return@register }
                    @Suppress("DEPRECATION")
                    val r = data.getParcelableExtra(CompanionDeviceManager.EXTRA_DEVICE, ScanResult::class.java)
                    if (r != null) {
                        val o = device(r.device)
                        if (!o.has("name")) r.scanRecord?.deviceName?.takeIf { it.isNotEmpty() }?.let { o.put("name", it) }
                        done(Result.success(o.toString().toByteArray()))
                    }
                    // Else onAssociationCreated answers.
                }
                l.launch(IntentSenderRequest.Builder(sender).build())
            }

            override fun onAssociationCreated(info: AssociationInfo) { chosen(info) }

            override fun onFailure(error: CharSequence?) {
                val m = error?.toString() ?: ""
                done(Result.failure(Exception(when {
                    m.contains("cancel", true) || m.contains("reject", true) -> "canceled"
                    m.contains("timeout", true) -> "timeout"
                    else -> "failed: $m"
                })))
            }
        })
    }

    // The methods

    fun methods(): Map<String, AsukaModuleMethod> = calls().mapValues { (_, f) ->
        { args: ByteArray, reply: (Result<ByteArray>) -> Unit -> start(); f(args, reply) }
    }

    private fun calls(): Map<String, AsukaModuleMethod> = mapOf(
        "state" to { _, reply -> answer(reply, JSONObject().put("state", state())) },
        "request" to { _, reply ->
            val s = state()
            val missing = permissions().filter { !granted(it) }
            if (s == "unsupported" || missing.isEmpty() || rt.activity == null) answer(reply, JSONObject().put("state", s))
            else {
                prefs.edit().putBoolean("asked", true).apply()
                rt.withPermission(missing.toTypedArray()) {
                    answer(reply, JSONObject().put("state", state()))
                    stateChanged()
                }
            }
        },
        "next" to { _, reply ->
            following = true
            waiting = reply
            flush()
        },
        "stop" to { _, reply ->
            following = false
            queue.removeAll { it.optString("type") == "advertisement" }
            waiting?.let { waiting = null; fail(it, "stopped") }
            ok(reply)
        },
        "scan" to { args, reply ->
            val a = args(args)
            val bg = a.optBoolean("background")
            val s = GoScan(a.id("id"), AsukaBluetoothWire.uuids(a, "services"), a.optBoolean("duplicates"), mode(a.optString("mode"), bg))
            when {
                !on(reply) -> {}
                adapter?.bluetoothLeScanner == null -> fail(reply, "off")
                bg && !hasScanReceiver() -> fail(reply, "not available: a background scan needs asuka.json's bluetooth.background \"central\"")
                bg -> background.add(s, reply)
                else -> foreground.add(s, reply)
            }
        },
        "stopScan" to { args, reply ->
            val id = args(args).id("id")
            if (!foreground.remove(id)) background.remove(id)
            ok(reply)
        },
        "stopBackgroundScan" to { _, reply ->
            // This run's background scans are the scan now: what a previous run left is gone already.
            if (background.scans.isEmpty()) background.stop()
            ok(reply)
        },
        "choose" to { args, reply -> choose(args(args), reply) },
        "known" to { args, reply ->
            val out = JSONArray()
            val ad = adapter
            if (ad != null) for (id in AsukaBluetoothWire.strings(args(args), "ids")) {
                val addr = id.uppercase()
                if (BluetoothAdapter.checkBluetoothAddress(addr)) out.put(device(ad.getRemoteDevice(addr)))
            }
            answer(reply, out)
        },
        "connected" to { _, reply ->
            if (on(reply)) answer(reply, JSONArray(manager!!.getConnectedDevices(BluetoothProfile.GATT).map { device(it) }))
        },
        "connect" to { args, reply ->
            val a = args(args)
            val id = a.id("conn")
            val addr = a.optString("device").uppercase()
            when {
                !on(reply) -> {}
                !BluetoothAdapter.checkBluetoothAddress(addr) -> fail(reply, "not found")
                conns.containsKey(id) -> fail(reply, "failed: already connecting")
                else -> {
                    val c = Conn(id, adapter!!.getRemoteDevice(addr))
                    conns[id] = c
                    c.connecting = reply
                    val g = c.device.connectGatt(app, a.optBoolean("autoConnect"), c, BluetoothDevice.TRANSPORT_LE, BluetoothDevice.PHY_LE_1M_MASK, main)
                    if (g == null) { conns.remove(id); c.connecting = null; fail(reply, "failed to connect") }
                    else c.gatt = g
                }
            }
        },
        "disconnect" to { args, reply ->
            val c = conns[args(args).id("conn")]
            when {
                c == null -> ok(reply)
                !c.up -> {
                    // Still connecting: that ends, canceled.
                    conns.remove(c.id)
                    c.connecting?.let { c.connecting = null; fail(it, "canceled") }
                    runCatching { c.gatt?.disconnect() }
                    runCatching { c.gatt?.close() }
                    ok(reply)
                }
                else -> {
                    c.closing = true
                    c.failOps("disconnected")
                    c.disconnecting.add(reply)
                    if (runCatching { c.gatt?.disconnect() }.isFailure) c.ended(0)
                }
            }
        },
        "services" to { args, reply ->
            conn(args(args), reply)?.let { c ->
                val t = c.tree
                if (t != null) answer(reply, t)
                else c.enqueue(Kind.DISCOVER, reply) { op ->
                    // Discovered meanwhile (two asked at once): the same.
                    val t2 = c.tree
                    if (t2 != null) c.done(op, Result.success(t2.toString().toByteArray()))
                    else c.started(op, c.gatt!!.discoverServices(), "discovery")
                }
            }
        },
        "read" to { args, reply ->
            val a = args(args)
            conn(a, reply)?.let { c ->
                c.characteristic(a, reply)?.let { ch ->
                    if (ch.properties and BluetoothGattCharacteristic.PROPERTY_READ == 0) fail(reply, "att 2: read not permitted")
                    else c.enqueue(Kind.READ, reply) { op -> c.started(op, c.gatt!!.readCharacteristic(ch), "read") }
                }
            }
        },
        "write" to { args, reply ->
            val a = args(args)
            conn(a, reply)?.let { c ->
                c.characteristic(a, reply)?.let { ch ->
                    val response = a.optBoolean("response")
                    val want = if (response) BluetoothGattCharacteristic.PROPERTY_WRITE else BluetoothGattCharacteristic.PROPERTY_WRITE_NO_RESPONSE
                    val value = AsukaBluetoothWire.bytes(a, "value")
                    if (ch.properties and want == 0) fail(reply, "att 3: write not permitted")
                    else c.enqueue(Kind.WRITE, reply) { op ->
                        val type = if (response) BluetoothGattCharacteristic.WRITE_TYPE_DEFAULT else BluetoothGattCharacteristic.WRITE_TYPE_NO_RESPONSE
                        c.writeOp(op) { c.gatt!!.writeCharacteristic(ch, value, type) }
                    }
                }
            }
        },
        "readDescriptor" to { args, reply ->
            val a = args(args)
            conn(a, reply)?.let { c ->
                c.descriptor(a, reply)?.let { d -> c.enqueue(Kind.READ_DESC, reply) { op -> c.started(op, c.gatt!!.readDescriptor(d), "read") } }
            }
        },
        "writeDescriptor" to { args, reply ->
            val a = args(args)
            conn(a, reply)?.let { c ->
                c.descriptor(a, reply)?.let { d ->
                    val value = AsukaBluetoothWire.bytes(a, "value")
                    c.enqueue(Kind.WRITE_DESC, reply) { op -> c.writeOp(op) { c.gatt!!.writeDescriptor(d, value) } }
                }
            }
        },
        "subscribe" to { args, reply ->
            val a = args(args)
            conn(a, reply)?.let { c ->
                c.characteristic(a, reply)?.let { ch ->
                    if (ch.properties and (BluetoothGattCharacteristic.PROPERTY_NOTIFY or BluetoothGattCharacteristic.PROPERTY_INDICATE) == 0) {
                        fail(reply, "att 6: the characteristic doesn't notify")
                    } else {
                        val value = AsukaBluetoothWire.cccdOn(ch.properties, a.optBoolean("indicate"))
                        c.enqueue(Kind.WRITE_DESC, { r ->
                            if (r.isFailure) runCatching { c.gatt?.setCharacteristicNotification(ch, false) }
                            reply(r)
                        }) { op ->
                            if (!c.gatt!!.setCharacteristicNotification(ch, true)) return@enqueue c.done(op, Result.failure(Exception("failed: the system refused the subscription")))
                            // Without a CCCD (not as the spec has it), the device sends what it sends.
                            val cccd = ch.getDescriptor(AsukaBluetoothWire.CCCD) ?: return@enqueue c.done(op, Result.success("{}".toByteArray()))
                            c.writeOp(op) { c.gatt!!.writeDescriptor(cccd, value) }
                        }
                    }
                }
            }
        },
        "unsubscribe" to { args, reply ->
            val a = args(args)
            conn(a, reply)?.let { c ->
                c.characteristic(a, reply)?.let { ch ->
                    c.enqueue(Kind.WRITE_DESC, reply) { op ->
                        c.gatt!!.setCharacteristicNotification(ch, false)
                        val cccd = ch.getDescriptor(AsukaBluetoothWire.CCCD) ?: return@enqueue c.done(op, Result.success("{}".toByteArray()))
                        c.writeOp(op) { c.gatt!!.writeDescriptor(cccd, BluetoothGattDescriptor.DISABLE_NOTIFICATION_VALUE) }
                    }
                }
            }
        },
        "mtu" to { args, reply -> conn(args(args), reply)?.let { answer(reply, it.mtuJson()) } },
        "requestMtu" to { args, reply ->
            val a = args(args)
            conn(a, reply)?.let { c ->
                val n = a.optInt("mtu").coerceIn(23, 517)
                c.enqueue(Kind.MTU, reply) { op -> c.started(op, c.gatt!!.requestMtu(n), "MTU request") }
            }
        },
        "rssi" to { args, reply ->
            conn(args(args), reply)?.let { c -> c.enqueue(Kind.RSSI, reply) { op -> c.started(op, c.gatt!!.readRemoteRssi(), "RSSI read") } }
        },
        "priority" to { args, reply ->
            val a = args(args)
            conn(a, reply)?.let { c ->
                val p = when (a.optString("priority")) {
                    "high" -> BluetoothGatt.CONNECTION_PRIORITY_HIGH
                    "lowPower" -> BluetoothGatt.CONNECTION_PRIORITY_LOW_POWER
                    else -> BluetoothGatt.CONNECTION_PRIORITY_BALANCED
                }
                c.enqueue(Kind.NOW, reply) { op ->
                    if (c.gatt!!.requestConnectionPriority(p)) c.done(op, Result.success("{}".toByteArray()))
                    else c.done(op, Result.failure(Exception("failed: the system refused the priority")))
                }
            }
        },
        "advertise" to { args, reply ->
            val a = args(args)
            val id = a.id("id")
            if (on(reply)) {
                val adv = adapter?.bluetoothLeAdvertiser
                if (adv == null) fail(reply, "not available: this device can't advertise")
                else {
                    val settings = AdvertiseSettings.Builder().setConnectable(a.optBoolean("connectable")).setTimeout(0)
                        .setAdvertiseMode(when (a.optString("mode")) {
                            "lowPower" -> AdvertiseSettings.ADVERTISE_MODE_LOW_POWER
                            "lowLatency" -> AdvertiseSettings.ADVERTISE_MODE_LOW_LATENCY
                            else -> AdvertiseSettings.ADVERTISE_MODE_BALANCED
                        }).build()
                    val data = AdvertiseData.Builder().setIncludeDeviceName(false).setIncludeTxPowerLevel(a.optBoolean("includeTxPower"))
                    for (u in AsukaBluetoothWire.uuids(a, "services")) data.addServiceUuid(ParcelUuid(u))
                    a.optJSONObject("serviceData")?.let { sd ->
                        for (k in sd.keys()) data.addServiceData(ParcelUuid(AsukaBluetoothWire.uuid(k) ?: throw IllegalArgumentException("bad args: not a UUID: $k")), AsukaBluetoothWire.bytes(sd, k))
                    }
                    a.optJSONObject("manufacturerData")?.let { md -> for (k in md.keys()) data.addManufacturerData(k.toInt(), AsukaBluetoothWire.bytes(md, k)) }
                    // Android advertises the adapter's name, not one of the app's: in the scan response, where there's room.
                    val response = if (a.optString("localName").isNotEmpty()) AdvertiseData.Builder().setIncludeDeviceName(true).build() else null
                    adverts.remove(id)?.let { old -> runCatching { adv.stopAdvertising(old) } }
                    val cb = Advert(id, reply)
                    adverts[id] = cb
                    adv.startAdvertising(settings, data.build(), response, cb)
                }
            }
        },
        "stopAdvertise" to { args, reply ->
            adverts.remove(args(args).id("id"))?.let { cb ->
                runCatching { adapter?.bluetoothLeAdvertiser?.stopAdvertising(cb) }
                cb.reply?.let { cb.reply = null; fail(it, "canceled") }
            }
            ok(reply)
        },
        "serve" to { args, reply ->
            val a = args(args)
            val id = a.id("id")
            unserve(id)
            if (on(reply)) {
                val s = gattServer ?: manager?.openGattServer(app, serverCallback)?.also { gattServer = it }
                if (s == null) fail(reply, "not available: this device can't serve")
                else {
                    val svcs = ArrayList<BluetoothGattService>()
                    val ws = a.optJSONArray("services") ?: JSONArray()
                    for (i in 0 until ws.length()) {
                        val w = ws.getJSONObject(i)
                        val svc = BluetoothGattService(AsukaBluetoothWire.uuid(w.getString("uuid")) ?: throw IllegalArgumentException("bad args: not a UUID"),
                            if (w.optBoolean("primary")) BluetoothGattService.SERVICE_TYPE_PRIMARY else BluetoothGattService.SERVICE_TYPE_SECONDARY)
                        val cs = w.optJSONArray("characteristics") ?: JSONArray()
                        for (j in 0 until cs.length()) {
                            val wc = cs.getJSONObject(j)
                            val props = wc.optInt("properties") and 0xff
                            val p = wc.optInt("permissions")
                            var perms = 0
                            if (p and 0x01 != 0) perms = perms or BluetoothGattCharacteristic.PERMISSION_READ
                            if (p and 0x02 != 0) perms = perms or BluetoothGattCharacteristic.PERMISSION_WRITE
                            if (p and 0x04 != 0) perms = perms or BluetoothGattCharacteristic.PERMISSION_READ_ENCRYPTED
                            if (p and 0x08 != 0) perms = perms or BluetoothGattCharacteristic.PERMISSION_WRITE_ENCRYPTED
                            val ch = BluetoothGattCharacteristic(AsukaBluetoothWire.uuid(wc.getString("uuid")) ?: throw IllegalArgumentException("bad args: not a UUID"), props, perms)
                            if (props and (BluetoothGattCharacteristic.PROPERTY_NOTIFY or BluetoothGattCharacteristic.PROPERTY_INDICATE) != 0) {
                                ch.addDescriptor(BluetoothGattDescriptor(AsukaBluetoothWire.CCCD,
                                    BluetoothGattDescriptor.PERMISSION_READ or BluetoothGattDescriptor.PERMISSION_WRITE))
                            }
                            val ds = wc.optJSONArray("descriptors") ?: JSONArray()
                            for (k in 0 until ds.length()) {
                                val wd = ds.getJSONObject(k)
                                val d = BluetoothGattDescriptor(AsukaBluetoothWire.uuid(wd.getString("uuid")) ?: throw IllegalArgumentException("bad args: not a UUID"),
                                    BluetoothGattDescriptor.PERMISSION_READ)
                                descValues[d] = AsukaBluetoothWire.bytes(wd, "value")
                                ch.addDescriptor(d)
                            }
                            svc.addCharacteristic(ch)
                            val lc = LocalChar(id, wc.optInt("handle"), ch, if (wc.isNull("value")) null else AsukaBluetoothWire.bytes(wc, "value"))
                            localChars[ch] = lc
                            localByHandle[id to lc.handle] = lc
                        }
                        svcs.add(svc)
                    }
                    servers[id] = svcs
                    serving[id] = reply
                    var left = svcs.size
                    if (left == 0) { serving.remove(id); ok(reply) }
                    for (svc in svcs) adding.add((id to svc) to { added ->
                        if (serving.containsKey(id)) {
                            if (!added) {
                                val r = serving.remove(id)!!
                                unserve(id)
                                fail(r, "failed: the system couldn't add a service")
                            } else if (--left == 0) ok(serving.remove(id)!!)
                        }
                    })
                    addNext()
                }
            }
        },
        "unserve" to { args, reply ->
            val id = args(args).id("id")
            serving[id]?.let { fail(it, "canceled") }
            unserve(id)
            ok(reply)
        },
        "respond" to { args, reply ->
            val a = args(args)
            val r = requests.remove(a.optInt("request"))
            if (r != null) {
                val err = a.optInt("error")
                if (r.execute != null) answerExecute(r.execute, err)
                else respond(r.device, r.request, if (err == 0) BluetoothGatt.GATT_SUCCESS else err, r.offset, if (err == 0) AsukaBluetoothWire.bytes(a, "value") else null)
            }
            ok(reply)
        },
        "notify" to { args, reply ->
            val a = args(args)
            val lc = localByHandle[a.id("server") to a.optInt("handle")]
            if (lc == null) fail(reply, "not found")
            else {
                val value = AsukaBluetoothWire.bytes(a, "value")
                val only = AsukaBluetoothWire.strings(a, "centrals").map { it.uppercase() }.toSet()
                val to = subs[lc]?.values?.filter { only.isEmpty() || it.device.address in only } ?: emptyList()
                var left = to.size
                if (left == 0) ok(reply)
                for (s in to) {
                    notifying.getOrPut(s.device.address) { ArrayDeque() }.add(Notification(lc, value, s.indicate) { if (--left == 0) ok(reply) })
                    sendNext(s.device.address)
                }
            }
        },
        "restore" to { _, reply ->
            val f = restoredFile(app)
            val ads = if (f.exists()) runCatching { JSONArray(f.readText()) }.getOrNull() else null
            f.delete()
            if (ads == null || ads.length() == 0) ok(reply)
            else answer(reply, JSONObject().put("advertisements", ads))
        },
    )
}

/**
 * The background scan's results (a PendingIntent scan the system runs,
 * which outlives the process): to Go's background scans when the process
 * has some, else stored for "restore". It doesn't start the Go runtime:
 * the system may start the process only for this.
 */
class AsukaBluetoothScanReceiver : BroadcastReceiver() {
    override fun onReceive(c: Context, i: Intent) {
        val live = AsukaBluetooth.live
        if (live != null && live.backgroundResults(i)) return
        if (i.getIntExtra(BluetoothLeScanner.EXTRA_ERROR_CODE, 0) != 0) return
        val rs = AsukaBluetooth.results(i)
        if (rs.isEmpty()) return
        AsukaBluetooth.store(c, rs)
        live?.restoredStored()
    }
}
