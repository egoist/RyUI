#if canImport(UIKit)
import UIKit
#if canImport(RyUIOps)
import RyUIOps
#endif

/// Style.Transition: the state a view comes in from and goes out to.
struct RyUITransition {
    var fade = false
    var scale: CGFloat = 0
    var x: CGFloat = 0, y: CGFloat = 0

    init(_ b: UnsafeRawBufferPointer) {
        guard b.count >= 13 else { return }
        var r = RyUIReader(b)
        fade = r.u8() != 0
        scale = CGFloat(r.f32())
        x = CGFloat(r.f32()); y = CGFloat(r.f32())
    }

    init() {}

    var none: Bool { !fade && scale == 0 && x == 0 && y == 0 }

    /// Under Reduce Motion, a dissolve in place of movement, as UIKit's
    /// own transitions do then.
    @MainActor var reduced: RyUITransition {
        guard !none, UIAccessibility.isReduceMotionEnabled else { return self }
        var t = RyUITransition()
        t.fade = true
        return t
    }

    /// The transform it adds to the view's own.
    var transform: CGAffineTransform {
        let s = scale == 0 ? 1 : scale
        return CGAffineTransform(translationX: x, y: y).scaledBy(x: s, y: s)
    }
}

/// Sends Frame at each vsync, while the core has OnFrame callbacks.
@MainActor final class RyUIFrames: NSObject {
    static let shared = RyUIFrames()
    var link: CADisplayLink?

    @objc func tick(_ l: CADisplayLink) {
        let rt = RyUIRuntime.shared
        rt.send(RyUIEvents.frame(rt.rootID, l.targetTimestamp))
    }
}

@MainActor extension RyUIRuntime {
    func setFrames(_ on: Bool) {
        let f = RyUIFrames.shared
        if on, f.link == nil {
            let l = CADisplayLink(target: f, selector: #selector(RyUIFrames.tick(_:)))
            l.preferredFrameRateRange = CAFrameRateRange(minimum: 60, maximum: 120, preferred: 120) // ProMotion's
            l.add(to: .main, forMode: .common) // while scrolling too
            f.link = l
        } else if !on {
            f.link?.invalidate()
            f.link = nil
        }
    }

    /// Animates change on a's duration and curve: a spring is UIKit's.
    func animate(_ a: (Double, Int32), options: UIView.AnimationOptions, _ change: @escaping () -> Void,
                 completion: ((Bool) -> Void)? = nil) {
        let (duration, curve) = a
        let c: UIView.AnimationOptions
        switch RyUIAnimationCurve(rawValue: curve) {
        case .spring:
            UIView.animate(withDuration: duration, delay: 0, usingSpringWithDamping: 0.85, initialSpringVelocity: 0,
                           options: options, animations: change, completion: completion)
            return
        case .linear: c = .curveLinear
        case .easeIn: c = .curveEaseIn
        case .easeOut: c = .curveEaseOut
        default: c = .curveEaseInOut
        }
        UIView.animate(withDuration: duration, delay: 0, options: options.union(c), animations: change, completion: completion)
    }

    /// A view inserted during an animation, with a transition: it starts
    /// in the transition's state, and animates to its own once the batch
    /// is applied (and the rest of the change starts with it).
    func enter(_ n: RyUINode) {
        guard let t = n.transition?.reduced, !t.none, animation != nil, n.view.superview?.window != nil else { return }
        if t.fade { n.view.alpha = 0 }
        n.view.transform = n.baseTransform.concatenating(t.transform)
        entering.append((n, animation!))
    }

    /// Runs the enter animations of the batch just applied, at once: a
    /// view made in it has no frame yet (its first comes in the batch
    /// after), and its transition is an alpha and a transform, which
    /// don't wait for its size.
    func runEnters() {
        guard !entering.isEmpty else { return }
        let es = entering
        entering = []
        for (n, a) in es {
            let v = n.view
            animate(a, options: [.beginFromCurrentState, .allowUserInteraction]) {
                v.alpha = n.opacity
                v.transform = n.baseTransform
            }
        }
    }

    /// A view destroyed during an animation, with a transition: it stays
    /// until it has gone out. True if it does.
    func exit(_ n: RyUINode) -> Bool {
        guard let t = n.transition?.reduced, !t.none, let a = animation, n.view.window != nil else { return false }
        let v = n.view
        exiting.insert(ObjectIdentifier(v))
        v.isUserInteractionEnabled = false
        animate(a, options: .beginFromCurrentState, {
            if t.fade { v.alpha = 0 }
            v.transform = n.baseTransform.concatenating(t.transform)
        }, completion: { [weak self] _ in
            v.removeFromSuperview()
            self?.exiting.remove(ObjectIdentifier(v))
        })
        return true
    }

    /// Whether v is in a view going out: it leaves with it.
    func leaving(_ v: UIView) -> Bool {
        guard !exiting.isEmpty else { return false }
        var p: UIView? = v
        while let x = p {
            if exiting.contains(ObjectIdentifier(x)) { return true }
            p = x.superview
        }
        return false
    }
}
// MARK: Style.Match

@MainActor extension RyUIRuntime {
    /// A view with a key destroyed during an animation: it goes into the
    /// window, where it was, until the view coming with its key (if one
    /// does) is laid out. True if it does.
    func matchOut(_ n: RyUINode) -> Bool {
        guard let a = animation, !UIAccessibility.isReduceMotionEnabled, !matchViews.isEmpty || n.match != nil else {
            if let k = n.match, matchViews[k] === n { matchViews[k] = nil }
            return false
        }
        // Where every view with a key is, before the batch takes any away
        // (one may go with an ancestor, destroyed first).
        if matchFrames == nil {
            var fs: [String: CGRect] = [:]
            for (k, m) in matchViews {
                guard let w = m.view.window else { continue }
                fs[k] = m.view.convert(m.view.bounds, to: nil)
                matchWindow = w
            }
            matchFrames = fs
        }
        guard let k = n.match else { return false }
        if matchViews[k] === n { matchViews[k] = nil }
        guard let f = matchFrames?[k], let w = matchWindow else { return false }
        let v = n.view
        n.parent?.remove(n)
        v.removeFromSuperview()
        v.transform = .identity
        v.frame = f
        v.isUserInteractionEnabled = false
        w.addSubview(v)
        matchOut[k] = (v, f)
        matchAnimation = a
        finish(v, after: a.0 + 1) // if no frames ever come
        return true
    }

