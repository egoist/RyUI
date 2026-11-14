#if canImport(UIKit) && ASUKA_NFC // an app that calls asuka/sdk/nfc (see the CLI's iosFeatures)
import UIKit
@preconcurrency import CoreNFC

/// asuka/sdk/nfc's module (internal/nfcspec has the wire): Core NFC's reader
/// sessions with the system's sheet. A session without polling options
/// is an NFCNDEFReaderSession (NDEF tags, no identifiers or raw commands);
/// with them, an NFCTagReaderSession (ISO 7816, MIFARE, FeliCa, ISO 15693,
/// PACE), which reads Info.plist's AIDs and FeliCa system codes. One tag at
/// a time: a session holds the tag it found until "restart" (its
/// restartPolling) or its end. iPhones have no NFC switch: "nextState"
/// and "settings" answer not available; tags that open the app are
/// universal links, which come as any link does (ui.OnOpenURL).
@MainActor final class AsukaNFC: NSObject, NFCNDEFReaderSessionDelegate, NFCTagReaderSessionDelegate {
    static let shared = AsukaNFC()
    typealias Reply = (Result<Data, Error>) -> Void

    enum Held: @unchecked Sendable { // main thread only
        case ndef(NFCNDEFTag)
        case tag(NFCTag)
        var ndef: NFCNDEFTag {
            switch self {
            case .ndef(let t): return t
            case .tag(let t):
                switch t {
                case .miFare(let m): return m
                case .iso7816(let i): return i
                case .feliCa(let f): return f
                case .iso15693(let v): return v
                @unknown default: fatalError("a tag Core NFC added")
                }
            }
        }
    }

    final class Session: @unchecked Sendable { // main thread only
        let id: UInt32
        let skipNDEF: Bool
        var reader: NFCReaderSession?
        var waiting: Reply?
        var queue: [Data] = []
        var looking = true
        var held: Held?
        var handle: UInt32 = 0
        init(id: UInt32, skipNDEF: Bool) { self.id = id; self.skipNDEF = skipNDEF }
    }

    private var session: Session?
    /// A session the app ended, until its delegate hears it's over, and
    /// the next one, which begins then (Core NFC begins none while
    /// another closes).
    private var closing: NFCReaderSession?
    private var toBegin: NFCReaderSession?
    private var handles: UInt32 = 0
    private var lastEnded: (id: UInt32, word: String)?

    private static func fail(_ word: String) -> Result<Data, Error> { .failure(AsukaModuleError(word)) }
    private static func json(_ d: Data) -> [String: Any] { (try? JSONSerialization.jsonObject(with: d)) as? [String: Any] ?? [:] }
    private static func data(_ o: Any) -> Data { (try? JSONSerialization.data(withJSONObject: o)) ?? Data("{}".utf8) }
    private static func bytes(_ o: [String: Any], _ k: String) -> Data { (o[k] as? String).flatMap { Data(base64Encoded: $0) } ?? Data() }

