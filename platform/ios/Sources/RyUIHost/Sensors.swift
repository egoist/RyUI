#if canImport(UIKit) && RYUI_SENSORS // an app that calls ryui/sensors (see the CLI's iosFeatures)
import UIKit
import CoreMotion
#if canImport(RyUIOps)
import RyUIOps
#endif

/// ryui/sensors' module, with Core Motion: "available" (u8 sensor),
/// "start" (u8 sensor, u32 µs), "stop" (u8 sensor), "stopAll", "next"
/// (the samples since the last, once there are some: u8 sensor, f64
/// seconds since boot, u8 count, count f64s), "steps" (i64 ms from, to:
/// i64 steps, f64 meters). Accelerations are negated to the accelerometer's
/// reading (face up: z = +1 g), as Android's and the W3C's; device motion
/// sends gravity, user acceleration, rotation rate and, calibrated, the
/// magnetic field, from which the Go side computes the attitude as on
/// Android.
@MainActor final class RyUISensors {
    static let shared = RyUISensors()
    private let motion = CMMotionManager()
    private let altimeter = CMAltimeter()
    private let pedometer = CMPedometer()
    private var running = Set<UInt8>()
    private var intervals: [UInt8: TimeInterval] = [:]
    private var buffer = Data()
    private var count = 0
    private var waiting: [(Result<Data, Error>) -> Void] = []

    // As sensors.Sensor.
    static let accelerometer: UInt8 = 0, gyroscope: UInt8 = 1, magnetometer: UInt8 = 2, deviceMotion: UInt8 = 3
    static let barometer: UInt8 = 4, light: UInt8 = 5, steps: UInt8 = 6

