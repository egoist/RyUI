package dev.asuka

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.graphics.Matrix
import android.graphics.Paint
import android.graphics.pdf.PdfRenderer
import android.os.Bundle
import android.os.CancellationSignal
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import android.print.PageRange
import android.print.PrintAttributes
import android.print.PrintDocumentAdapter
import android.print.PrintDocumentInfo
import android.print.PrintJob
import android.print.PrintJobInfo
import android.print.PrintManager
import android.print.AsukaPdfWriter
import android.print.pdf.PrintedPdfDocument
import android.webkit.WebView
import android.webkit.WebViewClient
import androidx.exifinterface.media.ExifInterface
import org.json.JSONObject
import java.io.File
import java.io.FileOutputStream
import java.util.UUID

/*
 * asuka/sdk/print's module, each with JSON: "file" (source, type), "html"
 * (html) and "webView" (view, a WebView's node id), with the dialog's job,
 * landscape and letter (the region's paper), show Android's print screen
 * (PrintManager) and answer u8: 0 cancelled, 1 printed (queued, or saved
 * as a PDF); "pdf" (html, title, width, height, top, right, bottom, left
 * in points) answers the path of the PDF it wrote, named after the title: the HTML laid out by a
 * WebView that's never shown, paginated as it prints.
 */
fun AsukaRuntime.printMethods(): Map<String, AsukaModuleMethod> = mapOf(
    "file" to { args, reply ->
        val o = JSONObject(String(args, Charsets.UTF_8))
        localFiles(listOf(AsukaSource(o.optString("source"), o.optString("type")))) { r ->
            r.onFailure { reply(Result.failure(it)) }.onSuccess { (f) ->
                val mime = fileMIME(f, o.optString("type"))
                val job = o.optString("job").ifEmpty { f.name }
                when {
                    mime == "application/pdf" -> print(job, AsukaPdfFileAdapter(f, job), o, reply)
                    mime.startsWith("image/") -> {
                        val bmp = runCatching { printBitmap(f) }.getOrNull() ?: return@onSuccess reply(Result.failure(Exception("unsupported")))
                        // Turned for an image wider than tall, as PrintHelper does.
                        if (bmp.width > bmp.height) o.put("landscape", true)
                        print(job, AsukaImageAdapter(themed, bmp, job), o, reply, photo = true)
                    }
                    else -> reply(Result.failure(Exception("unsupported")))
                }
            }
        }
    },
    "html" to { args, reply ->
        val o = JSONObject(String(args, Charsets.UTF_8))
        loadPrintPage(o.optString("html")) { web ->
            val job = o.optString("job").ifEmpty { web.title?.takeIf { it.isNotBlank() && it != "about:blank" } ?: appName }
            print(job, web.createPrintDocumentAdapter(job), o, reply) { web.destroy() }
        }
    },
    "webView" to { args, reply ->
        val o = JSONObject(String(args, Charsets.UTF_8))
        val web = nodes[o.optInt("view")]?.view as? AsukaWebView
        if (web == null) reply(Result.failure(Exception("not a WebView")))
        else {
            val job = o.optString("job").ifEmpty { web.title?.takeIf { it.isNotBlank() } ?: appName }
            print(job, web.createPrintDocumentAdapter(job), o, reply)
        }
    },
    "pdf" to { args, reply ->
        val o = JSONObject(String(args, Charsets.UTF_8))
        fun mils(k: String) = (o.optDouble(k, 0.0) / 72 * 1000).toInt()
        val w = mils("width")
        val h = mils("height")
        if (w <= 0 || h <= 0 || mils("left") + mils("right") >= w || mils("top") + mils("bottom") >= h) {
            reply(Result.failure(Exception("the margins leave no room on the page")))
        } else {
            val attrs = PrintAttributes.Builder()
                .setMediaSize(PrintAttributes.MediaSize("asuka", "asuka", w, h))
                .setResolution(PrintAttributes.Resolution("pdf", "pdf", 600, 600))
                .setMinMargins(PrintAttributes.Margins(mils("left"), mils("top"), mils("right"), mils("bottom")))
                .setColorMode(PrintAttributes.COLOR_MODE_COLOR)
                .build()
            loadPrintPage(o.optString("html")) { web ->
                // Named after the page's title, as it's shared or mailed, in a folder of its own.
                val title = o.optString("title").replace('/', '-').replace(':', '-').trim().ifEmpty { "Document" }
                val f = File(app.cacheDir, "asuka-print/" + UUID.randomUUID() + "/" + title + ".pdf")
                AsukaPdfWriter.write(web.createPrintDocumentAdapter(appName), attrs, f) { e ->
                    web.destroy()
                    reply(if (e == null) Result.success(f.absolutePath.toByteArray()) else Result.failure(e))
                }
            }
        }
    },
)

