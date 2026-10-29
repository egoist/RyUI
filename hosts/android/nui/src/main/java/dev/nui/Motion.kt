package dev.nui

import android.view.View
import android.view.ViewGroup
import java.nio.ByteBuffer

/** Style.Transition: the state a view comes in from and goes out to. */
class NuiTransition(b: ByteBuffer) {
    var fade = false
    var scale = 0f
    var x = 0f
    var y = 0f

    init {
        if (b.remaining() >= 13) {
            fade = b.get().toInt() != 0
            scale = b.float
            x = b.float
            y = b.float
        }
    }

    val none get() = !fade && scale == 0f && x == 0f && y == 0f
}

/** A view's own transform (Style.Transform), which transitions start from and end at. */
class NuiBase {
    var alpha = 1f
    var tx = 0f
    var ty = 0f
    var sx = 1f
    var sy = 1f
    var rot = 0f
}

/**
 * A view inserted during an animation, with a transition: it starts in
 * the transition's state, and animates to its own once the batch is
 * applied.
 */
fun NuiRuntime.enter(n: NuiNode) {
    val t = n.transition ?: return
    val a = animation ?: return
    val v = n.outer
    if (t.none || (v.parent as? View)?.isAttachedToWindow != true) return
    toTransition(v, n.base, t)
    entering.add(n to a)
}

private fun NuiRuntime.toTransition(v: View, b: NuiBase, t: NuiTransition) {
    if (t.fade) v.alpha = 0f
    val s = if (t.scale == 0f) 1f else t.scale
    v.translationX = b.tx + t.x * density
    v.translationY = b.ty + t.y * density
    v.scaleX = b.sx * s
    v.scaleY = b.sy * s
}

/** Runs the enter animations of the batch just applied. */
fun NuiRuntime.runEnters() {
    if (entering.isEmpty()) return
    val es = ArrayList(entering)
    entering.clear()
    for ((n, a) in es) {
        val b = n.base
        n.outer.animate().alpha(b.alpha).translationX(b.tx).translationY(b.ty).scaleX(b.sx).scaleY(b.sy)
            .setDuration(a.millis).setInterpolator(a.interpolator()).start()
    }
}

/**
 * A view destroyed during an animation, with a transition: it stays
 * until it has gone out. True if it does.
 */
fun NuiRuntime.exit(n: NuiNode): Boolean {
    val t = n.transition ?: return false
    val a = animation ?: return false
    val v = n.outer
    if (t.none || !v.isAttachedToWindow) return false
    exiting.add(v)
    v.isEnabled = false
    val s = if (t.scale == 0f) 1f else t.scale
    v.animate().alpha(if (t.fade) 0f else v.alpha)
        .translationX(n.base.tx + t.x * density).translationY(n.base.ty + t.y * density)
        .scaleX(n.base.sx * s).scaleY(n.base.sy * s)
        .setDuration(a.millis).setInterpolator(a.interpolator())
        .withEndAction {
            (v.parent as? ViewGroup)?.removeView(v)
            exiting.remove(v)
        }.start()
    return true
}

/**
 * Animates v's properties to values with a: a ViewPropertyAnimator, or,
 * repeating, an ObjectAnimator (which can repeat and reverse). A
 * repeating one stops when they're set again.
 */
fun NuiRuntime.animateProps(v: View, a: NuiAnimation, vararg values: Pair<android.util.Property<View, Float>, Float>) {
    for ((prop, _) in values) stopRepeating(v, prop)
    if (a.repeat == 0 && !a.autoreverses) {
        val p = v.animate().setDuration(a.millis).setInterpolator(a.interpolator())
        for ((prop, to) in values) when (prop) {
            View.ALPHA -> p.alpha(to)
            View.TRANSLATION_X -> p.translationX(to)
            View.TRANSLATION_Y -> p.translationY(to)
            View.SCALE_X -> p.scaleX(to)
            View.SCALE_Y -> p.scaleY(to)
            View.ROTATION -> p.rotation(to)
        }
        p.start()
        return
    }
    val holders = values.map { (prop, to) -> android.animation.PropertyValuesHolder.ofFloat(prop, prop.get(v), to) }
    val o = android.animation.ObjectAnimator.ofPropertyValuesHolder(v, *holders.toTypedArray())
    o.duration = a.millis
    o.interpolator = a.interpolator()
    o.repeatCount = if (a.repeat < 0) android.animation.ValueAnimator.INFINITE else a.repeat
    o.repeatMode = if (a.autoreverses) android.animation.ValueAnimator.REVERSE else android.animation.ValueAnimator.RESTART
    for ((prop, _) in values) repeatingOf(v)[prop.name] = o
    o.start()
}

/** v's repeating animators, by the property each animates. */
@Suppress("UNCHECKED_CAST")
private fun repeatingOf(v: View): HashMap<String, android.animation.Animator> =
    (v.getTag(R.id.nui_repeating) as? HashMap<String, android.animation.Animator>)
        ?: HashMap<String, android.animation.Animator>().also { v.setTag(R.id.nui_repeating, it) }

/** Stops v's repeating animation of prop (set without an animation), or of all. */
fun stopRepeating(v: View, prop: android.util.Property<View, Float>? = null) {
    val m = v.getTag(R.id.nui_repeating) as? HashMap<*, *> ?: return
    if (prop == null) {
        for (o in m.values.toList()) (o as android.animation.Animator).cancel()
        m.clear()
        return
    }
    (m.remove(prop.name) as? android.animation.Animator)?.cancel()
}

/** Whether v is in a view going out: it leaves with it. */
fun NuiRuntime.leaving(v: View): Boolean {
    if (exiting.isEmpty()) return false
    var p: Any? = v
    while (p is View) {
        if (p in exiting) return true
        p = p.parent
    }
    return false
}
