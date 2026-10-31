package dev.ryui

import android.net.Uri
import androidx.media3.common.AudioAttributes
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.Player
import androidx.media3.exoplayer.ExoPlayer
import java.io.File
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * ryui/audio's module: players by ID (each call's args begin with it, a
 * u32-length string): load (the source), play, playOnce, pause, seek (f64
 * seconds), volume (f64), loop (u8), release. Media3's ExoPlayer, as a
 * Video's, with music's audio attributes.
 */
fun RyUIRuntime.audioMethods(): Map<String, RyUIModuleMethod> {
    val players = HashMap<String, ExoPlayer>()
    fun split(b: ByteArray): Pair<String, ByteBuffer> {
        val r = ByteBuffer.wrap(b).order(ByteOrder.LITTLE_ENDIAN)
        if (r.remaining() < 4) return "" to r
        val id = ByteArray(r.int).also { r.get(it) }.toString(Charsets.UTF_8)
        return id to r.slice().order(ByteOrder.LITTLE_ENDIAN)
    }
    fun release(id: String) { players.remove(id)?.release() }
    fun method(f: (String, ByteBuffer) -> Unit): RyUIModuleMethod = { args, reply ->
        val (id, rest) = split(args)
        f(id, rest)
        reply(Result.success(ByteArray(0)))
    }
    return mapOf(
        "load" to method { id, rest ->
            val source = ByteArray(rest.remaining()).also { rest.get(it) }.toString(Charsets.UTF_8)
            val uri = when {
                source.startsWith("http://") || source.startsWith("https://") || source.startsWith("file://") -> Uri.parse(source)
                source.startsWith("/") -> Uri.fromFile(File(source))
                File(app.filesDir, "ryui/assets/$source").exists() -> Uri.fromFile(File(app.filesDir, "ryui/assets/$source"))
                else -> Uri.parse("asset:///$source")
            }
            val p = ExoPlayer.Builder(app)
                .setAudioAttributes(AudioAttributes.Builder().setUsage(C.USAGE_MEDIA).setContentType(C.AUDIO_CONTENT_TYPE_MUSIC).build(), true)
                .build()
            p.setMediaItem(MediaItem.fromUri(uri))
            p.prepare()
            players[id] = p
        },
        "play" to method { id, _ -> players[id]?.play() },
        "playOnce" to method { id, _ ->
            val p = players[id] ?: return@method
            p.addListener(object : Player.Listener {
                override fun onPlaybackStateChanged(state: Int) {
                    if (state == Player.STATE_ENDED) release(id)
                }
            })
            p.play()
        },
        "pause" to method { id, _ -> players[id]?.pause() },
        "seek" to method { id, rest -> if (rest.remaining() >= 8) players[id]?.seekTo((rest.double * 1000).toLong()) },
        "volume" to method { id, rest -> if (rest.remaining() >= 8) players[id]?.volume = rest.double.toFloat() },
        "loop" to method { id, rest -> players[id]?.repeatMode = if (rest.remaining() > 0 && rest.get().toInt() == 1) Player.REPEAT_MODE_ONE else Player.REPEAT_MODE_OFF },
        "release" to method { id, _ -> release(id) },
    )
}
