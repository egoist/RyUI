package dev.nui

import android.annotation.SuppressLint
import android.net.Uri
import android.widget.FrameLayout
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.Tracks
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.VideoSize
import androidx.media3.common.util.UnstableApi
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.ui.AspectRatioFrameLayout
import androidx.media3.ui.PlayerView
import java.io.File

/**
 * A Video: Media3's ExoPlayer in its PlayerView (its controls), reporting
 * the video's size, its end and its errors.
 */
@SuppressLint("ViewConstructor")
@androidx.annotation.OptIn(UnstableApi::class)
class NuiVideo(private val rt: NuiRuntime, private val nuiID: Int) : FrameLayout(rt.themed) {
    val player: ExoPlayer = ExoPlayer.Builder(rt.app).build()
    private val view = (android.view.LayoutInflater.from(rt.themed).inflate(R.layout.nui_player, this, false) as PlayerView).apply {
        player = this@NuiVideo.player
        setShowBuffering(PlayerView.SHOW_BUFFERING_WHEN_PLAYING)
    }
    var autoPlay = false
    var loop = false
        set(v) { field = v; player.repeatMode = if (v) Player.REPEAT_MODE_ONE else Player.REPEAT_MODE_OFF }

    init {
        addView(view, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.MATCH_PARENT))
        player.addListener(object : Player.Listener {
            override fun onVideoSizeChanged(size: VideoSize) {
                if (size.width == 0 || size.height == 0) return
                report(size.width * size.pixelWidthHeightRatio.toDouble(), size.height.toDouble())
            }

            // The size from the track, known before the video draws: a
            // view of no size (until it's known) has no surface to draw on.
            override fun onTracksChanged(tracks: Tracks) {
                for (g in tracks.groups) {
                    if (g.type != C.TRACK_TYPE_VIDEO || !g.isSelected) continue
                    for (i in 0 until g.length) {
                        if (!g.isTrackSelected(i)) continue
                        val f = g.getTrackFormat(i)
                        if (f.width <= 0 || f.height <= 0) continue
                        val turned = f.rotationDegrees == 90 || f.rotationDegrees == 270
                        val w = f.width * f.pixelWidthHeightRatio.toDouble()
                        val h = f.height.toDouble()
                        if (turned) report(h, w) else report(w, h)
                        return
                    }
                }
            }

            override fun onPlaybackStateChanged(state: Int) {
                if (state == Player.STATE_ENDED) rt.emit(nuiID, NuiEventKind.ENDED) { NuiEvents.ended(nuiID) }
            }

            // Looping, an end is a jump back to the start.
            override fun onPositionDiscontinuity(old: Player.PositionInfo, new: Player.PositionInfo, reason: Int) {
                if (reason == Player.DISCONTINUITY_REASON_AUTO_TRANSITION) rt.emit(nuiID, NuiEventKind.ENDED) { NuiEvents.ended(nuiID) }
            }

            override fun onPlayerError(e: PlaybackException) {
                val m = e.message ?: e.errorCodeName
                rt.emit(nuiID, NuiEventKind.FAILED) { NuiEvents.failed(nuiID, m) }
            }
        })
    }

    private var reported = 0.0 to 0.0

    private fun report(w: Double, h: Double) {
        if (reported == w to h) return
        reported = w to h
        rt.emit(nuiID, NuiEventKind.LOADED) { NuiEvents.loaded(nuiID, w, h) }
    }

    /** Plays source: an http(s) URL, a file, or one of the app's assets. */
    fun load(source: String) {
        reported = 0.0 to 0.0
        if (source.isEmpty()) { player.clearMediaItems(); return }
        val uri = when {
            source.startsWith("http://") || source.startsWith("https://") || source.startsWith("file://") -> Uri.parse(source)
            source.startsWith("/") -> Uri.fromFile(File(source))
            // In development `nui dev` pushes the assets into the app's files.
            File(rt.app.filesDir, "nui/assets/$source").exists() -> Uri.fromFile(File(rt.app.filesDir, "nui/assets/$source"))
            else -> Uri.parse("asset:///$source")
        }
        player.setMediaItem(MediaItem.fromUri(uri))
        player.prepare()
        player.playWhenReady = autoPlay
    }

    fun setFill(fill: Boolean) {
        view.resizeMode = if (fill) AspectRatioFrameLayout.RESIZE_MODE_ZOOM else AspectRatioFrameLayout.RESIZE_MODE_FIT
    }

    fun setControls(on: Boolean) { view.useController = on }

    override fun onDetachedFromWindow() {
        super.onDetachedFromWindow()
        player.pause() // gone from the screen (popped, dismissed)
    }

    /** Frees the player: the view is destroyed. */
    fun release() = player.release()
}
