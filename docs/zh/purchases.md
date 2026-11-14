# 应用内购买

`asuka/sdk/purchase` 通过各平台自己的应用商店，在应用里售卖商品：消耗型商品（金币、提示）、非消耗型商品（一个关卡、永久的“Pro”）、自动续期的订阅，以及不自动续期的订阅。它覆盖了 react-native-iap、expo-iap 和 RevenueCat 的 SDK 在设备端提供的能力，用的都是各商店当前的 API：

| 平台 | 应用商店 | API |
| --- | --- | --- |
| iOS、macOS | App Store | StoreKit 2 |
| Android | Google Play | Play Billing Library 9 |
| Windows | Microsoft Store | `Windows.Services.Store`，仅限从商店安装的应用 |
| Web | Google Play | Trusted Web Activity 中的 Digital Goods API 和 Payment Request |
| Linux | 无 | |

在没有应用商店的地方（Linux、不支持 Digital Goods API 的浏览器、不是从商店安装的 Windows 应用、`asuka dev` 里的播放器应用），`purchase.Available()` 为 false，其余调用都以 `purchase.ErrNotAvailable` 失败。

## 商品

`Item` 是应用售卖的一件商品：它在商店里的 ID 和它的类型。Play 分不清消耗型和非消耗型商品，所以要由应用说明。

```go
var (
	coins = purchase.Item{ID: "coins_100", Kind: purchase.Consumable}
	pro   = purchase.Item{ID: "pro", Kind: purchase.NonConsumable}
	plus  = purchase.Item{ID: "plus_monthly", Kind: purchase.AutoRenewable}
)

products, err := purchase.Products(coins, pro, plus)
```

这些调用会等待商店的回复，所以不要在主线程上调用（放在 goroutine 里，用 `ui.Post` 写回它们改变的状态）。`Products` 返回商店在售的那些商品，使用用户所在国家和语言；商店不卖的会被略去。每个 `Product` 有 `Title`、`Description`，用于显示的 `Price`（“$4.99”“4,99 €”），以及用于计算的 `PriceMicros` 和 `Currency`。订阅的 `Subscription` 包含它的订阅群组、在群组中的级别、周期和优惠：Apple 的推介优惠、促销优惠和赢回优惠，Play 的基础方案和其上的优惠（每个都带有若干阶段：免费试用、若干周期的优惠价、常规价格）。`IntroEligible` 表示用户是否还能享受推介优惠。

这些 ID 就是 App Store Connect 和 Play 管理中心里的产品 ID；在 Windows 上，是每个附加产品在合作伙伴中心里的产品 ID（它的 `InAppOfferToken`）或它的 Store ID。

## 购买

```go
r, err := purchase.Buy(coins, purchase.Options{AccountToken: userUUID})
switch {
case err != nil:
	// purchase.ErrNotAllowed、ErrUnknownProduct、ErrAlreadyOwned……
case r.Status == purchase.Bought:
	deliver(r.Transaction)
	r.Transaction.Finish()
case r.Status == purchase.PendingApproval:
	// “询问购买”、Play 正在等待的付款：之后由 Observe 送达
case r.Status == purchase.Cancelled:
}
```

`Buy` 弹出商店的购买界面，等到用户完成购买、取消，或者购买进入等待批准状态时返回。`Options` 包括：

- `Quantity`：消耗型商品的数量（Apple 的；在 Play 上，如果在 Play 管理中心里开启了多件购买，由用户自己选择）。
- `AccountToken`：把这次购买和应用里的用户关联起来。请使用 UUID：Apple 的 `appAccountToken` 必须是 UUID，Play 则把它当作 `obfuscatedAccountId`。它会出现在交易和商店的服务器通知里。不要用邮箱或姓名。`ProfileToken` 是 Play 的 `obfuscatedProfileId`。
- `Offer`：商品的某个优惠。Play 购买订阅时必须指定一个；不指定时，`Buy` 使用第一个基础方案。Apple 的促销优惠还需要 `Signature`，由应用的服务器用它的 App 内购买项目密钥签名（密钥 ID、一个 nonce、签名和签名时间；iOS 18.4 起也可以用一个 JWS）。
- `Replace`：这次购买要替换的 Play 订阅（升级或降级），以及替换方式（`WithTimeProration`、`ChargeProratedPrice`、`ChargeFullPrice`、`WithoutProration`、`Deferred`）。Apple 会自己处理同一订阅群组内的切换。
- `Personalized`：Play 的个性化价格标记，欧盟要求应用向用户说明。

## 交易

`Transaction` 是一次购买：它的 `ID`、`ProductID`、`Kind`、`State`（`Purchased`、`Pending`、`Revoked`）、`Quantity`、`Date`、订阅的 `Expires`，以及被撤销时的 `Revoked` 和撤销原因。