/** html in a WebView that's never shown, once it's loaded. */
private fun AsukaRuntime.loadPrintPage(html: String, loaded: (WebView) -> Unit) {
    val web = WebView(themed)
    web.settings.javaScriptEnabled = true
    var done = false
    web.webViewClient = object : WebViewClient() {
        override fun onPageFinished(view: WebView, url: String?) {
            if (done) return
            done = true
            // Its fonts too, which the load doesn't wait for.
            view.evaluateJavascript("document.fonts ? document.fonts.ready.then(() => true) : true") { loaded(view) }
        }
    }
    web.loadDataWithBaseURL(null, html, "text/html", "utf-8", null)
}

/**
 * Shows the print screen for adapter and answers once it's closed, with
 * what became of the job: printed (queued, started, or saved as a PDF),
 * cancelled, or failed.
 */
private fun AsukaRuntime.print(job: String, adapter: PrintDocumentAdapter, o: JSONObject, reply: (Result<ByteArray>) -> Unit,
                              photo: Boolean = false, after: () -> Unit = {}) {
    val act = activity ?: return reply(Result.failure(Exception("no screen")))
    val pm = act.getSystemService(Context.PRINT_SERVICE) as PrintManager
    val attrs = PrintAttributes.Builder()
    if (o.optBoolean("landscape")) {
        // A size to turn: the region's paper, which the user can change.
        val size = if (o.optBoolean("letter")) PrintAttributes.MediaSize.NA_LETTER else PrintAttributes.MediaSize.ISO_A4
        attrs.setMediaSize(size.asLandscape())
    }
    if (photo) attrs.setColorMode(PrintAttributes.COLOR_MODE_COLOR)
    var printJob: PrintJob? = null
    val main = Handler(Looper.getMainLooper())
    val tracked = object : PrintDocumentAdapter() {
        override fun onStart() = adapter.onStart()
        override fun onLayout(old: PrintAttributes?, new: PrintAttributes, cancel: CancellationSignal?, cb: LayoutResultCallback, extras: Bundle?) =
            adapter.onLayout(old, new, cancel, cb, extras)
        override fun onWrite(pages: Array<out PageRange>, dest: ParcelFileDescriptor, cancel: CancellationSignal?, cb: WriteResultCallback) =
            adapter.onWrite(pages, dest, cancel, cb)
        override fun onFinish() {
            adapter.onFinish()
            // The spooler moves the job out of "created" as its screen closes.
            fun check() {
                val state = printJob?.info?.state ?: PrintJobInfo.STATE_CANCELED
                when (state) {
                    PrintJobInfo.STATE_CREATED -> main.postDelayed(::check, 50)
                    PrintJobInfo.STATE_CANCELED -> { after(); reply(Result.success(byteArrayOf(0))) }
                    PrintJobInfo.STATE_FAILED -> { after(); reply(Result.failure(Exception("the job failed"))) }
                    else -> { after(); reply(Result.success(byteArrayOf(1))) }
                }
            }
            main.post(::check)
        }
    }
    try {
        printJob = pm.print(job, tracked, attrs.build())
    } catch (e: Exception) {
        after()
        reply(Result.failure(e))
    }
}

