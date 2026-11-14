// asuka/sdk/lottie's player on Android: lottie-android's LottieAnimationView,
// driven as lottieplayer's props say (lottieplayer.go). It reports the
// progress from the animator's updates (Report), and runs seeks that
// animate (ui.Animate's duration and curve) on the Choreographer's frames.
package com.github.egoist.asuka.sdk.lottie.internal.lottieplayer

import android.animation.Animator
import android.animation.AnimatorListenerAdapter
import android.graphics.Color
import android.graphics.PorterDuff
import android.graphics.PorterDuffColorFilter
import android.net.Uri
import android.os.Handler
import android.os.Looper
import android.view.Choreographer
import android.widget.ImageView
import com.airbnb.lottie.LottieAnimationView
import com.airbnb.lottie.LottieComposition
import com.airbnb.lottie.LottieCompositionFactory
import com.airbnb.lottie.LottieDrawable
import com.airbnb.lottie.LottieProperty
import com.airbnb.lottie.model.KeyPath
import com.airbnb.lottie.value.LottieValueCallback
import java.io.ByteArrayInputStream
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.Executors
import java.util.zip.ZipInputStream
import kotlin.math.abs
import kotlin.math.cos
import kotlin.math.exp
import kotlin.math.max
import kotlin.math.min
import kotlin.math.sin
import kotlin.math.sqrt

private val loader = Executors.newFixedThreadPool(2)
private val main = Handler(Looper.getMainLooper())

private class LottiePlayer(private val events: PlayerEvents) : Player {
    private val lottie = LottieAnimationView(events.context)
    override val view: android.view.View get() = lottie

    private var source = ""
    private var data = ByteArray(0)
    private var animationId = ""
    private var loop = false
    private var report = false
    private var playing = false
    private var speed = 1.0
    private var segment = FrameRange()
    private var marker = ""
    private var seekSerial = 0L
    private var pendingSeek: SeekTo? = null
    private var overrides: List<KeypathOverride> = emptyList()
    private var applied = ArrayList<Pair<KeyPath, Any>>()
    private var token = 0
    private var composition: LottieComposition? = null
    private var done = false
    private var cancelled = false
    private var tween: Tween? = null
    private var lastReported = -1.0

    private class Tween(val from: Float, val to: Float, val start: Long, val seconds: Double, val curve: Long, val damping: Double)

    init {
        lottie.scaleType = ImageView.ScaleType.FIT_CENTER
        // Reduce Motion is the Go side's to follow (asuka/sdk/lottie).
        lottie.setIgnoreDisabledSystemAnimations(true)
        lottie.addAnimatorUpdateListener { reportNow() }
        lottie.addAnimatorListener(object : AnimatorListenerAdapter() {
            override fun onAnimationStart(a: Animator) { cancelled = false }
            override fun onAnimationCancel(a: Animator) { cancelled = true }
            override fun onAnimationEnd(a: Animator) {
                if (cancelled || loop || composition == null) return
                done = true
                reportNow()
                events.completed()
            }
        })
    }

    // Loading

    override fun setSource(value: String) {
        source = value
        if (value.isNotEmpty()) load()
    }

    override fun setData(value: ByteArray) {
        data = value
        if (value.isNotEmpty() && source.isEmpty()) load()
    }

    override fun setAnimation(value: String) { animationId = value }

    private fun load() {
        val t = ++token
        val src = source
        val bytes = data
        val app = events.context.applicationContext
        val id = animationId
        loader.execute {
            val r = runCatching {
                val d = if (src.isEmpty()) bytes else read(app, src)
                val key = if (src.isEmpty()) "asuka-data-${d.contentHashCode()}" else "$src#$id"
                if (d.size >= 4 && d[0] == 0x50.toByte() && d[1] == 0x4B.toByte() && d[2] == 3.toByte() && d[3] == 4.toByte()) {
                    if (id.isEmpty()) LottieCompositionFactory.fromZipStreamSync(ZipInputStream(ByteArrayInputStream(d)), key)
                    else LottieCompositionFactory.fromJsonStringSync(dotLottieJson(d, id), key)
                } else LottieCompositionFactory.fromJsonInputStreamSync(ByteArrayInputStream(d), key)
            }
            main.post {
                if (t != token) return@post
                val c = r.getOrNull()?.value
                val e = r.exceptionOrNull() ?: r.getOrNull()?.exception
                if (c == null) {
                    events.failed(e?.message ?: "not a Lottie animation")
                    return@post
                }
                loaded(c)
            }
        }
    }

    /** A dotLottie's animation by its ID (dotLottie 1's animations/, 2's a/). */
    private fun dotLottieJson(zip: ByteArray, id: String): String {
        ZipInputStream(ByteArrayInputStream(zip)).use { z ->
            while (true) {
                val e = z.nextEntry ?: break
                val n = e.name.trimStart('/')
                if (n == "animations/$id.json" || n == "a/$id.json") return z.readBytes().toString(Charsets.UTF_8)
            }
        }
        error("the dotLottie has no animation \"$id\"")
    }

