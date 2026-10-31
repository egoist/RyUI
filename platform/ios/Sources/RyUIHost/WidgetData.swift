#if canImport(UIKit) && RYUI_WIDGET // an app that calls widget.Set (see the CLI's iosFeatures)
import UIKit
import WidgetKit

/// ryui/widget's module: "set" (JSON: kind, entries) keeps the kind's
/// entries in the App Group's defaults (RyUIAppGroup), where the widget
/// extension reads them, and reloads its widgets.
enum RyUIWidgetData {
    static func methods() -> [String: RyUIModuleMethod] {
        [
            "set": { args, reply in
                guard let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any],
                      let kind = o["kind"] as? String, let entries = o["entries"] else {
                    reply(.failure(RyUIModuleError("bad args"))); return
                }
                guard let group = Bundle.main.object(forInfoDictionaryKey: "RyUIAppGroup") as? String,
                      let defaults = UserDefaults(suiteName: group) else {
                    reply(.failure(RyUIModuleError("no App Group: declare the widgets in ryui.json"))); return
                }
                defaults.set(try? JSONSerialization.data(withJSONObject: entries), forKey: "ryui.widget." + kind)
                WidgetCenter.shared.reloadTimelines(ofKind: kind)
                reply(.success(Data()))
            },
        ]
    }
}
#endif
