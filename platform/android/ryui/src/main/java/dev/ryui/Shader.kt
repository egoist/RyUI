package dev.ryui

import android.annotation.SuppressLint
import android.graphics.Canvas
import android.graphics.Paint
import android.os.Build
import android.util.Log
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * A Shader: its box drawn by an AGSL RuntimeShader (Android 13 and later),
 * the app's shade(uv) with time, size and the uniforms as uniforms, under
 * its children. Animated, it draws again at each frame (postInvalidate-
 * OnAnimation). Before Android 13 there's no RuntimeShader: its background
 * alone shows.
 */
@SuppressLint("ViewConstructor")
class RyUIShaderView(private val rt: RyUIRuntime) : RyUIContainer(rt.themed) {
    var source = ""
        set(v) { field = v; shader = null; invalidate() }
    var uniforms: List<Pair<String, FloatArray>> = emptyList()
        set(v) {
            if (v.map { it.first } != field.map { it.first }) shader = null
            field = v
            invalidate()
        }
    var animated = false
        set(v) { field = v; invalidate() }

    private var shader: Any? = null // a RuntimeShader, made lazily: the class is Android 13's
    private var failed = false
    private val paint = Paint()
    private val start = System.nanoTime()

    init { setWillNotDraw(false) }

    private fun program(): String {
        val decl = uniforms.joinToString("\n") { "uniform float4 ${it.first};" }
        return """
            uniform float time;
            uniform float2 size;
            uniform float2 ryui_pixels;
            $decl
            $source
            half4 main(float2 p) {
                float4 c = shade(p / ryui_pixels);
                return half4(c.rgb * c.a, c.a);
            }
        """.trimIndent()
    }

    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        if (Build.VERSION.SDK_INT < 33 || source.isEmpty() || width == 0 || height == 0) return
        var s = shader as android.graphics.RuntimeShader?
        if (s == null && !failed) {
            try {
                s = android.graphics.RuntimeShader(program())
                shader = s
            } catch (e: IllegalArgumentException) {
                failed = true
                Log.e("ryui", "ui.Shader's source doesn't compile with AGSL: ${e.message}")
            }
        }
        s ?: return
        val d = rt.density
        s.setFloatUniform("time", (System.nanoTime() - start) / 1e9f)
        s.setFloatUniform("size", width / d, height / d)
        s.setFloatUniform("ryui_pixels", width.toFloat(), height.toFloat())
        for ((name, v) in uniforms) s.setFloatUniform(name, v[0], v[1], v[2], v[3])
        paint.shader = s
        canvas.drawRect(0f, 0f, width.toFloat(), height.toFloat(), paint)
        if (animated && isAttachedToWindow) postInvalidateOnAnimation()
    }

    fun setUniforms(b: ByteBuffer) {
        val r = b.duplicate().order(ByteOrder.LITTLE_ENDIAN)
        if (r.remaining() < 4) return
        val out = ArrayList<Pair<String, FloatArray>>()
        for (i in 0 until r.int) {
            val name = ByteArray(r.int).also { r.get(it) }.toString(Charsets.UTF_8)
            out.add(name to floatArrayOf(r.float, r.float, r.float, r.float))
        }
        failed = false
        uniforms = out
    }
}
