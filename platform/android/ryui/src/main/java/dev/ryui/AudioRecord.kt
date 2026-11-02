package dev.ryui

import android.Manifest
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.media.MediaRecorder
import android.os.Build
import android.os.IBinder
import android.os.SystemClock
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import java.io.File
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * ryui/audio/record's module: "start" (u8 quality: voice, music; u32 max
 * milliseconds, 0: none; u8 flags, 1 background, 2 stream; u32-length
 * notification title; u32-length path, empty for the app's caches) records
 * AAC, in MPEG-4 or as an ADTS stream, until "stop" and answers with the path;
 * "pause", "resume"; "level" answers u8, 0 to 255; "status" answers u8
 * level, u8 recording, u32 milliseconds kept. With background, a foreground
 * service of the microphone type keeps it going out of sight.
 */
class RyUIAudioRecorder(private val rt: RyUIRuntime) {
    private var recorder: MediaRecorder? = null
    private var file: File? = null
    private var done: ((Result<ByteArray>) -> Unit)? = null
    private var kept = 0L // milliseconds before the last start or resume
    private var since = 0L // when it last started or resumed; 0: paused
    private var service = false

    init { current = this }

    companion object {
        const val CHANNEL = "ryui.recording"
        const val NOTIFICATION = 0x6e76_0001
        var current: RyUIAudioRecorder? = null
        var title = "Recording"
    }

    /** The notification's Stop button. */
    fun stopFromNotification() = finish(null)

    private fun level(): Byte {
        // The loudest sample since the last call, of 32767.
        val amp = if (since == 0L) 0 else runCatching { recorder?.maxAmplitude ?: 0 }.getOrDefault(0)
        return (minOf(amp, 32767) * 255 / 32767).toByte()
    }

    private fun startService() {
        val nm = rt.app.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (nm.getNotificationChannel(CHANNEL) == null) {
            nm.createNotificationChannel(NotificationChannel(CHANNEL, "Recording", NotificationManager.IMPORTANCE_LOW))
        }
        // Its notification shows (Android 13 asks first); recording goes on either way.
        if (Build.VERSION.SDK_INT >= 33) rt.withPermission(arrayOf(Manifest.permission.POST_NOTIFICATIONS)) {}
        ContextCompat.startForegroundService(rt.app, Intent(rt.app, RyUIRecordService::class.java))
        service = true
    }

    private fun stopService() {
        if (service) rt.app.stopService(Intent(rt.app, RyUIRecordService::class.java))
        service = false
    }

    fun methods(): Map<String, RyUIModuleMethod> = mapOf(
        "start" to { args, reply ->
            if (recorder != null) reply(Result.failure(Exception("already recording")))
            else {
                val b = ByteBuffer.wrap(args).order(ByteOrder.LITTLE_ENDIAN)
                val quality = if (b.remaining() >= 1) b.get().toInt() else 0
                val max = if (b.remaining() >= 4) b.int else 0
                val flags = if (b.remaining() >= 1) b.get().toInt() else 0
                fun str(): String = if (b.remaining() < 4) "" else ByteArray(b.int).also { b.get(it) }.toString(Charsets.UTF_8)
                title = str().ifEmpty { "Recording" }
                val path = str()
                rt.withPermission(arrayOf(Manifest.permission.RECORD_AUDIO)) { ok ->
                    if (!ok) reply(Result.failure(Exception("not allowed")))
                    else start(quality, max, flags and 1 != 0, flags and 2 != 0, path, reply)
                }
            }
        },
        "stop" to { _, reply -> finish(null); reply(Result.success(ByteArray(0))) },
        "pause" to { _, reply ->
            runCatching { recorder?.pause() }
            if (since != 0L) kept += SystemClock.elapsedRealtime() - since
            since = 0
            reply(Result.success(ByteArray(0)))
        },
        "resume" to { _, reply ->
            runCatching { recorder?.resume() }
            if (recorder != null && since == 0L) since = SystemClock.elapsedRealtime()
            reply(Result.success(ByteArray(0)))
        },
        "level" to { _, reply -> reply(Result.success(byteArrayOf(level()))) },
        "status" to { _, reply ->
            val out = ByteBuffer.allocate(6).order(ByteOrder.LITTLE_ENDIAN)
            out.put(level())
            out.put(if (recorder != null && since != 0L) 1 else 0)
            out.putInt((kept + if (since != 0L) SystemClock.elapsedRealtime() - since else 0L).toInt())
            reply(Result.success(out.array()))
        },
    )