    func methods() -> [String: AsukaModuleMethod] {
        [
            "state": { _, reply in
                let on = NFCNDEFReaderSession.readingAvailable
                reply(.success(Self.data(["supported": on, "enabled": on])))
            },
            // An iPhone's NFC can't be turned off, nor its settings opened.
            "nextState": { _, reply in reply(Self.fail("not available")) },
            "stopState": { _, reply in reply(.success(Data())) },
            "settings": { _, reply in reply(Self.fail("not available: an iPhone has no NFC settings")) },
            "start": { [unowned self] args, reply in self.start(Self.json(args), reply) },
            "next": { [unowned self] args, reply in
                let id = UInt32(Self.json(args)["id"] as? Int ?? 0)
                guard let s = self.session, s.id == id else {
                    if let l = self.lastEnded, l.id == id { reply(Self.fail(l.word)) } else { reply(Self.fail("ended")) }
                    return
                }
                if !s.queue.isEmpty { reply(.success(s.queue.removeFirst())) } else { s.waiting = reply }
            },
            "restart": { [unowned self] args, reply in
                if let s = self.mine(args) {
                    s.held = nil
                    s.handle = 0
                    s.looking = true
                    if let r = s.reader as? NFCTagReaderSession { r.restartPolling() }
                    if let r = s.reader as? NFCNDEFReaderSession { r.restartPolling() }
                }
                reply(.success(Data()))
            },
            "alert": { [unowned self] args, reply in
                if let s = self.mine(args) { s.reader?.alertMessage = Self.json(args)["alert"] as? String ?? "" }
                reply(.success(Data()))
            },
            "stop": { [unowned self] args, reply in
                if let s = self.mine(args) {
                    let failure = Self.json(args)["error"] as? String ?? ""
                    self.close(s, failure)
                }
                reply(.success(Data()))
            },
            "read": { [unowned self] args, reply in self.op(args, reply) { t, _, reply in self.read(t, reply) } },
            "write": { [unowned self] args, reply in self.op(args, reply) { t, o, reply in self.write(t, Self.bytes(o, "message"), reply) } },
            "lock": { [unowned self] args, reply in
                self.op(args, reply) { t, _, reply in
                    t.ndef.writeLock { err in DispatchQueue.main.async { reply(err.map { .failure(Self.word($0)) } ?? .success(Data())) } }
                }
            },
            "status": { [unowned self] args, reply in
                self.op(args, reply) { t, _, reply in
                    t.ndef.queryNDEFStatus { status, capacity, err in
                        DispatchQueue.main.async {
                            if let err { reply(.failure(Self.word(err))); return }
                            reply(.success(Self.data(Self.ndefInfo(status, capacity))))
                        }
                    }
                }
            },
            "transceive": { [unowned self] args, reply in
                self.op(args, reply) { t, o, reply in self.transceive(t, o["tech"] as? String ?? "", Self.bytes(o, "data"), reply) }
            },
            "classic": { _, reply in reply(Self.fail("not supported: iOS reads no MIFARE Classic")) },
            // Tags open an iPhone app only as its universal links (ui.OnOpenURL).
            "nextOpened": { _, reply in reply(Self.fail("not available")) },
            "stopOpened": { _, reply in reply(.success(Data())) },
        ]
    }

    /// The session args name, if it's the one running.
    private func mine(_ args: Data) -> Session? {
        let id = UInt32(Self.json(args)["id"] as? Int ?? 0)
        guard let s = session, s.id == id else { return nil }
        return s
    }

    private func start(_ o: [String: Any], _ reply: @escaping Reply) {
        guard NFCNDEFReaderSession.readingAvailable else { reply(Self.fail("not available")); return }
        if let s = session { close(s, "") }
        let s = Session(id: UInt32(o["id"] as? Int ?? 0), skipNDEF: o["skipNDEF"] as? Bool ?? false)
        let poll = o["poll"] as? Int ?? 0
        let reader: NFCReaderSession
        if poll == 0 {
            reader = NFCNDEFReaderSession(delegate: self, queue: .main, invalidateAfterFirstRead: false)
        } else {
            var opts: NFCTagReaderSession.PollingOption = []
            if poll & 0b11 != 0 { opts.insert(.iso14443) } // A, B
            if poll & 0b100 != 0 { opts.insert(.iso18092) } // FeliCa
            if poll & 0b1000 != 0 { opts.insert(.iso15693) }
            if poll & 0b100000 != 0 {
                // Before iOS 26.4, PACE is alone.
                if #available(iOS 26.4, *) { opts.insert(.pace) } else { opts = .pace }
            }
            guard !opts.isEmpty, let r = NFCTagReaderSession(pollingOption: opts, delegate: self, queue: .main) else {
                reply(Self.fail("not supported: iOS polls for none of those"))
                return
            }
            reader = r
        }
        if let a = o["alert"] as? String, !a.isEmpty { reader.alertMessage = a }
        s.reader = reader
        session = s
        if closing != nil { toBegin = reader } else { reader.begin() }
        reply(.success(Data()))
    }

    /// close ends s as the app asks: the sheet closes with its tick, or
    /// with failure.
    private func close(_ s: Session, _ failure: String) {
        guard let reader = s.reader else { end(s, "ended"); return }
        end(s, "ended")
        if toBegin === reader {
            toBegin = nil // never began
            return
        }
        closing = reader
        if failure.isEmpty { reader.invalidate() } else { reader.invalidate(errorMessage: failure) }
    }

