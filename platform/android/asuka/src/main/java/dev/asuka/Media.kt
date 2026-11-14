package dev.asuka

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Matrix
import androidx.exifinterface.media.ExifInterface
import java.io.ByteArrayInputStream
import android.graphics.drawable.GradientDrawable
import android.os.Handler
import android.os.Looper
import android.util.Log
import android.util.LruCache
import android.view.GestureDetector
import android.view.MotionEvent
import android.view.VelocityTracker
import android.view.View
import android.view.ViewConfiguration
import android.view.animation.Interpolator
import android.view.animation.LinearInterpolator
import android.view.animation.OvershootInterpolator
import android.view.animation.PathInterpolator
import android.widget.FrameLayout
import android.widget.ImageView
import java.io.File
import android.net.Uri
import java.net.HttpURLConnection
import java.net.URL
import java.nio.ByteBuffer
import java.util.concurrent.Executors
import kotlin.math.abs

// Images

/**
 * A decoded image and its natural size (pixels); an animated one (GIF,
 * WebP, AVIF and HEIF sequences: what ImageDecoder animates) keeps its
 * file, which each view plays as an AnimatedImageDrawable of its own.
 */
class AsukaImage(val bitmap: Bitmap, val width: Int, val height: Int, val animation: ByteArray? = null)

/**
 * Loads images off the main thread: fetched (http, https; kept on disk,
 * AsukaDiskCache) or read (the app's assets), decoded there (subsampled to
 * the screen's size), then cached in memory.
 */
object AsukaImages {
    private val cache = object : LruCache<String, AsukaImage>(maxOf(16 shl 20, (Runtime.getRuntime().maxMemory() / 8).toInt())) {
        override fun sizeOf(key: String, v: AsukaImage) = v.bitmap.byteCount
    }
    private val pool = Executors.newFixedThreadPool(3)
    private val main = Handler(Looper.getMainLooper())
    private val waiting = HashMap<String, ArrayList<(Result<AsukaImage>) -> Unit>>() // main thread only

    fun cached(source: String): AsukaImage? = cache.get(source)

    /** Calls done on the main thread. */
    fun load(context: Context, source: String, done: (Result<AsukaImage>) -> Unit) {
        cached(source)?.let { done(Result.success(it)); return }
        waiting[source]?.let { it.add(done); return }
        waiting[source] = arrayListOf(done)
        val app = context.applicationContext
        val maxSide = maxOf(app.resources.displayMetrics.widthPixels, app.resources.displayMetrics.heightPixels)
        pool.execute {
            val r = runCatching {
                if (source.startsWith("http://") || source.startsWith("https://")) {
                    val disk = AsukaDiskCache.images(app)
                    disk.read(source)?.let { d -> runCatching { decode(d, maxSide) }.getOrNull() }?.let { return@runCatching it }
                    val (data, storable) = fetch(source)
                    decode(data, maxSide).also { if (storable) disk.write(source, data) }
                } else decode(read(app, source), maxSide)
            }
            main.post {
                r.getOrNull()?.let { cache.put(source, it) }
                waiting.remove(source)?.forEach { it(r) }
            }
        }
    }

    /** An image over http(s), and whether it may be kept on disk: not one its response says not to keep, nor one from this machine (asuka dev's assets, which change). */
    private fun fetch(source: String): Pair<ByteArray, Boolean> {
        val url = URL(source)
        val c = url.openConnection() as HttpURLConnection
        try {
            c.connectTimeout = 15000
            c.readTimeout = 30000
            if (c.responseCode !in 200..299) error("HTTP ${c.responseCode}")
            val data = c.inputStream.use { it.readBytes() }
            val noStore = c.getHeaderField("Cache-Control")?.contains("no-store", ignoreCase = true) == true
            val local = url.host in setOf("localhost", "127.0.0.1", "::1", "10.0.2.2")
            return data to (!noStore && !local)
        } finally {
            c.disconnect()
        }
    }

    private fun read(app: Context, source: String): ByteArray {
        // A file (a picked photo).
        if (source.startsWith("/")) return File(source).readBytes()
        if (source.startsWith("file://")) return File(Uri.parse(source).path!!).readBytes()
        // In development `asuka dev` pushes the assets into the app's files.
        val dev = File(app.filesDir, "asuka/assets/$source")
        if (dev.exists()) return dev.readBytes()
        return app.assets.open(source).use { it.readBytes() }
    }

