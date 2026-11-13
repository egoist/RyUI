package dev.asuka

import android.app.KeyguardManager
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.os.BatteryManager
import android.os.PowerManager
import androidx.core.content.ContextCompat

/**
 * asuka/sdk/power's module: "current" (u8 battery, on battery, level 0 to
 * 100 or 255, charging, low power, thermal), "idle" (nothing: a phone
 * doesn't say), and, while watching, "next" (the state, then the events
 * since: 3 lock, 4 unlock) and "stop". The battery is the sticky
 * ACTION_BATTERY_CHANGED; low power Battery Saver; thermal
 * PowerManager's status. The screen locks as it goes off on a secure
 * lock screen, and unlocks at ACTION_USER_PRESENT. A phone doesn't
 * suspend apps as a computer sleeps: no suspend or resume.
 */
class AsukaPower(private val rt: AsukaRuntime) {
    private val pm = rt.app.getSystemService(Context.POWER_SERVICE) as PowerManager
    private val km = rt.app.getSystemService(Context.KEYGUARD_SERVICE) as KeyguardManager
    private var battery: Intent? = null

    private val receiver = object : BroadcastReceiver() {
        override fun onReceive(c: Context, i: Intent) {
            when (i.action) {
                Intent.ACTION_BATTERY_CHANGED -> { battery = i; watch.changed() }
                Intent.ACTION_SCREEN_OFF -> if (km.isKeyguardSecure) watch.event(3)
                Intent.ACTION_USER_PRESENT -> watch.event(4)
                else -> watch.changed()
            }
        }
    }
    private val thermal = PowerManager.OnThermalStatusChangedListener { watch.changed() }

    private val watch: AsukaWatch = AsukaWatch(::state, {
        val f = IntentFilter().apply {
            addAction(Intent.ACTION_BATTERY_CHANGED)
            addAction(PowerManager.ACTION_POWER_SAVE_MODE_CHANGED)
            addAction(Intent.ACTION_SCREEN_OFF)
            addAction(Intent.ACTION_USER_PRESENT)
        }
        battery = ContextCompat.registerReceiver(rt.app, receiver, f, ContextCompat.RECEIVER_NOT_EXPORTED)
        pm.addThermalStatusListener(ContextCompat.getMainExecutor(rt.app), thermal)
    }, {
        rt.app.unregisterReceiver(receiver)
        pm.removeThermalStatusListener(thermal)
        battery = null
    })

    private fun state(): ByteArray {
        val b = battery ?: rt.app.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
        val present = b?.getBooleanExtra(BatteryManager.EXTRA_PRESENT, false) == true
        val level = b?.getIntExtra(BatteryManager.EXTRA_LEVEL, -1) ?: -1
        val scale = b?.getIntExtra(BatteryManager.EXTRA_SCALE, -1) ?: -1
        val status = b?.getIntExtra(BatteryManager.EXTRA_STATUS, -1) ?: -1
        val plugged = (b?.getIntExtra(BatteryManager.EXTRA_PLUGGED, 0) ?: 0) != 0
        val pct = if (present && level >= 0 && scale > 0) Math.round(level * 100f / scale).coerceIn(0, 100) else 255
        val charging = present && status == BatteryManager.BATTERY_STATUS_CHARGING
        val heat = when (pm.currentThermalStatus) {
            PowerManager.THERMAL_STATUS_NONE, PowerManager.THERMAL_STATUS_LIGHT -> 0
            PowerManager.THERMAL_STATUS_MODERATE -> 1
            PowerManager.THERMAL_STATUS_SEVERE -> 2
            else -> 3
        }
        fun b2(v: Boolean): Byte = if (v) 1 else 0
        return byteArrayOf(b2(present), b2(present && !plugged), pct.toByte(), b2(charging), b2(pm.isPowerSaveMode), heat.toByte())
    }

    fun methods(): Map<String, AsukaModuleMethod> = watch.methods() + mapOf(
        "idle" to { _, reply -> reply(Result.success(ByteArray(0))) },
    )
}

/** asuka/sdk/dock: a phone has no Dock or taskbar: nothing to show, nothing asked. */
fun dockMethods(): Map<String, AsukaModuleMethod> = mapOf(
    "progress" to { _, reply -> reply(Result.success(ByteArray(0))) },
    "attention" to { _, reply -> reply(Result.success(ByteArray(4))) },
    "cancelAttention" to { _, reply -> reply(Result.success(ByteArray(0))) },
)
