#if canImport(UIKit)
import UIKit

/// A Scroll: a UIScrollView that takes the rows of its content more than
/// a screen outside what it shows out of the view tree, as a collection
/// view does its cells, and puts them back as they come near. Core
/// Animation draws every layer in the tree that needs it, on screen or
/// not (hidden too): each label is a bitmap of its own, and a long Scroll
/// of text cost as much to update as all its rows, in time and memory.
/// A row out of the tree keeps its frame and props, so it comes back as
/// it was. While VoiceOver runs it takes nothing out: VoiceOver moves
/// through every row, scrolling to it.
final class RyUIScrollView: UIScrollView {
    weak var node: RyUINode?

    override func layoutSubviews() {
        super.layoutSubviews()
        cull()
    }

    func cull() {
        guard let content = node?.children.first, content.view.superview === self else { return }
        let all = UIAccessibility.isVoiceOverRunning
        let c = content.container
        // A screen's worth around what shows, in the content's coordinates.
        let keep = bounds.insetBy(dx: -bounds.width, dy: -bounds.height)
            .offsetBy(dx: -content.view.frame.minX, dy: -content.view.frame.minY)
        // From the last: a row comes back below the next one in the tree.
        var next: UIView?
        for row in content.children.reversed() {
            let v = row.view
            let near = all || v.frame.intersects(keep)
            if near, v.superview == nil {
                if let next { c.insertSubview(v, belowSubview: next) } else { c.addSubview(v) }
            } else if !near, v.superview === c {
                v.removeFromSuperview()
            }
            if v.superview === c { next = v }
        }
    }
}

@MainActor extension RyUIRuntime {
    /// VoiceOver started (or stopped): the Scrolls put back (or take out)
    /// their rows out of sight.
    @objc func voiceOverChanged() {
        for n in nodes.values { (n.view as? RyUIScrollView)?.cull() }
    }
}
#endif