    /** Makes an animated image's drawable off the main thread; done gets it there (null if it can't be). */
    fun animation(data: ByteArray, done: (android.graphics.drawable.AnimatedImageDrawable?) -> Unit) {
        pool.execute {
            val d = runCatching {
                android.graphics.ImageDecoder.decodeDrawable(android.graphics.ImageDecoder.createSource(ByteBuffer.wrap(data)))
                    as? android.graphics.drawable.AnimatedImageDrawable
            }.getOrNull()
            main.post { done(d) }
        }
    }

    /** Whether data is a file ImageDecoder animates: a GIF, a WebP with an ANIM chunk, an AVIF or HEIF sequence. */
    private fun animates(data: ByteArray): Boolean {
        fun at(i: Int, s: String) = data.size >= i + s.length && (s.indices).all { data[i + it] == s[it].code.toByte() }
        if (at(0, "GIF8")) return true
        if (at(0, "RIFF") && at(8, "WEBPVP8X") && data.size > 20) return data[20].toInt() and 2 != 0
        if (at(4, "ftyp")) return at(8, "avis") || at(8, "msf1") || at(8, "hevs")
        return false
    }

    private fun decode(data: ByteArray, maxSide: Int): AsukaImage {
        val o = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(data, 0, data.size, o)
        if (o.outWidth <= 0) return decodeOther(data, maxSide)
        var sample = 1
        while (maxOf(o.outWidth, o.outHeight) / (sample * 2) >= maxSide) sample *= 2
        var b = BitmapFactory.decodeByteArray(data, 0, data.size, BitmapFactory.Options().apply { inSampleSize = sample })
            ?: error("can't decode the image")
        // A camera's photo is stored as the sensor saw it, with the turn to
        // show it upright in its EXIF (BitmapFactory ignores it).
        var w = o.outWidth
        var h = o.outHeight
        val m = exifMatrix(data)
        if (m != null) {
            b = Bitmap.createBitmap(b, 0, 0, b.width, b.height, m.first, true)
            if (m.second) { w = o.outHeight; h = o.outWidth }
        }
        b.prepareToDraw()
        return AsukaImage(b, w, h, if (animates(data)) data else null)
    }

    /** What BitmapFactory doesn't read (HEIF and AVIF, where the device has their codecs): ImageDecoder, which turns it upright itself. */
    private fun decodeOther(data: ByteArray, maxSide: Int): AsukaImage {
        var w = 0
        var h = 0
        val b = runCatching {
            android.graphics.ImageDecoder.decodeBitmap(android.graphics.ImageDecoder.createSource(ByteBuffer.wrap(data))) { d, info, _ ->
                w = info.size.width
                h = info.size.height
                var sample = 1
                while (maxOf(w, h) / (sample * 2) >= maxSide) sample *= 2
                d.setTargetSampleSize(sample)
                d.allocator = android.graphics.ImageDecoder.ALLOCATOR_SOFTWARE
            }
        }.getOrNull() ?: error("can't decode the image")
        b.prepareToDraw()
        return AsukaImage(b, w, h, if (animates(data)) data else null)
    }

    // exifMatrix is the transform a JPEG's EXIF orientation asks for, and
    // whether it swaps width and height; null if none.
    private fun exifMatrix(data: ByteArray): Pair<Matrix, Boolean>? {
        val orientation = try {
            ExifInterface(ByteArrayInputStream(data)).getAttributeInt(ExifInterface.TAG_ORIENTATION, ExifInterface.ORIENTATION_NORMAL)
        } catch (_: Exception) { return null }
        val m = Matrix()
        val swap = when (orientation) {
            ExifInterface.ORIENTATION_FLIP_HORIZONTAL -> { m.setScale(-1f, 1f); false }
            ExifInterface.ORIENTATION_ROTATE_180 -> { m.setRotate(180f); false }
            ExifInterface.ORIENTATION_FLIP_VERTICAL -> { m.setScale(1f, -1f); false }
            ExifInterface.ORIENTATION_TRANSPOSE -> { m.setRotate(90f); m.postScale(-1f, 1f); true }
            ExifInterface.ORIENTATION_ROTATE_90 -> { m.setRotate(90f); true }
            ExifInterface.ORIENTATION_TRANSVERSE -> { m.setRotate(-90f); m.postScale(-1f, 1f); true }
            ExifInterface.ORIENTATION_ROTATE_270 -> { m.setRotate(-90f); true }
            else -> return null
        }
        return m to swap
    }
}

