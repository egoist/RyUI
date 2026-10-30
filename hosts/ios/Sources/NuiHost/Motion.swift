#if canImport(UIKit)
import UIKit
#if canImport(NuiOps)
import NuiOps
#endif

/// Style.Transition: the state a view comes in from and goes out to.
struct NuiTransition {
    var fade = false
    var scale: CGFloat = 0
    var x: CGFloat = 0, y: CGFloat = 0

    init(_ b: UnsafeRawBufferPointer) {
        guard b.count >= 13 else { return }
        var r = NuiReader(b)
        fade = r.u8() != 0
        scale = CGFloat(r.f32())
        x = CGFloat(r.f32()); y = CGFloat(r.f32())
    }

    init() {}

    var none: Bool { !fade && scale == 0 && x == 0 && y == 0 }

    /// Under Reduce Motion, a dissolve in place of movement, as UIKit's
    /// own transitions do then.
    @MainActor var reduced: NuiTransition {
        guard !none, UIAccessibility.isReduceMotionEnabled else { return self }
        var t = NuiTransition()
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
@MainActor final class NuiFrames: NSObject {
    static let shared = NuiFrames()
    var link: CADisplayLink?

    @objc func tick(_ l: CADisplayLink) {
        let rt = NuiRuntime.shared
        rt.send(NuiEvents.frame(rt.rootID, l.targetTimestamp))
    }
}

@MainActor extension NuiRuntime {
    func setFrames(_ on: Bool) {
        let f = NuiFrames.shared
        if on, f.link == nil {
            let l = CADisplayLink(target: f, selector: #selector(NuiFrames.tick(_:)))
            l.preferredFrameRateRange = CAFrameRateRange(minimum: 60, maximum: 120, preferred: 120) // ProMotion's
            l.add(to: .main, forMode: .common) // while scrolling too
            f.link = l
        } else if !on {
            f.link?.invalidate()
            f.link = nil
        }
    }

    /// A view inserted during an animation, with a transition: it starts
    /// in the transition's state, and animates to its own once the batch
    /// is applied (and the rest of the change starts with it).
    func enter(_ n: NuiNode) {
        guard let t = n.transition?.reduced, !t.none, animation != nil, n.view.superview?.window != nil else { return }
        if t.fade { n.view.alpha = 0 }
        n.view.transform = n.baseTransform.concatenating(t.transform)
        entering.append((n, animation!))
    }

    /// Runs the enter animations of the batch just applied.
    func runEnters() {
        guard !entering.isEmpty else { return }
        let es = entering
        entering = []
        for (n, a) in es {
            let saved = animation
            animation = a
            let v = n.view
            animated(v) {
                v.alpha = n.opacity
                v.transform = n.baseTransform
            }
            animation = saved
        }
    }

    /// A view destroyed during an animation, with a transition: it stays
    /// until it has gone out. True if it does.
    func exit(_ n: NuiNode) -> Bool {
        guard let t = n.transition?.reduced, !t.none, let a = animation, n.view.window != nil else { return false }
        let v = n.view
        exiting.insert(ObjectIdentifier(v))
        let (duration, curve) = a
        let options: UIView.AnimationOptions = curve == NuiAnimationCurve.linear.rawValue ? .curveLinear
            : curve == NuiAnimationCurve.easeIn.rawValue ? .curveEaseIn
            : curve == NuiAnimationCurve.easeOut.rawValue ? .curveEaseOut : .curveEaseInOut
        v.isUserInteractionEnabled = false
        UIView.animate(withDuration: duration, delay: 0, options: [options, .beginFromCurrentState]) {
            if t.fade { v.alpha = 0 }
            v.transform = n.baseTransform.concatenating(t.transform)
        } completion: { [weak self] _ in
            v.removeFromSuperview()
            self?.exiting.remove(ObjectIdentifier(v))
        }
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
#endif
