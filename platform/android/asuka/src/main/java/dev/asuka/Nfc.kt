package dev.asuka

import android.app.Activity
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.nfc.FormatException
import android.nfc.NdefMessage
import android.nfc.NfcAdapter
import android.nfc.Tag
import android.nfc.TagLostException
import android.nfc.tech.IsoDep
import android.nfc.tech.MifareClassic
import android.nfc.tech.MifareUltralight
import android.nfc.tech.Ndef
import android.nfc.tech.NdefFormatable
import android.nfc.tech.NfcA
import android.nfc.tech.NfcB
import android.nfc.tech.NfcBarcode
import android.nfc.tech.NfcF
import android.nfc.tech.NfcV
import android.nfc.tech.TagTechnology
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.Executors

/**
 * What asuka/sdk/nfc's wire needs that's plain JVM code: the technologies'
 * names, reader mode's flags, FeliCa's length byte, base64 (unit tests).
 */
object AsukaNfcWire {
    /** android.nfc.tech's classes, by the package's names for them. */
    val techNames = linkedMapOf(
        "android.nfc.tech.Ndef" to "ndef",
        "android.nfc.tech.NdefFormatable" to "ndefFormatable",
        "android.nfc.tech.NfcA" to "nfcA",
        "android.nfc.tech.NfcB" to "nfcB",
        "android.nfc.tech.NfcF" to "nfcF",
        "android.nfc.tech.NfcV" to "nfcV",
        "android.nfc.tech.IsoDep" to "isoDep",
        "android.nfc.tech.MifareClassic" to "mifareClassic",
        "android.nfc.tech.MifareUltralight" to "mifareUltralight",
        "android.nfc.tech.NfcBarcode" to "nfcBarcode",
    )

    /**
     * A tag's technologies: those both platforms name (ndef, iso7816,
     * mifare, felica, iso15693), then Android's own.
     */
    fun techs(classes: Array<String>, ndef: Boolean): List<String> {
        val own = classes.mapNotNull { techNames[it] }.filter { it != "ndef" } // ndef: as read (SkipNDEF reads none)
        val out = ArrayList<String>()
        if (ndef) out.add("ndef")
        if ("isoDep" in own) out.add("iso7816")
        if ("nfcA" in own) out.add("mifare")
        if ("nfcF" in own) out.add("felica")
        if ("nfcV" in own) out.add("iso15693")
        out.addAll(own)
        return out
    }

    /** Reader mode's flags for nfcspec's Poll bits (none: every kind). */
    fun readerFlags(poll: Int, skipNDEF: Boolean, noSound: Boolean): Int {
        var f = 0
        val p = if (poll == 0) 0b1111 else poll
        if (p and 0b1 != 0) f = f or NfcAdapter.FLAG_READER_NFC_A
        if (p and 0b10 != 0) f = f or NfcAdapter.FLAG_READER_NFC_B
        if (p and 0b100 != 0) f = f or NfcAdapter.FLAG_READER_NFC_F
        if (p and 0b1000 != 0) f = f or NfcAdapter.FLAG_READER_NFC_V
        if (p and 0b10000 != 0) f = f or NfcAdapter.FLAG_READER_NFC_BARCODE
        if (p and 0b100000 != 0) f = f or NfcAdapter.FLAG_READER_NFC_A or NfcAdapter.FLAG_READER_NFC_B // PACE: an ISO 14443 card's
        if (skipNDEF) f = f or NfcAdapter.FLAG_READER_SKIP_NDEF_CHECK
        if (noSound) f = f or NfcAdapter.FLAG_READER_NO_PLATFORM_SOUNDS
        return f
    }

    /** A FeliCa command as NfcF takes it: its length byte first. */
    fun feliCaFrame(command: ByteArray): ByteArray = byteArrayOf((command.size + 1).toByte()) + command

    /** NfcF's answer without its length byte. */
    fun feliCaAnswer(frame: ByteArray): ByteArray = if (frame.isEmpty()) frame else frame.copyOfRange(1, frame.size)

