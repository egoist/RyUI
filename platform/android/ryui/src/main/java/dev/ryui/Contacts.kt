package dev.ryui

import android.Manifest
import android.app.Activity
import android.content.ContentValues
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.provider.ContactsContract
import android.provider.ContactsContract.CommonDataKinds
import androidx.activity.result.ActivityResultLauncher
import androidx.activity.result.contract.ActivityResultContracts
import org.json.JSONArray
import org.json.JSONObject

private var contactLaunches = 0

/**
 * ryui/contacts' module, with ContactsContract: "request" (u8 access: 1
 * denied, 3 full), "list" (JSON: name, limit) and "pick" answer contacts
 * as JSON; "add" (a contact's JSON) opens the contacts app's new contact
 * screen and answers u8 saved. Pick needs no permission: the picker
 * grants the one contact; without READ_CONTACTS only its name is read.
 */
fun RyUIRuntime.contactsMethods(): Map<String, RyUIModuleMethod> {
    fun allowed() = app.checkSelfPermission(Manifest.permission.READ_CONTACTS) == PackageManager.PERMISSION_GRANTED
    return mapOf(
        "request" to { _, reply ->
            withPermission(arrayOf(Manifest.permission.READ_CONTACTS)) { ok -> reply(Result.success(byteArrayOf(if (ok) 3 else 1))) }
        },
        "list" to { args, reply ->
            if (!allowed()) reply(Result.failure(Exception("not allowed")))
            else {
                val o = runCatching { JSONObject(String(args, Charsets.UTF_8)) }.getOrDefault(JSONObject())
                Thread { reply(runCatching { readContacts(o.optString("name"), o.optInt("limit"), null).toString().toByteArray() }) }.start()
            }
        },
        "pick" to { _, reply ->
            val act = activity
            if (act == null) reply(Result.failure(Exception("no screen")))
            else {
                var l: ActivityResultLauncher<Void?>? = null
                l = act.activityResultRegistry.register("ryui-contact-${contactLaunches++}", ActivityResultContracts.PickContact()) { uri ->
                    l?.unregister()
                    if (uri == null) reply(Result.success(ByteArray(0)))
                    else Thread { reply(runCatching { pickedContact(uri, allowed()).toString().toByteArray() }) }.start()
                }
                l.launch(null)
            }
        },
        "add" to { args, reply ->
            val act = activity
            val o = runCatching { JSONObject(String(args, Charsets.UTF_8)) }.getOrNull()
            if (act == null || o == null) reply(Result.failure(Exception(if (o == null) "bad args" else "no screen")))
            else {
                val i = Intent(ContactsContract.Intents.Insert.ACTION).setType(ContactsContract.RawContacts.CONTENT_TYPE)
                val name = listOf(o.optString("givenName"), o.optString("familyName")).filter { it.isNotEmpty() }.joinToString(" ").ifEmpty { o.optString("name") }
                if (name.isNotEmpty()) i.putExtra(ContactsContract.Intents.Insert.NAME, name)
                o.optString("organization").takeIf { it.isNotEmpty() }?.let { i.putExtra(ContactsContract.Intents.Insert.COMPANY, it) }
                o.optString("jobTitle").takeIf { it.isNotEmpty() }?.let { i.putExtra(ContactsContract.Intents.Insert.JOB_TITLE, it) }
                o.optString("note").takeIf { it.isNotEmpty() }?.let { i.putExtra(ContactsContract.Intents.Insert.NOTES, it) }
                // Every phone and email (the extras take three of each), and the birthday: as data rows.
                val data = ArrayList<ContentValues>()
                fun items(key: String, mime: String, valueCol: String, typeCol: String, labelCol: String, custom: Int) {
                    val a = o.optJSONArray(key) ?: return
                    for (n in 0 until a.length()) {
                        val it = a.optJSONObject(n) ?: continue
                        data.add(ContentValues().apply {
                            put(ContactsContract.Data.MIMETYPE, mime)
                            put(valueCol, it.optString("value"))
                            val label = it.optString("label")
                            if (label.isEmpty()) put(typeCol, if (key == "phones") CommonDataKinds.Phone.TYPE_MOBILE else CommonDataKinds.Email.TYPE_HOME)
                            else { put(typeCol, custom); put(labelCol, label) }
                        })
                    }
                }
                items("phones", CommonDataKinds.Phone.CONTENT_ITEM_TYPE, CommonDataKinds.Phone.NUMBER, CommonDataKinds.Phone.TYPE, CommonDataKinds.Phone.LABEL, CommonDataKinds.Phone.TYPE_CUSTOM)
                items("emails", CommonDataKinds.Email.CONTENT_ITEM_TYPE, CommonDataKinds.Email.ADDRESS, CommonDataKinds.Email.TYPE, CommonDataKinds.Email.LABEL, CommonDataKinds.Email.TYPE_CUSTOM)
                o.optString("birthday").takeIf { it.isNotEmpty() }?.let { b ->
                    data.add(ContentValues().apply {
                        put(ContactsContract.Data.MIMETYPE, CommonDataKinds.Event.CONTENT_ITEM_TYPE)
                        put(CommonDataKinds.Event.TYPE, CommonDataKinds.Event.TYPE_BIRTHDAY)
                        put(CommonDataKinds.Event.START_DATE, b) // YYYY-MM-DD or --MM-DD, as Android keeps it
                    })
                }
                if (data.isNotEmpty()) i.putParcelableArrayListExtra(ContactsContract.Intents.Insert.DATA, data)
                i.putExtra("finishActivityOnSaveCompleted", true) // back to the app once saved
                var l: ActivityResultLauncher<Intent>? = null
                l = act.activityResultRegistry.register("ryui-contact-${contactLaunches++}", ActivityResultContracts.StartActivityForResult()) { r ->
                    l?.unregister()
                    reply(Result.success(byteArrayOf(if (r.resultCode == Activity.RESULT_OK) 1 else 0)))
                }
                try { l.launch(i) } catch (e: android.content.ActivityNotFoundException) { l.unregister(); reply(Result.failure(Exception("no contacts app"))) }
            }
        },
    )
}

