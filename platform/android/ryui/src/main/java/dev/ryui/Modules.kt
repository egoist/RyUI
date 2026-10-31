package dev.ryui

import android.content.Context
import android.net.Uri
import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.io.File
import java.nio.ByteBuffer
import java.nio.ByteOrder
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** A native module's method: the call's bytes, and a reply (once). */
typealias RyUIModuleMethod = (args: ByteArray, reply: (Result<ByteArray>) -> Unit) -> Unit

/** Registers a module's methods for ui.Call: an app's, alongside the host's own. */
fun RyUIRuntime.registerModule(name: String, methods: Map<String, RyUIModuleMethod>) {
    modules[name] = methods
}

/** Runs a Call command and answers with a Result event. */
fun RyUIRuntime.call(root: Int, args: ByteBuffer) {
    if (args.remaining() < 12) return
    val b = args.order(ByteOrder.LITTLE_ENDIAN)
    val id = b.int
    fun str(): String { val a = ByteArray(b.int); b.get(a); return String(a, Charsets.UTF_8) }
    val module = str()
    val method = str()
    val payload = ByteArray(b.remaining())
    b.get(payload)
    if (!builtinModules) {
        builtinModules = true
        val apps = HashMap(modules)
        registerBuiltinModules()
        modules.putAll(apps) // an app's module of the same name stays the app's
    }
    var answered = false
    val reply: (Result<ByteArray>) -> Unit = { res ->
        onMain {
            if (!answered) {
                answered = true
                val out = ByteBuffer.allocate(5 + (res.getOrNull()?.size ?: 0) + (res.exceptionOrNull()?.message?.length ?: 0) * 4)
                    .order(ByteOrder.LITTLE_ENDIAN)
                out.putInt(id)
                res.fold({ out.put(1); out.put(it) }, { out.put(0); out.put((it.message ?: "error").toByteArray()) })
                val bytes = out.array().copyOf(out.position())
                // An answer, not an event the root listens for: sent as is.
                send(RyUIEvents.result(root, bytes))
            }
        }
    }
    val m = modules[module]?.let { it[method] ?: it["*"] } ?: return reply(Result.failure(Exception("no module method $module.$method")))
    try {
        m(payload, reply)
    } catch (e: Exception) {
        reply(Result.failure(e))
    }
}

/** A webAuth sign-in under way: its session, callback scheme, and start's reply. */
class RyUIAuth(val id: Int, val scheme: String, val reply: (Result<ByteArray>) -> Unit) {
    /** The app went behind the tab: coming back without a callback is closing it. */
    var away = false
}

/** A u32-length string, then the rest. */
private fun split(b: ByteArray): Pair<String, ByteArray> {
    val buf = ByteBuffer.wrap(b).order(ByteOrder.LITTLE_ENDIAN)
    if (buf.remaining() < 4) return "" to ByteArray(0)
    val k = ByteArray(buf.int)
    buf.get(k)
    val rest = ByteArray(buf.remaining())
    buf.get(rest)
    return String(k, Charsets.UTF_8) to rest
}

