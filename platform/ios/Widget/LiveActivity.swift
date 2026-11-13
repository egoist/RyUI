#if ASUKA_LIVEACTIVITY // an app that calls asuka/sdk/liveactivity (the CLI builds the extension with it)
// The Live Activities' template (asuka/sdk/liveactivity): the lock screen's
// (and an iPhone without a Dynamic Island's banner), and the Dynamic
// Island's compact, minimal and expanded views, each laying out what the
// content has. The attributes are the app's (AsukaActivityAttributes,
// Sources/AsukaHost/LiveActivityAttributes.swift, compiled into both).
import ActivityKit
import SwiftUI
import WidgetKit

/// The bundle Kinds.swift adds, with the activity where iOS has them.
struct AsukaLiveActivityBundle: WidgetBundle {
    @WidgetBundleBuilder var body: some Widget {
        if #available(iOS 16.1, *) {
            AsukaLiveActivityWidget()
        }
    }
}

@available(iOS 16.1, *)
struct AsukaLiveActivityWidget: Widget {
    var body: some WidgetConfiguration {
        ActivityConfiguration(for: AsukaActivityAttributes.self) { context in
            AsukaActivityLockScreen(state: context.state)
                .widgetURL(URL(string: context.state.url))
        } dynamicIsland: { context in
            let s = context.state
            return DynamicIsland {
                DynamicIslandExpandedRegion(.leading) {
                    if !s.symbol.isEmpty {
                        Image(systemName: s.symbol).font(.title2).foregroundStyle(s.tintColor)
                            .frame(maxHeight: .infinity)
                    }
                }
                DynamicIslandExpandedRegion(.trailing) {
                    AsukaActivityFigure(state: s, large: true)
                        .frame(maxHeight: .infinity)
                }
                DynamicIslandExpandedRegion(.center) {
                    VStack(alignment: .leading, spacing: 2) {
                        if !s.title.isEmpty { Text(s.title).font(.headline).lineLimit(1) }
                        if !s.subtitle.isEmpty { Text(s.subtitle).font(.subheadline).foregroundStyle(.secondary).lineLimit(2) }
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                DynamicIslandExpandedRegion(.bottom) {
                    if s.showProgress { AsukaActivityBar(state: s) }
                }
            } compactLeading: {
                if !s.symbol.isEmpty {
                    Image(systemName: s.symbol).foregroundStyle(s.tintColor)
                } else {
                    Text(s.title).lineLimit(1).frame(maxWidth: 60)
                }
            } compactTrailing: {
                AsukaActivityFigure(state: s, large: false)
            } minimal: {
                if s.showProgress && s.since == nil {
                    ProgressView(value: s.clampedProgress).progressViewStyle(.circular).tint(s.tintColor)
                } else if !s.symbol.isEmpty {
                    Image(systemName: s.symbol).foregroundStyle(s.tintColor)
                } else {
                    AsukaActivityFigure(state: s, large: false)
                }
            }
            .widgetURL(URL(string: s.url))
            .keylineTint(s.tintColor)
        }
    }
}

@available(iOS 16.1, *)
extension AsukaActivityAttributes.ContentState {
    /// The tint, or the app's accent (AsukaAccent), or the system's.
    var tintColor: Color {
        Color(asukaHex: tint)
            ?? (Bundle.main.object(forInfoDictionaryKey: "AsukaAccent") as? String).flatMap { Color(asukaHex: $0) }
            ?? .accentColor
    }

    var clampedProgress: Double { min(max(progress, 0), 1) }
}

/// The value, or else the countdown: large at the side of the lock
/// screen's and the expanded island's, small in the compact island.
@available(iOS 16.1, *)
struct AsukaActivityFigure: View {
    let state: AsukaActivityAttributes.ContentState
    let large: Bool

    var body: some View {
        if !state.value.isEmpty {
            Text(state.value)
                .font(large ? .system(.title2, design: .rounded).weight(.semibold) : .system(.body, design: .rounded).weight(.semibold))
                .foregroundStyle(state.tintColor)
                .lineLimit(1)
                .minimumScaleFactor(0.5)
                .frame(maxWidth: large ? 120 : 64, alignment: .trailing)
        } else if let until = state.until {
            // Down to it, then up from it.
            Text(until, style: .timer)
                .font(large ? .system(.title2, design: .rounded).weight(.semibold) : .system(.body, design: .rounded).weight(.semibold))
                .monospacedDigit()
                .multilineTextAlignment(.trailing)
                .foregroundStyle(state.tintColor)
                .frame(maxWidth: large ? 120 : 52, alignment: .trailing)
        } else if state.showProgress && state.since == nil {
            ProgressView(value: state.clampedProgress).progressViewStyle(.circular).tint(state.tintColor)
        }
    }
}

/// The progress bar: at the progress, or filling from since to until by
/// itself.
@available(iOS 16.1, *)
struct AsukaActivityBar: View {
    let state: AsukaActivityAttributes.ContentState

    var body: some View {
        if let since = state.since, let until = state.until, since < until {
            ProgressView(timerInterval: since...until, countsDown: false) {
                EmptyView()
            } currentValueLabel: {
                EmptyView()
            }
            .tint(state.tintColor)
        } else {
            ProgressView(value: state.clampedProgress).tint(state.tintColor)
        }
    }
}

/// The lock screen's: the symbol, the title and the subtitle, the value
/// (or the countdown) at the side; the bar under them.
@available(iOS 16.1, *)
struct AsukaActivityLockScreen: View {
    let state: AsukaActivityAttributes.ContentState

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            HStack(alignment: .center, spacing: 12) {
                if !state.symbol.isEmpty {
                    Image(systemName: state.symbol).font(.title2).foregroundStyle(state.tintColor)
                }
                VStack(alignment: .leading, spacing: 2) {
                    if !state.title.isEmpty { Text(state.title).font(.headline).lineLimit(1) }
                    if !state.subtitle.isEmpty { Text(state.subtitle).font(.subheadline).foregroundStyle(.secondary).lineLimit(2) }
                    // The countdown under the title, when the value has the side.
                    if !state.value.isEmpty, let until = state.until {
                        Text(until, style: .timer).font(.subheadline).monospacedDigit().foregroundStyle(.secondary)
                    }
                }
                Spacer(minLength: 0)
                AsukaActivityFigure(state: state, large: true)
            }
            if state.showProgress { AsukaActivityBar(state: state) }
        }
        .padding(16)
    }
}
#endif
