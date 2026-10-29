package dev.nui

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
 * iOS host's NuiAutomation does:
 *
 *     adb shell am broadcast -a dev.nui.automation -p <package> --es commands 'tap Add'
 *
 * Commands, a line each: `tap <text>` presses the view whose text or
 * content description is <text> (or the pressable view it's in); `type
 * <text>` replaces the focused text field's text (or the first one's);
 * `submit` presses its return key; `dump`
 * writes what the screen shows (each text, or content description, a
 * line each, in creation order) to files/nui-dump.txt:
 *
 *     adb shell run-as <package> cat files/nui-dump.txt
 *
 * Only in a debuggable app.
 */
object NuiAutomation {
    private var receiver: BroadcastReceiver? = null

    fun start(rt: NuiRuntime) {
        val app = rt.app
        if (app.applicationInfo.flags and ApplicationInfo.FLAG_DEBUGGABLE == 0 || receiver != null) return
        val r = object : BroadcastReceiver() {
            override fun onReceive(c: Context, i: Intent) {
                for (line in i.getStringExtra("commands")?.lines() ?: emptyList()) run(rt, line)
            }
        }
        receiver = r
        val f = IntentFilter("dev.nui.automation")
        if (Build.VERSION.SDK_INT >= 33) app.registerReceiver(r, f, Context.RECEIVER_EXPORTED) else app.registerReceiver(r, f)
    }

    private fun run(rt: NuiRuntime, line: String) {
        val cmd = line.substringBefore(' ')
        val arg = line.substringAfter(' ', "")
        when (cmd) {
            "tap" -> tap(rt, arg)
            "type" -> field(rt)?.apply { requestFocus(); setText(arg); setSelection(arg.length) }
            "submit" -> field(rt)?.onEditorAction(android.view.inputmethod.EditorInfo.IME_ACTION_DONE)
            "dump" -> dump(rt)
        }
    }

    private fun shown(rt: NuiRuntime): List<View> =
        rt.nodes.values.sortedBy { it.id }.map { it.view }.filter { it.isShown && it.alpha > 0f }

    private fun textOf(v: View): String? {
        val t = (v as? TextView)?.text?.toString()
        return if (!t.isNullOrEmpty()) t else v.contentDescription?.toString()
    }

    private fun field(rt: NuiRuntime): android.widget.EditText? {
        val fields = shown(rt).filterIsInstance<android.widget.EditText>()
        return fields.firstOrNull { it.isFocused } ?: fields.firstOrNull()
    }

    private fun tap(rt: NuiRuntime, text: String) {
        var v: View? = shown(rt).firstOrNull { textOf(it) == text } ?: return
        // A label inside a pressable view: the view.
        while (v != null && !v.isClickable && v !is CompoundButton) v = v.parent as? View
        v?.performClick()
    }

    private fun dump(rt: NuiRuntime) {
        val lines = shown(rt).mapNotNull { v -> textOf(v)?.takeIf { it.isNotEmpty() } }
        val dir = rt.app.filesDir
        // Written whole, then moved: a reader never sees half of it.
        val tmp = File(dir, "nui-dump.txt.part")
        tmp.writeText(lines.joinToString("\n") + "\n")
        tmp.renameTo(File(dir, "nui-dump.txt"))
    }
}
