// The bench app's native module: the meter (Meter.kt), for Go to mark
// its scenarios with ui.Call("bench", ...).
package dev.nui.app

import dev.nui.NuiModuleMethod
import dev.nui.NuiNativeModule
import dev.nui.NuiRuntime
import dev.nui.registerModule

object NuiAppNatives : NuiNativeModule {
    override fun register(runtime: NuiRuntime) {
        runtime.activity?.let { BenchMeter.install(it) }
        fun method(f: (String) -> Unit): NuiModuleMethod = { args, reply ->
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
