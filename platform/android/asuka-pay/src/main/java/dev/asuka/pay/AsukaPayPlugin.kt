package dev.asuka.pay

import android.content.Context
import android.content.res.Configuration
import android.os.Handler
import android.os.Looper
import android.view.View
import android.widget.FrameLayout
import androidx.activity.result.ActivityResultLauncher
import com.google.android.gms.common.api.CommonStatusCodes
import com.google.android.gms.tasks.Task
import com.google.android.gms.wallet.IsReadyToPayRequest
import com.google.android.gms.wallet.PaymentData
import com.google.android.gms.wallet.PaymentDataRequest
import com.google.android.gms.wallet.PaymentsClient
import com.google.android.gms.wallet.Wallet
import com.google.android.gms.wallet.WalletConstants
import com.google.android.gms.wallet.button.ButtonConstants
import com.google.android.gms.wallet.button.ButtonOptions
import com.google.android.gms.wallet.button.PayButton
import com.google.android.gms.wallet.callback.BasePaymentDataCallbacks
import com.google.android.gms.wallet.callback.BasePaymentDataCallbacksService
import com.google.android.gms.wallet.callback.IntermediatePaymentData
import com.google.android.gms.wallet.callback.OnCompleteListener
import com.google.android.gms.wallet.callback.PaymentAuthorizationResult
import com.google.android.gms.wallet.callback.PaymentDataRequestUpdate
import com.google.android.gms.wallet.contract.TaskResultContracts
import dev.asuka.AsukaNativeContext
import dev.asuka.AsukaNativeView
import dev.asuka.AsukaPlugin
import dev.asuka.AsukaRuntime
import dev.asuka.registerModule
import org.json.JSONObject

/**
 * asuka/sdk/pay on Android: Google Pay (internal/payspec has the wire).
 * "available" asks isReadyToPay, and with existingPaymentMethodRequired
 * whether the user has a card; "present" runs loadPaymentData through
 * the Activity Result API, the PaymentDataRequest as the Go code made
 * it. Google Pay's sheet calls AsukaPayCallbacks as the user changes
 * the address, the option or the card, and pays: each call is an event
 * for Go ("next"), completed with Go's answer ("answer"), Google's own
 * JSON both ways. "done" when the sheet's result comes.
 */
class AsukaPayPlugin : AsukaPlugin {
    override fun register(rt: AsukaRuntime) {
        rt.registerModule("pay", mapOf(
            "available" to { args, reply ->
                val p = runCatching { JSONObject(String(args)) }.getOrDefault(JSONObject())
                val ready = p.optJSONObject("googleReady") ?: JSONObject()
                val client = client(rt, p.optString("googleEnv"))
                ready.remove("existingPaymentMethodRequired")
                client.isReadyToPay(IsReadyToPayRequest.fromJson(ready.toString())).addOnCompleteListener { t ->
                    val can = t.isSuccessful && t.result == true
                    if (!can) {
                        reply(Result.success("""{"wallet":"google","canPay":false,"hasCard":false}""".toByteArray()))
                        return@addOnCompleteListener
                    }
                    ready.put("existingPaymentMethodRequired", true)
                    client.isReadyToPay(IsReadyToPayRequest.fromJson(ready.toString())).addOnCompleteListener { e ->
                        val card = e.isSuccessful && e.result == true
                        reply(Result.success("""{"wallet":"google","canPay":true,"hasCard":$card}""".toByteArray()))
                    }
                }
            },
            "setUp" to { _, reply -> reply(Result.failure(Exception("not available: Google Pay's sheet adds a card"))) },
            "present" to { args, reply -> AsukaPaySession.present(rt, args, reply) },
            "next" to { args, reply -> AsukaPaySession.next(String(args), reply) },
            "answer" to { args, reply ->
                AsukaPaySession.answer(JSONObject(String(args)))
                reply(Result.success(ByteArray(0)))
            },
        ))
        rt.registerNative("asuka.payButton") { ctx -> AsukaPayButton(ctx.context, ctx) }
    }
}

/** A PaymentsClient in the environment ("TEST" or "PRODUCTION"), its sheet themed as the app is. */
internal fun client(rt: AsukaRuntime, env: String): PaymentsClient {
    val context: Context = rt.app
    val night = context.resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK == Configuration.UI_MODE_NIGHT_YES
    val o = Wallet.WalletOptions.Builder()
        .setEnvironment(if (env == "PRODUCTION") WalletConstants.ENVIRONMENT_PRODUCTION else WalletConstants.ENVIRONMENT_TEST)
        .setTheme(if (night) WalletConstants.THEME_DARK else WalletConstants.THEME_LIGHT)
        .build()
    val act = rt.activity
    return if (act != null) Wallet.getPaymentsClient(act, o) else Wallet.getPaymentsClient(context, o)
}

