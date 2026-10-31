#if canImport(UIKit) && RYUI_CALENDAR // an app that calls ryui/calendar (see the CLI's iosFeatures)
import UIKit
import EventKit
import EventKitUI

/// ryui/calendar's module: "request" (u8 access: not asked, denied, write
/// only, full), "calendars" and "events" (JSON: from, to in ms,
/// calendars) answer JSON; "add" (an event's JSON) shows the event editor
/// and answers u8 saved.
@MainActor final class RyUICalendar: NSObject, EKEventEditViewDelegate {
    static let shared = RyUICalendar()
    private let store = EKEventStore()
    private var adding: ((Result<Data, Error>) -> Void)?

    static func access() -> UInt8 {
        let s = EKEventStore.authorizationStatus(for: .event)
        if #available(iOS 17.0, *) {
            switch s {
            case .fullAccess: return 3
            case .writeOnly: return 2
            case .notDetermined: return 0
            default: return 1
            }
        }
        switch s {
        case .authorized: return 3
        case .notDetermined: return 0
        default: return 1
        }
    }

    static func hex(_ c: CGColor?) -> String {
        guard let c = c?.converted(to: CGColorSpace(name: CGColorSpace.sRGB)!, intent: .defaultIntent, options: nil),
              let p = c.components, p.count >= 3 else { return "" }
        return String(format: "#%02x%02x%02x", Int(p[0] * 255), Int(p[1] * 255), Int(p[2] * 255))
    }

    func methods(_ rt: RyUIRuntime) -> [String: RyUIModuleMethod] {
        [
            "request": { [unowned self] _, reply in
                guard Self.access() != 3 else { reply(.success(Data([3]))); return }
                let done: (Bool, Error?) -> Void = { _, _ in DispatchQueue.main.async { reply(.success(Data([Self.access()]))) } }
                if #available(iOS 17.0, *) { self.store.requestFullAccessToEvents(completion: done) }
                else { self.store.requestAccess(to: .event, completion: done) }
            },
            "calendars": { [unowned self] _, reply in
                guard Self.access() == 3 else { reply(.failure(RyUIModuleError("not allowed"))); return }
                let cs = self.store.calendars(for: .event).map { c -> [String: Any] in
                    ["id": c.calendarIdentifier, "title": c.title, "color": Self.hex(c.cgColor),
                     "source": c.source?.title ?? "", "writable": c.allowsContentModifications]
                }
                reply(.success((try? JSONSerialization.data(withJSONObject: cs)) ?? Data("[]".utf8)))
            },
            "events": { [unowned self] args, reply in
                guard Self.access() == 3 else { reply(.failure(RyUIModuleError("not allowed"))); return }
                let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] ?? [:]
                let from = Date(timeIntervalSince1970: (o["from"] as? Double ?? 0) / 1000)
                let to = Date(timeIntervalSince1970: (o["to"] as? Double ?? 0) / 1000)
                let ids = Set(o["calendars"] as? [String] ?? [])
                let cals = ids.isEmpty ? nil : self.store.calendars(for: .event).filter { ids.contains($0.calendarIdentifier) }
                let store = self.store
                DispatchQueue.global(qos: .userInitiated).async {
                    // EventKit's predicate spans at most four years: in steps.
                    var out: [[String: Any]] = []
                    var start = from
                    while start < to {
                        let end = min(to, Calendar.current.date(byAdding: .year, value: 4, to: start) ?? to)
                        for e in store.events(matching: store.predicateForEvents(withStart: start, end: end, calendars: cals)) {
                            // An event over a step's edge comes in both: once.
                            if e.startDate < start && start != from { continue }
                            out.append(Self.json(e))
                        }
                        start = end
                    }
                    out.sort { ($0["startMs"] as? Int64 ?? 0) < ($1["startMs"] as? Int64 ?? 0) }
                    reply(Result { try JSONSerialization.data(withJSONObject: out) })
                }
            },
            "add": { [unowned self, weak rt] args, reply in
                guard let top = rt?.topController else { reply(.failure(RyUIModuleError("no screen"))); return }
                let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] ?? [:]
                let present = {
                    let e = EKEvent(eventStore: self.store)
                    e.title = o["title"] as? String
                    e.location = (o["location"] as? String).flatMap { $0.isEmpty ? nil : $0 }
                    e.notes = (o["notes"] as? String).flatMap { $0.isEmpty ? nil : $0 }
                    e.url = (o["url"] as? String).flatMap { $0.isEmpty ? nil : URL(string: $0) }
                    let all = o["allDay"] as? Bool ?? false
                    e.isAllDay = all
                    var start = Date(timeIntervalSince1970: (o["startMs"] as? Double ?? 0) / 1000)
                    var end = Date(timeIntervalSince1970: (o["endMs"] as? Double ?? 0) / 1000)
                    if all { // days in UTC, as the Go side sends them: the same days here
                        var utc = Calendar(identifier: .gregorian)
                        utc.timeZone = TimeZone(identifier: "UTC")!
                        start = Calendar.current.date(from: utc.dateComponents([.year, .month, .day], from: start)) ?? start
                        end = Calendar.current.date(from: utc.dateComponents([.year, .month, .day], from: end)) ?? end
                    }
                    if start.timeIntervalSince1970 > 0 {
                        e.startDate = start
                        if all { // the end day is exclusive here, inclusive in EventKit
                            e.endDate = end > start ? end.addingTimeInterval(-1) : start
                        } else {
                            e.endDate = end > start ? end : start.addingTimeInterval(3600)
                        }
                    }
                    if let id = o["calendarId"] as? String, !id.isEmpty, let c = self.store.calendar(withIdentifier: id) { e.calendar = c }
                    let vc = EKEventEditViewController()
                    vc.eventStore = self.store
                    vc.event = e
                    vc.editViewDelegate = self
                    self.adding = reply
                    top.present(vc, animated: true)
                }
                // iOS 17's editor runs outside the app: no permission. Before, it needs access.
                if #available(iOS 17.0, *) { present() }
                else {
                    self.store.requestAccess(to: .event) { ok, _ in
                        DispatchQueue.main.async { ok ? present() : reply(.failure(RyUIModuleError("not allowed"))) }
                    }
                }
            },
        ]
    }

    nonisolated static func json(_ e: EKEvent) -> [String: Any] {
        // All-day: its days at UTC midnight, as Android keeps them.
        func ms(_ d: Date) -> Int64 {
            guard e.isAllDay else { return Int64(d.timeIntervalSince1970 * 1000) }
            var utc = Calendar(identifier: .gregorian)
            utc.timeZone = TimeZone(identifier: "UTC")!
            let day = utc.date(from: Calendar.current.dateComponents([.year, .month, .day], from: d)) ?? d
            return Int64(day.timeIntervalSince1970 * 1000)
        }
        // EventKit's all-day end is the last day's end: the next day's start, as Android's.
        let end: Date = e.isAllDay ? (Calendar.current.date(byAdding: .day, value: 1, to: Calendar.current.startOfDay(for: e.endDate)) ?? e.endDate) : e.endDate
        return [
            "id": e.eventIdentifier ?? "", "calendarId": e.calendar?.calendarIdentifier ?? "",
            "title": e.title ?? "", "location": e.location ?? "", "notes": e.notes ?? "", "url": e.url?.absoluteString ?? "",
            "startMs": ms(e.startDate), "endMs": ms(end),
            "allDay": e.isAllDay,
        ]
    }

    nonisolated func eventEditViewController(_ vc: EKEventEditViewController, didCompleteWith action: EKEventEditViewAction) {
        MainActor.assumeIsolated {
            vc.dismiss(animated: true)
            adding?(.success(Data([action == .saved ? 1 : 0])))
            adding = nil
        }
    }
}
#endif