    /** An ISO 15693 UID (NfcV's: least significant byte first) as its manufacturer and serial, most significant first. */
    fun iso15693(id: ByteArray): Pair<Int, ByteArray>? =
        if (id.size == 8) (id[6].toInt() and 0xff) to id.copyOfRange(0, 6).reversedArray() else null

    val emptyMessage = byteArrayOf(0xd0.toByte(), 0, 0)

    fun b64(b: ByteArray?): String = java.util.Base64.getEncoder().encodeToString(b ?: ByteArray(0))
    fun unb64(s: String?): ByteArray = if (s.isNullOrEmpty()) ByteArray(0) else java.util.Base64.getDecoder().decode(s)
}

/**
 * asuka/sdk/nfc's module (internal/nfcspec has the wire): reader mode
 * (enableReaderMode) for the Activity in front, enabled again for one
 * that takes its place; one tag at a time, held until "restart" or the
 * session's end. Tags' I/O runs on a thread of its own. The tags the
 * system opens the app with (its NDEF_DISCOVERED and TECH_DISCOVERED
 * filters, AsukaActivity's incoming) wait here for "nextOpened", their
 * URIs going to ui.OnOpenURL. State on the main thread.
 */
object AsukaNfcHub {
    private val main = Handler(Looper.getMainLooper())
    private val io = Executors.newSingleThreadExecutor { r -> Thread(r, "asuka-nfc").apply { isDaemon = true } }
    private var rt: AsukaRuntime? = null
    private val adapter: NfcAdapter? get() = rt?.let { NfcAdapter.getDefaultAdapter(it.app) }

    /** A tag the app holds; tech is the technology connected (I/O thread). */
    private class Held(val tag: Tag) {
        var tech: TagTechnology? = null
        var locked = false
        var formatted = false
        fun close() = io.execute { try { tech?.close() } catch (_: Exception) {}; tech = null }
    }

    private class Session(val id: Int, val flags: Int, val extras: Bundle, val skipNDEF: Boolean) {
        var waiting: ((Result<ByteArray>) -> Unit)? = null
        val queue = ArrayDeque<ByteArray>()
        var looking = true
        var pending: Tag? = null
        var on: Activity? = null
        var handle = 0
        var held: Held? = null
    }

    private var session: Session? = null
    private var lastEnded: Pair<Int, String>? = null
    private var handles = 0
    private var openedHandle = 0
    private var openedHeld: Held? = null
    private val opened = AsukaWaitQueue()

    private fun fail(word: String) = Result.failure<ByteArray>(Exception(word))
    private val ok = Result.success(ByteArray(0))
    private fun json(args: ByteArray) = try { JSONObject(String(args, Charsets.UTF_8)) } catch (_: Exception) { JSONObject() }

