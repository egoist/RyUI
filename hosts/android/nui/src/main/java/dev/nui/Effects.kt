package dev.nui

import android.graphics.Outline
import android.graphics.drawable.GradientDrawable
import android.os.Build
import android.view.View
import android.view.ViewOutlineProvider
import java.nio.ByteBuffer

/** A background gradient (Style.Gradient): stops, and its direction. */
class NuiGradient(val colors: IntArray, val offsets: FloatArray, val sx: Float, val sy: Float, val ex: Float, val ey: Float) {
    /** GradientDrawable's direction nearest the start to end vector. */
    fun orientation(): GradientDrawable.Orientation {
        val a = Math.toDegrees(Math.atan2((ey - sy).toDouble(), (ex - sx).toDouble()))
        val o = ((Math.round(a / 45.0).toInt() % 8) + 8) % 8 // 0: left to right, clockwise (y is down)
        return when (o) {
            0 -> GradientDrawable.Orientation.LEFT_RIGHT
            1 -> GradientDrawable.Orientation.TL_BR
            2 -> GradientDrawable.Orientation.TOP_BOTTOM
            3 -> GradientDrawable.Orientation.TR_BL
            4 -> GradientDrawable.Orientation.RIGHT_LEFT
            5 -> GradientDrawable.Orientation.BR_TL
            6 -> GradientDrawable.Orientation.BOTTOM_TOP
            else -> GradientDrawable.Orientation.BL_TR
        }
    }
}

/** Style.Transform: the view's translation, scale and rotation, around its center. */
fun NuiRuntime.setTransform(n: NuiNode, b: ByteBuffer) {
    if (b.remaining() < 20) return
    val d = density
    val tx = b.float * d
    val ty = b.float * d
    val sx = b.float
    val sy = b.float
    val rot = b.float
    n.base.tx = tx; n.base.ty = ty; n.base.sx = sx; n.base.sy = sy; n.base.rot = rot
    val v = n.outer
    val a = animation
    if (a != null && animates(v)) {
        animateProps(v, a, View.TRANSLATION_X to tx, View.TRANSLATION_Y to ty, View.SCALE_X to sx, View.SCALE_Y to sy, View.ROTATION to rot)
        return
    }
    for (p in listOf(View.TRANSLATION_X, View.TRANSLATION_Y, View.SCALE_X, View.SCALE_Y, View.ROTATION)) stopRepeating(v, p)
    v.translationX = tx
    v.translationY = ty
    v.scaleX = sx
    v.scaleY = sy
    v.rotation = rot
}

/**
 * Style.Shadow as Android draws shadows: an elevation, over the view's
 * rounded shape, tinted by the color where the system can (Android 9 and
 * later); the light, not the offset, places it.
 */
fun NuiRuntime.setShadow(n: NuiNode, b: ByteBuffer) {
    if (b.remaining() < 20) return
    val c = b.long
    val radius = b.float
    b.float; b.float // the offset: Android's light decides it
    val v = n.view
    if (c == 0L) {
        v.elevation = 0f
        return
    }
    v.outlineProvider = object : ViewOutlineProvider() {
        override fun getOutline(view: View, o: Outline) {
            o.setRoundRect(0, 0, view.width, view.height, n.cornerRadius * density)
        }
    }
    v.elevation = radius / 2 * density
    if (Build.VERSION.SDK_INT >= 28) {
        val col = color(c)
        v.outlineSpotShadowColor = col
        v.outlineAmbientShadowColor = col
    }
}

fun NuiRuntime.setGradient(n: NuiNode, b: ByteBuffer) {
    val count = if (b.remaining() >= 4) b.int else 0
    if (count == 0) {
        n.gradient = null
        applyBackground(n)
        return
    }
    val colors = IntArray(count)
    val offsets = FloatArray(count)
    for (i in 0 until count) {
        colors[i] = color(b.long)
        offsets[i] = b.float
    }
    n.gradient = NuiGradient(colors, offsets, b.float, b.float, b.float, b.float)
    applyBackground(n)
}
