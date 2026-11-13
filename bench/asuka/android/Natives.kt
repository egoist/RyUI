// The bench app's native module: the meter (Meter.kt), for Go to mark
// its scenarios with ui.Call("bench", ...).
package dev.asuka.app

import dev.asuka.AsukaModuleMethod
import dev.asuka.AsukaNativeModule
import dev.asuka.AsukaRuntime
import dev.asuka.registerModule

object AsukaAppNatives : AsukaNativeModule {
    override fun register(runtime: AsukaRuntime) {
        runtime.activity?.let { BenchMeter.install(it) }
        fun method(f: (String) -> Unit): AsukaModuleMethod = { args, reply ->
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