    fun methods(runtime: AsukaRuntime): Map<String, AsukaModuleMethod> {
        rt = runtime
        return mapOf(
            "state" to { _, reply -> reply(Result.success(state())) },
            "nextState" to { args, reply ->
                if (!sameState(args)) reply(Result.success(state())) else { watchState(); stateWaiting.add(args to reply) }
            },
            "stopState" to { _, reply ->
                val ws = stateWaiting.toList()
                stateWaiting.clear()
                for ((_, w) in ws) w(fail("stopped"))
                reply(ok)
            },
            "settings" to { _, reply ->
                val ctx: Context = rt?.activity ?: runtime.app
                val i = Intent(android.provider.Settings.ACTION_NFC_SETTINGS)
                if (ctx !is Activity) i.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
                try { ctx.startActivity(i); reply(ok) } catch (e: Exception) { reply(fail("not available: ${e.message}")) }
            },
            "start" to { args, reply -> start(json(args), reply) },
            "next" to { args, reply ->
                val id = json(args).optInt("id")
                val s = session
                if (s == null || s.id != id) {
                    val l = lastEnded
                    reply(fail(if (l != null && l.first == id) l.second else "ended"))
                } else {
                    val q = s.queue.removeFirstOrNull()
                    if (q != null) reply(Result.success(q)) else s.waiting = reply
                }
            },
            "restart" to { args, reply ->
                mine(args)?.let { s ->
                    s.held?.close()
                    s.held = null
                    s.handle = 0
                    s.looking = true
                    s.pending?.let { s.pending = null; discovered(s, it) }
                }
                reply(ok)
            },
            "alert" to { _, reply -> reply(ok) }, // no sheet: the app shows its own
            "stop" to { args, reply ->
                mine(args)?.let { end(it, "ended") }
                reply(ok)
            },
            "read" to { args, reply -> op(args, reply) { h, _ -> read(h) } },
            "write" to { args, reply -> op(args, reply) { h, o -> write(h, AsukaNfcWire.unb64(o.optString("message"))) } },
            "lock" to { args, reply -> op(args, reply) { h, _ -> lock(h) } },
            "status" to { args, reply -> op(args, reply) { h, _ -> status(h).toString().toByteArray() } },
            "transceive" to { args, reply -> op(args, reply) { h, o -> transceive(h, o.optString("tech"), AsukaNfcWire.unb64(o.optString("data"))) } },
            "classic" to { args, reply -> op(args, reply) { h, o -> classic(h, o) } },
            "nextOpened" to { _, reply -> opened.next(reply) },
            "stopOpened" to { _, reply -> opened.stop(); reply(ok) },
        )
    }

    private fun state(): ByteArray {
        val a = adapter
        return JSONObject().put("supported", a != null).put("enabled", a?.isEnabled == true).toString().toByteArray()
    }

    /** Whether last (a state's JSON) is the state now. */
    private fun sameState(last: ByteArray): Boolean {
        val o = json(last)
        val a = adapter
        return o.optBoolean("supported") == (a != null) && o.optBoolean("enabled") == (a?.isEnabled == true)
    }

    private val stateWaiting = ArrayList<Pair<ByteArray, (Result<ByteArray>) -> Unit>>()
    private var stateReceiver: BroadcastReceiver? = null

    /** Hears NFC turned on and off, once asked. */
    private fun watchState() {
        val app = rt?.app ?: return
        if (stateReceiver != null) return
        val r = object : BroadcastReceiver() {
            override fun onReceive(c: Context, i: Intent) {
                val s = state()
                val changed = stateWaiting.filter { !sameState(it.first) }
                stateWaiting.removeAll(changed)
                for ((_, w) in changed) w(Result.success(s))
            }
        }
        stateReceiver = r
        app.registerReceiver(r, IntentFilter(NfcAdapter.ACTION_ADAPTER_STATE_CHANGED), Context.RECEIVER_NOT_EXPORTED)
    }

    private fun mine(args: ByteArray): Session? {
        val id = json(args).optInt("id")
        return session?.takeIf { it.id == id }
    }

    private fun start(o: JSONObject, reply: (Result<ByteArray>) -> Unit) {
        val a = adapter ?: return reply(fail("not available"))
        if (!a.isEnabled) return reply(fail("disabled"))
        session?.let { end(it, "ended") }
        val extras = Bundle()
        o.optInt("presenceDelay").takeIf { it > 0 }?.let { extras.putInt(NfcAdapter.EXTRA_READER_PRESENCE_CHECK_DELAY, it) }
        val skip = o.optBoolean("skipNDEF")
        val s = Session(o.optInt("id"), AsukaNfcWire.readerFlags(o.optInt("poll"), skip, o.optBoolean("noSound")), extras, skip)
        session = s
        rt?.activity?.let { enable(s, it) }
        reply(ok)
    }

    /** Reader mode for act, while s runs (from the Activity's onResume too). */
    private fun enable(s: Session, act: Activity) {
        if (s.on === act) return
        val resumed = (act as? androidx.lifecycle.LifecycleOwner)?.lifecycle?.currentState?.isAtLeast(androidx.lifecycle.Lifecycle.State.RESUMED) == true
        if (!resumed) return
        val a = adapter ?: return
        try {
            a.enableReaderMode(act, { tag -> main.post { discovered(s, tag) } }, s.flags, s.extras)
            s.on = act
        } catch (e: Exception) {
            end(s, "not available: ${e.message}")
        }
    }

