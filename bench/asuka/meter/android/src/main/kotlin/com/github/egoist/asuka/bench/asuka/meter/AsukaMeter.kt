// The meter for Go: BenchMeter (dev/asuka/app/Meter.kt, shared with
// bench/compose).
package com.github.egoist.asuka.bench.asuka.meter

import dev.asuka.AsukaModules
import dev.asuka.app.BenchMeter

private class AsukaMeter : Meter {
    init {
        AsukaModules.activity?.let { BenchMeter.install(it) }
    }
    override fun begin(scenario: String) { BenchMeter.begin(scenario) }
    override fun end() { BenchMeter.end() }
    override fun startup() { BenchMeter.startup() }
    override fun memory(scenario: String) { BenchMeter.memory(scenario) }
    override fun done() { BenchMeter.done() }
}

fun makeMeter(): Meter = AsukaMeter()
