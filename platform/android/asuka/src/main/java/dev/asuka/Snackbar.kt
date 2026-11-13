package dev.asuka

import android.annotation.SuppressLint
import android.view.View
import android.view.ViewGroup
import android.view.ViewTreeObserver
import android.widget.FrameLayout
import androidx.coordinatorlayout.widget.CoordinatorLayout
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import com.google.android.material.bottomappbar.BottomAppBar
import com.google.android.material.bottomnavigation.BottomNavigationView
import com.google.android.material.bottomsheet.BottomSheetDialog
import com.google.android.material.floatingactionbutton.ExtendedFloatingActionButton
import com.google.android.material.floatingactionbutton.FloatingActionButton
import com.google.android.material.snackbar.BaseTransientBottomBar
import com.google.android.material.snackbar.Snackbar

/**
 * A Snackbar node: Material's Snackbar. It shows over the activity in an
 * overlay CoordinatorLayout, so it can be swiped away, above the current
 * screen's bottom navigation bar, bottom app bar or floating action
 * button (its anchor view), the system's navigation bar and the
 * keyboard; over a sheet that's showing, at the bottom of the sheet's
 * own CoordinatorLayout. Material's Snackbar lengthens its duration as
 * AccessibilityManager.getRecommendedTimeoutMillis says (longer with an
 * action while TalkBack is on), and TalkBack announces it. This view
 * stands for it in the node; it's never attached.
 */
@SuppressLint("ViewConstructor")
class AsukaSnackbar(private val rt: AsukaRuntime, private val id: Int) : View(rt.themed) {
    var message = ""
    var duration = AsukaSnackbarDuration.SHORT
    var action = ""
        set(v) {
            field = v
            bar?.let { setAction(it) } // a new language while it shows
        }
    private var bar: Snackbar? = null
    /** Reported (its action, a timeout, a swipe) or destroyed: nothing more to report. */
    private var done = false
    private var overlay: CoordinatorLayout? = null
    private var anchor: View? = null
    private val relayout = ViewTreeObserver.OnGlobalLayoutListener { place() }

    fun set(prop: Int, v: Any): Boolean {
        when (prop) {
            AsukaProp.Snackbar.MESSAGE -> {
                message = v as String
                bar?.setText(message)
            }
            AsukaProp.Snackbar.ACTION -> action = v as String
            AsukaProp.Snackbar.DURATION -> duration = v as Int
            AsukaProp.Snackbar.WINDOW -> {} // a phone has one window
            else -> return false
        }
        return true
    }

    fun present(act: AsukaActivity) {
        if (done || bar != null) return
        val ms = when (duration) {
            AsukaSnackbarDuration.LONG -> 10_000
            AsukaSnackbarDuration.INDEFINITE -> Snackbar.LENGTH_INDEFINITE
            else -> 4_000 // Material's short: 4 seconds (its LENGTH_SHORT is a toast's 1.5)
        }
        val ctx = rt.dialogContext(act)
        // A sheet showing covers the activity: it goes on the sheet, at the
        // bottom of its CoordinatorLayout, as Material's apps do.
        val sheet = rt.nodes.values.filter { it.kind == AsukaKind.SHEET && !it.dying && it.dialog?.isShowing == true }
            .maxByOrNull { it.id }?.dialog as? BottomSheetDialog
        val parent: View = sheet?.findViewById(com.google.android.material.R.id.coordinator)
            ?: overlayOf(act).also { overlay = it }
        val sb = Snackbar.make(ctx, parent, message, ms)
        bar = sb
        if (overlay != null) {
            // The overlay's padding keeps it above the system bars and the
            // keyboard (the activity's frame takes the window's insets).
            sb.isGestureInsetBottomIgnored = true
            sb.view.layoutDirection = if (rt.rtl) LAYOUT_DIRECTION_RTL else LAYOUT_DIRECTION_LTR
        }
        setAction(sb)
        sb.addCallback(object : BaseTransientBottomBar.BaseCallback<Snackbar>() {
            override fun onDismissed(b: Snackbar, event: Int) {
                if (bar === b) gone()
                // Its action reported already; a Destroy asked for it.
                if (done || event == DISMISS_EVENT_ACTION || event == DISMISS_EVENT_MANUAL) return
                done = true
                rt.emit(id, AsukaEventKind.DISMISSED) { AsukaEvents.dismissed(id) } // a timeout, a swipe, another app's
            }
        })
        place()
        act.window.decorView.viewTreeObserver.addOnGlobalLayoutListener(relayout)
        current = this
        sb.show()
    }