    private fun read(app: android.content.Context, src: String): ByteArray {
        if (src.startsWith("http://") || src.startsWith("https://")) {
            val c = URL(src).openConnection() as HttpURLConnection
            try {
                c.connectTimeout = 15000
                c.readTimeout = 30000
                if (c.responseCode !in 200..299) error("$src: HTTP ${c.responseCode}")
                return c.inputStream.use { it.readBytes() }
            } finally {
                c.disconnect()
            }
        }
        if (src.startsWith("/")) return File(src).readBytes()
        if (src.startsWith("file://")) return File(Uri.parse(src).path!!).readBytes()
        // In development `asuka dev` pushes the assets into the app's files.
        val dev = File(app.filesDir, "asuka/assets/$src")
        if (dev.exists()) return dev.readBytes()
        return app.assets.open(src).use { it.readBytes() }
    }

    private fun loaded(c: LottieComposition) {
        composition = c
        lottie.setComposition(c)
        lottie.speed = abs(speed).toFloat()
        applyOverrides()
        val b = c.bounds
        events.loaded(AnimationInfo(
            width = b.width().toDouble(), height = b.height().toDouble(),
            start = c.startFrame.toDouble(), end = c.endFrame.toDouble(), frameRate = c.frameRate.toDouble(),
            markers = c.markers.map { MarkerInfo(it.name, it.startFrame.toDouble(), it.durationFrames.toDouble()) },
        ))
        done = false
        setFrame(span().first)
        pendingSeek?.let { pendingSeek = null; seekSerial = 0; setSeek(it) }
        if (playing) play() else reportNow()
    }

    // Playing

    /** The range it plays, in frames: a marker's, the segment, or all of it. */
    private fun span(): Pair<Double, Double> {
        val c = composition ?: return 0.0 to 0.0
        var from = c.startFrame.toDouble()
        var to = c.endFrame.toDouble()
        if (marker.isNotEmpty()) c.getMarker(marker)?.let { return it.startFrame.toDouble() to (it.startFrame + it.durationFrames).toDouble() }
        if (segment.to > segment.from) {
            from = max(segment.from, from)
            to = min(segment.to, to)
        }
        return from to to
    }

    private fun progressOf(frame: Double): Float {
        val c = composition ?: return 0f
        val n = c.endFrame - c.startFrame
        return if (n <= 0) 0f else ((frame - c.startFrame) / n).toFloat().coerceIn(0f, 1f)
    }

    private fun setFrame(f: Double) { lottie.progress = progressOf(f) }

    private fun frameNow(): Double {
        val c = composition ?: return 0.0
        return c.startFrame + lottie.progress * (c.endFrame - c.startFrame).toDouble()
    }

    private fun play() {
        if (composition == null || tween != null) return
        val (from, to) = span()
        lottie.setMinAndMaxProgress(progressOf(from), progressOf(to))
        lottie.repeatCount = if (loop) LottieDrawable.INFINITE else 0
        lottie.speed = (if (speed < 0) -abs(speed) else abs(speed)).toFloat()
        val at = frameNow()
        val atEnd = if (speed < 0) at <= from + 0.001 else at >= to - 0.001
        if (done || atEnd || at < from || at > to) {
            done = false
            lottie.playAnimation() // from the range's start (its end, backwards)
        } else lottie.resumeAnimation()
    }

    override fun setPlaying(value: Boolean) {
        playing = value
        if (composition == null) return
        if (value) play() else { lottie.pauseAnimation(); reportNow() }
    }

    override fun setLoop(value: Boolean) {
        loop = value
        lottie.repeatCount = if (value) LottieDrawable.INFINITE else 0
    }

    override fun setReport(value: Boolean) { report = value }

    override fun setSpeed(value: Double) {
        speed = if (value == 0.0) 1.0 else value
        lottie.speed = speed.toFloat()
    }

    override fun setSegment(value: FrameRange) {
        segment = value
        rangeChanged()
    }

    override fun setMarker(value: String) {
        marker = value
        rangeChanged()
    }

    private fun rangeChanged() {
        if (composition == null) return
        val (from, to) = span()
        done = false
        tween = null
        lottie.setMinAndMaxProgress(progressOf(from), progressOf(to))
        setFrame(if (speed < 0) to else from)
        if (playing) play() else reportNow()
    }

    override fun setSeek(value: SeekTo) {
        if (value.serial == seekSerial) return
        val c = composition
        if (c == null) { pendingSeek = value; return }
        seekSerial = value.serial
        val (from, to) = span()
        var target = c.startFrame + value.progress * (c.endFrame - c.startFrame)
        if (value.to == 1L) target = from
        if (value.to == 2L) target = to
        done = false
        // A seek may go outside the range played: open it to the whole.
        lottie.setMinAndMaxProgress(0f, 1f)
        if (value.duration > 0) {
            val at = frameNow()
            lottie.pauseAnimation()
            tween = Tween(at.toFloat(), target.toFloat(), System.nanoTime(), value.duration, value.curve, value.damping)
            Choreographer.getInstance().postFrameCallback(frame)
            return
        }
        tween = null
        lottie.pauseAnimation()
        setFrame(target)
        if (playing) play() else reportNow()
    }