    /// end ends s (once): its waiting "next" fails with word, which a
    /// "next" after gets too.
    private func end(_ s: Session, _ word: String) {
        guard session === s else { return }
        session = nil
        lastEnded = (s.id, word)
        s.held = nil
        s.reader = nil
        if let w = s.waiting {
            s.waiting = nil
            w(Self.fail(word))
        }
    }

    private func deliver(_ s: Session, _ held: Held, _ info: [String: Any]) {
        guard session === s else { return }
        handles += 1
        s.held = held
        s.handle = handles
        var o = info
        o["handle"] = Int(handles)
        let d = Self.data(o)
        if let w = s.waiting {
            s.waiting = nil
            w(.success(d))
        } else {
            s.queue.append(d)
        }
    }

    /// op runs f on the tag args name while its session holds it.
    private func op(_ args: Data, _ reply: @escaping Reply, _ f: (Held, [String: Any], @escaping Reply) -> Void) {
        let o = Self.json(args)
        let h = UInt32(o["handle"] as? Int ?? 0)
        guard let s = session, h != 0, s.handle == h, let t = s.held else { reply(Self.fail("tag lost")); return }
        f(t, o, reply)
    }

    // The delegates (on the main queue, the sessions').

    nonisolated func readerSession(_ session: NFCNDEFReaderSession, didInvalidateWithError error: Error) {
        MainActor.assumeIsolated { self.invalidated(session, error) }
    }

    nonisolated func readerSession(_ session: NFCNDEFReaderSession, didDetectNDEFs messages: [NFCNDEFMessage]) {
        // Not called: didDetect tags is.
    }

    nonisolated func readerSession(_ session: NFCNDEFReaderSession, didDetect tags: [NFCNDEFTag]) {
        MainActor.assumeIsolated {
            guard let s = self.session, s.reader === session, s.looking, let tag = tags.first else { return }
            s.looking = false
            session.connect(to: tag) { err in
                DispatchQueue.main.async {
                    if err != nil {
                        s.looking = true
                        session.restartPolling()
                        return
                    }
                    self.found(s, .ndef(tag), ["techs": ["ndef"]])
                }
            }
        }
    }

    nonisolated func tagReaderSessionDidBecomeActive(_ session: NFCTagReaderSession) {}

    nonisolated func tagReaderSession(_ session: NFCTagReaderSession, didInvalidateWithError error: Error) {
        MainActor.assumeIsolated { self.invalidated(session, error) }
    }

    nonisolated func tagReaderSession(_ session: NFCTagReaderSession, didDetect tags: [NFCTag]) {
        MainActor.assumeIsolated {
            guard let s = self.session, s.reader === session, s.looking, let tag = tags.first else { return }
            s.looking = false
            session.connect(to: tag) { err in
                DispatchQueue.main.async {
                    if err != nil {
                        s.looking = true
                        session.restartPolling()
                        return
                    }
                    self.found(s, .tag(tag), Self.info(tag))
                }
            }
        }
    }

    private func invalidated(_ reader: NFCReaderSession, _ error: Error) {
        if reader === closing {
            closing = nil
            if let next = toBegin {
                toBegin = nil
                next.begin()
            }
            return
        }
        guard let s = session, s.reader === reader else { return }
        let code = (error as? NFCReaderError)?.code
        let word: String
        switch code {
        case .readerSessionInvalidationErrorUserCanceled: word = "canceled"
        case .readerSessionInvalidationErrorSessionTimeout: word = "timeout"
        case .readerErrorUnsupportedFeature: word = "not available"
        case .readerErrorRadioDisabled: word = "disabled"
        case .readerErrorSecurityViolation:
            word = "not available: the app lacks the NFC entitlement, or Info.plist its AIDs or FeliCa system codes (" + error.localizedDescription + ")"
        default: word = error.localizedDescription
        }
        end(s, word)
    }