    private fun start(quality: Int, maxMillis: Int, background: Boolean, stream: Boolean, path: String, reply: (Result<ByteArray>) -> Unit) {
        val music = quality == 1
        val dir = File(rt.app.cacheDir, "ryui-audio").apply { mkdirs() }
        val f = if (path.isNotEmpty()) File(path) else File(dir, java.util.UUID.randomUUID().toString() + if (stream) ".aac" else ".m4a")
        // While the app is on screen: Android doesn't let one start later.
        if (background) runCatching { startService() }
        val r = if (Build.VERSION.SDK_INT >= 31) MediaRecorder(rt.app) else @Suppress("DEPRECATION") MediaRecorder()
        try {
            r.setAudioSource(MediaRecorder.AudioSource.MIC)
            r.setOutputFormat(if (stream) MediaRecorder.OutputFormat.AAC_ADTS else MediaRecorder.OutputFormat.MPEG_4)
            r.setAudioEncoder(MediaRecorder.AudioEncoder.AAC)
            r.setAudioSamplingRate(if (music) 44100 else 22050)
            r.setAudioChannels(if (music) 2 else 1)
            r.setAudioEncodingBitRate(if (music) 128_000 else 64_000)
            if (maxMillis > 0) r.setMaxDuration(maxMillis)
            r.setOutputFile(f.absolutePath)
            r.setOnInfoListener { _, what, _ ->
                if (what == MediaRecorder.MEDIA_RECORDER_INFO_MAX_DURATION_REACHED) rt.onMain { finish(null) }
            }
            r.setOnErrorListener { _, what, _ -> rt.onMain { finish(Exception("the recording failed ($what)")) } }
            r.prepare()
            r.start()
        } catch (e: Exception) {
            r.release()
            stopService()
            reply(Result.failure(e))
            return
        }
        kept = 0
        since = SystemClock.elapsedRealtime()
        recorder = r
        file = f
        done = reply
    }

    /** Stops the recording (if any) and answers start: the file, or err. */
    private fun finish(err: Exception?) {
        val r = recorder ?: return
        recorder = null
        since = 0
        stopService()
        val stopped = runCatching { r.stop() }.isSuccess // fails if nothing was recorded
        r.release()
        val d = done
        done = null
        val f = file
        file = null
        when {
            err != null -> d?.invoke(Result.failure(err))
            !stopped || f == null -> { f?.delete(); d?.invoke(Result.failure(Exception("nothing was recorded"))) }
            else -> d?.invoke(Result.success(f.absolutePath.toByteArray()))
        }
    }
}

/** The foreground service that keeps a recording going out of sight. */
class RyUIRecordService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == "dev.ryui.record.stop") {
            RyUIAudioRecorder.current?.stopFromNotification()
            return START_NOT_STICKY
        }
        val open = packageManager.getLaunchIntentForPackage(packageName)
        val stop = PendingIntent.getService(this, 0, Intent(this, RyUIRecordService::class.java).setAction("dev.ryui.record.stop"),
            PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
        val b = NotificationCompat.Builder(this, RyUIAudioRecorder.CHANNEL)
            .setSmallIcon(applicationInfo.icon)
            .setContentTitle(RyUIAudioRecorder.title)
            .setOngoing(true)
            .setSilent(true)
            .setUsesChronometer(true)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .addAction(0, "Stop", stop)
        if (open != null) {
            b.setContentIntent(PendingIntent.getActivity(this, 0, open, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
        }
        val type = if (Build.VERSION.SDK_INT >= 30) ServiceInfo.FOREGROUND_SERVICE_TYPE_MICROPHONE else 0
        ServiceCompat.startForeground(this, RyUIAudioRecorder.NOTIFICATION, b.build(), type)
        return START_NOT_STICKY
    }
}