    private val frame = object : Choreographer.FrameCallback {
        override fun doFrame(now: Long) {
            val tw = tween ?: return
            val x = (System.nanoTime() - tw.start) / 1e9 / tw.seconds
            if (x >= 1) {
                tween = null
                setFrame(tw.to.toDouble())
                reportNow()
                if (playing) play()
                return
            }
            setFrame(tw.from + (tw.to - tw.from) * ease(tw.curve, tw.damping, x, tw.seconds))
            reportNow()
            Choreographer.getInstance().postFrameCallback(this)
        }
    }

    private fun reportNow() {
        if (!report || composition == null) return
        val p = lottie.progress.toDouble()
        if (p != lastReported) {
            lastReported = p
            events.progress(p)
        }
    }

    // Overrides

    override fun setOverrides(value: List<KeypathOverride>) {
        overrides = value
        if (composition != null) applyOverrides()
    }

    /**
     * Sets the overrides' value callbacks, clearing the last ones: a color
     * filter on what's under the keypath, an opacity on what it names, a
     * width on the strokes under it.
     */
    @Suppress("UNCHECKED_CAST")
    private fun applyOverrides() {
        for ((k, p) in applied) lottie.addValueCallback(k, p, null as LottieValueCallback<Any>?)
        applied.clear()
        for (o in overrides) {
            if (o.keypath.isEmpty()) continue
            val parts = o.keypath.split(".").toTypedArray()
            val self = KeyPath(*parts)
            val under = KeyPath(*parts, "**")
            when (o.kind) {
                0L -> {
                    val c = Color.argb((o.a * 255).toInt(), (o.r * 255).toInt(), (o.g * 255).toInt(), (o.b * 255).toInt())
                    val cb = LottieValueCallback(PorterDuffColorFilter(c, PorterDuff.Mode.SRC_ATOP) as android.graphics.ColorFilter)
                    for (k in listOf(self, under)) {
                        lottie.addValueCallback(k, LottieProperty.COLOR_FILTER, cb)
                        applied.add(k to LottieProperty.COLOR_FILTER)
                    }
                }
                1L -> {
                    val v = LottieValueCallback((o.value * 100).toInt())
                    lottie.addValueCallback(self, LottieProperty.TRANSFORM_OPACITY, v)
                    lottie.addValueCallback(self, LottieProperty.OPACITY, v)
                    applied.add(self to LottieProperty.TRANSFORM_OPACITY)
                    applied.add(self to LottieProperty.OPACITY)
                }
                else -> {
                    val v = LottieValueCallback(o.value.toFloat())
                    for (k in listOf(self, under)) {
                        lottie.addValueCallback(k, LottieProperty.STROKE_WIDTH, v)
                        applied.add(k to LottieProperty.STROKE_WIDTH)
                    }
                }
            }
        }
    }

    override fun setMode(value: Long) {
        lottie.scaleType = when (value) {
            1L -> ImageView.ScaleType.CENTER_CROP
            2L -> ImageView.ScaleType.FIT_XY
            3L -> ImageView.ScaleType.CENTER
            else -> ImageView.ScaleType.FIT_CENTER
        }
    }
}

/** A seek's curve at x (0 to 1) of its time: ui.Curve's, as package lottie's Go computes it. */
private fun ease(curve: Long, damping: Double, x: Double, seconds: Double): Double {
    fun bezier(x1: Double, y1: Double, x2: Double, y2: Double): Double {
        var lo = 0.0
        var hi = 1.0
        var t = x
        repeat(30) {
            val u = 1 - t
            val v = 3 * u * u * t * x1 + 3 * u * t * t * x2 + t * t * t
            if (abs(v - x) < 1e-6) return@repeat
            if (v < x) lo = t else hi = t
            t = (lo + hi) / 2
        }
        val u = 1 - t
        return 3 * u * u * t * y1 + 3 * u * t * t * y2 + t * t * t
    }
    return when (curve) {
        1L -> x
        2L -> bezier(0.42, 0.0, 1.0, 1.0)
        3L -> bezier(0.0, 0.0, 0.58, 1.0)
        4L -> {
            val z = damping.coerceIn(0.01, 1.0)
            val w = 10 / max(seconds, 0.01)
            val t = x * seconds
            if (z >= 1) 1 - exp(-w * t) * (1 + w * t)
            else {
                val wd = w * sqrt(1 - z * z)
                1 - exp(-z * w * t) * (cos(wd * t) + z * w / wd * sin(wd * t))
            }
        }
        else -> bezier(0.42, 0.0, 0.58, 1.0)
    }
}

fun makePlayer(events: PlayerEvents): Player = LottiePlayer(events)
