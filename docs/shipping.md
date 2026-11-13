# Shipping

`ryui build` makes the builds the stores take, from the app's directory
(or the one given), into its `dist`, and `ryui publish` uploads them:
to TestFlight and App Review, or a Google Play track (see
[Publishing](#publishing)). `ryui build -platform web` makes a static
site instead, to serve from any web server: see [the web](web.md#ryui-build--platform-web).

## ryui.json

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
  must grow with each. `ryui publish` numbers each build after the
  store's highest itself, so `build` is only where it starts.
- `icon` is a square PNG, 1024×1024, without transparency for iOS.
- `orientations`: `portrait`, `landscape` (both by default), on phones.
  On iPad the app takes every orientation, as Split View, Slide Over
  and Stage Manager need; Android 16 likewise ignores it on tablets and
  foldables.
- `ios.iphoneOnly` leaves iPad out. By default the app is an iPad app
  too, at whatever size the user gives its window; on Android it's
  resizable for split screen and desktop windows. Lay out from
  `ui.UseEnvironment()`'s `Width` and `Height`, the window's size, not
  the device's.
- `accent` is the app's accent color, `"#rrggbb"`: the tint on iOS, the
  seed of the Material 3 color scheme on Android, libadwaita's accent on
  Linux.
- `linux` is what a Linux build's desktop file and AppStream metainfo
  say: see [Linux](#linux).
- `mac.profile` is a Mac build's provisioning profile, and
  `mac.appStoreProfile` the Mac App Store's: see [macOS](#macos).
- `scheme` opens the app for `myapp://` URLs, and `links` for https links
  to your domains (universal links, App Links); `ui.OnOpenURL` gets them.
- `shareTarget` offers the app in other apps' share sheets: `true` for
  texts and links, or `{"types": ["text", "image/*"], "max": 10}` for
  files too (see [Device](device.md#shares-from-other-apps-uionshare));
  `ui.OnShare` gets them. On iOS it's a share extension in the app, with
  a bundle ID of its own (the app's plus `.share`).
- `splash` is the launch screen: `{"image": "logo.png", "size": 96,
  "background": "#ffffff", "darkBackground": "#000000"}` (see
  [Device](device.md#the-launch-screen-ryuisplash)).
- `widgets` are the app's home screen widgets and `shortcuts` its
  actions for Siri, Spotlight and the launcher (see
  [Device](device.md#home-screen-widgets-ryuiwidget)). The widget
  extension has a bundle ID of its own (the app's plus `.widgets`): a
  release needs a profile for it too, and `ios.widgetProfile` names one.
- `android.googleServices` is the Firebase project's
  `google-services.json`, for `ryui/push`; `android.mapsKey`, a Google
  Maps Platform API key, for `ryui/maps`.
- `web.vapidPublicKey` is the app server's VAPID public key, for
  `ryui/push` on the web (Web Push; see
  [the web](web.md#ryuipush-web-push)).
- `ios.usage` gives the purpose strings of protected resources
  (`NSCameraUsageDescription`...), and `ios.infoPlist` adds keys to
  Info.plist; `ios.backgroundModes` adds to `UIBackgroundModes` (`"audio"`,
  to record and play with the screen locked); `android.permissions` adds
  permissions.

The packages an app imports give it the permissions and purpose strings
they need: `ryui/camera` the camera's, `ryui/location` the location's.
Write your own in `ios.usage`: the default is generic. Some need a
capability on the App ID too (Push Notifications for `ryui/push`, Sign
in with Apple for `ryui/appleauth`, App Attest for `ryui/integrity`,
Declared Age Range for `ryui/agerange`): turn it on at
developer.apple.com, then make the profile again. `ryui build -platform ios` (and
`-platform mac`) says which a profile lacks.

## iOS

```sh
ryui build -platform ios
```

makes a `.ipa` signed for the App Store, with the team's distribution
certificate (in your keychain, from Xcode's Accounts) and a provisioning
profile for the app's ID: `ios.profile`, or `-profile`, else the best
match installed. `-distribution ad-hoc` or `development` signs it for the
devices of its profile instead, and `-simulator` makes an unsigned `.app`
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

`ryui publish -platform ios` makes it and uploads it to App Store Connect (see
[Publishing](#publishing)); Apple's Transporter uploads one too.

**Privacy manifest.** The App Store requires one, giving a reason for
each API of Apple's list the app uses. RyUI writes it, from what the
binary uses: Go's runtime reads the clock and file times, SQLite the disk
space; it declares no tracking and no data collected. If your app
collects data or tracks, write your own as `ios/PrivacyInfo.xcprivacy`,
with those reasons in it too; RyUI uses it instead.

## Android

A release is signed with your upload key. Make one, once, and keep it
safe:

```sh
keytool -genkeypair -v -keystore upload.jks -alias upload -keyalg RSA -keysize 2048 -validity 10000
```

Name it in `ryui.json` (`android.keystore`, `android.keyAlias`), and give
its password in the environment:

```sh
RYUI_KEYSTORE_PASSWORD=... ryui build -platform android
```

(`RYUI_KEY_PASSWORD` too if the key's differs; `RYUI_KEYSTORE` and
`RYUI_KEY_ALIAS` name them in CI.) It makes an `.aab` for Google Play, for
64-bit and 32-bit ARM phones and x86_64 (emulators, Chromebooks),
Android 13 and later; Play makes each phone's APK from it, with only
that phone's architecture. `ryui publish -platform android` makes it and uploads it to Google Play (see
[Publishing](#publishing)).

## Publishing

`ryui publish` makes the release build, as `ryui build` does, and
uploads it through the store's API. It asks the store first what could
stop it (that the app is there and the key may upload to it, that the
version still takes builds, that the TestFlight groups exist), so
nothing is built for an upload that would fail. Each build is numbered
one above the store's highest (and at least `ryui.json`'s `build`):
there's nothing to bump between releases. `-build 42` gives one.

**App Store Connect.** Make an API key in App Store Connect (Users and
Access › Integrations › App Store Connect API) with the App Manager
role, download its `.p8`, and name it in `ryui.json`, or in the
environment (`RYUI_ASC_KEY_ID`, `RYUI_ASC_ISSUER_ID`, and
`RYUI_ASC_KEY`, the `.p8`'s path or its text). Without a `key`, it's
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
ryui publish -platform ios                                  # to TestFlight: the team's testers
ryui publish -platform ios -groups "Friends, QA" -notes "Try the new sync."
ryui publish -platform ios -review -notes @release-notes/ios -release manual
```

It uploads the `.ipa`, waits for Apple to process it (minutes, usually;
at times an hour or more), printing what Apple warns of or turns down,
then:

- `-notes` is TestFlight's What to Test and, with `-review`, the App
  Store's What's New: text, or `@file`, in the app's primary language;
  or `@dir`, a file per language, named as App Store Connect names it
  (`en-US.txt`, `ja.txt`). An update needs What's New in each of the
  version's languages.
- `-groups` gives the build to TestFlight groups, by name. With an
  external group (testers outside your team), the build goes to beta
  review; fill in the test information in TestFlight first.
- `-review` makes the App Store version (`ryui.json`'s `version`) if
  there isn't one, gives it the build, and submits it for review. A
  first submission needs its listing, screenshots and privacy answers
  filled in App Store Connect first; what's missing is printed.
  `-release auto` releases it once approved, `-release manual` waits for
  you.
- `-no-wait` stops once Apple has the upload.

**Google Play.** Make a service account in Google Cloud, with a JSON key
(IAM › Service accounts › Keys), in a project with the Google Play
Android Developer API turned on. Invite its email in the Play Console
(Users and permissions), with the rights to release the app. Name its
key in `ryui.json` (`"android": {"serviceAccount": "play.json"}`) or in
`RYUI_PLAY_SERVICE_ACCOUNT` (its path or its JSON). Play takes an app's
first `.aab` only in the Play Console: create the app there and upload
its first build by hand; `ryui publish` uploads the ones after.

```sh
ryui publish -platform android                              # the internal test track
ryui publish -platform android -track beta -notes @release-notes/android
ryui publish -platform android -track production -rollout 0.1
```

- `-track` is `internal` (default), `alpha` (closed testing), `beta`
  (open testing), `production`, or a closed track's name.
- `-rollout 0.1` rolls the release out to a tenth of users; raise it in
  the Play Console. `-draft` makes a draft, to roll out there. Until the
  app is published, Play takes drafts only: the release becomes one.
- `-notes` are the release notes (at most 500 characters): text or
  `@file` in the listing's default language, or `@dir` with a file per
  language as Play names it (`en-US.txt`, `ja-JP.txt`).
- If the app has changes in review, it stops; `-cancel-review` cancels
  that review and sends them with this release. If Play holds the app's
  changes for you to send (after a rejection), they're saved, and you
  send them in the Play Console.

`-file dist/Notes.ipa` (or `.aab`) uploads a build `ryui build` made,
instead of making one.

## macOS

`ryui build -platform mac` makes a universal `.app` (Apple silicon and Intel),
signed ad hoc: it runs on your Mac. To give it to others outside the
Mac App Store, sign it with your Developer ID and have Apple notarize
it, so Gatekeeper opens it:

```sh
xcrun notarytool store-credentials ryui   # once: your Apple ID, team and an app-specific password
ryui build -platform mac -sign "Developer ID Application: Your Name (TEAMID)" -notarize ryui
```

`-notarize` sends the signed app to Apple's notary service, waits for
its verdict (a few minutes, usually), prints the service's log if it's
rejected, and staples the ticket to the app, so it opens offline too.
In CI, `-notarize env` takes `RYUI_APPLE_ID`, `RYUI_APPLE_PASSWORD` (an
app-specific password) and `RYUI_TEAM_ID` from the environment instead
of a keychain profile.

Push (`ryui/push`), Sign in with Apple (`ryui/appleauth`) and App Attest
(`ryui/integrity`) need entitlements only a provisioning profile grants.
Make a macOS Developer ID profile for the app's ID at developer.apple.com
(Profiles), with those capabilities on for the ID, and download it:

```sh
ryui build -platform mac -profile Notes.provisionprofile -notarize ryui
```

The profile goes in the app as `Contents/embedded.provisionprofile`, and
the app is signed with what its packages need of the profile's
entitlements (`com.apple.developer.aps-environment`,
`com.apple.developer.applesignin`,
`com.apple.developer.devicecheck.appattest-environment`), its app ID and
team, and the hardened runtime. The build says which a profile lacks.
Without `-sign`, the identity is the keychain's certificate the profile
allows (Developer ID Application); a `-sign` the profile doesn't list is
an error. `-profile` takes a path, or an installed profile's name or UUID;
`RYUI_MAC_PROFILE` and ryui.json's `mac.profile` say the same. With none,
an app that uses those packages is signed with the best match installed
for its ID (in Xcode's profile folders), else without one: the build
says what won't work on the Mac, and the rest of the app does.

`-appstore` makes a `.pkg` for the Mac App Store instead: the app in the
App Sandbox (with the network and the files the user picks), signed
with a Mac App Store profile's certificate (Apple Distribution;
ryui.json's `mac.appStoreProfile`, else `-profile`, else the best match),
and the package with the team's Mac Installer Distribution certificate.
Upload it with Transporter or `xcrun altool --upload-package`.

## Windows

`ryui build -platform windows` makes a folder, from any system: the
`.exe` (its icon, manifest and version information built in) and the
app's assets beside it. It's for x64 PCs, which Windows on Arm runs
emulated; `-arch arm64` makes it native for Arm PCs instead. Zip the
folder, or package it (an installer, MSIX).

Sign the `.exe`, so SmartScreen knows who it's from, with a code signing
certificate exported as a `.pfx`:

```sh
RYUI_WINDOWS_CERT=cert.pfx RYUI_WINDOWS_CERT_PASSWORD=... ryui build -platform windows
```

The build signs it with SHA-256 and has the signature timestamped, so
it stays valid once the certificate expires, by the first RFC 3161 server
that answers: `RYUI_WINDOWS_TIMESTAMP`'s (a comma-separated list), else
DigiCert's, Sectigo's and GlobalSign's. When none answers the build
fails, saying so; `RYUI_WINDOWS_TIMESTAMP=none` signs without a timestamp
(the signature then ends with the certificate). It signs with `signtool` on
Windows (the Windows SDK's; on the `PATH`, else the newest installed),
with `osslsigncode` elsewhere (`brew install osslsigncode`, `apt install
osslsigncode`). Without `RYUI_WINDOWS_CERT` the `.exe` isn't signed. A
certificate on a hardware token or in a cloud service (Azure Trusted
Signing) signs through its own tool: sign the `.exe` in the folder with
it after the build.

An app that shows notifications (`ryui/notify`) is given a shortcut in the
user's Start menu the first time, which Windows needs for an app's
toasts: its AppUserModelID is ryui.json's `id` (else one of its name).

## Linux

```sh
ryui build -platform linux                # this machine's architecture, else amd64
ryui build -platform linux -arch arm64    # amd64 or arm64
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

What the desktop file and metainfo say, in `ryui.json`:

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

## In an existing app

A RyUI app can live inside an app made in Xcode or Android Studio: a
feature written once in Go, shown from a native app.

**iOS.** `ryui build -platform ios -xcframework` makes `dist/RyUIApp.xcframework`:
the app's Go code and the UIKit host, for devices and the simulator, with
a Swift module interface and the app's assets. Add it to your target
(Embed & Sign), then show the RyUI app as any view controller:

```swift
import RyUIApp

present(RyUIViewController(), animated: true)
// or push it, or make it a tab, or a window's root
```

**Android.** `ryui build -platform android -aar` makes a Maven repository in
`dist/maven`: the host library with the app's Go code for each ABI, its
assets, and `RyUIActivity` declared, with its dependencies (Material,
AndroidX) in its POM, and the optional modules the app uses (`ryui/push`,
`ryui/maps`) beside it. Add the repository and the dependency the
command prints, then start the activity:

```kotlin
// settings.gradle.kts
dependencyResolutionManagement { repositories { maven { url = uri("path/to/dist/maven") } } }
// app/build.gradle.kts
dependencies { implementation("dev.ryui.counter:counter:1.0.0") }
// anywhere
startActivity(Intent(this, dev.ryui.RyUIActivity::class.java))
```

The embedded app is one RyUI app, mounted once, as in a standalone
build. What `ryui.json` sets for a whole app (its icon, URL scheme,
share target, widgets, permissions' texts) is the embedding app's to
set in its own project.

## Symbols for crash reports

A native crash's frames are addresses in the app's binaries; a crash
service (or App Store Connect, or Google Play) turns them into
functions and lines with the build's symbols. Go's own stack traces
need none (their tables are in the binary), so a Go panic reads as it
is.

**iOS.** `ryui build -platform ios` compiles the host with debug information,
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

**Android.** `ryui build -platform android` keeps the Go libraries' symbol tables
for Gradle, which strips the libraries it packages and puts their
symbols in the `.aab`, with R8's map of the Kotlin it renamed: Google
Play symbolicates native crashes and ANRs, and deobfuscates stack
traces, with them. For a crash service they're beside the `.aab` too:
`dist/<Name>-native-debug-symbols.zip` and `dist/<Name>-mapping.txt`.

```sh
sentry-cli debug-files upload --org ORG --project PROJECT dist/Notes-native-debug-symbols.zip
sentry-cli upload-proguard --org ORG --project PROJECT dist/Notes-mapping.txt
```

Keep each release's symbols: a crash is symbolicated with those of the
build it happened in. See [Device](device.md#crash-reports-ryuicrash)
for the reports themselves.

## What the stores check

Run the release build on a phone before you release it: an iOS build
with `-distribution development`, and the Android build from Play's
internal test track (`ryui publish -platform android -track internal`),
which installs the APK Play makes from the `.aab`. TestFlight and a Play
internal test also check what a simulator can't.

## In CI

This GitHub Actions workflow builds both apps when you push a tag like
`v1.2`, uploads the iOS build to TestFlight and the Android build to
Play's internal test track with `ryui publish`, and attaches the `.ipa`
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
      - name: the ryui CLI, at the app's version of ryui
        run: go install github.com/egoist/ryui/cmd/ryui@$(go list -m -f '{{.Version}}' github.com/egoist/ryui)
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
          # With shareTarget, the extension's profile, installed where ryui looks:
          # mkdir -p ~/Library/MobileDevice/Provisioning\ Profiles
          # echo "$SHARE_PROFILE" | base64 --decode > ~/Library/MobileDevice/Provisioning\ Profiles/share.mobileprovision
      - name: build, and upload to TestFlight
        env:
          RYUI_ASC_KEY_ID: ${{ secrets.ASC_KEY_ID }}
          RYUI_ASC_ISSUER_ID: ${{ secrets.ASC_ISSUER_ID }}
          RYUI_ASC_KEY: ${{ secrets.ASC_KEY }}
        run: ryui publish -platform ios -profile $RUNNER_TEMP/app.mobileprovision -no-wait
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
      - name: the ryui CLI, at the app's version of ryui
        run: go install github.com/egoist/ryui/cmd/ryui@$(go list -m -f '{{.Version}}' github.com/egoist/ryui)
      - name: the upload key
        env:
          KEYSTORE: ${{ secrets.ANDROID_KEYSTORE }}
        run: echo "$KEYSTORE" | base64 --decode > $RUNNER_TEMP/upload.jks
      - name: build, and upload to Play's internal test track
        run: ryui publish -platform android -track internal
        env:
          RYUI_KEYSTORE: ${{ runner.temp }}/upload.jks
          RYUI_KEYSTORE_PASSWORD: ${{ secrets.ANDROID_KEYSTORE_PASSWORD }}
          RYUI_PLAY_SERVICE_ACCOUNT: ${{ secrets.PLAY_SERVICE_ACCOUNT }}
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
  its password (add `RYUI_KEY_PASSWORD` if the key's differs).
- `PLAY_SERVICE_ACCOUNT`: a Google Cloud service account's JSON key,
  invited in the Play Console's Users and permissions with the right to
  release to testing tracks.

The CLI carries the iOS and Android hosts, so it's installed at the
version of RyUI in the app's `go.mod`. Each build is numbered after the
store's highest, so nothing is bumped before you tag; `-no-wait` ends
the iOS job once Apple has the upload, not once it's processed. The App Store takes builds
made with a recent Xcode only; if the runner's default is older, pick
one with `sudo xcode-select -s /Applications/Xcode_<version>.app`.

## Languages

An app with [catalogs](i18n.md) needs nothing more: `ryui build` declares its
languages to each store and system (`CFBundleLocalizations` and `.lproj`s on
iOS and macOS, a `locales_config` on Android), which makes Settings offer the
app's language. The app's name and its purpose strings can be translated too,
with keys `ryui.appName` and `ryui.NSCameraUsageDescription` in the catalogs
([Packaging](i18n.md#what-the-system-shows)).
