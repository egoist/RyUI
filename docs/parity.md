# What nui has, against SwiftUI and Expo

✓ has it · ◐ in part (what's missing is said) · ✗ not yet

## Layout

| | SwiftUI | Expo (React Native) | nui |
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

| | SwiftUI | Expo | nui |
|---|---|---|---|
| Text, styled text, links | `Text`, `AttributedString` | `Text` | ✓ `Text`, `RichText` (spans, links, inline views, selection), `nui/markdown` |
| Buttons (variants, icons, menus) | `Button`, `Menu` | `Pressable` | ✓ `Button` (filled, tinted, gray, bordered; icon; menu), `Menu`, context menus |
| Text fields | `TextField`, `SecureField`, `TextEditor` | `TextInput` | ✓ `TextField` (secure, keyboards, return keys, multiline) |
| Toggle, slider | `Toggle`, `Slider` | community | ✓ `Toggle`, `Slider` |
| Segmented control | `Picker(.segmented)` | community | ✓ `Segmented` |
| Date and time | `DatePicker` | community | ✓ `DatePicker` (date, time, both) |
| Progress, spinner | `ProgressView` | `ActivityIndicator` | ✓ `Progress`, `Activity` |
| Wheel and menu pickers, stepper | `Picker`, `Stepper` | community | ◐ a menu from `Button.Menu`; no wheel picker, no stepper |
| Color picker | `ColorPicker` | community | ✗ |
| Icons | SF Symbols | `@expo/vector-icons` | ✓ `Symbol` (SF Symbols; Material Symbols on Android, a curated set) |
| Images (remote, cached, placeholders) | `AsyncImage` | `expo-image` | ✓ `Image` (memory and disk cache, `Placeholder`) |
| Video | `VideoPlayer` | `expo-video` | ✓ `Video` |
| Web content | `WKWebView` | `react-native-webview` | ✓ `WebView` (URL, HTML, messages both ways) |
| Maps | `Map` | `react-native-maps` | ✗ (a native view through `ui.Native` meanwhile) |
| Custom drawing | `Shape`, `Path`, `Canvas` | `react-native-svg`, Skia | ✗ |
| Native escape hatch | `UIViewRepresentable` | native modules | ✓ `ui.Native` views, `ui.Call` modules |

## Navigation and presentation

| | SwiftUI | Expo (expo-router) | nui |
|---|---|---|---|
| Stacks with native push, pop, back gestures | `NavigationStack` | Stack | ✓ `Stack`, `UseNavigator` |
| Tabs | `TabView` | Tabs | ✓ `Tabs` |
| Bar buttons, titles, search | `.toolbar`, `.searchable` | header options | ✓ `ScreenOptions` (`Actions`, `Search`) |
| Sheets with detents | `.sheet`, `.presentationDetents` | modal | ✓ `Sheet` (`Detents`, `Grabber`) |
| Alerts, action sheets | `.alert`, `.confirmationDialog` | `Alert` | ✓ `Alert` (both styles) |
| Deep links, universal and app links | `.onOpenURL` | Linking | ✓ `OnOpenURL`, `nui.json` scheme and links |
| Pull to refresh, swipe actions | `.refreshable`, `.swipeActions` | `RefreshControl` | ✓ |

## Motion

| | SwiftUI | Expo (Reanimated) | nui |
|---|---|---|---|
| Animating changes | `withAnimation` | `withTiming`, `withSpring` | ✓ `Animate`, `AnimateWith` (ease, linear, spring; repeat, autoreverse) |
| Transforms | `.rotationEffect`, `.scaleEffect`, `.offset` | `transform` | ✓ `Style.Transform` |
| Enter and exit transitions | `.transition` | entering/exiting | ✓ `Style.Transition` (`Fade`, `Pop`, `SlideUp`, your own) |
| Frame-driven animation | `TimelineView(.animation)` | `useFrameCallback` | ✓ `OnFrame` |
| Shadows, gradients, blur | `.shadow`, `LinearGradient`, materials | style, expo-linear-gradient, expo-blur | ✓ `Shadow`, `LinearGradient`, `Material` |
| Gestures | tap, long press, drag, magnify, rotate | Gesture Handler | ◐ tap, long press, pan, pinch, swipe; no rotation |
| Shared-element transitions | `matchedGeometryEffect` | shared transitions | ✗ |
| Haptics | `sensoryFeedback` | `expo-haptics` | ✓ `Haptic` |
| Reduce Motion | `accessibilityReduceMotion` | `useReducedMotion` | ✓ `Environment.ReduceMotion` (iOS transitions dissolve) |

## Data and services

| | SwiftUI / Apple | Expo | nui |
|---|---|---|---|
| State | `@State`, `@Observable` | hooks | ✓ signals (fine-grained: a write updates only what reads it) |
| Small persisted values | `@AppStorage` | AsyncStorage | ✓ `nui/store` |
| SQLite | SwiftData / GRDB | `expo-sqlite` | ✓ `nui/sqlite` (migrations, typed queries, transactions, live queries) |
| Secure storage | Keychain | `expo-secure-store` | ✓ `nui/secure` |
| Files, network | Foundation | `expo-file-system`, fetch | ✓ Go's `os`, `net/http` |
| Photos | `PhotosPicker` | `expo-image-picker` | ✓ `device.PickPhotos` |
| Camera | AVFoundation | `expo-camera` | ✗ |
| Location | Core Location | `expo-location` | ✓ `nui/location` |
| Local notifications | UserNotifications | `expo-notifications` | ✓ `nui/notify` (local; no push yet) |
| Push notifications | APNs | `expo-notifications` | ✗ |
| Clipboard, share sheet, browser | `UIPasteboard`, `ShareLink`, `SFSafariViewController` | `expo-clipboard`, `expo-sharing`, `expo-web-browser` | ✓ `Copy`, `device.Clipboard`, `Share`, `device.Browse` |
| Keep awake, device info | `isIdleTimerDisabled`, `UIDevice` | `expo-keep-awake`, `expo-device` | ✓ `device.KeepAwake`, `device.GetInfo` |
| Audio | AVAudioPlayer | `expo-audio` | ✗ (a `Video` plays audio, without a view) |
| Custom fonts | `.font(.custom)` | `expo-font` | ✓ `Font.Family` |
| Lifecycle | `scenePhase` | AppState | ✓ `app.OnLifecycle` |

## Appearance and accessibility

| | SwiftUI | Expo | nui |
|---|---|---|---|
| Dark mode, semantic colors, accent | automatic | `useColorScheme` | ✓ semantic colors, `nui.json` accent (Material 3 dynamic color) |
| Dynamic Type, font scale | automatic | `allowFontScaling` | ✓ text styles and custom fonts scale |
| Screen reader labels, roles, hiding | `.accessibilityLabel`... | `accessibilityLabel`... | ✓ `Label`, `Hint`, `Role`, `Decorative`; pressable views are buttons |
| Announcements | `AccessibilityNotification.Announcement` | `announceForAccessibility` | ✓ `Announce` |
| Custom actions, adjustable | `.accessibilityAction`, `.accessibilityAdjustableAction` | `accessibilityActions` | ◐ swipe actions are actions on Android; no custom actions or adjustable views of your own |

## Tooling

| | Xcode / Expo | nui |
|---|---|---|
| Live reload, keeping state | previews, Fast Refresh | ✓ `nui dev`: the Go code runs on the Mac, reloads on save, keeps signals' values |
| Release builds and signing | Xcode, EAS Build | ✓ `nui build -ios` (.ipa), `nui build -android` (.aab, .apk) |
| GUI tests | XCUITest, Detox, Maestro | ✓ `e2e/` (both platforms; a CI job for each, not yet run on GitHub) |
| Benchmarks | Instruments | ✓ `bench/` (against SwiftUI and Compose) |
| Over-the-air updates | — | EAS Update: ✗ |

## Not yet

In order of what apps miss most: custom drawing (`Shape`, `Path`,
`Canvas`); push notifications; camera; maps; audio; shared-element
transitions; wheel pickers and steppers; rotation gestures; custom
accessibility actions. Maps and the camera can be an app's own native
view through `ui.Native` until then.
