# What Asuka has, against SwiftUI and Expo

Each row is something apps often need: how SwiftUI and Expo do it, and
what Asuka has for it. A ✓ is on every platform Asuka runs on (iOS,
Android, the Mac, Windows, Linux and the web) unless the row says
otherwise; where a platform hasn't something (a Mac has no sensors, a
browser no home screen widgets), its package answers that it hasn't.
In a browser, what each package does instead where a browser can't do
what a phone does is in [The web](web.md#native-modules).

SwiftUI also runs on the Mac. Expo runs on iOS, Android and the web;
React Native reaches Windows and the Mac through Microsoft's React
Native for Windows and macOS, outside Expo. For Asuka against the
desktop's own frameworks (Electron, Tauri, Wails), see
[On the desktop](#on-the-desktop), and for what a desktop app needs
beyond its windows, [The desktop](#the-desktop); against MyGo, which
draws its own UI, see [Asuka and MyGo](mygo.md).

✓ has it · ◐ in part (what's missing is said) · ✗ not yet

## Layout

| | SwiftUI | Expo (React Native) | Asuka |
|---|---|---|---|
| Stacks, spacing, padding, alignment | `HStack`, `VStack`, `Spacer` | flexbox | ✓ `Row`, `Column`, `Style` (flexbox: `Grow`, `Gap`, `Align`, `Justify`, `Wrap`) |
| Overlays, absolute placement | `ZStack`, `.overlay` | `position: absolute` | ✓ `Style.Absolute`, `Top`/`Left`/... |
| Scrolling | `ScrollView` | `ScrollView` | ✓ `Scroll` (vertical, horizontal; out-of-sight rows leave the tree) |
| Lazy lists, cell recycling | `List`, `LazyVStack` | `FlatList` | ✓ `List` (the platform's: UICollectionView, RecyclerView; on the Mac, Windows, Linux and the web, the host binds and recycles the rows in view) |
| Grids | `LazyVGrid` | `FlatList numColumns` | ✓ `ListProps.Columns`, `Gap` |
| Chat lists: start at the end, load older above | `defaultScrollAnchor(.bottom)`, `scrollPosition` | `FlatList inverted`, `maintainVisibleContentPosition` | ✓ `ListProps.Anchor`, `OnStartReached`; rows added above what shows leave it in place, in every list |
| Sections, sticky headers | `Section` in a `List` | `SectionList`, `stickySectionHeadersEnabled` | ✓ `ListProps.Section`, `Header` (sticky in plain and bare lists) |
| Selecting rows | `List(selection:)`, `EditButton` | community | ✓ `ListProps.Selecting`, `Selected`, `OnSelect`, `OnSelectBegin` (iOS's edit mode and two-finger pan; Android's checkboxes and long press) |
| Safe area, keyboard | automatic | `SafeAreaView`, `KeyboardAvoidingView` | ✓ `SafeArea`, `UseEnvironment().Keyboard` |
| Reading a view's size | `GeometryReader` | `onLayout` | ✓ `ViewProps.OnLayout` |
| Aspect ratio, min/max sizes | `.aspectRatio`, `.frame(min:max:)` | style | ✓ `Style` |

## Controls

| | SwiftUI | Expo | Asuka |
|---|---|---|---|
| Text, styled text, links | `Text`, `AttributedString` | `Text` | ✓ `Text`, `RichText` (spans, links, inline views, selection), `asuka/sdk/markdown` |
| Buttons (variants, icons, menus) | `Button`, `Menu` | `Pressable` | ✓ `Button` (filled, tinted, gray, bordered; icon; menu), `Menu`, context menus; menu items disabled, checked, in submenus and titled sections (`Action.Disabled`, `Checked`, `Children`, `Inline`; `ui.Submenu`, `ui.MenuSection`), bar buttons that open menus |
| Text fields | `TextField`, `SecureField`, `TextEditor` | `TextInput` | ✓ `TextField` (secure, keyboards, return keys, multiline) |
| The cursor, inserting at it | `TextSelection` | `selection`, `onSelectionChange` | ✓ `TextFieldProps.Selection`, `ui.InsertText` |
| Rich text editing (a mail's body) | `TextEditor` with `AttributedString` (iOS 26) | community (10tap, pell) | ✓ `TextFieldProps.Rich`, `ToggleFormat`, `SetLink`, `OnFormats` |
| A bar on the keyboard | `.toolbar { ToolbarItemGroup(placement: .keyboard) }` | `InputAccessoryView` (iOS) | ✓ `TextFieldProps.Accessory` (both) |
| Pasted images, keyboard GIFs and stickers | `onPasteCommand`, `PasteButton` | community (`onPaste`) | ✓ `TextFieldProps.PasteTypes`, `OnPaste` (Android's `commitContent` too) |
| Toggle, slider | `Toggle`, `Slider` | community | ✓ `Toggle`, `Slider` |
| Segmented control | `Picker(.segmented)` | community | ✓ `Segmented` |
| Date and time | `DatePicker` | community | ✓ `DatePicker` (date, time, both) |
| Progress, spinner | `ProgressView` | `ActivityIndicator` | ✓ `Progress`, `ProgressRing` (circular, with a value or without), `Activity` |
| Wheel and menu pickers, stepper | `Picker`, `Stepper` | community | ✓ `Wheel`, `Picker` (a pop-up button on iOS and the Mac, Material's exposed dropdown menu on Android), `Stepper` |
| Color picker | `ColorPicker` | community | ✓ `ColorPicker` (iOS's system picker; on Android, which has none, a Material dialog of Google's palette, with opacity) |
| Icons | SF Symbols | `@expo/vector-icons` | ✓ `Symbol` (SF Symbols on iOS and the Mac; for the same names, Material Symbols on Android and the web, Segoe Fluent Icons on Windows, the icon theme's or Material Symbols on Linux: a curated set) |
| Images (remote, cached, placeholders) | `AsyncImage` | `expo-image` | ✓ `Image` (memory and disk cache, `Placeholder`, BlurHash and ThumbHash placeholders, a cross-dissolving `Transition`; WebP, HEIC and AVIF where the platform decodes them, on Windows through WIC's installed codecs; animated GIF, APNG and WebP playing on every platform, `Animated`, Reduce Motion; SVG on the Mac, Linux and the web) |
| Lottie animations | community (lottie-ios) | `lottie-react-native` | ✓ `asuka/sdk/lottie` (lottie-ios, lottie-android, lottie-web; drawn by its own renderer on Windows and Linux: JSON and dotLottie, playing, looping, speed, `Progress` read and written, animated in `ui.Animate`, segments, markers, keypath overrides for themes and dark mode, Reduce Motion) |
| Video | `VideoPlayer` | `expo-video` | ✓ `Video` (AVKit on iOS and the Mac, Media3 on Android, WebView2 on Windows, GStreamer on Linux, `<video>` on the web) |
| Web content | `WKWebView` | `react-native-webview` | ✓ `WebView` (WKWebView on iOS and the Mac, Android's WebView, WebView2 on Windows, WebKitGTK on Linux; URL, HTML, messages both ways, scripts before the page's, blocked requests, its state and history, downloads, new windows, cookies, a script's value; each request on Android; on the web, all of it for the app's own site and for pages that cooperate through `postMessage`, while another site's page only loads); `asuka/sdk/js` runs JavaScript off screen |
| Maps | `Map` | `expo-maps` | ✓ `asuka/sdk/maps` (Apple Maps; Google Maps with an API key: region, markers, the user's location, taps; libshumate's OpenStreetMap map on Linux; on Windows the host draws OpenStreetMap's tiles itself) |
| Custom drawing | `Shape`, `Path`, `Canvas` | `react-native-svg`, Skia | ✓ `Shape` and `Path` (fill, stroke, caps, joins, dashes, an animatable trim); `Canvas` (paths, gradients, text, transforms, clips, opacity); SVG icons drawn natively (`asuka/sdk/svg`) |
| Charts | Swift Charts | `victory-native`, community | ✓ `asuka/sdk/chart` (bars, lines, areas, points) |
| Shaders | `.colorEffect(ShaderLibrary...)` | Skia's runtime effects | ✓ `Shader` (Kage, Go's syntax: Metal on iOS and the Mac, AGSL on Android, WebGL 2 on the web, Direct3D 11 on Windows, OpenGL on Linux) |
| Native escape hatch | `UIViewRepresentable` | native modules | ✓ native packages: typed modules and views declared in Go, implemented in Swift on iOS and macOS, Kotlin on Android, JavaScript on the web, Go on Windows and Linux ([Native code](native.md)) |
| Paging, page dots | `.scrollTargetBehavior(.paging)`, `TabView(.page)` | `react-native-pager-view` | ✓ `ScrollProps.Paging`, `PageControl` |
| Checkbox | `Toggle` (`.checkbox`, macOS) | community | ✓ `ToggleProps.Checkbox` |

## Navigation and presentation

| | SwiftUI | Expo (expo-router) | Asuka |
|---|---|---|---|
| Stacks with native push, pop, back gestures | `NavigationStack` | Stack | ✓ `Stack`, `UseNavigator` |
| Tabs | `TabView` | Tabs | ✓ `Tabs` |
| Sidebar, drawer | `NavigationSplitView` | Drawer | ✓ `SplitView` (a stack on iPhone, a drawer on Android; `NSSplitViewController` on the Mac, a navigation pane on Windows, libadwaita's split view on Linux) |
| Badges on tabs and sidebar rows | `badge` | `tabBarBadge`, `drawerItem` counts | ✓ `Tab.Badge`, `Destination.Badge` (`badgeValue`, `BadgeDrawable`, a drawer counter; the label on the Mac's `NSTabView`, the source list's count; Fluent's `InfoBadge`; libadwaita's `badge-number`) |
| Bar buttons, titles, search | `.toolbar`, `.searchable` | header options | ✓ `ScreenOptions` (`Actions`, `Search`) |
| Bottom toolbar, primary action (FAB) | `ToolbarItem(placement: .bottomBar)` | — | ✓ `ScreenOptions` (`Toolbar`, `Primary`, `PrimaryExtended`): UIKit's toolbar with a prominent trailing item, Material's `BottomAppBar` and (extended) FAB, the window's toolbar on a Mac, a command bar on Windows, a `GtkActionBar` on Linux, a bar and a floating button on the web |
| Sheets with detents | `.sheet`, `.presentationDetents` | modal | ✓ `Sheet` (`Detents`, `Grabber`) |
| Full-screen modals | `.fullScreenCover` | `presentation: "fullScreenModal"` | ✓ `Sheet` with `FullScreen` (iOS's `.fullScreen`; Material's full-screen dialog, edge to edge, predictive back; a sheet or window over all of a desktop window's content; a `<dialog>` over the page) |
| Taking back, preventing dismissal | `.navigationBarBackButtonHidden`, `.interactiveDismissDisabled` | `BackHandler`, `usePreventRemove` | ✓ `OnBack` (screens, sheets, the app) |
| Popovers | `.popover` | — | ✓ `Popover` (a popover on iPhone too) |
| Alerts, action sheets | `.alert`, `.confirmationDialog` | `Alert` | ✓ `Alert` (both styles; an alert's text fields: `Fields`, a password's `Secure`, buttons following them through `Disabled`) |
| Snackbars, toasts (a brief message with Undo) | — | community (react-native-paper's `Snackbar`) | ✓ `Snackbar` (Material's on Android, `AdwToast` on Linux; drawn as the platform's on iOS, macOS, Windows and the web) |
| Deep links, universal and app links | `.onOpenURL` | Linking | ✓ a `Router` goes to its routes' links itself (the scheme's, universal and app links, notifications' URLs); `OnOpenURL` for the rest; `asuka.json` scheme and links |
| URL routes: path patterns, nested navigators, an initial route under a link, redirects, guards, not found | `NavigationPath` with `Codable` values | expo-router (`[id]`, `[...rest]`, `unstable_settings`, `<Redirect>`, `Stack.Protected`, `+not-found`) | ✓ `Router` and `Route` (`:id`, `*rest`, `Initial`, `Redirect`, `Guard`, `NotFound`; stacks, tabs, split views, slots and sheets as routes), `Navigate`, `Push`, `Replace`, `Back`, `SetQuery`, `NavLink` and `Href` on views and buttons; the state restores from its address (a reload, a desktop window's last run, `asuka dev`'s reloads) |
| Typed links | `NavigationPath` values | typed routes (generated `.d.ts`) | ✓ `asuka vet`'s `routes` (a constant href no route takes, `HrefOf`'s values); `ui.HrefOf` fills a route's pattern |
| Pull to refresh, swipe actions | `.refreshable`, `.swipeActions` | `RefreshControl` | ✓ (both edges: `SwipeActions`, `LeadingSwipeActions`) |

## Motion

| | SwiftUI | Expo (Reanimated) | Asuka |
|---|---|---|---|
| Animating changes | `withAnimation` | `withTiming`, `withSpring` | ✓ `Animate`, `AnimateWith` (ease, linear, spring; repeat, autoreverse) |
| Transforms | `.rotationEffect`, `.scaleEffect`, `.offset` | `transform` | ✓ `Style.Transform` |
| Enter and exit transitions | `.transition` | entering/exiting | ✓ `Style.Transition` (`Fade`, `Pop`, `SlideUp`, your own) |
| Frame-driven animation | `TimelineView(.animation)` | `useFrameCallback` | ✓ `OnFrame` |
| Shadows, gradients, blur | `.shadow`, `LinearGradient`, materials | style, expo-linear-gradient, expo-blur | ✓ `Shadow`, `LinearGradient`, `Material` |
| Gestures | tap, long press, drag, magnify, rotate | Gesture Handler | ✓ tap, long press, pan, pinch, rotate, swipe |
| Keys of a hardware keyboard | `.onKeyPress`, `.keyboardShortcut` | `onKeyPress` (a `TextInput`'s; React Native for Windows and macOS: `onKeyDown`) | ✓ `ViewProps.Keys`, `TextFieldProps.Keys` (from the focused view out, ahead of the control and the menus), `UseKeys` (the window's) |
| Pressed and hover states | `ButtonStyle`'s `isPressed`, `.onHover` | `Pressable`'s `pressed`, `onHoverIn`/`onHoverOut` | ✓ `Style.Pressed`, `Hovered` variants; `Interaction` for a group's or a peer's (`Style.When`); `OnPressedChange`, `OnHoverChange` |
| Drag and drop | `.draggable`, `.dropDestination` | (none built in) | ✓ `Drag`, `Drop` (Go values within the app; texts, links, files with other apps; `Dragging` and `DragOver` variants) |
| Shared-element transitions | `matchedGeometryEffect`, `.navigationTransition(.zoom)` | shared transitions | ✓ `Style.Match` (a view grows from another's place and size), `ScreenOptions.ZoomFrom` (a pushed screen zooms out of a view: iOS 18's zoom; Material's container transform) |
| Haptics | `sensoryFeedback` | `expo-haptics` | ✓ `Haptic` |
| Reduce Motion | `accessibilityReduceMotion` | `useReducedMotion` | ✓ `Environment.ReduceMotion` (iOS transitions dissolve) |

## Data and services

| | SwiftUI / Apple | Expo | Asuka |
|---|---|---|---|
| State | `@State`, `@Observable` | hooks | ✓ signals (fine-grained: a write updates only what reads it) |
| Small persisted values | `@AppStorage` | AsyncStorage | ✓ `asuka/sdk/store` |
| SQLite | SwiftData / GRDB | `expo-sqlite` | ✓ `asuka/sdk/sqlite` (migrations, typed queries, transactions, writes off the main thread, live queries that run again when a table they read is written, also with arguments that change: `WatchWith`; in the browser too, SQLite's WebAssembly build) |
| Secure storage | Keychain | `expo-secure-store` | ✓ `asuka/sdk/secure` (on the web: encrypted in IndexedDB under a key the browser won't let out; not a keychain) |
| Files, network | Foundation | `expo-file-system`, fetch | ✓ Go's `os`, `net/http` |
| Photos | `PhotosPicker` | `expo-image-picker` | ✓ `device.PickPhotos` |
| Documents | `UIDocumentPickerViewController` | `expo-document-picker` | ✓ `device.PickFiles` (MIME types, several) |
| Camera | AVFoundation | `expo-camera` | ✓ `asuka/sdk/camera`: `Take` (the system's camera; on the Mac, Windows and Linux, a dialog with a live preview; Windows: Media Foundation's capture engine; Linux: the camera portal or Video4Linux through GStreamer), `View` (a live preview, front or back), `Capture`; `asuka/sdk/camera/record` records video with sound |
| Barcodes, QR codes | AVFoundation, Vision | `expo-camera` scanning | ✓ `asuka/sdk/scan`: live in `camera.View` (`Scan: scan.Codes(...)`) and in image files (`scan.File`); ML Kit on Android, only in the apps that import it; ZBar on Linux; on Windows Asuka's own reader in Go (`internal/barcode`: QR, Data Matrix, EAN, UPC, Code 128, Code 39) |
| Location | Core Location | `expo-location` | ✓ `asuka/sdk/location` (place names: on Windows, OpenStreetMap's Nominatim); when in use and always, precise or approximate; background updates and geofencing (`expo-task-manager`'s tasks: the app started for their events on iOS and Android; computed while the app runs where the system has none) |
| Translations, plurals, regional formats | String Catalogs, FormatStyle | `expo-localization`, i18next | ✓ `asuka/i18n`, catalogs ([Languages](i18n.md)) |
| Right-to-left layout | `layoutDirection` | `I18nManager` | ✓ follows the language, `ui.WithDirection` |
| Local notifications | UserNotifications | `expo-notifications` | ✓ `asuka/sdk/notify` (now or scheduled, images; iOS, Android, macOS, Linux; Windows' toasts, with buttons and replies through the app's COM activator; on the web through the app's service worker, so they stay and answer after the page closes, a scheduled one showing at its time while a page of the app is open, else as the next opens: browsers can't schedule one) |
| Push notifications | APNs | `expo-notifications` | ✓ `asuka/sdk/push` (APNs on iOS and macOS, Firebase Cloud Messaging, Web Push with VAPID on the web: the token, messages received and tapped; `asuka/sdk/push/webpush` sends Web Push from a Go server) |
| Notification actions, inline replies | `UNNotificationCategory`, `UNTextInputNotificationAction` | `setNotificationCategoryAsync` | ✓ `notify.SetCategories`, `OnAction` (background actions run the app's Go code, started without a screen if it isn't running; `Response.Defer`; Android's reply posted back into the conversation; on the web, the service worker's notification actions, inline replies where Chrome has them) |
| Threads, the notifications showing, the badge | `threadIdentifier`, `getDeliveredNotifications`, `setBadgeCount` | `getPresentedNotificationsAsync`, `setBadgeCountAsync` | ✓ `Thread` (Android: a group with its summary kept), `notify.Showing`, `Remove`, `RemoveThread`, `SetBadge` (Android: the launcher's, from the notifications) |
| Notifications in the foreground | `willPresent` | `setNotificationHandler` | ✓ `notify.Present` (banner, quiet, hidden) |
| Android notification channels | — | `setNotificationChannelAsync` | ✓ `notify.SetChannels`, `SetChannelGroups`; a push's `channel_id` (iOS has none) |
| Messages in conversations | Communication notifications (`INSendMessageIntent`) | community | ✓ `Notification.From`, `push.Message.From`: iOS's communication notifications (the entitlement added for the apps that use it), Android's MessagingStyle and conversation shortcuts; plain on macOS; no bubbles |
| Changing a push before it shows | Notification Service Extension | community (`expo-notifications` has none) | ✓ `push.Rewrite`: the app's Go code in iOS's extension (built and signed by `asuka build`), in the app's process for Android's data messages |
| Clipboard, share sheet, browser | `UIPasteboard`, `ShareLink`, `SFSafariViewController` | `expo-clipboard`, `expo-sharing`, `expo-web-browser` | ✓ `Copy`, `device.Clipboard` (text; images: `CopyImage`, `ClipboardImage`; `ClipboardHas`, `OnClipboardChange`), `Share` (text), `ShareFiles` (files, with expo-sharing's MIME type, UTI, title and iPad anchor; done when the sheet closes), `device.Browse` |
| Previewing files | `QLPreviewController`, `.quickLookPreview` | `react-native-file-viewer` | ✓ `asuka/sdk/preview` (Quick Look on iOS and the Mac, several files, Markup; Android's viewer apps; Windows's and Linux's default app) |
| Printing, PDFs from HTML | `UIPrintInteractionController`, `UIPrintPageRenderer` | `expo-print` | ✓ `asuka/sdk/print` (files, HTML, a WebView, with the system's dialog and its result; `PDF`: paginated, the region's paper, margins) |
| Mail and message composers | `MFMailComposeViewController`, `MFMessageComposeViewController` | `expo-mail-composer`, `expo-sms` | ✓ `asuka/sdk/compose` (recipients, subject, HTML body, attachments; what the user did on iOS; the mail apps only on Android; Messages on the Mac) |
| Keep awake, device info | `isIdleTimerDisabled`, `UIDevice` | `expo-keep-awake`, `expo-device` | ✓ `device.KeepAwake`, `device.GetInfo` |
| Network state | `NWPathMonitor` | `expo-network` | ✓ `asuka/sdk/network` (online, Wi-Fi or cellular, metered, Data Saver / Low Data Mode) |
| Saving to the gallery, Downloads | `PHPhotoLibrary` | `expo-media-library` | ✓ `asuka/sdk/library` (MediaStore's collections; Photos, or Documents in Files) |
| Work in the background | `beginBackgroundTask`, `BGTaskScheduler` | `expo-task-manager`, `expo-background-task` | ✓ `asuka/sdk/background`: Android keeps the app running, its progress and buttons in a notification; iOS gives about 30 seconds; the Mac keeps it running (App Nap off), Linux and Windows keep the system from suspending. `asuka/sdk/periodic`: tasks the system runs now and then, the app's Go code started without a screen (BGTaskScheduler, WorkManager; on the Mac while the app runs, `NSBackgroundActivityScheduler`; on Linux and Windows, a timer while it runs) |
| Receiving shares from other apps | Share extension | `expo-sharing` (`useIncomingShare`) | ✓ `ui.OnShare`: texts, links, images, videos, audio and files, one or many, with their name, MIME type and size (`asuka.json`'s `shareTarget`: types and max, Android's intent filters and iOS's activation rule from them; on iOS a share extension that opens the app, files through an App Group); `app.ShareSheet`, the app's own screen over the app that shares (iOS: its share extension; Android: a bottom sheet in a process of its own), its data shared with the app; no resolving of links (`net/http`) |
| Audio | AVAudioPlayer, AVAudioRecorder | `expo-audio` | ✓ `asuka/sdk/audio` (load, play, pause, seek, volume, loop); `asuka/sdk/audio/record` (voice or music quality, pause, a level meter) |
| Now Playing, lock screen controls, media keys | `MPNowPlayingInfoCenter`, `MPRemoteCommandCenter` | `expo-audio`'s lock screen controls, community (`react-native-track-player`) | ✓ `asuka/sdk/nowplaying` (title, artist, album, artwork, duration, a position the system advances, play, pause, next, previous, seek, skips, stop, enabled by the handlers set; follows an `audio.Player` by itself; a MediaSession and a media playback service on Android; Control Center on the Mac, the media flyout on Windows, MPRIS on Linux, the Media Session API on the web) |
| Sign in with Apple | `SignInWithAppleButton` | `expo-apple-authentication` | ✓ `asuka/sdk/appleauth` (Apple's button, the credential, its state; iOS and macOS) |
| Apple Pay, Google Pay | PassKit (`PayWithApplePayButton`) | `@stripe/stripe-react-native`'s Platform Pay, `react-native-payments` | ✓ `asuka/sdk/pay` (the wallet's sheet and button; the token for the app's processor; shipping, coupons and card changes that update the totals; errors by field; subscriptions, deferred and reload payments; iOS, macOS, Android, Apple Pay in Safari and Google Pay in other browsers) |
| Face ID, fingerprints | LocalAuthentication | `expo-local-authentication` | ✓ `asuka/sdk/biometric` (Windows Hello on Windows; on the web, the platform's authenticator through WebAuthn) |
| In-app purchases, subscriptions | StoreKit 2 | `expo-iap`, `react-native-iap`, RevenueCat | ✓ `asuka/sdk/purchase` (products with localized prices and offers, purchases, Ask to Buy and pending payments, a stream of transactions from the app's start: renewals, refunds, purchases outside the app; finishing and consuming, entitlements as a signal, restore, subscription status, Play's upgrades and downgrades, the manage, refund and offer code sheets, the JWS and the purchase token for a server; StoreKit 2 on iOS and macOS, Play Billing 9, the Microsoft Store, the Digital Goods API in a Trusted Web Activity; `fake.Store` for tests) |
| Store review | `requestReview` | `expo-store-review` | ✓ `asuka/sdk/review` (the App Store's prompt, Play's in-app review) |
| Tracking permission | App Tracking Transparency | `expo-tracking-transparency` | ✓ `asuka/sdk/tracking` |
| App integrity | App Attest | `expo-app-integrity` | ✓ `asuka/sdk/integrity` (App Attest, Play Integrity) |
| Age range | Declared Age Range | `expo-age-range` | ✓ `asuka/sdk/agerange` (Declared Age Range, Play Age Signals) |
| Sensors | Core Motion | `expo-sensors` | ✓ `asuka/sdk/sensors` (accelerometer, gyroscope, magnetometer, device motion with attitude and heading, barometer, light, pedometer; the same axes and units on both; a Mac and Windows have none; Linux: iio-sensor-proxy's light and orientation) |
| Image editing | Core Graphics, ImageIO | `expo-image-manipulator` | ✓ `asuka/sdk/imageedit` (resize, crop, rotate, flip, fill, pixelate; HEIC in, JPEG or PNG out, upright, without the source's metadata; GdkPixbuf's decoders on Linux, WIC's on Windows) |
| Text, faces and codes in an image | Vision | no | ✓ `asuka/sdk/vision`, with where each is; ML Kit on Android, only in the apps that import it; Linux: ZBar's codes and Tesseract's text, no faces; Windows: its OCR's text and faces, Asuka's reader's codes |
| The photo library | PhotoKit | `expo-media-library` | ✓ `asuka/sdk/photos`: permission, listing, thumbnails, the original files (Windows: the Pictures folder) |
| Video thumbnails | `AVAssetImageGenerator` | `expo-video-thumbnails` | ✓ `imageedit.VideoFrame` |
| Text to speech | `AVSpeechSynthesizer` | `expo-speech` | ✓ `asuka/sdk/speech` (a queue, voices, pitch, rate, pause at a word) |
| Contacts | Contacts, ContactsUI | `expo-contacts` | ✓ `asuka/sdk/contacts` (the picker and new contact screen without permission; the address book with it; Windows: adding opens a vCard, no address book) |
| Calendars | EventKit, EventKitUI | `expo-calendar` | ✓ `asuka/sdk/calendar` (the event editor without permission; calendars and events with it; Windows: adding opens an iCalendar file, no calendars) |
| Home screen widgets | WidgetKit | `expo-widgets` | ✓ `asuka/sdk/widget` and `asuka.json`'s `widgets` (a template in every size, the lock screen's on iOS; timelines, countdowns, taps with a URL) |
| Live Activities | ActivityKit | `expo-live-activity` | ✓ `asuka/sdk/liveactivity` (iOS: the lock screen and the Dynamic Island; Android: an ongoing notification, Android 16's Live Update; countdowns, progress, alerts, updates and starts from a server's push) |
| App Intents, shortcuts | App Intents, App Shortcuts | — | ✓ `asuka.json`'s `shortcuts` (iOS: App Shortcuts with Siri phrases, in Spotlight and Shortcuts; Android: launcher shortcuts) |
| Custom fonts | `.font(.custom)` | `expo-font` | ✓ `Font.Family` |
| Lifecycle | `scenePhase` | AppState | ✓ `app.OnLifecycle` |
| Launch screen | `UILaunchScreen` | `expo-splash-screen` | ✓ `asuka.json`'s `splash`; `asuka/sdk/splash` holds it while the app loads |
| Status bar, navigation bar | `statusBarHidden`, `preferredStatusBarStyle` | `expo-status-bar`, `expo-navigation-bar` | ✓ `asuka/sdk/screen`'s `SetBars` |
| Orientation lock | `supportedInterfaceOrientations` | `expo-screen-orientation` | ✓ `asuka/sdk/screen`'s `Lock` |
| Screenshots | `isCaptured`, `userDidTakeScreenshotNotification` | `expo-screen-capture` | ✓ `asuka/sdk/screen`'s `PreventCapture` (Android: blank captures; iOS: covered while recorded; macOS and Windows: the windows left out), `OnScreenshot` (Linux and Windows: new images in Pictures/Screenshots) |
| Crash reports | MetricKit, Crashlytics, Sentry | Sentry, Crashlytics (`@react-native-firebase/crashlytics`) | ✓ `asuka/sdk/crash`: Go panics and fatal errors, uncaught exceptions, MetricKit's crashes and hangs, Android's native crashes and ANRs, with breadcrumbs and values; the app sends them where it likes (no service built in); dSYMs and native debug symbols from `asuka build` |

## Appearance and accessibility

| | SwiftUI | Expo | Asuka |
|---|---|---|---|
| Dark mode, semantic colors, accent | automatic | `useColorScheme` | ✓ semantic colors, `asuka.json` accent (Material 3 dynamic color on Android; on the Mac, AppKit's own controls keep the user's accent) |
| Dynamic Type, font scale | automatic | `allowFontScaling` | ✓ text styles and custom fonts scale |
| Screen reader labels, roles, hiding | `.accessibilityLabel`... | `accessibilityLabel`... | ✓ `Label`, `Hint`, `Role`, `Decorative`; pressable views are buttons |
| Announcements | `AccessibilityNotification.Announcement` | `announceForAccessibility` | ✓ `Announce` |
| Moving a screen reader's focus | `@AccessibilityFocusState` | `setAccessibilityFocus` | ✓ `AccessibilityFocus` |
| Is a screen reader on | `voiceOverEnabled` | `isScreenReaderEnabled` | ✓ `Environment.ScreenReader` |
| Custom actions, adjustable | `.accessibilityAction`, `.accessibilityAdjustableAction` | `accessibilityActions` | ✓ `ViewProps.Actions`, `OnAdjust` (and List swipe actions are TalkBack actions) |

## Tooling

| | Xcode / Expo | Asuka |
|---|---|---|
| Live reload, keeping state | previews, Fast Refresh | ✓ `asuka dev`: reloads on save, keeps signals' values (on a phone the Go code runs on your computer; on the desktop the app is rebuilt and run again; in the browser the page reloads) |
| Release builds and signing | Xcode, EAS Build | ✓ `asuka build`: an `.ipa`, an `.aab`, a universal `.app` signed and notarized (or a Mac App Store `.pkg`), a Windows folder with its `.exe` signed, a Linux install prefix with a `.tar.gz` and a Flatpak manifest, a static site ([Shipping](shipping.md)) |
| Uploading to the stores | Xcode, EAS Submit | ✓ `asuka publish`: TestFlight, App Review, Google Play's tracks; not the Mac App Store yet |
| GUI tests | XCUITest, Detox, Maestro | ✓ `e2e/`: the examples on the iOS simulator, an Android emulator, Linux under Xvfb and headless Chrome; the Mac's and Windows' host tests drive real AppKit views and Win32 windows |
| Benchmarks | Instruments | ✓ `bench/` (against SwiftUI and Compose) |
| Embedding in an existing app | — | brownfield (React Native's) | ✓ `asuka build --platform ios --xcframework`, `asuka build --platform android --aar` |
| Over-the-air updates | — | EAS Update: ✗ |

## On the desktop

On the Mac, Windows and Linux, apps are often made with a web view
around HTML instead. How those frameworks compare with Asuka:

| | Electron | Tauri | Wails | React Native for Windows and macOS | Asuka |
|---|---|---|---|---|---|
| Platforms | Mac, Windows, Linux | Mac, Windows, Linux; iOS and Android (Tauri 2) | Mac, Windows, Linux | Windows, Mac (with React Native's iOS and Android) | Mac, Windows, Linux, iOS, Android, the web |
| What shows the interface | HTML and CSS in Chromium | HTML and CSS in the system's web view (WKWebView, WebView2, WebKitGTK) | HTML and CSS in the system's web view | the platform's native views | the platform's widgets: AppKit; Win32 and the common controls, drawn as Windows 11's; GTK 4 and libadwaita |
| The app's language | JavaScript (Node.js beside the page) | Rust, and JavaScript in the page | Go, and JavaScript in the page | JavaScript | Go |
| What ships with the app | Chromium and Node.js | the app's binary (the web view is the system's) | the app's binary (likewise) | a JavaScript engine and React Native's runtime | the app's Go binary (on Linux, GTK 4 and libadwaita come from the system); no web view unless the app shows a `WebView` |

A web view app looks the same on every system, as its HTML and CSS
say, and uses the web's libraries. A Asuka app looks as each platform's
apps do (an app that sets no style is a Mac app on the Mac, a GNOME app
on Linux, a Windows 11 app on Windows) and gets their accessibility,
text input and menus, with one layout engine, in Go, for all of them.
The desktop hosts run the app's Go in its own process, calling the
platform without cgo, so `asuka build` makes Windows and Linux builds
from any system.

## The desktop

What a desktop app needs beyond its windows and menu bar, against
Electron and MyGo ([The desktop](desktop.md)). A ✓ is on the Mac,
Windows and Linux unless the row says otherwise.

| | Electron | MyGo | Asuka |
|---|---|---|---|
| Controlling windows | `BrowserWindow` | `Window` | ✓ `ui.UseWindow`, `ui.MainWindow`: show, hide, focus, minimize, maximize, full screen, size, place, always on top, their state as signals; `OnCloseRequest`; where they were, by `StateKey`; child and modal windows; a hidden title bar (Wayland: no place, center or always on top, which the compositor decides; X11: done through Xlib) |
| Tray icons | `Tray` | tray icons | ✓ `tray.Show` (an `NSStatusItem`; the notification area; a StatusNotifierItem on Linux): icon or symbol, title, tooltip, menu, click |
| Submenus, a Quit item | `MenuItem.submenu`, roles | roles, submenus | ✓ `MenuItem.Submenu`, `ui.MenuQuit`, in the menu bar, tray and Dock menus |
| The Dock menu, jump list tasks | `app.dock.setMenu`, `setUserTasks` | the Dock menu, jump lists | ✓ `tray.DockMenu` (the Dock's menu, the jump list's Tasks, Linux's launcher quicklist) |
| Apps in the background | `app.dock.hide`, not quitting on `window-all-closed` | an app in the background | ✓ `app.KeepRunning`, `app.RunInBackground`, `asuka.json`'s `background` |
| Flyouts: a window hanging from a tray icon | a frameless `BrowserWindow` placed at `tray.getBounds()` by the app | flyouts | ✓ `WindowProps.Flyout` hanging from `tray.Icon.Anchor()` or a rectangle of its window: placed and flipped in the display, closed by a click elsewhere or Escape, a modal dialog over it (Linux: placed under X11 only) |
| Panels for launchers | `type: 'panel'` (macOS) | panels | ✓ `Panel`: floating over every app and Space, the keyboard without activating the app on a Mac, closed by Escape or a click elsewhere (Linux under Wayland: a window as others) |
| Windows sized to their content | `setContentSize` by the app | `FitContent` | ✓ `FitContent`: the core's layout sizes the window as its content changes |
| Click-through windows | `setIgnoreMouseEvents` | `SetIgnoreMouseEvents`, `SetInputRegion` | ✓ `ClickThrough`, `WindowHandle.SetClickThrough` (the whole window) |
| The apps installed, for a launcher | none | `InstalledApps`, `FileIcon`, `LaunchApp` | ✓ `asuka/sdk/apps` `List`, `Search`, `Icon`, `Open` |
| Global shortcuts | `globalShortcut` | global shortcuts | ✓ `hotkey.Register` (Carbon's hot keys; `RegisterHotKey`; X11 grabs or Wayland's GlobalShortcuts portal) |
| Open, save and folder dialogs | `dialog.showOpenDialog`, `showSaveDialog` | dialogs | ✓ `asuka/sdk/dialog` `Open`, `Save`, `Folder` (sheets on the Mac; the web: the File System Access pickers or the file input; phones: `Open` only) |
| Reveal, open in its app, move to the trash | `shell.showItemInFolder`, `openPath`, `trashItem` | the shell | ✓ `asuka/sdk/shell` `Reveal`, `Open`, `Trash`, `Beep` |
| File associations | `fileAssociations` (electron-builder), `open-file` | file associations | ✓ `asuka.json`'s `fileTypes`, `ui.OnOpenFiles` (also iOS, Android and installed web apps) |
| One instance, its second launch | `requestSingleInstanceLock`, `second-instance` | a single instance | ✓ `app.OnSecondInstance`, `asuka.json`'s `singleInstance` (the Mac passes files, not arguments) |
| Quitting, asking first | `app.quit`, `before-quit` | quitting | ✓ `app.Quit`, `app.OnQuit` (`Cancel`, `SessionEnding`) |
| The app's folders | `app.getPath` | directories | ✓ `app.DataDir`, `ConfigDir`, `CacheDir`, `LogDir` (everywhere) |
| Recent documents | `app.addRecentDocument` | recent documents | ✓ `asuka/sdk/recent` (Open Recent on the Mac, the jump list, the user's recent files on Linux) |
| Document-based apps | — | documents | ✓ `asuka/sdk/document`: a window per file, the File menu, autosave, recovery copies, save questions on close and quit, files changed by other apps (phones and the web: one document at a time, saved in place) |
| Starting at login | `app.setLoginItemSettings` | start at login | ✓ `asuka/sdk/login` (`SMAppService`, macOS 13+; the Run key; XDG autostart or the Background portal) |
| Watching files | Node's `fs.watch` (chokidar) | `WatchFiles`, the watch plugin | ✓ `asuka/sdk/watch` `Path`, `New` (FSEvents, inotify, `ReadDirectoryChangesW`; also iOS's kqueue, Android's inotify and the web's own files): renames paired, atomic saves as writes, bursts coalesced |
| Displays | `screen` | displays | ✓ `screen.UseDisplays`, `Nearest`, `Matching` (Wayland: no work area; phones and the web: theirs) |
| Sleep, wake, lock, idle time, battery | `powerMonitor` | power | ✓ `asuka/sdk/power` (phones: battery, Low Power Mode, lock; the web: the battery) |
| Progress and attention on the app's icon | `setProgressBar`, `flashFrame`, `dock.bounce` | the Dock and the taskbar | ✓ `dock.SetProgress`, `RequestAttention` (Linux: docks with the Unity launcher API) |
| Accent color, Increase Contrast, Reduce Transparency | `systemPreferences` | accent color and accessibility | ✓ `asuka/sdk/appearance` (no Reduce Transparency on Linux) |
| Now Playing and the media keys | (none: `navigator.mediaSession` in the page) | Now Playing | ✓ `asuka/sdk/nowplaying` (`MPNowPlayingInfoCenter`; the window's SystemMediaTransportControls; an MPRIS player) |
| Auto-updates: signed releases, deltas, Sparkle's window | `autoUpdater` (Squirrel), electron-updater | the updater, its plugin | ✓ `asuka/sdk/update` (`Window`, `CheckForUpdates`, `Check`, `Install`, `Relaunch`; Ed25519 signatures, delta updates, channels; GitHub releases, an S3 bucket or any HTTPS directory, a server of yours; `asuka publish --platform mac\|windows\|linux`); apps the system's packages, Flatpak or a store installed update through them; the web reloads |

Not yet, of what MyGo has: of its documents, versions (a Mac's Browse
All Versions) and locking; and of its click-through windows, an input
region (the parts of a window that still take clicks: Asuka's let the
mouse through all of it, or none).

## Not yet

In order of what apps miss most: App Intents with parameters and
entities (shortcuts take none yet), widgets of the app's own layout (one
template for now), and over-the-air updates (EAS Update's; Asuka apps are compiled Go, which the
stores don't let an app replace). On the Mac: `ScreenOptions.ZoomFrom`'s
zoom (a push slides instead), beginning a list's selection with a
gesture, and uploading to the Mac App Store with `asuka publish` (upload
the `.pkg` with Transporter).