fun AsukaRuntime.loadImage(n: AsukaNode, source: String) {
    n.imageSource = source
    n.imageLoaded = false
    val iv = n.view as AsukaImageView
    val id = n.id
    iv.clear()
    if (source.isEmpty()) { showPlaceholder(n); return }
    val show = { r: Result<AsukaImage> ->
        val cur = nodes[id]
        if (cur != null && cur.imageSource == source) {
            r.onSuccess { img ->
                cur.imageLoaded = true
                iv.show(img.bitmap)
                // One point per pixel, as on the web.
                emit(id, AsukaEventKind.LOADED) { AsukaEvents.loaded(id, img.width.toDouble(), img.height.toDouble()) }
                img.animation?.let { data ->
                    emit(id, AsukaEventKind.ANIMATED) { AsukaEvents.animated(id) }
                    AsukaImages.animation(data) { d ->
                        val now = nodes[id]
                        if (d != null && now != null && now.imageSource == source) iv.play(d)
                    }
                }
            }.onFailure {
                val m = it.message ?: it.javaClass.simpleName
                emit(id, AsukaEventKind.FAILED) { AsukaEvents.failed(id, m) }
            }
        }
    }
    val hit = AsukaImages.cached(source)
    if (hit != null) { iv.placeholder = null; show(Result.success(hit)) } else {
        showPlaceholder(n)
        AsukaImages.load(app, source, show)
    }
}

/**
 * Shows an image's placeholder until (or unless) the image loads: the
 * core's decoded hash, else the Placeholder image (tinted as the image is).
 */
fun AsukaRuntime.showPlaceholder(n: AsukaNode) {
    if (n.imageLoaded) return
    val iv = n.view as AsukaImageView
    n.imageHash?.let { iv.placeholder = it; iv.placeholderTinted = false; return }
    val p = n.imagePlaceholder
    if (p.isEmpty()) { iv.placeholder = null; return }
    val id = n.id
    AsukaImages.load(app, p) { r ->
        val cur = nodes[id] ?: return@load
        if (cur.imageLoaded || cur.imageHash != null || cur.imagePlaceholder != p) return@load
        r.onSuccess { iv.placeholder = it.bitmap; iv.placeholderTinted = true }
    }
}

/** PlaceholderImage's bytes (u32 width, u32 height, RGBA not premultiplied) as a bitmap. */
fun placeholderBitmap(b: ByteBuffer): Bitmap? {
    val buf = b.duplicate().order(java.nio.ByteOrder.LITTLE_ENDIAN)
    if (buf.remaining() < 8) return null
    val w = buf.int
    val h = buf.int
    if (w <= 0 || h <= 0 || buf.remaining() < w * h * 4) return null
    val px = IntArray(w * h) {
        val r = buf.get().toInt() and 255
        val g = buf.get().toInt() and 255
        val bl = buf.get().toInt() and 255
        val a = buf.get().toInt() and 255
        (a shl 24) or (r shl 16) or (g shl 8) or bl
    }
    return Bitmap.createBitmap(px, w, h, Bitmap.Config.ARGB_8888)
}

/**
 * An Image's view: the picture (an ImageView's drawable, at its scale
 * type), the placeholder under it until it arrives (in its own mode,
 * drawn here), the arrival cross-dissolving them over Transition
 * (config_shortAnimTime by default, over a placeholder), and an animated
 * picture's AnimatedImageDrawable, running while attached and not paused.
 */
class AsukaImageView(context: Context) : androidx.appcompat.widget.AppCompatImageView(context) {
    var placeholder: Bitmap? = null
        set(v) { field = v; fade?.cancel(); fade = null; placeholderAlpha = 1f; invalidate() }
    var placeholderTinted = false
        set(v) { field = v; invalidate() }
    /** PlaceholderMode: -1, the image's. */
    var placeholderMode = -1
        set(v) { field = v; invalidate() }
    var transition = 0.0
    var paused = false
        set(v) { field = v; animating() }
    private var placeholderAlpha = 1f
    private var fade: ValueAnimator? = null
    private var anim: android.graphics.drawable.AnimatedImageDrawable? = null
    private val paint = android.graphics.Paint(android.graphics.Paint.FILTER_BITMAP_FLAG)
    private val m = Matrix()

    init { scaleType = ScaleType.FIT_CENTER }

    /** Takes the picture away (a new source is loading). */
    fun clear() {
        anim?.stop()
        anim = null
        fade?.cancel()
        fade = null
        imageAlpha = 255
        setImageDrawable(null)
    }

