# Shipping

`nui build` makes the builds the stores take, from the app's directory
(or the one given), into its `dist`.

## nui.json

```json
{
  "name": "Notes",
  "id": "com.example.notes",
  "version": "1.2",
  "build": 7,
  "icon": "icon.png",
  "ios": { "team": "ABCDE12345" },
  "android": { "keystore": "upload.jks", "keyAlias": "upload" }
}
```

- `name` is under the icon; `id` is the bundle ID and application ID.
- `version` is what users see; `build` counts the builds uploaded, and
  must grow with each.
- `icon` is a square PNG, 1024×1024, without transparency for iOS.
- `orientations`: `portrait`, `landscape` (both by default).
- `accent` is the app's accent color, `"#rrggbb"`: the tint on iOS, the
  seed of the Material 3 color scheme on Android.
- `scheme` opens the app for `myapp://` URLs, and `links` for https links
  to your domains (universal links, App Links); `ui.OnOpenURL` gets them.
- `shareTarget` offers the app in other apps' share sheets, for text and
  links; `ui.OnShare` gets them. Android only, for now.
- `ios.usage` gives the purpose strings of protected resources
  (`NSCameraUsageDescription`...), and `ios.infoPlist` adds keys to
  Info.plist; `android.permissions` adds permissions.

The packages an app imports give it the permissions and purpose strings
they need: `nui/camera` the camera's, `nui/location` the location's.
Write your own in `ios.usage`: the default is generic.

## iOS

```sh
nui build -ios
```

makes a `.ipa` signed for the App Store, with the team's distribution
certificate (in your keychain, from Xcode's Accounts) and a provisioning
profile for the app's ID: `ios.profile`, or `-profile`, else the best
match installed. `-distribution ad-hoc` or `development` signs it for the
devices of its profile instead, and `-simulator` makes an unsigned `.app`
for the simulator.

Upload it with Apple's Transporter, or:

```sh
xcrun altool --upload-app -f dist/Notes.ipa -t ios --apiKey KEY --apiIssuer ISSUER
```

**Privacy manifest.** The App Store requires one, giving a reason for
each API of Apple's list the app uses. nui writes it, from what the
binary uses: Go's runtime reads the clock and file times, SQLite the disk
space; it declares no tracking and no data collected. If your app
collects data or tracks, write your own as `ios/PrivacyInfo.xcprivacy`,
with those reasons in it too; nui uses it instead.

## Android

A release is signed with your upload key. Make one, once, and keep it
safe:

```sh
keytool -genkeypair -v -keystore upload.jks -alias upload -keyalg RSA -keysize 2048 -validity 10000
```

Name it in `nui.json` (`android.keystore`, `android.keyAlias`), and give
its password in the environment:

```sh
NUI_KEYSTORE_PASSWORD=... nui build -android
```

(`NUI_KEY_PASSWORD` too if the key's differs; `NUI_KEYSTORE` and
`NUI_KEY_ALIAS` name them in CI.) It makes an `.aab` for Google Play and
an `.apk` to install yourself, for 64-bit and 32-bit ARM phones and
x86_64 (emulators, Chromebooks), Android 8 and later.

## What the stores check

Run the release build on a phone before you upload it: `nui build
-android`'s `.apk` with `adb install`, and an iOS build with
`-distribution development`. Then a first upload through TestFlight and a
Play internal test, which check what a simulator can't.
