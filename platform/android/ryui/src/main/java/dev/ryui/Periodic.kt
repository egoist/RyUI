package dev.ryui

import android.os.Handler
import android.os.Looper
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * ryui/periodic's runs: the ryui-periodic module's worker hands a task
 * over (run, from its thread) and waits for the Go code to report it
 * done; "next" gives the Go code each one (string name, u32 seconds),
 * "done" (string name, u8 ok) reports it. Scheduling is the module's
 * (WorkManager), set as it loads.
 */
object RyUIPeriodicHub {
    private val main = Handler(Looper.getMainLooper())
    private val due = ArrayDeque<Pair<String, Int>>()
    private val waiting = ArrayDeque<(Result<ByteArray>) -> Unit>()
    private val running = HashMap<String, (Boolean) -> Unit>()

    /** The module's: schedule (name, seconds, network, charging) and cancel (name). */
    var schedule: ((RyUIRuntime, String, Long, Boolean, Boolean) -> Unit)? = null
    var cancel: ((RyUIRuntime, String) -> Unit)? = null

    private fun encode(name: String, seconds: Int): ByteArray {
        val n = name.toByteArray()
        return ByteBuffer.allocate(8 + n.size).order(ByteOrder.LITTLE_ENDIAN).putInt(n.size).put(n).putInt(seconds).array()
    }

    /** Runs the task name with seconds to do it in, starting the core if it hasn't; done gets whether it went well. Any thread. */
    fun run(rt: RyUIRuntime, name: String, seconds: Int, done: (Boolean) -> Unit) = main.post {
        running[name]?.invoke(false) // a run still going: the new one replaces it
        running[name] = done
        rt.startHeadless()
        val w = waiting.removeFirstOrNull()
        if (w != null) w(Result.success(encode(name, seconds))) else due.addLast(name to seconds)
    }

    /** The worker gave up (stopped by the system): forget the run. Any thread. */
    fun stopped(name: String) = main.post {
        running.remove(name)
        due.removeAll { it.first == name }
    }

    fun methods(rt: RyUIRuntime): Map<String, RyUIModuleMethod> = mapOf(
        "schedule" to { args, reply ->
            val b = ByteBuffer.wrap(args).order(ByteOrder.LITTLE_ENDIAN)
            val name = ByteArray(b.int).also { b.get(it) }.toString(Charsets.UTF_8)
            val seconds = b.int.toLong() and 0xffffffffL
            val flags = b.get().toInt()
            val s = schedule
            if (s == null) reply(Result.failure(Exception("ryui/periodic isn't in this build")))
            else { s(rt, name, seconds, flags and 1 != 0, flags and 2 != 0); reply(Result.success(ByteArray(0))) }
        },
        "cancel" to { args, reply ->
            cancel?.invoke(rt, String(args, Charsets.UTF_8))
            reply(Result.success(ByteArray(0)))
        },
        "next" to { _, reply ->
            val d = due.removeFirstOrNull()
            if (d != null) reply(Result.success(encode(d.first, d.second))) else waiting.addLast(reply)
        },
        "done" to { args, reply ->
            val b = ByteBuffer.wrap(args).order(ByteOrder.LITTLE_ENDIAN)
            val name = ByteArray(b.int).also { b.get(it) }.toString(Charsets.UTF_8)
            val ok = b.hasRemaining() && b.get().toInt() == 1
            running.remove(name)?.invoke(ok)
            reply(Result.success(ByteArray(0)))
        },
    )
}
