package dev.nui

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.os.Handler
import android.os.Looper

/**
 * nui/network's module: "current", and, while watching, "next" (answered
 * once the state isn't the one given) and "stop", from the default
 * network's capabilities. Replies are u8 connected, u8 type (none, wifi,
 * cellular, ethernet, other), u8 metered, u8 constrained (Data Saver).
 */
@android.annotation.SuppressLint("MissingPermission") // ACCESS_NETWORK_STATE: an app that imports nui/network has it
class NuiNetwork(private val rt: NuiRuntime) {
    private val cm = rt.app.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
    private val main = Handler(Looper.getMainLooper())
    private val next = ArrayList<Pair<ByteArray?, (Result<ByteArray>) -> Unit>>()
    private var caps: NetworkCapabilities? = null
    private var watching = false

    private val callback = object : ConnectivityManager.NetworkCallback() {
        override fun onCapabilitiesChanged(network: Network, c: NetworkCapabilities) { main.post { caps = c; changed() } }
        override fun onLost(network: Network) { main.post { caps = null; changed() } }
    }

    private fun state(): ByteArray {
        val c = caps ?: (if (watching) null else cm.getNetworkCapabilities(cm.activeNetwork))
        if (c == null || !c.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)) return byteArrayOf(0, 0, 0, 0)
        val type = when {
            c.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> 1
            c.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> 2
            c.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> 3
            else -> 4
        }
        val metered = if (c.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED)) 0 else 1
        val saver = if (cm.restrictBackgroundStatus == ConnectivityManager.RESTRICT_BACKGROUND_STATUS_ENABLED) 1 else 0
        return byteArrayOf(1, type.toByte(), metered.toByte(), saver.toByte())
    }

    private fun changed() {
        val s = state()
        val waiting = next.filter { !it.first.contentEquals(s) }
        next.removeAll(waiting.toSet())
        for ((_, reply) in waiting) reply(Result.success(s))
    }

    fun methods(): Map<String, NuiModuleMethod> = mapOf(
        "current" to { _, reply -> reply(Result.success(state())) },
        "next" to { args, reply ->
            if (!watching) {
                watching = true
                caps = cm.getNetworkCapabilities(cm.activeNetwork)
                cm.registerDefaultNetworkCallback(callback)
            }
            val s = state()
            if (args.isEmpty() || !args.contentEquals(s)) reply(Result.success(s)) else next.add(args to reply)
        },
        "stop" to { _, reply ->
            if (watching) {
                watching = false
                cm.unregisterNetworkCallback(callback)
            }
            val ws = ArrayList(next)
            next.clear()
            for ((_, w) in ws) w(Result.failure(Exception("stopped")))
            reply(Result.success(ByteArray(0)))
        },
    )
}
