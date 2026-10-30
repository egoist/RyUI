package dev.nui

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

/** A decoded image and its natural size (pixels). */
class NuiImage(val bitmap: Bitmap, val width: Int, val height: Int)

/**
 * Loads images off the main thread: fetched (http, https; kept on disk,
 * NuiDiskCache) or read (the app's assets), decoded there (subsampled to
 * the screen's size), then cached in memory.
 */
object NuiImages {
    private val cache = object : LruCache<String, NuiImage>(maxOf(16 shl 20, (Runtime.getRuntime().maxMemory() / 8).toInt())) {
        override fun sizeOf(key: String, v: NuiImage) = v.bitmap.byteCount
    }
    private val pool = Executors.newFixedThreadPool(3)
    private val main = Handler(Looper.getMainLooper())
    private val waiting = HashMap<String, ArrayList<(Result<NuiImage>) -> Unit>>() // main thread only

    fun cached(source: String): NuiImage? = cache.get(source)

    /** Calls done on the main thread. */
    fun load(context: Context, source: String, done: (Result<NuiImage>) -> Unit) {
        cached(source)?.let { done(Result.success(it)); return }
        waiting[source]?.let { it.add(done); return }
        waiting[source] = arrayListOf(done)
        val app = context.applicationContext
        val maxSide = maxOf(app.resources.displayMetrics.widthPixels, app.resources.displayMetrics.heightPixels)
        pool.execute {
            val r = runCatching {
                if (source.startsWith("http://") || source.startsWith("https://")) {
                    val disk = NuiDiskCache.images(app)
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

    /** An image over http(s), and whether it may be kept on disk: not one its response says not to keep, nor one from this machine (nui dev's assets, which change). */
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
        // In development `nui dev` pushes the assets into the app's files.
        val dev = File(app.filesDir, "nui/assets/$source")
        if (dev.exists()) return dev.readBytes()
        return app.assets.open(source).use { it.readBytes() }
    }

    private fun decode(data: ByteArray, maxSide: Int): NuiImage {
        val o = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeByteArray(data, 0, data.size, o)
        if (o.outWidth <= 0) error("can't decode the image")
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
        return NuiImage(b, w, h)
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

fun NuiRuntime.loadImage(n: NuiNode, source: String) {
    n.imageSource = source
    n.imageLoaded = false
    val iv = n.view as ImageView
    val id = n.id
    if (source.isEmpty()) { iv.setImageDrawable(null); showPlaceholder(n); return }
    val show = { r: Result<NuiImage> ->
        val cur = nodes[id]
        if (cur != null && cur.imageSource == source) {
            r.onSuccess {
                cur.imageLoaded = true
                (cur.view as ImageView).setImageBitmap(it.bitmap)
                // One point per pixel, as on the web.
                emit(id, NuiEventKind.LOADED) { NuiEvents.loaded(id, it.width.toDouble(), it.height.toDouble()) }
            }.onFailure {
                val m = it.message ?: it.javaClass.simpleName
                emit(id, NuiEventKind.FAILED) { NuiEvents.failed(id, m) }
            }
        }
    }
    val hit = NuiImages.cached(source)
    if (hit != null) show(Result.success(hit)) else {
        iv.setImageDrawable(null)
        showPlaceholder(n)
        NuiImages.load(app, source, show)
    }
}

/** Shows an image's placeholder until (or unless) the image loads. */
fun NuiRuntime.showPlaceholder(n: NuiNode) {
    val p = n.imagePlaceholder
    if (p.isEmpty() || n.imageLoaded) return
    val id = n.id
    NuiImages.load(app, p) { r ->
        val cur = nodes[id] ?: return@load
        if (cur.imageLoaded || cur.imagePlaceholder != p) return@load
        r.onSuccess { (cur.view as ImageView).setImageBitmap(it.bitmap) }
    }
}

/**
 * Files kept across launches in the app's cache directory (which the
 * system may clear), the least recently used going first past a limit.
 * Any thread.
 */
class NuiDiskCache(private val dir: File, private val limit: Long) {
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
        private var images: NuiDiskCache? = null
        /** Images fetched over http(s), up to 256 MB. */
        @Synchronized fun images(ctx: Context): NuiDiskCache =
            images ?: NuiDiskCache(File(ctx.cacheDir, "nui-images"), 256L shl 20).also { images = it }
    }
}

fun imageScaleType(mode: Int) = when (mode) {
    NuiImageMode.FILL -> ImageView.ScaleType.CENTER_CROP
    NuiImageMode.STRETCH -> ImageView.ScaleType.FIT_XY
    NuiImageMode.CENTER -> ImageView.ScaleType.CENTER
    else -> ImageView.ScaleType.FIT_CENTER
}

// Native views

/** A view an app registers for ui.Native. Values are Boolean, Long, Double or String. */
interface NuiNativeView {
    val view: View
    fun set(name: String, value: Any)
}

/** What a native view is given: a way to send its events. */
class NuiNativeContext(val id: Int, private val rt: NuiRuntime) {
    val context: Context get() = rt.themed

    /** Sends an event to the view's ui.NativeProps.On handler. */
    fun emit(name: String, values: Map<String, Any> = emptyMap()) {
        val w = NuiWriter()
        w.string(name)
        w.u32(values.size)
        for ((k, v) in values) writeNative(w, k, v)
        val b = w.bytes()
        rt.emit(id, NuiEventKind.NATIVE) { NuiEvents.native(id, b) }
    }
}

private fun writeNative(w: NuiWriter, k: String, v: Any) {
    w.string(k)
    when (v) {
        is Boolean -> { w.u8(1); w.u8(if (v) 1 else 0) }
        is Int -> { w.u8(2); w.u64(v.toLong()) }
        is Long -> { w.u8(2); w.u64(v) }
        is Float -> { w.u8(3); w.f64(v.toDouble()) }
        is Double -> { w.u8(3); w.f64(v) }
        else -> { w.u8(4); w.string(v.toString()) }
    }
}

/** Apps register native views from an object dev.nui.app.NuiAppNatives implementing this. */
interface NuiNativeModule {
    fun register(runtime: NuiRuntime)
}

/** Holds a native view, which fills it; it measures as the native view. */
class NuiNativeHost(context: Context) : FrameLayout(context) {
    var native: NuiNativeView? = null
        set(v) {
            removeAllViews()
            field = v
            v?.let { addView(it.view, LayoutParams(LayoutParams.MATCH_PARENT, LayoutParams.WRAP_CONTENT)) }
        }
}

fun NuiRuntime.registerNatives() {
    registerNative("nui.camera") { ctx -> NuiCameraView(this, ctx) }
    try {
        val c = Class.forName("dev.nui.app.NuiAppNatives")
        (c.getField("INSTANCE").get(null) as NuiNativeModule).register(this)
    } catch (_: ClassNotFoundException) {
    } catch (e: Exception) {
        Log.e("nui", "registering native views", e)
    }
}

fun NuiRuntime.setNativeProp(n: NuiNode, prop: Int, v: Any): Boolean {
    when (prop) {
        NuiProp.Native.TYPE -> {
            val make = nativeFactories[v as String]
            if (make == null) {
                Log.e("nui", "no native view \"$v\" is registered")
                return true
            }
            (n.view as NuiNativeHost).native = make(NuiNativeContext(n.id, this))
        }
        NuiProp.Native.PROP -> {
            val b = v as ByteBuffer
            val name = readString(b)
            val value: Any = when (b.get().toInt()) {
                1 -> b.get().toInt() != 0
                2 -> b.long
                3 -> b.double
                else -> readString(b)
            }
            (n.view as NuiNativeHost).native?.set(name, value)
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
class NuiGestures(private val rt: NuiRuntime, private val id: Int, context: Context) : View.OnTouchListener {
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
                    send(NuiEventKind.PINCH, NuiGestureState.BEGAN, fx, fy, 0f, 0f, 0f, 0f, 1f)
                } else if (scaling) {
                    scale = sp / startSpan
                    send(NuiEventKind.PINCH, NuiGestureState.CHANGED, fx, fy, 0f, 0f, 0f, 0f, scale)
                }
            }
            MotionEvent.ACTION_POINTER_UP, MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                if (scaling) {
                    val state = if (e.actionMasked == MotionEvent.ACTION_CANCEL) NuiGestureState.CANCELLED else NuiGestureState.ENDED
                    send(NuiEventKind.PINCH, state, e.x, e.y, 0f, 0f, 0f, 0f, scale)
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
                    sendRotate(NuiGestureState.BEGAN, fx, fy)
                } else if (rotating) {
                    sendRotate(NuiGestureState.CHANGED, fx, fy)
                }
            }
            MotionEvent.ACTION_POINTER_UP, MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                if (rotating) sendRotate(if (e.actionMasked == MotionEvent.ACTION_CANCEL) NuiGestureState.CANCELLED else NuiGestureState.ENDED, e.x, e.y)
                rotating = false
            }
        }
    }

    private fun sendRotate(state: Int, x: Float, y: Float) {
        val g = NuiGesture(state, x / d, y / d, rotation = turned.toFloat())
        rt.emit(id, NuiEventKind.ROTATE) { NuiEvents.rotate(id, g) }
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
            rt.emit(id, NuiEventKind.SWIPE) { NuiEvents.swipe(id, dir) }
            return true
        }
    })

    private fun send(kind: Int, state: Int, x: Float, y: Float, tx: Float, ty: Float, vx: Float, vy: Float, s: Float) {
        val g = NuiGesture(state, x / d, y / d, tx / d, ty / d, vx / d, vy / d, s)
        rt.emit(id, kind) { if (kind == NuiEventKind.PAN) NuiEvents.pan(id, g) else NuiEvents.pinch(id, g) }
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
                    send(NuiEventKind.PAN, NuiGestureState.BEGAN, e.x, e.y, tx, ty, 0f, 0f, 1f)
                } else if (panning) {
                    velocity?.computeCurrentVelocity(1000)
                    send(NuiEventKind.PAN, NuiGestureState.CHANGED, e.x, e.y, tx, ty, velocity?.xVelocity ?: 0f, velocity?.yVelocity ?: 0f, 1f)
                }
            }
            MotionEvent.ACTION_UP, MotionEvent.ACTION_CANCEL -> {
                if (panning) {
                    velocity?.addMovement(e)
                    velocity?.computeCurrentVelocity(1000)
                    val state = if (e.actionMasked == MotionEvent.ACTION_UP) NuiGestureState.ENDED else NuiGestureState.CANCELLED
                    send(NuiEventKind.PAN, state, e.x, e.y, e.rawX - downX, e.rawY - downY, velocity?.xVelocity ?: 0f, velocity?.yVelocity ?: 0f, 1f)
                }
                panning = false
                velocity?.recycle()
                velocity = null
            }
        }
    }
}

