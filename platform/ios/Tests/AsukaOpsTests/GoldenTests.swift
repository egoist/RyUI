import Foundation
import XCTest
@testable import AsukaOps

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
private final class Dumper: AsukaOpHandler {
    var out = ""
    func create(_ id: UInt32, kind: UInt16) { out += "create \(id) \(kind)\n" }
    func destroy(_ id: UInt32) { out += "destroy \(id)\n" }
    func insert(_ parent: UInt32, child: UInt32, index: Int) { out += "insert \(parent) \(child) \(index)\n" }
    func remove(_ parent: UInt32, child: UInt32) { out += "remove \(parent) \(child)\n" }
    func setProp(_ id: UInt32, prop: UInt16, value: AsukaValue) {
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
        bin.withUnsafeBytes { asukaDecode($0, d) }
        XCTAssertEqual(d.out, want)
    }

    func testEncodesGoldenEvents() throws {
        var env = AsukaEnvironment()
        env.width = 402; env.height = 874; env.safeTop = 62; env.safeBottom = 34; env.keyboard = 336
        env.scale = 3; env.fontScale = 1.25; env.dark = true; env.reduceMotion = true; env.screenReader = true; env.widthClass = 1; env.heightClass = 3; env.locale = "en_US"; env.timeZone = "Asia/Shanghai"; env.languages = "en-US,zh-Hans"; env.firstWeekday = 2; env.hourCycle = 24
        var pan = AsukaGesture()
        pan.state = AsukaGestureState.changed.rawValue
        pan.x = 10; pan.y = 20; pan.translationX = 5; pan.translationY = -3; pan.velocityX = 100; pan.velocityY = -50; pan.scale = 1; pan.rotation = 0.5
        let events: [[UInt8]] = [
            AsukaEvents.press(7),
            AsukaEvents.textChanged(7, "hé"),
            AsukaEvents.toggled(8, true),
            AsukaEvents.valueChanged(9, 0.25),
            AsukaEvents.scrolled(10, x: 1.5, y: -2),
            AsukaEvents.lifecycle(1, Int64(AsukaLifecycle.background.rawValue)),
            AsukaEvents.environment(1, env),
            AsukaEvents.pan(11, pan),
            AsukaEvents.native(12, [1, 2, 3]),
        ]
        var w = AsukaWriter()
        for e in events { w.u32(UInt32(e.count)); w.bytes.append(contentsOf: e) }
        let want = try Data(contentsOf: testdata.appendingPathComponent("events.bin"))
        XCTAssertEqual(Data(w.bytes), want)
    }
}

final class ActionsTests: XCTestCase {
    /// The Swift reader of action menus agrees with Go's: it decodes
    /// actions.bin to actions.txt.
    func testDecodesGoldenMenu() throws {
        let bin = try Data(contentsOf: testdata.appendingPathComponent("actions.bin"))
        let want = try String(contentsOf: testdata.appendingPathComponent("actions.txt"), encoding: .utf8)
        var out = ""
        func walk(_ items: [AsukaActionItem], _ depth: Int) {
            for a in items {
                out += "\(a.index)\t\(depth)\t\(a.role)\t\(a.flags)\t\(a.title)\t\(a.icon)\n"
                walk(a.items, depth + 1)
            }
        }
        walk(bin.withUnsafeBytes { AsukaActions.decode($0) }, 0)
        XCTAssertEqual(out, want)
        // Cut short, it ends where it's cut.
        let cut = bin.prefix(bin.count - 3).withUnsafeBytes { AsukaActions.decode($0) }
        var n = 0
        func count(_ items: [AsukaActionItem]) { for a in items { n += 1; count(a.items) } }
        count(cut)
        XCTAssertEqual(n, 11)
        XCTAssertEqual(Data().withUnsafeBytes { AsukaActions.decode($0) }, [])
    }
}

final class SidebarTests: XCTestCase {
    /// The Swift reader of a sidebar agrees with Go's: it decodes
    /// sidebar.bin to sidebar.txt.
    func testDecodesGoldenSidebar() throws {
        let bin = try Data(contentsOf: testdata.appendingPathComponent("sidebar.bin"))
        let want = try String(contentsOf: testdata.appendingPathComponent("sidebar.txt"), encoding: .utf8)
        let sections = bin.withUnsafeBytes { AsukaSidebar.decode($0) }
        var out = ""
        for (i, s) in sections.enumerated() {
            out += "#\t\(s.title)\n"
            for it in s.items { out += "\(i)\t\(it.title)\t\(it.icon)\t\(it.badge)\n" }
        }
        XCTAssertEqual(out, want)
        // Cut short, it ends where it's cut, with whole items.
        let cut = bin.prefix(bin.count - 18).withUnsafeBytes { AsukaSidebar.decode($0) }
        XCTAssertEqual(cut.count, 2)
        XCTAssertEqual(cut[1].items.map(\.title), ["Archiv"])
        XCTAssertEqual(Data().withUnsafeBytes { AsukaSidebar.decode($0) }, [])
        var more = sections
        more[0].items[0].badge = "4"
        XCTAssertTrue(AsukaSidebar.sameShape(sections, more))
        more[2].items.append(.init(title: "Spam"))
        XCTAssertFalse(AsukaSidebar.sameShape(sections, more))
    }
}

final class AlertFieldsTests: XCTestCase {
    /// The Swift reader of an alert's fields agrees with Go's: it decodes
    /// alertfields.bin to alertfields.txt, and writes FieldChanged's
    /// payload as fieldchanged.bin.
    func testDecodesGoldenFields() throws {
        let bin = try Data(contentsOf: testdata.appendingPathComponent("alertfields.bin"))
        let want = try String(contentsOf: testdata.appendingPathComponent("alertfields.txt"), encoding: .utf8)
        var out = ""
        for f in bin.withUnsafeBytes({ AsukaAlertFields.decode($0) }) {
            out += "\(f.placeholder)\t\(f.text)\t\(f.flags)\t\(f.keyboard)\t\(f.contentType)\n"
        }
        XCTAssertEqual(out, want)
        XCTAssertEqual(bin.prefix(bin.count - 3).withUnsafeBytes { AsukaAlertFields.decode($0) }.count, 3)
        XCTAssertEqual(Data().withUnsafeBytes { AsukaAlertFields.decode($0) }, [])
        let changed = try Data(contentsOf: testdata.appendingPathComponent("fieldchanged.bin"))
        XCTAssertEqual(Data(AsukaAlertFields.changed(2, "Grüße")), changed)
    }
}