    func methods() -> [String: RyUIModuleMethod] {
        [
            "available": { [unowned self] args, reply in
                reply(.success(Data([self.available(args.first ?? 255) ? 1 : 0])))
            },
            "start": { [unowned self] args, reply in
                guard args.count >= 5 else { reply(.failure(RyUIModuleError("bad args"))); return }
                let micros = args.withUnsafeBytes { b -> UInt32 in var r = RyUIReader(b); _ = r.u8(); return r.u32() }
                guard self.available(args[args.startIndex]) else { reply(.failure(RyUIModuleError("not available"))); return }
                self.start(args[args.startIndex], max(TimeInterval(micros) / 1_000_000, 0.005))
                reply(.success(Data()))
            },
            "stop": { [unowned self] args, reply in
                if let k = args.first { self.stop(k) }
                reply(.success(Data()))
            },
            "stopAll": { [unowned self] _, reply in
                for k in self.running { self.stop(k) }
                self.buffer = Data()
                self.count = 0
                let ws = self.waiting
                self.waiting = []
                for w in ws { w(.failure(RyUIModuleError("stopped"))) }
                reply(.success(Data()))
            },
            "next": { [unowned self] _, reply in
                if self.count > 0 { self.flush(reply) } else { self.waiting.append(reply) }
            },
            // The simulator has no motion sensors: "simulate" (JSON in Core
            // Motion's terms: kind, acceleration, gravity, user, rotation,
            // field, each [x, y, z]) feeds a reading through the same
            // conversions as the hardware's, for tests.
            "simulate": { [unowned self] args, reply in
                #if targetEnvironment(simulator)
                let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] ?? [:]
                func v3(_ k: String) -> [Double]? { (o[k] as? [Double]).flatMap { $0.count == 3 ? $0 : nil } }
                let t = ProcessInfo.processInfo.systemUptime
                switch UInt8(o["kind"] as? Int ?? 255) {
                case Self.accelerometer:
                    guard let a = v3("acceleration") else { break }
                    self.add(Self.accelerometer, t, Self.accelerometerValues(CMAcceleration(x: a[0], y: a[1], z: a[2])))
                case Self.deviceMotion:
                    guard let g = v3("gravity") else { break }
                    let u = v3("user") ?? [0, 0, 0], r = v3("rotation") ?? [0, 0, 0]
                    let f = v3("field").map { CMMagneticField(x: $0[0], y: $0[1], z: $0[2]) }
                    self.add(Self.deviceMotion, t, Self.motionValues(gravity: CMAcceleration(x: g[0], y: g[1], z: g[2]),
                                                                     user: CMAcceleration(x: u[0], y: u[1], z: u[2]),
                                                                     rotation: CMRotationRate(x: r[0], y: r[1], z: r[2]), field: f))
                default:
                    reply(.failure(RyUIModuleError("can't simulate that sensor"))); return
                }
                reply(.success(Data()))
                #else
                reply(.failure(RyUIModuleError("only on the simulator")))
                #endif
            },
            "steps": { [unowned self] args, reply in
                guard CMPedometer.isStepCountingAvailable() else { reply(.failure(RyUIModuleError("not available"))); return }
                guard args.count >= 16 else { reply(.failure(RyUIModuleError("bad args"))); return }
                let (from, to) = args.withUnsafeBytes { b -> (Int64, Int64) in
                    var r = RyUIReader(b)
                    return (Int64(bitPattern: r.u64()), Int64(bitPattern: r.u64()))
                }
                self.pedometer.queryPedometerData(from: Date(timeIntervalSince1970: Double(from) / 1000), to: Date(timeIntervalSince1970: Double(to) / 1000)) { d, e in
                    guard let d else { reply(.failure(Self.failure(e))); return }
                    var out = Data()
                    withUnsafeBytes(of: d.numberOfSteps.int64Value.littleEndian) { out.append(contentsOf: $0) }
                    withUnsafeBytes(of: (d.distance?.doubleValue ?? -1).bitPattern.littleEndian) { out.append(contentsOf: $0) }
                    reply(.success(out))
                }
            },
        ]
    }

    // Core Motion's readings in ryui/sensors' terms: accelerations negated
    // (Core Motion's read gravity's way, -1 g on z face up; the
    // accelerometer's, Android's and the W3C's read +1), rotation as is;
    // device motion as gravity, user acceleration, rotation and, if
    // calibrated, the magnetic field (µT).
    nonisolated static func accelerometerValues(_ a: CMAcceleration) -> [Double] { [-a.x, -a.y, -a.z] }

    nonisolated static func motionValues(gravity g: CMAcceleration, user u: CMAcceleration, rotation r: CMRotationRate, field f: CMMagneticField?) -> [Double] {
        var v = [-g.x, -g.y, -g.z, -u.x, -u.y, -u.z, r.x, r.y, r.z]
        if let f { v += [f.x, f.y, f.z] }
        return v
    }

    nonisolated static func failure(_ e: Error?) -> Error {
        if let e = e as NSError?, e.domain == CMErrorDomain, e.code == Int(CMErrorMotionActivityNotAuthorized.rawValue) {
            return RyUIModuleError("not allowed")
        }
        return e ?? RyUIModuleError("no data")
    }

    private func available(_ k: UInt8) -> Bool {
        switch k {
        case Self.accelerometer: return motion.isAccelerometerAvailable
        case Self.gyroscope: return motion.isGyroAvailable
        case Self.magnetometer: return motion.isDeviceMotionAvailable && CMMotionManager.availableAttitudeReferenceFrames().contains(.xArbitraryCorrectedZVertical)
        case Self.deviceMotion: return motion.isDeviceMotionAvailable
        case Self.barometer: return CMAltimeter.isRelativeAltitudeAvailable()
        case Self.steps: return CMPedometer.isStepCountingAvailable()
        default: return false // light: no sensor apps can read
        }
    }

    private func add(_ k: UInt8, _ t: TimeInterval, _ v: [Double]) {
        buffer.append(k)
        withUnsafeBytes(of: t.bitPattern.littleEndian) { buffer.append(contentsOf: $0) }
        buffer.append(UInt8(v.count))
        for x in v { withUnsafeBytes(of: x.bitPattern.littleEndian) { buffer.append(contentsOf: $0) } }
        count += 1
        // The Go side behind: the oldest go (a second of 200 Hz for each sensor watched).
        if count > 1400 {
            var cut = 0, n = 0
            buffer.withUnsafeBytes { b in
                while n < 200, cut + 10 <= b.count { cut += 10 + 8 * Int(b[cut + 9]); n += 1 }
            }
            buffer.removeSubrange(buffer.startIndex ..< buffer.startIndex + cut)
            count -= n
        }
        if !waiting.isEmpty { flush(waiting.removeFirst()) }
    }

    private func flush(_ reply: (Result<Data, Error>) -> Void) {
        let b = buffer
        buffer = Data()
        count = 0
        reply(.success(b))
    }

    private func start(_ k: UInt8, _ interval: TimeInterval) {
        intervals[k] = interval
        running.insert(k)
        switch k {
        case Self.accelerometer:
            motion.accelerometerUpdateInterval = interval
            if !motion.isAccelerometerActive {
                motion.startAccelerometerUpdates(to: .main) { [weak self] d, _ in
                    guard let a = d?.acceleration, let t = d?.timestamp else { return }
                    self?.add(Self.accelerometer, t, Self.accelerometerValues(a))
                }
            }
        case Self.gyroscope:
            motion.gyroUpdateInterval = interval
            if !motion.isGyroActive {
                motion.startGyroUpdates(to: .main) { [weak self] d, _ in
                    guard let r = d?.rotationRate, let t = d?.timestamp else { return }
                    self?.add(Self.gyroscope, t, [r.x, r.y, r.z])
                }
            }
        case Self.magnetometer, Self.deviceMotion:
            restartDeviceMotion()
        case Self.barometer:
            altimeter.stopRelativeAltitudeUpdates()
            altimeter.startRelativeAltitudeUpdates(to: .main) { [weak self] d, _ in
                guard let d else { return }
                self?.add(Self.barometer, d.timestamp, [d.pressure.doubleValue * 10, d.relativeAltitude.doubleValue])
            }
        case Self.steps:
            pedometer.stopUpdates()
            pedometer.startUpdates(from: Date()) { d, _ in
                guard let d else { return }
                let steps = d.numberOfSteps.doubleValue, meters = d.distance?.doubleValue ?? -1
                DispatchQueue.main.async {
                    RyUISensors.shared.add(Self.steps, ProcessInfo.processInfo.systemUptime, [steps, meters])
                }
            }
        default: break
        }
    }

    /// Device motion feeds both Magnetometer and Motion: one stream, at the
    /// faster's interval, corrected by the magnetometer where there's one.
    private func restartDeviceMotion() {
        let mine = [Self.magnetometer, Self.deviceMotion].filter { running.contains($0) }
        motion.stopDeviceMotionUpdates()
        guard !mine.isEmpty else { return }
        motion.deviceMotionUpdateInterval = mine.compactMap { intervals[$0] }.min() ?? 0.05
        let frame: CMAttitudeReferenceFrame = CMMotionManager.availableAttitudeReferenceFrames().contains(.xArbitraryCorrectedZVertical)
            ? .xArbitraryCorrectedZVertical : .xArbitraryZVertical
        motion.startDeviceMotionUpdates(using: frame, to: .main) { [weak self] d, _ in
            guard let self, let d else { return }
            let f = d.magneticField
            let calibrated = f.accuracy != .uncalibrated
            if self.running.contains(Self.magnetometer), calibrated {
                self.add(Self.magnetometer, d.timestamp, [f.field.x, f.field.y, f.field.z])
            }
            if self.running.contains(Self.deviceMotion) {
                self.add(Self.deviceMotion, d.timestamp, Self.motionValues(gravity: d.gravity, user: d.userAcceleration, rotation: d.rotationRate,
                                                                            field: calibrated ? f.field : nil))
            }
        }
    }

    private func stop(_ k: UInt8) {
        running.remove(k)
        intervals[k] = nil
        switch k {
        case Self.accelerometer: motion.stopAccelerometerUpdates()
        case Self.gyroscope: motion.stopGyroUpdates()
        case Self.magnetometer, Self.deviceMotion: restartDeviceMotion()
        case Self.barometer: altimeter.stopRelativeAltitudeUpdates()
        case Self.steps: pedometer.stopUpdates()
        default: break
        }
    }
}
#endif