    private fun setAction(sb: Snackbar) {
        if (action.isEmpty()) sb.setAction(null, null)
        else sb.setAction(action) {
            if (done) return@setAction
            done = true
            rt.emit(id, AsukaEventKind.ACTION) { AsukaEvents.action(id, 0) }
        }
    }

    /** The core destroyed it: out, animated. */
    fun dismiss() {
        done = true
        val b = bar ?: return
        b.dismiss()
    }

    private fun gone() {
        bar = null
        rt.activity?.window?.decorView?.viewTreeObserver?.removeOnGlobalLayoutListener(relayout)
        if (current === this) current = null
    }

    /**
     * Places it above the current screen's bottom bar or floating action
     * button (its anchor view, which it then follows) if one shows above
     * the keyboard, else above the keyboard or the navigation bar.
     */
    fun place() {
        val sb = bar ?: return
        val o = overlay ?: return
        val act = rt.activity ?: return
        val frame = o.parent as? ViewGroup ?: return
        if (frame.getChildAt(frame.childCount - 1) !== o) o.bringToFront()
        val insets = ViewCompat.getRootWindowInsets(frame)
        val bars = insets?.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout())
        val ime = insets?.getInsets(WindowInsetsCompat.Type.ime())?.bottom ?: act.keyboardPx
        val bottom = maxOf(ime, bars?.bottom ?: 0)
        if (o.paddingLeft != (bars?.left ?: 0) || o.paddingRight != (bars?.right ?: 0) || o.paddingBottom != bottom)
            o.setPadding(bars?.left ?: 0, 0, bars?.right ?: 0, bottom)
        val a = anchorIn(frame, frame.height - ime)
        if (a !== anchor) {
            anchor = a
            sb.anchorView = a
        }
    }

    /** The highest bottom bar or floating action button near the bottom of the current screen, above y; or null. */
    private fun anchorIn(frame: ViewGroup, above: Int): View? {
        val loc = IntArray(2)
        frame.getLocationInWindow(loc)
        val frameTop = loc[1]
        var best: View? = null
        var bestTop = Int.MAX_VALUE
        fun visit(v: View) {
            if (v === overlay || v.visibility != VISIBLE || v.alpha == 0f) return
            if (v is BottomNavigationView || v is BottomAppBar || v is FloatingActionButton || v is ExtendedFloatingActionButton) {
                if (!v.isShown || v.height == 0) return
                v.getLocationInWindow(loc)
                val top = loc[1] - frameTop
                val bottom = top + v.height
                // Near the bottom (a FAB in the content's middle isn't one), and above the keyboard.
                if (bottom >= frame.height * 3 / 4 && bottom <= above && top < bestTop) {
                    best = v
                    bestTop = top
                }
                return
            }
            when (v) {
                // Only the screens that show: a stack's top, a tabs' page and bar.
                is AsukaStack -> v.top?.let { visit(it.outer) }
                is AsukaTabs -> { visit(v.bar); v.current?.let { visit(it.outer) } }
                is ViewGroup -> for (i in 0 until v.childCount) visit(v.getChildAt(i))
            }
        }
        for (i in 0 until frame.childCount) visit(frame.getChildAt(i))
        return best
    }

    companion object {
        /** The snackbar showing in the activity's overlay. */
        var current: AsukaSnackbar? = null

        /** The activity's overlay for snackbars: a CoordinatorLayout over its content, which only its snackbar takes touches in. */
        fun overlayOf(act: AsukaActivity): CoordinatorLayout {
            val frame = act.rootFrame
            for (i in 0 until frame.childCount) {
                val v = frame.getChildAt(i)
                if (v is CoordinatorLayout && v.tag == TAG) return v
            }
            return CoordinatorLayout(act).also {
                it.tag = TAG
                it.importantForAccessibility = IMPORTANT_FOR_ACCESSIBILITY_NO
                frame.addView(it, FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
            }
        }

        private const val TAG = "asuka.snackbar"
    }
}
