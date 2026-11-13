package dev.asuka

import android.app.ForegroundServiceStartNotAllowedException
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.graphics.Bitmap
import android.media.MediaMetadata
import android.media.session.MediaSession
import android.media.session.PlaybackState
import android.os.Bundle
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.os.SystemClock
import org.json.JSONObject
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * asuka/sdk/nowplaying's module: a MediaSession, which the notification
 * shade's media controls, the lock screen, Bluetooth headsets, Android
 * Auto and Wear OS show and drive, with its media-style notification.
 * "set" (JSON: internal/nowplayingspec's Info) sets the session's
 * metadata and playback state (its actions the commands enabled); "clear"
 * releases it; "next" answers with the commands since (u8 command, f64
 * value each) once there are some; "stop" fails the waiting "next" with
 * "stopped". While playing, AsukaMediaService is a foreground service of
 * the media playback type, so the app runs on out of sight; paused, the
 * notification stays, and can be swiped away.
 */
class AsukaNowPlaying(private val rt: AsukaRuntime) {
    companion object {
        /** The notification channel, made once. */
        fun channel(ctx: Context): String {
            val nm = ctx.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            if (nm.getNotificationChannel(CHANNEL) == null) {
                nm.createNotificationChannel(NotificationChannel(CHANNEL, "Now playing", NotificationManager.IMPORTANCE_LOW).apply { setShowBadge(false) })
            }
            return CHANNEL
        }

        const val CHANNEL = "asuka.media"
        const val NOTIFICATION = 0x6e76_0002
        const val SKIP_FORWARD = "dev.asuka.skipForward"
        const val SKIP_BACKWARD = "dev.asuka.skipBackward"

        // The commands, by internal/nowplayingspec's numbers.
        const val PLAY = 1
        const val PAUSE = 2
        const val TOGGLE = 3
        const val STOP = 4
        const val NEXT = 5
        const val PREVIOUS = 6
        const val SEEK = 7
        const val FORWARD = 8
        const val BACKWARD = 9

        var current: AsukaNowPlaying? = null

        /** The PlaybackState actions of the commands enabled. */
        fun actions(commands: Long): Long {
            fun has(c: Int) = commands and (1L shl c) != 0L
            var a = 0L
            if (has(PLAY) || has(TOGGLE)) a = a or PlaybackState.ACTION_PLAY or PlaybackState.ACTION_PLAY_PAUSE
            if (has(PAUSE) || has(TOGGLE)) a = a or PlaybackState.ACTION_PAUSE or PlaybackState.ACTION_PLAY_PAUSE
            if (has(STOP)) a = a or PlaybackState.ACTION_STOP
            if (has(NEXT)) a = a or PlaybackState.ACTION_SKIP_TO_NEXT
            if (has(PREVIOUS)) a = a or PlaybackState.ACTION_SKIP_TO_PREVIOUS
            if (has(SEEK)) a = a or PlaybackState.ACTION_SEEK_TO
            if (has(FORWARD)) a = a or PlaybackState.ACTION_FAST_FORWARD
            if (has(BACKWARD)) a = a or PlaybackState.ACTION_REWIND
            return a
        }

        /** A "next" answer's command: u8 command, f64 value. */
        fun encode(c: Int, v: Double): ByteArray =
            ByteBuffer.allocate(9).order(ByteOrder.LITTLE_ENDIAN).put(c.toByte()).putDouble(v).array()
    }

    /** What shows (internal/nowplayingspec's Info). */
    class Info(j: JSONObject) {
        val title = j.optString("title")
        val artist = j.optString("artist")
        val album = j.optString("album")
        val artwork = j.optString("artwork")
        val duration = j.optDouble("duration", 0.0)
        val position = j.optDouble("position", 0.0)
        val rate = j.optDouble("rate", 1.0)
        val state = j.optString("state", "paused")
        val commands = j.optLong("commands", 0)
        val skip = j.optDouble("skip", 15.0)
        fun has(c: Int) = commands and (1L shl c) != 0L
    }

    private var session: MediaSession? = null
    private var info: Info? = null
    private var artSource = ""
    private var art: Bitmap? = null
    private var following = false
    private var waiting: ((Result<ByteArray>) -> Unit)? = null
    private var queue = ByteArray(0)
    private var service = false // AsukaMediaService was started

    init { current = this }

    fun methods(): Map<String, AsukaModuleMethod> = mapOf(
        "set" to { args, reply ->
            set(Info(JSONObject(String(args, Charsets.UTF_8))))
            reply(Result.success(ByteArray(0)))
        },
        "clear" to { _, reply ->
            clear()
            reply(Result.success(ByteArray(0)))
        },
        "next" to { _, reply ->
            waiting?.invoke(Result.failure(Exception("stopped"))) // one poll at a time
            following = true
            waiting = reply
            flush()
        },
        "stop" to { _, reply ->
            val w = waiting
            following = false
            waiting = null
            queue = ByteArray(0)
            w?.invoke(Result.failure(Exception("stopped")))
            reply(Result.success(ByteArray(0)))
        },
    )

    private fun command(c: Int, v: Double) {
        val i = info ?: return
        if (!following || !i.has(c)) return
        queue += encode(c, v)
        flush()
    }

    private fun flush() {
        val w = waiting ?: return
        if (queue.isEmpty()) return
        val b = queue
        waiting = null
        queue = ByteArray(0)
        w(Result.success(b))
    }

    private fun session(): MediaSession {
        session?.let { return it }
        val s = MediaSession(rt.app, "asuka")
        s.setCallback(object : MediaSession.Callback() {
            override fun onPlay() = command(PLAY, 0.0)
            override fun onPause() = command(PAUSE, 0.0)
            override fun onStop() = command(STOP, 0.0)
            override fun onSkipToNext() = command(NEXT, 0.0)
            override fun onSkipToPrevious() = command(PREVIOUS, 0.0)
            override fun onSeekTo(pos: Long) = command(SEEK, pos / 1000.0)
            override fun onFastForward() = command(FORWARD, info?.skip ?: 0.0)
            override fun onRewind() = command(BACKWARD, info?.skip ?: 0.0)
            override fun onCustomAction(action: String, extras: Bundle?) {
                when (action) {
                    SKIP_FORWARD -> command(FORWARD, info?.skip ?: 0.0)
                    SKIP_BACKWARD -> command(BACKWARD, info?.skip ?: 0.0)
                }
            }
        }, Handler(Looper.getMainLooper()))
        rt.app.packageManager.getLaunchIntentForPackage(rt.app.packageName)?.let {
            s.setSessionActivity(PendingIntent.getActivity(rt.app, 0, it, PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT))
        }
        session = s
        return s
    }

    private fun set(i: Info) {
        info = i
        val s = session()
        val playing = i.state == "playing"
        val state = when (i.state) {
            "playing" -> PlaybackState.STATE_PLAYING
            "stopped" -> PlaybackState.STATE_STOPPED
            else -> PlaybackState.STATE_PAUSED
        }
        val ps = PlaybackState.Builder()
            .setActions(actions(i.commands))
            // The system advances the position from now at the rate, while playing.
            .setState(state, (i.position * 1000).toLong(), if (i.rate > 0) i.rate.toFloat() else 1f, SystemClock.elapsedRealtime())
        // Skips fill the slots of previous and next when the app has none.
        if (i.has(BACKWARD)) ps.addCustomAction(PlaybackState.CustomAction.Builder(SKIP_BACKWARD, "Skip back", android.R.drawable.ic_media_rew).build())
        if (i.has(FORWARD)) ps.addCustomAction(PlaybackState.CustomAction.Builder(SKIP_FORWARD, "Skip forward", android.R.drawable.ic_media_ff).build())
        s.setPlaybackState(ps.build())
        if (i.artwork != artSource) {
            artSource = i.artwork
            art = null
            if (i.artwork.isNotEmpty()) {
                val src = i.artwork
                AsukaImages.load(rt.app, src) { r ->
                    if (artSource == src && info != null) {
                        art = r.getOrNull()?.bitmap
                        metadata()
                        notifyShown()
                    }
                }
            }
        }
        metadata()
        s.isActive = true
        if (playing) startForeground() else stopForeground()
        notifyShown()
    }

    private fun metadata() {
        val i = info ?: return
        val m = MediaMetadata.Builder()
            .putString(MediaMetadata.METADATA_KEY_TITLE, i.title)
            .putString(MediaMetadata.METADATA_KEY_ARTIST, i.artist)
            .putString(MediaMetadata.METADATA_KEY_ALBUM, i.album)
        if (i.duration > 0) m.putLong(MediaMetadata.METADATA_KEY_DURATION, (i.duration * 1000).toLong())
        art?.let { m.putBitmap(MediaMetadata.METADATA_KEY_ART, it) }
        session?.setMetadata(m.build())
    }

    private fun manager() = rt.app.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager

    /** Whether it plays: AsukaMediaService stays in the foreground only then. */
    val playing get() = info?.state == "playing"

    /** The media notification of what shows, or null. */
    fun notification(ctx: Context): Notification? {
        val i = info ?: return null
        val s = session ?: return null
        val b = Notification.Builder(ctx, channel(ctx))
            .setSmallIcon(ctx.applicationInfo.icon)
            .setContentTitle(i.title)
            .setContentText(i.artist)
            .setStyle(Notification.MediaStyle().setMediaSession(s.sessionToken))
            .setVisibility(Notification.VISIBILITY_PUBLIC)
            .setCategory(Notification.CATEGORY_TRANSPORT)
            .setOnlyAlertOnce(true)
            .setOngoing(i.state == "playing")
            .setForegroundServiceBehavior(Notification.FOREGROUND_SERVICE_IMMEDIATE)
        if (i.album.isNotEmpty()) b.setSubText(i.album)
        art?.let { b.setLargeIcon(it) }
        s.controller.sessionActivity?.let { b.setContentIntent(it) }
        return b.build()
    }

    /** Posts the notification again, as what shows changed. */
    private fun notifyShown() {
        val n = notification(rt.app) ?: return
        runCatching { manager().notify(NOTIFICATION, n) }
    }

    private fun startForeground() {
        if (service) return
        try {
            rt.app.startForegroundService(Intent(rt.app, AsukaMediaService::class.java))
            service = true
        } catch (_: ForegroundServiceStartNotAllowedException) {
            // In the background without an exemption: the notification
            // alone, as Media3 does.
        } catch (_: SecurityException) {
            // Not declared (an app built without asuka/sdk/nowplaying's manifest).
        }
    }

    /** AsukaMediaService left the foreground by itself (paused as it started). */
    fun serviceStopped() { service = false }

    private fun stopForeground() {
        if (!service) return
        service = false
        AsukaMediaService.running?.let {
            it.stopForeground(Service.STOP_FOREGROUND_DETACH)
            it.stopSelf()
        }
    }

    private fun clear() {
        info = null
        artSource = ""
        art = null
        if (service) {
            service = false
            AsukaMediaService.running?.let {
                it.stopForeground(Service.STOP_FOREGROUND_REMOVE)
                it.stopSelf()
            }
        }
        manager().cancel(NOTIFICATION)
        session?.let {
            it.isActive = false
            it.release()
        }
        session = null
    }
}

/** The foreground service that keeps the app playing out of sight. */
class AsukaMediaService : Service() {
    companion object {
        var running: AsukaMediaService? = null
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        running = this
    }

    override fun onDestroy() {
        if (running === this) running = null
        super.onDestroy()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        // Started for the foreground, it must go there, even if playback
        // paused or went meanwhile; then it leaves it at once.
        val np = AsukaNowPlaying.current
        val n = np?.notification(this)
            ?: Notification.Builder(this, AsukaNowPlaying.channel(this)).setSmallIcon(applicationInfo.icon).build()
        startForeground(AsukaNowPlaying.NOTIFICATION, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_MEDIA_PLAYBACK)
        if (np?.playing != true) {
            stopForeground(if (np?.notification(this) != null) STOP_FOREGROUND_DETACH else STOP_FOREGROUND_REMOVE)
            stopSelf()
            np?.serviceStopped()
        }
        return START_NOT_STICKY
    }
}