    /** Shows the picture, fading it in over the placeholder (or from nothing). */
    fun show(b: Bitmap) {
        fade?.cancel()
        val d = when {
            transition < 0 -> 0.0
            transition > 0 -> transition
            placeholder != null -> resources.getInteger(android.R.integer.config_shortAnimTime) / 1000.0
            else -> 0.0
        }
        setImageBitmap(b)
        if (d <= 0 || !isAttachedToWindow) {
            imageAlpha = 255
            placeholder = null
            return
        }
        imageAlpha = 0
        fade = ValueAnimator.ofFloat(0f, 1f).apply {
            duration = (d * 1000).toLong()
            interpolator = android.view.animation.DecelerateInterpolator()
            addUpdateListener {
                val t = it.animatedValue as Float
                imageAlpha = (t * 255).toInt()
                placeholderAlpha = 1 - t
                invalidate()
            }
            addListener(object : android.animation.AnimatorListenerAdapter() {
                override fun onAnimationEnd(a: android.animation.Animator) {
                    if (fade === a) { fade = null; imageAlpha = 255; placeholder = null }
                }
            })
            start()
        }
    }

    /** Plays an animated picture in place of its first frame. */
    fun play(d: android.graphics.drawable.AnimatedImageDrawable) {
        val a = imageAlpha
        anim = d
        setImageDrawable(d)
        imageAlpha = a
        animating()
    }

    private fun animating() {
        val d = anim ?: return
        if (!paused && isAttachedToWindow) { if (!d.isRunning) d.start() } else if (d.isRunning) d.stop()
    }

    override fun onAttachedToWindow() { super.onAttachedToWindow(); animating() }
    override fun onDetachedFromWindow() { super.onDetachedFromWindow(); anim?.stop() }

    override fun onDraw(c: android.graphics.Canvas) {
        val p = placeholder
        if (p != null && placeholderAlpha > 0f && width > 0 && height > 0) {
            val mode = if (placeholderMode >= 0) placeholderMode else when (scaleType) {
                ScaleType.CENTER_CROP -> AsukaImageMode.FILL
                ScaleType.FIT_XY -> AsukaImageMode.STRETCH
                ScaleType.CENTER -> AsukaImageMode.CENTER
                else -> AsukaImageMode.FIT
            }
            val w = (width - paddingLeft - paddingRight).toFloat()
            val h = (height - paddingTop - paddingBottom).toFloat()
            val bw = p.width.toFloat()
            val bh = p.height.toFloat()
            m.reset()
            when (mode) {
                AsukaImageMode.STRETCH -> m.setScale(w / bw, h / bh)
                AsukaImageMode.CENTER -> m.setTranslate((w - bw) / 2, (h - bh) / 2)
                else -> {
                    val k = if (mode == AsukaImageMode.FILL) maxOf(w / bw, h / bh) else minOf(w / bw, h / bh)
                    m.setScale(k, k)
                    m.postTranslate((w - bw * k) / 2, (h - bh * k) / 2)
                }
            }
            m.postTranslate(paddingLeft.toFloat(), paddingTop.toFloat())
            paint.alpha = (placeholderAlpha * 255).toInt()
            val tint = imageTintList
            paint.colorFilter = if (placeholderTinted && tint != null)
                android.graphics.PorterDuffColorFilter(tint.defaultColor, android.graphics.PorterDuff.Mode.SRC_IN) else null
            c.save()
            c.clipRect(paddingLeft, paddingTop, width - paddingRight, height - paddingBottom)
            c.drawBitmap(p, m, paint)
            c.restore()
        }
        super.onDraw(c)
    }
}

/**
 * Files kept across launches in the app's cache directory (which the
 * system may clear), the least recently used going first past a limit.
 * Any thread.
 */
class AsukaDiskCache(private val dir: File, private val limit: Long) {
    @Volatile private var trimming = false

    private fun file(key: String): File {
        val h = java.security.MessageDigest.getInstance("SHA-256").digest(key.toByteArray())
        return File(dir, h.joinToString("") { "%02x".format(it) })
    }

    fun read(key: String): ByteArray? {
        val f = file(key)
        if (!f.exists()) return null
        return try { f.readBytes().also { f.setLastModified(System.currentTimeMillis()) } } catch (_: Exception) { null }
    }

    fun write(key: String, data: ByteArray) {
        try {
            dir.mkdirs()
            val tmp = File(dir, "tmp-" + Thread.currentThread().id)
            tmp.writeBytes(data)
            tmp.renameTo(file(key))
        } catch (_: Exception) { return }
        // Once a launch, after it has written: what's past the limit goes.
        if (!trimming) {
            trimming = true
            Thread { Thread.sleep(5000); trim() }.apply { priority = Thread.MIN_PRIORITY }.start()
        }
    }

