package dev.ryui

import android.Manifest
import android.media.MediaRecorder
import android.os.Build
import java.io.File
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * ryui/audio/record's module: "start" (u8 quality: voice, music; u32 max
 * milliseconds, 0: none) records AAC in MPEG-4 until "stop" and answers
 * with the file's path; "pause", "resume"; "level" answers u8, 0 to 255.
 */
class RyUIAudioRecorder(private val rt: RyUIRuntime) {
    private var recorder: MediaRecorder? = null
    private var file: File? = null
    private var done: ((Result<ByteArray>) -> Unit)? = null

    fun methods(): Map<String, RyUIModuleMethod> = mapOf(
        "start" to { args, reply ->
            if (recorder != null) reply(Result.failure(Exception("already recording")))
            else {
                val b = ByteBuffer.wrap(args).order(ByteOrder.LITTLE_ENDIAN)
                val quality = if (b.remaining() >= 1) b.get().toInt() else 0
                val max = if (b.remaining() >= 4) b.int else 0
                rt.withPermission(arrayOf(Manifest.permission.RECORD_AUDIO)) { ok ->
                    if (!ok) reply(Result.failure(Exception("not allowed"))) else start(quality, max, reply)
                }
            }
        },
        "stop" to { _, reply -> finish(null); reply(Result.success(ByteArray(0))) },
        "pause" to { _, reply -> runCatching { recorder?.pause() }; reply(Result.success(ByteArray(0))) },
        "resume" to { _, reply -> runCatching { recorder?.resume() }; reply(Result.success(ByteArray(0))) },
        "level" to { _, reply ->
            // The loudest sample since the last call, of 32767.
            val amp = runCatching { recorder?.maxAmplitude ?: 0 }.getOrDefault(0)
            reply(Result.success(byteArrayOf((minOf(amp, 32767) * 255 / 32767).toByte())))
        },
    )

    private fun start(quality: Int, maxMillis: Int, reply: (Result<ByteArray>) -> Unit) {
        val music = quality == 1
        val dir = File(rt.app.cacheDir, "ryui-audio").apply { mkdirs() }
        val f = File(dir, java.util.UUID.randomUUID().toString() + ".m4a")
        val r = if (Build.VERSION.SDK_INT >= 31) MediaRecorder(rt.app) else @Suppress("DEPRECATION") MediaRecorder()
        try {
            r.setAudioSource(MediaRecorder.AudioSource.MIC)
            r.setOutputFormat(MediaRecorder.OutputFormat.MPEG_4)
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
            reply(Result.failure(e))
            return
        }
        recorder = r
        file = f
        done = reply
    }

    /** Stops the recording (if any) and answers start: the file, or err. */
    private fun finish(err: Exception?) {
        val r = recorder ?: return
        recorder = null
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
