package dev.ryui

import android.Manifest
import android.content.Context
import android.hardware.Sensor
import android.hardware.SensorEvent
import android.hardware.SensorEventListener
import android.hardware.SensorManager
import android.os.Build
import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer
import java.nio.ByteOrder
import kotlin.math.sqrt

/**
 * ryui/sensors' module, with SensorManager: "available" (u8 sensor),
 * "start" (u8 sensor, u32 µs), "stop" (u8 sensor), "stopAll", "next"
 * (the samples since the last, once there are some: u8 sensor, f64
 * seconds since boot, u8 count, count f64s), "steps" (Android keeps no
 * history: "not available"). Accelerations in g; device motion is
 * gravity, linear acceleration, rotation rate and the rotation vector's
 * quaternion (x, y, z, w), from which the Go side computes the attitude.
 */
class RyUISensors(private val rt: RyUIRuntime) {
    private val sm get() = rt.app.getSystemService(Context.SENSOR_SERVICE) as SensorManager
    private val listeners = HashMap<Int, List<SensorEventListener>>()
    private var buffer = ByteArrayOutputStream()
    private var count = 0
    private val waiting = ArrayList<(Result<ByteArray>) -> Unit>()

    companion object {
        // As sensors.Sensor.
        const val ACCELEROMETER = 0; const val GYROSCOPE = 1; const val MAGNETOMETER = 2; const val MOTION = 3
        const val BAROMETER = 4; const val LIGHT = 5; const val STEPS = 6
        const val G = SensorManager.GRAVITY_EARTH.toDouble()
    }

    /** The platform sensors each kind reads. */
    private fun types(kind: Int): List<Int> = when (kind) {
        ACCELEROMETER -> listOf(Sensor.TYPE_ACCELEROMETER)
        GYROSCOPE -> listOf(Sensor.TYPE_GYROSCOPE)
        MAGNETOMETER -> listOf(Sensor.TYPE_MAGNETIC_FIELD)
        MOTION -> listOf(Sensor.TYPE_ROTATION_VECTOR, Sensor.TYPE_GRAVITY, Sensor.TYPE_LINEAR_ACCELERATION, Sensor.TYPE_GYROSCOPE)
        BAROMETER -> listOf(Sensor.TYPE_PRESSURE)
        LIGHT -> listOf(Sensor.TYPE_LIGHT)
        STEPS -> listOf(Sensor.TYPE_STEP_COUNTER)
        else -> emptyList()
    }

    private fun available(kind: Int): Boolean {
        val ts = types(kind)
        return ts.isNotEmpty() && ts.all { sm.getDefaultSensor(it) != null }
    }

    fun methods(): Map<String, RyUIModuleMethod> = mapOf(
        "available" to { args, reply ->
            reply(Result.success(byteArrayOf(if (available(args.firstOrNull()?.toInt() ?: -1)) 1 else 0)))
        },
        "start" to { args, reply ->
            val b = ByteBuffer.wrap(args).order(ByteOrder.LITTLE_ENDIAN)
            val kind = b.get().toInt()
            val micros = maxOf(b.int, 5000)
            when {
                !available(kind) -> reply(Result.failure(Exception("not available")))
                kind == STEPS && Build.VERSION.SDK_INT >= 29 -> rt.withPermission(arrayOf(Manifest.permission.ACTIVITY_RECOGNITION)) { ok ->
                    if (ok) { start(kind, micros); reply(Result.success(ByteArray(0))) }
                    else reply(Result.failure(Exception("not allowed")))
                }
                else -> { start(kind, micros); reply(Result.success(ByteArray(0))) }
            }
        },
        "stop" to { args, reply ->
            args.firstOrNull()?.toInt()?.let { stop(it) }
            reply(Result.success(ByteArray(0)))
        },
        "stopAll" to { _, reply ->
            for (k in listeners.keys.toList()) stop(k)
            buffer = ByteArrayOutputStream()
            count = 0
            val ws = ArrayList(waiting)
            waiting.clear()
            for (w in ws) w(Result.failure(Exception("stopped")))
            reply(Result.success(ByteArray(0)))
        },
        "next" to { _, reply -> if (count > 0) flush(reply) else waiting.add(reply) },
        "steps" to { _, reply -> reply(Result.failure(Exception("not available"))) },
    )