    /** The Activity came to the front: reader mode for it, if a session runs (an Activity recreated, another one's). */
    fun resumed(act: Activity) {
        session?.let { enable(it, act) }
    }

    private fun end(s: Session, word: String) {
        if (session !== s) return
        session = null
        lastEnded = s.id to word
        s.on?.let { act -> if (!act.isDestroyed) try { adapter?.disableReaderMode(act) } catch (_: Exception) {} }
        s.on = null
        s.held?.close()
        s.held = null
        s.waiting?.let { s.waiting = null; it(fail(word)) }
    }

    private fun discovered(s: Session, tag: Tag) {
        if (session !== s) return
        if (!s.looking) {
            s.pending = tag // for the next "restart"
            return
        }
        s.looking = false
        s.held?.close()
        val h = Held(tag)
        handles++
        s.held = h
        s.handle = handles
        val d = info(tag, s.skipNDEF, null).put("handle", handles).toString().toByteArray()
        val w = s.waiting
        if (w != null) { s.waiting = null; w(Result.success(d)) } else s.queue.addLast(d)
    }

    /** A tag's identity, NDEF status and message (as read at discovery: no I/O) and its technologies' details. */
    private fun info(tag: Tag, skipNDEF: Boolean, message: ByteArray?): JSONObject {
        val ndef = if (skipNDEF) null else Ndef.get(tag)
        val o = JSONObject().put("id", AsukaNfcWire.b64(tag.id))
            .put("techs", JSONArray(AsukaNfcWire.techs(tag.techList, ndef != null)))
        var max = 0
        when {
            ndef != null -> {
                o.put("ndef", ndefJSON(ndef, false))
                o.put("message", AsukaNfcWire.b64(ndef.cachedNdefMessage?.toByteArray() ?: message ?: AsukaNfcWire.emptyMessage))
            }
            !skipNDEF && NdefFormatable.get(tag) != null -> o.put("ndef", JSONObject().put("format", 1).put("capacity", -1))
            else -> o.put("ndef", JSONObject().put("format", 0).put("capacity", -1))
        }
        NfcA.get(tag)?.let {
            o.put("nfcA", JSONObject().put("atqa", AsukaNfcWire.b64(it.atqa)).put("sak", it.sak.toInt()))
            max = it.maxTransceiveLength
        }
        NfcB.get(tag)?.let {
            o.put("nfcB", JSONObject().put("applicationData", AsukaNfcWire.b64(it.applicationData)).put("protocolInfo", AsukaNfcWire.b64(it.protocolInfo)))
            max = it.maxTransceiveLength
        }
        IsoDep.get(tag)?.let {
            val i = JSONObject()
            it.historicalBytes?.let { b -> i.put("historical", AsukaNfcWire.b64(b)) }
            it.hiLayerResponse?.let { b -> i.put("hiLayer", AsukaNfcWire.b64(b)) }
            o.put("iso7816", i)
            max = it.maxTransceiveLength
        }
        NfcF.get(tag)?.let {
            o.put("felica", JSONObject().put("systemCode", AsukaNfcWire.b64(it.systemCode)).put("idm", AsukaNfcWire.b64(tag.id)).put("pmm", AsukaNfcWire.b64(it.manufacturer)))
            max = it.maxTransceiveLength
        }
        NfcV.get(tag)?.let {
            val v = JSONObject().put("dsfid", it.dsfId.toInt() and 0xff)
            AsukaNfcWire.iso15693(tag.id)?.let { (m, serial) -> v.put("manufacturer", m).put("serial", AsukaNfcWire.b64(serial)) }
            o.put("iso15693", v)
            max = it.maxTransceiveLength
        }
        val classic = MifareClassic.get(tag)
        val ultralight = MifareUltralight.get(tag)
        when {
            classic != null -> o.put("mifare", JSONObject().put("family", "classic")
                .put("type", when (classic.type) { MifareClassic.TYPE_PLUS -> "plus"; MifareClassic.TYPE_PRO -> "pro"; else -> "classic" })
                .put("size", classic.size).put("sectors", classic.sectorCount).put("blocks", classic.blockCount))
            ultralight != null -> o.put("mifare", JSONObject().put("family", "ultralight")
                .put("type", if (ultralight.type == MifareUltralight.TYPE_ULTRALIGHT_C) "ultralightC" else "ultralight"))
            NfcA.get(tag) != null -> o.put("mifare", JSONObject().put("family", "unknown"))
        }
        NfcBarcode.get(tag)?.let { o.put("barcode", AsukaNfcWire.b64(it.barcode)) }
        if (max > 0) o.put("maxTransceive", max)
        return o
    }