    /// A view inserted during an animation: if it has a key (or holds
    /// views that do), it's matched once laid out.
    func matchIn(_ n: RyUINode) {
        guard animation != nil, !matchViews.isEmpty || !matchOut.isEmpty else { return }
        matchIn.append(n)
    }

    /// Once the batch's frames are applied: each view coming with a key
    /// grows from where the one going with it was, which moves along and
    /// fades; one going alone fades.
    func runMatches() {
        if !framesApplied {
            matchFrames = nil // a props batch: the frames come next
            return
        }
        framesApplied = false
        matchFrames = nil
        guard !matchOut.isEmpty else { matchIn = []; return }
        let a = matchAnimation ?? (0.35, RyUIAnimationCurve.easeInOut.rawValue)
        var coming: [RyUINode] = []
        func collect(_ n: RyUINode) {
            if let k = n.match, matchOut[k] != nil { coming.append(n) }
            for c in n.children { collect(c) }
        }
        for n in matchIn { collect(n) }
        matchIn = []
        let saved = animation
        animation = a
        for n in coming {
            guard let k = n.match, let out = matchOut.removeValue(forKey: k), n.view.window != nil else { continue }
            let v = n.view
            let to = v.convert(v.bounds, to: nil)
            guard to.width > 0, to.height > 0, out.frame.width > 0, out.frame.height > 0 else { continue }
            let from = out.frame
            v.transform = n.baseTransform.concatenating(CGAffineTransform(translationX: from.midX - to.midX, y: from.midY - to.midY)
                .scaledBy(x: from.width / to.width, y: from.height / to.height))
            let old = out.view
            animated(v) { v.transform = n.baseTransform }
            animated(old) {
                old.center = CGPoint(x: to.midX, y: to.midY)
                old.transform = CGAffineTransform(scaleX: to.width / from.width, y: to.height / from.height)
                old.alpha = 0
            }
            finish(old, after: a.0)
        }
        for (_, out) in matchOut {
            let old = out.view
            animated(old) { old.alpha = 0 }
            finish(old, after: a.0)
        }
        matchOut = [:]
        animation = saved
    }

    private func finish(_ v: UIView, after d: Double) {
        DispatchQueue.main.asyncAfter(deadline: .now() + d) { v.removeFromSuperview() }
    }
}
#endif