    private fun add(kind: Int, nanos: Long, v: DoubleArray) {
        val b = ByteBuffer.allocate(10 + 8 * v.size).order(ByteOrder.LITTLE_ENDIAN)
        b.put(kind.toByte()).putDouble(nanos / 1e9).put(v.size.toByte())
        for (x in v) b.putDouble(x)
        buffer.write(b.array())
        count++
        // The Go side behind: the oldest go.
        if (count > 1400) {
            val all = ByteBuffer.wrap(buffer.toByteArray()).order(ByteOrder.LITTLE_ENDIAN)
            var cut = 0
            repeat(200) { if (cut + 10 <= all.limit()) cut += 10 + 8 * (all.get(cut + 9).toInt() and 0xff) }
            val rest = ByteArrayOutputStream()
            rest.write(all.array(), cut, all.limit() - cut)
            buffer = rest
            count -= 200
        }
        waiting.removeFirstOrNull()?.let { flush(it) }
    }

    private fun flush(reply: (Result<ByteArray>) -> Unit) {
        val b = buffer.toByteArray()
        buffer = ByteArrayOutputStream()
        count = 0
        reply(Result.success(b))
    }

    private fun listener(f: (SensorEvent) -> Unit) = object : SensorEventListener {
        override fun onSensorChanged(e: SensorEvent) = f(e)
        override fun onAccuracyChanged(s: Sensor, accuracy: Int) {}
    }

    private fun start(kind: Int, micros: Int) {
        stop(kind)
        val ls = ArrayList<SensorEventListener>()
        fun on(type: Int, f: (SensorEvent) -> Unit) {
            val l = listener(f)
            sm.registerListener(l, sm.getDefaultSensor(type), micros)
            ls.add(l)
        }
        when (kind) {
            ACCELEROMETER -> on(Sensor.TYPE_ACCELEROMETER) { e -> add(kind, e.timestamp, doubleArrayOf(e.values[0] / G, e.values[1] / G, e.values[2] / G)) }
            GYROSCOPE -> on(Sensor.TYPE_GYROSCOPE) { e -> add(kind, e.timestamp, doubleArrayOf(e.values[0].toDouble(), e.values[1].toDouble(), e.values[2].toDouble())) }
            MAGNETOMETER -> on(Sensor.TYPE_MAGNETIC_FIELD) { e -> add(kind, e.timestamp, doubleArrayOf(e.values[0].toDouble(), e.values[1].toDouble(), e.values[2].toDouble())) }
            MOTION -> {
                // The latest of each, sent with each rotation vector.
                val gravity = DoubleArray(3); val linear = DoubleArray(3); val gyro = DoubleArray(3)
                on(Sensor.TYPE_GRAVITY) { e -> for (i in 0..2) gravity[i] = e.values[i] / G }
                on(Sensor.TYPE_LINEAR_ACCELERATION) { e -> for (i in 0..2) linear[i] = e.values[i] / G }
                on(Sensor.TYPE_GYROSCOPE) { e -> for (i in 0..2) gyro[i] = e.values[i].toDouble() }
                on(Sensor.TYPE_ROTATION_VECTOR) { e ->
                    val x = e.values[0].toDouble(); val y = e.values[1].toDouble(); val z = e.values[2].toDouble()
                    // w: the 4th value where the sensor gives it, else from the unit length.
                    val w = if (e.values.size > 3) e.values[3].toDouble() else sqrt(maxOf(0.0, 1 - x * x - y * y - z * z))
                    add(kind, e.timestamp, gravity + linear + gyro + doubleArrayOf(x, y, z, w))
                }
            }
            // The altitude: the Go side's, from the pressure (NaN: none here).
            BAROMETER -> on(Sensor.TYPE_PRESSURE) { e -> add(kind, e.timestamp, doubleArrayOf(e.values[0].toDouble(), Double.NaN)) }
            LIGHT -> on(Sensor.TYPE_LIGHT) { e -> add(kind, e.timestamp, doubleArrayOf(e.values[0].toDouble())) }
            STEPS -> {
                // The counter counts since boot: steps since the first reading.
                var base = -1.0
                on(Sensor.TYPE_STEP_COUNTER) { e ->
                    val n = e.values[0].toDouble()
                    if (base < 0) base = n
                    add(kind, e.timestamp, doubleArrayOf(n - base, -1.0))
                }
            }
        }
        listeners[kind] = ls
    }

    private fun stop(kind: Int) {
        listeners.remove(kind)?.forEach { sm.unregisterListener(it) }
    }
}
