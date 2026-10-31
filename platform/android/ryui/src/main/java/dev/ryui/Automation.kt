package dev.ryui

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.ApplicationInfo
import android.os.Build
import android.view.View
import android.widget.CompoundButton
import android.widget.TextView
import java.io.File

/**
 * Drives a debug build from outside, for development and tests, as the
 * iOS host's RyUIAutomation does:
 *
 *     adb shell am broadcast -a dev.ryui.automation -p <package> --es commands 'tap Add'
 *
 * Commands, a line each: `tap <text>` presses the view whose text or
 * content description is <text> (or the pressable view it's in); `type
 * <text>` replaces the focused text field's text (or the first one's);
 * `submit` presses its return key; `back` is the system's back; `tab
 * <title>` selects the tab with that title; `rotate <degrees> <text>`
 * turns two fingers on the view with that text or content description
 * (a two-pointer touch, as a person's); `dump`
 * writes what the screen shows (each text, or content description, a
 * line each, in creation order) to files/ryui-dump.txt:
 *
 *     adb shell run-as <package> cat files/ryui-dump.txt
 *
 * Only in a debuggable app.
 */
object RyUIAutomation {
    private var receiver: BroadcastReceiver? = null

    fun start(rt: RyUIRuntime) {
        val app = rt.app
        if (app.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE == 0 || receiver != null) return
        val r = object : BroadcastReceiver() {
            override fun onReceive(c: Context, i: Intent) {
                for (line in i.getStringExtra("commands")?.lines() ?: emptyList()) run(rt, line)
            }
        }
        receiver = r
        val f = IntentFilter("dev.ryui.automation")
        if (Build.VERSION.SDK_INT >= 33) app.registerReceiver(r, f, Context.RECEIVER_EXPORTED) else app.registerReceiver(r, f)
    }

    private fun run(rt: RyUIRuntime, line: String) {
        val cmd = line.substringBefore(' ')
        val arg = line.substringAfter(' ', "")
        when (cmd) {
            "tap" -> tap(rt, arg)
            "type" -> field(rt)?.apply { requestFocus(); setText(arg); setSelection(arg.length) }
            "submit" -> field(rt)?.onEditorAction(android.view.inputmethod.EditorInfo.IME_ACTION_DONE)
            "dump" -> dump(rt)
            "rotate" -> {
                val deg = arg.substringBefore(' ').toFloatOrNull() ?: return
                shown(rt).firstOrNull { textOf(it) == arg.substringAfter(' ') }?.let { rotate(it, deg) }
            }
            "back" -> rt.activity?.onBackPressedDispatcher?.onBackPressed()
            "tab" -> rt.activity?.window?.decorView?.let { tab(it, arg) }
        }
    }

    private fun shown(rt: RyUIRuntime): List<View> =
        rt.nodes.values.sortedBy { it.id }.map { it.view }.filter { it.isShown && it.alpha > 0f }

    private fun textOf(v: View): String? {
        val t = (v as? TextView)?.text?.toString()
        return if (!t.isNullOrEmpty()) t else v.contentDescription?.toString()
    }

    private fun field(rt: RyUIRuntime): android.widget.EditText? {
        val fields = shown(rt).filterIsInstance<android.widget.EditText>()
        return fields.firstOrNull { it.isFocused } ?: fields.firstOrNull()
    }

    /** Two fingers on v, a quarter of its width from its center, turning by degrees in 10 steps. */
    private fun rotate(v: View, degrees: Float) {
        val cx = v.width / 2f
        val cy = v.height / 2f
        val r = v.width / 4f
        val t0 = android.os.SystemClock.uptimeMillis()
        val props = Array(2) { android.view.MotionEvent.PointerProperties().apply { id = it; toolType = android.view.MotionEvent.TOOL_TYPE_FINGER } }
        fun coords(a: Double) = Array(2) { i ->
            val b = a + i * Math.PI
            android.view.MotionEvent.PointerCoords().apply { x = cx + r * Math.cos(b).toFloat(); y = cy + r * Math.sin(b).toFloat(); pressure = 1f; size = 1f }
        }
        fun send(action: Int, n: Int, a: Double, at: Long) {
            val e = android.view.MotionEvent.obtain(t0, t0 + at, action, n, props, coords(a), 0, 0, 1f, 1f, 0, 0, 0, 0)
            v.dispatchTouchEvent(e)
            e.recycle()
        }
        val second = android.view.MotionEvent.ACTION_POINTER_DOWN or (1 shl android.view.MotionEvent.ACTION_POINTER_INDEX_SHIFT)
        val secondUp = android.view.MotionEvent.ACTION_POINTER_UP or (1 shl android.view.MotionEvent.ACTION_POINTER_INDEX_SHIFT)
        send(android.view.MotionEvent.ACTION_DOWN, 1, 0.0, 0)
        send(second, 2, 0.0, 10)
        for (s in 1..10) send(android.view.MotionEvent.ACTION_MOVE, 2, Math.toRadians(degrees * s / 10.0), 10L + s * 16)
        send(secondUp, 2, Math.toRadians(degrees.toDouble()), 200)
        send(android.view.MotionEvent.ACTION_UP, 1, Math.toRadians(degrees.toDouble()), 210)
    }

    private fun tap(rt: RyUIRuntime, text: String) {
        var v: View? = shown(rt).firstOrNull { textOf(it) == text } ?: return
        // A label inside a pressable view: the view.
        while (v != null && !v.isClickable && v !is CompoundButton) v = v.parent as? View
        v?.performClick()
    }

    private fun tab(v: View, title: String): Boolean {
        if (v is com.google.android.material.navigation.NavigationBarView) {
            val m = v.menu
            for (i in 0 until m.size()) if (m.getItem(i).title == title) { v.selectedItemId = m.getItem(i).itemId; return true }
        }
        if (v is android.view.ViewGroup) for (i in 0 until v.childCount) if (tab(v.getChildAt(i), title)) return true
        return false
    }

    private fun dump(rt: RyUIRuntime) {
        val lines = shown(rt).mapNotNull { v -> textOf(v)?.takeIf { it.isNotEmpty() } }
        val dir = rt.app.filesDir
        // Written whole, then moved: a reader never sees half of it.
        val tmp = File(dir, "ryui-dump.txt.part")
        tmp.writeText(lines.joinToString("\n") + "\n")
        tmp.renameTo(File(dir, "ryui-dump.txt"))
    }
}