    /// found reads the tag's NDEF status and message (unless the session
    /// skips them), then hands it over.
    private func found(_ s: Session, _ held: Held, _ info: [String: Any]) {
        var o = info
        if s.skipNDEF {
            o["ndef"] = ["format": 0, "capacity": -1]
            deliver(s, held, o)
            return
        }
        let t = held.ndef
        t.queryNDEFStatus { status, capacity, err in
            DispatchQueue.main.async {
                var techs = o["techs"] as? [String] ?? []
                if err != nil || status == .notSupported {
                    o["ndef"] = ["format": 0, "capacity": -1]
                    techs.removeAll { $0 == "ndef" }
                    o["techs"] = techs
                    self.deliver(s, held, o)
                    return
                }
                if !techs.contains("ndef") { techs.insert("ndef", at: 0) }
                o["techs"] = techs
                o["ndef"] = Self.ndefInfo(status, capacity)
                t.readNDEF { m, err in
                    DispatchQueue.main.async {
                        if let m { o["message"] = Self.encode(m).base64EncodedString() } else if (err as? NFCReaderError)?.code == .ndefReaderSessionErrorZeroLengthMessage {
                            o["message"] = Data([0xd0, 0, 0]).base64EncodedString()
                        }
                        self.deliver(s, held, o)
                    }
                }
            }
        }
    }

    private static func ndefInfo(_ status: NFCNDEFStatus, _ capacity: Int) -> [String: Any] {
        switch status {
        case .readWrite: return ["format": 2, "capacity": capacity, "canLock": true]
        case .readOnly: return ["format": 3, "capacity": capacity]
        default: return ["format": 0, "capacity": -1]
        }
    }

    /// A tag's identity and its technology's details.
    private static func info(_ tag: NFCTag) -> [String: Any] {
        var o: [String: Any] = [:]
        switch tag {
        case .miFare(let t):
            o["id"] = t.identifier.base64EncodedString()
            let family: String
            switch t.mifareFamily {
            case .ultralight: family = "ultralight"
            case .plus: family = "plus"
            case .desfire: family = "desfire"
            default: family = "unknown"
            }
            o["mifare"] = ["family": family]
            var techs = ["ndef", "mifare"]
            if t.mifareFamily == .desfire || t.mifareFamily == .plus { techs.append("iso7816") } // sendMiFareISO7816Command
            o["techs"] = techs
            if let h = t.historicalBytes { o["iso7816"] = ["historical": h.base64EncodedString()] }
        case .iso7816(let t):
            o["id"] = t.identifier.base64EncodedString()
            o["techs"] = ["ndef", "iso7816"]
            var i: [String: Any] = ["aid": t.initialSelectedAID, "proprietary": t.proprietaryApplicationDataCoding]
            if let h = t.historicalBytes { i["historical"] = h.base64EncodedString() }
            if let a = t.applicationData { i["applicationData"] = a.base64EncodedString() }
            o["iso7816"] = i
        case .feliCa(let t):
            o["id"] = t.currentIDm.base64EncodedString()
            o["techs"] = ["ndef", "felica"]
            o["felica"] = ["systemCode": t.currentSystemCode.base64EncodedString(), "idm": t.currentIDm.base64EncodedString()]
        case .iso15693(let t):
            o["id"] = t.identifier.base64EncodedString()
            o["techs"] = ["ndef", "iso15693"]
            o["iso15693"] = ["manufacturer": t.icManufacturerCode, "serial": t.icSerialNumber.base64EncodedString(), "dsfid": 0, "afi": 0]
        @unknown default:
            o["techs"] = ["ndef"]
        }
        return o
    }

    private func read(_ t: Held, _ reply: @escaping Reply) {
        t.ndef.readNDEF { m, err in
            DispatchQueue.main.async {
                if let m { reply(.success(Self.encode(m))); return }
                if (err as? NFCReaderError)?.code == .ndefReaderSessionErrorZeroLengthMessage { reply(.success(Data([0xd0, 0, 0]))); return }
                reply(.failure(Self.word(err)))
            }
        }
    }

    private func write(_ t: Held, _ bytes: Data, _ reply: @escaping Reply) {
        guard let m = NFCNDEFMessage(data: bytes) else { reply(Self.fail("not an NDEF message")); return }
        let tag = t.ndef
        tag.queryNDEFStatus { status, capacity, err in
            DispatchQueue.main.async {
                if let err { reply(.failure(Self.word(err))); return }
                switch status {
                case .notSupported: reply(Self.fail("not supported: not an NDEF tag")); return
                case .readOnly: reply(Self.fail("read only")); return
                default: break
                }
                if bytes.count > capacity { reply(Self.fail("too large: \(bytes.count) bytes, the tag holds \(capacity)")); return }
                tag.writeNDEF(m) { err in DispatchQueue.main.async { reply(err.map { .failure(Self.word($0)) } ?? .success(Data())) } }
            }
        }
    }

