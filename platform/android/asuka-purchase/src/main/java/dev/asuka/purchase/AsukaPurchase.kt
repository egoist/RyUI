package dev.asuka.purchase

import android.app.Activity
import android.app.Application
import android.content.Intent
import android.net.Uri
import android.os.Bundle
import com.android.billingclient.api.AcknowledgePurchaseParams
import com.android.billingclient.api.BillingClient
import com.android.billingclient.api.BillingClientStateListener
import com.android.billingclient.api.BillingFlowParams
import com.android.billingclient.api.BillingResult
import com.android.billingclient.api.ConsumeParams
import com.android.billingclient.api.GetBillingConfigParams
import com.android.billingclient.api.PendingPurchasesParams
import com.android.billingclient.api.ProductDetails
import com.android.billingclient.api.Purchase
import com.android.billingclient.api.PurchasesUpdatedListener
import com.android.billingclient.api.QueryProductDetailsParams
import com.android.billingclient.api.QueryPurchasesParams
import dev.asuka.AsukaModuleMethod
import dev.asuka.AsukaPlugin
import dev.asuka.AsukaRuntime
import dev.asuka.registerModule
import com.android.billingclient.api.BillingFlowParams.ProductDetailsParams.SubscriptionProductReplacementParams.ReplacementMode as Mode
import org.json.JSONArray
import org.json.JSONObject

/** asuka/sdk/purchase's module: Google Play Billing, registered at start, so the store's purchases are followed from then. */
class AsukaPurchasePlugin : AsukaPlugin {
    override fun register(rt: AsukaRuntime) {
        val s = AsukaStore(rt)
        rt.registerModule("purchase", s.methods())
        s.start()
    }
}

typealias Reply = (Result<ByteArray>) -> Unit

/**
 * internal/purchasespec's methods and JSON over the Play Billing Library.
 * Purchases that come outside a "buy" (a pending one paid for, one made
 * in the Play Store, one left unacknowledged from before) are kept for
 * the "next" that asks: those PurchasesUpdatedListener gives, and those
 * queryPurchasesAsync finds unacknowledged as the client connects and as
 * the app comes back to the front. Everything runs on the main thread.
 */
class AsukaStore(private val rt: AsukaRuntime) : PurchasesUpdatedListener {
    private val client: BillingClient = BillingClient.newBuilder(rt.app)
        .setListener(this)
        .enablePendingPurchases(PendingPurchasesParams.newBuilder().enableOneTimeProducts().enablePrepaidPlans().build())
        .enableAutoServiceReconnection()
        .build()
    private var connecting = false
    private val whenConnected = mutableListOf<(BillingResult) -> Unit>()
    /** Products asked for, by ID: launchBillingFlow needs Play's. */
    private val details = HashMap<String, ProductDetails>()
    /** Each product's kind, as the app said (purchasespec's), or Play's type. */
    private val kinds = HashMap<String, String>()
    /** The purchase under way: its product and "buy"'s reply. */
    private var buying: Pair<String, Reply>? = null
    /** What each purchase token was last given as (its state), not to give it twice. */
    private val given = HashMap<String, Int>()
    private val events = ArrayDeque<ByteArray>()
    private var waiting: Reply? = null
    /** Replies waiting for the app to come back (a store page it opened). */
    private val onReturn = mutableListOf<() -> Unit>()

    private class Failure(m: String) : Exception(m)

    fun start() {
        rt.app.registerActivityLifecycleCallbacks(object : Application.ActivityLifecycleCallbacks {
            override fun onActivityResumed(a: Activity) {
                val fs = onReturn.toList()
                onReturn.clear()
                fs.forEach { it() }
                reconcile()
            }
            override fun onActivityCreated(a: Activity, b: Bundle?) {}
            override fun onActivityStarted(a: Activity) {}
            override fun onActivityPaused(a: Activity) {}
            override fun onActivityStopped(a: Activity) {}
            override fun onActivitySaveInstanceState(a: Activity, b: Bundle) {}
            override fun onActivityDestroyed(a: Activity) {}
        })
        reconcile()
    }

    /** Runs f once the client is connected (or failed to: f gets why). */
    private fun connected(f: (BillingResult) -> Unit) {
        if (client.isReady) {
            f(BillingResult.newBuilder().setResponseCode(BillingClient.BillingResponseCode.OK).build())
            return
        }
        whenConnected.add(f)
        if (connecting) return
        connecting = true
        client.startConnection(object : BillingClientStateListener {
            override fun onBillingSetupFinished(r: BillingResult) {
                rt.onMain {
                    connecting = false
                    val fs = whenConnected.toList()
                    whenConnected.clear()
                    fs.forEach { it(r) }
                }
            }
            override fun onBillingServiceDisconnected() {
                rt.onMain { connecting = false } // the client reconnects by itself (enableAutoServiceReconnection)
            }
        })
    }