先交付交易售出的内容，再调用 `Finish`。在完成之前，商店每次启动都会再次送来这笔交易：StoreKit 会一直把它当作未完成，Play 则会给三天内没有确认的购买退款。对 `Consumable`，`Finish` 会消耗掉它（Play 的 `consumeAsync`、Microsoft Store 的履行），用户才能再次购买；对其他类型，它对应 StoreKit 的 `finish` 和 Play 的确认。`Pending` 的交易不能完成。

`Buy` 之外发生的一切都由 `Observe` 送达：

```go
stop := purchase.Observe(func(e purchase.Event) {
	switch e.Type {
	case purchase.TransactionUpdated:
		t := e.Transaction
		switch t.State {
		case purchase.Purchased:
			deliver(t)
			go t.Finish()
		case purchase.Revoked:
			takeBack(t)
		}
	case purchase.StorefrontChanged:
		// 价格换成了另一个国家的：重新获取商品
	}
})
```

包括：被批准或已付款的待处理购买、在应用外完成的购买（在商店里兑换的优惠码、在另一台设备上的购买）、订阅续期、退款或撤销，以及上次没有完成的交易。宿主从应用启动时就开始监听（StoreKit 的 `Transaction.updates`、Play 的 `PurchasesUpdatedListener`，以及应用启动和回到前台时找到的未确认购买），并把第一个观察者出现之前收到的内容保留给它，所以不会丢失任何一笔。请在应用启动时、在根组件里调用 `Observe`。

在开始观察之前调用 `purchase.Declare(coins, pro, plus)`，这样上次没有完成的消耗型商品在你完成它时会被消耗掉：Play 不会告诉你它是哪种类型。`Products` 和 `Buy` 也会登记它们的商品。

各商店告诉设备的内容并不相同：

- Apple 什么都会说：续期是交易，退款是设置了 `Revoked` 的交易。
- Play 的客户端看不到续期（订阅一直使用同一个购买令牌）、退款和过期：这些会在发生时反映在 `CurrentEntitlements` 里，你的服务器则通过 Play 的实时开发者通知得知。
- Microsoft Store 会在用户的许可证变化时通知：新的许可证是一笔交易，消失了的耐用品许可证视为撤销。

## 已购权益与恢复购买

`CurrentEntitlements()` 是用户当前拥有的东西：非消耗型商品、处于订阅周期或宽限期内的订阅，以及已完成的 Apple 非续期订阅（它们的有效期由应用决定）。`Entitlements.Has(id)` 检查其中某一项。

在组件里，`purchase.Use()` 以 signal 的形式提供已购权益。组件挂载时获取一次，之后每次 `Buy`、`Finish`、`Restore`，以及应用的 `Observe` 收到交易时，都会重新获取：

```go
owned := purchase.Use()
ui.Show(signal.Func(func() bool { return owned.Get().Has(pro.ID) }), proFeatures)
```

`Restore()` 重新向商店查询，并返回已购权益。在 iOS 上它是 `AppStore.sync`，会要求用户登录，所以只能从“恢复购买”按钮调用。售卖非消耗型商品或订阅的应用，App 审核会要求有这样一个按钮。

## 订阅

`SubscriptionStatuses(group)` 返回一个群组里每个订阅的状态（Apple 的订阅群组 ID；在 Play 上是产品 ID）：`Subscribed`、`Expired`、`BillingRetry`（扣款失败，商店正在重试；即 Play 的账号保留状态）、`GracePeriod`（重试期间仍然享有权益）或 `SubscriptionRevoked`。它还说明是否会续期（`WillRenew`）、以什么商品续期（`RenewsTo`，升级或降级之后）、何时续期，以及过期的原因。`RenewalJWS` 是 Apple 签名的续期信息。

`ManageSubscriptions(productID)` 打开商店里用户的订阅管理页面，用户可以在那里取消或更改订阅：iOS 上是系统的面板，Play 上是订阅中心（定位到该商品），桌面端则是 App Store 或 Microsoft 的账户页面。`RedeemCode()` 打开 Apple 的优惠码兑换面板（iOS 16、macOS 15），或打开 Play 的兑换页面；兑换到的东西由 `Observe` 送达。`RequestRefund(t)` 打开 Apple 的退款申请面板。Play 没有这样的界面：退款由开发者在 Play 管理中心或自己的服务器上处理。

`CurrentStorefront()` 是商店所在的国家或地区，价格和在售商品都随它而定。

## 在服务器上验证

服务器在授予所售内容之前，应该先向商店核实这笔购买。`Transaction.Verification` 里有各商店 API 需要的内容：

- Apple：`JWS`，由 App Store 签名的交易。用 App Store Server Library（它的 `SignedDataVerifier`）验证，或者自己检查它的证书链是否一直通到 Apple 的根证书。`Verified` 表示 StoreKit 是否已经在设备上验证过它。
- Google Play：`Token`，即购买令牌，连同 `PackageName`，交给 Google Play Developer API（`purchases.productsv2`、`purchases.subscriptionsv2`）。`OriginalJSON` 和 `Signature` 是用应用的许可密钥签名的购买信息。
- Windows：`Token`，即附加产品的 Store ID，交给 Microsoft Store 的收藏 API。

