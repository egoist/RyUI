package dev.asuka

import android.annotation.SuppressLint
import android.graphics.Canvas
import android.graphics.DashPathEffect
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.Path
import android.graphics.Shader
import android.graphics.Typeface
import android.view.View
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * A Canvas: its display list (ui.Graphics's commands, in points), replayed
 * on Android's Canvas in onDraw, in pixels. Semantic colors resolve in the
 * theme as it draws. Paths and paints are reused from draw to draw.
 */
@SuppressLint("ViewConstructor")
class AsukaCanvas(private val rt: AsukaRuntime) : View(rt.themed) {
    private var commands: ByteBuffer? = null
    private val fill = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.FILL }
    private val stroke = Paint(Paint.ANTI_ALIAS_FLAG).apply { style = Paint.Style.STROKE }
    private val text = Paint(Paint.ANTI_ALIAS_FLAG)
    private val paths = ArrayList<Path>()
    private val alphas = ArrayList<Float>() // the opacity each Save kept
    private val rules = ArrayList<Boolean>() // and the fill rule (even-odd)
    private val matrix = android.graphics.Matrix()
    private val values = FloatArray(9)

    init {
        importantForAccessibility = IMPORTANT_FOR_ACCESSIBILITY_NO
    }

    fun setCommands(b: ByteBuffer) {
        val copy = ByteBuffer.allocate(b.remaining()).order(ByteOrder.LITTLE_ENDIAN)
        copy.put(b.duplicate())
        copy.flip()
        commands = copy
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        val r = commands?.duplicate()?.order(ByteOrder.LITTLE_ENDIAN) ?: return
        val d = rt.density
        var used = 0
        var alpha = 1f
        var evenOdd = false
        alphas.clear()
        rules.clear()
        fun f() = r.float
        fun path(): Path {
            val n = r.int
            if (used == paths.size) paths.add(Path())
            val p = paths[used++]
            p.reset()
            val sub = r.slice().order(ByteOrder.LITTLE_ENDIAN)
            sub.limit(minOf(n, sub.remaining()))
            r.position(r.position() + n)
            readPath(sub, p, d)
            p.fillType = if (evenOdd) Path.FillType.EVEN_ODD else Path.FillType.WINDING
            return p
        }
        fun color(): Int {
            val c = rt.color(r.long)
            val a = Math.round(((c ushr 24) and 0xff) * alpha)
            return (c and 0x00ffffff) or (a shl 24)
        }
        fun stops(): Pair<IntArray, FloatArray> {
            val n = r.int
            val colors = IntArray(n)
            val stops = FloatArray(n)
            for (i in 0 until n) { colors[i] = color(); stops[i] = f() }
            return colors to stops
        }
        /** A radial gradient from its focus out to its circle (points read: center, radius, focus), in pixels. */
        fun radial(colors: IntArray, stops: FloatArray): Shader? {
            val cx = f() * d; val cy = f() * d; val rad = f() * d; val fx = f() * d; val fy = f() * d
            if (colors.size < 2 || rad <= 0f) return null
            val longs = LongArray(colors.size) { android.graphics.Color.pack(colors[it]) }
            return android.graphics.RadialGradient(fx, fy, 0f, cx, cy, rad, longs, stops, Shader.TileMode.CLAMP)
        }
        fun strokeStyle(p: Paint) {
            p.strokeWidth = f() * d
            p.strokeCap = when (r.get().toInt()) { 1 -> Paint.Cap.ROUND; 2 -> Paint.Cap.SQUARE; else -> Paint.Cap.BUTT }
            p.strokeJoin = when (r.get().toInt()) { 1 -> Paint.Join.ROUND; 2 -> Paint.Join.BEVEL; else -> Paint.Join.MITER }
            val n = r.int
            val phase = f() * d
            if (n > 0) {
                val dashes = FloatArray(if (n % 2 == 0) n else n * 2)
                for (i in 0 until n) dashes[i] = f() * d
                if (n % 2 != 0) for (i in 0 until n) dashes[n + i] = dashes[i] // Android wants pairs
                p.pathEffect = DashPathEffect(dashes, phase)
            } else p.pathEffect = null
        }
        while (r.remaining() > 0) {
            when (r.get().toInt()) {
                AsukaCanvasOp.SAVE -> { canvas.save(); alphas.add(alpha); rules.add(evenOdd) }
                AsukaCanvasOp.RESTORE -> {
                    if (alphas.isNotEmpty()) {
                        canvas.restore()
                        alpha = alphas.removeAt(alphas.size - 1)
                        evenOdd = rules.removeAt(rules.size - 1)
                    }
                }
                AsukaCanvasOp.TRANSFORM -> {
                    // In points; the canvas is in pixels: the translation scales.
                    val a = f(); val b = f(); val c = f(); val dd = f(); val tx = f() * d; val ty = f() * d
                    values[0] = a; values[1] = c; values[2] = tx
                    values[3] = b; values[4] = dd; values[5] = ty
                    values[6] = 0f; values[7] = 0f; values[8] = 1f
                    matrix.setValues(values)
                    canvas.concat(matrix)
                }
                AsukaCanvasOp.FILL_RULE -> evenOdd = r.get().toInt() == 1
                AsukaCanvasOp.RADIAL_GRADIENT -> {
                    val (colors, st) = stops()
                    fill.shader = radial(colors, st)
                    if (colors.size == 1) fill.color = colors[0] else fill.color = android.graphics.Color.BLACK
                    canvas.drawPath(path(), fill)
                    fill.shader = null
                }
                AsukaCanvasOp.STROKE_GRADIENT -> {
                    stroke.color = android.graphics.Color.BLACK
                    strokeStyle(stroke)
                    val kind = r.get().toInt()
                    val (colors, st) = stops()
                    stroke.shader = if (kind == 1) radial(colors, st) else {
                        val x0 = f() * d; val y0 = f() * d; val x1 = f() * d; val y1 = f() * d
                        if (colors.size >= 2) LinearGradient(x0, y0, x1, y1, colors, st, Shader.TileMode.CLAMP) else null
                    }
                    if (colors.size == 1) stroke.color = colors[0]
                    canvas.drawPath(path(), stroke)
                    stroke.shader = null
                }
                AsukaCanvasOp.TRANSLATE -> canvas.translate(f() * d, f() * d)
                AsukaCanvasOp.SCALE -> canvas.scale(f(), f())
                AsukaCanvasOp.ROTATE -> canvas.rotate(f())
                AsukaCanvasOp.ALPHA -> alpha *= f()
                AsukaCanvasOp.CLIP -> canvas.clipPath(path())
                AsukaCanvasOp.FILL -> {
                    fill.shader = null
                    fill.color = color()
                    canvas.drawPath(path(), fill)
                }
                AsukaCanvasOp.STROKE -> {
                    stroke.shader = null
                    stroke.color = color()
                    strokeStyle(stroke)
                    canvas.drawPath(path(), stroke)
                }
                AsukaCanvasOp.GRADIENT -> {
                    val n = r.int
                    val colors = IntArray(n)
                    val stops = FloatArray(n)
                    for (i in 0 until n) { colors[i] = color(); stops[i] = f() }
                    val x0 = f() * d; val y0 = f() * d; val x1 = f() * d; val y1 = f() * d
                    fill.shader = if (n >= 2) LinearGradient(x0, y0, x1, y1, colors, stops, Shader.TileMode.CLAMP) else null
                    if (n == 1) fill.color = colors[0]
                    canvas.drawPath(path(), fill)
                    fill.shader = null
                }
                AsukaCanvasOp.TEXT -> {
                    text.color = color()
                    text.textSize = f() * d
                    val weight = weightOf(r.get().toInt())
                    val align = r.get().toInt()
                    val x = f() * d; val y = f() * d
                    val b = ByteArray(r.int).also { r.get(it) }
                    text.typeface = Typeface.create(Typeface.DEFAULT, if (weight > 0) weight else 400, false)
                    text.textAlign = when (align) {
                        AsukaTextAlign.CENTER -> Paint.Align.CENTER
                        AsukaTextAlign.END -> Paint.Align.RIGHT
                        else -> Paint.Align.LEFT
                    }
                    canvas.drawText(String(b, Charsets.UTF_8), x, y, text)
                }
                else -> break
            }
        }
        while (alphas.isNotEmpty()) { canvas.restore(); alphas.removeAt(alphas.size - 1) }
        rules.clear()
    }
}
