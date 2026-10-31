package dev.ryui.agerange

import com.google.android.play.agesignals.AgeSignalsAccessRequest
import com.google.android.play.agesignals.AgeSignalsException
import com.google.android.play.agesignals.AgeSignalsManagerFactory
import com.google.android.play.agesignals.AgeSignalsRequest
import com.google.android.play.agesignals.model.AgeSignalsErrorCode
import com.google.android.play.agesignals.model.AgeSignalsStatus
import dev.ryui.RyUIPlugin
import dev.ryui.RyUIRuntime
import dev.ryui.registerModule
import java.nio.ByteBuffer
import java.nio.ByteOrder

/**
 * ryui/agerange's Play Age Signals: "request" asks the user to share
 * (Play's consent screen), then reads the age range. The reply: u8 status
 * (declined, shared, verification required), u8 source (unknown), u32
 * lower, u32 upper (0: open); "not available" where Play can't answer.
 */
class RyUIAgeRangePlugin : RyUIPlugin {
    override fun register(rt: RyUIRuntime) {
        fun answer(status: Int, lower: Int, upper: Int): ByteArray =
            ByteBuffer.allocate(10).order(ByteOrder.LITTLE_ENDIAN).put(status.toByte()).put(0).putInt(lower).putInt(upper).array()
        fun failed(e: Exception?): Exception {
            val code = (e as? AgeSignalsException)?.errorCode
            return if (code == AgeSignalsErrorCode.API_NOT_AVAILABLE || code == AgeSignalsErrorCode.PLAY_STORE_NOT_FOUND ||
                code == AgeSignalsErrorCode.PLAY_SERVICES_NOT_FOUND || code == AgeSignalsErrorCode.APP_NOT_OWNED) Exception("not available")
            else e ?: Exception("no age signals")
        }
        rt.registerModule("ageRange", mapOf(
            "request" to { _, reply ->
                val act = rt.activity
                if (act == null) reply(Result.failure(Exception("no screen")))
                else {
                    val m = AgeSignalsManagerFactory.create(act)
                    m.requestAgeSignalsAccess(AgeSignalsAccessRequest.builder().setActivity(act).build()).addOnCompleteListener { a ->
                        when {
                            !a.isSuccessful -> reply(Result.failure(failed(a.exception)))
                            a.result.ageSignalsStatus() == AgeSignalsStatus.NOT_SHARED -> reply(Result.success(answer(0, 0, 0)))
                            a.result.ageSignalsStatus() == AgeSignalsStatus.VERIFICATION_REQUIRED -> reply(Result.success(answer(2, 0, 0)))
                            else -> m.checkAgeSignals(AgeSignalsRequest.builder().build()).addOnCompleteListener { t ->
                                if (!t.isSuccessful) reply(Result.failure(failed(t.exception)))
                                else reply(Result.success(answer(1, t.result.ageLower() ?: 0, t.result.ageUpper() ?: 0)))
                            }
                        }
                    }
                }
            },
        ))
    }
}
