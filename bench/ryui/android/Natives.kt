// The bench app's native module: the meter (Meter.kt), for Go to mark
// its scenarios with ui.Call("bench", ...).
package dev.ryui.app

import dev.ryui.RyUIModuleMethod
import dev.ryui.RyUINativeModule
import dev.ryui.RyUIRuntime
import dev.ryui.registerModule

object RyUIAppNatives : RyUINativeModule {
    override fun register(runtime: RyUIRuntime) {
        runtime.activity?.let { BenchMeter.install(it) }
        fun method(f: (String) -> Unit): RyUIModuleMethod = { args, reply ->
            f(String(args, Charsets.UTF_8))
            reply(Result.success(ByteArray(0)))
        }
        runtime.registerModule("bench", mapOf(
            "begin" to method { BenchMeter.begin(it) },
            "end" to method { BenchMeter.end() },
            "startup" to method { BenchMeter.startup() },
            "memory" to method { BenchMeter.memory(it) },
            "done" to method { BenchMeter.done() },
        ))
    }
}
