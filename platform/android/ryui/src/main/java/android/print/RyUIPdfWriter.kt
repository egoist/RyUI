package android.print

import android.os.CancellationSignal
import android.os.ParcelFileDescriptor
import java.io.File

/**
 * Writes what a PrintDocumentAdapter prints (a WebView's) to a PDF file,
 * without the print dialog: ryui/print's PDF. The adapter's callbacks
 * have no public constructor in the SDK, but one in their package, which
 * is why this is in android.print, as Expo's expo-print does it.
 */
object RyUIPdfWriter {
    fun write(adapter: PrintDocumentAdapter, attrs: PrintAttributes, file: File, done: (Throwable?) -> Unit) {
        var finished = false
        fun finish(e: Throwable?) {
            if (finished) return
            finished = true
            runCatching { adapter.onFinish() }
            done(e)
        }
        adapter.onStart()
        adapter.onLayout(null, attrs, null, object : PrintDocumentAdapter.LayoutResultCallback() {
            override fun onLayoutFinished(info: PrintDocumentInfo, changed: Boolean) {
                val fd = try {
                    file.parentFile?.mkdirs()
                    ParcelFileDescriptor.open(file, ParcelFileDescriptor.MODE_CREATE or ParcelFileDescriptor.MODE_TRUNCATE or ParcelFileDescriptor.MODE_READ_WRITE)
                } catch (e: Exception) {
                    return finish(e)
                }
                adapter.onWrite(arrayOf(PageRange.ALL_PAGES), fd, CancellationSignal(), object : PrintDocumentAdapter.WriteResultCallback() {
                    override fun onWriteFinished(pages: Array<out PageRange>) {
                        fd.close()
                        finish(null)
                    }

                    override fun onWriteFailed(error: CharSequence?) {
                        fd.close()
                        finish(Exception(error?.toString() ?: "the page couldn't be written"))
                    }

                    override fun onWriteCancelled() {
                        fd.close()
                        finish(Exception("cancelled"))
                    }
                })
            }

            override fun onLayoutFailed(error: CharSequence?) = finish(Exception(error?.toString() ?: "the page couldn't be laid out"))
            override fun onLayoutCancelled() = finish(Exception("cancelled"))
        }, null)
    }
}
