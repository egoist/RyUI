import CGreet
import Foundation

/// What GreeterKit adds to a greeting: the word in its resource, and its C's answer.
public enum GreeterKit {
    public static func signature() -> String {
        let url = Bundle.module.url(forResource: "word", withExtension: "txt")
        let word = url.flatMap { try? String(contentsOf: $0, encoding: .utf8) }?
            .trimmingCharacters(in: .whitespacesAndNewlines) ?? "no resource"
        return "\(word) \(cgreet_answer())"
    }
}
