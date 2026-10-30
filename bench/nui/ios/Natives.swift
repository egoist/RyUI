// The bench app's native module: the meter (Meter.swift), for Go to
// mark its scenarios with ui.Call("bench", ...).
import UIKit

@objc(NuiAppNatives) final class AppNatives: NSObject, NuiNativeModule {
    @MainActor static func register() {
        let m = BenchMeter.shared
        m.install()
        func method(_ f: @escaping (String) -> Void) -> NuiModuleMethod {
            { args, reply in
                f(String(decoding: args, as: UTF8.self))
                reply(.success(Data()))
            }
        }
        NuiRuntime.shared.registerModule("bench", [
            "begin": method { m.begin($0) },
            "end": method { _ in m.end() },
            "startup": method { _ in m.startup() },
            "memory": method { m.memory($0) },
            "done": method { _ in m.done() },
        ])
    }
}
