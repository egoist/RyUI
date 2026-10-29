package dev.nui

import android.content.Context
import android.graphics.Typeface
import android.graphics.fonts.Font
import android.graphics.fonts.FontFamily
import android.graphics.fonts.FontStyle
import android.os.Build
import android.util.Log
import java.io.File
import java.net.HttpURLConnection
import java.net.URL

/**
 * The app's fonts: the files in its assets/fonts (or, under nui dev,
 * those pushed to its files), a family of them (Inter: Inter-Regular,
 * Inter-Bold...), the system's families (serif), and under nui dev
 * fonts by URL. Main thread.
 */
object NuiFonts {
    private val cache = HashMap<String, Typeface?>()
    private val urls = HashMap<String, Typeface>()
    private val loading = HashSet<String>()
    private var sources: Map<String, String>? = null
    /** Counts the fonts by URL loaded, for caches of text laid out in them. */
    var version = 0
        private set
    /** Runs when a font by URL has loaded (its URL). */
    var loaded: ((String) -> Unit)? = null

    /** A typeface of family in weight (100-900; 0: the family's regular), or null: none such, or not loaded yet. */
    fun typeface(ctx: Context, family: String, weight: Int, italic: Boolean = false): Typeface? {
        if ("://" in family) {
            urls[family]?.let { return if (italic) Typeface.create(it, Typeface.ITALIC) else it }
            load(ctx, family)
            return null
        }
        val key = "$family|$weight|$italic"
        if (cache.containsKey(key)) return cache[key]
        val t = try { resolve(ctx, family, weight, italic) } catch (e: Exception) { Log.w("nui", "font $family: $e"); null }
        cache[key] = t
        return t
    }

    private fun resolve(ctx: Context, family: String, weight: Int, italic: Boolean): Typeface? {
        val src = sources(ctx)
        src[family]?.let { p ->
            val t = if (p.startsWith("/")) Typeface.createFromFile(p) else Typeface.createFromAsset(ctx.assets, p)
            return if (italic) Typeface.create(t, Typeface.ITALIC) else t
        }
        val faces = src.filterKeys { it.startsWith("$family-") }
        if (faces.isNotEmpty()) {
            if (Build.VERSION.SDK_INT >= 29) {
                var b: FontFamily.Builder? = null
                for (p in faces.values) {
                    val f = (if (p.startsWith("/")) Font.Builder(File(p)) else Font.Builder(ctx.assets, p)).build()
                    if (b == null) b = FontFamily.Builder(f)
                    else try { b.addFont(f) } catch (_: IllegalArgumentException) {} // a style it has
                }
                val style = FontStyle(if (weight > 0) weight else 400, if (italic) FontStyle.FONT_SLANT_ITALIC else FontStyle.FONT_SLANT_UPRIGHT)
                return Typeface.CustomFallbackBuilder(b!!.build()).setSystemFallback("sans-serif").setStyle(style).build()
            }
            val p = faces["$family-Regular"] ?: faces.values.first()
            val t = if (p.startsWith("/")) Typeface.createFromFile(p) else Typeface.createFromAsset(ctx.assets, p)
            return Typeface.create(t, styleOf(weight, italic))
        }
        // A system family; an unknown one is the default.
        val t = Typeface.create(family, Typeface.NORMAL)
        if (t == Typeface.DEFAULT) return null
        return if (weight > 0 && Build.VERSION.SDK_INT >= 28) Typeface.create(t, weight, italic)
        else Typeface.create(t, styleOf(weight, italic))
    }

    private fun styleOf(weight: Int, italic: Boolean) = when {
        weight >= 600 && italic -> Typeface.BOLD_ITALIC
        weight >= 600 -> Typeface.BOLD
        italic -> Typeface.ITALIC
        else -> Typeface.NORMAL
    }

    /** A font file's name (no extension) → its path: a pushed file's, or an asset's ("fonts/Inter-Bold.ttf"). */
    private fun sources(ctx: Context): Map<String, String> {
        sources?.let { return it }
        val out = HashMap<String, String>()
        fun font(name: String) = name.endsWith(".ttf", true) || name.endsWith(".otf", true)
        ctx.assets.list("fonts")?.filter(::font)?.forEach { out[it.substringBeforeLast('.')] = "fonts/$it" }
        File(ctx.filesDir, "nui/assets/fonts").listFiles()?.filter { font(it.name) }?.forEach {
            out[it.name.substringBeforeLast('.')] = it.path
        }
        sources = out
        return out
    }

    private fun load(ctx: Context, url: String) {
        if (!loading.add(url)) return // a failed load isn't tried again
        val dir = File(ctx.cacheDir, "nui-fonts").apply { mkdirs() }
        Thread {
            try {
                val c = URL(url).openConnection() as HttpURLConnection
                val bytes = try {
                    if (c.responseCode !in 200..299) error("HTTP ${c.responseCode}")
                    c.inputStream.use { it.readBytes() }
                } finally { c.disconnect() }
                val f = File(dir, Integer.toHexString(url.hashCode()) + "." + url.substringAfterLast('.'))
                f.writeBytes(bytes)
                val t = Typeface.createFromFile(f)
                android.os.Handler(android.os.Looper.getMainLooper()).post {
                    urls[url] = t
                    version++
                    loaded?.invoke(url)
                }
            } catch (e: Exception) {
                Log.w("nui", "font $url: $e")
            }
        }.start()
    }
}
