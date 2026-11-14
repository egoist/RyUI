#if canImport(UIKit) && ASUKA_BLUETOOTH // an app that calls asuka/sdk/bluetooth (see the CLI's iosFeatures)
import UIKit
import CoreBluetooth

/// asuka/sdk/bluetooth's module (internal/bluetoothspec has its methods,
/// their JSON and the events), with Core Bluetooth: a CBCentralManager for
/// the central role (scans, connections, GATT), a CBPeripheralManager for
/// the peripheral role (advertising, a GATT server), both on the main
/// queue, created once the app may use Bluetooth (creating one asks for
/// the permission) or once the system relaunches the app for them (state
/// restoration, with the bluetooth-central and bluetooth-peripheral
/// background modes).
///
/// Core Bluetooth has one scan, so the Go scans are merged into it (the
/// union of their services, duplicates if one wants them) and each gets
/// its own advertisements; one advertisement, so the Go advertisements
/// are merged too (the union of their services, the newest's local name);
/// one connection to a peripheral, which the Go connections to it share
/// (its handles, its subscriptions: on while one of them wants it).
@MainActor final class AsukaBluetooth: NSObject, CBCentralManagerDelegate, CBPeripheralDelegate, CBPeripheralManagerDelegate {
    static let shared = AsukaBluetooth()
    static let centralRestoreID = "dev.asuka.bluetooth.central"
    static let peripheralRestoreID = "dev.asuka.bluetooth.peripheral"

    private var central: CBCentralManager?
    private var pm: CBPeripheralManager?
    private var centralWaiters: [() -> Void] = []
    private var pmWaiters: [() -> Void] = []
    private var lastState: String?
    private let encoder = JSONEncoder()

    // The events, and the "next" waiting for them.
    private var queue: [BTEvent] = []
    private var waiting: BTReply?
    private var following = false // Go asks for events ("next" until "stop")
    private var flushing = false

    // Scans.
    private var scans: [UInt32: BTScan] = [:]
    private var scanning: (services: [String]?, duplicates: Bool)? // Core Bluetooth's scan

    // Peripherals by identifier: those scanned, retrieved or restored, which
    // "connect" names; the connections (Go's by its numbers, links by
    // peripheral), and those closing (a "disconnect" waiting).
    private var peripherals: [UUID: CBPeripheral] = [:]
    private var conns: [UInt32: BTLink] = [:]
    private var links: [UUID: BTLink] = [:]
    private var closing: [UUID: BTLink] = [:]

    // What iOS restored at launch, for "restore" (once).
    private var restored: BTRestored?

    // The peripheral role.
    private var adverts: [UInt32: BTAdvertiseArgs] = [:]
    private var advertOrder: [UInt32] = []
    private var advertStarting: [(UInt32, BTReply)] = []
    private var advertInFlight = false, advertDirty = false
    private var servers: [UInt32: [CBMutableService]] = [:]
    private var locals: [ObjectIdentifier: BTLocal] = [:]
    private var localChars: [UInt32: [UInt32: CBMutableCharacteristic]] = [:]
    private var fixed: [ObjectIdentifier: Data] = [:] // values the host answers reads with
    private var subscribers: [ObjectIdentifier: [UUID: CBCentral]] = [:]
    private var serveJobs: [BTServeJob] = []
    private var nextRequest: UInt32 = 0
    private var readRequests: [UInt32: (server: UInt32, request: CBATTRequest)] = [:]
    private var writeRequests: [UInt32: BTWriteBatch] = [:]
    private var notifyQueue: [(CBMutableCharacteristic, Data, [CBCentral]?, BTReply)] = []
    private var restoredServices: [CBMutableService] = [] // published by the app's last run

    private static let backgroundModes = Bundle.main.object(forInfoDictionaryKey: "UIBackgroundModes") as? [String] ?? []

    // MARK: Launch

    /// The system relaunched the app for Bluetooth: its managers again, with
    /// their restore identifiers, before launch ends (AppDelegate).
    func launched(_ options: [UIApplication.LaunchOptionsKey: Any]?) {
        if let ids = options?[.bluetoothCentrals] as? [String], ids.contains(Self.centralRestoreID), central == nil {
            makeCentral(alert: false)
        }
        if let ids = options?[.bluetoothPeripherals] as? [String], ids.contains(Self.peripheralRestoreID), pm == nil {
            makePeripheralManager()
        }
    }

    private func makeCentral(alert: Bool) {
        var o: [String: Any] = [CBCentralManagerOptionShowPowerAlertKey: alert]
        if Self.backgroundModes.contains("bluetooth-central") {
            o[CBCentralManagerOptionRestoreIdentifierKey] = Self.centralRestoreID
        }
        central = CBCentralManager(delegate: self, queue: nil, options: o)
    }

    private func makePeripheralManager() {
        var o: [String: Any] = [CBPeripheralManagerOptionShowPowerAlertKey: false]
        if Self.backgroundModes.contains("bluetooth-peripheral") {
            o[CBPeripheralManagerOptionRestoreIdentifierKey] = Self.peripheralRestoreID
        }
        pm = CBPeripheralManager(delegate: self, queue: nil, options: o)
    }

    // MARK: State

    /// The permission's state where it stops the app (nil: allowed).
    private static func authorizationWord() -> String? {
        switch CBManager.authorization {
        case .notDetermined: return "notAsked"
        case .denied, .restricted: return "denied"
        default: return nil
        }
    }

    private static func word(_ s: CBManagerState) -> String {
        switch s {
        case .unsupported: return "unsupported"
        case .unauthorized: return "denied"
        case .poweredOff: return "off"
        case .poweredOn: return "on"
        default: return "unknown"
        }
    }

    /// Whether a manager's state is known: not starting or resetting, nor
    /// unauthorized while the prompt is still up.
    private static func settled(_ s: CBManagerState) -> Bool {
        switch s {
        case .unknown, .resetting: return false
        case .unauthorized: return CBManager.authorization != .notDetermined
        default: return true
        }
    }

    /// The adapter's state as the managers have it (nil: not known yet).
    private func managersState() -> String? {
        if let c = central, Self.settled(c.state) { return Self.word(c.state) }
        if let p = pm, Self.settled(p.state) { return Self.word(p.state) }
        return nil
    }

    /// Answers "state" (what Go's watchers start from: the events say what
    /// changes after it) or "request" (recorded: false).
    private func answerState(_ reply: BTReply, _ s: String, record: Bool = true) {
        if record { lastState = s }
        json(reply, BTStateOut(state: s))
    }

    private func emitState() {
        guard let s = managersState(), s != lastState else { return }
        lastState = s
        push(BTEvent(type: "state", state: s))
    }

    /// Runs then with the central manager once it's on, or fails with its
    /// state's word; it's created if the app may use Bluetooth.
    private func withCentral(_ reply: @escaping BTReply, _ then: @escaping (CBCentralManager) -> Void) {
        if central == nil {
            if let w = Self.authorizationWord() { fail(reply, w); return }
            makeCentral(alert: false)
        }
        guard let c = central else { return }
        guard Self.settled(c.state) else {
            centralWaiters.append { [unowned self] in self.withCentral(reply, then) }
            return
        }
        guard c.state == .poweredOn else { fail(reply, Self.word(c.state)); return }
        then(c)
    }

    /// withCentral's, for the peripheral manager.
    private func withPeripheralManager(_ reply: @escaping BTReply, _ then: @escaping (CBPeripheralManager) -> Void) {
        if pm == nil {
            if let w = Self.authorizationWord() { fail(reply, w); return }
            makePeripheralManager()
        }
        guard let p = pm else { return }
        guard Self.settled(p.state) else {
            pmWaiters.append { [unowned self] in self.withPeripheralManager(reply, then) }
            return
        }
        guard p.state == .poweredOn else { fail(reply, Self.word(p.state)); return }
        then(p)
    }

    // MARK: Events

