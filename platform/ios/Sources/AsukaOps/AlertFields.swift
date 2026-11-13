// An alert's text fields (ops/alert.go): its Fields prop, and the
// FieldChanged event's payload. Written by hand, beside the generated
// decoder.

/// A text field of an alert.
public struct AsukaAlertField: Equatable {
    public var placeholder: String
    /// Its starting text.
    public var text: String
    public var flags: UInt8
    /// An AsukaKeyboard.
    public var keyboard: Int32
    /// AutoFill's hint, as a TextField's ContentType.
    public var contentType: String

    public init(placeholder: String = "", text: String = "", flags: UInt8 = 0, keyboard: Int32 = 0, contentType: String = "") {
        self.placeholder = placeholder
        self.text = text
        self.flags = flags
        self.keyboard = keyboard
        self.contentType = contentType
    }

    public static let secureFlag: UInt8 = 1

    /// It hides what's typed: a password.
    public var secure: Bool { flags & Self.secureFlag != 0 }
}

public enum AsukaAlertFields {
    /// An alert's fields (ops.DecodeAlertFields): u32 count, then per
    /// field a u32-length placeholder, a u32-length text, u8 flags, u8
    /// keyboard and a u32-length content type. What's cut short is left
    /// out.
    public static func decode(_ b: UnsafeRawBufferPointer) -> [AsukaAlertField] {
        var i = 0
        func u8() -> UInt8? {
            guard i < b.count else { return nil }
            i += 1
            return b[i - 1]
        }
        func u32() -> Int? {
            guard i + 4 <= b.count else { return nil }
            let v = UInt32(b[i]) | UInt32(b[i + 1]) << 8 | UInt32(b[i + 2]) << 16 | UInt32(b[i + 3]) << 24
            i += 4
            return Int(v)
        }
        func string() -> String? {
            guard let n = u32(), n <= b.count - i else { return nil }
            let s = String(decoding: UnsafeRawBufferPointer(rebasing: b[i..<i + n]), as: UTF8.self)
            i += n
            return s
        }
        guard let n = u32() else { return [] }
        var out: [AsukaAlertField] = []
        for _ in 0..<min(n, b.count) {
            guard let placeholder = string(), let text = string(), let flags = u8(), let keyboard = u8(),
                  let contentType = string() else { break }
            out.append(AsukaAlertField(placeholder: placeholder, text: text, flags: flags, keyboard: Int32(keyboard), contentType: contentType))
        }
        return out
    }

    /// FieldChanged's payload (ops.AppendFieldChanged): u32 the field's
    /// index, then its text.
    public static func changed(_ index: Int, _ text: String) -> [UInt8] {
        var w = AsukaWriter()
        w.u32(UInt32(index))
        w.string(text)
        return w.bytes
    }
}
