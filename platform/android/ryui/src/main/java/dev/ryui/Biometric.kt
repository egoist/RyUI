package dev.ryui

import android.app.KeyguardManager
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import androidx.biometric.BiometricManager
import androidx.biometric.BiometricManager.Authenticators.BIOMETRIC_STRONG
import androidx.biometric.BiometricManager.Authenticators.BIOMETRIC_WEAK
import androidx.biometric.BiometricManager.Authenticators.DEVICE_CREDENTIAL
import androidx.biometric.BiometricPrompt
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * ryui/biometric's module: "check" answers u8 kind (none, fingerprint,
 * face, iris), u8 enrolled, u8 passcode; "authenticate" (u8 flags: 1 no
 * passcode; string reason; string cancel) shows BiometricPrompt, and
 * fails with "canceled", "not available" or "failed".
 */
fun RyUIRuntime.biometricMethods(): Map<String, RyUIModuleMethod> = mapOf(
    "check" to { _, reply ->
        val pm = app.packageManager
        val kind = when {
            pm.hasSystemFeature(PackageManager.FEATURE_FINGERPRINT) -> 1
            Build.VERSION.SDK_INT >= 29 && pm.hasSystemFeature(PackageManager.FEATURE_FACE) -> 2
            Build.VERSION.SDK_INT >= 29 && pm.hasSystemFeature(PackageManager.FEATURE_IRIS) -> 3
            else -> 0
        }
        val enrolled = BiometricManager.from(app).canAuthenticate(BIOMETRIC_WEAK) == BiometricManager.BIOMETRIC_SUCCESS
        val passcode = (app.getSystemService(Context.KEYGUARD_SERVICE) as KeyguardManager).isDeviceSecure
        reply(Result.success(byteArrayOf(kind.toByte(), if (enrolled) 1 else 0, if (passcode) 1 else 0)))
    },
    "authenticate" to { args, reply ->
        val act = activity
        if (act == null) reply(Result.failure(Exception("no screen")))
        else {
            val b = ByteBuffer.wrap(args).order(ByteOrder.LITTLE_ENDIAN)
            val flags = b.get().toInt()
            fun str(): String { val a = ByteArray(b.int); b.get(a); return String(a, Charsets.UTF_8) }
            val reason = str()
            val cancel = str()
            val biometryOnly = flags and 1 != 0
            // A strong biometric alone; or any, or the device's PIN,
            // pattern or password (BIOMETRIC_STRONG with DEVICE_CREDENTIAL
            // isn't on Android 9 and 10).
            val allowed = if (biometryOnly) BIOMETRIC_STRONG else BIOMETRIC_WEAK or DEVICE_CREDENTIAL
            if (BiometricManager.from(app).canAuthenticate(allowed) != BiometricManager.BIOMETRIC_SUCCESS) {
                reply(Result.failure(Exception("not available")))
            } else {
                val title = reason.ifEmpty { app.applicationInfo.loadLabel(app.packageManager).toString() }
                val info = BiometricPrompt.PromptInfo.Builder().setTitle(title).setAllowedAuthenticators(allowed)
                if (biometryOnly) info.setNegativeButtonText(cancel.ifEmpty { act.getString(android.R.string.cancel) })
                val prompt = BiometricPrompt(act, act.mainExecutor, object : BiometricPrompt.AuthenticationCallback() {
                    override fun onAuthenticationSucceeded(result: BiometricPrompt.AuthenticationResult) {
                        reply(Result.success(ByteArray(0)))
                    }

                    override fun onAuthenticationError(code: Int, message: CharSequence) {
                        reply(Result.failure(Exception(when (code) {
                            BiometricPrompt.ERROR_USER_CANCELED, BiometricPrompt.ERROR_NEGATIVE_BUTTON, BiometricPrompt.ERROR_CANCELED -> "canceled"
                            BiometricPrompt.ERROR_NO_BIOMETRICS, BiometricPrompt.ERROR_HW_NOT_PRESENT, BiometricPrompt.ERROR_HW_UNAVAILABLE,
                            BiometricPrompt.ERROR_LOCKOUT, BiometricPrompt.ERROR_LOCKOUT_PERMANENT, BiometricPrompt.ERROR_NO_DEVICE_CREDENTIAL -> "not available"
                            else -> message.toString()
                        })))
                    }
                    // onAuthenticationFailed: a try that wasn't the owner; the prompt stays for another.
                })
                prompt.authenticate(info.build())
            }
        }
    },
)

/** ryui/tracking's module: Android has no tracking prompt; the answer is allowed (3). */
fun trackingMethods(): Map<String, RyUIModuleMethod> = mapOf(
    "request" to { _, reply -> reply(Result.success(byteArrayOf(3))) },
    "status" to { _, reply -> reply(Result.success(byteArrayOf(3))) },
)