    private func push(_ e: BTEvent) {
        queue.append(e)
        guard waiting != nil, !flushing else { return }
        // The events of this turn of the run loop together.
        flushing = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            self.flushing = false
            self.flush()
        }
    }

    private func flush() {
        guard let w = waiting, !queue.isEmpty else { return }
        let data = (try? encoder.encode(queue)) ?? Data("[]".utf8)
        queue = []
        waiting = nil
        w(.success(data))
    }

    // MARK: Methods

    func methods() -> [String: AsukaModuleMethod] {
        [
            "state": { [unowned self] _, reply in
                if let c = self.central {
                    if Self.settled(c.state) { self.answerState(reply, Self.word(c.state)); return }
                    self.centralWaiters.append { [unowned self] in self.answerState(reply, Self.word(c.state)) }
                    return
                }
                if let s = self.managersState() { self.answerState(reply, s); return }
                if let w = Self.authorizationWord() { self.answerState(reply, w); return }
                // Allowed: the manager knows whether it's on, and asks nothing.
                self.makeCentral(alert: false)
                self.centralWaiters.append { [unowned self] in self.answerState(reply, Self.word(self.central!.state)) }
            },
            "request": { [unowned self] _, reply in
                // Creating the manager shows the prompt (and, off, the
                // system's alert to turn Bluetooth on).
                if self.central == nil { self.makeCentral(alert: true) }
                let c = self.central!
                if Self.settled(c.state) { self.answerState(reply, Self.word(c.state), record: false); return }
                self.centralWaiters.append { [unowned self] in self.answerState(reply, Self.word(c.state), record: false) }
            },
            "next": { [unowned self] _, reply in
                if let w = self.waiting { w(.failure(AsukaModuleError("stopped"))) }
                self.waiting = reply
                self.following = true
                self.flush()
            },
            "stop": { [unowned self] _, reply in
                self.following = false
                if let w = self.waiting {
                    self.waiting = nil
                    w(.failure(AsukaModuleError("stopped")))
                }
                ok(reply)
            },
            "scan": { [unowned self] args, reply in
                guard let a = decode(BTScanArgs.self, args, reply) else { return }
                var services: Set<String>?
                if let ss = a.services, !ss.isEmpty {
                    let us = ss.compactMap(Self.cbUUID)
                    guard us.count == ss.count else { fail(reply, "bad args: not a UUID in \(ss)"); return }
                    services = Set(us.map(Self.wire))
                }
                self.withCentral(reply) { c in
                    self.scans[a.id] = BTScan(services: services, duplicates: a.duplicates ?? false)
                    self.updateScan(c, restart: true)
                    ok(reply)
                }
            },
            "stopScan": { [unowned self] args, reply in
                guard let a = decode(BTIDArgs.self, args, reply) else { return }
                if self.scans.removeValue(forKey: a.id) != nil, let c = self.central, c.state == .poweredOn {
                    self.updateScan(c, restart: false)
                }
                ok(reply)
            },
            // Android's: iOS's background scan is the app's own, while it runs.
            "stopBackgroundScan": { _, reply in ok(reply) },
            "choose": { _, reply in fail(reply, "not available: iOS has no device chooser for apps (scan instead)") },
            "known": { [unowned self] args, reply in
                guard let a = decode(BTKnownArgs.self, args, reply) else { return }
                self.withCentral(reply) { c in
                    let ids = (a.ids ?? []).compactMap { UUID(uuidString: $0) }
                    let ps = ids.isEmpty ? [] : c.retrievePeripherals(withIdentifiers: ids)
                    for p in ps { self.remember(p) }
                    json(reply, ps.map(Self.device))
                }
            },
            "connected": { [unowned self] args, reply in
                guard let a = decode(BTConnectedArgs.self, args, reply) else { return }
                let us = (a.services ?? []).compactMap(Self.cbUUID)
                guard us.count == (a.services ?? []).count else { fail(reply, "bad args: not a UUID"); return }
                self.withCentral(reply) { c in
                    // iOS lists them by service only.
                    let ps = us.isEmpty ? [] : c.retrieveConnectedPeripherals(withServices: us)
                    for p in ps { self.remember(p) }
                    json(reply, ps.map(Self.device))
                }
            },
            "connect": { [unowned self] args, reply in
                guard let a = decode(BTConnectArgs.self, args, reply) else { return }
                self.withCentral(reply) { c in self.connect(c, a, reply) }
            },
            "disconnect": { [unowned self] args, reply in
                guard let a = decode(BTConnArgs.self, args, reply) else { return }
                self.disconnect(a.conn, reply)
            },
            "services": { [unowned self] args, reply in
                guard let a = decode(BTConnArgs.self, args, reply), let l = self.link(a.conn, reply) else { return }
                if let t = l.tree { reply(.success(t)); return }
                l.discoveryWaiters.append(BTPending(a.conn, reply))
                if l.discovering == 0 {
                    l.discovering = 1
                    l.discoveryError = nil
                    l.peripheral.discoverServices(nil)
                }
            },
            "read": { [unowned self] args, reply in
                guard let a = decode(BTAttrArgs.self, args, reply), let l = self.link(a.conn, reply) else { return }
                guard let ch = l.attrs[a.handle] as? CBCharacteristic else { fail(reply, "not found"); return }
                l.reads[ObjectIdentifier(ch), default: []].append(BTPending(a.conn, reply))
                l.peripheral.readValue(for: ch)
            },
            "write": { [unowned self] args, reply in
                guard let a = decode(BTWriteArgs.self, args, reply), let l = self.link(a.conn, reply) else { return }
                guard let ch = l.attrs[a.handle] as? CBCharacteristic else { fail(reply, "not found"); return }
                let v = a.value ?? Data()
                if a.response ?? false {
                    guard ch.properties.contains(.write) else { fail(reply, "att 3: write not permitted"); return }
                    l.writes[ObjectIdentifier(ch), default: []].append(BTPending(a.conn, reply))
                    l.peripheral.writeValue(v, for: ch, type: .withResponse)
                    return
                }
                guard ch.properties.contains(.writeWithoutResponse) else { fail(reply, "att 3: write without response not permitted"); return }
                let most = l.peripheral.maximumWriteValueLength(for: .withoutResponse)
                guard v.count <= most else { fail(reply, "att 13: the value is longer than a write without response takes (\(most) bytes)"); return }
                l.noResponse.append((ch, v, BTPending(a.conn, reply)))
                self.sendWrites(l)
            },
            "readDescriptor": { [unowned self] args, reply in
                guard let a = decode(BTAttrArgs.self, args, reply), let l = self.link(a.conn, reply) else { return }
                guard let d = l.attrs[a.handle] as? CBDescriptor else { fail(reply, "not found"); return }
                l.descReads[ObjectIdentifier(d), default: []].append(BTPending(a.conn, reply))
                l.peripheral.readValue(for: d)
            },
            "writeDescriptor": { [unowned self] args, reply in
                guard let a = decode(BTWriteArgs.self, args, reply), let l = self.link(a.conn, reply) else { return }
                guard let d = l.attrs[a.handle] as? CBDescriptor else { fail(reply, "not found"); return }
                let v = a.value ?? Data()
                // Core Bluetooth writes the CCCD itself: setNotifyValue.
                if d.uuid == CBUUID(string: CBUUIDClientCharacteristicConfigurationString) {
                    guard let ch = d.characteristic else { fail(reply, "not found"); return }
                    let on = (v.first ?? 0) & 3 != 0
                    if on { self.subscribe(l, a.conn, ch, reply) } else { self.unsubscribe(l, a.conn, ch, reply) }
                    return
                }
                l.descWrites[ObjectIdentifier(d), default: []].append(BTPending(a.conn, reply))
                l.peripheral.writeValue(v, for: d)
            },
            "subscribe": { [unowned self] args, reply in
                // Indicate: Core Bluetooth picks notifications where the
                // characteristic has both.
                guard let a = decode(BTSubscribeArgs.self, args, reply), let l = self.link(a.conn, reply) else { return }
                guard let ch = l.attrs[a.handle] as? CBCharacteristic else { fail(reply, "not found"); return }
                guard !ch.properties.isDisjoint(with: [.notify, .indicate]) else { fail(reply, "att 6: the characteristic doesn't notify"); return }
                self.subscribe(l, a.conn, ch, reply)
            },
            "unsubscribe": { [unowned self] args, reply in
                guard let a = decode(BTAttrArgs.self, args, reply), let l = self.link(a.conn, reply) else { return }
                guard let ch = l.attrs[a.handle] as? CBCharacteristic else { fail(reply, "not found"); return }
                self.unsubscribe(l, a.conn, ch, reply)
            },
            // iOS negotiates the MTU itself: requestMtu answers what it is.
            "mtu": { [unowned self] args, reply in
                guard let a = decode(BTConnArgs.self, args, reply), let l = self.link(a.conn, reply) else { return }
                json(reply, Self.mtu(l.peripheral))
            },
            "requestMtu": { [unowned self] args, reply in
                guard let a = decode(BTConnArgs.self, args, reply), let l = self.link(a.conn, reply) else { return }
                json(reply, Self.mtu(l.peripheral))
            },
            "rssi": { [unowned self] args, reply in
                guard let a = decode(BTConnArgs.self, args, reply), let l = self.link(a.conn, reply) else { return }
                l.rssi.append(BTPending(a.conn, reply))
                l.peripheral.readRSSI()
            },
            // Android's: iOS picks the connection's parameters itself.
            "priority": { [unowned self] args, reply in
                guard let a = decode(BTConnArgs.self, args, reply), self.link(a.conn, reply) != nil else { return }
                ok(reply)
            },
            "advertise": { [unowned self] args, reply in
                guard let a = decode(BTAdvertiseArgs.self, args, reply) else { return }
                let us = (a.services ?? []).compactMap(Self.cbUUID)
                guard us.count == (a.services ?? []).count else { fail(reply, "bad args: not a UUID"); return }
                self.withPeripheralManager(reply) { p in
                    self.adverts[a.id] = a
                    self.advertOrder.removeAll { $0 == a.id }
                    self.advertOrder.append(a.id)
                    self.advertStarting.append((a.id, reply))
                    self.startAdvertising(p)
                }
            },
            "stopAdvertise": { [unowned self] args, reply in
                guard let a = decode(BTIDArgs.self, args, reply) else { return }
                if self.adverts.removeValue(forKey: a.id) != nil {
                    self.advertOrder.removeAll { $0 == a.id }
                    for (id, r) in self.advertStarting where id == a.id { fail(r, "canceled") }
                    self.advertStarting.removeAll { $0.0 == a.id }
                    if let p = self.pm, p.state == .poweredOn { self.startAdvertising(p) }
                }
                ok(reply)
            },
            "serve": { [unowned self] args, reply in
                guard let a = decode(BTServeArgs.self, args, reply) else { return }
                self.withPeripheralManager(reply) { p in self.serve(p, a, reply) }
            },
            "unserve": { [unowned self] args, reply in
                guard let a = decode(BTIDArgs.self, args, reply) else { return }
                self.unserve(a.id)
                ok(reply)
            },
            "respond": { [unowned self] args, reply in
                guard let a = decode(BTRespondArgs.self, args, reply) else { return }
                let code = a.error ?? 0
                if let r = self.readRequests.removeValue(forKey: a.request) {
                    if code == 0 { r.request.value = a.value ?? Data() }
                    self.pm?.respond(to: r.request, withResult: Self.result(code))
                } else if let b = self.writeRequests.removeValue(forKey: a.request) {
                    self.answer(b, a.request, code)
                }
                ok(reply)
            },
            "notify": { [unowned self] args, reply in
                guard let a = decode(BTNotifyArgs.self, args, reply) else { return }
                guard let p = self.pm, p.state == .poweredOn else { fail(reply, self.pm.map { Self.word($0.state) } ?? "off"); return }
                guard let ch = self.localChars[a.server]?[a.handle] else { fail(reply, "not found"); return }
                var to: [CBCentral]?
                if let ids = a.centrals, !ids.isEmpty {
                    let subs = self.subscribers[ObjectIdentifier(ch)] ?? [:]
                    let cs = ids.compactMap { UUID(uuidString: $0).flatMap { subs[$0] } }
                    guard !cs.isEmpty else { ok(reply); return } // none of them subscribed
                    to = cs
                }
                self.notifyQueue.append((ch, a.value ?? Data(), to, reply))
                self.sendNotifications(p)
            },
            "restore": { [unowned self] _, reply in
                let r = self.restored ?? BTRestored()
                self.restored = nil
                json(reply, r)
            },
        ]
    }

    // MARK: Scans

    /// Core Bluetooth's scan for the Go scans: the union of their services
    /// (none if one has none), duplicates if one wants them. A new Go scan
    /// starts it again, which hears the devices around again (Core
    /// Bluetooth reports each once a scan, without duplicates).
    private func updateScan(_ c: CBCentralManager, restart: Bool) {
        guard !scans.isEmpty else {
            if c.isScanning { c.stopScan() }
            scanning = nil
            return
        }
        var services: [String]? = []
        var duplicates = false
        for s in scans.values {
            duplicates = duplicates || s.duplicates
            if let ss = s.services { services?.append(contentsOf: ss) } else { services = nil }
        }
        services = services.map { Array(Set($0)).sorted() }
        if !restart, let now = scanning, now.services == services, now.duplicates == duplicates, c.isScanning { return }
        if c.isScanning { c.stopScan() }
        scanning = (services, duplicates)
        c.scanForPeripherals(withServices: services?.compactMap(Self.cbUUID),
                             options: [CBCentralManagerScanOptionAllowDuplicatesKey: duplicates])
    }

    private func remember(_ p: CBPeripheral) {
        peripherals[p.identifier] = p
        p.delegate = self
    }

    nonisolated func centralManager(_ c: CBCentralManager, didDiscover p: CBPeripheral, advertisementData ad: [String: Any], rssi: NSNumber) {
        MainActor.assumeIsolated {
            remember(p)
            guard !scans.isEmpty else { return } // a restored scan no Go scan has taken over
            let a = Self.advertisement(p, ad, rssi)
            let heard = Set((a.services ?? []))
            // What Core Bluetooth's filter let through matches a scan whose
            // services are all of the filter's.
            let filter = scanning?.services.map(Set.init)
            for (id, s) in scans {
                if let want = s.services {
                    let filtered = filter.map { $0.isSubset(of: want) } ?? false
                    if !filtered && want.isDisjoint(with: heard) { continue }
                }
                if !s.duplicates {
                    if s.seen.contains(p.identifier) { continue }
                    s.seen.insert(p.identifier)
                }
                push(BTEvent(type: "advertisement", scan: id, advertisement: a))
            }
        }
    }

    private static func advertisement(_ p: CBPeripheral, _ ad: [String: Any], _ rssi: NSNumber) -> BTAdvertisement {
        var a = BTAdvertisement(device: device(p), rssi: rssi.intValue, time: Int64(Date().timeIntervalSince1970 * 1000))
        a.localName = ad[CBAdvertisementDataLocalNameKey] as? String
        let services = (ad[CBAdvertisementDataServiceUUIDsKey] as? [CBUUID] ?? []) + (ad[CBAdvertisementDataOverflowServiceUUIDsKey] as? [CBUUID] ?? [])
        if !services.isEmpty { a.services = services.map(wire) }
        if let s = ad[CBAdvertisementDataSolicitedServiceUUIDsKey] as? [CBUUID], !s.isEmpty { a.solicited = s.map(wire) }
        if let d = ad[CBAdvertisementDataServiceDataKey] as? [CBUUID: Data], !d.isEmpty {
            var m: [String: Data] = [:]
            for (k, v) in d { m[wire(k)] = v }
            a.serviceData = m
        }
        // The company's identifier first, little-endian.
        if let d = ad[CBAdvertisementDataManufacturerDataKey] as? Data, d.count >= 2 {
            let b = [UInt8](d)
            a.manufacturerData = [String(UInt16(b[0]) | UInt16(b[1]) << 8): Data(b[2...])]
        }
        if let t = ad[CBAdvertisementDataTxPowerLevelKey] as? NSNumber { a.txPower = t.intValue }
        if let c = ad[CBAdvertisementDataIsConnectable] as? NSNumber { a.connectable = c.boolValue }
        return a
    }

    private static func device(_ p: CBPeripheral) -> BTDevice { BTDevice(id: p.identifier.uuidString, name: p.name) }

    // MARK: Connections

    private func connect(_ c: CBCentralManager, _ a: BTConnectArgs, _ reply: @escaping BTReply) {
        guard let id = UUID(uuidString: a.device) else { fail(reply, "not found"); return }
        guard let p = peripherals[id] ?? c.retrievePeripherals(withIdentifiers: [id]).first else { fail(reply, "not found"); return }
        remember(p)
        let l: BTLink
        if let have = links[id] {
            l = have
        } else {
            l = BTLink(p)
            links[id] = l
        }
        conns[a.conn] = l
        l.conns.insert(a.conn)
        // Connected already: another Go connection's, or kept by iOS (state
        // restoration).
        if p.state == .connected && closing[id] == nil {
            l.up = true
            ok(reply)
            return
        }
        l.connecting[a.conn] = reply
        if closing[id] != nil {
            l.deferred = true // once the one closing has disconnected
        } else if p.state != .connecting {
            c.connect(p, options: nil)
        }
    }

    private func disconnect(_ conn: UInt32, _ reply: @escaping BTReply) {
        guard let l = conns.removeValue(forKey: conn) else { ok(reply); return }
        l.conns.remove(conn)
        if let r = l.connecting.removeValue(forKey: conn) { fail(r, "canceled") }
        l.fail(conn, AsukaModuleError("disconnected"))
        let p = l.peripheral
        // Its subscriptions: off unless another Go connection has them.
        for (k, var s) in l.subs where s.contains(conn) {
            s.remove(conn)
            l.subs[k] = s.isEmpty ? nil : s
            if s.isEmpty, !l.conns.isEmpty, p.state == .connected, let ch = l.characteristic(k), ch.isNotifying {
                p.setNotifyValue(false, for: ch)
            }
        }
        guard l.conns.isEmpty else { ok(reply); return }
        links[p.identifier] = nil
        guard let c = central else { ok(reply); return }
        switch p.state {
        case .connected:
            // Answered once it has disconnected.
            l.closers.append(reply)
            closing[p.identifier] = l
            c.cancelPeripheralConnection(p)
        case .connecting:
            c.cancelPeripheralConnection(p)
            ok(reply)
        default:
            ok(reply)
        }
    }

    /// The link a GATT call names, up; else it fails "disconnected".
    private func link(_ conn: UInt32, _ reply: BTReply) -> BTLink? {
        guard let l = conns[conn], l.up else { fail(reply, "disconnected"); return nil }
        return l
    }

    nonisolated func centralManager(_ c: CBCentralManager, didConnect p: CBPeripheral) {
        MainActor.assumeIsolated {
            guard let l = links[p.identifier], !l.up else { return }
            l.up = true
            let rs = l.connecting
            l.connecting = [:]
            for r in rs.values { ok(r) }
        }
    }

    nonisolated func centralManager(_ c: CBCentralManager, didFailToConnect p: CBPeripheral, error: Error?) {
        MainActor.assumeIsolated {
            guard let l = links[p.identifier], !l.up else { return }
            let e = Self.error(error, "the connection failed")
            for (conn, r) in l.connecting {
                conns[conn] = nil
                l.conns.remove(conn)
                r(.failure(e))
            }
            l.connecting = [:]
            if l.conns.isEmpty { links[p.identifier] = nil }
        }
    }

    nonisolated func centralManager(_ c: CBCentralManager, didDisconnectPeripheral p: CBPeripheral, error: Error?) {
        MainActor.assumeIsolated {
            let id = p.identifier
            if let l = closing.removeValue(forKey: id) {
                // Go's "disconnect": answered, no event.
                for r in l.closers { ok(r) }
                l.closers = []
                if let next = links[id], next.deferred {
                    next.deferred = false
                    c.connect(p, options: nil)
                }
                return
            }
            guard let l = links[id], l.up else { return } // connecting: Core Bluetooth goes on
            links[id] = nil
            var reason = "local", code = 0
            if let e = error as NSError? {
                code = e.code
                if e.domain == CBErrorDomain {
                    switch CBError.Code(rawValue: e.code) {
                    case .connectionTimeout: reason = "timeout"
                    case .peripheralDisconnected: reason = "remote"
                    default: reason = "failed"
                    }
                } else {
                    reason = "unknown"
                }
            }
            end(l, reason: reason, message: error?.localizedDescription, code: code)
        }
    }

    /// Ends a link that was up: each Go connection's "disconnected", and the
    /// calls waiting on it failed.
    private func end(_ l: BTLink, reason: String, message: String?, code: Int) {
        l.fail(nil, AsukaModuleError("disconnected"))
        for conn in l.conns.sorted() {
            conns[conn] = nil
            push(BTEvent(type: "disconnected", conn: conn, reason: reason, error: message, code: code == 0 ? nil : code))
        }
        l.conns = []
        l.up = false
    }

    nonisolated func centralManagerDidUpdateState(_ c: CBCentralManager) {
        MainActor.assumeIsolated {
            if c.state != .poweredOn { centralStopped(c.state) }
            if Self.settled(c.state) {
                let ws = centralWaiters
                centralWaiters = []
                for w in ws { w() }
            }
            emitState()
        }
    }

    /// The central role's state isn't on: its scans and connections end.
    private func centralStopped(_ s: CBManagerState) {
        let w = Self.word(s)
        for id in scans.keys.sorted() { push(BTEvent(type: "scanStopped", scan: id, error: w)) }
        scans = [:]
        scanning = nil
        for l in closing.values {
            for r in l.closers { ok(r) }
        }
        closing = [:]
        for l in links.values.sorted(by: { ($0.conns.min() ?? 0) < ($1.conns.min() ?? 0) }) {
            if l.up {
                end(l, reason: s == .poweredOff ? "off" : "failed", message: s == .poweredOff ? "Bluetooth turned off" : "Bluetooth is \(w)", code: 0)
            } else {
                for (conn, r) in l.connecting {
                    conns[conn] = nil
                    fail(r, w)
                }
                l.connecting = [:]
                l.conns = []
            }
        }
        links = [:]
    }

    nonisolated func centralManager(_ c: CBCentralManager, willRestoreState dict: [String: Any]) {
        MainActor.assumeIsolated {
            var r = restored ?? BTRestored()
            for p in dict[CBCentralManagerRestoredStatePeripheralsKey] as? [CBPeripheral] ?? [] {
                remember(p)
                if !(r.devices ?? []).contains(where: { $0.id == p.identifier.uuidString }) {
                    r.devices = (r.devices ?? []) + [Self.device(p)]
                }
            }
            if let s = dict[CBCentralManagerRestoredStateScanServicesKey] as? [CBUUID], !s.isEmpty {
                r.scanServices = s.map(Self.wire)
                let dups = (dict[CBCentralManagerRestoredStateScanOptionsKey] as? [String: Any])?[CBCentralManagerScanOptionAllowDuplicatesKey] as? Bool ?? false
                scanning = (r.scanServices.map { Array(Set($0)).sorted() }, dups)
            }
            guard r.devices != nil || r.scanServices != nil else { return }
            restored = r
            if following { push(BTEvent(type: "restored")) }
        }
    }

    // MARK: GATT client

    nonisolated func peripheral(_ p: CBPeripheral, didDiscoverServices error: Error?) {
        MainActor.assumeIsolated {
            guard let l = links[p.identifier], l.discovering > 0 else { return }
            if let error { l.discoveryError = l.discoveryError ?? error }
            if error == nil {
                for s in p.services ?? [] {
                    l.discovering += 2
                    p.discoverIncludedServices(nil, for: s)
                    p.discoverCharacteristics(nil, for: s)
                }
            }
            discovered(l)
        }
    }

    nonisolated func peripheral(_ p: CBPeripheral, didDiscoverIncludedServicesFor service: CBService, error: Error?) {
        MainActor.assumeIsolated {
            guard let l = links[p.identifier], l.discovering > 0 else { return }
            if let error { l.discoveryError = l.discoveryError ?? error }
            for s in service.includedServices ?? [] where l.visited.insert(ObjectIdentifier(s)).inserted {
                l.discovering += 2
                p.discoverIncludedServices(nil, for: s)
                p.discoverCharacteristics(nil, for: s)
            }
            discovered(l)
        }
    }

    nonisolated func peripheral(_ p: CBPeripheral, didDiscoverCharacteristicsFor service: CBService, error: Error?) {
        MainActor.assumeIsolated {
            guard let l = links[p.identifier], l.discovering > 0 else { return }
            if let error { l.discoveryError = l.discoveryError ?? error }
            for ch in service.characteristics ?? [] {
                l.discovering += 1
                p.discoverDescriptors(for: ch)
            }
            discovered(l)
        }
    }

    nonisolated func peripheral(_ p: CBPeripheral, didDiscoverDescriptorsFor ch: CBCharacteristic, error: Error?) {
        MainActor.assumeIsolated {
            guard let l = links[p.identifier], l.discovering > 0 else { return }
            if let error { l.discoveryError = l.discoveryError ?? error }
            discovered(l)
        }
    }

    /// One discovery's step done; the last answers "services" with the tree.
    private func discovered(_ l: BTLink) {
        l.discovering -= 1
        guard l.discovering == 0 else { return }
        l.visited = []
        let waiters = l.discoveryWaiters
        l.discoveryWaiters = []
        if let e = l.discoveryError {
            let err = Self.error(e, "discovery failed")
            for w in waiters { w.answer(.failure(err)) }
            return
        }
        // The services, those they include after them, each once.
        var all: [CBService] = []
        var seen = Set<ObjectIdentifier>()
        func add(_ s: CBService) {
            guard seen.insert(ObjectIdentifier(s)).inserted else { return }
            all.append(s)
            for i in s.includedServices ?? [] { add(i) }
        }
        for s in l.peripheral.services ?? [] { add(s) }
        var keep = Set<UInt32>()
        let tree: [BTServiceOut] = all.map { s in
            let h = l.handle(s)
            keep.insert(h)
            let chars: [BTCharOut] = (s.characteristics ?? []).map { ch in
                let ch_h = l.handle(ch)
                keep.insert(ch_h)
                let ds: [BTDescOut] = (ch.descriptors ?? []).map { d in
                    let dh = l.handle(d)
                    keep.insert(dh)
                    return BTDescOut(handle: dh, uuid: Self.wire(d.uuid))
                }
                return BTCharOut(handle: ch_h, uuid: Self.wire(ch.uuid), properties: Int(ch.properties.rawValue & 0xff), descriptors: ds.isEmpty ? nil : ds)
            }
            return BTServiceOut(handle: h, uuid: Self.wire(s.uuid), primary: s.isPrimary, characteristics: chars.isEmpty ? nil : chars)
        }
        l.prune(keep)
        let data = (try? encoder.encode(tree)) ?? Data("[]".utf8)
        l.tree = data
        for w in waiters { w.answer(.success(data)) }
    }

    nonisolated func peripheral(_ p: CBPeripheral, didModifyServices invalidated: [CBService]) {
        MainActor.assumeIsolated {
            guard let l = links[p.identifier], l.up else { return }
            l.tree = nil
            // Their handles go: "not found" from now on.
            for s in invalidated { l.forget(s) }
            for conn in l.conns.sorted() { push(BTEvent(type: "servicesChanged", conn: conn)) }
        }
    }

    nonisolated func peripheral(_ p: CBPeripheral, didUpdateValueFor ch: CBCharacteristic, error: Error?) {
        MainActor.assumeIsolated {
            guard let l = links[p.identifier] else { return }
            if let r = l.pop(\.reads, ch) {
                r.answer(error.map { .failure(Self.error($0, "read failed")) } ?? .success(ch.value ?? Data()))
                return
            }
            // Not a read's: a notification, or an indication.
            guard error == nil, let h = l.handles[ObjectIdentifier(ch)] else { return }
            for conn in (l.subs[ObjectIdentifier(ch)] ?? []).sorted() {
                push(BTEvent(type: "value", conn: conn, handle: h, value: ch.value ?? Data()))
            }
        }
    }

    nonisolated func peripheral(_ p: CBPeripheral, didWriteValueFor ch: CBCharacteristic, error: Error?) {
        MainActor.assumeIsolated {
            guard let l = links[p.identifier], let r = l.pop(\.writes, ch) else { return }
            r.answer(error.map { .failure(Self.error($0, "write failed")) } ?? .success(Data("{}".utf8)))
        }
    }

    nonisolated func peripheral(_ p: CBPeripheral, didUpdateValueFor d: CBDescriptor, error: Error?) {
        MainActor.assumeIsolated {
            guard let l = links[p.identifier], let r = l.pop(\.descReads, d) else { return }
            r.answer(error.map { .failure(Self.error($0, "read failed")) } ?? .success(Self.bytes(d.value)))
        }
    }

    nonisolated func peripheral(_ p: CBPeripheral, didWriteValueFor d: CBDescriptor, error: Error?) {
        MainActor.assumeIsolated {
            guard let l = links[p.identifier], let r = l.pop(\.descWrites, d) else { return }
            r.answer(error.map { .failure(Self.error($0, "write failed")) } ?? .success(Data("{}".utf8)))
        }
    }

    nonisolated func peripheralIsReady(toSendWriteWithoutResponse p: CBPeripheral) {
        MainActor.assumeIsolated {
            if let l = links[p.identifier] { sendWrites(l) }
        }
    }

    /// Hands the writes without response over while Core Bluetooth has room,
    /// each answered then.
    private func sendWrites(_ l: BTLink) {
        while !l.noResponse.isEmpty, l.peripheral.canSendWriteWithoutResponse {
            let (ch, v, r) = l.noResponse.removeFirst()
            l.peripheral.writeValue(v, for: ch, type: .withoutResponse)
            r.answer(.success(Data("{}".utf8)))
        }
    }

    private func subscribe(_ l: BTLink, _ conn: UInt32, _ ch: CBCharacteristic, _ reply: @escaping BTReply) {
        let k = ObjectIdentifier(ch)
        l.subs[k, default: []].insert(conn)
        if ch.isNotifying && (l.notifying[k] ?? []).isEmpty { ok(reply); return }
        l.notifying[k, default: []].append(BTPending(conn, reply, on: true))
        l.peripheral.setNotifyValue(true, for: ch)
    }

    private func unsubscribe(_ l: BTLink, _ conn: UInt32, _ ch: CBCharacteristic, _ reply: @escaping BTReply) {
        let k = ObjectIdentifier(ch)
        l.subs[k]?.remove(conn)
        if l.subs[k]?.isEmpty == true { l.subs[k] = nil }
        // Another Go connection still has them on.
        if l.subs[k] != nil { ok(reply); return }
        if !ch.isNotifying && (l.notifying[k] ?? []).isEmpty { ok(reply); return }
        l.notifying[k, default: []].append(BTPending(conn, reply, on: false))
        l.peripheral.setNotifyValue(false, for: ch)
    }

    nonisolated func peripheral(_ p: CBPeripheral, didUpdateNotificationStateFor ch: CBCharacteristic, error: Error?) {
        MainActor.assumeIsolated {
            guard let l = links[p.identifier] else { return }
            let k = ObjectIdentifier(ch)
            let ps = l.notifying[k] ?? []
            if let error {
                let e = Self.error(error, "subscribing failed")
                for r in ps {
                    if r.on {
                        l.subs[k]?.remove(r.conn)
                        if l.subs[k]?.isEmpty == true { l.subs[k] = nil }
                    }
                    r.answer(.failure(e))
                }
                l.notifying[k] = nil
                return
            }
            // Those that wanted it as it is now; a toggle the other way waits.
            for r in ps where r.on == ch.isNotifying { r.answer(.success(Data("{}".utf8))) }
            let rest = ps.filter { $0.reply != nil }
            l.notifying[k] = rest.isEmpty ? nil : rest
        }
    }

    nonisolated func peripheral(_ p: CBPeripheral, didReadRSSI rssi: NSNumber, error: Error?) {
        MainActor.assumeIsolated {
            guard let l = links[p.identifier], !l.rssi.isEmpty else { return }
            let r = l.rssi.removeFirst()
            if let error { r.answer(.failure(Self.error(error, "reading the RSSI failed"))); return }
            r.answer(.success((try? encoder.encode(BTRSSIOut(rssi: rssi.intValue))) ?? Data("{}".utf8)))
        }
    }

    private static func mtu(_ p: CBPeripheral) -> BTMTUOut {
        let without = p.maximumWriteValueLength(for: .withoutResponse)
        return BTMTUOut(mtu: without + 3, writeWithResponse: p.maximumWriteValueLength(for: .withResponse), writeWithoutResponse: without)
    }

    // MARK: Advertising

    /// Core Bluetooth's one advertisement, for the Go ones: the union of
    /// their services, the newest's local name (iOS advertises only these).
    private func startAdvertising(_ p: CBPeripheralManager) {
        // One start at a time: another change waits for its answer.
        if advertInFlight { advertDirty = true; return }
        if p.isAdvertising { p.stopAdvertising() }
        guard !advertOrder.isEmpty else { return }
        var services: [CBUUID] = []
        var name: String?
        for id in advertOrder {
            guard let a = adverts[id] else { continue }
            for u in (a.services ?? []).compactMap(Self.cbUUID) where !services.contains(u) { services.append(u) }
            if let n = a.localName, !n.isEmpty { name = n }
        }
        var data: [String: Any] = [:]
        if !services.isEmpty { data[CBAdvertisementDataServiceUUIDsKey] = services }
        if let name { data[CBAdvertisementDataLocalNameKey] = name }
        advertInFlight = true
        p.startAdvertising(data)
    }

    nonisolated func peripheralManagerDidStartAdvertising(_ p: CBPeripheralManager, error: Error?) {
        MainActor.assumeIsolated {
            advertInFlight = false
            if advertDirty {
                // Changed since: the advertisement as it is now decides.
                advertDirty = false
                if p.state == .poweredOn { startAdvertising(p) }
                if advertInFlight { return }
            }
            let starting = advertStarting
            advertStarting = []
            guard let error else {
                for (_, r) in starting { ok(r) }
                return
            }
            let message = error.localizedDescription
            if starting.isEmpty {
                // Those advertising already: stopped.
                for id in advertOrder { push(BTEvent(type: "advertiseStopped", error: message, id: id)) }
                adverts = [:]
                advertOrder = []
                return
            }
            // The new ones failed; the others again.
            for (id, r) in starting {
                adverts[id] = nil
                advertOrder.removeAll { $0 == id }
                fail(r, message)
            }
            if p.state == .poweredOn, !advertOrder.isEmpty { startAdvertising(p) }
        }
    }

    // MARK: GATT server

    private func serve(_ p: CBPeripheralManager, _ a: BTServeArgs, _ reply: @escaping BTReply) {
        for ls in a.services {
            for u in [ls.uuid] + (ls.characteristics ?? []).map(\.uuid) where Self.cbUUID(u) == nil {
                fail(reply, "bad args: not a UUID: \(u)")
                return
            }
        }
        var services: [CBMutableService] = []
        var chars: [UInt32: CBMutableCharacteristic] = [:]
        for ls in a.services {
            let s = CBMutableService(type: Self.cbUUID(ls.uuid)!, primary: ls.primary ?? false)
            var cs: [CBMutableCharacteristic] = []
            for lc in ls.characteristics ?? [] {
                let cu = Self.cbUUID(lc.uuid)!
                // Broadcast and extended properties aren't a local
                // characteristic's (Core Bluetooth's own).
                let props = CBCharacteristicProperties(rawValue: UInt(lc.properties & 0x7e))
                let perms = CBAttributePermissions(rawValue: UInt(lc.permissions & 0x0f))
                // Core Bluetooth answers a fixed value itself only for a
                // read-only characteristic; for another, the host does.
                let cached = lc.value != nil && props.subtracting(.read).isEmpty
                let ch = CBMutableCharacteristic(type: cu, properties: props, value: cached ? lc.value : nil, permissions: perms)
                var ds: [CBMutableDescriptor] = []
                for ld in lc.descriptors ?? [] {
                    guard let du = Self.cbUUID(ld.uuid) else { continue }
                    // The only descriptors Core Bluetooth publishes: the
                    // user description (a string) and the presentation format.
                    if du == CBUUID(string: CBUUIDCharacteristicUserDescriptionString) {
                        ds.append(CBMutableDescriptor(type: du, value: String(decoding: ld.value ?? Data(), as: UTF8.self)))
                    } else if du == CBUUID(string: CBUUIDCharacteristicFormatString), let v = ld.value {
                        ds.append(CBMutableDescriptor(type: du, value: v))
                    }
                }
                if !ds.isEmpty { ch.descriptors = ds }
                if !cached, let v = lc.value { fixed[ObjectIdentifier(ch)] = v }
                locals[ObjectIdentifier(ch)] = BTLocal(server: a.id, handle: lc.handle)
                chars[lc.handle] = ch
                cs.append(ch)
            }
            s.characteristics = cs
            services.append(s)
        }
        servers[a.id] = services
        localChars[a.id] = chars
        // One at a time: each answered by didAdd.
        serveJobs.append(BTServeJob(a.id, services, reply))
        if serveJobs.count == 1 { addNext(p) }
    }

    private func addNext(_ p: CBPeripheralManager) {
        while let job = serveJobs.first {
            if job.next < job.services.count {
                let s = job.services[job.next]
                // The app's last run published it (state restoration): this one's instead.
                for r in restoredServices where r.uuid == s.uuid { p.remove(r) }
                restoredServices.removeAll { $0.uuid == s.uuid }
                p.add(s)
                return
            }
            serveJobs.removeFirst()
            ok(job.reply)
        }
    }

    nonisolated func peripheralManager(_ p: CBPeripheralManager, didAdd service: CBService, error: Error?) {
        MainActor.assumeIsolated {
            guard let job = serveJobs.first, job.next < job.services.count else { return }
            if job.canceled || error != nil {
                serveJobs.removeFirst()
                if job.canceled {
                    // "unserve" removed those before it.
                    if error == nil { p.remove(job.services[job.next]) }
                    fail(job.reply, "canceled")
                } else {
                    // Those added go again.
                    for s in job.services[..<job.next] { p.remove(s) }
                    forgetServer(job.id)
                    job.reply(.failure(Self.error(error, "couldn't add the service")))
                }
            } else {
                job.next += 1
            }
            addNext(p)
        }
    }

    private func unserve(_ id: UInt32) {
        guard let services = servers[id] else { return }
        if let i = serveJobs.firstIndex(where: { $0.id == id }) {
            let job = serveJobs[i]
            if i == 0 {
                // Its service being added: removed once it is (didAdd).
                job.canceled = true
                for s in job.services[..<job.next] { pm?.remove(s) }
            } else {
                serveJobs.remove(at: i)
                fail(job.reply, "canceled")
            }
        } else {
            for s in services { pm?.remove(s) }
        }
        forgetServer(id)
    }

    /// Forgets a server's characteristics, its requests answered with an error.
    private func forgetServer(_ id: UInt32) {
        servers[id] = nil
        for ch in (localChars.removeValue(forKey: id) ?? [:]).values {
            let k = ObjectIdentifier(ch)
            locals[k] = nil
            fixed[k] = nil
            subscribers[k] = nil
        }
        notifyQueue.removeAll { entry in
            guard locals[ObjectIdentifier(entry.0)] == nil else { return false }
            ok(entry.3)
            return true
        }
        for (req, r) in readRequests where r.server == id {
            readRequests[req] = nil
            pm?.respond(to: r.request, withResult: .unlikelyError)
        }
        for (req, b) in writeRequests where b.servers[req] == id {
            writeRequests[req] = nil
            answer(b, req, CBATTError.Code.unlikelyError.rawValue)
        }
    }

    private func local(_ ch: CBCharacteristic) -> BTLocal? {
        if let l = locals[ObjectIdentifier(ch)] { return l }
        // Core Bluetooth's own copy: by its UUIDs.
        for (server, chars) in localChars {
            for (handle, c) in chars where c.uuid == ch.uuid && c.service?.uuid == ch.service?.uuid {
                return BTLocal(server: server, handle: handle)
            }
        }
        return nil
    }

    private func localChar(_ l: BTLocal) -> CBMutableCharacteristic? { localChars[l.server]?[l.handle] }

    nonisolated func peripheralManager(_ p: CBPeripheralManager, didReceiveRead request: CBATTRequest) {
        MainActor.assumeIsolated {
            guard let l = local(request.characteristic), let ch = localChar(l) else {
                p.respond(to: request, withResult: .attributeNotFound)
                return
            }
            if let v = fixed[ObjectIdentifier(ch)] {
                guard request.offset <= v.count else { p.respond(to: request, withResult: .invalidOffset); return }
                request.value = v.subdata(in: v.startIndex + request.offset ..< v.endIndex)
                p.respond(to: request, withResult: .success)
                return
            }
            nextRequest &+= 1
            readRequests[nextRequest] = (l.server, request)
            push(BTEvent(type: "read", handle: l.handle, server: l.server, request: nextRequest,
                         central: BTDevice(id: request.central.identifier.uuidString, name: nil),
                         offset: request.offset == 0 ? nil : request.offset))
        }
    }

    /// One "write" event for each characteristic's write in the batch (a
    /// long write's parts put together); Core Bluetooth wants one answer
    /// for the batch, to its first request, once Go has answered each:
    /// the first error, or success.
    nonisolated func peripheralManager(_ p: CBPeripheralManager, didReceiveWrite requests: [CBATTRequest]) {
        MainActor.assumeIsolated {
            guard let first = requests.first else { return }
            // Consecutive parts of one characteristic's value, together.
            var groups: [(CBATTRequest, Data)] = []
            for r in requests {
                if let last = groups.last, last.0.characteristic === r.characteristic || last.0.characteristic.uuid == r.characteristic.uuid,
                   last.0.offset + last.1.count == r.offset {
                    groups[groups.count - 1].1.append(r.value ?? Data())
                } else {
                    groups.append((r, r.value ?? Data()))
                }
            }
            let b = BTWriteBatch(first)
            var events: [BTEvent] = []
            for (r, v) in groups {
                guard let l = local(r.characteristic), let ch = localChar(l) else {
                    b.failure = b.failure ?? CBATTError.Code.attributeNotFound.rawValue
                    continue
                }
                // A characteristic that takes writes without response only
                // gets those: no one answers them.
                let response = ch.properties.contains(.write) || !ch.properties.contains(.writeWithoutResponse)
                b.respond = b.respond || response
                nextRequest &+= 1
                if response {
                    b.remaining.insert(nextRequest)
                    b.servers[nextRequest] = l.server
                    writeRequests[nextRequest] = b
                }
                events.append(BTEvent(type: "write", handle: l.handle, value: v, server: l.server, request: nextRequest,
                                      central: BTDevice(id: r.central.identifier.uuidString, name: nil),
                                      offset: r.offset == 0 ? nil : r.offset, response: response ? true : nil))
            }
            if b.failure != nil && b.remaining.isEmpty || !b.respond {
                if b.respond || b.failure != nil { p.respond(to: first, withResult: Self.result(b.failure ?? 0)) }
                b.done = true
            }
            for e in events { push(e) }
        }
    }

    private func answer(_ b: BTWriteBatch, _ request: UInt32, _ code: Int) {
        b.remaining.remove(request)
        if code != 0 && b.failure == nil { b.failure = code }
        guard b.remaining.isEmpty, !b.done else { return }
        b.done = true
        pm?.respond(to: b.first, withResult: Self.result(b.failure ?? 0))
    }

    nonisolated func peripheralManager(_ p: CBPeripheralManager, central: CBCentral, didSubscribeTo ch: CBCharacteristic) {
        MainActor.assumeIsolated { subscription(central, ch, on: true) }
    }

    nonisolated func peripheralManager(_ p: CBPeripheralManager, central: CBCentral, didUnsubscribeFrom ch: CBCharacteristic) {
        MainActor.assumeIsolated { subscription(central, ch, on: false) }
    }

    private func subscription(_ central: CBCentral, _ c: CBCharacteristic, on: Bool) {
        guard let l = local(c), let ch = localChar(l) else { return }
        let k = ObjectIdentifier(ch)
        if on { subscribers[k, default: [:]][central.identifier] = central } else { subscribers[k]?[central.identifier] = nil }
        push(BTEvent(type: "subscription", handle: l.handle, server: l.server,
                     central: BTDevice(id: central.identifier.uuidString, name: nil), on: on ? true : nil))
    }

    /// Sends the notifications queued while Core Bluetooth takes them, each
    /// answered then; the rest once it has room again.
    private func sendNotifications(_ p: CBPeripheralManager) {
        while let n = notifyQueue.first {
            guard p.updateValue(n.1, for: n.0, onSubscribedCentrals: n.2) else { return }
            notifyQueue.removeFirst()
            ok(n.3)
        }
    }

    nonisolated func peripheralManagerIsReady(toUpdateSubscribers p: CBPeripheralManager) {
        MainActor.assumeIsolated { sendNotifications(p) }
    }

    nonisolated func peripheralManagerDidUpdateState(_ p: CBPeripheralManager) {
        MainActor.assumeIsolated {
            if p.state != .poweredOn { peripheralStopped(p.state) }
            if Self.settled(p.state) {
                let ws = pmWaiters
                pmWaiters = []
                for w in ws { w() }
            }
            emitState()
        }
    }

    /// The peripheral role's state isn't on: advertising stops, and the
    /// services go (Core Bluetooth clears its database).
    private func peripheralStopped(_ s: CBManagerState) {
        let w = Self.word(s)
        for (_, r) in advertStarting { fail(r, w) }
        advertStarting = []
        advertInFlight = false
        advertDirty = false
        for id in advertOrder { push(BTEvent(type: "advertiseStopped", error: w, id: id)) }
        adverts = [:]
        advertOrder = []
        let jobs = serveJobs
        serveJobs = []
        for j in jobs where !j.canceled { fail(j.reply, w) }
        for id in servers.keys.sorted() where !jobs.contains(where: { $0.id == id }) {
            push(BTEvent(type: "serverStopped", error: w, server: id))
        }
        for id in Array(servers.keys) { forgetServer(id) }
        for (_, _, _, r) in notifyQueue { fail(r, w) }
        notifyQueue = []
        restoredServices = []
    }

    nonisolated func peripheralManager(_ p: CBPeripheralManager, willRestoreState dict: [String: Any]) {
        MainActor.assumeIsolated {
            // Published by the app's last run: replaced as the app serves
            // them again; its advertisement goes on until the app advertises.
            restoredServices = dict[CBPeripheralManagerRestoredStateServicesKey] as? [CBMutableService] ?? []
        }
    }

    // MARK: Values

    /// An error as the wire has it: the device's ATT error with its code,
    /// Core Bluetooth's own in Go's words where it has them.
    private static func error(_ e: Error?, _ fallback: String) -> Error {
        guard let e = e as NSError? else { return AsukaModuleError(fallback) }
        if e.domain == CBATTErrorDomain { return AsukaModuleError("att \(e.code): \(e.localizedDescription)") }
        if e.domain == CBErrorDomain {
            switch CBError.Code(rawValue: e.code) {
            case .connectionTimeout: return AsukaModuleError("timeout")
            case .peripheralDisconnected, .notConnected: return AsukaModuleError("disconnected")
            case .operationCancelled: return AsukaModuleError("canceled")
            default: break
            }
        }
        return AsukaModuleError(e.localizedDescription)
    }

    private static func result(_ code: Int) -> CBATTError.Code {
        code == 0 ? .success : CBATTError.Code(rawValue: code) ?? .unlikelyError
    }

    /// A descriptor's value as bytes (Core Bluetooth gives some as a number
    /// or a string).
    private static func bytes(_ v: Any?) -> Data {
        switch v {
        case let d as Data: return d
        case let s as String: return Data(s.utf8)
        case let n as NSNumber:
            let u = n.uint16Value
            return Data([UInt8(u & 0xff), UInt8(u >> 8)])
        default: return Data()
        }
    }

    /// The bytes 4 to 16 of the Bluetooth Base UUID (0000xxxx-0000-1000-8000-00805f9b34fb).
    private static let base: [UInt8] = [0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0x80, 0x5f, 0x9b, 0x34, 0xfb]

    /// A UUID as the wire has it: 128-bit, lower case, with dashes.
    static func wire(_ u: CBUUID) -> String {
        let b = [UInt8](u.data)
        let full: [UInt8]
        switch b.count {
        case 2: full = [0, 0, b[0], b[1]] + base
        case 4: full = b + base
        default: full = b
        }
        var s = ""
        for (i, x) in full.enumerated() {
            if i == 4 || i == 6 || i == 8 || i == 10 { s += "-" }
            s += String(format: "%02x", x)
        }
        return s
    }

    /// A UUID as the wire (or a platform) writes it, in Core Bluetooth's
    /// shortest form; nil if it isn't one (CBUUID(string:) would throw).
    static func cbUUID(_ str: String) -> CBUUID? {
        var s = str.trimmingCharacters(in: .whitespaces)
        if s.hasPrefix("0x") || s.hasPrefix("0X") { s.removeFirst(2) }
        if s.hasPrefix("{") && s.hasSuffix("}") { s = String(s.dropFirst().dropLast()) }
        let c = Array(s)
        if c.count == 36 {
            guard c[8] == "-", c[13] == "-", c[18] == "-", c[23] == "-" else { return nil }
            s = s.replacingOccurrences(of: "-", with: "")
        }
        switch s.count {
        case 4: s = "0000" + s
        case 8, 32: break
        default: return nil
        }
        var b: [UInt8] = []
        var hi: UInt8?
        for ch in s.utf8 {
            let v: UInt8
            switch ch {
            case 0x30...0x39: v = ch - 0x30
            case 0x61...0x66: v = ch - 0x61 + 10
            case 0x41...0x46: v = ch - 0x41 + 10
            default: return nil
            }
            if let h = hi { b.append(h << 4 | v); hi = nil } else { hi = v }
        }
        if b.count == 4 { b += base }
        if Array(b[4...]) == base {
            return CBUUID(data: b[0] == 0 && b[1] == 0 ? Data(b[2..<4]) : Data(b[0..<4]))
        }
        return CBUUID(data: Data(b))
    }
}