    fun trim() {
        val files = dir.listFiles()?.sortedBy { it.lastModified() } ?: return
        var total = files.sumOf { it.length() }
        for (f in files) {
            if (total <= limit) break
            total -= f.length()
            f.delete()
        }
    }

    companion object {
        private var images: AsukaDiskCache? = null
        /** Images fetched over http(s), up to 256 MB. */
        @Synchronized fun images(ctx: Context): AsukaDiskCache =
            images ?: AsukaDiskCache(File(ctx.cacheDir, "asuka-images"), 256L shl 20).also { images = it }
    }
}

fun imageScaleType(mode: Int) = when (mode) {
    AsukaImageMode.FILL -> ImageView.ScaleType.CENTER_CROP
    AsukaImageMode.STRETCH -> ImageView.ScaleType.FIT_XY
    AsukaImageMode.CENTER -> ImageView.ScaleType.CENTER
    else -> ImageView.ScaleType.FIT_CENTER
}

// Native views

/** A view an app registers for ui.Native. Values are Boolean, Long, Double or String. */
interface AsukaNativeView {
    val view: View
    fun set(name: String, value: Any)
}

/** What a native view is given: a way to send its events. */
class AsukaNativeContext(val id: Int, private val rt: AsukaRuntime) {
    val context: Context get() = rt.themed

    /** Sends an event to the view's ui.NativeProps.On handler. */
    fun emit(name: String, values: Map<String, Any> = emptyMap()) {
        val w = AsukaWriter()
        w.string(name)
        w.u32(values.size)
        for ((k, v) in values) writeNative(w, k, v)
        val b = w.bytes()
        rt.emit(id, AsukaEventKind.NATIVE) { AsukaEvents.native(id, b) }
    }
}

private fun writeNative(w: AsukaWriter, k: String, v: Any) {
    w.string(k)
    when (v) {
        is Boolean -> { w.u8(1); w.u8(if (v) 1 else 0) }
        is Int -> { w.u8(2); w.u64(v.toLong()) }
        is Long -> { w.u8(2); w.u64(v) }
        is Float -> { w.u8(3); w.f64(v.toDouble()) }
        is Double -> { w.u8(3); w.f64(v) }
        is ByteArray -> { w.u8(5); w.u32(v.size); w.raw(v) }
        else -> { w.u8(4); w.string(v.toString()) }
    }
}

/** Holds a native view, which fills it; it measures as the native view. */
class AsukaNativeHost(context: Context) : FrameLayout(context) {
    var native: AsukaNativeView? = null
        set(v) {
            removeAllViews()
            field = v
            v?.let { addView(it.view, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT)) }
        }
}

fun AsukaRuntime.registerNatives() {
    registerNative("asuka.camera") { ctx -> AsukaCameraView(this, ctx) }
    registerPlugins() // asuka-maps' view, the optional modules' methods
    // Packages' typed views are found as they're shown (AsukaModules.makeView).
}

fun AsukaRuntime.setNativeProp(n: AsukaNode, prop: Int, v: Any): Boolean {
    when (prop) {
        AsukaProp.Native.TYPE -> {
            // The host's own kinds, else a package's typed view (its
            // module's class # its name).
            val ctx = AsukaNativeContext(n.id, this)
            val nv = nativeFactories[v as String]?.invoke(ctx) ?: AsukaModules.makeView(v, ctx)
            if (nv == null) {
                Log.e("asuka", "no native view \"$v\": build the app with its packages' native code")
                return true
            }
            (n.view as AsukaNativeHost).native = nv
        }
        AsukaProp.Native.PROP -> {
            val b = v as ByteBuffer
            val name = readString(b)
            val value: Any = when (b.get().toInt()) {
                1 -> b.get().toInt() != 0
                2 -> b.long
                3 -> b.double
                5 -> ByteArray(b.int).also { b.get(it) }
                else -> readString(b)
            }
            (n.view as AsukaNativeHost).native?.set(name, value)
        }
        else -> return false
    }
    return true
}

private fun readString(b: ByteBuffer): String {
    val a = ByteArray(b.int)
    b.get(a)
    return String(a, Charsets.UTF_8)
}

// Gestures

/**
 * A view's pans, pinches and swipes. It sees every touch first; while a
 * pan or pinch is under way it takes the touches (and cancels the view's
 * press), and its parent stops intercepting them.
 */
