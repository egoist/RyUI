// The bench app's native module: the meter (Meter.swift), for Go to
// mark its scenarios with ui.Call("bench", ...).
import UIKit

@objc(AsukaAppNatives) final class AppNatives: NSObject, AsukaNativeModule {
    @MainActor static func register() {
        let m = BenchMeter.shared
        m.install()
        func method(_ f: @escaping (String) -> Void) -> AsukaModuleMethod {
            { args, reply in
                f(String(decoding: args, as: UTF8.self))
                reply(.success(Data()))
            }
        }
        AsukaRuntime.shared.registerModule("bench", [
            "begin": method { m.begin($0) },
            "end": method { _ in m.end() },
            "startup": method { _ in m.startup() },
            "memory": method { m.memory($0) },
            "done": method { _ in m.done() },
        ])
    }
}
