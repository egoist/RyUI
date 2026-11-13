// The meter for Go: BenchMeter (Meter.swift, shared with bench/swiftui).
import Asuka

@MainActor final class AsukaMeter: Meter {
    let m = BenchMeter.shared
    init() { m.install() }
    func begin(scenario: String) { m.begin(scenario) }
    func end() { m.end() }
    func startup() { m.startup() }
    func memory(scenario: String) { m.memory(scenario) }
    func done() { m.done() }
}

@MainActor func makeMeter() throws -> any Meter { AsukaMeter() }
