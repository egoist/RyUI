package dev.asuka

import android.content.Context
import android.util.Log
import java.io.File

/** The Go core's JNI entry points (mobile/jni_android.c). Main thread. */
object AsukaNative {
    @JvmStatic external fun start(runtime: AsukaRuntime)
    @JvmStatic external fun attach()
    @JvmStatic external fun event(bytes: ByteArray)
    @JvmStatic external fun listCount(list: Int): Long
    @JvmStatic external fun listBind(list: Int, index: Long, cell: Int)
    @JvmStatic external fun listUnbind(list: Int, cell: Int)
    @JvmStatic external fun runPosted()
    @JvmStatic external fun setenv(key: String, value: String)
    @JvmStatic external fun crashStart(info: String): String
    @JvmStatic external fun crashNotify()

    private var loaded = false
    private var loader: Thread? = null

    /**
     * Starts loading the core on a thread of its own, as the app starts:
     * mapping the library and starting Go's runtime then overlap with the
     * Activity's own start, rather than waiting for its first frame.
     */
    fun preload(context: Context) {
        if (loader != null || loaded) return
        val app = context.applicationContext
        loader = Thread({ open(app) }, "asuka-load").apply { start() }
    }

    /** Loads the core, or waits for preload to have. */
    fun load(context: Context) {
        if (loaded) return
        loaded = true
        loader?.let { it.join(); return }
        open(context)
    }

    /**
     * In development `asuka dev` pushes the app's library to
     * files/asuka/libasuka.so, so a change needs no reinstall; otherwise (and
     * always in a release build) it's packaged with the app.
     */
    private fun open(context: Context) {
        val debuggable = context.applicationInfo.flags and android.content.pm.ApplicationInfo.FLAG_DEBUGGABLE != 0
        val dev = File(context.filesDir, "asuka/libasuka.so")
        if (debuggable && dev.exists()) {
            Log.i("asuka", "loading ${dev.path}")
            System.load(dev.absolutePath)
        } else {
            System.loadLibrary("asuka")
        }
        // Where the Go code keeps its files (asuka/sdk/store). A library from
        // before asuka_setenv hasn't it: nothing to set then.
        runCatching { setenv("ASUKA_DATA", context.filesDir.absolutePath) }
        // asuka/sdk/crash's record of the run, before the Go code's main.
        AsukaCrash.start(context)
    }
}
