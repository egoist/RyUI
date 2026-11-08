package dev.ryui

import android.view.View
import android.view.ViewGroup
import android.widget.PopupWindow
import com.google.android.material.color.MaterialColors
import com.google.android.material.shape.MaterialShapeDrawable
import com.google.android.material.shape.ShapeAppearanceModel

/**
 * A Popover: a PopupWindow over its anchor, as big as the core laid its
 * content out, on Material's container surface with its rounded corners
 * and elevation (as a menu's or a rich tooltip's). A tap outside or the
 * back button dismisses it, reported as Dismissed; while ui.OnBack in it
 * intercepts, they ask the core (Back) instead (its dismiss is overridden).
 */
class RyUIPopover(private val rt: RyUIRuntime, private val n: RyUINode) {
    var anchor = 0
    var width = 0f
    var height = 0f
    private var popup: PopupWindow? = null
    private var closing = false // the core closes it

    private fun px(v: Float) = Math.round(v * rt.density)

    fun present() {
        val screen = n.view as? RyUIScreen ?: return
        val a = rt.nodes[anchor]?.view ?: return
        if (!a.isAttachedToWindow) return
        (screen.parent as? ViewGroup)?.removeView(screen)
        val ctx = rt.themed
        val shape = MaterialShapeDrawable(ShapeAppearanceModel.builder().setAllCorners(
            com.google.android.material.shape.CornerFamily.ROUNDED, 16 * rt.density).build()).apply {
            fillColor = android.content.res.ColorStateList.valueOf(
                MaterialColors.getColor(ctx, com.google.android.material.R.attr.colorSurfaceContainer, android.graphics.Color.WHITE))
            initializeElevationOverlay(ctx)
        }
        val p = object : PopupWindow(screen, px(width), px(height), true) {
            // Android's back and a tap outside dismiss it themselves: while
            // ui.OnBack in it intercepts, they ask the core (Back) instead.
            override fun dismiss() {
                if (!closing && !n.dying && n.intercepts) {
                    rt.emit(n.id, RyUIEventKind.BACK) { RyUIEvents.back(n.id) }
                    return
                }
                super.dismiss()
            }
        }
        p.setBackgroundDrawable(shape)
        p.elevation = 3 * rt.density
        screen.clipToOutline = true
        screen.outlineProvider = object : android.view.ViewOutlineProvider() {
            override fun getOutline(view: View, outline: android.graphics.Outline) {
                outline.setRoundRect(0, 0, view.width, view.height, 16 * rt.density)
            }
        }
        p.isOutsideTouchable = true
        p.setOnDismissListener {
            popup = null
            if (!n.dying) {
                n.dying = true
                rt.emit(n.id, RyUIEventKind.DISMISSED) { RyUIEvents.dismissed(n.id) }
            }
        }
        popup = p
        // Below its anchor, or above it where there's no room (PopupWindow's own choice), centered on it.
        p.showAsDropDown(a, (a.width - px(width)) / 2, px(4f))
    }

    fun resize() {
        val p = popup ?: return
        if (p.isShowing) p.update(px(width), px(height))
    }

    fun dismiss() {
        val p = popup ?: return
        popup = null
        closing = true
        p.dismiss()
    }
}