    /** f once connected; reply's failure if the client can't connect. */
    private fun ready(reply: Reply, f: () -> Unit) = connected { r ->
        if (r.responseCode == BillingClient.BillingResponseCode.OK) f() else reply(Result.failure(failure(r)))
    }

    private fun failure(r: BillingResult): Exception = Failure(
        when (r.responseCode) {
            BillingClient.BillingResponseCode.BILLING_UNAVAILABLE -> "not available: Google Play Billing (${r.debugMessage})"
            BillingClient.BillingResponseCode.FEATURE_NOT_SUPPORTED -> "not available: ${r.debugMessage}"
            BillingClient.BillingResponseCode.ITEM_UNAVAILABLE -> "unknown product: ${r.debugMessage}"
            BillingClient.BillingResponseCode.ITEM_ALREADY_OWNED -> "already owned"
            BillingClient.BillingResponseCode.NETWORK_ERROR, BillingClient.BillingResponseCode.SERVICE_UNAVAILABLE ->
                "the network: ${r.debugMessage}"
            else -> "Play Billing (${r.responseCode}): ${r.debugMessage}"
        }
    )

    fun methods(): Map<String, AsukaModuleMethod> = mapOf(
        "available" to { _, reply ->
            connected { r -> reply(Result.success(byteArrayOf(if (r.responseCode == BillingClient.BillingResponseCode.OK) 1 else 0))) }
        },
        "products" to { args, reply -> ready(reply) { products(JSONObject(String(args)), reply) } },
        "buy" to { args, reply -> ready(reply) { buy(JSONObject(String(args)), reply) } },
        "finish" to { args, reply -> ready(reply) { finish(JSONObject(String(args)), reply) } },
        "entitlements" to { _, reply -> ready(reply) { entitlements(reply) } },
        "restore" to { _, reply -> ready(reply) { reconcile(); entitlements(reply) } },
        "status" to { args, reply -> ready(reply) { status(JSONObject(String(args)).optString("group"), reply) } },
        "manage" to { args, reply ->
            val id = JSONObject(String(args)).optString("productId")
            var u = "https://play.google.com/store/account/subscriptions"
            if (id.isNotEmpty()) u += "?sku=" + Uri.encode(id) + "&package=" + Uri.encode(rt.app.packageName)
            open(u, reply)
        },
        "refund" to { _, reply -> reply(Result.failure(Failure("not available: Play's refunds are the developer's (Play Console, the Developer API)"))) },
        "redeem" to { _, reply -> open("https://play.google.com/redeem", reply) },
        "storefront" to { _, reply ->
            ready(reply) {
                client.getBillingConfigAsync(GetBillingConfigParams.newBuilder().build()) { r, c ->
                    rt.onMain {
                        if (r.responseCode != BillingClient.BillingResponseCode.OK || c == null) reply(Result.failure(failure(r)))
                        else reply(Result.success(JSONObject().put("country", c.countryCode).toString().toByteArray()))
                    }
                }
            }
        },
        "next" to { _, reply ->
            val e = events.removeFirstOrNull()
            if (e != null) reply(Result.success(e))
            else {
                waiting?.invoke(Result.failure(Failure("stopped")))
                waiting = reply
            }
        },
        "stop" to { _, reply ->
            waiting?.invoke(Result.failure(Failure("stopped")))
            waiting = null
            reply(Result.success(ByteArray(0)))
        },
    )

