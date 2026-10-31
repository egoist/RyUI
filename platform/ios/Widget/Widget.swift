// The widget extension (ryui.json's widgets): WidgetKit's, so SwiftUI's,
// the one place RyUI's iOS host uses it. It runs no Go: the app's Go code
// sets each kind's entries (ryui/widget), which the app keeps in the App
// Group's defaults (RyUIAppGroup) for this extension to show with one
// template, laid out for each size. The CLI compiles this with the app's
// kinds (Kinds.swift: a WidgetBundle of RyUIKindWidget<...>).
import SwiftUI
import WidgetKit

/// A kind of widget the app declares.
protocol RyUIWidgetKind {
    static var kind: String { get }
    static var name: String { get }
    static var summary: String { get }
    static var families: [WidgetFamily] { get }
}

/// What the app set: as ryui/widget's Entry on the wire.
struct RyUIWidgetEntry: TimelineEntry, Decodable {
    var date: Date = .now
    var title = "", value = "", body = "", caption = "", symbol = "", url = "", tint = ""
    var progress = 0.0
    var showProgress = false
    var until: Date?
    var placeholder = false

    enum CodingKeys: String, CodingKey { case title, value, body, caption, symbol, url, tint, progress, showProgress, untilMs, atMs }

    init() {}

    init(from d: Decoder) throws {
        let c = try d.container(keyedBy: CodingKeys.self)
        title = try c.decodeIfPresent(String.self, forKey: .title) ?? ""
        value = try c.decodeIfPresent(String.self, forKey: .value) ?? ""
        body = try c.decodeIfPresent(String.self, forKey: .body) ?? ""
        caption = try c.decodeIfPresent(String.self, forKey: .caption) ?? ""
        symbol = try c.decodeIfPresent(String.self, forKey: .symbol) ?? ""
        url = try c.decodeIfPresent(String.self, forKey: .url) ?? ""
        tint = try c.decodeIfPresent(String.self, forKey: .tint) ?? ""
        progress = try c.decodeIfPresent(Double.self, forKey: .progress) ?? 0
        showProgress = try c.decodeIfPresent(Bool.self, forKey: .showProgress) ?? false
        if let ms = try c.decodeIfPresent(Double.self, forKey: .untilMs) { until = Date(timeIntervalSince1970: ms / 1000) }
        if let ms = try c.decodeIfPresent(Double.self, forKey: .atMs) { date = Date(timeIntervalSince1970: ms / 1000) }
    }

    /// The gallery's and the redacted placeholder's: the widget's name.
    static func sample(_ name: String) -> RyUIWidgetEntry {
        var e = RyUIWidgetEntry()
        e.title = name
        e.value = "—"
        e.placeholder = true
        return e
    }
}

/// Reads the kind's entries from the App Group.
struct RyUIWidgetProvider: TimelineProvider {
    let kind: String
    let name: String

    func entries() -> [RyUIWidgetEntry] {
        guard let group = Bundle.main.object(forInfoDictionaryKey: "RyUIAppGroup") as? String,
              let data = UserDefaults(suiteName: group)?.data(forKey: "ryui.widget." + kind),
              let es = try? JSONDecoder().decode([RyUIWidgetEntry].self, from: data) else { return [] }
        return es
    }

    func placeholder(in context: Context) -> RyUIWidgetEntry { .sample(name) }

    func getSnapshot(in context: Context, completion: @escaping (RyUIWidgetEntry) -> Void) {
        // The gallery: what it shows now, if the app set anything.
        completion(current(entries()) ?? .sample(name))
    }

    func getTimeline(in context: Context, completion: @escaping (Timeline<RyUIWidgetEntry>) -> Void) {
        var es = entries()
        if es.isEmpty {
            completion(Timeline(entries: [.sample(name)], policy: .never))
            return
        }
        // From the one showing now (an entry dated before now shows from now).
        if let now = current(es) {
            es = [now] + es.filter { $0.date > Date.now }
            es[0].date = .now
        }
        completion(Timeline(entries: es, policy: .never)) // the app reloads it as it sets new entries
    }

    /// The entry whose time has come: the last at or before now (or the first).
    func current(_ es: [RyUIWidgetEntry]) -> RyUIWidgetEntry? {
        es.last { $0.date <= Date.now } ?? es.first
    }
}

/// One kind of widget.
struct RyUIKindWidget<K: RyUIWidgetKind>: Widget {
    var body: some WidgetConfiguration {
        StaticConfiguration(kind: K.kind, provider: RyUIWidgetProvider(kind: K.kind, name: K.name)) { e in
            RyUIWidgetView(entry: e)
        }
        .configurationDisplayName(K.name)
        .description(K.summary)
        .supportedFamilies(K.families)
    }
}