/** A PDF file, printed as it is. */
private class AsukaPdfFileAdapter(private val file: File, private val name: String) : PrintDocumentAdapter() {
    private val pages = runCatching {
        ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_READ_ONLY).use { fd -> PdfRenderer(fd).use { it.pageCount } }
    }.getOrDefault(PrintDocumentInfo.PAGE_COUNT_UNKNOWN)

    override fun onLayout(old: PrintAttributes?, new: PrintAttributes, cancel: CancellationSignal?, cb: LayoutResultCallback, extras: Bundle?) {
        if (cancel?.isCanceled == true) return cb.onLayoutCancelled()
        cb.onLayoutFinished(PrintDocumentInfo.Builder(name).setContentType(PrintDocumentInfo.CONTENT_TYPE_DOCUMENT).setPageCount(pages).build(), old == null)
    }

    override fun onWrite(pages: Array<out PageRange>, dest: ParcelFileDescriptor, cancel: CancellationSignal?, cb: WriteResultCallback) {
        try {
            file.inputStream().use { i -> FileOutputStream(dest.fileDescriptor).use { o -> i.copyTo(o) } }
            if (cancel?.isCanceled == true) cb.onWriteCancelled() else cb.onWriteFinished(arrayOf(PageRange.ALL_PAGES))
        } catch (e: Exception) {
            cb.onWriteFailed(e.message)
        }
    }
}

/** An image, fit to the page's printable area and centred, as PrintHelper prints one. */
private class AsukaImageAdapter(private val ctx: Context, private val bitmap: Bitmap, private val name: String) : PrintDocumentAdapter() {
    private var attrs: PrintAttributes? = null

    override fun onLayout(old: PrintAttributes?, new: PrintAttributes, cancel: CancellationSignal?, cb: LayoutResultCallback, extras: Bundle?) {
        attrs = new
        if (cancel?.isCanceled == true) return cb.onLayoutCancelled()
        cb.onLayoutFinished(PrintDocumentInfo.Builder(name).setContentType(PrintDocumentInfo.CONTENT_TYPE_PHOTO).setPageCount(1).build(),
            old == null || old.mediaSize != new.mediaSize || old.minMargins != new.minMargins)
    }

    override fun onWrite(pages: Array<out PageRange>, dest: ParcelFileDescriptor, cancel: CancellationSignal?, cb: WriteResultCallback) {
        val doc = PrintedPdfDocument(ctx, attrs ?: return cb.onWriteFailed("no layout"))
        try {
            val page = doc.startPage(0)
            val r = page.info.contentRect
            val scale = minOf(r.width().toFloat() / bitmap.width, r.height().toFloat() / bitmap.height)
            val m = Matrix().apply {
                postScale(scale, scale)
                postTranslate(r.left + (r.width() - bitmap.width * scale) / 2, r.top + (r.height() - bitmap.height * scale) / 2)
            }
            page.canvas.drawBitmap(bitmap, m, Paint(Paint.FILTER_BITMAP_FLAG))
            doc.finishPage(page)
            if (cancel?.isCanceled == true) return cb.onWriteCancelled()
            FileOutputStream(dest.fileDescriptor).use { doc.writeTo(it) }
            cb.onWriteFinished(arrayOf(PageRange.ALL_PAGES))
        } catch (e: Exception) {
            cb.onWriteFailed(e.message)
        } finally {
            doc.close()
        }
    }
}

/**
 * The image at f, upright (its EXIF orientation applied), at most 3500
 * pixels on a side: PrintHelper's size, about 300 dpi over a page.
 */
private fun printBitmap(f: File): Bitmap? {
    val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
    BitmapFactory.decodeFile(f.path, bounds)
    if (bounds.outWidth <= 0 || bounds.outHeight <= 0) return null
    var sample = 1
    while (maxOf(bounds.outWidth, bounds.outHeight) / (sample * 2) >= 3500) sample *= 2
    val bmp = BitmapFactory.decodeFile(f.path, BitmapFactory.Options().apply { inSampleSize = sample }) ?: return null
    val turn = runCatching { ExifInterface(f.path).rotationDegrees }.getOrDefault(0)
    if (turn == 0) return bmp
    return Bitmap.createBitmap(bmp, 0, 0, bmp.width, bmp.height, Matrix().apply { postRotate(turn.toFloat()) }, true)
}