有了负责授予购买的服务器，应用可以按服务器的结果交付内容，并在服务器确认之后再完成交易。

## 测试购买

商店只向它认识的应用售卖商品：`asuka dev` 里的播放器应用卖不了你的应用的商品。请用构建出来的应用测试。

**iOS 模拟器。** 在 Xcode 里创建一个 StoreKit 配置文件（File › New › File › StoreKit Configuration File，也可以从 App Store Connect 同步），填好商品，然后在 `asuka.json` 里指定它：

```json
{
  "ios": { "storekit": "Products.storekit" }
}
```

`asuka dev` 为模拟器生成的构建会带上这个文件；由 `asuka dev` 启动时，它会像在 Xcode 的 scheme 里设置了这个文件的应用那样，从文件里售卖商品。价格、“询问购买”、交易失败和加速续期都由这个文件控制：

```sh
asuka dev --platform ios --no-hot examples/store   # 构建出的应用，保存后重新构建
asuka dev --platform ios --once examples/store     # 构建并启动一次
```

如果用别的方式启动（比如从主屏幕点开），应用自己加载不了这个文件（它的会话需要 Xcode 的 XCTest，`asuka dev` 会像测试运行那样提供给它）；StoreKit 会继续使用 `asuka dev` 上一次为这个应用设置的配置。其他构建永远不会带这个文件：它们都从 App Store 的沙盒购买。在设备上用沙盒账户登录（“设置” › “开发者”，或者在购买时登录），或者安装 TestFlight 构建。

**Android。** Play 只向它拥有的构建售卖商品。把一个构建上传到内部测试轨道（`asuka publish --platform android --track internal`），在 Play 管理中心添加商品，把你的 Google 账号加入许可测试人员（Play 管理中心 › 设置 › 许可测试），然后通过测试轨道的链接安装。许可测试人员不会被真正扣款，续期也会加快。

**Mac。** 和 iOS 一样，使用沙盒账户；`asuka build` 生成的 Mac 构建会在它的 Swift 库里带上 StoreKit 2。

**Windows。** Microsoft Store 只向它安装的应用售卖：在合作伙伴中心把应用与商店关联，并从商店安装（可以用软件包外部测试版来测试）。用 `asuka dev` 运行的应用没有应用商店。

**在 `go test` 里。** `host/fake` 的 `Store` 售卖测试给它的商品，并且像 StoreKit 那样保存交易：在应用完成之前一直是未完成状态，并会被再次送达。

```go
var store *fake.Store
a := uitest.Mount(t, Store, uitest.Setup(func(h *fake.Host) {
	store = h.Store()
	store.Products = []fake.StoreProduct{
		{ID: "coins_100", Kind: "consumable", Title: "100 Coins", Price: "$0.99", PriceMicros: 990000},
		{ID: "plus_monthly", Kind: "autoRenewable", Title: "Plus", Price: "$4.99", Period: "P1M", Trial: "P1W"},
	}
}))
a.Tap("$0.99")
a.WaitFor("100 coins")

store.Outcome = fake.PurchasePending // “询问购买”
a.Tap("$0.99")
store.Approve("coins_100")           // 家长批准：Observe 收到它
store.Renew("plus_monthly")          // 一次续期
store.Refund(id)                     // 一次退款
store.Deliver("pro")                 // 在另一台设备上购买
```

`Outcome` 还可以是 `PurchaseCancelled` 和 `PurchaseFailed`；`Unavailable` 和 `NotAllowed` 分别模拟没有商店和关闭了付款；`Unfinished` 列出应用还没有完成的交易。

## 配置应用

- **iOS 和 macOS。** 每个 App ID 都默认开启了 App 内购买项目能力，它不需要任何 entitlement：无需添加什么。在 App Store Connect 里同意付费 App 协议，并在那里添加商品。
- **Android。** 导入了 `asuka/sdk/purchase` 的应用，构建时会带上 Play Billing 和 `com.android.vending.BILLING` 权限；没有导入的应用不会。
- **Windows。** 在合作伙伴中心为已打包（MSIX）并与商店关联的应用添加附加产品。
- **Web。** 把网站打包成启用了 Play Billing 的 Trusted Web Activity（Bubblewrap 的 `playBilling`），并在 Play 管理中心添加商品。Digital Goods API 不会为非消耗型商品做确认：请在三天内用 Play Developer API 从你的服务器确认它。

[examples/store](../../examples/store) 售卖 100 金币、Pro 和一个带一周免费试用的月度订阅，附带 StoreKit 配置文件，以及基于 `fake.Store` 的测试。
