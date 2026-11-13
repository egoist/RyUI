package dev.asuka

import android.os.Handler
import android.os.Looper
import android.webkit.MimeTypeMap
import android.webkit.URLUtil
import java.io.File
import java.net.HttpURLConnection
import java.net.URL
import java.util.UUID
import java.util.concurrent.Executors

/*
 * The files asuka/preview, asuka/print and asuka/compose take: a path, an
 * asset's path (pushed by asuka dev, else in the APK, copied out), or an
 * http(s) URL, downloaded to the caches (asuka-files/) first, under its
 * name, with the extension of the type the server gives if it has none.
 */

private val fetcher = Executors.newCachedThreadPool()

/** One source to resolve: its MIME type, if the app knows it. */
class AsukaSource(val source: String, val type: String = "")

/** The local files for sources, in their order, on the main thread; the first failure fails them all. */
fun AsukaRuntime.localFiles(sources: List<AsukaSource>, done: (Result<List<File>>) -> Unit) {
    fetcher.execute {
        val r = runCatching { sources.map { localFile(it) } }
        Handler(Looper.getMainLooper()).post { done(r) }
    }
}

/** source's file; off the main thread. */
private fun AsukaRuntime.localFile(s: AsukaSource): File {
    val source = s.source
    val f = when {
        source.startsWith("http://") || source.startsWith("https://") -> download(source, s.type)
        source.startsWith("file://") -> File(android.net.Uri.parse(source).path ?: "")
        source.startsWith("/") -> File(source)
        File(app.filesDir, "asuka/assets/$source").exists() -> File(app.filesDir, "asuka/assets/$source")
        else -> {
            val dst = File(app.cacheDir, "asuka-files/assets/$source")
            try {
                dst.parentFile?.mkdirs()
                app.assets.open(source).use { i -> dst.outputStream().use { o -> i.copyTo(o) } }
            } catch (_: java.io.IOException) {
                throw Exception("no file at $source")
            }
            dst
        }
    }
    if (!f.isFile) throw Exception("no file at $source")
    return f
}

private fun AsukaRuntime.download(url: String, type: String): File {
    val c = URL(url).openConnection() as HttpURLConnection
    try {
        if (c.responseCode !in 200..299) throw Exception("couldn't download $url: HTTP ${c.responseCode}")
        val mime = type.ifEmpty { c.contentType?.substringBefore(';')?.trim().orEmpty() }
        var name = URLUtil.guessFileName(url, c.getHeaderField("Content-Disposition"), mime.ifEmpty { null })
            .replace('/', '_').ifEmpty { "file" }
        if (!name.contains('.')) MimeTypeMap.getSingleton().getExtensionFromMimeType(mime)?.let { name += ".$it" }
        val f = File(File(app.cacheDir, "asuka-files/" + UUID.randomUUID()).apply { mkdirs() }, name)
        c.inputStream.use { i -> f.outputStream().use { o -> i.copyTo(o) } }
        return f
    } finally {
        c.disconnect()
    }
}

/** A file's MIME type: the app's, else its extension's. */
fun fileMIME(f: File, type: String): String = type.ifEmpty {
    MimeTypeMap.getSingleton().getMimeTypeFromExtension(f.extension.lowercase()) ?: "application/octet-stream"
}

/** The app's name, as the launcher has it. */
val AsukaRuntime.appName: String get() = app.applicationInfo.loadLabel(app.packageManager).toString()