// MARK: The module's state

private typealias BTReply = (Result<Data, Error>) -> Void

private func ok(_ reply: BTReply) { reply(.success(Data("{}".utf8))) }

private func fail(_ reply: BTReply, _ message: String) { reply(.failure(AsukaModuleError(message))) }

@MainActor private func json<T: Encodable>(_ reply: BTReply, _ v: T) {
    reply(.success((try? JSONEncoder().encode(v)) ?? Data("{}".utf8)))
}

private func decode<T: Decodable>(_ t: T.Type, _ args: Data, _ reply: BTReply) -> T? {
    do {
        return try JSONDecoder().decode(T.self, from: args.isEmpty ? Data("{}".utf8) : args)
    } catch {
        fail(reply, "bad args: \(error)")
        return nil
    }
}

/// A call waiting on a connection, in the order Core Bluetooth answers;
/// one its Go connection left is answered by no one.
@MainActor private final class BTPending {
    let conn: UInt32
    let on: Bool // a subscription's: on or off
    var reply: BTReply?
    init(_ conn: UInt32, _ reply: @escaping BTReply, on: Bool = false) {
        self.conn = conn
        self.reply = reply
        self.on = on
    }

    func answer(_ r: Result<Data, Error>) {
        guard let f = reply else { return }
        reply = nil
        f(r)
    }
}