/** The sheet that shows: its events for Go, and the callbacks waiting for Go's answers. Main thread. */
internal object AsukaPaySession {
    private val main = Handler(Looper.getMainLooper())
    private var id = ""
    private var showing = false
    private var seq = 0
    private val queue = ArrayDeque<ByteArray>()
    private var waiting: ((Result<ByteArray>) -> Unit)? = null
    private val pending = HashMap<Int, (String) -> Unit>()
    private var launcher: ActivityResultLauncher<Task<PaymentData>>? = null
    /** The sheet's authorization callback came and Go accepted it. */
    private var authorized = false
    private var launches = 0

    fun present(rt: AsukaRuntime, args: ByteArray, reply: (Result<ByteArray>) -> Unit) {
        if (showing) return reply(Result.failure(Exception("busy")))
        val p = runCatching { JSONObject(String(args)) }.getOrElse { return reply(Result.failure(it)) }
        p.optString("googleError").takeIf { it.isNotEmpty() }?.let { return reply(Result.failure(Exception(it))) }
        val act = rt.activity ?: return reply(Result.failure(Exception("no screen to show Google Pay's sheet on")))
        val request = runCatching { PaymentDataRequest.fromJson(p.getJSONObject("google").toString()) }
            .getOrElse { return reply(Result.failure(Exception("pay: Google Pay's request: ${it.message}"))) }
        id = p.optString("id")
        seq = 0
        queue.clear()
        pending.clear()
        authorized = false
        showing = true
        var l: ActivityResultLauncher<Task<PaymentData>>? = null
        l = act.activityResultRegistry.register("asuka-pay-${launches++}", TaskResultContracts.GetPaymentDataResult()) { r ->
            l?.unregister()
            if (launcher === l) launcher = null
            // Authorized in the sheet (AsukaPayCallbacks), or canceled, or failed.
            val code = r.status.statusCode
            val data = r.result
            if (code == CommonStatusCodes.SUCCESS && !authorized && data != null) {
                // Paid without the callback (Google Pay didn't call it): Go
                // authorizes it now, then it's done.
                seq++
                pending[seq] = { finish("") }
                send(JSONObject().put("type", "googleAuthorize").put("seq", seq).put("google", JSONObject(data.toJson())).toString().toByteArray())
                return@register
            }
            val error = when (code) {
                CommonStatusCodes.SUCCESS, CommonStatusCodes.CANCELED -> ""
                else -> r.status.statusMessage ?: CommonStatusCodes.getStatusCodeString(code)
            }
            finish(error)
        }
        launcher = l
        try {
            l.launch(client(rt, p.optString("googleEnv")).loadPaymentData(request))
        } catch (e: Exception) {
            l.unregister()
            showing = false
            return reply(Result.failure(e))
        }
        reply(Result.success(ByteArray(0)))
    }

    private fun finish(error: String) {
        if (!showing) return
        showing = false
        // Callbacks Go didn't answer: the sheet's gone.
        pending.clear()
        val e = JSONObject().put("type", "done")
        if (error.isNotEmpty()) e.put("error", error)
        send(e.toString().toByteArray())
    }

    fun next(sheet: String, reply: (Result<ByteArray>) -> Unit) {
        if (sheet != id || (!showing && queue.isEmpty())) return reply(Result.failure(Exception("stopped")))
        queue.removeFirstOrNull()?.let { return reply(Result.success(it)) }
        waiting?.invoke(Result.failure(Exception("stopped")))
        waiting = reply
    }

    private fun send(b: ByteArray) {
        val w = waiting
        if (w != null) {
            waiting = null
            w(Result.success(b))
        } else queue.addLast(b)
    }

    fun answer(a: JSONObject) {
        if (a.optString("id") != id) return
        val g = a.optJSONObject("google")
        if (g?.optString("transactionState") == "SUCCESS") authorized = true
        pending.remove(a.optInt("seq"))?.invoke(g?.toString() ?: "{}")
    }

    /**
     * event gives Go a callback of the sheet's (type: "google" or
     * "googleAuthorize", data: Google's JSON); done gets Go's answer
     * (Google's JSON), or fallback's if there's no sheet. Any thread.
     */
    fun event(type: String, data: String, fallback: String, done: (String) -> Unit) {
        main.post {
            if (!showing) return@post done(fallback)
            seq++
            pending[seq] = done
            send(JSONObject().put("type", type).put("seq", seq).put("google", JSONObject(data)).toString().toByteArray())
        }
    }
}