    /** Opens a Play page, answering as the app comes back to the front. */
    private fun open(url: String, reply: Reply) {
        val act = rt.activity ?: return reply(Result.failure(Failure("no screen")))
        try {
            act.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(url)))
            onReturn.add { reply(Result.success(ByteArray(0))) }
        } catch (e: Exception) {
            reply(Result.failure(e))
        }
    }

    private fun playType(kind: String) = when (kind) {
        "autoRenewable", "nonRenewing", "subs" -> BillingClient.ProductType.SUBS
        else -> BillingClient.ProductType.INAPP
    }

    /** Asks Play for products, one query per type. */
    private fun query(items: List<Pair<String, String>>, done: (Result<List<String>>) -> Unit) {
        val byType = items.groupBy { playType(it.second) }
        var left = byType.size
        if (left == 0) return done(Result.success(emptyList()))
        val missing = mutableListOf<String>()
        var failed: Exception? = null
        for ((type, its) in byType) {
            val ps = its.map { QueryProductDetailsParams.Product.newBuilder().setProductId(it.first).setProductType(type).build() }
            client.queryProductDetailsAsync(QueryProductDetailsParams.newBuilder().setProductList(ps).build()) { r, res ->
                rt.onMain {
                    if (r.responseCode != BillingClient.BillingResponseCode.OK) failed = failure(r)
                    else {
                        res.productDetailsList.forEach { details[it.productId] = it }
                        res.unfetchedProductList.forEach { missing.add(it.productId) }
                    }
                    if (--left == 0) {
                        val f = failed
                        done(if (f != null) Result.failure(f) else Result.success(missing))
                    }
                }
            }
        }
    }

    private fun products(a: JSONObject, reply: Reply) {
        val items = mutableListOf<Pair<String, String>>()
        val arr = a.optJSONArray("items") ?: JSONArray()
        for (i in 0 until arr.length()) {
            val o = arr.getJSONObject(i)
            val id = o.getString("id")
            val kind = o.optString("kind")
            if (kind.isNotEmpty()) kinds[id] = kind
            items.add(id to kind)
        }
        query(items) { res ->
            res.fold({ missing ->
                val out = JSONArray()
                for ((id, _) in items) details[id]?.let { out.put(wire(it)) }
                reply(Result.success(JSONObject().put("products", out).put("missing", JSONArray(missing)).toString().toByteArray()))
            }, { reply(Result.failure(it)) })
        }
    }

    private fun buy(a: JSONObject, reply: Reply) {
        val id = a.getString("id")
        val kind = a.optString("kind")
        if (kind.isNotEmpty()) kinds[id] = kind
        val p = details[id]
        if (p == null) {
            query(listOf(id to kind)) { res ->
                if (res.isFailure) reply(Result.failure(res.exceptionOrNull()!!))
                else if (details[id] == null) reply(Result.failure(Failure("unknown product: $id")))
                else buy(a, reply)
            }
            return
        }
        val act = rt.activity ?: return reply(Result.failure(Failure("no screen")))
        if (buying != null) return reply(Result.failure(Failure("a purchase is under way")))
        val pp = BillingFlowParams.ProductDetailsParams.newBuilder().setProductDetails(p)
        val token = a.optString("offerToken")
        if (token.isNotEmpty()) pp.setOfferToken(token)
        else p.subscriptionOfferDetails?.let { offers ->
            // A subscription needs an offer: the first base plan's.
            val base = offers.firstOrNull { it.offerId == null } ?: offers.firstOrNull()
            base?.let { pp.setOfferToken(it.offerToken) }
        }
        val flow = BillingFlowParams.newBuilder()
        a.optJSONObject("replace")?.let { r ->
            pp.setSubscriptionProductReplacementParams(
                BillingFlowParams.ProductDetailsParams.SubscriptionProductReplacementParams.newBuilder()
                    .setOldProductId(r.optString("productId"))
                    .setReplacementMode(replacementMode(r.optString("mode")))
                    .build()
            )
            flow.setSubscriptionUpdateParams(BillingFlowParams.SubscriptionUpdateParams.newBuilder().setOldPurchaseToken(r.getString("token")).build())
        }
        flow.setProductDetailsParamsList(listOf(pp.build()))
        a.optString("accountToken").takeIf { it.isNotEmpty() }?.let { flow.setObfuscatedAccountId(it) }
        a.optString("profileToken").takeIf { it.isNotEmpty() }?.let { flow.setObfuscatedProfileId(it) }
        if (a.optBoolean("personalized")) flow.setIsOfferPersonalized(true)
        buying = id to reply
        val r = client.launchBillingFlow(act, flow.build())
        if (r.responseCode != BillingClient.BillingResponseCode.OK) {
            buying = null
            reply(Result.failure(failure(r)))
        }
    }

    private fun replacementMode(m: String): Int {
        return when (m) {
            "chargeProratedPrice" -> Mode.CHARGE_PRORATED_PRICE
            "chargeFullPrice" -> Mode.CHARGE_FULL_PRICE
            "withoutProration" -> Mode.WITHOUT_PRORATION
            "deferred" -> Mode.DEFERRED
            else -> Mode.WITH_TIME_PRORATION
        }
    }

    override fun onPurchasesUpdated(r: BillingResult, purchases: MutableList<Purchase>?) {
        rt.onMain { updated(r, purchases ?: emptyList()) }
    }

    private fun updated(r: BillingResult, purchases: List<Purchase>) {
        val b = buying
        var mine: Purchase? = null
        if (b != null) {
            buying = null
            val (id, reply) = b
            when (r.responseCode) {
                BillingClient.BillingResponseCode.OK -> {
                    mine = purchases.firstOrNull { id in it.products } ?: purchases.firstOrNull()
                    val p = mine
                    if (p == null) reply(Result.success(JSONObject().put("status", "cancelled").toString().toByteArray()))
                    else {
                        given[p.purchaseToken] = p.purchaseState
                        val status = if (p.purchaseState == Purchase.PurchaseState.PURCHASED) "purchased" else "pending"
                        reply(Result.success(JSONObject().put("status", status).put("transaction", wire(p)).toString().toByteArray()))
                    }
                }
                BillingClient.BillingResponseCode.USER_CANCELED ->
                    reply(Result.success(JSONObject().put("status", "cancelled").toString().toByteArray()))
                else -> reply(Result.failure(failure(r)))
            }
        }
        if (r.responseCode == BillingClient.BillingResponseCode.OK) {
            for (p in purchases) if (p !== mine) give(p)
        }
    }

    /** Keeps a purchase for "next", unless it was given as it is. */
    private fun give(p: Purchase) {
        if (given[p.purchaseToken] == p.purchaseState) return
        given[p.purchaseToken] = p.purchaseState
        val e = JSONObject().put("type", "transaction").put("transaction", wire(p)).toString().toByteArray()
        val w = waiting
        if (w != null) {
            waiting = null
            w(Result.success(e))
        } else events.addLast(e)
    }

    /** Both types' purchases. */
    private fun purchases(suspended: Boolean, done: (Result<List<Purchase>>) -> Unit) {
        val out = mutableListOf<Purchase>()
        var left = 2
        var failed: Exception? = null
        for (type in listOf(BillingClient.ProductType.INAPP, BillingClient.ProductType.SUBS)) {
            val params = QueryPurchasesParams.newBuilder().setProductType(type)
            if (suspended && type == BillingClient.ProductType.SUBS) params.includeSuspendedSubscriptions(true)
            client.queryPurchasesAsync(params.build()) { r, ps ->
                rt.onMain {
                    if (r.responseCode != BillingClient.BillingResponseCode.OK) failed = failure(r)
                    else {
                        for (p in ps) p.products.forEach { id -> kinds.putIfAbsent(id, if (type == BillingClient.ProductType.SUBS) "subs" else "inapp") }
                        out.addAll(ps)
                    }
                    if (--left == 0) {
                        val f = failed
                        done(if (f != null) Result.failure(f) else Result.success(out))
                    }
                }
            }
        }
    }

    /** Gives the purchases paid for but not acknowledged: from before, outside the app, a pending one paid. */
    private fun reconcile() = connected { r ->
        if (r.responseCode != BillingClient.BillingResponseCode.OK) return@connected
        purchases(false) { res ->
            res.getOrNull()?.forEach { if (it.purchaseState == Purchase.PurchaseState.PURCHASED && !it.isAcknowledged) give(it) }
        }
    }

    private fun entitlements(reply: Reply) = purchases(false) { res ->
        res.fold({ ps ->
            val out = JSONArray()
            ps.filter { it.purchaseState == Purchase.PurchaseState.PURCHASED }.forEach { out.put(wire(it)) }
            reply(Result.success(out.toString().toByteArray()))
        }, { reply(Result.failure(it)) })
    }

    private fun status(group: String, reply: Reply) = purchases(true) { res ->
        res.fold({ ps ->
            val out = JSONArray()
            for (p in ps) {
                if (group !in p.products || p.purchaseState != Purchase.PurchaseState.PURCHASED) continue
                if (playType(kinds[group] ?: "subs") != BillingClient.ProductType.SUBS) continue
                out.put(JSONObject().put("productId", group).put("state", if (p.isSuspended) "billingRetry" else "subscribed")
                    .put("willRenew", p.isAutoRenewing).put("transaction", wire(p)))
            }
            reply(Result.success(out.toString().toByteArray()))
        }, { reply(Result.failure(it)) })
    }

    private fun finish(a: JSONObject, reply: Reply) {
        val token = a.optString("token")
        if (token.isEmpty()) return reply(Result.failure(Failure("no purchase token")))
        val done = { r: BillingResult ->
            rt.onMain {
                if (r.responseCode == BillingClient.BillingResponseCode.OK || r.responseCode == BillingClient.BillingResponseCode.ITEM_NOT_OWNED && !a.optBoolean("consume")) {
                    reply(Result.success(ByteArray(0)))
                } else reply(Result.failure(failure(r)))
            }
        }
        if (a.optBoolean("consume")) client.consumeAsync(ConsumeParams.newBuilder().setPurchaseToken(token).build()) { r, _ -> done(r) }
        else client.acknowledgePurchase(AcknowledgePurchaseParams.newBuilder().setPurchaseToken(token).build()) { r -> done(r) }
    }

    // The wire.

    private fun period(iso: String): JSONObject {
        // ISO 8601: P1W, P1M, P3M, P1Y (P7D: a week's days).
        val m = Regex("P(\\d+)([DWMY])").find(iso)
        val n = m?.groupValues?.get(1)?.toInt() ?: 0
        val unit = when (m?.groupValues?.get(2)) { "D" -> "day"; "W" -> "week"; "M" -> "month"; else -> "year" }
        return JSONObject().put("unit", unit).put("count", n)
    }

    private fun phases(ps: List<ProductDetails.PricingPhase>): JSONArray {
        val out = JSONArray()
        for (ph in ps) {
            val mode = when (ph.recurrenceMode) {
                ProductDetails.RecurrenceMode.INFINITE_RECURRING -> "recurring"
                ProductDetails.RecurrenceMode.FINITE_RECURRING -> if (ph.priceAmountMicros == 0L) "freeTrial" else "payAsYouGo"
                else -> if (ph.priceAmountMicros == 0L) "freeTrial" else "payUpFront"
            }
            out.put(JSONObject().put("price", ph.formattedPrice).put("micros", ph.priceAmountMicros).put("currency", ph.priceCurrencyCode)
                .put("period", period(ph.billingPeriod)).put("cycles", maxOf(ph.billingCycleCount, 1)).put("mode", mode))
        }
        return out
    }

    private fun wire(p: ProductDetails): JSONObject {
        val o = JSONObject().put("id", p.productId).put("kind", kinds[p.productId] ?: p.productType)
            .put("title", p.name).put("description", p.description)
        val offers = JSONArray()
        val subs = p.subscriptionOfferDetails
        if (subs != null) {
            o.put("group", p.productId)
            val base = subs.firstOrNull { it.offerId == null } ?: subs.firstOrNull()
            base?.pricingPhases?.pricingPhaseList?.lastOrNull()?.let {
                o.put("price", it.formattedPrice).put("micros", it.priceAmountMicros).put("currency", it.priceCurrencyCode).put("period", period(it.billingPeriod))
            }
            var intro = false
            for (s in subs) {
                val ph = s.pricingPhases.pricingPhaseList
                if (s.offerId != null && ph.size > 1) intro = true
                val j = JSONObject().put("type", if (s.offerId == null) "base" else "offer").put("token", s.offerToken)
                    .put("basePlan", s.basePlanId).put("tags", JSONArray(s.offerTags)).put("phases", phases(ph))
                s.offerId?.let { j.put("id", it) }
                offers.put(j)
            }
            o.put("introEligible", intro)
        } else {
            p.oneTimePurchaseOfferDetails?.let {
                o.put("price", it.formattedPrice).put("micros", it.priceAmountMicros).put("currency", it.priceCurrencyCode)
            }
            p.oneTimePurchaseOfferDetailsList?.forEach { d ->
                val j = JSONObject().put("type", "oneTime").put("token", d.offerToken).put("tags", JSONArray(d.offerTags))
                    .put("phases", JSONArray().put(JSONObject().put("price", d.formattedPrice).put("micros", d.priceAmountMicros)
                        .put("currency", d.priceCurrencyCode).put("cycles", 1).put("mode", "payUpFront")
                        .put("period", JSONObject().put("unit", "day").put("count", 0))))
                d.offerId?.let { j.put("id", it) }
                d.purchaseOptionId?.let { j.put("basePlan", it) }
                offers.put(j)
            }
        }
        if (offers.length() > 0) o.put("offers", offers)
        return o
    }

    private fun wire(p: Purchase): JSONObject {
        val id = p.products.firstOrNull() ?: ""
        val o = JSONObject().put("id", p.orderId ?: p.purchaseToken).put("productId", id).put("kind", kinds[id] ?: "inapp")
            .put("quantity", p.quantity)
            .put("state", if (p.purchaseState == Purchase.PurchaseState.PURCHASED) "purchased" else "pending")
            .put("dateMs", p.purchaseTime).put("autoRenews", p.isAutoRenewing).put("acknowledged", p.isAcknowledged)
            .put("token", p.purchaseToken).put("packageName", p.packageName).put("originalJson", p.originalJson).put("signature", p.signature)
        p.accountIdentifiers?.let { a ->
            a.obfuscatedAccountId?.let { o.put("accountToken", it) }
            a.obfuscatedProfileId?.let { o.put("profileToken", it) }
        }
        return o
    }
}