class AsukaGestures(private val rt: AsukaRuntime, private val id: Int, context: Context) : View.OnTouchListener {
    var pan = false
    var pinch = false
    var swipe = false
    var rotate = false
    private var rotating = false
    private var startAngle = 0.0
    private var lastAngle = 0.0
    private var turned = 0.0 // radians since the rotation began, unwrapped
    private val slop = ViewConfiguration.get(context).scaledTouchSlop
    private val minFling = ViewConfiguration.get(context).scaledMinimumFlingVelocity * 4
    private var downX = 0f
    private var downY = 0f
    private var panning = false
    private var scaling = false
    private var scale = 1f
    private var velocity: VelocityTracker? = null
    private val d get() = rt.density

    // A pinch: two fingers whose span changes by more than the touch
    // slop (Android's ScaleGestureDetector also wants a span of about
    // 27mm, which rules out small pinches).
    private var startSpan = 0f

    private fun span(e: MotionEvent): Float {
        if (e.pointerCount < 2) return 0f
        return Math.hypot((e.getX(0) - e.getX(1)).toDouble(), (e.getY(0) - e.getY(1)).toDouble()).toFloat()
    }

    private fun trackPinch(v: View, e: MotionEvent) {
        when (e.actionMasked) {
            MotionEvent.ACTION_POINTER_DOWN -> if (e.pointerCount == 2) { startSpan = span(e); scaling = false }
            MotionEvent.ACTION_MOVE -> if (e.pointerCount >= 2 && startSpan > 0) {
                val sp = span(e)
                val fx = (e.getX(0) + e.getX(1)) / 2
                val fy = (e.getY(0) + e.getY(1)) / 2
                if (!scaling && abs(sp - startSpan) > slop) {
                    scaling = true
                    startSpan = sp
                    claim(v, e)
                    send(AsukaEventKind.PINCH, AsukaGestureState.BEGAN, fx, fy, 0f, 0f, 0f, 0f, 1f)
                } else if (scaling) {
                    scale = sp / startSpan
                    send(AsukaEventKind.PINCH, AsukaGestureState.CHANGED, fx, fy, 0f, 0f, 0f, 0f, scale)
                }
            }
            MotionEvent.ACTION_POINTER_UP, MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                if (scaling) {
                    val state = if (e.actionMasked == MotionEvent.ACTION_CANCEL) AsukaGestureState.CANCELLED else AsukaGestureState.ENDED
                    send(AsukaEventKind.PINCH, state, e.x, e.y, 0f, 0f, 0f, 0f, scale)
                }
                scaling = false
                startSpan = 0f
                scale = 1f
            }
        }
    }

    private fun angle(e: MotionEvent) = Math.atan2((e.getY(1) - e.getY(0)).toDouble(), (e.getX(1) - e.getX(0)).toDouble())

    // A rotation: two fingers whose line turns by more than 5 degrees.
    private fun trackRotate(v: View, e: MotionEvent) {
        when (e.actionMasked) {
            MotionEvent.ACTION_POINTER_DOWN -> if (e.pointerCount == 2) { startAngle = angle(e); lastAngle = startAngle; turned = 0.0; rotating = false }
            MotionEvent.ACTION_MOVE -> if (e.pointerCount >= 2) {
                val a = angle(e)
                var step = a - lastAngle
                if (step > Math.PI) step -= 2 * Math.PI
                if (step < -Math.PI) step += 2 * Math.PI
                lastAngle = a
                turned += step
                val fx = (e.getX(0) + e.getX(1)) / 2
                val fy = (e.getY(0) + e.getY(1)) / 2
                if (!rotating && abs(turned) > Math.toRadians(5.0)) {
                    rotating = true
                    turned = 0.0
                    claim(v, e)
                    sendRotate(AsukaGestureState.BEGAN, fx, fy)
                } else if (rotating) {
                    sendRotate(AsukaGestureState.CHANGED, fx, fy)
                }
            }
            MotionEvent.ACTION_POINTER_UP, MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                if (rotating) sendRotate(if (e.actionMasked == MotionEvent.ACTION_CANCEL) AsukaGestureState.CANCELLED else AsukaGestureState.ENDED, e.x, e.y)
                rotating = false
            }
        }
    }

    private fun sendRotate(state: Int, x: Float, y: Float) {
        val g = AsukaGesture(state, x / d, y / d, rotation = turned.toFloat())
        rt.emit(id, AsukaEventKind.ROTATE) { AsukaEvents.rotate(id, g) }
    }

    /** The touch is a gesture now, not a press, and the parent mustn't take it. */
    private fun claim(v: View, e: MotionEvent) {
        v.parent?.requestDisallowInterceptTouchEvent(true)
        val cancel = MotionEvent.obtain(e).apply { action = MotionEvent.ACTION_CANCEL }
        v.onTouchEvent(cancel)
        cancel.recycle()
    }

    private val flinger = GestureDetector(context, object : GestureDetector.SimpleOnGestureListener() {
        override fun onDown(e: MotionEvent) = true
        override fun onFling(e1: MotionEvent?, e2: MotionEvent, vx: Float, vy: Float): Boolean {
            val dir = if (abs(vx) > abs(vy)) {
                if (abs(vx) < minFling) return false
                if (vx > 0) 1L else 2L
            } else {
                if (abs(vy) < minFling) return false
                if (vy > 0) 8L else 4L
            }
            rt.emit(id, AsukaEventKind.SWIPE) { AsukaEvents.swipe(id, dir) }
            return true
        }
    })

    private fun send(kind: Int, state: Int, x: Float, y: Float, tx: Float, ty: Float, vx: Float, vy: Float, s: Float) {
        val g = AsukaGesture(state, x / d, y / d, tx / d, ty / d, vx / d, vy / d, s)
        rt.emit(id, kind) { if (kind == AsukaEventKind.PAN) AsukaEvents.pan(id, g) else AsukaEvents.pinch(id, g) }
    }

    /**
     * A touch on a child that's pressable never reaches onTouch: the view
     * watches it on its way there, and takes it (the child gets a cancel)
     * once it's a gesture.
     */
    fun intercept(v: View, e: MotionEvent): Boolean {
        if (!pan && !pinch && !rotate) return false
        if (pinch) trackPinch(v, e)
        if (rotate) trackRotate(v, e)
        if (pan) trackPan(v, e)
        return panning || scaling || rotating
    }

    override fun onTouch(v: View, e: MotionEvent): Boolean {
        if (pinch) trackPinch(v, e)
        if (rotate) trackRotate(v, e)
        if (swipe) flinger.onTouchEvent(e)
        if (pan) trackPan(v, e)
        if (e.actionMasked == MotionEvent.ACTION_DOWN) return !v.isClickable // keep getting this touch's events
        return panning || scaling || rotating
    }

    private fun trackPan(v: View, e: MotionEvent) {
        when (e.actionMasked) {
            MotionEvent.ACTION_DOWN -> {
                downX = e.rawX
                downY = e.rawY
                panning = false
                velocity?.recycle()
                velocity = VelocityTracker.obtain().also { it.addMovement(e) }
            }
            MotionEvent.ACTION_MOVE -> {
                velocity?.addMovement(e)
                val tx = e.rawX - downX
                val ty = e.rawY - downY
                if (!panning && !scaling && e.pointerCount == 1 && maxOf(abs(tx), abs(ty)) > slop) {
                    panning = true
                    claim(v, e)
                    send(AsukaEventKind.PAN, AsukaGestureState.BEGAN, e.x, e.y, tx, ty, 0f, 0f, 1f)
                } else if (panning) {
                    velocity?.computeCurrentVelocity(1000)
                    send(AsukaEventKind.PAN, AsukaGestureState.CHANGED, e.x, e.y, tx, ty, velocity?.xVelocity ?: 0f, velocity?.yVelocity ?: 0f, 1f)
                }
            }
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                if (panning) {
                    velocity?.addMovement(e)
                    velocity?.computeCurrentVelocity(1000)
                    val state = if (e.actionMasked == MotionEvent.ACTION_UP) AsukaGestureState.ENDED else AsukaGestureState.CANCELLED
                    send(AsukaEventKind.PAN, state, e.x, e.y, e.rawX - downX, e.rawY - downY, velocity?.xVelocity ?: 0f, velocity?.yVelocity ?: 0f, 1f)
                }
                panning = false
                velocity?.recycle()
                velocity = null
            }
        }
    }
}

