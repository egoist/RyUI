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
    "capture" to { _, reply ->
        val v = NuiCameraView.current
        if (v == null) reply(Result.failure(Exception("no camera view"))) else v.capture(reply)
    },
    "take" to { _, reply -> takePhoto(reply) },
    // args: u8 mute, u32 max milliseconds (0: none).
    "record" to { args, reply ->
        val v = NuiCameraView.current
        if (v == null) reply(Result.failure(Exception("no camera view")))
        else if (args.size < 5) reply(Result.failure(Exception("bad arguments")))
        else {
            val ms = java.nio.ByteBuffer.wrap(args, 1, 4).order(java.nio.ByteOrder.LITTLE_ENDIAN).int.toLong() and 0xffffffffL
            v.record(args[0] == 1.toByte(), ms, reply)
        }
    },
    "stopRecording" to { _, reply -> NuiCameraView.current?.stopRecording(); reply(Result.success(ByteArray(0))) },
)

/** Whether the app's manifest asks for permission (nui.json's, or a package's: the CLI's featurePermissions). */
fun NuiRuntime.declares(permission: String): Boolean = try {
    app.packageManager.getPackageInfo(app.packageName, android.content.pm.PackageManager.GET_PERMISSIONS)
        .requestedPermissions?.contains(permission) == true
} catch (_: Exception) { false }

/** The camera app, after the camera permission if the app declares it (then the camera app needs it granted). */
private fun NuiRuntime.takePhoto(reply: (Result<ByteArray>) -> Unit) {
    if (declares(android.Manifest.permission.CAMERA)) {
        withPermission(arrayOf(android.Manifest.permission.CAMERA)) { ok ->
            if (ok) launchCamera(reply) else reply(Result.failure(Exception("not allowed")))
        }
    } else launchCamera(reply)
}

private fun NuiRuntime.launchCamera(reply: (Result<ByteArray>) -> Unit) {
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
}

/**
 * nui/camera's View: CameraX's PreviewView, bound to the Activity's
 * lifecycle with a preview and an image capture (for camera.Capture),
 * once the camera's allowed.
 */
class NuiCameraView(private val rt: NuiRuntime, private val ctx: NuiNativeContext) : NuiNativeView {
    companion object {
        var current: NuiCameraView? = null
    }

    private val preview = androidx.camera.view.PreviewView(ctx.context).apply {
        scaleType = androidx.camera.view.PreviewView.ScaleType.FILL_CENTER
    }
    override val view: android.view.View get() = preview
    private var capture: androidx.camera.core.ImageCapture? = null
    private var provider: androidx.camera.lifecycle.ProcessCameraProvider? = null
    private var front = false
    private var scan = false
    private var kinds = emptySet<String>()
    private var analyzer: NuiCodeAnalyzer? = null
    private var analysis: java.util.concurrent.ExecutorService? = null
    private val seen = NuiCodeSeen()
    private var binding = false
    private var video = false
    private var videoCapture: androidx.camera.video.VideoCapture<androidx.camera.video.Recorder>? = null
    private var recording: androidx.camera.video.Recording? = null

    init {
        // The camera is open while the view is in a window: leaving the
        // screen closes it.
        preview.addOnAttachStateChangeListener(object : android.view.View.OnAttachStateChangeListener {
            override fun onViewAttachedToWindow(v: android.view.View) { current = this@NuiCameraView; if (!binding) bind() }
            override fun onViewDetachedFromWindow(v: android.view.View) { unbind() }
        })
    }

    override fun set(name: String, value: Any) {
        when (name) {
            "facing" -> front = value == "front"
            "scan" -> scan = value == true
            "codes" -> kinds = NuiScan.kinds(value as? String ?: "")
            "video" -> video = value == true
            else -> return
        }
        // Once for the props set together.
        if (binding) return
        binding = true
        preview.post {
            binding = false
            if (preview.isAttachedToWindow) bind()
        }
    }

    private fun unbind() {
        if (current === this) current = null
        provider?.unbindAll()
        provider = null
        capture = null
        videoCapture = null
        analyzer?.close()
        analyzer = null
        analysis?.shutdown()
        analysis = null
    }

