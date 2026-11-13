#if canImport(UIKit) && ASUKA_WIDGET // an app that calls widget.Set (see the CLI's iosFeatures)
import UIKit
import WidgetKit

/// asuka/sdk/widget's module: "set" (JSON: kind, entries) keeps the kind's
/// entries in the App Group's defaults (AsukaAppGroup), where the widget
/// extension reads them, and reloads its widgets.
enum AsukaWidgetData {
    static func methods() -> [String: AsukaModuleMethod] {
        [
            "set": { args, reply in
                guard let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any],
                      let kind = o["kind"] as? String, let entries = o["entries"] else {
                    reply(.failure(AsukaModuleError("bad args"))); return
                }
                guard let group = Bundle.main.object(forInfoDictionaryKey: "AsukaAppGroup") as? String,
                      let defaults = UserDefaults(suiteName: group) else {
                    reply(.failure(AsukaModuleError("no App Group: declare the widgets in asuka.json"))); return
                }
                defaults.set(try? JSONSerialization.data(withJSONObject: entries), forKey: "asuka.widget." + kind)
                WidgetCenter.shared.reloadTimelines(ofKind: kind)
                reply(.success(Data()))
            },
        ]
    }
}
#endif
