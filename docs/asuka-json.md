# asuka.json

`asuka.json`, in the app's directory, says what the platforms and the
stores need to know of the app: its name and ID, its icon, the links
and files it opens, its signing keys. `asuka dev`, `asuka build` and
`asuka publish` read it; `asuka dev` reads it as it starts, so stop it
and run it again after a change.

Every key is optional, and an app without the file builds and runs with
the defaults. A key Asuka doesn't know is ignored, so check a key's
spelling when it seems to do nothing. A value Asuka can't take stops the
build, saying which and what it wants (`asuka.json: accent "red" (want
#rrggbb or #rrggbbaa)`). Paths are relative to the app's directory.

This page lists every key. [Shipping](shipping.md#asukajson) tells how
they come together for a release, and each guide linked below tells
more of what its keys do.

```json
{
  "name": "Notes",
  "id": "com.example.notes",
  "version": "1.2.0",
  "build": 7,
  "icon": "icon.png",
  "accent": "#7c3aed",
  "scheme": "notes",
  "links": ["notes.example.com"],
  "fileTypes": [{ "name": "Notes Document", "extensions": ["note"] }],
  "shareTarget": true,
  "splash": { "image": "logo.png", "background": "#ffffff", "darkBackground": "#000000" },
  "ios": {
    "team": "ABCDE12345",
    "usage": { "NSCameraUsageDescription": "To scan your receipts." }
  },
  "android": { "keystore": "upload.jks", "keyAlias": "upload", "dynamicColor": true },
  "linux": { "summary": "Write things down", "categories": ["Office"], "license": "MIT" },
  "updates": { "publicKey": "…", "github": "you/notes" }
}
```

## The app

These apply on every platform the key means something on.

| Key | Type | Default | What it is |
|---|---|---|---|
| `name` | string | the directory's name | The app's name: under its icon, in the Mac's menu bar, the window's title on Windows, the launcher's on Linux, the web page's title. It names the build's files too (`Notes.app`, `Notes.exe`). |
| `id` | string | `dev.asuka.<directory>` | The bundle ID on iOS and the Mac, the application ID on Android and Linux (its desktop file, icon, D-Bus name and data directory), the AppUserModelID on Windows. Change it before the first release: the stores and the systems know the app by it. On Linux it must be letters, digits, `_` and `-`, in two parts or more, none starting with a digit. |
| `version` | string | `1.0` | The version users see. |
| `build` | integer | `1` | The build's number, which must grow with each upload. `asuka publish` numbers a build one above the store's highest itself, so this is only where it starts. Windows's file version is `version` and `build` together (`1.2.0.7`). |
| `icon` | path | none: the platform's generic icon | A square PNG, 1024×1024, without transparency for iOS. Each build makes the sizes its platform wants. The App Store needs one. |
| `accent` | color | the platform's | `"#rrggbb"` or `"#rrggbbaa"`: the tint on iOS, the seed of the Material 3 color scheme on Android, the color of `Accent`, filled buttons and links on the Mac (AppKit's own controls keep the user's), the controls' accent on Windows and libadwaita's on Linux, the page's and the manifest's `theme_color` on the web. |
| `orientations` | strings | both | `"portrait"`, `"landscape"`: the orientations the app takes on a phone. iPads take all four, as Split View and Stage Manager need, and Android 16 ignores it on tablets and foldables. |
| `scheme` | string | none | The app's URL scheme: `notes://` URLs open it, and `ui.OnOpenURL` gets them, on iOS, Android, the Mac, Windows and Linux. Letters, digits, `+`, `-` and `.`, a letter first. See [Deep links](navigation.md#deep-links). |
| `links` | strings | none | Domains, as `example.com`, whose https links open the app: iOS's universal links and Android's App Links. The domain serves the association file. See [Deep links](navigation.md#deep-links). |
| `fileTypes` | objects | none | The kinds of files the app opens. See [fileTypes](#filetypes). |
| `shareTarget` | `true` or object | off | The app in other apps' share sheets, on iOS and Android. See [shareTarget](#sharetarget). |
| `splash` | object | none | The launch screen, on iOS and Android. See [splash](#splash). |
| `widgets` | objects | none | Home screen widgets, on iOS and Android. See [widgets](#widgets). |
| `shortcuts` | objects | none | Actions for Siri, Spotlight and the launcher, on iOS and Android. See [shortcuts](#shortcuts). |
| `background` | boolean | `false` | On the desktop, an app that lives in the tray, as `app.RunInBackground` makes it: no window at launch, kept running with none, and on the Mac no Dock icon from launch (`LSUIElement`). See [Apps in the background](desktop.md#apps-in-the-background). |
| `singleInstance` | boolean | `true` | On Windows and Linux, a second launch hands its arguments to the running app (`app.OnSecondInstance`, its files to `ui.OnOpenFiles`) and quits. `false` runs the app as often as it's started. The Mac runs an app once either way. See [One instance](desktop.md#one-instance). |
| `i18n` | object | `locales`, when there | Where the app's catalogs are. See [i18n](#i18n). |
| `updates` | object | none | A desktop app's signed updates. See [updates](#updates). |
| `ios` | object | | The iOS build's, and some of the Mac's. See [ios](#ios). |
| `mac` | object | | The Mac build's profiles. See [mac](#mac). |
| `android` | object | | The Android build's. See [android](#android). |
| `linux` | object | | What a Linux build says of the app. See [linux](#linux). |
| `web` | object | | The web build's. See [web](#web). |
| `bluetooth` | object | | What an app of `asuka/sdk/bluetooth` asks beyond the package's needs: the background, the location from scans. See [bluetooth](#bluetooth). |

A Windows build needs nothing beyond these: its signing certificate is
given in the environment (see [Shipping](shipping.md#windows)).

## fileTypes

The kinds of files the app opens, from the file manager, a mail's
attachment or "Open With"; `ui.OnOpenFiles` gets them. Each is an
object:

```json
"fileTypes": [
  { "name": "Markdown Document", "extensions": ["md", "markdown"], "role": "viewer" },
  { "name": "Notes Document", "extensions": ["note"], "icon": "note.png" }
]
```

| Key | Type | Default | What it is |
|---|---|---|---|
| `extensions` | strings | required | The files' name extensions, without the dot: `"md"`, `"tar.gz"`. |
| `name` | string | the first extension in capitals and "Document" | What the files are, as the system shows it. |
| `mimeTypes` | strings | those Asuka knows for the extensions | Their MIME types. For an extension Asuka doesn't know, the type is the app's own: `application/x-<app>-<extension>`. |
| `uti` | string | the system's for a known extension, else `<id>.<extension>` | Their Uniform Type Identifier on Apple's platforms. One under the app's ID is the app's own type: exported, and the app is its files' owner. Another the system may not know is imported. |
| `conformsTo` | strings | `public.data`, `public.content` | The UTIs an exported or imported type conforms to. `public.plain-text` makes Quick Look and text editors take it. |
| `role` | string | `"editor"` | `"editor"` or `"viewer"`: what the app does with them (`CFBundleTypeRole` on the Mac and iOS). |
| `icon` | path | the system's, or the app's | A square PNG, the files' icon on the Mac, Windows and Linux. |

Each platform declares them its own way: Info.plist's
`CFBundleDocumentTypes` on the Mac and iOS, intent filters on Android,
the user's registry on Windows as the app starts, the desktop file and
a shared-mime-info package on Linux, the manifest's `file_handlers` on
the web. See [Shipping](shipping.md#asukajson) and
[Opening files](desktop.md#opening-files).

## shareTarget

The app in other apps' share sheets, on iOS and Android; `ui.OnShare`
gets what's shared. `true` takes texts and web links; an object says
what else:

```json
"shareTarget": { "types": ["text", "image/*", "video/*"], "max": 10 }
```

| Key | Type | Default | What it is |
|---|---|---|---|
| `types` | strings | `["text"]` | `"text"` (texts and web links), and files by MIME type: `"image/*"`, `"video/*"`, `"audio/*"`, `"application/pdf"`, `"*/*"` for any. |
| `max` | integer | `1` | How many files it takes at once, 1 or more. |

On iOS it's a share extension in the app, with its own bundle ID (the
app's plus `.share`), which a release signs with `ios.shareProfile`. One
that takes files also needs the App Group `group.<id>`. See
[Shipping](shipping.md#ios) and
[Shares from other apps](device.md#shares-from-other-apps-uionshare).

## splash

The launch screen on iOS and Android: an image in the middle of a color,
from the tap on the icon until the app's first frame (or
`splash.Hide`). The desktops and the web have none.

```json
"splash": { "image": "logo.png", "size": 96, "background": "#4A63B5", "darkBackground": "#1B2440" }
```

| Key | Type | Default | What it is |
|---|---|---|---|
| `image` | path | none | A square PNG, a logo. |
| `size` | integer | `120` | The image's width in points (dp), at most 160, the most Android 12's icon circle holds. |
| `background` | color | `"#ffffff"` | `"#rrggbb"`, in light mode. |
| `darkBackground` | color | `background` | `"#rrggbb"`, in dark mode. |

See [The launch screen](device.md#the-launch-screen-asukasdksplash).

## widgets

The app's home screen widgets, on iOS and Android; `widget.Set` fills
them from the app's Go code.

```json
"widgets": [{ "kind": "today", "name": "Today", "description": "What's next", "sizes": ["small", "medium", "rectangular"] }]
```

| Key | Type | Default | What it is |
|---|---|---|---|
| `kind` | string | required | The widget's name for `widget.Set`: letters, digits and `_`, a letter first, each widget's its own. |
| `name` | string | `kind` | What the widget gallery calls it. |
| `description` | string | none | What the widget gallery says of it. |
| `sizes` | strings | `["small", "medium"]` | `small`, `medium`, `large`, and iOS's `extraLarge` (iPad) and lock screen `circular`, `rectangular` and `inline`. |

On iOS the widgets are an extension with its own bundle ID (the app's
plus `.widgets`), which a release signs with `ios.widgetProfile`. See
[Home screen widgets](device.md#home-screen-widgets-asukasdkwidget).

## shortcuts

Actions the system offers for the app, at most ten: iOS's App Shortcuts
(Siri, Spotlight, the Shortcuts app, the Action button) and Android's
launcher shortcuts. Each opens the app with its URL, for
`ui.OnOpenURL`.

```json
"shortcuts": [{ "id": "newNote", "title": "New Note", "url": "notes://new", "symbol": "square.and.pencil", "phrases": ["New note in ${app}"] }]
```

| Key | Type | Default | What it is |
|---|---|---|---|
| `id` | string | required | Letters, digits and `_`, a letter first, each shortcut's its own. |
| `title` | string | required | What the system shows. |
| `url` | string | required | What the app is opened with: a URL of its `scheme`, or one of its `links`. |
| `symbol` | string | none | An SF Symbol's name, as `ui.Symbol` takes it. |
| `phrases` | strings | `"<title> in ${app}"` | What Siri hears, each with `${app}`, the app's name. iOS only. |

See [Shortcuts](device.md#shortcuts-siri-spotlight-the-launcher).

## i18n

Where the app's message catalogs are. An app with catalogs in a
`locales` directory needs none of it.

```json
"i18n": { "dir": "translations", "source": "ja" }
```

| Key | Type | Default | What it is |
|---|---|---|---|
| `dir` | path | `locales` | The directory of catalogs, one per language, named after its BCP 47 tag: `en.json`, `pt-BR.json`, `zh-Hans.json`. |
| `source` | string | `en`, or the only catalog there is | The language the app is written in: its catalog has every message, and the others fall back to it. |

See [Languages](i18n.md#catalogs).

## updates

A desktop app's updates: the key they're signed with, and where they're
published. `asuka update keygen` makes the key pair and prints this
object.

```json
"updates": { "publicKey": "…the contents of asuka-update.pub…", "github": "you/notes" }
```

| Key | Type | Default | What it is |
|---|---|---|---|
| `publicKey` | string | required | `asuka-update.pub`'s contents, built into the app: it takes only updates signed with the secret key. |
| `github` | string | | `"owner/name"`: a public repository whose releases, tagged `v1.2.0`, hold the updates. |
| `tagPrefix` | string | `"v"` | The releases' tags' prefix. Another, as `"desktop-v"`, lets the repository's latest release be another's: apps then find the newest tagged with it through the GitHub API. |
| `url` | string | | Instead of `github`: an HTTPS directory that serves the files (an S3 or R2 bucket, a CDN); apps read `<url>/update-<target>.json`. |
| `feed` | string | none | An HTTPS server of yours that apps ask instead, its URL with `{target}`, `{version}` and `{channel}`. It needs `url`, where the files are. |
| `s3` | object | none | The bucket that serves `url`, which `asuka publish` uploads to: `bucket` (required), `prefix` (its directory; default its root), `region` (default `AWS_REGION`, else `AWS_DEFAULT_REGION`, else `us-east-1`), `endpoint` (R2's, MinIO's; default Amazon S3), `pathStyle` (default: with `endpoint`, or a bucket name with dots). |
| `privateKey` | path | none | The secret key's path, for builds on your machine (`~/` is the home directory). |
| `changelog` | path | `CHANGELOG.md`, when there is one | A Markdown file whose `## 1.2.0` section is the release notes. |
| `deltas` | integer | `3` | How many versions before get a delta update, a download of what changed alone; `0`, none. |

It needs either `github` or `url`, not both. See
[Updates](shipping.md#updates).

## ios

The iOS build's. `team` is the Mac build's team too, and `usage`'s
purpose strings go in the Mac's Info.plist as well.

| Key | Type | Default | What it is |
|---|---|---|---|
| `team` | string | none | The Apple Developer team ID that signs the app (`ABCDE12345`). |
| `profile` | string | the best match installed | The app's provisioning profile: its path, name or UUID. `--profile` overrides it. |
| `shareProfile` | string | the best match installed | The share extension's (`shareTarget`), for the ID `<id>.share`. |
| `widgetProfile` | string | the best match installed | The widget extension's (`widgets`), for the ID `<id>.widgets`. |
| `notificationProfile` | string | the best match installed | The Notification Service Extension's (`push.Rewrite`), for the ID `<id>.notification`. |
| `usage` | object | the packages' own | Purpose strings of protected resources, by Info.plist key: `{"NSCameraUsageDescription": "To scan your receipts."}`. The packages the app imports give generic ones; write your own. |
| `infoPlist` | object | none | String keys added to Info.plist. |
| `backgroundModes` | strings | none | Added to Info.plist's `UIBackgroundModes`: `"audio"`, to record and play with the screen locked. |
| `iphoneOnly` | boolean | `false` | Leaves iPad out: the app runs there as an iPhone app, in a phone-sized window. |
| `nonExemptEncryption` | string | `"no"` | What a build says of its encryption (`ITSAppUsesNonExemptEncryption`), a legal statement that's yours to make: `"no"`, `"yes"`, or `"ask"`, which says nothing, so App Store Connect asks at each upload. |
| `collected` | objects | none | The data the app collects, for its privacy manifest (see below). |
| `appStoreConnect` | object | | The App Store Connect API key `asuka publish` uploads with: `keyId`, `issuerId` (none for an individual key) and `key`, the `.p8`'s path (default: `AuthKey_<keyId>.p8` in `~/.appstoreconnect/private_keys`, as `altool` looks). See [Publishing](shipping.md#publishing). |

Each of `collected` is a kind of data, by Apple's names without their
prefixes, for the privacy manifest Asuka writes (an app's own
`ios/PrivacyInfo.xcprivacy` replaces it, and this with it):

```json
"collected": [{ "type": "EmailAddress", "linked": true, "tracking": false, "purposes": ["AppFunctionality"] }]
```

| Key | Type | What it is |
|---|---|---|
| `type` | string | `"EmailAddress"` is `NSPrivacyCollectedDataTypeEmailAddress`. |
| `linked` | boolean | Tied to the user's identity. |
| `tracking` | boolean | Used to track them. |
| `purposes` | strings | `"AppFunctionality"` is `NSPrivacyCollectedDataTypePurposeAppFunctionality`. |

See [Shipping](shipping.md#ios).

## mac

What a Mac build signs the app with; its team is `ios.team`.

| Key | Type | Default | What it is |
|---|---|---|---|
| `profile` | string | `ASUKA_MAC_PROFILE`, else, for an app that needs one, the best match installed | A Developer ID (or development) provisioning profile's path, name or UUID, which push, Sign in with Apple and App Attest need. |
| `appStoreProfile` | string | the best match installed | A Mac App Store profile, for `--appstore`. |

`--profile` and `ASUKA_MAC_PROFILE` override them. See
[Shipping](shipping.md#macos).

## android

The Android build's.

| Key | Type | Default | What it is |
|---|---|---|---|
| `keystore` | path | none | The upload key's keystore, which signs a release. Its password is `ASUKA_KEYSTORE_PASSWORD`'s, and the key's `ASUKA_KEY_PASSWORD`'s (default: the keystore's). |
| `keyAlias` | string | none | The upload key's alias in the keystore. |
| `dynamicColor` | boolean | `false` | Takes the colors from the user's wallpaper (Android 12 and later), as Material apps do; `accent` is the seed elsewhere. |
| `permissions` | strings | the packages' own | Permissions added to the manifest: `"android.permission.CAMERA"`. `"<name>:28"` asks for one up to Android 9 (API 28) only. The packages the app imports add those they need. |
| `googleServices` | path | none | The Firebase project's `google-services.json`, for `asuka/sdk/push`. |
| `mapsKey` | string | none | A Google Maps Platform API key (Maps SDK for Android), for `asuka/sdk/maps`. |
| `serviceAccount` | path | none | The Google Cloud service account's JSON key `asuka publish` uploads to Google Play with. Keep it out of the repository. |

See [Shipping](shipping.md#android).

## linux

What a Linux build's desktop file and AppStream metainfo say of the
app: what launchers, software centers and Flathub show. Flathub and the
software centers want the summary, description, license and developer.

| Key | Type | Default | What it is |
|---|---|---|---|
| `summary` | string | none | A line saying what the app does. |
| `description` | string | none | A few paragraphs, separated by blank lines. |
| `categories` | strings | `["Utility"]` | [freedesktop.org's main categories](https://specifications.freedesktop.org/menu-spec/latest/category-registry.html): `Utility`, `Office`, `Development`, `Network`... |
| `keywords` | strings | none | More words a launcher finds the app by. |
| `license` | string | none | The app's license, as an SPDX expression: `MIT`, `GPL-3.0-or-later`, `LicenseRef-proprietary`. |
| `developer` | string | none | The developer's name. |
| `homepage` | string | none | The app's web page. |
| `flatpakRuntime` | string | a recent one | The GNOME runtime's version the Flatpak manifest asks for, 46 or later. |

See [Shipping](shipping.md#linux).

## web

The web build's.

| Key | Type | Default | What it is |
|---|---|---|---|
| `vapidPublicKey` | string | none | The app server's VAPID public key (base64url), with which the browser subscribes to Web Push for `asuka/sdk/push`. |

See [Web Push](web.md#asukasdkpush-web-push).

## bluetooth

What an app that imports `asuka/sdk/bluetooth` asks for beyond what the
package gives it (its permissions and purpose string). It does nothing
for an app that doesn't import the package.

```json
"bluetooth": { "background": ["central"], "location": false }
```

| Key | Type | Default | What it is |
|---|---|---|---|
| `background` | strings | none | The roles the app keeps in the background. `"central"`: iOS's `bluetooth-central` mode, with state restoration (`bluetooth.OnRestore`), and the receiver of Android's background scan (`ScanOptions.Background`). `"peripheral"`: iOS's `bluetooth-peripheral` mode. |
| `location` | boolean | `false` | The app finds where the user is from its scans (beacons): Android's `BLUETOOTH_SCAN` without `neverForLocation`, and the location permission `bluetooth.Request` then asks for too (iOS: `NSLocationWhenInUseUsageDescription`). |

See [Bluetooth](bluetooth.md#in-the-background).

## The environment

Secrets stay out of `asuka.json`: give them in the environment, which
CI does from its secrets. These also override the keys they name.

| Variable | What it is |
|---|---|
| `ASUKA_KEYSTORE`, `ASUKA_KEY_ALIAS` | Over `android.keystore` and `android.keyAlias`. |
| `ASUKA_KEYSTORE_PASSWORD`, `ASUKA_KEY_PASSWORD` | The keystore's password, and the key's when it differs. |
| `ASUKA_ASC_KEY_ID`, `ASUKA_ASC_ISSUER_ID`, `ASUKA_ASC_KEY` | Over `ios.appStoreConnect`; `ASUKA_ASC_KEY` is the `.p8`'s path or its text. |
| `ASUKA_PLAY_SERVICE_ACCOUNT` | Over `android.serviceAccount`: the key's path or its JSON. |
| `ASUKA_MAC_PROFILE` | Over `mac.profile`. |
| `ASUKA_UPDATE_PUBLIC_KEY`, `ASUKA_UPDATE_URL` | Over `updates.publicKey` and `updates.url` (which then replaces `github`). |
| `ASUKA_UPDATE_PRIVATE_KEY` | The updates' secret key itself, or its path, over `updates.privateKey`. |
| `ASUKA_WINDOWS_CERT`, `ASUKA_WINDOWS_CERT_PASSWORD`, `ASUKA_WINDOWS_TIMESTAMP` | A Windows build's signing certificate (a `.pfx`), its password and the timestamp servers, which `asuka.json` doesn't hold. See [Shipping](shipping.md#windows). |
