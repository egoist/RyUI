package dev.ryui

import android.content.ActivityNotFoundException
import android.content.ClipData
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.provider.Telephony
import android.text.Html
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.UUID

/*
 * ryui/compose's module. "mail" (JSON: to, cc, bcc, subject, body, html,
 * text, attachments: source, type, name) hands the mail to a mail app
 * (one that takes mailto:, as Android picks it): ACTION_SENDTO, or with
 * attachments ACTION_SEND with mailto: as its selector, the files through
 * the app's FileProvider. "message" (to, subject, body, attachments)
 * hands it to the messages app: smsto:, or with attachments ACTION_SEND
 * to the default SMS app. Both answer u8 3 (unknown: the app doesn't say
 * what the user did) once the user is back, or "unavailable". "can"
 * answers u8s: mail, Mail's composer (iOS's: 0), texts, attachments.
 */
fun RyUIRuntime.composeMethods(): Map<String, RyUIModuleMethod> = mapOf(
    "can" to { _, reply ->
        val pm = app.packageManager
        fun takes(uri: String) = pm.queryIntentActivities(Intent(Intent.ACTION_SENDTO, Uri.parse(uri)), 0).isNotEmpty()
        val phone = pm.hasSystemFeature(PackageManager.FEATURE_TELEPHONY_MESSAGING)
        val text = phone && takes("smsto:")
        val mms = text && Telephony.Sms.getDefaultSmsPackage(app) != null
        reply(Result.success(byteArrayOf(if (takes("mailto:")) 1 else 0, 0, if (text) 1 else 0, if (mms) 1 else 0)))
    },
    "mail" to { args, reply ->
        val o = JSONObject(String(args, Charsets.UTF_8))
        withAttachments(o, reply) { uris, type ->
            val mailto = Intent(Intent.ACTION_SENDTO, Uri.parse("mailto:"))
            val i = if (uris.isEmpty()) mailto else Intent(if (uris.size == 1) Intent.ACTION_SEND else Intent.ACTION_SEND_MULTIPLE).apply {
                this.type = type
                // Mail apps only: the ones that take mailto:.
                selector = mailto
            }
            i.putExtra(Intent.EXTRA_EMAIL, strings(o.optJSONArray("to")))
            i.putExtra(Intent.EXTRA_CC, strings(o.optJSONArray("cc")))
            i.putExtra(Intent.EXTRA_BCC, strings(o.optJSONArray("bcc")))
            i.putExtra(Intent.EXTRA_SUBJECT, o.optString("subject"))
            if (o.optBoolean("html")) {
                // The markup's formatting as spans, which mail apps keep; the markup itself for those that read it.
                i.putExtra(Intent.EXTRA_TEXT, Html.fromHtml(o.optString("body"), Html.FROM_HTML_MODE_LEGACY))
                i.putExtra(Intent.EXTRA_HTML_TEXT, o.optString("body"))
            } else {
                i.putExtra(Intent.EXTRA_TEXT, o.optString("body"))
            }
            stream(i, uris)
            i
        }
    },
    "message" to { args, reply ->
        val o = JSONObject(String(args, Charsets.UTF_8))
        val to = strings(o.optJSONArray("to")).joinToString(";")
        val subject = o.optString("subject")
        val body = if (subject.isEmpty()) o.optString("body") else subject + (if (o.optString("body").isEmpty()) "" else "\n" + o.optString("body"))
        withAttachments(o, reply) { uris, type ->
            if (uris.isEmpty()) {
                Intent(Intent.ACTION_SENDTO, Uri.parse("smsto:" + Uri.encode(to, ";+"))).putExtra("sms_body", body)
            } else {
                val sms = Telephony.Sms.getDefaultSmsPackage(app) ?: return@withAttachments null
                Intent(if (uris.size == 1) Intent.ACTION_SEND else Intent.ACTION_SEND_MULTIPLE).apply {
                    this.type = type
                    setPackage(sms)
                    putExtra("address", to)
                    putExtra("sms_body", body)
                    putExtra(Intent.EXTRA_TEXT, body)
                    stream(this, uris)
                }
            }
        }
    },
)

private fun strings(a: JSONArray?): Array<String> = Array(a?.length() ?: 0) { a!!.optString(it) }

/** The attachments' files through the FileProvider, then intent's start; "unavailable" if it's null or no app takes it. */
private fun RyUIRuntime.withAttachments(o: JSONObject, reply: (Result<ByteArray>) -> Unit, intent: (List<Uri>, String) -> Intent?) {
    val a = o.optJSONArray("attachments") ?: JSONArray()
    val list = List(a.length()) { a.getJSONObject(it) }
    localFiles(list.map { RyUISource(it.optString("source"), it.optString("type")) }) { r ->
        r.onFailure { reply(Result.failure(it)) }.onSuccess { files ->
            val uris = files.mapIndexed { n, f -> namedURI(f, list[n].optString("name")) }
            val types = list.map { it.optString("type").ifEmpty { "application/octet-stream" } }.distinct()
            // One type for all, else their common kind ("image/*"), else any.
            val type = types.singleOrNull() ?: types.map { it.substringBefore('/') }.distinct().singleOrNull()?.let { "$it/*" } ?: "*/*"
            val i = intent(uris, type) ?: return@onSuccess reply(Result.failure(Exception("unavailable")))
            launchAndWait(i, { reply(Result.success(byteArrayOf(3))) }) { e ->
                reply(Result.failure(if (e is ActivityNotFoundException) Exception("unavailable") else e))
            }
        }
    }
}

/** The files as intent's stream, readable by the app that takes it. */
private fun stream(i: Intent, uris: List<Uri>) {
    if (uris.isEmpty()) return
    if (uris.size == 1) i.putExtra(Intent.EXTRA_STREAM, uris[0]) else i.putParcelableArrayListExtra(Intent.EXTRA_STREAM, ArrayList(uris))
    i.clipData = ClipData.newRawUri(null, uris[0]).also { c -> for (u in uris.drop(1)) c.addItem(ClipData.Item(u)) }
    i.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
}

/** f's content URI under name, which the app that takes it shows: a copy in the caches if f's name isn't it. */
private fun RyUIRuntime.namedURI(f: File, name: String): Uri {
    val n = name.replace('/', '_').trim()
    if (n.isEmpty() || n == f.name) return providerURI(f.path)
    val c = File(File(app.cacheDir, "ryui-share/" + UUID.randomUUID()).apply { mkdirs() }, n)
    f.copyTo(c, overwrite = true)
    return providerURI(c.path)
}
