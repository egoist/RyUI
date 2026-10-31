package dev.ryui

import android.content.Context
import android.media.AudioAttributes
import android.media.AudioFocusRequest
import android.media.AudioManager
import android.os.Bundle
import android.speech.tts.TextToSpeech
import android.speech.tts.UtteranceProgressListener
import android.speech.tts.Voice
import org.json.JSONArray
import org.json.JSONObject
import java.util.Locale

/**
 * ryui/speech's module, with TextToSpeech: "speak" (JSON: id, text,
 * language, voice, pitch, rate, volume) answers once the utterance is
 * spoken, or fails with "stopped"; "stop", "pause", "resume"; "speaking"
 * (u8); "voices" (JSON). Utterances queue here: TextToSpeech can't pause,
 * so a pause stops at the word being spoken (onRangeStart) and a resume
 * speaks on from it. Other apps duck while it speaks (transient audio
 * focus).
 */
class RyUISpeech(private val rt: RyUIRuntime) {
    private class Utterance(val id: String, val text: String, val o: JSONObject, val reply: (Result<ByteArray>) -> Unit) {
        var offset = 0 // where to speak from: the word a pause stopped at
    }

    private var tts: TextToSpeech? = null
    private var ready = false
    private var failed: String? = null
    private val queue = ArrayDeque<Utterance>()
    private var paused = false
    // Each start of the engine's queue (a pause or stop ends it): callbacks
    // for an earlier one are stale. The engine's utterance IDs are id#gen.
    private var gen = 0
    private val waitingVoices = ArrayList<(Result<ByteArray>) -> Unit>()
    private val audio get() = rt.app.getSystemService(Context.AUDIO_SERVICE) as AudioManager
    private val attributes = AudioAttributes.Builder().setUsage(AudioAttributes.USAGE_ASSISTANCE_NAVIGATION_GUIDANCE)
        .setContentType(AudioAttributes.CONTENT_TYPE_SPEECH).build()
    private val focus = AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN_TRANSIENT_MAY_DUCK).setAudioAttributes(attributes).build()
    private var focused = false

    /** The engine, made the first time it's needed; it's ready a moment later. */
    private fun engine(): TextToSpeech = tts ?: TextToSpeech(rt.app) { status ->
        rt.onMain {
            if (status == TextToSpeech.SUCCESS) {
                ready = true
                tts?.setAudioAttributes(attributes)
                if (!paused) for (u in queue) enqueue(u)
            } else {
                failed = "no text-to-speech engine"
                for (u in queue) u.reply(Result.failure(Exception(failed)))
                queue.clear()
            }
            val vs = ArrayList(waitingVoices)
            waitingVoices.clear()
            for (v in vs) voices(v)
        }
    }.also { t ->
        tts = t
        t.setOnUtteranceProgressListener(object : UtteranceProgressListener() {
            override fun onStart(id: String) {}
            override fun onDone(id: String) = rt.onMain { current(id)?.let { done(it, Result.success(ByteArray(0))) } }
            @Deprecated("the old signature") override fun onError(id: String) = rt.onMain { current(id)?.let { done(it, Result.failure(Exception("speech failed"))) } }
            override fun onError(id: String, code: Int) = rt.onMain { current(id)?.let { done(it, Result.failure(Exception("speech failed ($code)"))) } }
            // Stopped by a pause or stop: a stale generation, handled there.
            override fun onStop(id: String, interrupted: Boolean) = rt.onMain { current(id)?.let { done(it, Result.failure(Exception("stopped"))) } }
            // The word being spoken: where a pause resumes from.
            override fun onRangeStart(id: String, start: Int, end: Int, frame: Int) = rt.onMain {
                val u = current(id)?.let { uid -> queue.firstOrNull { it.id == uid } } ?: return@onMain
                u.offset = (spokenFrom[u.id] ?: 0) + start
            }
        })
    }

    /** Where each utterance's text, as given to the engine, starts in its full text. */
    private val spokenFrom = HashMap<String, Int>()

    private fun enqueue(u: Utterance) {
        val t = tts ?: return
        val o = u.o
        val voice = o.optString("voice")
        val lang = o.optString("language")
        val chosen = voice.takeIf { it.isNotEmpty() }?.let { v -> t.voices?.firstOrNull { it.name == v } }
        when {
            chosen != null -> t.voice = chosen
            lang.isNotEmpty() -> t.language = Locale.forLanguageTag(lang)
            else -> t.language = Locale.getDefault()
        }
        t.setPitch(o.optDouble("pitch", 1.0).toFloat().coerceIn(0.5f, 2f))
        t.setSpeechRate(o.optDouble("rate", 1.0).toFloat().coerceIn(0.5f, 2f))
        val params = Bundle().apply { putFloat(TextToSpeech.Engine.KEY_PARAM_VOLUME, o.optDouble("volume", 1.0).toFloat().coerceIn(0f, 1f)) }
        if (!focused) focused = audio.requestAudioFocus(focus) == AudioManager.AUDIOFOCUS_REQUEST_GRANTED
        spokenFrom[u.id] = u.offset
        t.speak(u.text.substring(u.offset), TextToSpeech.QUEUE_ADD, params, u.id + "#" + gen)
    }

    /** An engine utterance ID's utterance, if it's of the current generation. */
    private fun current(engineID: String): String? {
        val i = engineID.lastIndexOf('#')
        if (i < 0 || engineID.substring(i + 1).toIntOrNull() != gen) return null
        return engineID.substring(0, i)
    }

    private fun done(id: String, r: Result<ByteArray>) {
        val i = queue.indexOfFirst { it.id == id }
        if (i < 0) return
        val u = queue.removeAt(i)
        spokenFrom.remove(id)
        u.reply(r)
        if (queue.isEmpty() && focused) { audio.abandonAudioFocusRequest(focus); focused = false }
    }

    /** Stops the engine's queue; its callbacks after are stale. */
    private fun halt() {
        gen++
        tts?.stop()
    }

    private fun voices(reply: (Result<ByteArray>) -> Unit) {
        val a = JSONArray()
        for (v in tts?.voices.orEmpty().sortedBy { it.name }) {
            val q = when { v.quality >= Voice.QUALITY_VERY_HIGH -> 2; v.quality >= Voice.QUALITY_HIGH -> 1; else -> 0 }
            a.put(JSONObject().put("id", v.name).put("name", v.name).put("language", v.locale.toLanguageTag())
                .put("quality", q).put("network", v.isNetworkConnectionRequired))
        }
        reply(Result.success(a.toString().toByteArray()))
    }

    fun methods(): Map<String, RyUIModuleMethod> = mapOf(
        "speak" to { args, reply ->
            val o = runCatching { JSONObject(String(args, Charsets.UTF_8)) }.getOrNull()
            when {
                o == null -> reply(Result.failure(Exception("bad args")))
                failed != null -> reply(Result.failure(Exception(failed)))
                else -> {
                    val u = Utterance(o.optString("id"), o.optString("text"), o, reply)
                    queue.addLast(u)
                    engine()
                    if (ready && !paused) enqueue(u)
                }
            }
        },
        "stop" to { _, reply ->
            val us = queue.toList()
            queue.clear()
            spokenFrom.clear()
            paused = false
            halt()
            for (u in us) u.reply(Result.failure(Exception("stopped")))
            if (focused) { audio.abandonAudioFocusRequest(focus); focused = false }
            reply(Result.success(ByteArray(0)))
        },
        "pause" to { _, reply ->
            if (!paused && queue.isNotEmpty()) {
                paused = true
                halt()
            }
            reply(Result.success(ByteArray(0)))
        },
        "resume" to { _, reply ->
            if (paused) {
                paused = false
                if (ready) for (u in queue) enqueue(u)
            }
            reply(Result.success(ByteArray(0)))
        },
        "speaking" to { _, reply ->
            reply(Result.success(byteArrayOf(if (queue.isNotEmpty()) 1 else 0)))
        },
        "voices" to { _, reply ->
            engine()
            if (ready || failed != null) voices(reply) else waitingVoices.add(reply)
        },
    )
}
