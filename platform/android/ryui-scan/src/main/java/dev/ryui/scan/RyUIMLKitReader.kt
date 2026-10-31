package dev.ryui.scan

import android.content.Context
import android.net.Uri
import androidx.camera.core.ExperimentalGetImage
import androidx.camera.core.ImageProxy
import com.google.mlkit.vision.barcode.BarcodeScanner
import com.google.mlkit.vision.barcode.BarcodeScannerOptions
import com.google.mlkit.vision.barcode.BarcodeScanning
import com.google.mlkit.vision.barcode.common.Barcode
import com.google.mlkit.vision.common.InputImage
import dev.ryui.RyUICodeAnalyzer
import dev.ryui.RyUICodeReader
import java.io.File

/** ryui/scan's reader, with ML Kit. */
class RyUIMLKitReader : RyUICodeReader {
    private val formats = mapOf(
        "qr" to Barcode.FORMAT_QR_CODE, "aztec" to Barcode.FORMAT_AZTEC,
        "datamatrix" to Barcode.FORMAT_DATA_MATRIX, "pdf417" to Barcode.FORMAT_PDF417,
        "ean13" to Barcode.FORMAT_EAN_13, "ean8" to Barcode.FORMAT_EAN_8,
        "upca" to Barcode.FORMAT_UPC_A, "upce" to Barcode.FORMAT_UPC_E,
        "code128" to Barcode.FORMAT_CODE_128, "code39" to Barcode.FORMAT_CODE_39,
        "code93" to Barcode.FORMAT_CODE_93, "codabar" to Barcode.FORMAT_CODABAR,
        "itf" to Barcode.FORMAT_ITF,
    )
    private val kindOf = formats.entries.associate { (k, v) -> v to k }

    private fun client(kinds: Set<String>): BarcodeScanner {
        val fs = kinds.mapNotNull { formats[it] }.ifEmpty { listOf(Barcode.FORMAT_ALL_FORMATS) }
        val o = BarcodeScannerOptions.Builder().setBarcodeFormats(fs[0], *fs.drop(1).toIntArray()).build()
        return BarcodeScanning.getClient(o)
    }

    private fun codes(list: List<Barcode>): List<Pair<String, String>> =
        list.mapNotNull { b -> val k = kindOf[b.format]; val v = b.rawValue; if (k == null || v == null) null else k to v }.distinct()

    override fun analyzer(kinds: Set<String>, found: (String, String) -> Unit): RyUICodeAnalyzer {
        val c = client(kinds)
        return object : RyUICodeAnalyzer {
            @ExperimentalGetImage
            override fun analyze(p: ImageProxy) {
                val img = p.image ?: run { p.close(); return }
                // Its listeners run on the main thread; the next frame
                // waits for this one (the analysis keeps only the latest).
                c.process(InputImage.fromMediaImage(img, p.imageInfo.rotationDegrees))
                    .addOnSuccessListener { for ((k, v) in codes(it)) found(k, v) }
                    .addOnCompleteListener { p.close() }
            }

            override fun close() = c.close()
        }
    }

    override fun file(context: Context, path: String, kinds: Set<String>, done: (Result<List<Pair<String, String>>>) -> Unit) {
        val img = try {
            InputImage.fromFilePath(context, Uri.fromFile(File(path)))
        } catch (e: Exception) {
            done(Result.failure(Exception("can't read the image"))); return
        }
        val c = client(kinds)
        c.process(img)
            .addOnSuccessListener { done(Result.success(codes(it))) }
            .addOnFailureListener { done(Result.failure(it)) }
            .addOnCompleteListener { c.close() }
    }
}
