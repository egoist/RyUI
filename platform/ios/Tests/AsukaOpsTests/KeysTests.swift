import XCTest
@testable import AsukaOps

// AsukaKeys mirrors ops.DecodeKeys, ops.MatchKey and ops.KeyName.
final class KeysTests: XCTestCase {
    /// The Keys prop as ops.AppendKeys writes it.
    private func encode(_ keys: [AsukaKey]) -> [UInt8] {
        var w = AsukaWriter()
        w.u32(UInt32(keys.count))
        for k in keys {
            let b = Array(k.name.utf8)
            w.u32(UInt32(b.count))
            for c in b { w.u8(c) }
            w.u8(k.mods)
        }
        return w.bytes
    }

    func testDecode() {
        let keys = [AsukaKey("down"), AsukaKey("n", 8), AsukaKey("é", 1 | 2), AsukaKey("f12", 4)]
        let b = encode(keys)
        XCTAssertEqual(AsukaKeys.decode(b), keys)
        // Cut short: the keys whole before the cut.
        XCTAssertEqual(AsukaKeys.decode(Array(b.prefix(b.count - 1))), Array(keys.prefix(3)))
        XCTAssertEqual(AsukaKeys.decode(Array(b.prefix(4 + 4 + 4 + 1))), [AsukaKey("down")])
        XCTAssertEqual(AsukaKeys.decode(Array(b.prefix(4 + 4 + 4))), []) // its modifiers cut off
        XCTAssertEqual(AsukaKeys.decode([]), [])
        XCTAssertEqual(AsukaKeys.decode([1, 0]), [])
        // A count larger than the bytes could hold.
        XCTAssertEqual(AsukaKeys.decode([0xff, 0xff, 0xff, 0x7f]), [])
    }

    func testMatch() {
        let keys = [AsukaKey("down"), AsukaKey("up"), AsukaKey("n", 8), AsukaKey("k", 1), AsukaKey("k", 1 | 2)]
        XCTAssertEqual(AsukaKeys.match(keys, "down", 0), 0)
        XCTAssertEqual(AsukaKeys.match(keys, "up", 0), 1)
        XCTAssertEqual(AsukaKeys.match(keys, "down", 2), -1) // exactly the modifiers
        XCTAssertEqual(AsukaKeys.match(keys, "n", 8), 2)
        XCTAssertEqual(AsukaKeys.match(keys, "N", 8), 2) // a character, any case
        XCTAssertEqual(AsukaKeys.match(keys, "n", 1), -1)
        XCTAssertEqual(AsukaKeys.match(keys, "k", 1), 3)
        XCTAssertEqual(AsukaKeys.match(keys, "K", 3), 4)
        XCTAssertEqual(AsukaKeys.match(keys, "x", 0), -1)
        XCTAssertEqual(AsukaKeys.match([], "down", 0), -1)
    }

    func testKeyName() {
        XCTAssertEqual(AsukaKeys.keyName("A"), "a")
        XCTAssertEqual(AsukaKeys.keyName("É"), "é")
        XCTAssertEqual(AsukaKeys.keyName("Down"), "Down") // a named key as it is
        XCTAssertEqual(AsukaKeys.keyName("pageup"), "pageup")
    }
}