    private fun ndefJSON(n: Ndef, locked: Boolean): JSONObject {
        val writable = n.isWritable && !locked
        return JSONObject().put("format", if (writable) 2 else 3).put("capacity", n.maxSize)
            .put("canLock", writable && n.canMakeReadOnly()).put("type", n.type ?: "")
    }

    /**
     * The tag the system opened the app with, if intent is one (an
     * NDEF_DISCOVERED, TECH_DISCOVERED or TAG_DISCOVERED of the app's
     * filters): its URI for ui.OnOpenURL, or "" when it's kept for
     * nfc.OnOpen; null for any other intent. Main thread.
     */
    fun opened(intent: Intent): String? {
        val action = intent.action
        if (action != NfcAdapter.ACTION_NDEF_DISCOVERED && action != NfcAdapter.ACTION_TECH_DISCOVERED && action != NfcAdapter.ACTION_TAG_DISCOVERED) return null
        if (action == NfcAdapter.ACTION_NDEF_DISCOVERED) intent.data?.let { return it.toString() }
        val tag = intent.getParcelableExtra(NfcAdapter.EXTRA_TAG, Tag::class.java)
        val message = intent.getParcelableArrayExtra(NfcAdapter.EXTRA_NDEF_MESSAGES, NdefMessage::class.java)?.firstOrNull()?.toByteArray()
        val o = if (tag != null) info(tag, false, message) else JSONObject().put("techs", JSONArray()).put("ndef", JSONObject().put("format", 0).put("capacity", -1))
        if (tag == null && message != null) o.put("message", AsukaNfcWire.b64(message))
        openedHeld?.close()
        openedHeld = tag?.let { Held(it) }
        handles++
        openedHandle = if (tag != null) handles else 0
        opened.deliver(o.put("handle", openedHandle).toString().toByteArray())
        return ""
    }

    /** op runs f on the tag args names, on the I/O thread. */
    private fun op(args: ByteArray, reply: (Result<ByteArray>) -> Unit, f: (Held, JSONObject) -> ByteArray) {
        val o = json(args)
        val h = o.optInt("handle")
        val s = session
        val held = when {
            h == 0 -> null
            s != null && s.handle == h -> s.held
            h == openedHandle -> openedHeld
            else -> null
        } ?: return reply(fail("tag lost"))
        io.execute {
            val r = try {
                Result.success(f(held, o))
            } catch (e: TagLostException) {
                fail("tag lost")
            } catch (e: SecurityException) {
                fail("tag lost") // "out of date": another tag since
            } catch (e: FormatException) {
                fail("not an NDEF message: ${e.message}")
            } catch (e: Exception) {
                fail(e.message ?: e.javaClass.simpleName)
            }
            reply(r)
        }
    }

    /** h's technology t, connected (closing another one first: one at a time). I/O thread. */
    private fun <T : TagTechnology> connect(h: Held, t: T?, name: String): T {
        if (t == null) throw Exception("not supported: the tag has no $name")
        val cur = h.tech
        if (cur != null && cur.javaClass == t.javaClass && cur.isConnected) {
            @Suppress("UNCHECKED_CAST")
            return cur as T
        }
        try { cur?.close() } catch (_: Exception) {}
        h.tech = null
        t.connect()
        h.tech = t
        return t
    }

