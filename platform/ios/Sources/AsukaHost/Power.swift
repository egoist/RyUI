#if canImport(UIKit)
import UIKit

/// asuka/sdk/power's module: "current" (u8 battery, on battery, level 0 to
/// 100 or 255, charging, low power, thermal), "idle" (nothing: iOS
/// doesn't say), and, while watching, "next" (the state, then the events
/// since: 3 lock, 4 unlock) and "stop". The battery is UIDevice's
/// (monitored while read or watched; unknown on the simulator), Low Power
/// Mode and the thermal state ProcessInfo's. With a passcode, the screen
/// locks as protected data goes, and unlocks as it comes back. iOS
/// suspends an app as it goes to the background, not as the device
/// sleeps: no suspend or resume.
@MainActor final class AsukaPower: NSObject {
    static let shared = AsukaPower()
    private lazy var watch = AsukaWatch(state: { [unowned self] in self.state() }, start: { [unowned self] in self.observe(true) }, end: { [unowned self] in self.observe(false) })

    private func observe(_ on: Bool) {
        UIDevice.current.isBatteryMonitoringEnabled = on
        let nc = NotificationCenter.default
        let names = [UIDevice.batteryStateDidChangeNotification, UIDevice.batteryLevelDidChangeNotification,
                     Notification.Name.NSProcessInfoPowerStateDidChange, ProcessInfo.thermalStateDidChangeNotification]
        for n in names {
            if on { nc.addObserver(self, selector: #selector(changed), name: n, object: nil) } else { nc.removeObserver(self, name: n, object: nil) }
        }
        if on {
            nc.addObserver(self, selector: #selector(locked), name: UIApplication.protectedDataWillBecomeUnavailableNotification, object: nil)
            nc.addObserver(self, selector: #selector(unlocked), name: UIApplication.protectedDataDidBecomeAvailableNotification, object: nil)
        } else {
            nc.removeObserver(self, name: UIApplication.protectedDataWillBecomeUnavailableNotification, object: nil)
            nc.removeObserver(self, name: UIApplication.protectedDataDidBecomeAvailableNotification, object: nil)
        }
    }

    // ProcessInfo's notifications come on the thread that changed it.
    @objc nonisolated func changed() { DispatchQueue.main.async { self.watch.changed() } }
    @objc func locked() { watch.event(3) }
    @objc func unlocked() { watch.event(4) }

    private func state() -> Data {
        let d = UIDevice.current
        let was = d.isBatteryMonitoringEnabled
        if !was { d.isBatteryMonitoringEnabled = true }
        defer { if !was { d.isBatteryMonitoringEnabled = false } }
        let st = d.batteryState
        let battery = st != .unknown
        let level: UInt8 = battery && d.batteryLevel >= 0 ? UInt8(min(100, max(0, (d.batteryLevel * 100).rounded()))) : 255
        let p = ProcessInfo.processInfo
        let thermal: UInt8
        switch p.thermalState {
        case .nominal: thermal = 0
        case .fair: thermal = 1
        case .serious: thermal = 2
        default: thermal = 3
        }
        return Data([battery ? 1 : 0, st == .unplugged ? 1 : 0, level, st == .charging ? 1 : 0, p.isLowPowerModeEnabled ? 1 : 0, thermal])
    }

    func methods() -> [String: AsukaModuleMethod] {
        var m = watch.methods()
        m["idle"] = { _, reply in reply(.success(Data())) }
        return m
    }
}
#endif