/// A peripheral's connection, which the Go connections to it share.
@MainActor private final class BTLink {
    let peripheral: CBPeripheral
    var conns = Set<UInt32>()
    var connecting: [UInt32: BTReply] = [:]
    var up = false
    var deferred = false // connect once the link closing before it has disconnected
    var closers: [BTReply] = []

    // Handles: the same for the same attribute while the link lasts.
    var nextHandle: UInt32 = 0
    var handles: [ObjectIdentifier: UInt32] = [:]
    var attrs: [UInt32: CBAttribute] = [:]
    var tree: Data? // "services"' answer, until they change
    var discovering = 0
    var discoveryError: Error?
    var discoveryWaiters: [BTPending] = []
    var visited = Set<ObjectIdentifier>() // included services, discovering

    var reads: [ObjectIdentifier: [BTPending]] = [:]
    var writes: [ObjectIdentifier: [BTPending]] = [:]
    var descReads: [ObjectIdentifier: [BTPending]] = [:]
    var descWrites: [ObjectIdentifier: [BTPending]] = [:]
    var notifying: [ObjectIdentifier: [BTPending]] = [:]
    var rssi: [BTPending] = []
    var noResponse: [(CBCharacteristic, Data, BTPending)] = []
    var subs: [ObjectIdentifier: Set<UInt32>] = [:] // the Go connections subscribed, by characteristic

