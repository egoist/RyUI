// Typed native modules (//asuka:native) and views (//asuka:view): what
// the Kotlin that asuka native generates for a Go package calls, and what
// Go's calls reach through JNI (mobile/jni_android.c), from any thread.
//
// The wire format is package native's: values in declaration order,
// little-endian.
package dev.asuka

import android.os.Handler
import android.os.Looper
import java.time.Instant
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.CountDownLatch
import java.util.concurrent.atomic.AtomicLong
import kotlin.time.Duration
import kotlin.time.Duration.Companion.nanoseconds
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob

/** Encodes values (package native's Writer). */
class AsukaNativeWriter {
    private var b = ByteArray(64)
    private var n = 0

    private fun room(k: Int) {
        if (n + k > b.size) b = b.copyOf(maxOf(b.size * 2, n + k))
    }

    private fun put64(v: Long) {
        room(8)
        for (i in 0 until 8) b[n + i] = (v ushr (8 * i)).toByte()
        n += 8
    }

    private fun put32(v: Int) {
        room(4)
        for (i in 0 until 4) b[n + i] = (v ushr (8 * i)).toByte()
        n += 4
    }

    fun bool(v: Boolean) { room(1); b[n++] = if (v) 1 else 0 }
    fun int8(v: Byte) { room(1); b[n++] = v }
    fun int16(v: Short) { room(2); b[n++] = v.toByte(); b[n++] = (v.toInt() ushr 8).toByte() }
    fun int32(v: Int) = put32(v)
    fun int64(v: Long) = put64(v)
    fun int(v: Long) = put64(v)
    fun uint8(v: UByte) = int8(v.toByte())
    fun uint16(v: UShort) = int16(v.toShort())
    fun uint32(v: UInt) = put32(v.toInt())
    fun uint64(v: ULong) = put64(v.toLong())
    fun uint(v: ULong) = put64(v.toLong())
    fun float32(v: Float) = put32(java.lang.Float.floatToRawIntBits(v))
    fun float64(v: Double) = put64(java.lang.Double.doubleToRawLongBits(v))
    fun string(v: String) = data(v.toByteArray(Charsets.UTF_8))

    fun data(v: ByteArray) {
        put32(v.size)
        room(v.size)
        System.arraycopy(v, 0, b, n, v.size)
        n += v.size
    }

    /** A time: seconds and nanoseconds since 1970. */
    fun time(v: Instant) { put64(v.epochSecond); put32(v.nano) }
    fun duration(v: Duration) = put64(v.inWholeNanoseconds)
    fun len(k: Int) = put32(k)
    fun present(v: Boolean) = bool(v)

    fun toByteArray(): ByteArray = b.copyOf(n)

    companion object {
        /** An answer's writer: its status, ok, first. */
        fun ok() = AsukaNativeWriter().apply { int8(0) }
    }
}

/** Decodes values (package native's Reader); past what's there, every read is its zero value and failed is true. */
class AsukaNativeReader(private val b: ByteArray) {
    private var p = 0
    var failed = false
        private set

    private fun take(k: Int): Int {
        if (failed || k < 0 || b.size - p < k) {
            failed = true
            return -1
        }
        val at = p
        p += k
        return at
    }

    private fun get64(): Long {
        val at = take(8)
        if (at < 0) return 0
        var v = 0L
        for (i in 7 downTo 0) v = (v shl 8) or (b[at + i].toLong() and 0xff)
        return v
    }

    private fun get32(): Int {
        val at = take(4)
        if (at < 0) return 0
        var v = 0
        for (i in 3 downTo 0) v = (v shl 8) or (b[at + i].toInt() and 0xff)
        return v
    }

