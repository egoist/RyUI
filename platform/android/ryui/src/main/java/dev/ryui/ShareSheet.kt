package dev.ryui

import android.content.Intent
import android.graphics.Color
import android.graphics.drawable.ColorDrawable
import android.graphics.drawable.Drawable
import android.os.Bundle
import android.view.View
import android.view.ViewGroup
import android.widget.FrameLayout
import android.widget.LinearLayout
import androidx.coordinatorlayout.widget.CoordinatorLayout
import androidx.core.view.ViewCompat
import androidx.core.view.WindowInsetsCompat
import com.google.android.material.bottomsheet.BottomSheetBehavior
import com.google.android.material.bottomsheet.BottomSheetDragHandleView
import com.google.android.material.color.MaterialColors
import com.google.android.material.shape.MaterialShapeDrawable
import com.google.android.material.shape.ShapeAppearanceModel
import java.io.File

/**
 * The app's share sheet (app.ShareSheet): what another app shares opens
 * this Activity over that app, in its task, in a process of its own
 * (":share"), where the Go code's root is the sheet (RYUI_SHARE_SHEET),
 * as an iOS share extension runs it. It shows the root in a Material 3
 * bottom sheet, nearly the screen's height, over a scrim; the root's
 * ShareClose ends it, and so does dragging it down, tapping the scrim or
 * going back (cancelled). Its process ends with it, as an iOS share
 * extension's: the next share starts afresh.
 */
class RyUIShareActivity : RyUIActivity() {
    private lateinit var sheet: LinearLayout
    private lateinit var behavior: BottomSheetBehavior<LinearLayout>
    private var top = 0
    private var closing = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        overridePendingTransition(0, 0)
        onBackPressedDispatcher.addCallback(this, object : androidx.activity.OnBackPressedCallback(true) {
            override fun handleOnBackPressed() {
                // The root's own back (a screen pushed in the sheet) first.
                if (runtime.back.isEnabled) runtime.back.handleOnBackPressed() else close(1)
            }
        })
    }

    override fun content(frame: FrameLayout): View {
        val d = resources.displayMetrics.density
        val themed = RyUIRuntime.get(this).themed
        val root = CoordinatorLayout(this)
        // The scrim: the other app, dimmed; a tap cancels.
        val scrim = View(this).apply {
            setBackgroundColor(Color.argb(0x52, 0, 0, 0))
            alpha = 0f
            setOnClickListener { close(1) }
        }
        root.addView(scrim, CoordinatorLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT))
        sheet = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            background = MaterialShapeDrawable(ShapeAppearanceModel.builder().setTopLeftCornerSize(28 * d).setTopRightCornerSize(28 * d).build()).apply {
                fillColor = android.content.res.ColorStateList.valueOf(
                    MaterialColors.getColor(themed, com.google.android.material.R.attr.colorSurfaceContainerLow, Color.WHITE))
            }
            clipToOutline = true
        }
        sheet.addView(BottomSheetDragHandleView(themed), LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.WRAP_CONTENT))
        sheet.addView(frame, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1f))
        val lp = CoordinatorLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT)
        behavior = BottomSheetBehavior<LinearLayout>().apply {
            isHideable = true
            skipCollapsed = true
            isFitToContents = true
            state = BottomSheetBehavior.STATE_HIDDEN
            addBottomSheetCallback(object : BottomSheetBehavior.BottomSheetCallback() {
                override fun onStateChanged(v: View, state: Int) {
                    if (state == BottomSheetBehavior.STATE_HIDDEN) {
                        if (closing) finishSheet() else close(1)
                    }
                }
                override fun onSlide(v: View, offset: Float) {
                    scrim.alpha = (1 + offset).coerceIn(0f, 1f)
                }
            })
        }
        lp.behavior = behavior
        root.addView(sheet, lp)
        // The sheet stops below the status bar, with a gap of the other
        // app showing; its height follows the window's.
        ViewCompat.setOnApplyWindowInsetsListener(root) { _, insets ->
            top = insets.getInsets(WindowInsetsCompat.Type.systemBars() or WindowInsetsCompat.Type.displayCutout()).top
            fit(root)
            insets
        }
        root.addOnLayoutChangeListener { _, _, t, _, b, _, ot, _, ob -> if (b - t != ob - ot) fit(root) }
        root.post {
            fit(root)
            behavior.state = BottomSheetBehavior.STATE_EXPANDED
        }
        return root
    }

    private fun fit(root: View) {
        if (root.height == 0) return
        val h = root.height - top - (12 * resources.displayMetrics.density).toInt()
        if (sheet.layoutParams.height != h) {
            sheet.layoutParams = sheet.layoutParams.apply { height = h }
        }
    }

    /** The sheet starts below the status bar: no top inset in it. */
    override fun safeInsets(bars: IntArray): IntArray = intArrayOf(0, bars[1], bars[2], bars[3])

    /** The other app shows around the sheet. */
    override fun windowBackground(): Drawable = ColorDrawable(Color.TRANSPARENT)

    /**
     * Ends the sheet: 0 done, 1 cancelled, 2 open the app with what was
     * shared (RyUIActivity takes it to ui.OnShare). Otherwise the copies
     * of the files go.
     */
    fun close(how: Int) {
        if (closing) return
        closing = true
        val rt = runtime
        if (how == 2) {
            rt.lastShared?.let { items ->
                startActivity(Intent(this, RyUIActivity::class.java).setAction(OPEN_APP).putExtra(ITEMS, items)
                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
            }
        } else {
            val files = ArrayList(rt.lastSharedFiles)
            Thread { for (f in files) File(f).parentFile?.deleteRecursively() }.start()
        }
        rt.lastSharedFiles.clear()
        rt.lastShared = null
        setResult(if (how == 1) RESULT_CANCELED else RESULT_OK)
        if (behavior.state == BottomSheetBehavior.STATE_HIDDEN) finishSheet() else behavior.state = BottomSheetBehavior.STATE_HIDDEN
    }

    private fun finishSheet() {
        if (isFinishing) return
        finish()
        overridePendingTransition(0, 0)
    }

    override fun onDestroy() {
        super.onDestroy()
        if (closing || isFinishing) android.os.Process.killProcess(android.os.Process.myPid())
    }

    companion object {
        /** The action RyUIActivity is opened with by OpenApp, and its extra: the Shared event's payload. */
        const val OPEN_APP = "dev.ryui.SHARED"
        const val ITEMS = "dev.ryui.ITEMS"
    }
}
