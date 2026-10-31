package dev.ryui

import android.content.pm.PackageManager
import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.ContextCompat

private var asks = 0

/** Asks for the permissions (those not granted yet), then calls done with whether any of them is granted. Main thread. */
fun RyUIRuntime.withPermission(permissions: Array<String>, done: (Boolean) -> Unit) {
    if (permissions.any { ContextCompat.checkSelfPermission(app, it) == PackageManager.PERMISSION_GRANTED }) { done(true); return }
    val act = activity ?: run { done(false); return }
    var launcher: androidx.activity.result.ActivityResultLauncher<Array<String>>? = null
    launcher = act.activityResultRegistry.register("ryui-permission-${asks++}", ActivityResultContracts.RequestMultiplePermissions()) { granted ->
        launcher?.unregister()
        done(granted.values.any { it })
    }
    launcher.launch(permissions)
}