fun AsukaRuntime.listenGesture(n: AsukaNode, event: Int) {
    val g = n.gestures ?: AsukaGestures(this, n.id, themed).also {
        n.gestures = it
        n.view.setOnTouchListener(it)
        (n.view as? AsukaContainer)?.gestures = it
    }
    when (event) {
        AsukaEventKind.PAN -> g.pan = true
        AsukaEventKind.PINCH -> g.pinch = true
        AsukaEventKind.ROTATE -> g.rotate = true
        AsukaEventKind.SWIPE -> g.swipe = true
    }
}

// Animation: between AnimateBegin and AnimateEnd, frames, opacity and
// backgrounds change with animators.

class AsukaAnimation(
    val millis: Long, val curve: Int, val repeat: Int = 0, val autoreverses: Boolean = false,
    /** Before it starts. */
    val delay: Long = 0,
    /** A spring's damping ratio (1: no overshoot) and initial velocity (the change's fraction per second). */
    val damping: Float = 0.85f, val velocity: Float = 0f,
) {
    fun interpolator(): Interpolator = when (curve) {
        AsukaAnimationCurve.LINEAR -> LinearInterpolator()
        AsukaAnimationCurve.EASE_IN -> PathInterpolator(0.42f, 0f, 1f, 1f)
        AsukaAnimationCurve.EASE_OUT -> PathInterpolator(0f, 0f, 0.58f, 1f)
        AsukaAnimationCurve.SPRING -> AsukaSpring(millis / 1000f, damping, velocity)
        else -> PathInterpolator(0.42f, 0f, 0.58f, 1f)
    }
}

