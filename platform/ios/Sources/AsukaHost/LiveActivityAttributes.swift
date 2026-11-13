#if canImport(ActivityKit) && ASUKA_LIVEACTIVITY // asuka/sdk/liveactivity (see the CLI's iosFeatures)
// The Live Activities' attributes, compiled into both the app (the host's
// module, LiveActivity.swift) and the widget extension (its template,
// Widget/LiveActivity.swift): ActivityKit matches the two by the type's
// name and its Codable shape, and a server's push-to-start names it
// ("attributes-type": "AsukaActivityAttributes").
import ActivityKit
import Foundation

@available(iOS 16.1, *)
struct AsukaActivityAttributes: ActivityAttributes {
    /// What an activity shows: asuka/sdk/liveactivity's Content on the wire,
    /// which a server's push carries as its "content-state". Each key is
    /// optional.
    struct ContentState: Codable, Hashable {
        var title = "", subtitle = "", value = "", symbol = "", tint = "", url = ""
        var progress = 0.0
        var showProgress = false
        var sinceMs: Double?
        var untilMs: Double?

        enum CodingKeys: String, CodingKey { case title, subtitle, value, symbol, tint, url, progress, showProgress, sinceMs, untilMs }

        init() {}

        init(from d: Decoder) throws {
            let c = try d.container(keyedBy: CodingKeys.self)
            title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
            subtitle = try c.decodeIfPresent(String.self, forKey: .subtitle) ?? ""
            value = try c.decodeIfPresent(String.self, forKey: .value) ?? ""
            symbol = try c.decodeIfPresent(String.self, forKey: .symbol) ?? ""
            tint = try c.decodeIfPresent(String.self, forKey: .tint) ?? ""
            url = try c.decodeIfPresent(String.self, forKey: .url) ?? ""
            progress = try c.decodeIfPresent(Double.self, forKey: .progress) ?? 0
            showProgress = try c.decodeIfPresent(Bool.self, forKey: .showProgress) ?? false
            sinceMs = try c.decodeIfPresent(Double.self, forKey: .sinceMs)
            untilMs = try c.decodeIfPresent(Double.self, forKey: .untilMs)
        }

        func encode(to e: Encoder) throws {
            var c = e.container(keyedBy: CodingKeys.self)
            if !title.isEmpty { try c.encode(title, forKey: .title) }
            if !subtitle.isEmpty { try c.encode(subtitle, forKey: .subtitle) }
            if !value.isEmpty { try c.encode(value, forKey: .value) }
            if !symbol.isEmpty { try c.encode(symbol, forKey: .symbol) }
            if !tint.isEmpty { try c.encode(tint, forKey: .tint) }
            if !url.isEmpty { try c.encode(url, forKey: .url) }
            if progress != 0 { try c.encode(progress, forKey: .progress) }
            if showProgress { try c.encode(showProgress, forKey: .showProgress) }
            try c.encodeIfPresent(sinceMs, forKey: .sinceMs)
            try c.encodeIfPresent(untilMs, forKey: .untilMs)
        }

        var since: Date? { sinceMs.map { Date(timeIntervalSince1970: $0 / 1000) } }
        var until: Date? { untilMs.map { Date(timeIntervalSince1970: $0 / 1000) } }
    }

    /// The activity's ID: asuka/sdk/liveactivity's Activity.ID (the app's, not
    /// ActivityKit's), so a server and the Go code name it alike.
    var id: String
}
#endif
