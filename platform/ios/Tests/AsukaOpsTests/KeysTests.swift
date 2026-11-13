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

    /// A node of the core's tree, as the host has it.
    private final class Node {
        let name: String
        let kind: AsukaKind?
        var parent: Node?
        var list: Node? // a cell's
        var keys: [AsukaKey] = []
        init(_ name: String, _ kind: AsukaKind? = .view, parent: Node? = nil, keys: [AsukaKey] = []) {
            self.name = name
            self.kind = kind
            self.parent = parent
            self.keys = keys
        }
    }

    /// Who takes a key from start out: the first node on the way with it.
    private func taker(_ start: Node?, _ root: Node, _ name: String, _ mods: UInt8 = 0) -> String? {
        AsukaKeys.way(from: start, root: root, parent: { $0.parent }, list: { $0.list }, kind: { $0.kind })
            .first { AsukaKeys.match($0.keys, name, mods) >= 0 }?.name
    }

    /// A field in a list's row: its cell has no parent; it goes on to its
    /// List, the view around it, the root.
    func testWayListRow() {
        let root = Node("root", keys: [AsukaKey("escape")])
        let around = Node("around", parent: root, keys: [AsukaKey("down")])
        let list = Node("list", .list, parent: around)
        let cell = Node("cell", nil)
        cell.list = list
        let field = Node("field", .textField, parent: Node("row", parent: cell), keys: [AsukaKey("enter")])
        XCTAssertEqual(taker(field, root, "enter"), "field")
        XCTAssertEqual(taker(field, root, "down"), "around")
        XCTAssertEqual(taker(field, root, "escape"), "root")
        XCTAssertNil(taker(field, root, "up"))
        XCTAssertEqual(AsukaKeys.way(from: field, root: root, parent: { $0.parent }, list: { $0.list }, kind: { $0.kind }).map(\.name),
                       ["field", "row", "cell", "list", "around", "root"])
    }

    /// A sheet's and a popover's content: what it declares first, then
    /// the root's (UseKeys, the sheet's content's too); nothing focused in
    /// it, from its node. A Window's node is an end.
    func testWaySheetPopoverWindow() {
        let root = Node("root", keys: [AsukaKey("escape"), AsukaKey("k", 1)])
        _ = Node("main content", parent: root)
        for kind in [AsukaKind.sheet, .popover] {
            let node = Node("presented", kind)
            let content = Node("content", parent: node, keys: [AsukaKey("escape")])
            let field = Node("field", .textField, parent: content)
            XCTAssertEqual(taker(field, root, "escape"), "content")
            XCTAssertEqual(taker(field, root, "k", 1), "root")
            XCTAssertEqual(taker(node, root, "escape"), "root")
        }
        XCTAssertEqual(taker(nil, root, "k", 1), "root") // nothing focused
        let window = Node("window", .window, keys: [AsukaKey("w", 1)])
        let field = Node("field", .textField, parent: window)
        XCTAssertEqual(taker(field, root, "w", 1), "window")
        XCTAssertNil(taker(field, root, "k", 1))
    }
}
