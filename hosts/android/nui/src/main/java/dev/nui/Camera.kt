package dev.nui

import androidx.activity.result.contract.ActivityResultContracts
import androidx.core.content.FileProvider
import java.io.File

private var shots = 0

/**
 * nui/camera's module: "take" opens the system's camera app (which needs
 * no camera permission of the app's) to a file of the app's, through its
 * FileProvider, and answers with that file ("" if cancelled), or fails
 * "no camera".
 */
fun NuiRuntime.cameraMethods(): Map<String, NuiModuleMethod> = mapOf(
    "take" to { _, reply ->
        val act = activity
        if (act == null) {
            reply(Result.failure(Exception("no screen")))
        } else if (!app.packageManager.hasSystemFeature(android.content.pm.PackageManager.FEATURE_CAMERA_ANY)) {
            reply(Result.failure(Exception("no camera")))
        } else {
            val dir = File(app.cacheDir, "nui-camera").apply { mkdirs() }
            val f = File(dir, java.util.UUID.randomUUID().toString() + ".jpg")
            val uri = FileProvider.getUriForFile(app, app.packageName + ".nui.files", f)
            var launcher: androidx.activity.result.ActivityResultLauncher<android.net.Uri>? = null
            launcher = act.activityResultRegistry.register("nui-camera-${shots++}", ActivityResultContracts.TakePicture()) { _ ->
                launcher?.unregister()
                // The file is the answer: some camera apps write it and
                // still report a failure.
                reply(Result.success(if (f.length() > 0) f.absolutePath.toByteArray() else ByteArray(0)))
            }
            try {
                launcher.launch(uri)
            } catch (e: android.content.ActivityNotFoundException) {
                launcher.unregister()
                reply(Result.failure(Exception("no camera")))
            }
        }
    },
)
