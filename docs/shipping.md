# Shipping

`asuka build` makes an app's release build for one platform, from the
app's directory (or the one given), into its `dist`. `asuka publish`
uploads the iOS and Android builds to their stores: to TestFlight and
App Review, or a Google Play track (see [Publishing](#publishing)).

| `--platform` | What it makes, in `dist` | Builds on | Given to users |
|---|---|---|---|
| `ios` | `<Name>.ipa`, signed for the App Store | a Mac with Xcode | TestFlight, the App Store (`asuka publish`) |
| `android` | `<Name>.aab`, signed with your upload key | any system with a JDK, the Android SDK and the NDK | Google Play (`asuka publish`) |
| `mac` | `<Name>.app`, universal; or a `.pkg` (`--appstore`) | a Mac with Xcode's command line tools | a download, signed and notarized; the Mac App Store |
| `windows` | `<Name>/`, the `.exe` and its assets | any system | a zip, or an installer or MSIX of yours |
| `linux` | `linux/`: an install prefix, a `.tar.gz`, a Flatpak manifest | any system | the `.tar.gz`, a Flatpak |
| `web` | `web/`, a static site | any system | any web server or CDN |

Without `--platform`, on a Mac, a Windows PC or a Linux machine, it
builds for that computer. Every build but iOS's and Android's is plain
Go with `CGO_ENABLED=0`, so it needs no C toolchain.

## asuka.json

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

Every key is optional; these apply on every platform:

- `name` is the app's name: under the icon, in the Mac's menu bar, the
  window's title on Windows, the launcher's on Linux, the web page's
  title. It names the build's files too (`Notes.app`, `Notes.exe`).
  The default is the directory's name.
- `id` is the bundle ID on iOS and the Mac, the application ID on
  Android and Linux (its desktop file, icon and D-Bus name), the
  AppUserModelID on Windows (its toasts'). The default is
  `dev.asuka.<directory>`.
- `version` is what users see (default `1.0`); `build` counts the builds
  uploaded, and must grow with each (default 1). `asuka publish` numbers
  each build after the store's highest itself, so `build` is only where
  it starts. On Windows they're the `.exe`'s file version
  (`1.2.0.7` for version 1.2 and build 7), on Linux the metainfo's
  release.
- `icon` is a square PNG, 1024×1024, without transparency for iOS. Each
  build makes the sizes its platform wants: an asset catalog on iOS,
  the launcher's on Android, an `.icns` on the Mac, an `.ico` in the
  `.exe` on Windows, the hicolor theme's from 16 to 512 pixels on
  Linux, the web manifest's, the favicon and iOS's home screen icon on
  the web.
- `accent` is the app's accent color, `"#rrggbb"`: the tint on iOS, the
  seed of the Material 3 color scheme on Android, the color of `Accent`,
  filled buttons and links on the Mac (AppKit's own controls keep the
  accent the user picks), the controls' accent on Windows in place of
  the system's, libadwaita's accent on Linux, and on the web the page's
  accent and its manifest's `theme_color`. Without it, each platform's
  own.
- `scheme` opens the app for `myapp://` URLs: on iOS, Android, the Mac
  (`CFBundleURLTypes`), Linux (the desktop file's
  `x-scheme-handler`) and Windows (the app registers it for the user as
  it starts, in `HKEY_CURRENT_USER\Software\Classes`). `links`
  opens it for https links to your domains (universal links, App Links;
  iOS and Android). `ui.OnOpenURL` gets them.
- `fileTypes` are the kinds of files the app opens, which
  `ui.OnOpenFiles` gets:

  ```json
  "fileTypes": [
    {"name": "Markdown Document", "extensions": ["md", "markdown"], "role": "viewer"},
    {"name": "Notes Document", "extensions": ["note"], "icon": "note.png"}
  ]
  ```

  `extensions` is required; `name` defaults to the first extension in
  capitals and "Document". `mimeTypes` and `uti` default to what the
  systems call an extension Asuka knows (md, txt, json, csv, pdf, png,
  jpg...); for another, the type is the app's own:
  `application/x-<app>-<ext>` and `<id>.<ext>`, exported (the Mac and
  iOS), defined for shared-mime-info (Linux), and the app is its files'
  default app. `conformsTo` is the UTIs an exported or imported type
  conforms to (default `public.data`, `public.content`); `role` is
  `editor` (default) or `viewer`; `icon`, a square PNG, is the files'
  icon on the Mac, Windows and Linux. The Mac and iOS declare them in
  Info.plist (`CFBundleDocumentTypes`, iOS opening documents in place),
  Android as intent filters, Windows registers them for the user as the
  app starts (a ProgID per type, the extensions' `OpenWithProgids`, the
  icons in the `.exe`), Linux in the desktop file's `MimeType` and
  `share/mime/packages/<id>.xml` (run `update-mime-database` and
  `update-desktop-database` after unpacking the `.tar.gz`; a package
  manager and Flatpak do it), the web in the manifest's
  `file_handlers` (an installed app, in Chromium).
- `singleInstance`, `true` by default: on Windows and Linux an app with
  an `id` runs once, a second launch handing its arguments to the
  running one (`app.OnSecondInstance`, its files to `ui.OnOpenFiles`)
  and quitting. `false` runs it as often as it's started. The Mac runs
  an app once either way.

Platform by platform:

- `ios.*` is the iOS build's ([iOS](#ios)); `ios.team` is the Mac
  build's team too, and `ios.usage`'s purpose strings go in the Mac's
  Info.plist as well.
- `android.*` is the Android build's ([Android](#android)).
- `mac.profile` is a Mac build's provisioning profile, and
  `mac.appStoreProfile` the Mac App Store's: see [macOS](#macos).
- `linux.*` is what a Linux build's desktop file and AppStream metainfo
  say: see [Linux](#linux).
- `web.vapidPublicKey` is the app server's VAPID public key, for
  `asuka/push` on the web (Web Push; see
  [the web](web.md#asukapush-web-push)).
- A Windows build needs nothing beyond the keys above; its signing
  certificate is given in the environment ([Windows](#windows)).

These are for the phones and tablets:

- `orientations`: `portrait`, `landscape` (both by default), on phones.
  On iPad the app takes every orientation, as Split View, Slide Over
  and Stage Manager need; Android 16 likewise ignores it on tablets and
  foldables.
- `ios.iphoneOnly` leaves iPad out. By default the app is an iPad app
  too, at whatever size the user gives its window; on Android it's
  resizable for split screen and desktop windows. Lay out from
  `ui.UseEnvironment()`'s `Width` and `Height`, the window's size, not
  the device's.
- `shareTarget` offers the app in other apps' share sheets: `true` for
  texts and links, or `{"types": ["text", "image/*"], "max": 10}` for
  files too (see [Device](device.md#shares-from-other-apps-uionshare));
  `ui.OnShare` gets them. On iOS it's a share extension in the app, with
  a bundle ID of its own (the app's plus `.share`).
- `splash` is the launch screen: `{"image": "logo.png", "size": 96,
  "background": "#ffffff", "darkBackground": "#000000"}` (see
  [Device](device.md#the-launch-screen-asukasplash)).
- `widgets` are the app's home screen widgets and `shortcuts` its
  actions for Siri, Spotlight and the launcher (see
  [Device](device.md#home-screen-widgets-asukawidget)). The widget
  extension has a bundle ID of its own (the app's plus `.widgets`): a
  release needs a profile for it too, and `ios.widgetProfile` names one.
- `android.googleServices` is the Firebase project's
  `google-services.json`, for `asuka/push`; `android.mapsKey`, a Google
  Maps Platform API key, for `asuka/maps`.
- `ios.usage` gives the purpose strings of protected resources
  (`NSCameraUsageDescription`...; the Mac's too), and `ios.infoPlist` adds keys to
  Info.plist; `ios.backgroundModes` adds to `UIBackgroundModes` (`"audio"`,
  to record and play with the screen locked); `android.permissions` adds
  permissions.

The packages an app imports give it the permissions and purpose strings
they need: `asuka/camera` the camera's, `asuka/location` the location's.
Write your own in `ios.usage`: the default is generic. Some need a
capability on the App ID too (Push Notifications for `asuka/push`, Sign
in with Apple for `asuka/appleauth`, App Attest for `asuka/integrity`,
Declared Age Range for `asuka/agerange`): turn it on at
developer.apple.com, then make the profile again. `asuka build --platform ios` (and
`--platform mac`) says which a profile lacks.

## iOS

```sh
asuka build --platform ios
```

makes a `.ipa` signed for the App Store, with the team's distribution
certificate (in your keychain, from Xcode's Accounts) and a provisioning
profile for the app's ID: `ios.profile`, or `--profile`, else the best
match installed. `--distribution ad-hoc` or `development` signs it for the
devices of its profile instead, and `--simulator` makes an unsigned `.app`
for the simulator.

With `shareTarget`, the share extension is signed too, with a profile
for its own ID (`com.example.notes.share` for `com.example.notes`), from
the app's team: `ios.shareProfile`, else the best match installed. A
wildcard profile (`com.example.*`) serves both. Register the extension's
ID at developer.apple.com and make it a profile as you did the app's.

A `shareTarget` that takes files also needs the App Group
`group.<app ID>`, through which the extension hands the files to the
app. At developer.apple.com, register the group, give both IDs (the
app's and the extension's) the App Groups capability with it, then make
their profiles again. A wildcard ID can't have App Groups. The build
stops, saying so, if a profile lacks the group. The simulator needs
none of this. An app with its own share sheet (`app.ShareSheet`) needs
the group too, for its data, whatever it takes.

`asuka publish --platform ios` makes it and uploads it to App Store Connect (see
[Publishing](#publishing)); Apple's Transporter uploads one too.

**Privacy manifest.** The App Store requires one, giving a reason for
each API of Apple's list the app uses. Asuka writes it, from what the
binary uses: Go's runtime reads the clock and file times, SQLite the disk
space; it declares no tracking and no data collected. If your app
collects data or tracks, write your own as `ios/PrivacyInfo.xcprivacy`,
with those reasons in it too; Asuka uses it instead.

## Android

A release is signed with your upload key. Make one, once, and keep it
safe:

```sh
keytool -genkeypair -v -keystore upload.jks -alias upload -keyalg RSA -keysize 2048 -validity 10000
```

Name it in `asuka.json` (`android.keystore`, `android.keyAlias`), and give
its password in the environment:

```sh
ASUKA_KEYSTORE_PASSWORD=... asuka build --platform android
```

(`ASUKA_KEY_PASSWORD` too if the key's differs; `ASUKA_KEYSTORE` and
`ASUKA_KEY_ALIAS` name them in CI.) It makes an `.aab` for Google Play, for
64-bit and 32-bit ARM phones and x86_64 (emulators, Chromebooks),
Android 13 and later; Play makes each phone's APK from it, with only
that phone's architecture. `asuka publish --platform android` makes it and uploads it to Google Play (see
[Publishing](#publishing)).

## Publishing

`asuka publish` makes the release build, as `asuka build` does, and
uploads it through the store's API. It asks the store first what could
stop it (that the app is there and the key may upload to it, that the
version still takes builds, that the TestFlight groups exist), so
nothing is built for an upload that would fail. Each build is numbered
one above the store's highest (and at least `asuka.json`'s `build`):
there's nothing to bump between releases. `--build 42` gives one.

**App Store Connect.** Make an API key in App Store Connect (Users and
Access › Integrations › App Store Connect API) with the App Manager
role, download its `.p8`, and name it in `asuka.json`, or in the
environment (`ASUKA_ASC_KEY_ID`, `ASUKA_ASC_ISSUER_ID`, and
`ASUKA_ASC_KEY`, the `.p8`'s path or its text). Without a `key`, it's
looked for as `altool` does, `AuthKey_<id>.p8` in
`~/.appstoreconnect/private_keys`. Keep the `.p8` out of your
repository. Add the app in App Store Connect first (Apps › + › New
App), with its bundle ID.

```json
"ios": {
  "appStoreConnect": { "keyId": "ABC123DEF4", "issuerId": "57246542-96fe-1a63-e053-0824d011072a" }
}
```

```sh
asuka publish --platform ios                                  # to TestFlight: the team's testers
asuka publish --platform ios --groups "Friends, QA" --notes "Try the new sync."
asuka publish --platform ios --review --notes @release-notes/ios --release manual
```

It uploads the `.ipa`, waits for Apple to process it (minutes, usually;
at times an hour or more), printing what Apple warns of or turns down,
then:

- `--notes` is TestFlight's What to Test and, with `--review`, the App
  Store's What's New: text, or `@file`, in the app's primary language;
  or `@dir`, a file per language, named as App Store Connect names it
  (`en-US.txt`, `ja.txt`). An update needs What's New in each of the
  version's languages.
- `--groups` gives the build to TestFlight groups, by name. With an
  external group (testers outside your team), the build goes to beta
  review; fill in the test information in TestFlight first.
- `--review` makes the App Store version (`asuka.json`'s `version`) if
  there isn't one, gives it the build, and submits it for review. A
  first submission needs its listing, screenshots and privacy answers
  filled in App Store Connect first; what's missing is printed.
  `--release auto` releases it once approved, `--release manual` waits for
  you.
- `--no-wait` stops once Apple has the upload.

**Google Play.** Make a service account in Google Cloud, with a JSON key
(IAM › Service accounts › Keys), in a project with the Google Play
Android Developer API turned on. Invite its email in the Play Console
(Users and permissions), with the rights to release the app. Name its
key in `asuka.json` (`"android": {"serviceAccount": "play.json"}`) or in
`ASUKA_PLAY_SERVICE_ACCOUNT` (its path or its JSON). Play takes an app's
first `.aab` only in the Play Console: create the app there and upload
its first build by hand; `asuka publish` uploads the ones after.

```sh
asuka publish --platform android                              # the internal test track
asuka publish --platform android --track beta --notes @release-notes/android
asuka publish --platform android --track production --rollout 0.1
```

- `--track` is `internal` (default), `alpha` (closed testing), `beta`
  (open testing), `production`, or a closed track's name.
- `--rollout 0.1` rolls the release out to a tenth of users; raise it in
  the Play Console. `--draft` makes a draft, to roll out there. Until the
  app is published, Play takes drafts only: the release becomes one.
- `--notes` are the release notes (at most 500 characters): text or
  `@file` in the listing's default language, or `@dir` with a file per
  language as Play names it (`en-US.txt`, `ja-JP.txt`).
- If the app has changes in review, it stops; `--cancel-review` cancels
  that review and sends them with this release. If Play holds the app's
  changes for you to send (after a rejection), they're saved, and you
  send them in the Play Console.

`--file dist/Notes.ipa` (or `.aab`) uploads a build `asuka build` made,
instead of making one.

## macOS

```sh
asuka build --platform mac
```

makes `dist/<Name>.app` on a Mac: a universal app (Apple silicon and
Intel), for macOS 11 and later, with its Info.plist, icon, languages and
assets, signed ad hoc: it runs on your Mac. The packages the app imports
give it their purpose strings, as on iOS, from `ios.usage` (location's
is `NSLocationUsageDescription` on the Mac, taken from
`NSLocationWhenInUseUsageDescription`), and the hardened runtime's
entitlements for them (`com.apple.security.device.camera`,
`...audio-input`, `...personal-information.location`...), without which
an app signed with a Developer ID is refused the camera, the microphone
or the user's data. An app with Swift of its own (`macos/*.swift`) has it
compiled for both architectures into `Contents/Frameworks`, signed with
the app (see [Native code](native.md)).

To give it to others outside the Mac App Store, sign it with your
Developer ID and have Apple notarize it, so Gatekeeper opens it:

```sh
xcrun notarytool store-credentials asuka   # once: your Apple ID, team and an app-specific password
asuka build --platform mac --sign "Developer ID Application: Your Name (TEAMID)" --notarize asuka
```

`--notarize` sends the signed app to Apple's notary service, waits for
its verdict (a few minutes, usually), prints the service's log if it's
rejected, and staples the ticket to the app, so it opens offline too.
In CI, `--notarize env` takes `ASUKA_APPLE_ID`, `ASUKA_APPLE_PASSWORD` (an
app-specific password) and `ASUKA_TEAM_ID` from the environment instead
of a keychain profile.

Push (`asuka/push`), Sign in with Apple (`asuka/appleauth`) and App Attest
(`asuka/integrity`) need entitlements only a provisioning profile grants.
Make a macOS Developer ID profile for the app's ID at developer.apple.com
(Profiles), with those capabilities on for the ID, and download it:

```sh
asuka build --platform mac --profile Notes.provisionprofile --notarize asuka
```

The profile goes in the app as `Contents/embedded.provisionprofile`, and
the app is signed with what its packages need of the profile's
entitlements (`com.apple.developer.aps-environment`,
`com.apple.developer.applesignin`,
`com.apple.developer.devicecheck.appattest-environment`), its app ID and
team, and the hardened runtime. The build says which a profile lacks.
Without `--sign`, the identity is the keychain's certificate the profile
allows (Developer ID Application); a `--sign` the profile doesn't list is
an error. `--profile` takes a path, or an installed profile's name or UUID;
`ASUKA_MAC_PROFILE` and asuka.json's `mac.profile` say the same. With none,
an app that uses those packages is signed with the best match installed
for its ID (in Xcode's profile folders), else without one: the build
says what won't work on the Mac, and the rest of the app does.

`--appstore` makes `dist/<Name>.pkg` for the Mac App Store instead: the app in the
App Sandbox (with the network and the files the user picks), signed
with a Mac App Store profile's certificate (Apple Distribution;
asuka.json's `mac.appStoreProfile`, else `--profile`, else the best match),
and the package with the team's Mac Installer Distribution certificate.
Upload it with Transporter or `xcrun altool --upload-package`;
`asuka publish` doesn't upload Mac builds yet. Notarizing is for apps
outside the store: `--notarize` with `--appstore` is an error.

## Windows

```sh
asuka build --platform windows               # x64, which Windows on Arm runs emulated
asuka build --platform windows --arch arm64  # native on Arm PCs
```

makes a folder, `dist/<Name>`, from any system: `<Name>.exe` and the
app's assets beside it. The `.exe` is a GUI program (no console window)
with its resources built in: the icon at Windows's sizes, its version
information (`version` and `build`), and a manifest that asks for the
common controls 6, per-monitor DPI awareness and UTF-8, for Windows 10
and 11. An app with a `.syso` of its own in its directory has its own
resources instead. Zip the folder, or package it (an installer, MSIX).

Sign the `.exe`, so SmartScreen knows who it's from, with a code signing
certificate exported as a `.pfx`:

```sh
ASUKA_WINDOWS_CERT=cert.pfx ASUKA_WINDOWS_CERT_PASSWORD=... asuka build --platform windows
```

The build signs it with SHA-256 and has the signature timestamped, so
it stays valid once the certificate expires, by the first RFC 3161 server
that answers: `ASUKA_WINDOWS_TIMESTAMP`'s (a comma-separated list), else
DigiCert's, Sectigo's and GlobalSign's. When none answers the build
fails, saying so; `ASUKA_WINDOWS_TIMESTAMP=none` signs without a timestamp
(the signature then ends with the certificate). It signs with `signtool` on
Windows (the Windows SDK's; on the `PATH`, else the newest installed),
with `osslsigncode` elsewhere (`brew install osslsigncode`, `apt install
osslsigncode`). Without `ASUKA_WINDOWS_CERT` the `.exe` isn't signed. A
certificate on a hardware token or in a cloud service (Azure Trusted
Signing) signs through its own tool: sign the `.exe` in the folder with
it after the build.

An app that shows notifications (`asuka/notify`) is given a shortcut in the
user's Start menu the first time, which Windows needs for an app's
toasts: its AppUserModelID is asuka.json's `id` (else one of its name).

## Linux

```sh
asuka build --platform linux                # this machine's architecture, else amd64
asuka build --platform linux --arch arm64   # amd64 or arm64
```

It builds from any system: the host calls GTK 4 and libadwaita without
cgo. In `dist/linux` it makes:

- `<name>/`, a folder laid out as an install prefix, named after the
  app in lower case (`My Notes`: `my-notes`):
  - `bin/my-notes`, the app;
  - `share/my-notes/assets`, its assets, which it finds from
    `bin/`;
  - `share/applications/<id>.desktop`, what launchers show and run;
  - `share/icons/hicolor/<size>/apps/<id>.png`, the `icon` at each size
    from 16 to 512 pixels;
  - `share/metainfo/<id>.metainfo.xml`, its AppStream metainfo, what
    software centers show.

  Run `bin/my-notes` from it, or install it into a prefix, where
  launchers find it:

  ```sh
  tar -xzf my-notes-1.0-linux-amd64.tar.gz --strip-components=1 -C ~/.local
  ```

- `my-notes-<version>-linux-<arch>.tar.gz`, that folder, to hand out.
- `<id>.yml`, a [Flatpak](https://flatpak.org) manifest that packages
  that folder on the GNOME runtime:

  ```sh
  flatpak install flathub org.gnome.Platform//50 org.gnome.Sdk//50   # once
  flatpak-builder --user --install --force-clean build dist/linux/com.example.notes.yml
  flatpak run com.example.notes
  ```

The app needs GTK 4.14 and libadwaita 1.5 or later where it runs (the
GNOME runtime 46 or later has them). Its ID is its application ID: it
names its desktop file, its icon and its D-Bus name (a second launch
shows the first's window), and its data directory
(`~/.local/share/<id>`); it must be letters, digits, `_` and `-`, in two
parts or more, none starting with a digit.

What the desktop file and metainfo say, in `asuka.json`:

```json
"linux": {
  "summary": "Write things down",
  "description": "Notes, kept on your computer.\n\nA second paragraph.",
  "categories": ["Office"],
  "keywords": ["memo"],
  "license": "MIT",
  "developer": "Example Inc.",
  "homepage": "https://example.com",
  "flatpakRuntime": "50"
}
```

`categories` are [freedesktop.org's](https://specifications.freedesktop.org/menu-spec/latest/category-registry.html)
(`Utility` by default); `license` is an SPDX expression; Flathub and
software centers want the summary, description, license and developer.

## The web

```sh
asuka build --platform web
```

makes a static site in `dist/web`: `index.html`, the app as
WebAssembly, the web host's scripts, the app's service worker, its
assets, a web app manifest with its icons, gzipped copies, and a
`_headers` file for Cloudflare and Netlify. The files are named by their
digests, so all but `index.html` and the manifest can be cached for
good. Serve the folder from any web server or CDN, `.wasm` as
`application/wasm`, over HTTPS (service workers, Web Push and the secure
store need it). See [the web](web.md) for what's in it and what a
browser does differently.

## In an existing app

A Asuka app can live inside an app made in Xcode or Android Studio: a
feature written once in Go, shown from a native app. (On the web, the
build's app runs in an element of a page of yours: see
[the web](web.md).)

**iOS.** `asuka build --platform ios --xcframework` makes `dist/AsukaApp.xcframework`:
the app's Go code and the UIKit host, for devices and the simulator, with
a Swift module interface and the app's assets. Add it to your target
(Embed & Sign), then show the Asuka app as any view controller:

```swift
import AsukaApp

present(AsukaViewController(), animated: true)
// or push it, or make it a tab, or a window's root
```

**Android.** `asuka build --platform android --aar` makes a Maven repository in
`dist/maven`: the host library with the app's Go code for each ABI, its
assets, and `AsukaActivity` declared, with its dependencies (Material,
AndroidX) in its POM, and the optional modules the app uses (`asuka/push`,
`asuka/maps`) beside it. Add the repository and the dependency the
command prints, then start the activity:

```kotlin
// settings.gradle.kts
dependencyResolutionManagement { repositories { maven { url = uri("path/to/dist/maven") } } }
// app/build.gradle.kts
dependencies { implementation("dev.asuka.counter:counter:1.0.0") }
// anywhere
startActivity(Intent(this, dev.asuka.AsukaActivity::class.java))
```

The embedded app is one Asuka app, mounted once, as in a standalone
build. What `asuka.json` sets for a whole app (its icon, URL scheme,
share target, widgets, permissions' texts) is the embedding app's to
set in its own project.

## Symbols for crash reports

A native crash's frames are addresses in the app's binaries; a crash
service (or App Store Connect, or Google Play) turns them into
functions and lines with the build's symbols. Go's own stack traces
need none (their tables are in the binary), so a Go panic reads as it
is.

**iOS.** `asuka build --platform ios` compiles the host with debug information,
keeps the Go code's, and makes a dSYM of each binary (the app, its
framework when it has a share sheet, its extensions), as Xcode's archive
does, then strips the binaries. They're in `dist/<Name>.dSYMs`, beside
the `.ipa` (or the simulator's `.app`), and their symbols in the `.ipa`
(`Symbols/`), so App Store Connect and Xcode's Organizer symbolicate
the crashes they show. Upload the dSYMs to your crash service:

```sh
sentry-cli debug-files upload --org ORG --project PROJECT dist/Notes.dSYMs
# Crashlytics' tool, from its SDK:
upload-symbols -gsp GoogleService-Info.plist -p ios dist/Notes.dSYMs
```

**Android.** `asuka build --platform android` keeps the Go libraries' symbol tables
for Gradle, which strips the libraries it packages and puts their
symbols in the `.aab`, with R8's map of the Kotlin it renamed: Google
Play symbolicates native crashes and ANRs, and deobfuscates stack
traces, with them. For a crash service they're beside the `.aab` too:
`dist/<Name>-native-debug-symbols.zip` and `dist/<Name>-mapping.txt`.

```sh
sentry-cli debug-files upload --org ORG --project PROJECT dist/Notes-native-debug-symbols.zip
sentry-cli upload-proguard --org ORG --project PROJECT dist/Notes-mapping.txt
```

**The Mac, Windows, Linux and the web.** The app is one Go binary, built
without its symbol table and debug information (`-s -w`); Go's stack
traces keep their function names and lines, so its panics read as they
are, and there are no symbols to upload.

Keep each release's symbols: a crash is symbolicated with those of the
build it happened in. See [Device](device.md#crash-reports-asukacrash)
for the reports themselves.

## Before you release

Run the release build where your users will, before you release it:

- On a phone: an iOS build with `--distribution development`, and the
  Android build from Play's internal test track (`asuka publish
  --platform android --track internal`), which installs the APK Play makes
  from the `.aab`. TestFlight and a Play internal test also check what a
  simulator can't.
- On the Mac: the signed, notarized `.app`, downloaded on another Mac
  (a download is what Gatekeeper checks); `spctl --assess --type execute
  -vv Notes.app` says what Gatekeeper will make of it.
- On Windows and Linux: the folder, or the `.tar.gz` or Flatpak, on a
  machine that has never had the app (Linux: with GTK 4.14 and
  libadwaita 1.5 or later).
- On the web: the built site served over HTTPS, in each browser you
  support.

## In CI

This GitHub Actions workflow builds both apps when you push a tag like
`v1.2`, uploads the iOS build to TestFlight and the Android build to
Play's internal test track with `asuka publish`, and attaches the `.ipa`
and the `.aab` to a draft GitHub release for the tag, each as soon as
it's uploaded; publish
the draft when you're ready. (Run by hand, it keeps them as the run's
artifacts only.) It expects the app at the root of its repository. Do
your first uploads by hand: Play won't take one through its API until
the app has had one.

```yaml
# .github/workflows/release.yml
name: release
on:
  push:
    tags: ["v*"]
  workflow_dispatch:
permissions:
  contents: write # to attach the builds to the tag's release

jobs:
  release:
    if: startsWith(github.ref, 'refs/tags/')
    runs-on: ubuntu-latest
    steps:
      - run: gh release create "$GITHUB_REF_NAME" --draft --generate-notes --verify-tag
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}

  ios:
    needs: release
    if: ${{ !failure() && !cancelled() }} # and when run by hand, which skips release
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - name: the asuka CLI, at the app's version of asuka
        run: go install github.com/egoist/asuka/cmd/asuka@$(go list -m -f '{{.Version}}' github.com/egoist/asuka)
      - name: the distribution certificate and the profile
        env:
          CERTIFICATE: ${{ secrets.IOS_CERTIFICATE }}
          CERTIFICATE_PASSWORD: ${{ secrets.IOS_CERTIFICATE_PASSWORD }}
          PROFILE: ${{ secrets.IOS_PROFILE }}
        run: |
          KC=$RUNNER_TEMP/signing.keychain-db
          PW=$(openssl rand -hex 16)
          security create-keychain -p "$PW" "$KC"
          security set-keychain-settings -lut 3600 "$KC"
          security unlock-keychain -p "$PW" "$KC"
          echo "$CERTIFICATE" | base64 --decode > $RUNNER_TEMP/certificate.p12
          security import $RUNNER_TEMP/certificate.p12 -k "$KC" -P "$CERTIFICATE_PASSWORD" -T /usr/bin/codesign
          security set-key-partition-list -S apple-tool:,apple: -k "$PW" "$KC"
          security list-keychains -d user -s "$KC" $(security list-keychains -d user | tr -d '"')
          echo "$PROFILE" | base64 --decode > $RUNNER_TEMP/app.mobileprovision
          # With shareTarget, the extension's profile, installed where asuka looks:
          # mkdir -p ~/Library/MobileDevice/Provisioning\ Profiles
          # echo "$SHARE_PROFILE" | base64 --decode > ~/Library/MobileDevice/Provisioning\ Profiles/share.mobileprovision
      - name: build, and upload to TestFlight
        env:
          ASUKA_ASC_KEY_ID: ${{ secrets.ASC_KEY_ID }}
          ASUKA_ASC_ISSUER_ID: ${{ secrets.ASC_ISSUER_ID }}
          ASUKA_ASC_KEY: ${{ secrets.ASC_KEY }}
        run: asuka publish --platform ios --profile $RUNNER_TEMP/app.mobileprovision --no-wait
      - uses: actions/upload-artifact@v4
        with: { name: ios, path: dist/*.ipa }
      - name: attach to the tag's release
        if: startsWith(github.ref, 'refs/tags/')
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: gh release upload "$GITHUB_REF_NAME" dist/*.ipa --clobber

  android:
    needs: release
    if: ${{ !failure() && !cancelled() }}
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: actions/setup-java@v4
        with: { distribution: temurin, java-version: 17 }
      - uses: android-actions/setup-android@v3
        with: { packages: platform-tools }
      - name: an NDK
        run: sdkmanager "ndk;27.1.12297006"
      - name: the asuka CLI, at the app's version of asuka
        run: go install github.com/egoist/asuka/cmd/asuka@$(go list -m -f '{{.Version}}' github.com/egoist/asuka)
      - name: the upload key
        env:
          KEYSTORE: ${{ secrets.ANDROID_KEYSTORE }}
        run: echo "$KEYSTORE" | base64 --decode > $RUNNER_TEMP/upload.jks
      - name: build, and upload to Play's internal test track
        run: asuka publish --platform android --track internal
        env:
          ASUKA_KEYSTORE: ${{ runner.temp }}/upload.jks
          ASUKA_KEYSTORE_PASSWORD: ${{ secrets.ANDROID_KEYSTORE_PASSWORD }}
          ASUKA_PLAY_SERVICE_ACCOUNT: ${{ secrets.PLAY_SERVICE_ACCOUNT }}
      - uses: actions/upload-artifact@v4
        with:
          name: android
          path: dist/*.aab
      - name: attach to the tag's release
        if: startsWith(github.ref, 'refs/tags/')
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: gh release upload "$GITHUB_REF_NAME" dist/*.aab --clobber
```

Its secrets (Settings › Secrets and variables › Actions), the files in
base64 (`base64 -i file | pbcopy`):

- `IOS_CERTIFICATE`: your Apple Distribution certificate with its
  private key, a `.p12` exported from Keychain Access;
  `IOS_CERTIFICATE_PASSWORD` the password you exported it with.
- `IOS_PROFILE`: the app's App Store provisioning profile, from
  developer.apple.com (with `shareTarget`, `SHARE_PROFILE` the share
  extension's).
- `ASC_KEY_ID`, `ASC_ISSUER_ID` and `ASC_KEY` (the `.p8`'s text): an App
  Store Connect API key with the App Manager role, from Users and Access
  › Integrations.
- `ANDROID_KEYSTORE`: the upload key's `.jks`; `ANDROID_KEYSTORE_PASSWORD`
  its password (add `ASUKA_KEY_PASSWORD` if the key's differs).
- `PLAY_SERVICE_ACCOUNT`: a Google Cloud service account's JSON key,
  invited in the Play Console's Users and permissions with the right to
  release to testing tracks.

The CLI carries the iOS and Android hosts, so it's installed at the
version of Asuka in the app's `go.mod`. Each build is numbered after the
store's highest, so nothing is bumped before you tag; `--no-wait` ends
the iOS job once Apple has the upload, not once it's processed. The App Store takes builds
made with a recent Xcode only; if the runner's default is older, pick
one with `sudo xcode-select -s /Applications/Xcode_<version>.app`.

### The desktop

These jobs, added to the same workflow, build the desktop apps for the
tag and attach them to its draft release: the Mac's signed with your
Developer ID and notarized, Windows' for x64 and Arm, signed, and
Linux's for amd64 and arm64. Windows' and Linux's build on Linux, so
one Ubuntu runner makes both. (The app is named Notes here: its files
are named after `asuka.json`'s `name`.)

```yaml
  mac:
    needs: release
    runs-on: macos-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - name: the asuka CLI, at the app's version of asuka
        run: go install github.com/egoist/asuka/cmd/asuka@$(go list -m -f '{{.Version}}' github.com/egoist/asuka)
      - name: the Developer ID certificate
        env:
          CERTIFICATE: ${{ secrets.MAC_CERTIFICATE }}
          CERTIFICATE_PASSWORD: ${{ secrets.MAC_CERTIFICATE_PASSWORD }}
        run: |
          KC=$RUNNER_TEMP/signing.keychain-db
          PW=$(openssl rand -hex 16)
          security create-keychain -p "$PW" "$KC"
          security set-keychain-settings -lut 3600 "$KC"
          security unlock-keychain -p "$PW" "$KC"
          echo "$CERTIFICATE" | base64 --decode > $RUNNER_TEMP/certificate.p12
          security import $RUNNER_TEMP/certificate.p12 -k "$KC" -P "$CERTIFICATE_PASSWORD" -T /usr/bin/codesign
          security set-key-partition-list -S apple-tool:,apple: -k "$PW" "$KC"
          security list-keychains -d user -s "$KC" $(security list-keychains -d user | tr -d '"')
      - name: build, sign and notarize
        env:
          ASUKA_APPLE_ID: ${{ secrets.APPLE_ID }}
          ASUKA_APPLE_PASSWORD: ${{ secrets.APPLE_APP_PASSWORD }}
          ASUKA_TEAM_ID: ${{ secrets.APPLE_TEAM_ID }}
        run: asuka build --platform mac --sign "Developer ID Application: Your Name (TEAMID)" --notarize env
      - name: attach to the tag's release
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: |
          ditto -c -k --keepParent dist/Notes.app dist/Notes-mac.zip
          gh release upload "$GITHUB_REF_NAME" dist/Notes-mac.zip --clobber

  windows-linux:
    needs: release
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - name: the asuka CLI, at the app's version of asuka
        run: go install github.com/egoist/asuka/cmd/asuka@$(go list -m -f '{{.Version}}' github.com/egoist/asuka)
      - name: osslsigncode, and the code signing certificate
        env:
          CERTIFICATE: ${{ secrets.WINDOWS_CERTIFICATE }}
        run: |
          sudo apt-get install -y osslsigncode
          echo "$CERTIFICATE" | base64 --decode > $RUNNER_TEMP/cert.pfx
      - name: Windows, x64 and Arm
        env:
          ASUKA_WINDOWS_CERT: ${{ runner.temp }}/cert.pfx
          ASUKA_WINDOWS_CERT_PASSWORD: ${{ secrets.WINDOWS_CERTIFICATE_PASSWORD }}
        run: |
          asuka build --platform windows -o dist/x64
          asuka build --platform windows --arch arm64 -o dist/arm64
          (cd dist/x64 && zip -r ../Notes-windows-x64.zip Notes)
          (cd dist/arm64 && zip -r ../Notes-windows-arm64.zip Notes)
      - name: Linux, amd64 and arm64
        run: |
          asuka build --platform linux
          asuka build --platform linux --arch arm64
      - name: attach to the tag's release
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: gh release upload "$GITHUB_REF_NAME" dist/*.zip dist/linux/*.tar.gz --clobber
```

Their secrets: `MAC_CERTIFICATE`, your Developer ID Application
certificate with its private key (a `.p12`), and
`MAC_CERTIFICATE_PASSWORD`; `APPLE_ID`, `APPLE_APP_PASSWORD` (an
app-specific password, from account.apple.com) and `APPLE_TEAM_ID`, for
the notary service; `WINDOWS_CERTIFICATE`, a code signing certificate
(a `.pfx`), and `WINDOWS_CERTIFICATE_PASSWORD`. Each Linux build makes
its own `.tar.gz`; the Flatpak manifest packages the last one built, for
its architecture.

## Languages

An app with [catalogs](i18n.md) needs nothing more: `asuka build` declares its
languages to each store and system (`CFBundleLocalizations` and `.lproj`s on
iOS and macOS, a `locales_config` on Android), which makes Settings offer the
app's language. Windows, Linux and web builds declare none: the app
shows the language the user's settings pick from its catalogs. The app's name and its purpose strings can be translated too,
with keys `asuka.appName` and `asuka.NSCameraUsageDescription` in the catalogs
([Packaging](i18n.md#what-the-system-shows)).