    fun bool(): Boolean = int8().toInt() != 0
    fun int8(): Byte { val at = take(1); return if (at < 0) 0 else b[at] }
    fun int16(): Short {
        val at = take(2)
        return if (at < 0) 0 else ((b[at].toInt() and 0xff) or (b[at + 1].toInt() shl 8)).toShort()
    }
    fun int32(): Int = get32()
    fun int64(): Long = get64()
    fun int(): Long = get64()
    fun uint8(): UByte = int8().toUByte()
    fun uint16(): UShort = int16().toUShort()
    fun uint32(): UInt = get32().toUInt()
    fun uint64(): ULong = get64().toULong()
    fun uint(): ULong = get64().toULong()
    fun float32(): Float = java.lang.Float.intBitsToFloat(get32())
    fun float64(): Double = java.lang.Double.longBitsToDouble(get64())
    fun string(): String = String(data(), Charsets.UTF_8)

    fun data(): ByteArray {
        val k = get32()
        val at = take(k)
        return if (at < 0) ByteArray(0) else b.copyOfRange(at, at + k)
    }

    fun time(): Instant {
        val s = get64()
        val ns = get32()
        return Instant.ofEpochSecond(s, ns.toLong())
    }

    fun duration(): Duration = get64().nanoseconds

    /** A slice's or a map's length, bounded by what's left. */
    fun len(): Int {
        val k = get32()
        if (k < 0 || k > b.size - p) {
            failed = true
            return 0
        }
        return k
    }

    fun present(): Boolean = bool()
    /** A callback's id (0: none). */
    fun callback(): Long = get64()

    /** Throws when a read went past what's there. */
    fun check() {
        if (failed) throw AsukaMalformedException()
    }
}

/** An error a package declares (native.NewError): its Go gets that value. */
interface AsukaDeclaredError {
    val asukaIndex: Int
}

/** What the platform doesn't have: Go gets errors.ErrUnsupported. */
class AsukaUnsupportedException(message: String) : Exception(message)

class AsukaMalformedException : Exception("asuka: a malformed call")

class AsukaClosedException : Exception("asuka: the object is closed")

/** A Go package's native code, as asuka native generates it: the host finds it by its class's name (the module's Kotlin, in Go). */
interface AsukaModuleProvider {
    fun make(iface: Int): Any
    fun call(obj: Any, iface: Int, method: Int, r: AsukaNativeReader, w: AsukaNativeWriter)
    fun start(obj: Any, iface: Int, method: Int, r: AsukaNativeReader, done: (Result<AsukaNativeWriter>) -> Unit): Job?
    fun makeView(name: String, ctx: AsukaNativeContext): AsukaNativeView?
}

/**
 * The typed modules' runtime: the objects Go holds, by handle; the calls
 * it started; and the way back to Go (JNI).
 */
object AsukaModules {
    private class Entry(val obj: Any, val provider: AsukaModuleProvider, val iface: Int)

    private val objects = ConcurrentHashMap<Long, Entry>()
    private val nextHandle = AtomicLong()
    private val jobs = ConcurrentHashMap<Long, Job>()
    private val finished = ConcurrentHashMap.newKeySet<Long>()
    private val providers = ConcurrentHashMap<String, AsukaModuleProvider>()
    private val main = Handler(Looper.getMainLooper())

    /**
     * The application's context, for a module's Android APIs (any thread).
     * Throws before the host started, when no module can be called.
     */
    val context: android.content.Context
        get() = AsukaRuntime.current?.app ?: throw IllegalStateException("asuka: the host hasn't started")

    /**
     * The activity showing the app, if one is: for what needs one (a
     * dialog, a permission request, a result). Read it on the main thread.
     */
    val activity: android.app.Activity? get() = AsukaRuntime.current?.activity

    /** Where the generated Kotlin's suspend funs run. */
    @JvmField val scope = CoroutineScope(SupervisorJob() + Dispatchers.Default)

    @JvmStatic external fun done(call: Long, answer: ByteArray)
    @JvmStatic external fun invoke(id: Long, args: ByteArray): ByteArray?

    /** The provider of a class's name, looked up once. */
    fun provider(name: String): AsukaModuleProvider? = providers[name] ?: try {
        (Class.forName(name).getField("INSTANCE").get(null) as AsukaModuleProvider).also { providers[name] = it }
    } catch (_: ReflectiveOperationException) {
        null
    }

