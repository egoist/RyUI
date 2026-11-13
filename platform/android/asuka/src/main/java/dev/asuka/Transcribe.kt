package dev.asuka

import android.Manifest
import android.content.Intent
import android.media.AudioFormat
import android.media.MediaCodec
import android.media.MediaExtractor
import android.media.MediaFormat
import android.os.Build
import android.os.Bundle
import android.os.ParcelFileDescriptor
import android.speech.RecognitionListener
import android.speech.RecognizerIntent
import android.speech.SpeechRecognizer
import org.json.JSONArray
import org.json.JSONObject
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.util.Locale

/**
 * asuka/sdk/transcribe's module: "check" (the language) answers JSON onDevice,
 * language; "file" (JSON path, language) answers JSON text, segments,
 * language. Android 13's on-device recognizer, in a segmented session, fed
 * the file decoded to 16 kHz mono: only ever on the device.
 */
class AsukaTranscribe(private val rt: AsukaRuntime) {
    private var busy = false

    private fun available() = SpeechRecognizer.isOnDeviceRecognitionAvailable(rt.app)

    fun methods(): Map<String, AsukaModuleMethod> = mapOf(
        "check" to { args, reply ->
            val language = String(args, Charsets.UTF_8).ifEmpty { Locale.getDefault().toLanguageTag() }
            reply(Result.success(JSONObject().put("onDevice", available()).put("language", language).toString().toByteArray()))
        },
        "file" to { args, reply ->
            val o = JSONObject(String(args, Charsets.UTF_8))
            val language = o.optString("language").ifEmpty { Locale.getDefault().toLanguageTag() }
            when {
                !available() -> reply(Result.failure(Exception("not available")))
                busy -> reply(Result.failure(Exception("already transcribing")))
                else -> rt.withPermission(arrayOf(Manifest.permission.RECORD_AUDIO)) { ok ->
                    if (!ok) reply(Result.failure(Exception("not allowed"))) else run(o.getString("path"), language, reply)
                }
            }
        },
    )

    private fun run(path: String, language: String, reply: (Result<ByteArray>) -> Unit) {
        busy = true
        val pipe = ParcelFileDescriptor.createPipe()
        val sr = SpeechRecognizer.createOnDeviceSpeechRecognizer(rt.app)
        val intent = Intent(RecognizerIntent.ACTION_RECOGNIZE_SPEECH)
            .putExtra(RecognizerIntent.EXTRA_LANGUAGE_MODEL, RecognizerIntent.LANGUAGE_MODEL_FREE_FORM)
            .putExtra(RecognizerIntent.EXTRA_LANGUAGE, language)
            .putExtra(RecognizerIntent.EXTRA_AUDIO_SOURCE, pipe[0])
            .putExtra(RecognizerIntent.EXTRA_AUDIO_SOURCE_CHANNEL_COUNT, 1)
            .putExtra(RecognizerIntent.EXTRA_AUDIO_SOURCE_ENCODING, AudioFormat.ENCODING_PCM_16BIT)
            .putExtra(RecognizerIntent.EXTRA_AUDIO_SOURCE_SAMPLING_RATE, 16000)
            .putExtra(RecognizerIntent.EXTRA_SEGMENTED_SESSION, RecognizerIntent.EXTRA_AUDIO_SOURCE)
        if (Build.VERSION.SDK_INT >= 34) intent.putExtra(RecognizerIntent.EXTRA_REQUEST_WORD_TIMING, true)

        val text = StringBuilder()
        val segments = JSONArray()
        fun finish(err: Exception?) {
            if (!busy) return
            busy = false
            runCatching { sr.destroy() }
            runCatching { pipe[0].close() }
            if (err != null) reply(Result.failure(err))
            else reply(Result.success(JSONObject().put("text", text.toString().trim()).put("segments", segments)
                .put("language", language).toString().toByteArray()))
        }
        fun add(results: Bundle) {
            results.getStringArrayList(SpeechRecognizer.RESULTS_RECOGNITION)?.firstOrNull()?.takeIf { it.isNotBlank() }?.let {
                if (text.isNotEmpty()) text.append(' ')
                text.append(it.trim())
            }
            if (Build.VERSION.SDK_INT >= 34) {
                val parts = results.getParcelableArrayList(SpeechRecognizer.RECOGNITION_PARTS, android.speech.RecognitionPart::class.java)
                parts?.forEach { p ->
                    segments.put(JSONObject().put("text", p.formattedText ?: p.rawText).put("start", p.timestampMillis / 1000.0).put("duration", 0.0))
                }
            }
        }
        sr.setRecognitionListener(object : RecognitionListener {
            override fun onReadyForSpeech(params: Bundle?) {}
            override fun onBeginningOfSpeech() {}
            override fun onRmsChanged(rmsdB: Float) {}
            override fun onBufferReceived(buffer: ByteArray?) {}
            override fun onEndOfSpeech() {}
            override fun onPartialResults(partialResults: Bundle?) {}
            override fun onEvent(eventType: Int, params: Bundle?) {}
            override fun onSegmentResults(results: Bundle) { add(results) }
            override fun onEndOfSegmentedSession() { finish(null) }
            override fun onResults(results: Bundle?) { results?.let { add(it) }; finish(null) }
            override fun onError(error: Int) {
                when (error) {
                    SpeechRecognizer.ERROR_NO_MATCH, SpeechRecognizer.ERROR_SPEECH_TIMEOUT -> finish(null)
                    // Often only that its pack isn't here yet: ask for it, for next time.
                    SpeechRecognizer.ERROR_LANGUAGE_NOT_SUPPORTED -> {
                        runCatching { sr.triggerModelDownload(intent) }
                        finish(Exception("not available"))
                    }
                    SpeechRecognizer.ERROR_LANGUAGE_UNAVAILABLE -> {
                        runCatching { sr.triggerModelDownload(intent) }
                        finish(Exception("downloading"))
                    }
                    else -> finish(Exception("the recognizer failed ($error)"))
                }
            }
        })
        sr.startListening(intent)
        Thread { decode(path, pipe[1]) { e -> rt.onMain { finish(e) } } }.start()
    }

