package dev.asuka

import android.annotation.SuppressLint
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RuntimeShader
import android.util.Log
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * A Shader: its box drawn by the shader's AGSL program (package shadercode
 * compiles it from the app's Kage source) as a RuntimeShader, under its
 * children. Its uniforms are U0, U1..., set from the lanes the core sends;
 * the host sets the size, the scale and Time. Animated, it draws again at
 * each frame (postInvalidateOnAnimation).
 */
@SuppressLint("ViewConstructor")
class AsukaShaderView(private val rt: AsukaRuntime) : AsukaContainer(rt.themed) {
    /** A uniform: its lanes, and whether they're ints. */
    private class Uniform(val lane: Int, val lanes: Int, val int: Boolean)

    private var agsl = ""
    private var uniforms: List<Uniform> = emptyList()
    private var sizeLane = 0
    private var scaleLane = 0
    private var timeLane = -1
    private var words = IntArray(0)
    var animated = false
        set(v) { field = v; invalidate() }

    private var shader: RuntimeShader? = null
    private var failed = false
    private val paint = Paint()
    private val start = System.nanoTime()

    init { setWillNotDraw(false) }

    /** The compiled shader (internal/shadercode's Encode). */
    fun setCode(b: ByteBuffer) {
        val r = b.duplicate().order(ByteOrder.LITTLE_ENDIAN)
        fun str(): String = ByteArray(r.int).also { r.get(it) }.toString(Charsets.UTF_8)
        str() // the Kage source: the CPU's
        str() // Metal: Apple's
        agsl = str()
        val lanes = r.int
        sizeLane = r.int
        scaleLane = r.int
        timeLane = r.int - 1
        r.int // the Metal buffer's size
        repeat(lanes) { r.int } // the Metal offsets
        val us = ArrayList<Uniform>()
        repeat(r.int) {
            str()
            val lane = r.int
            val n = r.int
            us.add(Uniform(lane, n, r.get().toInt() != 0)) // 0 float; AGSL has no uints or bools
        }
        uniforms = us
        if (words.size != lanes) words = IntArray(lanes)
        shader = null
        failed = false
        invalidate()
    }

    /** The uniforms' lanes. */
    fun setUniforms(b: ByteBuffer) {
        val r = b.duplicate().order(ByteOrder.LITTLE_ENDIAN)
        val n = r.remaining() / 4
        if (words.size != n) words = IntArray(n)
        for (i in 0 until n) words[i] = r.int
        invalidate()
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        if (agsl.isEmpty() || width == 0 || height == 0 || words.isEmpty()) return
        var s = shader
        if (s == null && !failed) {
            try {
                s = RuntimeShader(agsl)
                shader = s
            } catch (e: IllegalArgumentException) {
                failed = true
                Log.e("asuka", "a ui.Shader's AGSL doesn't compile: ${e.message}")
            }
        }
        s ?: return
        val d = rt.density
        words[sizeLane] = java.lang.Float.floatToRawIntBits(width / d)
        words[sizeLane + 1] = java.lang.Float.floatToRawIntBits(height / d)
        words[scaleLane] = java.lang.Float.floatToRawIntBits(d)
        if (timeLane >= 0) words[timeLane] = java.lang.Float.floatToRawIntBits((System.nanoTime() - start) / 1e9f)
        for ((i, u) in uniforms.withIndex()) {
            try {
                if (u.int) s.setIntUniform("U$i", IntArray(u.lanes) { words[u.lane + it] })
                else s.setFloatUniform("U$i", FloatArray(u.lanes) { java.lang.Float.intBitsToFloat(words[u.lane + it]) })
            } catch (_: IllegalArgumentException) {
                // A uniform the program doesn't read: AGSL dropped it.
            }
        }
        paint.shader = s
        canvas.drawRect(0f, 0f, width.toFloat(), height.toFloat(), paint)
        if (animated && isAttachedToWindow) postInvalidateOnAnimation()
    }
}
