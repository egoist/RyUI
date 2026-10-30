package dev.nui

import android.animation.ValueAnimator
import android.annotation.SuppressLint
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.Path
import android.graphics.PathMeasure
import android.graphics.RectF
import android.view.View
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * A Shape: its path (the core's, in points) filled and stroked on the
 * canvas, the stroke trimmed (a PathMeasure's segment); inside an
 * Animate, the trim animates.
 */
@SuppressLint("ViewConstructor")
class NuiShape(private val rt: NuiRuntime) : View(rt.themed) {
    private val path = Path()
    private val trimmed = Path()
    private val fillPaint = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.FILL }
    private val strokePaint = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.STROKE }
    var fill = 0L
        set(v) { field = v; invalidate() }
    var stroke = 0L
        set(v) { field = v; invalidate() }
    private var start = 0f
    private var end = 1f
    private val animators = HashMap<Int, ValueAnimator>()

    init {
        importantForAccessibility = IMPORTANT_FOR_ACCESSIBILITY_NO
    }

    fun setLineWidth(points: Float) { strokePaint.strokeWidth = points * rt.density; invalidate() }

    fun setCap(cap: Int) {
        strokePaint.strokeCap = when (cap) { NuiLineCap.ROUND -> Paint.Cap.ROUND; NuiLineCap.SQUARE -> Paint.Cap.SQUARE; else -> Paint.Cap.BUTT }
        invalidate()
    }

    fun setJoin(join: Int) {
        strokePaint.strokeJoin = when (join) { NuiLineJoin.ROUND -> Paint.Join.ROUND; NuiLineJoin.BEVEL -> Paint.Join.BEVEL; else -> Paint.Join.MITER }
        invalidate()
    }

    /** Sets the trim's start (which 0) or end (1): animated in an Animate. */
    fun setTrim(which: Int, to: Float) {
        animators.remove(which)?.cancel()
        val a = rt.animation
        val from = if (which == 0) start else end
        fun apply(v: Float) { if (which == 0) start = v else end = v; invalidate() }
        if (a == null || !isAttachedToWindow) { apply(to); return }
        val va = ValueAnimator.ofFloat(from, to).apply {
            duration = a.millis
            interpolator = a.interpolator()
            addUpdateListener { apply(it.animatedValue as Float) }
        }
        animators[which] = va
        va.start()
    }

    /** The path from its wire format: u8 PathOp, then its f32 arguments, in points. */
    fun setPath(b: ByteBuffer) {
        val r = b.duplicate().order(ByteOrder.LITTLE_ENDIAN)
        val d = rt.density
        fun f() = r.float * d
        path.reset()
        while (r.remaining() > 0) {
            when (r.get().toInt()) {
                NuiPathOp.MOVE -> path.moveTo(f(), f())
                NuiPathOp.LINE -> path.lineTo(f(), f())
                NuiPathOp.QUAD -> path.quadTo(f(), f(), f(), f())
                NuiPathOp.CUBIC -> path.cubicTo(f(), f(), f(), f(), f(), f())
                NuiPathOp.CLOSE -> path.close()
                NuiPathOp.RECT -> { val x = f(); val y = f(); path.addRect(x, y, x + f(), y + f(), Path.Direction.CW) }
                NuiPathOp.ROUNDED_RECT -> {
                    val x = f(); val y = f(); val w = f(); val h = f()
                    val rad = minOf(f(), w / 2, h / 2)
                    path.addRoundRect(RectF(x, y, x + w, y + h), rad, rad, Path.Direction.CW)
                }
                NuiPathOp.ELLIPSE -> { val x = f(); val y = f(); path.addOval(RectF(x, y, x + f(), y + f()), Path.Direction.CW) }
                NuiPathOp.ARC -> {
                    val cx = f(); val cy = f(); val rad = f()
                    val s = Math.toDegrees(r.float.toDouble()).toFloat()
                    val e = Math.toDegrees(r.float.toDouble()).toFloat()
                    val cw = r.float != 0f
                    // Degrees, clockwise on the screen, from the start; going
                    // the other way, the sweep is negative.
                    var sweep = e - s
                    if (cw && sweep < 0) sweep += 360f
                    if (!cw && sweep > 0) sweep -= 360f
                    val oval = RectF(cx - rad, cy - rad, cx + rad, cy + rad)
                    if (kotlin.math.abs(sweep) >= 360f) {
                        // A whole circle: arcTo takes 360° for none, so two halves.
                        path.arcTo(oval, s, sweep / 2, false)
                        path.arcTo(oval, s + sweep / 2, sweep / 2, false)
                    } else {
                        path.arcTo(oval, s, sweep, false)
                    }
                }
                else -> break
            }
        }
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        if (fill != 0L) {
            fillPaint.color = rt.color(fill)
            canvas.drawPath(path, fillPaint)
        }
        if (stroke != 0L && strokePaint.strokeWidth > 0) {
            strokePaint.color = rt.color(stroke)
            if (start <= 0f && end >= 1f) {
                canvas.drawPath(path, strokePaint)
            } else if (end > start) {
                // Each figure's part, in proportion to its length.
                trimmed.reset()
                val m = PathMeasure(path, false)
                var total = 0f
                do { total += m.length } while (m.nextContour())
                val m2 = PathMeasure(path, false)
                val from = start * total
                val to = end * total
                var at = 0f
                do {
                    val len = m2.length
                    val a = maxOf(from - at, 0f)
                    val b = minOf(to - at, len)
                    if (b > a) m2.getSegment(a, b, trimmed, true)
                    at += len
                } while (m2.nextContour())
                canvas.drawPath(trimmed, strokePaint)
            }
        }
    }
}

/** Sets a Shape's prop; false if prop isn't one. */
fun NuiRuntime.setShapeProp(n: NuiNode, prop: Int, v: Any): Boolean {
    val s = n.view as? NuiShape ?: return false
    when (prop) {
        NuiProp.Shape.PATH -> s.setPath(v as ByteBuffer)
        NuiProp.Shape.FILL -> s.fill = v as Long
        NuiProp.Shape.STROKE -> s.stroke = v as Long
        NuiProp.Shape.LINE_WIDTH -> s.setLineWidth((v as Double).toFloat())
        NuiProp.Shape.LINE_CAP -> s.setCap(v as Int)
        NuiProp.Shape.LINE_JOIN -> s.setJoin(v as Int)
        NuiProp.Shape.TRIM_START -> s.setTrim(0, (v as Double).toFloat())
        NuiProp.Shape.TRIM_END -> s.setTrim(1, (v as Double).toFloat())
        else -> return false
    }
    return true
}