    /** Writes the file to out as 16-bit, 16 kHz mono PCM, and closes it. */
    private fun decode(path: String, out: ParcelFileDescriptor, failed: (Exception) -> Unit) {
        val extractor = MediaExtractor()
        var codec: MediaCodec? = null
        try {
            ParcelFileDescriptor.AutoCloseOutputStream(out).use { stream ->
                extractor.setDataSource(path)
                val track = (0 until extractor.trackCount).first { extractor.getTrackFormat(it).getString(MediaFormat.KEY_MIME)?.startsWith("audio/") == true }
                extractor.selectTrack(track)
                val format = extractor.getTrackFormat(track)
                codec = MediaCodec.createDecoderByType(format.getString(MediaFormat.KEY_MIME)!!).apply { configure(format, null, null, 0); start() }
                val c = codec!!
                var rate = format.getInteger(MediaFormat.KEY_SAMPLE_RATE)
                var channels = format.getInteger(MediaFormat.KEY_CHANNEL_COUNT)
                val info = MediaCodec.BufferInfo()
                var inputDone = false
                var position = 0.0 // in source samples, of the next output sample
                var sourceIndex = 0L
                var last = 0
                while (true) {
                    if (!inputDone) {
                        val i = c.dequeueInputBuffer(10_000)
                        if (i >= 0) {
                            val n = extractor.readSampleData(c.getInputBuffer(i)!!, 0)
                            if (n < 0) {
                                c.queueInputBuffer(i, 0, 0, 0, MediaCodec.BUFFER_FLAG_END_OF_STREAM)
                                inputDone = true
                            } else {
                                c.queueInputBuffer(i, 0, n, extractor.sampleTime, 0)
                                extractor.advance()
                            }
                        }
                    }
                    val o = c.dequeueOutputBuffer(info, 10_000)
                    if (o == MediaCodec.INFO_OUTPUT_FORMAT_CHANGED) {
                        rate = c.outputFormat.getInteger(MediaFormat.KEY_SAMPLE_RATE)
                        channels = c.outputFormat.getInteger(MediaFormat.KEY_CHANNEL_COUNT)
                    } else if (o >= 0) {
                        val pcm = c.getOutputBuffer(o)!!.order(ByteOrder.LITTLE_ENDIAN).asShortBuffer()
                        val outBytes = ByteBuffer.allocate(pcm.remaining() * 2).order(ByteOrder.LITTLE_ENDIAN)
                        val step = rate / 16000.0
                        while (pcm.remaining() >= channels) {
                            var sum = 0
                            for (ch in 0 until channels) sum += pcm.get()
                            val sample = sum / channels
                            // Linear resampling: each output sample between last and this one.
                            while (position <= sourceIndex) {
                                val t = position - (sourceIndex - 1)
                                outBytes.putShort((last + (sample - last) * t).toInt().toShort())
                                position += step
                            }
                            last = sample
                            sourceIndex++
                        }
                        stream.write(outBytes.array(), 0, outBytes.position())
                        c.releaseOutputBuffer(o, false)
                        if (info.flags and MediaCodec.BUFFER_FLAG_END_OF_STREAM != 0) break
                    }
                }
            }
        } catch (e: java.io.IOException) {
            // The recognizer closed its end: it answers for itself.
        } catch (e: Exception) {
            failed(e)
        } finally {
            runCatching { codec?.stop() }
            runCatching { codec?.release() }
            extractor.release()
        }
    }
}