    /** Keeps an object for Go: its handle. */
    fun retain(obj: Any, provider: AsukaModuleProvider, iface: Int): Long {
        val h = nextHandle.incrementAndGet()
        objects[h] = Entry(obj, provider, iface)
        return h
    }

    /** The object of a handle Go passed (null for 0, or one let go). */
    fun obj(h: Long): Any? = objects[h]?.obj

    // What Go calls, through JNI.

    @JvmStatic fun new(cls: ByteArray, iface: Int): ByteArray {
        val name = String(cls, Charsets.UTF_8)
        val p = provider(name) ?: return answer(AsukaUnsupportedException("no Kotlin $name: build the app with its packages' native code"))
        return try {
            val o = p.make(iface)
            AsukaNativeWriter.ok().apply { int64(retain(o, p, iface)) }.toByteArray()
        } catch (e: Throwable) {
            answer(e)
        }
    }

    @JvmStatic fun call(h: Long, method: Int, input: ByteArray): ByteArray {
        val e = objects[h] ?: return answer(AsukaClosedException())
        val w = AsukaNativeWriter.ok()
        return try {
            e.provider.call(e.obj, e.iface, method, AsukaNativeReader(input), w)
            w.toByteArray()
        } catch (t: Throwable) {
            answer(t)
        }
    }

    @JvmStatic fun start(h: Long, method: Int, input: ByteArray, call: Long) {
        val e = objects[h]
        if (e == null) {
            done(call, answer(AsukaClosedException()))
            return
        }
        val job = try {
            e.provider.start(e.obj, e.iface, method, AsukaNativeReader(input)) { finish(call, it) }
        } catch (t: Throwable) {
            finish(call, Result.failure(t))
            null
        }
        if (!finished.remove(call) && job != null) jobs[call] = job
    }

    private fun finish(call: Long, result: Result<AsukaNativeWriter>) {
        if (jobs.remove(call) == null) finished.add(call)
        done(call, result.fold({ it.toByteArray() }, { answer(it) }))
    }

    @JvmStatic fun cancel(call: Long) {
        jobs[call]?.cancel()
    }

    @JvmStatic fun release(h: Long) {
        objects.remove(h)
    }

    // What the generated Kotlin calls.

    /** Calls a Go callback that answers nothing (it runs on Go's main thread). */
    fun notify(id: Long, w: AsukaNativeWriter) {
        if (id != 0L) invoke(id, w.toByteArray())
    }

    /** Calls a Go callback and reads its answer. */
    fun ask(id: Long, w: AsukaNativeWriter): AsukaNativeReader =
        AsukaNativeReader(if (id == 0L) ByteArray(0) else invoke(id, w.toByteArray()) ?: ByteArray(0))

    /** Runs body on the main thread: now if this is it, else waiting for it. */
    fun <T> onMain(body: () -> T): T {
        if (Looper.myLooper() == Looper.getMainLooper()) return body()
        var result: Result<T>? = null
        val latch = CountDownLatch(1)
        main.post {
            result = runCatching(body)
            latch.countDown()
        }
        latch.await()
        return result!!.getOrThrow()
    }

    /** An error's answer: declared (its index), unsupported, canceled, or its class, 0 and message. */
    fun answer(e: Throwable): ByteArray {
        val w = AsukaNativeWriter()
        when {
            e is AsukaDeclaredError -> { w.int8(1); w.int32(e.asukaIndex) }
            e is AsukaUnsupportedException -> { w.int8(3); w.string(e.message ?: "") }
            e is kotlinx.coroutines.CancellationException -> w.int8(4)
            else -> { w.int8(2); w.string(e.javaClass.name); w.int64(0); w.string(e.message ?: e.toString()) }
        }
        return w.toByteArray()
    }

    /** A typed view of a kind (its module's class # its name). */
    fun makeView(kind: String, ctx: AsukaNativeContext): AsukaNativeView? {
        val i = kind.indexOf('#')
        if (i < 0) return null
        return provider(kind.substring(0, i))?.makeView(kind.substring(i + 1), ctx)
    }
}
