import Foundation
import XCTest
@testable import NuiOps

// The Go side writes ops/testdata (go test ./ops -update). These tests
// check the generated Swift decoder and event encoders agree with it.
private let testdata = URL(fileURLWithPath: #filePath)
    .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
    .deletingLastPathComponent().deletingLastPathComponent()
    .appendingPathComponent("ops/testdata")

private func hex(_ b: UnsafeRawBufferPointer) -> String { b.map { String(format: "%02x", $0) }.joined() }
private func hex(_ s: String) -> String { Array(s.utf8).map { String(format: "%02x", $0) }.joined() }
private func f32(_ f: Float) -> String { String(format: "%08x", f.bitPattern) }

@MainActor
private final class Dumper: NuiOpHandler {
    var out = ""
    func create(_ id: UInt32, kind: UInt16) { out += "create \(id) \(kind)\n" }
    func destroy(_ id: UInt32) { out += "destroy \(id)\n" }
    func insert(_ parent: UInt32, child: UInt32, index: Int) { out += "insert \(parent) \(child) \(index)\n" }
    func remove(_ parent: UInt32, child: UInt32) { out += "remove \(parent) \(child)\n" }
    func setProp(_ id: UInt32, prop: UInt16, value: NuiValue) {
        let v: String
        let t: Int
        switch value {
        case .bool(let b): t = 1; v = b ? "true" : "false"
        case .int(let i): t = 2; v = "\(i)"
        case .float(let f): t = 3; v = String(format: "%016llx", f.bitPattern)
        case .string(let s): t = 4; v = hex(s)
        case .color(let c): t = 5; v = String(format: "%016llx", c)
        case .edges(let a, let b, let c, let d): t = 6; v = "\(f32(a)) \(f32(b)) \(f32(c)) \(f32(d))"
        case .enumValue(let e): t = 7; v = "\(e)"
        case .bytes(let b): t = 8; v = hex(b)
        }
        out += "set \(id) \(prop) \(t) \(v)\n"
    }
    func setFrame(_ id: UInt32, x: Float, y: Float, width: Float, height: Float) {
        out += "frame \(id) \(f32(x)) \(f32(y)) \(f32(width)) \(f32(height))\n"
    }
    func listen(_ id: UInt32, event: UInt16) { out += "listen \(id) \(event)\n" }
    func unlisten(_ id: UInt32, event: UInt16) { out += "unlisten \(id) \(event)\n" }
    func command(_ id: UInt32, command: UInt16, args: UnsafeRawBufferPointer) { out += "command \(id) \(command) \(hex(args))\n" }
    func setRoot(_ id: UInt32) { out += "root \(id)\n" }
}

final class GoldenTests: XCTestCase {
    @MainActor func testDecodesGoldenBatch() throws {
        let bin = try Data(contentsOf: testdata.appendingPathComponent("golden.bin"))
        let want = try String(contentsOf: testdata.appendingPathComponent("golden.txt"), encoding: .utf8)
        let d = Dumper()
        bin.withUnsafeBytes { nuiDecode($0, d) }
        XCTAssertEqual(d.out, want)
    }

    func testEncodesGoldenEvents() throws {
        var env = NuiEnvironment()
        env.width = 402; env.height = 874; env.safeTop = 62; env.safeBottom = 34; env.keyboard = 336
        env.scale = 3; env.fontScale = 1.25; env.dark = true; env.locale = "en_US"
        let events: [[UInt8]] = [
            NuiEvents.press(7),
            NuiEvents.textChanged(7, "hé"),
            NuiEvents.toggled(8, true),
            NuiEvents.valueChanged(9, 0.25),
            NuiEvents.scrolled(10, x: 1.5, y: -2),
            NuiEvents.lifecycle(1, Int64(NuiLifecycle.background.rawValue)),
            NuiEvents.environment(1, env),
        ]
        var w = NuiWriter()
        for e in events { w.u32(UInt32(e.count)); w.bytes.append(contentsOf: e) }
        let want = try Data(contentsOf: testdata.appendingPathComponent("events.bin"))
        XCTAssertEqual(Data(w.bytes), want)
    }
}