extension Color {
    /// "#rrggbb" or "#rrggbbaa".
    init?(ryuiHex s: String) {
        let h = s.hasPrefix("#") ? String(s.dropFirst()) : s
        guard h.count == 6 || h.count == 8, let v = UInt64(h, radix: 16) else { return nil }
        let rgba = h.count == 6 ? v << 8 | 0xff : v
        self.init(.sRGB, red: Double(rgba >> 24 & 0xff) / 255, green: Double(rgba >> 16 & 0xff) / 255,
                  blue: Double(rgba >> 8 & 0xff) / 255, opacity: Double(rgba & 0xff) / 255)
    }
}

/// The template: what the entry has, laid out for the family.
struct RyUIWidgetView: View {
    let entry: RyUIWidgetEntry
    @Environment(\.widgetFamily) var family

    var tint: Color {
        Color(ryuiHex: entry.tint)
            ?? (Bundle.main.object(forInfoDictionaryKey: "RyUIAccent") as? String).flatMap { Color(ryuiHex: $0) }
            ?? .accentColor
    }

    var bodyLines: Int {
        switch family {
        case .systemSmall: return entry.value.isEmpty ? 4 : 2
        case .systemMedium: return entry.value.isEmpty ? 4 : 3
        case .systemLarge: return 10
        case .systemExtraLarge: return 12
        default: return 1
        }
    }

    @ViewBuilder var countdown: some View {
        if let until = entry.until {
            Text(until, style: .timer).monospacedDigit()
        }
    }

    var body: some View {
        content
            .redacted(reason: entry.placeholder ? .placeholder : [])
            .widgetURL(URL(string: entry.url))
            .modifier(RyUIWidgetBackground())
    }

    @ViewBuilder var content: some View {
        switch family {
        case .accessoryInline:
            // One line: the symbol, then the value or the title.
            Label(entry.value.isEmpty ? entry.title : "\(entry.title) \(entry.value)", systemImage: entry.symbol.isEmpty ? "circle.fill" : entry.symbol)
        case .accessoryCircular:
            if entry.showProgress {
                Gauge(value: min(max(entry.progress, 0), 1)) {
                    if !entry.symbol.isEmpty { Image(systemName: entry.symbol) }
                } currentValueLabel: {
                    Text(entry.value).minimumScaleFactor(0.5)
                }
                .gaugeStyle(.accessoryCircularCapacity)
            } else {
                ZStack {
                    AccessoryWidgetBackground()
                    VStack(spacing: 0) {
                        if !entry.symbol.isEmpty { Image(systemName: entry.symbol).font(.caption) }
                        Text(entry.value.isEmpty ? entry.title : entry.value).font(.headline).minimumScaleFactor(0.5).lineLimit(1)
                    }
                    .padding(4)
                }
            }
        case .accessoryRectangular:
            VStack(alignment: .leading, spacing: 1) {
                HStack(spacing: 4) {
                    if !entry.symbol.isEmpty { Image(systemName: entry.symbol) }
                    Text(entry.title).font(.headline).lineLimit(1)
                }
                if !entry.value.isEmpty { Text(entry.value).lineLimit(1) }
                if entry.until != nil { countdown }
                else if !entry.body.isEmpty { Text(entry.body).lineLimit(entry.value.isEmpty ? 2 : 1) }
                if entry.showProgress { ProgressView(value: min(max(entry.progress, 0), 1)) }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        default:
            VStack(alignment: .leading, spacing: 4) {
                if !entry.title.isEmpty || !entry.symbol.isEmpty {
                    HStack(spacing: 4) {
                        if !entry.symbol.isEmpty { Image(systemName: entry.symbol) }
                        Text(entry.title).lineLimit(1)
                    }
                    .font(.caption.weight(.semibold))
                    .foregroundStyle(tint)
                }
                Spacer(minLength: 0)
                if !entry.value.isEmpty {
                    Text(entry.value)
                        .font(.system(size: family == .systemSmall ? 32 : 40, weight: .bold, design: .rounded))
                        .minimumScaleFactor(0.4)
                        .lineLimit(1)
                }
                if entry.until != nil {
                    countdown.font(.title3.weight(.semibold))
                }
                if !entry.body.isEmpty {
                    Text(entry.body).font(.subheadline).lineLimit(bodyLines)
                }
                if entry.showProgress {
                    ProgressView(value: min(max(entry.progress, 0), 1)).tint(tint)
                }
                if !entry.caption.isEmpty {
                    Text(entry.caption).font(.caption2).foregroundStyle(.secondary).lineLimit(1)
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        }
    }
}

/// The widget's background: iOS 17's container background (removable on
/// StandBy and the lock screen), or before, a padded system background.
struct RyUIWidgetBackground: ViewModifier {
    @Environment(\.widgetFamily) var family

    func body(content: Content) -> some View {
        if #available(iOS 17.0, *) {
            content.containerBackground(for: .widget) { Color(.systemBackground) }
        } else if family == .accessoryCircular || family == .accessoryRectangular || family == .accessoryInline {
            content
        } else {
            content.padding().background(Color(.systemBackground))
        }
    }
}