    private fun bind() {
        val act = rt.activity ?: return
        rt.withPermission(arrayOf(android.Manifest.permission.CAMERA)) { ok ->
            if (!ok) { ctx.emit("error", mapOf("message" to "not allowed")); return@withPermission }
            val future = androidx.camera.lifecycle.ProcessCameraProvider.getInstance(act)
            future.addListener({
                if (!preview.isAttachedToWindow) return@addListener // gone meanwhile
                try {
                    val provider = future.get()
                    this.provider = provider
                    val p = androidx.camera.core.Preview.Builder().build().also { it.surfaceProvider = preview.surfaceProvider }
                    val c = androidx.camera.core.ImageCapture.Builder().build()
                    val selector = if (front) androidx.camera.core.CameraSelector.DEFAULT_FRONT_CAMERA else androidx.camera.core.CameraSelector.DEFAULT_BACK_CAMERA
                    provider.unbindAll()
                    analyzer?.close()
                    analyzer = null
                    val cases = mutableListOf<androidx.camera.core.UseCase>(p, c)
                    val reader = NuiScan.reader
                    if (scan && reader == null) ctx.emit("error", mapOf("message" to "nui/scan isn't in this build"))
                    if (scan && reader != null) {
                        val a = reader.analyzer(kinds) { k, v -> if (seen.isNew(k, v)) ctx.emit("code", mapOf("kind" to k, "value" to v)) }
                        val ex = analysis ?: java.util.concurrent.Executors.newSingleThreadExecutor().also { analysis = it }
                        val ia = androidx.camera.core.ImageAnalysis.Builder()
                            .setBackpressureStrategy(androidx.camera.core.ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST).build()
                        ia.setAnalyzer(ex, a)
                        analyzer = a
                        cases += ia
                    }
                    val vc = if (video) androidx.camera.video.VideoCapture.withOutput(androidx.camera.video.Recorder.Builder().build()) else null
                    if (vc != null) cases += vc
                    provider.bindToLifecycle(act, selector, *cases.toTypedArray())
                    capture = c
                    videoCapture = vc
                } catch (e: Exception) {
                    ctx.emit("error", mapOf("message" to (e.message ?: "no camera")))
                }
            }, androidx.core.content.ContextCompat.getMainExecutor(act))
        }
    }

    /** Records to a file until stopRecording (or maxMs, if not 0), with the microphone unless mute (or not allowed). */
    fun record(mute: Boolean, maxMs: Long, reply: (Result<ByteArray>) -> Unit) {
        if (capture == null) { reply(Result.failure(Exception("the camera isn't showing"))); return }
        val vc = videoCapture ?: run { reply(Result.failure(Exception("the camera view isn't in video mode"))); return }
        if (recording != null) { reply(Result.failure(Exception("already recording"))); return }
        val start = { sound: Boolean -> startRecording(vc, sound, maxMs, reply) }
        val mic = android.Manifest.permission.RECORD_AUDIO
        if (mute || !rt.declares(mic)) start(false) else rt.withPermission(arrayOf(mic)) { ok -> start(ok) }
    }

    // The permission is checked: withAudioEnabled is only called with it granted.
    @android.annotation.SuppressLint("MissingPermission")
    private fun startRecording(vc: androidx.camera.video.VideoCapture<androidx.camera.video.Recorder>, sound: Boolean, maxMs: Long, reply: (Result<ByteArray>) -> Unit) {
        if (videoCapture !== vc) { reply(Result.failure(Exception("the camera isn't showing"))); return }
        val dir = File(rt.app.cacheDir, "nui-camera").apply { mkdirs() }
        val f = File(dir, java.util.UUID.randomUUID().toString() + ".mp4")
        val opts = androidx.camera.video.FileOutputOptions.Builder(f).apply { if (maxMs > 0) setDurationLimitMillis(maxMs) }.build()
        var pending = vc.output.prepareRecording(rt.app, opts)
        val granted = androidx.core.content.ContextCompat.checkSelfPermission(rt.app, android.Manifest.permission.RECORD_AUDIO) ==
            android.content.pm.PackageManager.PERMISSION_GRANTED
        if (sound && granted) pending = pending.withAudioEnabled()
        recording = pending.start(androidx.core.content.ContextCompat.getMainExecutor(rt.app)) { ev ->
            if (ev is androidx.camera.video.VideoRecordEvent.Finalize) {
                recording = null
                // Its limit reached, or the camera closed under it (the
                // screen left), the file is still whole.
                val e = ev.error
                val ok = !ev.hasError() || (f.length() > 0 && (e == androidx.camera.video.VideoRecordEvent.Finalize.ERROR_DURATION_LIMIT_REACHED ||
                    e == androidx.camera.video.VideoRecordEvent.Finalize.ERROR_SOURCE_INACTIVE))
                reply(if (ok) Result.success(f.absolutePath.toByteArray()) else Result.failure(Exception(ev.cause?.message ?: "couldn't record ($e)")))
            }
        }
    }

    fun stopRecording() { recording?.stop() }

    fun capture(reply: (Result<ByteArray>) -> Unit) {
        val c = capture ?: run { reply(Result.failure(Exception("the camera isn't showing"))); return }
        val dir = File(rt.app.cacheDir, "nui-camera").apply { mkdirs() }
        val f = File(dir, java.util.UUID.randomUUID().toString() + ".jpg")
        c.takePicture(androidx.camera.core.ImageCapture.OutputFileOptions.Builder(f).build(),
            androidx.core.content.ContextCompat.getMainExecutor(rt.app),
            object : androidx.camera.core.ImageCapture.OnImageSavedCallback {
                override fun onImageSaved(r: androidx.camera.core.ImageCapture.OutputFileResults) = reply(Result.success(f.absolutePath.toByteArray()))
                override fun onError(e: androidx.camera.core.ImageCaptureException) = reply(Result.failure(e))
            })
    }
}