/**
 * A damped spring over the animation's duration, as UIKit's
 * usingSpringWithDamping: its natural period is the duration, so it has
 * settled by its end; underdamped, it overshoots and comes back. Its
 * initial velocity is in the change's fraction per second.
 */
class AsukaSpring(seconds: Float, damping: Float, private val v0: Float) : Interpolator {
    private val dur = maxOf(seconds, 0.001f)
    private val zeta = damping.coerceIn(0.05f, 1f)
    // Settled to about 0.1% by the end: e^(-zeta*w0*dur) = 0.001.
    private val w0 = maxOf(6.9f / (zeta * dur), 2f * Math.PI.toFloat() / dur)

    override fun getInterpolation(f: Float): Float {
        if (f >= 1f) return 1f
        val t = f * dur
        val decay = Math.exp((-zeta * w0 * t).toDouble()).toFloat()
        if (zeta >= 0.999f) return 1f - decay * (1f + (w0 - v0) * t)
        val wd = w0 * Math.sqrt((1 - zeta * zeta).toDouble()).toFloat()
        return 1f - decay * (Math.cos((wd * t).toDouble()).toFloat() + (zeta * w0 - v0) / wd * Math.sin((wd * t).toDouble()).toFloat())
    }
}

fun AsukaRuntime.beginAnimation(args: ByteBuffer) {
    if (args.remaining() < 5) return
    val seconds = args.float
    val curve = args.get().toInt()
    var repeat = 0
    var auto = false
    if (args.remaining() >= 5) {
        repeat = args.int
        auto = args.get().toInt() != 0
    }
    var delay = 0L
    var damping = 0.85f
    var velocity = 0f
    if (args.remaining() >= 12) {
        delay = (args.float * 1000).toLong()
        damping = args.float
        velocity = args.float
    }
    animation = AsukaAnimation((seconds * 1000).toLong(), curve, repeat, auto, delay, damping, velocity)
}

/** Whether a change to v animates now: an animation is on and v is on screen. */
fun AsukaRuntime.animates(v: View) = animation != null && v.isAttachedToWindow && v.width > 0 && v.height > 0

/** Moves v from its frame to (l, t, r, b), calling apply each frame. */
fun AsukaRuntime.animateFrame(n: AsukaNode, v: View, l: Int, t: Int, r: Int, b: Int, apply: (Int, Int, Int, Int) -> Unit) {
    val a = animation ?: return apply(l, t, r, b)
    val ol = v.left
    val ot = v.top
    val or = v.right
    val ob = v.bottom
    n.frameAnimator?.cancel()
    if (ol == l && ot == t && or == r && ob == b) return apply(l, t, r, b)
    fun lerp(from: Int, to: Int, f: Float) = from + Math.round((to - from) * f)
    n.frameAnimator = ValueAnimator.ofFloat(0f, 1f).apply {
        duration = a.millis
        startDelay = a.delay
        interpolator = a.interpolator()
        addUpdateListener {
            val f = it.animatedValue as Float
            apply(lerp(ol, l, f), lerp(ot, t, f), lerp(or, r, f), lerp(ob, b, f))
        }
        start()
    }
}

/** Animates a background's color and corner radius from what it shows to the node's. */
fun AsukaRuntime.animateBackground(n: AsukaNode, d: GradientDrawable, fromColor: Int, toColor: Int, fromRadius: Float, toRadius: Float) {
    val a = animation ?: return
    n.backgroundAnimator?.cancel()
    n.backgroundAnimator = ValueAnimator.ofFloat(0f, 1f).apply {
        duration = a.millis
        startDelay = a.delay
        interpolator = a.interpolator()
        val argb = android.animation.ArgbEvaluator()
        addUpdateListener {
            val f = it.animatedValue as Float
            d.setColor(argb.evaluate(f, fromColor, toColor) as Int)
            d.cornerRadius = fromRadius + (toRadius - fromRadius) * f
        }
        start()
    }
}