/**
 * The service Google Pay's sheet calls (this module's manifest): the
 * dynamic price updates and the authorization, answered by the app's Go
 * code through AsukaPaySession. A call while no sheet of the app's shows
 * (the process started again) is answered with an error.
 */
class AsukaPayCallbacks : BasePaymentDataCallbacksService() {
    override fun createPaymentDataCallbacks(): BasePaymentDataCallbacks = object : BasePaymentDataCallbacks() {
        override fun onPaymentDataChanged(data: IntermediatePaymentData?, listener: OnCompleteListener<PaymentDataRequestUpdate>) {
            AsukaPaySession.event("google", data?.toJson() ?: "{}", "{}") { listener.complete(PaymentDataRequestUpdate.fromJson(it)) }
        }

        override fun onPaymentAuthorized(data: PaymentData?, listener: OnCompleteListener<PaymentAuthorizationResult>) {
            val gone = """{"transactionState":"ERROR","error":{"reason":"OTHER_ERROR","message":"Try again","intent":"PAYMENT_AUTHORIZATION"}}"""
            AsukaPaySession.event("googleAuthorize", data?.toJson() ?: "{}", gone) { listener.complete(PaymentAuthorizationResult.fromJson(it)) }
        }
    }
}

/**
 * pay.Button: Google's PayButton, made again when its props change (its
 * options are fixed once initialized) and, for the automatic style, when
 * the app goes dark or light. Props: googleType (ButtonConstants'),
 * style (automatic, black, white, white outline: Google's dark, or its
 * light, which is outlined), cornerRadius (dp; -1: Google's), methods
 * (allowedPaymentMethods, for the card it shows); event: press.
 */
class AsukaPayButton(context: Context, private val ctx: AsukaNativeContext) : AsukaNativeView {
    private val box: FrameLayout = object : FrameLayout(context) {
        override fun onMeasure(widthSpec: Int, heightSpec: Int) {
            val d = resources.displayMetrics.density
            val w = if (MeasureSpec.getMode(widthSpec) == MeasureSpec.UNSPECIFIED) (300 * d).toInt() else MeasureSpec.getSize(widthSpec)
            val h = if (MeasureSpec.getMode(heightSpec) == MeasureSpec.EXACTLY) MeasureSpec.getSize(heightSpec) else (48 * d).toInt()
            super.onMeasure(MeasureSpec.makeMeasureSpec(w, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(h, MeasureSpec.EXACTLY))
        }

        override fun onConfigurationChanged(c: Configuration?) {
            super.onConfigurationChanged(c)
            if (style == 0 && dark() != madeDark) post { make() }
        }
    }
    override val view: View get() = box
    private var type = ButtonConstants.ButtonType.BUY
    private var style = 0
    private var radius = -1
    private var methods = "[]"
    private var madeDark = false
    private var dirty = false

    init {
        make()
    }

    private fun dark(): Boolean = box.resources.configuration.uiMode and Configuration.UI_MODE_NIGHT_MASK == Configuration.UI_MODE_NIGHT_YES

    override fun set(name: String, value: Any) {
        when (name) {
            "googleType" -> type = (value as Long).toInt()
            "style" -> style = (value as Long).toInt()
            "cornerRadius" -> radius = (value as Double).toInt()
            "methods" -> methods = value as String
            else -> return
        }
        // The props come together: made once they have.
        if (!dirty) {
            dirty = true
            box.post { dirty = false; make() }
        }
    }

    private fun make() {
        madeDark = dark()
        val theme = when (style) {
            1 -> ButtonConstants.ButtonTheme.DARK
            2, 3 -> ButtonConstants.ButtonTheme.LIGHT
            else -> if (madeDark) ButtonConstants.ButtonTheme.LIGHT else ButtonConstants.ButtonTheme.DARK
        }
        val o = ButtonOptions.newBuilder().setButtonType(type).setButtonTheme(theme).setAllowedPaymentMethods(methods)
        if (radius >= 0) o.setCornerRadius(radius)
        val b = PayButton(box.context)
        b.initialize(o.build())
        b.setOnClickListener { ctx.emit("press") }
        box.removeAllViews()
        box.addView(b, FrameLayout.LayoutParams(FrameLayout.LayoutParams.MATCH_PARENT, FrameLayout.LayoutParams.MATCH_PARENT))
    }
}
