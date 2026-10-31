# What RyUI has, against SwiftUI and Expo

Each row is something apps often need: how SwiftUI and Expo do it, and
what RyUI has for it.

✓ has it · ◐ in part (what's missing is said) · ✗ not yet

## Layout

| | SwiftUI | Expo (React Native) | RyUI |
|---|---|---|---|
| Stacks, spacing, padding, alignment | `HStack`, `VStack`, `Spacer` | flexbox | ✓ `Row`, `Column`, `Style` (flexbox: `Grow`, `Gap`, `Align`, `Justify`, `Wrap`) |
| Overlays, absolute placement | `ZStack`, `.overlay` | `position: absolute` | ✓ `Style.Absolute`, `Top`/`Left`/... |
| Scrolling | `ScrollView` | `ScrollView` | ✓ `Scroll` (vertical, horizontal; out-of-sight rows leave the tree) |
| Lazy lists, cell recycling | `List`, `LazyVStack` | `FlatList` | ✓ `List` (the platform's: UICollectionView, RecyclerView) |
| Grids | `LazyVGrid` | `FlatList numColumns` | ✓ `ListProps.Columns`, `Gap` |
| Safe area, keyboard | automatic | `SafeAreaView`, `KeyboardAvoidingView` | ✓ `SafeArea`, `UseEnvironment().Keyboard` |
| Reading a view's size | `GeometryReader` | `onLayout` | ✓ `ViewProps.OnLayout` |
| Aspect ratio, min/max sizes | `.aspectRatio`, `.frame(min:max:)` | style | ✓ `Style` |

## Controls

| | SwiftUI | Expo | RyUI |
|---|---|---|---|
| Text, styled text, links | `Text`, `AttributedString` | `Text` | ✓ `Text`, `RichText` (spans, links, inline views, selection), `ryui/markdown` |
| Buttons (variants, icons, menus) | `Button`, `Menu` | `Pressable` | ✓ `Button` (filled, tinted, gray, bordered; icon; menu), `Menu`, context menus |
| Text fields | `TextField`, `SecureField`, `TextEditor` | `TextInput` | ✓ `TextField` (secure, keyboards, return keys, multiline) |
| Toggle, slider | `Toggle`, `Slider` | community | ✓ `Toggle`, `Slider` |
| Segmented control | `Picker(.segmented)` | community | ✓ `Segmented` |
| Date and time | `DatePicker` | community | ✓ `DatePicker` (date, time, both) |
| Progress, spinner | `ProgressView` | `ActivityIndicator` | ✓ `Progress`, `Activity` |
| Wheel and menu pickers, stepper | `Picker`, `Stepper` | community | ✓ `Wheel`, `Stepper`, a menu from `Button.Menu` |
| Color picker | `ColorPicker` | community | ✓ `ColorPicker` (iOS's system picker; on Android, which has none, a Material dialog of Google's palette, with opacity) |
| Icons | SF Symbols | `@expo/vector-icons` | ✓ `Symbol` (SF Symbols; Material Symbols on Android, a curated set) |
| Images (remote, cached, placeholders) | `AsyncImage` | `expo-image` | ✓ `Image` (memory and disk cache, `Placeholder`) |
| Video | `VideoPlayer` | `expo-video` | ✓ `Video` |
| Web content | `WKWebView` | `react-native-webview` | ✓ `WebView` (URL, HTML, messages both ways, scripts before the page's, blocked requests, its state and history, downloads, new windows, cookies, a script's value; each request on Android); `ryui/js` runs JavaScript off screen |
| Maps | `Map` | `react-native-maps` | ✗ (a native view through `ui.Native` meanwhile) |
| Custom drawing | `Shape`, `Path`, `Canvas` | `react-native-svg`, Skia | ◐ `Shape` and `Path` (lines, curves, arcs, rects, ellipses; fill, stroke, caps, joins; an animatable trim); SVG icons drawn natively (`ryui/svg`); no immediate-mode canvas, no gradient fills |
| Native escape hatch | `UIViewRepresentable` | native modules | ✓ `ui.Native` views, `ui.Call` modules |

## Navigation and presentation

| | SwiftUI | Expo (expo-router) | RyUI |
|---|---|---|---|
| Stacks with native push, pop, back gestures | `NavigationStack` | Stack | ✓ `Stack`, `UseNavigator` |
| Tabs | `TabView` | Tabs | ✓ `Tabs` |
| Bar buttons, titles, search | `.toolbar`, `.searchable` | header options | ✓ `ScreenOptions` (`Actions`, `Search`) |
| Sheets with detents | `.sheet`, `.presentationDetents` | modal | ✓ `Sheet` (`Detents`, `Grabber`) |
| Alerts, action sheets | `.alert`, `.confirmationDialog` | `Alert` | ✓ `Alert` (both styles) |
| Deep links, universal and app links | `.onOpenURL` | Linking | ✓ `OnOpenURL`, `ryui.json` scheme and links |
| Pull to refresh, swipe actions | `.refreshable`, `.swipeActions` | `RefreshControl` | ✓ |

## Motion

| | SwiftUI | Expo (Reanimated) | RyUI |
|---|---|---|---|
| Animating changes | `withAnimation` | `withTiming`, `withSpring` | ✓ `Animate`, `AnimateWith` (ease, linear, spring; repeat, autoreverse) |
| Transforms | `.rotationEffect`, `.scaleEffect`, `.offset` | `transform` | ✓ `Style.Transform` |
| Enter and exit transitions | `.transition` | entering/exiting | ✓ `Style.Transition` (`Fade`, `Pop`, `SlideUp`, your own) |
| Frame-driven animation | `TimelineView(.animation)` | `useFrameCallback` | ✓ `OnFrame` |
| Shadows, gradients, blur | `.shadow`, `LinearGradient`, materials | style, expo-linear-gradient, expo-blur | ✓ `Shadow`, `LinearGradient`, `Material` |
| Gestures | tap, long press, drag, magnify, rotate | Gesture Handler | ✓ tap, long press, pan, pinch, rotate, swipe |
| Shared-element transitions | `matchedGeometryEffect`, `.navigationTransition(.zoom)` | shared transitions | ✓ `Style.Match` (a view grows from another's place and size), `ScreenOptions.ZoomFrom` (a pushed screen zooms out of a view: iOS 18's zoom; Material's container transform) |
| Haptics | `sensoryFeedback` | `expo-haptics` | ✓ `Haptic` |
| Reduce Motion | `accessibilityReduceMotion` | `useReducedMotion` | ✓ `Environment.ReduceMotion` (iOS transitions dissolve) |

## Data and services

| | SwiftUI / Apple | Expo | RyUI |
|---|---|---|---|
| State | `@State`, `@Observable` | hooks | ✓ signals (fine-grained: a write updates only what reads it) |
| Small persisted values | `@AppStorage` | AsyncStorage | ✓ `ryui/store` |
| SQLite | SwiftData / GRDB | `expo-sqlite` | ✓ `ryui/sqlite` (migrations, typed queries, transactions, writes off the main thread, live queries that run again when a table they read is written, also with arguments that change: `WatchWith`) |
| Secure storage | Keychain | `expo-secure-store` | ✓ `ryui/secure` |
| Files, network | Foundation | `expo-file-system`, fetch | ✓ Go's `os`, `net/http` |
| Photos | `PhotosPicker` | `expo-image-picker` | ✓ `device.PickPhotos` |
| Camera | AVFoundation | `expo-camera` | ✓ `ryui/camera`: `Take` (the system's camera), `View` (a live preview, front or back), `Capture`; `ryui/camera/record` records video with sound |
| Barcodes, QR codes | AVFoundation, Vision | `expo-camera` scanning | ✓ `ryui/scan`: live in `camera.View` (`Scan: scan.Codes(...)`) and in image files (`scan.File`); ML Kit on Android, only in the apps that import it |
| Location | Core Location | `expo-location` | ✓ `ryui/location` |
| Local notifications | UserNotifications | `expo-notifications` | ✓ `ryui/notify` (local; no push yet) |
| Push notifications | APNs | `expo-notifications` | ✗ |
| Clipboard, share sheet, browser | `UIPasteboard`, `ShareLink`, `SFSafariViewController` | `expo-clipboard`, `expo-sharing`, `expo-web-browser` | ✓ `Copy`, `device.Clipboard`, `Share` (text), `ShareFiles` (files, with expo-sharing's MIME type, UTI, title and iPad anchor; done when the sheet closes), `device.Browse` |
| Keep awake, device info | `isIdleTimerDisabled`, `UIDevice` | `expo-keep-awake`, `expo-device` | ✓ `device.KeepAwake`, `device.GetInfo` |
| Network state | `NWPathMonitor` | `expo-network` | ✓ `ryui/network` (online, Wi-Fi or cellular, metered, Data Saver / Low Data Mode) |
| Saving to the gallery, Downloads | `PHPhotoLibrary` | `expo-media-library` | ✓ `ryui/library` (MediaStore's collections; Photos, or Documents in Files) |
| Work in the background | `beginBackgroundTask`, `BGTaskScheduler` | `expo-task-manager` | ◐ `ryui/background`: Android keeps the app running, its progress and buttons in a notification; iOS gives about 30 seconds; no scheduled tasks |
| Receiving shares from other apps | Share extension | `expo-sharing` (`useIncomingShare`) | ✓ `ui.OnShare`: texts, links, images, videos, audio and files, one or many, with their name, MIME type and size (`ryui.json`'s `shareTarget`: types and max, Android's intent filters and iOS's activation rule from them; on iOS a share extension that opens the app, files through an App Group); `app.ShareSheet`, the app's own screen over the app that shares (iOS: its share extension; Android: a bottom sheet in a process of its own), its data shared with the app; no resolving of links (`net/http`) |
| Audio | AVAudioPlayer | `expo-audio` | ✓ `ryui/audio` (load, play, pause, seek, volume, loop); no recording |
| Custom fonts | `.font(.custom)` | `expo-font` | ✓ `Font.Family` |
| Lifecycle | `scenePhase` | AppState | ✓ `app.OnLifecycle` |

## Appearance and accessibility

| | SwiftUI | Expo | RyUI |
|---|---|---|---|
| Dark mode, semantic colors, accent | automatic | `useColorScheme` | ✓ semantic colors, `ryui.json` accent (Material 3 dynamic color) |
| Dynamic Type, font scale | automatic | `allowFontScaling` | ✓ text styles and custom fonts scale |
| Screen reader labels, roles, hiding | `.accessibilityLabel`... | `accessibilityLabel`... | ✓ `Label`, `Hint`, `Role`, `Decorative`; pressable views are buttons |
| Announcements | `AccessibilityNotification.Announcement` | `announceForAccessibility` | ✓ `Announce` |
| Custom actions, adjustable | `.accessibilityAction`, `.accessibilityAdjustableAction` | `accessibilityActions` | ✓ `ViewProps.Actions`, `OnAdjust` (and List swipe actions are TalkBack actions) |

## Tooling

| | Xcode / Expo | RyUI |
|---|---|---|
| Live reload, keeping state | previews, Fast Refresh | ✓ `ryui dev`: the Go code runs on the Mac, reloads on save, keeps signals' values |
| Release builds and signing | Xcode, EAS Build | ✓ `ryui build -ios` (.ipa), `ryui build -android` (.aab, .apk) |
| GUI tests | XCUITest, Detox, Maestro | ✓ `e2e/` (both platforms; a CI job for each, not yet run on GitHub) |
| Benchmarks | Instruments | ✓ `bench/` (against SwiftUI and Compose) |
| Over-the-air updates | — | EAS Update: ✗ |

## Not yet

In order of what apps miss most: push notifications and maps (both set
aside for now: push needs an APNs key and a Firebase project, Android's
maps a provider's choice). Maps can be an app's own native view through
`ui.Native` until then.
