package dev.ryui.integrity

import com.google.android.play.core.integrity.IntegrityManagerFactory
import com.google.android.play.core.integrity.StandardIntegrityManager
import dev.ryui.RyUIPlugin
import dev.ryui.RyUIRuntime
import dev.ryui.registerModule

/**
 * ryui/integrity's Play Integrity: "prepare" (the cloud project number)
 * readies a token provider; "token" (the request hash) answers its token.
 * App Attest's methods are iOS's.
 */
class RyUIIntegrityPlugin : RyUIPlugin {
    private var provider: StandardIntegrityManager.StandardIntegrityTokenProvider? = null

    override fun register(rt: RyUIRuntime) {
        val ios = { _: ByteArray, reply: (Result<ByteArray>) -> Unit -> reply(Result.failure(Exception("App Attest is iOS's: use Prepare and Token on Android"))) }
        rt.registerModule("integrity", mapOf(
            "supported" to { _, reply -> reply(Result.success(byteArrayOf(1))) },
            "generateKey" to ios, "attestKey" to ios, "assert" to ios,
            "prepare" to { args, reply ->
                val n = String(args, Charsets.UTF_8).toLongOrNull()
                if (n == null) reply(Result.failure(Exception("bad cloud project number")))
                else IntegrityManagerFactory.createStandard(rt.app)
                    .prepareIntegrityToken(StandardIntegrityManager.PrepareIntegrityTokenRequest.builder().setCloudProjectNumber(n).build())
                    .addOnCompleteListener { t ->
                        if (t.isSuccessful) { provider = t.result; reply(Result.success(ByteArray(0))) }
                        else reply(Result.failure(t.exception ?: Exception("Play Integrity isn't ready")))
                    }
            },
            "token" to { args, reply ->
                val p = provider
                if (p == null) reply(Result.failure(Exception("call integrity.Prepare first")))
                else p.request(StandardIntegrityManager.StandardIntegrityTokenRequest.builder().setRequestHash(String(args, Charsets.UTF_8)).build())
                    .addOnCompleteListener { t ->
                        if (t.isSuccessful) reply(Result.success(t.result.token().toByteArray()))
                        else reply(Result.failure(t.exception ?: Exception("no token")))
                    }
            },
        ))
    }
}
