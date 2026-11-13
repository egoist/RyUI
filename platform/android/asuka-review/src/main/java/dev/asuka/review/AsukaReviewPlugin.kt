package dev.asuka.review

import com.google.android.play.core.review.ReviewManagerFactory
import dev.asuka.AsukaPlugin
import dev.asuka.AsukaRuntime
import dev.asuka.registerModule

/** asuka/review's module: "request" asks Play for its in-app review, which it shows or not (its quota). */
class AsukaReviewPlugin : AsukaPlugin {
    override fun register(rt: AsukaRuntime) {
        rt.registerModule("review", mapOf(
            "request" to { _, reply ->
                val act = rt.activity
                if (act == null) reply(Result.failure(Exception("no screen")))
                else {
                    val m = ReviewManagerFactory.create(act)
                    m.requestReviewFlow().addOnCompleteListener { t ->
                        if (!t.isSuccessful) reply(Result.failure(t.exception ?: Exception("no review")))
                        else m.launchReviewFlow(act, t.result).addOnCompleteListener { reply(Result.success(ByteArray(0))) }
                    }
                }
            },
        ))
    }
}