    init(_ p: CBPeripheral) { peripheral = p }

    func handle(_ a: CBAttribute) -> UInt32 {
        let k = ObjectIdentifier(a)
        if let h = handles[k] { return h }
        nextHandle += 1
        handles[k] = nextHandle
        attrs[nextHandle] = a
        return nextHandle
    }

    func characteristic(_ k: ObjectIdentifier) -> CBCharacteristic? {
        handles[k].flatMap { attrs[$0] as? CBCharacteristic }
    }

    /// Forgets the handles of the attributes no longer there.
    func prune(_ keep: Set<UInt32>) {
        for (h, a) in attrs where !keep.contains(h) {
            attrs[h] = nil
            handles[ObjectIdentifier(a)] = nil
        }
    }

    /// Forgets a service's handles, its characteristics' and descriptors'.
    func forget(_ s: CBService) {
        var gone: [CBAttribute] = [s]
        for ch in s.characteristics ?? [] {
            gone.append(ch)
            gone.append(contentsOf: (ch.descriptors ?? []) as [CBAttribute])
        }
        for a in gone {
            if let h = handles.removeValue(forKey: ObjectIdentifier(a)) { attrs[h] = nil }
        }
    }

    /// The first call waiting on o's answer (nil: none).
    func pop(_ m: ReferenceWritableKeyPath<BTLink, [ObjectIdentifier: [BTPending]]>, _ o: AnyObject) -> BTPending? {
        let k = ObjectIdentifier(o)
        guard var l = self[keyPath: m][k], !l.isEmpty else { return nil }
        let p = l.removeFirst()
        self[keyPath: m][k] = l.isEmpty ? nil : l
        return p
    }