fun NuiRuntime.listenGesture(n: NuiNode, event: Int) {
    val g = n.gestures ?: NuiGestures(this, n.id, themed).also {
        n.gestures = it
        n.view.setOnTouchListener(it)
    }
    when (event) {
        NuiEventKind.PAN -> g.pan = true
        NuiEventKind.PINCH -> g.pinch = true
        NuiEventKind.ROTATE -> g.rotate = true
        NuiEventKind.SWIPE -> g.swipe = true
    }
}

// Animation: between AnimateBegin and AnimateEnd, frames, opacity and
// backgrounds change with animators.

class NuiAnimation(val millis: Long, val curve: Int, val repeat: Int = 0, val autoreverses: Boolean = false) {
    fun interpolator(): Interpolator = when (curve) {
        NuiAnimationCurve.LINEAR -> LinearInterpolator()
        NuiAnimationCurve.EASE_IN -> PathInterpolator(0.42f, 0f, 1f, 1f)
        NuiAnimationCurve.EASE_OUT -> PathInterpolator(0f, 0f, 0.58f, 1f)
        NuiAnimationCurve.SPRING -> OvershootInterpolator(0.9f)
        else -> PathInterpolator(0.42f, 0f, 0.58f, 1f)
    }
}

fun NuiRuntime.beginAnimation(args: ByteBuffer) {
    if (args.remaining() < 5) return
    val seconds = args.float
    val curve = args.get().toInt()
    var repeat = 0
    var auto = false
    if (args.remaining() >= 5) {
        repeat = args.int
        auto = args.get().toInt() != 0
    }
    animation = NuiAnimation((seconds * 1000).toLong(), curve, repeat, auto)
}

/** Whether a change to v animates now: an animation is on and v is on screen. */
fun NuiRuntime.animates(v: View) = animation != null && v.isAttachedToWindow && v.width > 0 && v.height > 0

/** Moves v from its frame to (l, t, r, b), calling apply each frame. */
fun NuiRuntime.animateFrame(n: NuiNode, v: View, l: Int, t: Int, r: Int, b: Int, apply: (Int, Int, Int, Int) -> Unit) {
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
        interpolator = a.interpolator()
        addUpdateListener {
            val f = it.animatedValue as Float
            apply(lerp(ol, l, f), lerp(ot, t, f), lerp(or, r, f), lerp(ob, b, f))
        }
        start()
    }
}

/** Animates a background's color and corner radius from what it shows to the node's. */
fun NuiRuntime.animateBackground(n: NuiNode, d: GradientDrawable, fromColor: Int, toColor: Int, fromRadius: Float, toRadius: Float) {
    val a = animation ?: return
    n.backgroundAnimator?.cancel()
    n.backgroundAnimator = ValueAnimator.ofFloat(0f, 1f).apply {
        duration = a.millis
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
