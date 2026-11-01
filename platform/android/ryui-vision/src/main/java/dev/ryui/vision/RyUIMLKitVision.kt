package dev.ryui.vision

import android.content.Context
import android.graphics.Rect
import android.net.Uri
import com.google.android.gms.tasks.Tasks
import com.google.mlkit.vision.barcode.BarcodeScanning
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.common.InputImage
import com.google.mlkit.vision.face.FaceDetection
import com.google.mlkit.vision.face.FaceDetectorOptions
import com.google.mlkit.vision.text.Text
import com.google.mlkit.vision.text.TextRecognition
import com.google.mlkit.vision.text.chinese.ChineseTextRecognizerOptions
import dev.ryui.RyUIVisionReader
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.text.BreakIterator
import java.util.Locale

/** ryui/vision's reader, with ML Kit. Blocking: called off the main thread. */
class RyUIMLKitVision : RyUIVisionReader {
    private val kinds = mapOf(
        Barcode.FORMAT_QR_CODE to "qr", Barcode.FORMAT_AZTEC to "aztec", Barcode.FORMAT_DATA_MATRIX to "datamatrix",
        Barcode.FORMAT_PDF417 to "pdf417", Barcode.FORMAT_EAN_13 to "ean13", Barcode.FORMAT_EAN_8 to "ean8",
        Barcode.FORMAT_UPC_A to "upca", Barcode.FORMAT_UPC_E to "upce", Barcode.FORMAT_CODE_128 to "code128",
        Barcode.FORMAT_CODE_39 to "code39", Barcode.FORMAT_CODE_93 to "code93", Barcode.FORMAT_CODABAR to "codabar",
        Barcode.FORMAT_ITF to "itf",
    )

    override fun analyze(context: Context, path: String, text: Boolean, faces: Boolean, codes: Boolean): JSONObject {
        // Turned upright as its EXIF says: width and height are the upright image's.
        val img = try {
            InputImage.fromFilePath(context, Uri.fromFile(File(path)))
        } catch (e: Exception) {
            throw Exception("no image")
        }
        val w = img.width.toDouble(); val h = img.height.toDouble()
        fun box(r: Rect?): JSONObject {
            val b = r ?: Rect()
            return JSONObject().put("x", b.left / w).put("y", b.top / h).put("w", b.width() / w).put("h", b.height() / h)
        }
        val out = JSONObject().put("width", img.width).put("height", img.height)
        val lines = JSONArray()
        if (text) {
            val c = TextRecognition.getClient(ChineseTextRecognizerOptions.Builder().build())
            try {
                for (block in Tasks.await(c.process(img)).textBlocks) for (l in block.lines) lines.put(line(l, ::box))
            } finally {
                c.close()
            }
        }
        out.put("lines", lines)
        val faceBoxes = JSONArray()
        if (faces) {
            val c = FaceDetection.getClient(FaceDetectorOptions.Builder().setPerformanceMode(FaceDetectorOptions.PERFORMANCE_MODE_ACCURATE).build())
            try {
                for (f in Tasks.await(c.process(img))) faceBoxes.put(box(f.boundingBox))
            } finally {
                c.close()
            }
        }
        out.put("faces", faceBoxes)
        val found = JSONArray()
        if (codes) {
            val c = BarcodeScanning.getClient()
            try {
                for (b in Tasks.await(c.process(img))) {
                    found.put(JSONObject().put("kind", kinds[b.format] ?: "other").put("value", b.rawValue ?: "").put("box", box(b.boundingBox)))
                }
            } finally {
                c.close()
            }
        }
        out.put("codes", found)
        return out
    }

    /** The line, with what BreakIterator finds in it, each boxed around its symbols. */
    private fun line(l: Text.Line, box: (Rect?) -> JSONObject): JSONObject {
        val t = l.text
        // Each UTF-16 index's symbol box: the symbols come in the text's order.
        val at = arrayOfNulls<Rect>(t.length)
        var cursor = 0
        for (e in l.elements) {
            for (s in e.symbols) {
                val i = t.indexOf(s.text, cursor)
                if (i < 0) continue
                for (k in i until i + s.text.length) at[k] = s.boundingBox
                cursor = i + s.text.length
            }
            // An element with no symbols: its box for its text.
            if (e.symbols.isEmpty()) {
                val i = t.indexOf(e.text, cursor)
                if (i >= 0) {
                    for (k in i until i + e.text.length) at[k] = e.boundingBox
                    cursor = i + e.text.length
                }
            }
        }
        val words = JSONArray()
        val it = BreakIterator.getWordInstance(Locale.getDefault())
        it.setText(t)
        var a = it.first()
        var b = it.next()
        while (b != BreakIterator.DONE) {
            val word = t.substring(a, b)
            if (word.isNotBlank()) {
                var r: Rect? = null
                for (k in a until b) at[k]?.let { s -> r = r?.let { u -> Rect(u).apply { union(s) } } ?: Rect(s) }
                words.put(JSONObject().put("text", word).put("box", box(r ?: l.boundingBox))
                    .put("start", t.codePointCount(0, a)).put("end", t.codePointCount(0, b)))
            }
            a = b
            b = it.next()
        }
        return JSONObject().put("text", t).put("box", box(l.boundingBox)).put("words", words)
    }
}