    /// Fails the calls waiting, a Go connection's (nil: all).
    func fail(_ conn: UInt32?, _ e: Error) {
        func f(_ p: BTPending) {
            if conn == nil || p.conn == conn { p.answer(.failure(e)) }
        }
        for m in [reads, writes, descReads, descWrites, notifying] {
            for l in m.values { l.forEach(f) }
        }
        rssi.forEach(f)
        discoveryWaiters.forEach(f)
        discoveryWaiters.removeAll { $0.reply == nil }
        noResponse.forEach { f($0.2) }
        noResponse.removeAll { $0.2.reply == nil }
    }
}

@MainActor private final class BTScan {
    let services: Set<String>? // nil: every device
    let duplicates: Bool
    var seen = Set<UUID>()
    init(services: Set<String>?, duplicates: Bool) {
        self.services = services
        self.duplicates = duplicates
    }
}

private struct BTLocal {
    let server: UInt32
    let handle: UInt32
}

@MainActor private final class BTServeJob {
    let id: UInt32
    let services: [CBMutableService]
    let reply: BTReply
    var next = 0
    var canceled = false
    init(_ id: UInt32, _ services: [CBMutableService], _ reply: @escaping BTReply) {
        self.id = id
        self.services = services
        self.reply = reply
    }
}