/** The secure store's key: in the Android Keystore, never out of it. */
private fun secureKey(): SecretKey {
    val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
    (ks.getKey("ryui.secure", null) as? SecretKey)?.let { return it }
    val g = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
    g.init(KeyGenParameterSpec.Builder("ryui.secure", KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
        .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
        .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
        .build())
    return g.generateKey()
}

private fun RyUIRuntime.registerBuiltinModules() {
    val prefs = app.getSharedPreferences("ryui.secure", Context.MODE_PRIVATE)
    modules["secureStore"] = mapOf(
        // args: key; result: u8 found, then the value. Values are AES-GCM
        // ciphertext (iv first), base64, in private preferences.
        "get" to { args, reply ->
            val v = prefs.getString(String(args, Charsets.UTF_8), null)
            if (v == null) reply(Result.success(byteArrayOf(0)))
            else {
                val raw = Base64.decode(v, Base64.NO_WRAP)
                val c = Cipher.getInstance("AES/GCM/NoPadding")
                c.init(Cipher.DECRYPT_MODE, secureKey(), GCMParameterSpec(128, raw, 0, 12))
                reply(Result.success(byteArrayOf(1) + c.doFinal(raw, 12, raw.size - 12)))
            }
        },
        "set" to { args, reply ->
            val (key, value) = split(args)
            val c = Cipher.getInstance("AES/GCM/NoPadding")
            c.init(Cipher.ENCRYPT_MODE, secureKey())
            val sealed = c.iv + c.doFinal(value)
            prefs.edit().putString(key, Base64.encodeToString(sealed, Base64.NO_WRAP)).apply()
            reply(Result.success(ByteArray(0)))
        },
        "delete" to { args, reply ->
            prefs.edit().remove(String(args, Charsets.UTF_8)).apply()
            reply(Result.success(ByteArray(0)))
        },
    )
    modules["browser"] = mapOf(
        // args: the URL: shown in the app (a Custom Tab).
        "open" to { args, reply ->
            val act = activity
            val url = Uri.parse(String(args, Charsets.UTF_8))
            if (act == null || (url.scheme != "https" && url.scheme != "http")) reply(Result.failure(Exception("not a web URL")))
            else {
                androidx.browser.customtabs.CustomTabsIntent.Builder().build().launchUrl(act, url)
                reply(Result.success(ByteArray(0)))
            }
        },
    )
    modules["webAuth"] = mapOf(
        // args: u32 session, u8 flags (1 ephemeral), string URL, string
        // callback scheme (empty: none); result: the callback URL, or the
        // error "canceled". A Custom Tab: it ends with a link of the
        // scheme (the app's), with cancel, or when the user comes back.
        "start" to { args, reply ->
            val b = ByteBuffer.wrap(args).order(ByteOrder.LITTLE_ENDIAN)
            val id = b.int
            b.get() // flags: Custom Tabs keep the browser's cookies
            fun str(): String { val a = ByteArray(b.int); b.get(a); return String(a, Charsets.UTF_8) }
            val url = Uri.parse(str())
            val scheme = str()
            val act = activity
            if (act == null || (url.scheme != "https" && url.scheme != "http")) reply(Result.failure(Exception("not a web URL")))
            else {
                auth?.reply?.invoke(Result.failure(Exception("canceled"))) // one at a time
                auth = RyUIAuth(id, scheme, reply)
                androidx.browser.customtabs.CustomTabsIntent.Builder().build().launchUrl(act, url)
            }
        },
        "cancel" to { args, reply ->
            val id = ByteBuffer.wrap(args).order(ByteOrder.LITTLE_ENDIAN).int
            val a = auth
            if (a != null && a.id == id) {
                auth = null
                a.reply(Result.failure(Exception("canceled")))
                // The app again, over the tab (in its task): it closes.
                activity?.let { act ->
                    act.startActivity(android.content.Intent(act, act.javaClass)
                        .addFlags(android.content.Intent.FLAG_ACTIVITY_CLEAR_TOP or android.content.Intent.FLAG_ACTIVITY_SINGLE_TOP))
                }
            }
            reply(Result.success(ByteArray(0)))
        },
    )
    modules["photos"] = mapOf(
        // args: u8 limit (0: one); result: the picked images' files, a
        // line each (none: cancelled). The system's Photo Picker.
        "pick" to { args, reply ->
            val act = activity
            if (act == null) reply(Result.failure(Exception("no screen")))
            else pickPhotos(act, maxOf(args.firstOrNull()?.toInt() ?: 1, 1), reply)
        },
    )
    modules["location"] = RyUILocation(this).methods()
    modules["network"] = RyUINetwork(this).methods()
    modules["js"] = scriptMethods()
    modules["web"] = webMethods()
    modules["library"] = libraryMethods()
    modules["share"] = shareMethods()
    modules["background"] = RyUIBackground(this).methods()
    modules["notify"] = notifyMethods()
    modules["audio"] = audioMethods()
    modules["camera"] = cameraMethods()
    modules["scan"] = scanMethods()
    modules["screen"] = screenMethods()
    modules["splash"] = RyUISplash.methods()
    modules["clipboard"] = RyUIClipboard(this).methods()
    modules["imageEdit"] = RyUIImageEdit(this).methods()
    modules["speech"] = RyUISpeech(this).methods()
    modules["sensors"] = RyUISensors(this).methods()
    modules["contacts"] = contactsMethods()
    modules["calendar"] = calendarMethods()
    modules["widget"] = widgetMethods()
    modules["files"] = filesMethods()
    modules["biometric"] = biometricMethods()
    modules["tracking"] = trackingMethods()
    modules["push"] = pushMethods()
    modules["audioRecord"] = RyUIAudioRecorder(this).methods()
    modules["periodic"] = RyUIPeriodicHub.methods(this)
    modules["appleAuth"] = mapOf(
        "available" to { _, reply -> reply(Result.success(byteArrayOf(0))) },
        "*" to { _, reply -> reply(Result.failure(Exception("not available"))) },
    )
    // Without its module, a package's calls fail with what's missing.
    for ((m, pkg) in listOf("review" to "review", "integrity" to "integrity", "ageRange" to "agerange")) {
        modules[m] = mapOf("*" to { _, reply -> reply(Result.failure(Exception("ryui/$pkg isn't in this build"))) })
    }
    modules["device"] = mapOf(
        // result: JSON: model, system, systemVersion, appVersion, build.
        "info" to { _, reply ->
            val p = app.packageManager.getPackageInfo(app.packageName, 0)
            val build = if (Build.VERSION.SDK_INT >= 28) p.longVersionCode else @Suppress("DEPRECATION") p.versionCode.toLong()
            val json = org.json.JSONObject(mapOf(
                "model" to Build.MODEL, "system" to "Android", "systemVersion" to Build.VERSION.RELEASE,
                "appVersion" to (p.versionName ?: ""), "build" to build.toString(),
            ))
            reply(Result.success(json.toString().toByteArray()))
        },
        // result: u8 has, then the clipboard's text.
        "clipboard" to { _, reply ->
            val cm = app.getSystemService(android.content.Context.CLIPBOARD_SERVICE) as android.content.ClipboardManager
            val t = cm.primaryClip?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.coerceToText(app)?.toString()
            reply(Result.success(if (t == null) byteArrayOf(0) else byteArrayOf(1) + t.toByteArray()))
        },
        // args: u8 on: the screen stays on while on.
        "keepAwake" to { args, reply ->
            val w = activity?.window
            if (args.firstOrNull()?.toInt() == 1) w?.addFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
            else w?.clearFlags(android.view.WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
            reply(Result.success(ByteArray(0)))
        },
    )
}

private var pickers = 0

/** Picks images with the Photo Picker; copies them to files (its URIs are the picker's). */
private fun RyUIRuntime.pickPhotos(act: RyUIActivity, limit: Int, reply: (Result<ByteArray>) -> Unit) {
    val reg = act.activityResultRegistry
    val key = "ryui-photos-${pickers++}"
    val done = { uris: List<Uri> ->
        Thread {
            val dir = File(app.cacheDir, "ryui-photos").apply { mkdirs() }
            val paths = uris.mapNotNull { u ->
                runCatching {
                    val f = File(dir, java.util.UUID.randomUUID().toString() + ".jpg")
                    app.contentResolver.openInputStream(u)!!.use { i -> f.outputStream().use { o -> i.copyTo(o) } }
                    f.absolutePath
                }.getOrNull()
            }
            reply(Result.success(paths.joinToString("\n").toByteArray()))
        }.start()
    }
    val request = androidx.activity.result.PickVisualMediaRequest(androidx.activity.result.contract.ActivityResultContracts.PickVisualMedia.ImageOnly)
    if (limit > 1) {
        var launcher: androidx.activity.result.ActivityResultLauncher<androidx.activity.result.PickVisualMediaRequest>? = null
        launcher = reg.register(key, androidx.activity.result.contract.ActivityResultContracts.PickMultipleVisualMedia(limit)) { uris ->
            launcher?.unregister()
            done(uris)
        }
        launcher.launch(request)
    } else {
        var launcher: androidx.activity.result.ActivityResultLauncher<androidx.activity.result.PickVisualMediaRequest>? = null
        launcher = reg.register(key, androidx.activity.result.contract.ActivityResultContracts.PickVisualMedia()) { uri ->
            launcher?.unregister()
            done(listOfNotNull(uri))
        }
        launcher.launch(request)
    }
}

/**
 * An optional module's (platform/android/ryui-<name>: Play's libraries,
 * Google Maps, Firebase), which only the apps that import its package are
 * built with: it registers its methods and views. Found by name.
 */
interface RyUIPlugin {
    fun register(rt: RyUIRuntime)
}

/** The optional modules' plugins, by class name. */
private val plugins = listOf(
    "dev.ryui.review.RyUIReviewPlugin",
    "dev.ryui.integrity.RyUIIntegrityPlugin",
    "dev.ryui.agerange.RyUIAgeRangePlugin",
    "dev.ryui.maps.RyUIMapsPlugin",
    "dev.ryui.push.RyUIPushPlugin",
    "dev.ryui.periodic.RyUIPeriodicPlugin",
)

/**
 * Registers the optional modules this app was built with: at start (with
 * the native views), so their modules are the app's, over the fallbacks.
 */
fun RyUIRuntime.registerPlugins() {
    for (name in plugins) {
        try {
            (Class.forName(name).getDeclaredConstructor().newInstance() as RyUIPlugin).register(this)
        } catch (_: ReflectiveOperationException) {
        }
    }
}
