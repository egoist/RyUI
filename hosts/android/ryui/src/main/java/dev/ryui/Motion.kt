package dev.ryui

import android.view.View
import android.view.ViewGroup
import java.nio.ByteBuffer

/** Style.Transition: the state a view comes in from and goes out to. */
class RyUITransition(b: ByteBuffer) {
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
class RyUIBase {
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
fun RyUIRuntime.enter(n: RyUINode) {
    val t = n.transition ?: return
    val a = animation ?: return
    val v = n.outer
    if (t.none || (v.parent as? View)?.isAttachedToWindow != true) return
    toTransition(v, n.base, t)
    entering.add(n to a)
}

private fun RyUIRuntime.toTransition(v: View, b: RyUIBase, t: RyUITransition) {
    if (t.fade) v.alpha = 0f
    val s = if (t.scale == 0f) 1f else t.scale
    v.translationX = b.tx + t.x * density
    v.translationY = b.ty + t.y * density
    v.scaleX = b.sx * s
    v.scaleY = b.sy * s
}

/** Runs the enter animations of the batch just applied. */
fun RyUIRuntime.runEnters() {
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
fun RyUIRuntime.exit(n: RyUINode): Boolean {
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
fun RyUIRuntime.animateProps(v: View, a: RyUIAnimation, vararg values: Pair<android.util.Property<View, Float>, Float>) {
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
    (v.getTag(R.id.ryui_repeating) as? HashMap<String, android.animation.Animator>)
        ?: HashMap<String, android.animation.Animator>().also { v.setTag(R.id.ryui_repeating, it) }

/** Stops v's repeating animation of prop (set without an animation), or of all. */
fun stopRepeating(v: View, prop: android.util.Property<View, Float>? = null) {
    val m = v.getTag(R.id.ryui_repeating) as? HashMap<*, *> ?: return
    if (prop == null) {
        for (o in m.values.toList()) (o as android.animation.Animator).cancel()
        m.clear()
        return
    }
    (m.remove(prop.name) as? android.animation.Animator)?.cancel()
}

/** Whether v is in a view going out: it leaves with it. */
fun RyUIRuntime.leaving(v: View): Boolean {
    if (exiting.isEmpty()) return false
    var p: Any? = v
    while (p is View) {
        if (p in exiting) return true
        p = p.parent
    }
    return false
}

// Style.Match

/** Where v is in the window (px). */
private fun windowRect(v: View): android.graphics.Rect {
    val p = IntArray(2)
    v.getLocationInWindow(p)
    return android.graphics.Rect(p[0], p[1], p[0] + v.width, p[1] + v.height)
}

/**
 * A view with a key destroyed during an animation: it goes into the
 * window's overlay, where it was, until the view coming with its key (if
 * one does) is laid out. True if it does.
 */
fun RyUIRuntime.matchOut(n: RyUINode): Boolean {
    val a = animation
    if (a == null || (matchViews.isEmpty() && n.match == null)) {
        n.match?.let { if (matchViews[it] === n) matchViews.remove(it) }
        return false
    }
    // Where every view with a key is, before the batch takes any away (one
    // may go with an ancestor, destroyed first).
    val frames = matchFrames ?: HashMap<String, android.graphics.Rect>().also { fs ->
        for ((k, m) in matchViews) if (m.outer.isAttachedToWindow) fs[k] = windowRect(m.outer)
        matchFrames = fs
    }
    val k = n.match ?: return false
    if (matchViews[k] === n) matchViews.remove(k)
    val f = frames[k] ?: return false
    val decor = activity?.window?.decorView as? ViewGroup ?: return false
    val v = n.outer
    v.animate().cancel()
    n.frameAnimator?.cancel()
    decor.overlay.add(v) // out of its parent
    v.translationX = 0f
    v.translationY = 0f
    v.scaleX = 1f
    v.scaleY = 1f
    v.rotation = 0f
    v.layout(f.left, f.top, f.right, f.bottom)
    v.isEnabled = false
    matchOut[k] = v to f
    matchAnimation = a
    // If no frames ever come.
    decor.postDelayed({ if (matchOut[k]?.first === v) { matchOut.remove(k); decor.overlay.remove(v) } }, a.millis + 1000)
    return true
}

/** A view inserted during an animation: if it has a key (or holds views that do), it's matched once laid out. */
fun RyUIRuntime.matchIn(n: RyUINode) {
    if (animation == null || (matchViews.isEmpty() && matchOut.isEmpty())) return
    matchIn.add(n)
}

/**
 * Once the batch's frames are applied: each view coming with a key grows
 * from where the one going with it was, which moves along and fades; one
 * going alone fades.
 */
fun RyUIRuntime.runMatches() {
    if (!framesApplied) {
        matchFrames = null // a props batch: the frames come next
        return
    }
    framesApplied = false
    matchFrames = null
    if (matchOut.isEmpty()) { matchIn.clear(); return }
    val a = matchAnimation ?: return
    val decor = activity?.window?.decorView as? ViewGroup
    val coming = ArrayList<RyUINode>()
    fun collect(n: RyUINode) {
        n.match?.let { if (matchOut.containsKey(it)) coming.add(n) }
        for (c in n.children) collect(c)
    }
    for (n in matchIn) collect(n)
    matchIn.clear()
    fun gone(v: View) = decor?.overlay?.remove(v)
    for (n in coming) {
        val k = n.match ?: continue
        val v = n.outer
        if (!v.isAttachedToWindow) continue
        val (old, from) = matchOut.remove(k) ?: continue
        // Where it will be: an ancestor's frame may still be on its way.
        val to = windowRect(v)
        var m: RyUINode? = n
        while (m != null) {
            if (m.targetX != Int.MIN_VALUE && m.frameAnimator?.isRunning == true) to.offset(m.targetX - m.outer.left, m.targetY - m.outer.top)
            m = m.parent
        }
        if (to.width() <= 0 || to.height() <= 0 || from.width() <= 0 || from.height() <= 0) {
            old.animate().alpha(0f).setDuration(a.millis).withEndAction { gone(old) }.start()
            continue
        }
        val b = n.base
        v.translationX = b.tx + (from.exactCenterX() - to.exactCenterX())
        v.translationY = b.ty + (from.exactCenterY() - to.exactCenterY())
        v.scaleX = b.sx * from.width() / to.width()
        v.scaleY = b.sy * from.height() / to.height()
        v.animate().translationX(b.tx).translationY(b.ty).scaleX(b.sx).scaleY(b.sy)
            .setDuration(a.millis).setInterpolator(a.interpolator()).start()
        old.animate().translationX(to.exactCenterX() - from.exactCenterX()).translationY(to.exactCenterY() - from.exactCenterY())
            .scaleX(to.width().toFloat() / from.width()).scaleY(to.height().toFloat() / from.height()).alpha(0f)
            .setDuration(a.millis).setInterpolator(a.interpolator()).withEndAction { gone(old) }.start()
    }
    for ((_, out) in matchOut) {
        val old = out.first
        old.animate().alpha(0f).setDuration(a.millis).setInterpolator(a.interpolator()).withEndAction { gone(old) }.start()
    }
    matchOut.clear()
}