    private func transceive(_ t: Held, _ tech: String, _ data: Data, _ reply: @escaping Reply) {
        guard case .tag(let tag) = t else { reply(Self.fail("not supported: an NDEF session's tags take no commands (start the session with Poll)")); return }
        let done: (Data?, Error?) -> Void = { d, err in
            DispatchQueue.main.async { reply(err.map { .failure(Self.word($0)) } ?? .success(d ?? Data())) }
        }
        let apdu: (Data, UInt8, UInt8, Error?) -> Void = { d, sw1, sw2, err in done(d + [sw1, sw2], err) }
        switch (tech, tag) {
        case ("iso7816", .iso7816(let c)):
            guard let a = NFCISO7816APDU(data: data) else { reply(Self.fail("not an APDU")); return }
            c.sendCommand(apdu: a, completionHandler: apdu)
        case ("iso7816", .miFare(let c)) where c.mifareFamily == .desfire || c.mifareFamily == .plus:
            guard let a = NFCISO7816APDU(data: data) else { reply(Self.fail("not an APDU")); return }
            c.sendMiFareISO7816Command(a, completionHandler: apdu)
        case ("mifare", .miFare(let c)):
            c.sendMiFareCommand(commandPacket: data, completionHandler: done)
        case ("felica", .feliCa(let c)):
            c.sendFeliCaCommand(commandPacket: data, completionHandler: done)
        case ("iso15693", .iso15693(let c)):
            guard data.count >= 2 else { reply(Self.fail("not a request: flags and a command code first")); return }
            let rest = data.count > 2 ? data.subdata(in: data.startIndex + 2..<data.endIndex) : nil
            c.sendRequest(requestFlags: Int(data[data.startIndex]), commandCode: Int(data[data.startIndex + 1]), data: rest) { r in
                switch r {
                case .success(let (flags, d)): done(Data([UInt8(truncatingIfNeeded: flags.rawValue)]) + (d ?? Data()), nil)
                case .failure(let e): done(nil, e)
                }
            }
        default:
            reply(Self.fail("not supported: the tag has no \(tech) (iOS has iso7816, mifare, felica, iso15693)"))
        }
    }

    /// word is a Core NFC error as the wire's words say it.
    private static func word(_ err: Error?) -> Error {
        guard let err else { return AsukaModuleError("failed") }
        switch (err as? NFCReaderError)?.code {
        case .readerTransceiveErrorTagConnectionLost, .readerTransceiveErrorTagNotConnected, .readerTransceiveErrorSessionInvalidated:
            return AsukaModuleError("tag lost")
        case .ndefReaderSessionErrorTagNotWritable: return AsukaModuleError("read only")
        case .ndefReaderSessionErrorTagSizeTooSmall: return AsukaModuleError("too large")
        case .readerErrorUnsupportedFeature: return AsukaModuleError("not supported")
        default: return AsukaModuleError(err.localizedDescription)
        }
    }

    /// encode is a message's bytes (NFC Forum's layout: short records
    /// where they fit), which Core NFC doesn't give.
    static func encode(_ m: NFCNDEFMessage) -> Data {
        let rs = m.records
        if rs.isEmpty { return Data([0xd0, 0, 0]) }
        var b = Data()
        for (i, r) in rs.enumerated() {
            var h = UInt8(r.typeNameFormat.rawValue) & 0x07
            if i == 0 { h |= 0x80 }
            if i == rs.count - 1 { h |= 0x40 }
            let short = r.payload.count < 256
            if short { h |= 0x10 }
            if !r.identifier.isEmpty { h |= 0x08 }
            b.append(h)
            b.append(UInt8(r.type.count))
            if short {
                b.append(UInt8(r.payload.count))
            } else {
                let n = UInt32(r.payload.count)
                b.append(contentsOf: [UInt8(n >> 24), UInt8(n >> 16 & 0xff), UInt8(n >> 8 & 0xff), UInt8(n & 0xff)])
            }
            if !r.identifier.isEmpty { b.append(UInt8(r.identifier.count)) }
            b.append(r.type)
            b.append(r.identifier)
            b.append(r.payload)
        }
        return b
    }
}
#endif