/// A batch of writes (didReceiveWrite), answered once.
@MainActor private final class BTWriteBatch {
    let first: CBATTRequest
    var remaining = Set<UInt32>()
    var servers: [UInt32: UInt32] = [:] // request → server
    var failure: Int?
    var respond = false // a write with response is in it
    var done = false
    init(_ first: CBATTRequest) { self.first = first }
}

// MARK: The wire (internal/bluetoothspec)

private struct BTIDArgs: Decodable { let id: UInt32 }
private struct BTConnArgs: Decodable { let conn: UInt32 }
private struct BTAttrArgs: Decodable { let conn: UInt32; let handle: UInt32 }
private struct BTScanArgs: Decodable { let id: UInt32; let services: [String]?; let duplicates: Bool? }
private struct BTKnownArgs: Decodable { let ids: [String]? }
private struct BTConnectedArgs: Decodable { let services: [String]? }
private struct BTConnectArgs: Decodable { let conn: UInt32; let device: String; let autoConnect: Bool? }
private struct BTWriteArgs: Decodable { let conn: UInt32; let handle: UInt32; let value: Data?; let response: Bool? }
private struct BTSubscribeArgs: Decodable { let conn: UInt32; let handle: UInt32; let indicate: Bool? }
private struct BTAdvertiseArgs: Decodable { let id: UInt32; let localName: String?; let services: [String]? }
private struct BTServeArgs: Decodable { let id: UInt32; let services: [BTLocalService] }
private struct BTLocalService: Decodable { let uuid: String; let primary: Bool?; let characteristics: [BTLocalCharacteristic]? }
private struct BTLocalCharacteristic: Decodable {
    let handle: UInt32
    let uuid: String
    let properties: Int
    let permissions: Int
    let value: Data?
    let descriptors: [BTLocalDescriptor]?
}
private struct BTLocalDescriptor: Decodable { let uuid: String; let value: Data? }
private struct BTRespondArgs: Decodable { let server: UInt32; let request: UInt32; let value: Data?; let error: Int? }
private struct BTNotifyArgs: Decodable { let server: UInt32; let handle: UInt32; let value: Data?; let centrals: [String]? }