    private fun read(h: Held): ByteArray {
        val n = Ndef.get(h.tag)
        if (n == null) {
            throw Exception(if (NdefFormatable.get(h.tag) != null && !h.formatted) "not supported: not formatted for NDEF yet" else "not supported: not an NDEF tag")
        }
        return connect(h, n, "ndef").ndefMessage?.toByteArray() ?: AsukaNfcWire.emptyMessage
    }

    private fun write(h: Held, bytes: ByteArray): ByteArray {
        val m = NdefMessage(bytes)
        val n = Ndef.get(h.tag)
        if (n != null) {
            if (!n.isWritable || h.locked) throw Exception("read only")
            if (bytes.size > n.maxSize) throw Exception("too large: ${bytes.size} bytes, the tag holds ${n.maxSize}")
            connect(h, n, "ndef").writeNdefMessage(m)
            return ByteArray(0)
        }
        val f = NdefFormatable.get(h.tag) ?: throw Exception("not supported: not an NDEF tag")
        connect(h, f, "ndefFormatable").format(m)
        h.formatted = true
        return ByteArray(0)
    }

    private fun lock(h: Held): ByteArray {
        val n = Ndef.get(h.tag) ?: throw Exception("not supported: not an NDEF tag")
        if (!n.canMakeReadOnly()) throw Exception("not supported: the tag can't be made read-only")
        if (!connect(h, n, "ndef").makeReadOnly()) throw Exception("not supported: the tag refused")
        h.locked = true
        return ByteArray(0)
    }

    private fun status(h: Held): JSONObject {
        val n = Ndef.get(h.tag)
        return when {
            n != null -> ndefJSON(n, h.locked)
            h.formatted -> JSONObject().put("format", 2).put("capacity", -1)
            NdefFormatable.get(h.tag) != null -> JSONObject().put("format", 1).put("capacity", -1)
            else -> JSONObject().put("format", 0).put("capacity", -1)
        }
    }

    private fun transceive(h: Held, tech: String, data: ByteArray): ByteArray {
        val t = h.tag
        return when (tech) {
            "iso7816", "isoDep" -> connect(h, IsoDep.get(t), tech).transceive(data)
            "mifare", "nfcA" -> connect(h, NfcA.get(t), tech).transceive(data)
            "mifareUltralight" -> connect(h, MifareUltralight.get(t), tech).transceive(data)
            "mifareClassic" -> connect(h, MifareClassic.get(t), tech).transceive(data)
            "felica" -> AsukaNfcWire.feliCaAnswer(connect(h, NfcF.get(t), tech).transceive(AsukaNfcWire.feliCaFrame(data)))
            "nfcF" -> connect(h, NfcF.get(t), tech).transceive(data)
            "iso15693", "nfcV" -> connect(h, NfcV.get(t), tech).transceive(data)
            "nfcB" -> connect(h, NfcB.get(t), tech).transceive(data)
            else -> throw Exception("not supported: no commands for $tech")
        }
    }

    private fun classic(h: Held, o: JSONObject): ByteArray {
        val c = connect(h, MifareClassic.get(h.tag), "mifareClassic")
        val block = o.optInt("block")
        when (o.optString("op")) {
            "auth" -> {
                val key = AsukaNfcWire.unb64(o.optString("key"))
                val sector = o.optInt("sector")
                val ok = if (o.optBoolean("keyB")) c.authenticateSectorWithKeyB(sector, key) else c.authenticateSectorWithKeyA(sector, key)
                if (!ok) throw Exception("authentication failed")
            }
            "read" -> return c.readBlock(block)
            "write" -> c.writeBlock(block, AsukaNfcWire.unb64(o.optString("data")))
            "increment" -> c.increment(block, o.optInt("value"))
            "decrement" -> c.decrement(block, o.optInt("value"))
            "transfer" -> c.transfer(block)
            "restore" -> c.restore(block)
            else -> throw Exception("not supported: ${o.optString("op")}")
        }
        return ByteArray(0)
    }
}
