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

## In CI

This GitHub Actions workflow builds both apps when you push a tag like
`v1.2`, uploads the iOS build to TestFlight and the Android build to
Play's internal test track, and attaches the `.ipa` and the `.apk` to a
draft GitHub release for the tag, each as soon as it's built; publish
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
      - name: the nui CLI, at the app's version of nui
        run: go install github.com/egoist/nui/cmd/nui@$(go list -m -f '{{.Version}}' github.com/egoist/nui)
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
      - run: nui build -ios -profile $RUNNER_TEMP/app.mobileprovision
      - uses: actions/upload-artifact@v4
        with: { name: ios, path: dist/*.ipa }
      - name: attach to the tag's release
        if: startsWith(github.ref, 'refs/tags/')
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: gh release upload "$GITHUB_REF_NAME" dist/*.ipa --clobber
      - name: upload to TestFlight
        env:
          ASC_KEY_ID: ${{ secrets.ASC_KEY_ID }}
          ASC_ISSUER_ID: ${{ secrets.ASC_ISSUER_ID }}
          ASC_KEY: ${{ secrets.ASC_KEY }}
        run: |
          mkdir -p ~/.appstoreconnect/private_keys
          echo "$ASC_KEY" > ~/.appstoreconnect/private_keys/AuthKey_$ASC_KEY_ID.p8
          xcrun altool --upload-app -f dist/*.ipa -t ios --apiKey "$ASC_KEY_ID" --apiIssuer "$ASC_ISSUER_ID"

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
      - name: the nui CLI, at the app's version of nui
        run: go install github.com/egoist/nui/cmd/nui@$(go list -m -f '{{.Version}}' github.com/egoist/nui)
      - name: the upload key
        env:
          KEYSTORE: ${{ secrets.ANDROID_KEYSTORE }}
        run: echo "$KEYSTORE" | base64 --decode > $RUNNER_TEMP/upload.jks
      - run: nui build -android
        env:
          NUI_KEYSTORE: ${{ runner.temp }}/upload.jks
          NUI_KEYSTORE_PASSWORD: ${{ secrets.ANDROID_KEYSTORE_PASSWORD }}
      - uses: actions/upload-artifact@v4
        with:
          name: android
          path: |
            dist/*.aab
            dist/*.apk
      - name: attach to the tag's release
        if: startsWith(github.ref, 'refs/tags/')
        env:
          GH_TOKEN: ${{ github.token }}
          GH_REPO: ${{ github.repository }}
        run: gh release upload "$GITHUB_REF_NAME" dist/*.apk --clobber
      - uses: r0adkll/upload-google-play@v1
        with:
          serviceAccountJsonPlainText: ${{ secrets.PLAY_SERVICE_ACCOUNT }}
          packageName: com.example.notes
          releaseFiles: dist/*.aab
          track: internal
```

Its secrets (Settings › Secrets and variables › Actions), the files in
base64 (`base64 -i file | pbcopy`):

- `IOS_CERTIFICATE`: your Apple Distribution certificate with its
  private key, a `.p12` exported from Keychain Access;
  `IOS_CERTIFICATE_PASSWORD` the password you exported it with.
- `IOS_PROFILE`: the app's App Store provisioning profile, from
  developer.apple.com.
- `ASC_KEY_ID`, `ASC_ISSUER_ID` and `ASC_KEY` (the `.p8`'s text): an App
  Store Connect API key with the App Manager role, from Users and Access
  › Integrations.
- `ANDROID_KEYSTORE`: the upload key's `.jks`; `ANDROID_KEYSTORE_PASSWORD`
  its password (add `NUI_KEY_PASSWORD` if the key's differs).
- `PLAY_SERVICE_ACCOUNT`: a Google Cloud service account's JSON key,
  invited in the Play Console's Users and permissions with the right to
  release to testing tracks.

The CLI carries the iOS and Android hosts, so it's installed at the
version of nui in the app's `go.mod`. `build` in `nui.json` must still
grow with each upload: bump it before you tag. The App Store takes builds
made with a recent Xcode only; if the runner's default is older, pick
one with `sudo xcode-select -s /Applications/Xcode_<version>.app`.