private struct BTStateOut: Encodable { let state: String }
private struct BTRSSIOut: Encodable { let rssi: Int }
private struct BTMTUOut: Encodable { let mtu: Int; let writeWithResponse: Int; let writeWithoutResponse: Int }
private struct BTDevice: Encodable { let id: String; let name: String? }
private struct BTDescOut: Encodable { let handle: UInt32; let uuid: String }
private struct BTCharOut: Encodable { let handle: UInt32; let uuid: String; let properties: Int; let descriptors: [BTDescOut]? }
private struct BTServiceOut: Encodable { let handle: UInt32; let uuid: String; let primary: Bool; let characteristics: [BTCharOut]? }
private struct BTRestored: Encodable {
    var devices: [BTDevice]?
    var scanServices: [String]?
}

private struct BTAdvertisement: Encodable {
    let device: BTDevice
    let rssi: Int
    var localName: String?
    var services: [String]?
    var solicited: [String]?
    var serviceData: [String: Data]?
    var manufacturerData: [String: Data]?
    var txPower: Int?
    var connectable: Bool?
    let time: Int64
    init(device: BTDevice, rssi: Int, time: Int64) {
        self.device = device
        self.rssi = rssi
        self.time = time
    }
}

private struct BTEvent: Encodable {
    let type: String
    var state: String?
    var scan: UInt32?
    var advertisement: BTAdvertisement?
    var conn: UInt32?
    var handle: UInt32?
    var value: Data?
    var reason: String?
    var error: String?
    var code: Int?
    var id: UInt32?
    var server: UInt32?
    var request: UInt32?
    var central: BTDevice?
    var offset: Int?
    var response: Bool?
    var on: Bool?
}
#endif
