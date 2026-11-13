// A view's Keys prop (ops/keys.go): what a hardware keyboard's keys do
// while the keyboard's focus is in the view. Written by hand, beside the
// generated decoder.

/// A key of a view's Keys prop (ops.Key): a lower case character or a
/// named key (enter, escape, tab, space, delete, backspace, up, down,
/// left, right, home, end, pageup, pagedown, f1-f12), with modifiers (1
/// primary: ⌘; 2 shift; 4 alt: ⌥; 8 control).
public struct AsukaKey: Equatable, Hashable {
    public let name: String
    public let mods: UInt8

    public init(_ name: String, _ mods: UInt8 = 0) {
        self.name = name
        self.mods = mods
    }
}

public enum AsukaKeys {
    /// A Keys prop's keys (ops.DecodeKeys): u32 count, per key a
    /// u32-length name and u8 modifiers; what's cut short is left out.
    public static func decode(_ b: UnsafeRawBufferPointer) -> [AsukaKey] {
        var i = 0
        func u32() -> Int? {
            guard i + 4 <= b.count else { return nil }
            let v = UInt32(b[i]) | UInt32(b[i + 1]) << 8 | UInt32(b[i + 2]) << 16 | UInt32(b[i + 3]) << 24
            i += 4
            return Int(v)
        }
        guard let n = u32(), n <= b.count else { return [] }
        var keys: [AsukaKey] = []
        keys.reserveCapacity(n)
        for _ in 0..<n {
            guard let len = u32(), len <= b.count - i, i + len < b.count else { break }
            let name = String(decoding: UnsafeRawBufferPointer(rebasing: b[i..<i + len]), as: UTF8.self)
            let mods = b[i + len]
            i += len + 1
            keys.append(AsukaKey(name, mods))
        }
        return keys
    }

    public static func decode(_ b: [UInt8]) -> [AsukaKey] {
        b.withUnsafeBytes { decode($0) }
    }

    /// The index of the key in keys that a key going down is (name as the
    /// Keys prop says it: a character lower case, as the key types it
    /// without modifiers), or -1 (ops.MatchKey).
    public static func match(_ keys: [AsukaKey], _ name: String, _ mods: UInt8) -> Int {
        let name = keyName(name)
        return keys.firstIndex { $0.mods == mods && $0.name == name } ?? -1
    }

    /// A key's name as the Keys prop has it: a single character lower
    /// case, a named key as it is (ops.KeyName).
    public static func keyName(_ name: String) -> String {
        name.unicodeScalars.count == 1 ? name.lowercased() : name
    }
}