/** The contacts (all, or those whose name has name; or the one id), with their details, as JSON. */
private fun RyUIRuntime.readContacts(name: String, limit: Int, id: Long?): JSONArray {
    val cr = app.contentResolver
    val base = if (name.isNotEmpty()) Uri.withAppendedPath(ContactsContract.Contacts.CONTENT_FILTER_URI, Uri.encode(name)) else ContactsContract.Contacts.CONTENT_URI
    val contacts = LinkedHashMap<Long, JSONObject>()
    cr.query(base, arrayOf(ContactsContract.Contacts._ID, ContactsContract.Contacts.DISPLAY_NAME_PRIMARY),
        id?.let { "${ContactsContract.Contacts._ID} = ?" }, id?.let { arrayOf(it.toString()) }, ContactsContract.Contacts.SORT_KEY_PRIMARY)?.use { c ->
        while (c.moveToNext() && (limit <= 0 || contacts.size < limit)) {
            contacts[c.getLong(0)] = JSONObject().put("id", c.getLong(0).toString()).put("name", c.getString(1) ?: "")
                .put("givenName", "").put("familyName", "").put("organization", "").put("jobTitle", "")
                .put("phones", JSONArray()).put("emails", JSONArray()).put("birthday", "").put("note", "")
        }
    }
    if (contacts.isEmpty()) return JSONArray()
    // Their details, in one query (in chunks, for SQLite's limit on parameters).
    val res = app.resources
    for (chunk in contacts.keys.chunked(500)) {
        val sel = "${ContactsContract.Data.CONTACT_ID} IN (${chunk.joinToString(",") { "?" }})"
        cr.query(ContactsContract.Data.CONTENT_URI, arrayOf(ContactsContract.Data.CONTACT_ID, ContactsContract.Data.MIMETYPE,
            ContactsContract.Data.DATA1, ContactsContract.Data.DATA2, ContactsContract.Data.DATA3, ContactsContract.Data.DATA4),
            sel, chunk.map { it.toString() }.toTypedArray(), null)?.use { c ->
            while (c.moveToNext()) {
                val o = contacts[c.getLong(0)] ?: continue
                val d1 = c.getString(2) ?: ""; val d2 = c.getString(3); val d3 = c.getString(4)
                when (c.getString(1)) {
                    CommonDataKinds.StructuredName.CONTENT_ITEM_TYPE -> o.put("givenName", d2 ?: "").put("familyName", d3 ?: "")
                    CommonDataKinds.Organization.CONTENT_ITEM_TYPE -> o.put("organization", d1).put("jobTitle", c.getString(5) ?: "")
                    CommonDataKinds.Phone.CONTENT_ITEM_TYPE -> o.getJSONArray("phones").put(JSONObject().put("value", d1)
                        .put("label", CommonDataKinds.Phone.getTypeLabel(res, d2?.toIntOrNull() ?: 0, d3).toString()))
                    CommonDataKinds.Email.CONTENT_ITEM_TYPE -> o.getJSONArray("emails").put(JSONObject().put("value", d1)
                        .put("label", CommonDataKinds.Email.getTypeLabel(res, d2?.toIntOrNull() ?: 0, d3).toString()))
                    CommonDataKinds.Event.CONTENT_ITEM_TYPE -> if (d2?.toIntOrNull() == CommonDataKinds.Event.TYPE_BIRTHDAY) o.put("birthday", d1)
                    CommonDataKinds.Note.CONTENT_ITEM_TYPE -> o.put("note", d1)
                }
            }
        }
    }
    return JSONArray(contacts.values)
}

/** A picked contact: all of it with the permission; else its name, which the picker's grant reads. */
private fun RyUIRuntime.pickedContact(uri: Uri, allowed: Boolean): JSONObject {
    val cr = app.contentResolver
    var id = -1L
    var name = ""
    cr.query(uri, arrayOf(ContactsContract.Contacts._ID, ContactsContract.Contacts.DISPLAY_NAME_PRIMARY), null, null, null)?.use { c ->
        if (c.moveToFirst()) { id = c.getLong(0); name = c.getString(1) ?: "" }
    }
    if (allowed && id >= 0) readContacts("", 0, id).optJSONObject(0)?.let { return it }
    return JSONObject().put("id", if (id >= 0) id.toString() else "").put("name", name).put("givenName", "").put("familyName", "")
        .put("organization", "").put("jobTitle", "").put("phones", JSONArray()).put("emails", JSONArray()).put("birthday", "").put("note", "")
}
