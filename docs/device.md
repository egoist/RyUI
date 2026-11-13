# Device and services

Asuka's packages reach what the device has beyond views: the camera, codes,
what's in an image, photos and files, previewing, printing and PDFs, the
mail and message composers, location, notifications and push, sound and recording,
the network's state, the shared library, work in the background, shares
from other apps, JavaScript, sign-in and biometrics, the screen, maps,
the stores' services, and crash reports.
This page covers each package: what it does, its API, the permissions it
brings and how it behaves on each platform.

A package has one API everywhere. iOS and Android have nearly all of
it. On macOS, Windows and Linux the host answers each package with the
desktop's own services, and in a browser with the web's APIs. Where a
platform has nothing to offer, the package answers as on a phone
without it: `ErrNotAvailable`, an error that names what's missing, or a
call that does nothing. Each package's section says what it does and
what each platform does it with. Each desktop and the browser also has a
section of its own ([On a Mac](#on-a-mac), [On Windows](#on-windows),
[On Linux](#on-linux), [In a browser](#in-a-browser)): what an app needs
set up there, and a table of where it differs from iOS and Android.

## Permissions and purpose strings

An app that imports a package is given what the package needs: Android
permissions in the manifest, iOS purpose strings in Info.plist, and the
platform code itself. An app that doesn't import it goes without, so a
photo app doesn't ask for the microphone. A Mac app is given the same
purpose strings and the entitlements they need; Windows, Linux and the
web need nothing declared (see each one's section below).

| Package | Android | iOS |
| --- | --- | --- |
| `asuka/camera` | `CAMERA` | `NSCameraUsageDescription` |
| `asuka/camera/record` | `RECORD_AUDIO` | `NSMicrophoneUsageDescription` |
| `asuka/location` | `ACCESS_COARSE_LOCATION`, `ACCESS_FINE_LOCATION` | `NSLocationWhenInUseUsageDescription` |
| `asuka/notify` | `POST_NOTIFICATIONS` | |
| `asuka/network` | `ACCESS_NETWORK_STATE` | |
| `asuka/library` | `WRITE_EXTERNAL_STORAGE` (Android 9 and earlier) | `NSPhotoLibraryAddUsageDescription`; the app's Documents shown in Files |
| `asuka/background` | `FOREGROUND_SERVICE`, `FOREGROUND_SERVICE_DATA_SYNC`, `POST_NOTIFICATIONS`, `WAKE_LOCK`; a foreground service | |
| `asuka/scan` | ML Kit's barcode model (about 2 MB) | |
| `asuka/vision` | ML Kit's text, face and barcode models (about 30 MB) | |
| `asuka/photos` | `READ_MEDIA_IMAGES`, `READ_MEDIA_VISUAL_USER_SELECTED`; `READ_EXTERNAL_STORAGE` (Android 12 and earlier) | `NSPhotoLibraryUsageDescription` |
| `asuka/push` | Firebase Cloud Messaging, `POST_NOTIFICATIONS` | the `aps-environment` entitlement; `remote-notification` background mode |
| `asuka/appleauth` | | the Sign in with Apple entitlement |
| `asuka/biometric` | `USE_BIOMETRIC` | `NSFaceIDUsageDescription` |
| `asuka/audio/record` | `RECORD_AUDIO`, `FOREGROUND_SERVICE_MICROPHONE`; a foreground service | `NSMicrophoneUsageDescription` |
| `asuka/transcribe` | `RECORD_AUDIO` | `NSSpeechRecognitionUsageDescription` |
| `asuka/screen` | `DETECT_SCREEN_CAPTURE` | |
| `asuka/tracking` | | `NSUserTrackingUsageDescription` |
| `asuka/review` | Play's in-app review library | |
| `asuka/integrity` | Play Integrity | the App Attest entitlement |
| `asuka/agerange` | Play Age Signals | the Declared Age Range entitlement |
| `asuka/maps` | Google's Maps SDK | |
| `asuka/periodic` | WorkManager | `fetch` and `processing` background modes; the task identifiers |
| `asuka/sensors` | `ACTIVITY_RECOGNITION` (the pedometer) | `NSMotionUsageDescription` (the pedometer) |
| `asuka/speech` | the text-to-speech engine's visibility (`<queries>`) | |
| `asuka/contacts` | `READ_CONTACTS` (for `List`) | `NSContactsUsageDescription` |
| `asuka/calendar` | `READ_CALENDAR` (for `Calendars`, `Events`) | `NSCalendarsFullAccessUsageDescription`, `NSCalendarsUsageDescription` |
| `asuka/compose` | the visibility of the apps for `mailto:` and `smsto:` (`<queries>`) | `mailto` in `LSApplicationQueriesSchemes` |

`asuka/device`, `asuka/audio`, `asuka/splash`, `asuka/imageedit`,
`asuka/widget`, `asuka/js`, `asuka/secure`, `asuka/preview` and
`asuka/print` need none.

An entitlement needs its capability on the app's App ID
(developer.apple.com, Identifiers) for a release build: the provisioning
profile has it then. `asuka build -platform ios` stops with what's missing if the
profile lacks one; simulator builds have them all.

The purpose strings Asuka writes are generic ("To take a photo."). iOS
and macOS show them when they ask the user, so write your own in
`asuka.json`'s `ios.usage`:

```json
{
  "ios": {
    "usage": {
      "NSCameraUsageDescription": "To photograph your receipts.",
      "NSLocationWhenInUseUsageDescription": "To find stores near you."
    }
  }
}
```

Each package asks the user the first time it needs to, not at launch.
Calls that wait (for the user, a location fix, the platform) are marked
"not on the main thread" below: call them from a goroutine and
`ui.Post` the result back (see [the main thread](concepts.md#the-main-thread)).
The others take a callback, called on the main thread.

## On a Mac

A Mac app (`asuka dev` and `asuka build`) has the same purpose strings in
its Info.plist (location's as `NSLocationUsageDescription` too). Its
signature has the hardened runtime's entitlements for what they ask
(camera, microphone, location, contacts, calendars, photos), without
which a Developer ID build is refused them. Push's `aps-environment`,
Sign in with Apple and App Attest are entitlements only a provisioning
profile grants: `asuka build` embeds one for an app that uses them
(`asuka.json`'s `mac.profile`, else `ASUKA_MAC_PROFILE`, else the best
match installed). Without one, the app builds, the CLI says what won't
work, and on the Mac registering for pushes and signing in with Apple
fail, and App Attest says it isn't supported. `asuka dev` signs ad hoc,
without a profile, so the same holds there. A Mac App Store build
(`-appstore`) is sandboxed, with the network and the files the user
picks. See [Shipping](shipping.md).

The packages work on macOS as on iOS, through the same frameworks
(AVFoundation, PhotoKit, Core Location, MapKit, Vision, Speech,
UserNotifications, AuthenticationServices), except where a Mac differs:

| Package | On a Mac |
| --- | --- |
| `asuka/device` | `PickPhotos` and `PickFiles`: an open panel, a sheet on the window (no permission), copies in the app's caches. `Browse` opens the page in the user's browser. `GetInfo`: `Model` `"Mac"`, `System` `"macOS"` |
| `asuka/camera` | `Take`: a sheet over the window with a live preview, Take Photo and Cancel (AppKit has no camera screen). `View`'s "back" is the built-in camera (a Mac has no back one), or the one plugged in; scanning codes in it needs macOS 13 |
| `asuka/audio/record` | No audio session: other apps' sound plays on; `Background` changes nothing, a Mac app records out of sight anyway |
| `asuka/contacts`, `asuka/calendar` | `Add` asks in an alert over the window (macOS has no new contact or event screen), then saves, asking for access first |
| `asuka/photos` | PhotoKit, as on iOS; a Mac has no limited library: `ManageLimited` does nothing |
| `asuka/library` | Photos and videos go to Photos; `Downloads` and `Music` to the user's `~/Downloads` and `~/Music`, in the folder asked |
| `asuka/biometric` | Touch ID's prompt, or the user's password (not with `NoPasscode`) |
| `asuka/push` | APNs, as on iOS; no `Rewrite` (it needs a notification service extension). Needs a provisioning profile (above) |
| `asuka/appleauth` | Apple's button and sheet, as on iOS. Needs a provisioning profile (above) |
| `asuka/integrity` | App Attest, as on iOS. Needs a provisioning profile (above): without one `Supported` is false |
| `asuka/background` | The task keeps App Nap from slowing the app; there's no notification, so `Update` changes nothing |
| `asuka/periodic` | `NSBackgroundActivityScheduler`: the tasks run while the app runs (it isn't launched for them); one that wants power waits while the Mac is on its battery |
| `asuka/screen` | `Lock` and `SetBars` do nothing; `PreventCapture` leaves the app's windows out of screenshots and recordings; `OnScreenshot` hears of the screenshots Spotlight finds |
| `asuka/transcribe` | On the device only; the app's bundle needs its purpose string, which `asuka build` writes |
| `asuka/sensors` | A Mac has no motion sensors: `Available` is false, watching gets nothing, `StepsBetween` fails with `ErrNotAvailable` |
| `ui.Haptic` | The trackpad's feedback (a Force Touch trackpad): an alignment for `HapticSelection`, a level change for the others |
| `asuka/widget`, `asuka/agerange` | Not on a Mac: `widget.Set` does nothing, `agerange.Request` fails with `ErrNotAvailable` |
| `asuka/splash` | A Mac app has no launch screen: `Hold` and `Hide` do nothing |
| `ui.OnShare`, `app.ShareSheet`, shortcuts | iOS and Android only: a Mac app isn't offered in other apps' share menus, and `asuka.json`'s `shortcuts` aren't added |

## On Windows

A Windows app declares nothing: `asuka build -platform windows` writes no
capabilities, and Windows asks a desktop app for no permission. The
privacy switches in Settings (Privacy & security) still hold for the
camera, the microphone and location: with one off, the camera and
recording fail with `"not allowed"`, and location with
`location.ErrDenied`.

The packages work on Windows through its own services (Media Foundation,
WIC, WinRT, Windows Hello, SAPI, the Credential Manager), except where
Windows differs; where it has nothing, the package answers as on a
device without it:

| Package | On Windows |
| --- | --- |
| `asuka/device` | `PickPhotos` and `PickFiles`: the open dialog over the window, copies in the app's caches. `Browse` opens the page in the user's browser. The clipboard's images are PNGs. `KeepAwake` keeps the display on. `GetInfo`: `Model` `"PC"`, `System` `"Windows"`, `SystemVersion` `"10.0.<build>"` |
| `asuka/secure` | The user's generic credentials in the Credential Manager, named `<app>/<key>` (a value up to 2560 bytes) |
| `asuka/camera` | Media Foundation's capture engine. `Take`: a dialog over the window with a live preview, Take Photo and Cancel (Enter and Escape). `View` picks the camera Windows names front or rear, else the first; with `Scan`, it reads codes in four frames a second with Asuka's own reader (as `asuka/scan`'s `File`). Settings' camera switch off: "not allowed" |
| `asuka/camera/record` | H.264 and AAC in MPEG-4 (`.mp4`), the default microphone unless muted |
| `asuka/audio` | MCI (DirectShow's playback); a URL is downloaded to the app's caches first |
| `asuka/audio/record` | The default microphone, AAC at 44.1 kHz (voice mono, music stereo); `Background` changes nothing |
| `asuka/speech` | SAPI's voices, those installed (Settings, Time & language, Speech) |
| `asuka/network` | The Network List Manager: Wi-Fi, cellular or a cable; `Metered` for a connection with a cost, `Constrained` near or over its data limit |
| `asuka/location` | WinRT's Geolocator. `PlaceName` asks OpenStreetMap's Nominatim (Windows's own geocoder needs a Bing Maps key): one request a second at most, answers kept in the app's caches |
| `asuka/biometric` | Windows Hello's prompt; `Check`'s kind is the sensor's; Hello always offers its PIN, and names its own cancel button |
| `asuka/webauth` | A window over the app's with WebView2 in it (the app's web profile; `Ephemeral`: a throwaway one) |
| `asuka/js` | A WebView2 that's never shown |
| `asuka/contacts`, `asuka/calendar` | Windows gives the contacts and calendars to packaged apps only: `Request` says denied; `Add` opens a vCard or an event file in the user's contacts or calendar app (`saved` is false: it doesn't say) |
| `asuka/photos` | The Pictures folder (no permission to ask); screenshots are its Screenshots folder's |
| `asuka/library` | The user's Downloads, Videos, Music and Pictures folders, in the folder asked |
| `asuka/vision` | Windows's OCR, in the languages whose OCR is installed (Settings, Time & language), and its face detector; codes as `asuka/scan` reads them |
| `asuka/scan` | Windows has no reader of codes for apps: Asuka's own, in Go, reads QR codes, Data Matrix, EAN-13 and EAN-8, UPC-A and UPC-E, Code 128 and Code 39 (not Aztec, PDF417, Code 93, Codabar nor ITF) |
| `asuka/imageedit` | Windows's codecs read (HEIC and WebP where their extensions are installed); a video's frame through Media Foundation |
| `asuka/maps` | Windows has no map control for desktop apps (its own needs a Bing Maps key): the host draws OpenStreetMap's tiles, downloaded two at a time and kept a week in the app's caches, with "© OpenStreetMap contributors" on the map; drag, the wheel or a pinch, double-click, + and -. Satellite and Hybrid show the streets (no free imagery), and `ShowsUser` shows nothing, as on Linux |
| `asuka/background` | The task keeps the PC from sleeping; there's no notification, so `Update` changes nothing |
| `asuka/periodic` | Timers while the app runs (it isn't launched for them); one that wants power waits while the PC is on its battery |
| `asuka/screen` | `Lock` and `SetBars` do nothing; `PreventCapture` leaves the app's windows out of screenshots and recordings; `OnScreenshot` hears of the screenshots saved in the Screenshots folder (Print Screen with Windows, the Snipping Tool) |
| `asuka/tracking` | No prompt: allowed while the advertising ID is on in Settings, else denied |
| `asuka/push`, `asuka/widget`, `asuka/review`, `asuka/integrity` | Packaged (Store) apps' or Apple's and Google's: `push.Register` fails, `widget.Set` does nothing, `review.Request` shows nothing, `integrity.Supported` is false |
| `asuka/appleauth`, `asuka/agerange`, `asuka/transcribe`, `asuka/sensors` | Not on Windows: unavailable, as on a device without them |
| `ui.Haptic` | A PC has no haptics: it does nothing |
| `asuka/splash` | A Windows app has no launch screen: `Hold` and `Hide` do nothing |
| `ui.OnShare`, `app.ShareSheet`, shortcuts | iOS and Android only |

## On Linux

A Linux app declares nothing either. Where the host goes through an XDG
desktop portal (files, the camera, location, notifications, mail,
printing, opening files and links, keeping the system awake), the
portal asks the user as it does, and it works in a Flatpak sandbox as
out of one. The Flatpak manifest `asuka build -platform linux` writes
lets the app reach only the display, the GPU, the network and the
portals. Services the host reaches without a portal (Speech Dispatcher,
Evolution Data Server for contacts and calendars, NetworkManager for the
connection's type, iio-sensor-proxy, a Video4Linux camera) need the
`finish-args` that open them, added to the manifest; without them, the
package answers as on a system without the service. A notification due
later needs systemd outside the sandbox: in a Flatpak it shows only
while the app runs (see [On each platform](#on-each-platform)).

The Linux host reaches the desktop's services through the portals where
there's one, GLib and GTK, and the libraries a desktop has, each loaded
the first time it's used: a system without it answers an error that
names what's missing.

| Package | On Linux |
| --- | --- |
| `asuka/device` | `PickPhotos` and `PickFiles`: GTK's file dialog (the portal's in a sandbox), copies in the app's caches. Clipboard images and changes through GDK's clipboard. `Browse` opens the page in the user's browser. `KeepAwake` keeps the session from idling. `GetInfo`: `Model` `"PC"`, `System` `"Linux"`, `SystemVersion` the distribution's (`/etc/os-release`) |
| `asuka/secure` | The keyring (libsecret: GNOME Keyring, KWallet, KeePassXC), an item per key labeled with the app's name; without libsecret or a keyring, the calls fail saying so |
| `asuka/network` | GNetworkMonitor (the portal's in a sandbox); Wi-Fi, ethernet or cellular from NetworkManager; never constrained |
| `asuka/location` | The location portal (GeoClue's, which asks the user), else GeoClue itself; `PlaceName` with geocode-glib, when it's installed |
| `asuka/speech` | Speech Dispatcher (started when it isn't running): its voices, rate, pitch, volume |
| `asuka/audio`, `asuka/audio/record` | GStreamer: an AAC encoder (FDK, libav's...) records, the default source the microphone |
| `asuka/library` | The user's folders (XDG's Pictures, Videos, Music, Downloads), in the folder asked |
| `ui.ShareFiles` | No share sheet: the files are copied to the clipboard, with a toast |
| `asuka/camera`, `asuka/camera/record` | The camera portal (it asks the user once; PipeWire's cameras, `gstreamer1.0-pipewire`), else the first Video4Linux camera, through GStreamer. `Take`: a dialog over the window with a live preview, Take Photo and Cancel. `View`: the preview, mirrored, filling its box; front and back are the same camera (a computer's face the user). Videos are MPEG-4 (H.264 from x264 or OpenH264, AAC), else WebM; scanning codes needs GStreamer's `zbar` element. No camera: `ErrNoCamera`; refused: "not allowed" |
| `asuka/scan`, `asuka/vision` | ZBar (`libzbar0`) reads codes: no Aztec or Data Matrix. Tesseract (`libtesseract5` and a language's data) reads text; nothing finds faces: `Faces` is empty. Without ZBar, `scan.File` fails "not available"; `vision.Analyze` leaves out what isn't installed |
| `asuka/imageedit` | GdkPixbuf's loaders decode (HEIF and WebP where their loaders are installed), upright; the actions are done in Go; video frames from GStreamer |
| `asuka/webauth` | A dialog over the window with a WebKitGTK web view (`Ephemeral`: a session of its own); closing it cancels |
| `asuka/js` | A WebKitGTK web view that's never shown |
| `asuka/maps` | libshumate's map of OpenStreetMap's tiles (`libshumate-1.0-1`); Satellite and Hybrid show the streets; no user location layer. Without libshumate, the view says so and `OnError` is called |
| `asuka/background` | The task keeps the system from suspending; no notification, `Update` changes nothing |
| `asuka/periodic` | The tasks run on a timer while the app runs (it isn't launched for them); one that wants power waits while on the battery, one that wants the network while it's offline |
| `asuka/screen` | `Lock`, `SetBars` and `PreventCapture` do nothing; `OnScreenshot` hears of new images in Pictures/Screenshots (GNOME's, KDE's) |
| `asuka/sensors` | iio-sensor-proxy (convertibles, tablets): the light sensor, in lux, and the accelerometer's orientation as 1 g along the axis that's up, as it turns. No others: `Available` is false, `StepsBetween` fails with `ErrNotAvailable` |
| `asuka/contacts`, `asuka/calendar` | Evolution Data Server's address books and calendars (GNOME Contacts', GNOME Calendar's, the online accounts'): access is `Full` when it has one; `Pick` is a dialog of the contacts with a search field; `Add` asks, then saves in the default address book or the event's calendar. Without it: contacts' access is `Denied`, calendars' `WriteOnly`; `Add` opens a vCard or an iCalendar file in the user's contacts or calendar app (`saved` is false); `contacts.Pick` fails |
| `asuka/photos`' library, `asuka/appleauth`, `asuka/transcribe`, `asuka/widget` | None on Linux: the library's access is `Denied`; `appleauth.Available` is false and its button shows nothing; `transcribe` fails with `ErrNotAvailable`; `widget.Set` does nothing. `asuka/splash` has nothing to hold |
| `asuka/biometric`, `asuka/integrity`, `asuka/agerange`, `asuka/push` | None on Linux: not available, not supported. `asuka/review` does nothing; `asuka/tracking` answers allowed |
| `asuka/crash` | Go's crash output, which has a crash in native code too (a signal in GTK's code, a GLib abort), reported as `Native`; the system's own record (systemd-coredump, apport) isn't read |
| `ui.Haptic` | A desktop has no haptics: it does nothing |
| `ui.OnShare`, `app.ShareSheet`, shortcuts | iOS and Android only |

## In a browser

A page declares nothing: the browser asks the user the first time a
package needs the camera, the microphone, location or notifications.
Most of these APIs need a secure page, served over `https` or from
`localhost` (`asuka dev -platform web`'s, but not `-lan`'s addresses).
The packages call the web's APIs; [the web](web.md) has the details,
and its service worker for notifications and Web Push.

| Package | In a browser |
| --- | --- |
| `asuka/device` | `PickPhotos` and `PickFiles`: the browser's file picker, copies in the caches. `Browse`: a new tab. The clipboard: the app's own copies, and other apps' where the user lets the page read it. `KeepAwake`: the Screen Wake Lock. `GetInfo` from the user agent (Chromium's client hints give the system's version and an Android phone's model) |
| `asuka/secure` | The values encrypted (AES-GCM) in IndexedDB, with a key the browser won't let out; not a keychain (see [the web](web.md)) |
| `asuka/camera`, `asuka/camera/record` | `Take` opens a phone's camera, or a dialog with the live camera and a shutter; `View` is the camera in a `<video>`, with `Capture`, recording, and scanning codes where the browser has `BarcodeDetector` |
| `asuka/scan`, `asuka/vision` | `BarcodeDetector` reads codes; faces and text only where the browser has them |
| `asuka/imageedit` | A canvas: JPEG or PNG; a remote image needs CORS |
| `asuka/photos` | A page can't read the library: access is `Denied` (`device.PickPhotos` picks photos) |
| `asuka/library` | `Save` downloads the file |
| `asuka/location` | The Geolocation API; `PlaceName` isn't on the web (a browser has no geocoder) |
| `asuka/network` | Online from `navigator.onLine`; Wi-Fi, cellular or Ethernet where the browser says (Chrome on Android), metered for cellular, constrained for Data Saver |
| `asuka/notify`, `asuka/push` | The Notification API and Web Push, through the app's service worker (`push.Register`'s `Service` is `"webpush"`); a notification due later can't show while no page of the app is open |
| `asuka/audio`, `asuka/audio/record` | An audio element per player (the browser's autoplay rules apply: play from a tap); `MediaRecorder`, AAC in MPEG-4 where the browser records it, else Opus in WebM |
| `asuka/speech` | `speechSynthesis`, the browser's voices |
| `asuka/sensors` | The Generic Sensor API (Chrome) or device motion and orientation events (Safari asks on a tap), in Android's axes and units; no barometer or pedometer |
| `asuka/biometric` | The platform's authenticator through WebAuthn (Touch ID, Face ID, Windows Hello, Android's screen lock): the first `Authenticate` makes a passkey for the site. The prompt is the browser's: `Reason` and `Cancel` don't show |
| `asuka/webauth` | A popup, done when it comes back to a page of the app's own origin (a custom scheme can't open a page) |
| `asuka/js` | A hidden, sandboxed frame |
| `asuka/contacts`, `asuka/calendar` | `contacts.Pick` where the browser has the Contact Picker (Chrome on Android); `Add` downloads a vCard or an `.ics` file; reading them isn't on the web |
| `asuka/screen` | `Lock` only full screen or in an installed app; hiding the bars is full screen; no `PreventCapture` or `OnScreenshot` |
| `asuka/background` | The page runs while it's open; a task adds nothing, and has no notification |
| `asuka/tracking` | No prompt: `Denied` when the browser sends Global Privacy Control or Do Not Track, else `Allowed` |
| `ui.Haptic` | `navigator.vibrate`, where the browser has it (Chrome on Android) |
| `asuka/review`, `asuka/integrity`, `asuka/agerange`, `asuka/appleauth`, `asuka/periodic`, `asuka/widget`, `asuka/transcribe` | Not on the web: an error that says so, or the package's `ErrNotAvailable` |
| `asuka/maps` | OpenStreetMap's tiles, drawn by the host, as on Windows; `ShowsUser` once the page has the location permission |
| `asuka/crash` | Go's crashes (the host keeps the runtime's output) and uncaught JavaScript errors, as `Exception` reports that aren't fatal |
| `asuka/splash` | `Hold` and `Hide` do nothing |
| `ui.OnShare`, `app.ShareSheet`, shortcuts | iOS and Android only |

## The camera: asuka/camera

`camera.Take` shows the system's camera (iOS's camera screen, Android's
camera app) and calls `done` with the photo's file, a JPEG, or `""` if the
user cancelled. On a device without a camera (the iOS simulator) the error
is `camera.ErrNoCamera`. A desktop has no camera screen: there `Take`
shows a dialog of Asuka's over the window, with a live preview, Take
Photo and Cancel; a browser opens a phone's camera, or a dialog like it.

```go
ui.Button(ui.ButtonProps{Title: ui.Val("Take a photo"), OnPress: func() {
	camera.Take(func(path string, err error) {
		switch {
		case errors.Is(err, camera.ErrNoCamera):
			status.Set("This device has no camera")
		case err != nil:
			status.Set(err.Error())
		case path != "":
			photo.Set(path) // ui.Image shows it
		}
	})
}})
```

For a capture screen of the app's own, `camera.View` shows what the camera
sees, live, filling its box (AVFoundation's preview on iOS and the Mac,
CameraX's `PreviewView` on Android, Media Foundation's on Windows,
GStreamer's on Linux, a `<video>` in a browser), and `camera.Capture`
takes a photo from it:

```go
camera.View(camera.ViewProps{
	Mode:    camera.Video,
	Facing:  camera.Back,
	Style:   ui.Style{Grow: 1},
	OnError: func(message string) { status.Set(message) },
}),
ui.Button(ui.ButtonProps{Title: ui.Val("Photo"), OnPress: func() {
	camera.Capture(func(path string, err error) {
		if err != nil {
			status.Set(err.Error())
			return
		}
		status.Set("Saved " + path)
	})
}}),
```

`ViewProps`:

- `Facing`: `camera.Back` (the default) or `camera.Front`.
- `Mode`: `camera.Photo` (the default), or `camera.Video` for recording
  with `asuka/camera/record`, which takes photos too.
- `Style`: its box.
- `OnError`: called when the camera can't show: `"not allowed"` if the
  user refused it, `"no camera"` if there's none.
- `Scan`: reads codes in what it sees; see `asuka/scan`.

The view asks for the camera the first time it shows, and the camera is
on while the view is on screen. Show one at a time: `Capture` and
recording use the one on screen, and fail with `"no camera view"` if
there's none.

The files from `Take` and `Capture` are in the app's caches, which the
system may clear: move one into `store.Dir()` to keep it, or save it to
the photo library with [`asuka/library`](#saving-to-the-library-asukalibrary).

**Android**: as `asuka/camera` declares the camera permission (the view
needs it), Android requires it of the camera app too: `Take` asks for it
first, and fails with `"not allowed"` if the user refuses.

## Recording video: asuka/camera/record

`asuka/camera/record` records video from the `camera.View` on screen, whose
`Mode` is `camera.Video`. `record.Start` records until `record.Stop`, or
until `MaxDuration` if set, then calls `done` with the video's file (in
the app's caches: `.mov` on iOS and the Mac, `.mp4` on Android and
Windows, `.mp4` or else `.webm` on Linux, as its encoders allow).

```go
ui.Button(ui.ButtonProps{
	Title: signal.Func(func() string {
		if recording.Get() {
			return "Stop"
		}
		return "Record"
	}),
	OnPress: func() {
		if recording.Peek() {
			record.Stop()
			return
		}
		recording.Set(true)
		record.Start(record.Options{MaxDuration: time.Minute}, func(path string, err error) {
			recording.Set(false)
			if err != nil {
				status.Set(err.Error())
				return
			}
			status.Set("Recorded " + path) // ui.Video plays it
		})
	},
})
```

It records sound too, and asks for the microphone the first time; if the
user refuses, the video has no sound. `record.Options{Mute: true}`
records without sound and without asking. One recording at a time.

The microphone's permission and purpose string come with this package,
not with `asuka/camera`: an app that only takes photos goes without them.

## Barcodes and QR codes: asuka/scan

`asuka/scan` reads barcodes and QR codes, live in what a `camera.View`
sees, or in an image file. iOS and the Mac read them with AVFoundation
and Vision; Android with ML Kit, which only the apps that import
`asuka/scan` are built with; Windows with a reader of Asuka's own, in Go;
Linux with ZBar; a browser with `BarcodeDetector`. Not every reader has
every kind: see [On Windows](#on-windows) and [On Linux](#on-linux).

`scan.Codes` makes the view's `Scan`. It reads the kinds given (every
kind, if none), and calls its function on the main thread as a code comes
into view: once while it stays, and again after it's been out of view for
a second.

```go
camera.View(camera.ViewProps{
	Style: ui.Style{Grow: 1},
	Scan: scan.Codes(func(c scan.Code) {
		found.Set(fmt.Sprintf("%s: %s", c.Kind, c.Value))
		ui.Haptic(ui.HapticSuccess)
	}, scan.QR, scan.EAN13),
})
```

`scan.File` reads the codes in an image, a photo taken or picked, and
calls `done` with them (none, if there are none):

```go
scan.File(path, func(codes []scan.Code, err error) {
	switch {
	case err != nil:
		found.Set(err.Error())
	case len(codes) == 0:
		found.Set("No code in it")
	default:
		found.Set(codes[0].Value)
	}
}, scan.QR)
```

A `scan.Code` is its `Kind` and its `Value`, the text it holds. The
kinds: `QR`, `Aztec`, `DataMatrix`, `PDF417`, `EAN13`, `EAN8`, `UPCA`,
`UPCE`, `Code128`, `Code39`, `Code93`, `Codabar`, `ITF`.

**iOS** reads a UPC-A as an EAN-13 with a leading 0; asked for `UPCA`,
Asuka gives it as a UPC-A again.

## What's in an image: asuka/vision

`vision.Analyze(path, options)` finds text, faces and codes in an image,
and where each is. It runs on the device: Apple's Vision on iOS and the
Mac, ML Kit's bundled models on Android, which only the apps that import
`asuka/vision` are built with, Windows's OCR and face detector, Tesseract
on Linux (no faces), and in a browser what its Shape Detection API has.
Not on the main thread.

```go
go func() {
	r, err := vision.Analyze(photo, vision.Options{Languages: []string{"zh-Hans", "en-US"}})
	if err != nil {
		return
	}
	ui.Post(func() {
		for _, l := range r.Lines {
			fmt.Println(l.Text, l.Box)
			for _, w := range l.Words {
				fmt.Println(" ", w.Text, w.Box, w.Start, w.End)
			}
		}
		fmt.Println(len(r.Faces), "faces,", len(r.Codes), "codes")
	})
}()
```

- A `Box` is in fractions of the image from its top left, with the image
  upright: it's the same at any scale, for a view over the image or
  `asuka/imageedit`'s pixels.
- A `Line` has its `Text`, `Box` and `Words`. A `Word` is what the platform's
  word breaking finds (dictionary-based for Chinese and Japanese), or a mark of
  punctuation. `Start` and `End` are its place in the line's text, in runes.
- A `Code` has its `Kind` (as `asuka/scan` names them), `Value` and `Box`.
- `Options` turn off what you don't need (`NoText`, `NoFaces`, `NoCodes`).
  `Languages` are the text's, most likely first, iOS only: Vision reads only
  the script of the first's family well, so put Chinese first where it may
  appear. Its Chinese model reads Latin letters too. Android reads Chinese and
  Latin scripts always.

**iOS** boxes what it read as one group: words of a Chinese line that share a
box divide it by their width, so a word's box there is an estimate. A barcode's
box from Vision can be the line it read along; Asuka grows it to the bars. On
**Android** the words' boxes come from each symbol's.

OCR scales what it's given to a size of its own, and loses small text in a very
long screenshot: slice it with `imageedit.Crop` and analyze each slice.

## The photo library: asuka/photos

`asuka/photos` reads the user's library, with their permission: PhotoKit on iOS
and the Mac, MediaStore on Android, the Pictures folder on Windows. Linux
and the web have no library an app can read: access is `Denied`. It's for an app whose screen is the library. To let the
user pick a few photos, `device.PickPhotos` asks for no permission: prefer it
where it'll do. Google Play asks an app that reads the library why it does, in
the Play Console. Every call waits: not on the main thread.

```go
go func() {
	if a, _ := photos.Request(); a == photos.Denied {
		return
	}
	assets, _ := photos.List(photos.Query{Screenshots: true, Limit: 60})
	thumb, _ := photos.Thumbnail(assets[0].ID, 300) // a file, for ui.Image
	full, _ := photos.Export(assets[0].ID)          // the original, as a file
	fmt.Println(thumb, full.Path, full.Name)
}()
```

- `photos.Current()` says what's allowed without asking: `NotAsked`, `Denied`,
  `Limited` (only the photos the user chose: iOS 14, Android 14) or `Allowed`.
  `photos.Request()` asks the first time. `photos.ManageLimited()` lets a
  `Limited` user change which photos.
- `photos.List(query)` is newest first, from `Offset`, at most `Limit`. An
  `Asset` has its `ID`, `Width`, `Height`, `Created()` and `Screenshot`.
  `Screenshots` keeps only those: iOS knows; on Android it's what's in a
  Screenshots folder.
- `photos.Thumbnail(id, size)` is a JPEG at least `size` pixels on its short
  side, kept in the app's caches for the next time.
- `photos.Export(id)` copies the full image as it is in the library (HEIC,
  JPEG, PNG), with its name. iCloud photos download first.

## Photos, the browser, the clipboard: asuka/device

`asuka/device` is what the platform offers beyond views.

```go
ui.Button(ui.ButtonProps{Title: ui.Val("Pick photos"), OnPress: func() {
	device.PickPhotos(3, func(paths []string, err error) {
		if err == nil && len(paths) > 0 {
			photos.Set(paths)
		}
	})
}}),
ui.Button(ui.ButtonProps{Title: ui.Val("Help"), OnPress: func() {
	device.Browse("https://example.com/help")
}}),
```

- `device.PickPhotos(limit, done)` shows the system's photo picker
  (PHPicker on iOS, the Photo Picker on Android; the open dialog on the
  desktops, the file picker in a browser) for up to `limit` images
  (1 to 255). It asks for no permission: the user picks, and the app sees
  only what was picked. `done` gets the images' files, which `ui.Image`
  shows, or none if the user cancelled. The files are copies in the
  app's temporary files or caches: move one to keep it.
- `device.Browse(url)` shows a web page in the app (SFSafariViewController,
  a Custom Tab), over the app's screens. The URL must be http or https.
  `ui.OpenURL` leaves the app for the browser instead. A desktop opens
  the page in the user's browser, and a browser in a new tab.
- `device.Clipboard()` reads the clipboard's text; `ok` is false if it has
  none. Not on the main thread. iOS asks the user the first time the app
  reads what another app put there. `ui.Copy` writes it.
- `device.KeepAwake(on)` keeps the screen on while `on` is true: a video,
  a recipe, a map. Turn it off when the screen that wants it goes.
- `device.PickFiles(options, done)` shows the system's document picker
  (iOS's Files browser, Android's storage picker, the open dialog on the
  desktops, the file picker in a browser; no permission). `Types`
  are MIME types (`"application/pdf"`, `"image/*"`; none: any file) and
  `Multiple` lets the user pick several. `done` gets a `device.File` for
  each, its `Path` a copy in the app's caches, with its `Name`, `Type` and
  `Size`, or none if the user cancelled.
- `device.ClipboardHas()` says whether the clipboard holds `Text`, an
  `Image` or a `URL`, without reading it (iOS doesn't ask the user for
  this): show a Paste button only when there's something to paste.
  `device.ClipboardImage()` reads its image into a file, in the format it
  was copied in; `device.CopyImage(path, done)` puts an image file on it.
  `device.OnClipboardChange(f)` calls `f` when it changes, until `stop`.
- `device.GetInfo()` is the device and the app: `Model` ("iPhone", "Pixel
  9", "Mac", "PC"), `System` ("iOS", "Android", "macOS", "Windows",
  "Linux"), `SystemVersion`, and `AppVersion` and
  `Build`, `asuka.json`'s `version` and `build`. Not on the main thread.

```go
func Player() ui.Node {
	info := signal.New("")
	go func() {
		if i, err := device.GetInfo(); err == nil {
			ui.Post(func() { info.Set(i.Model + ", " + i.System + " " + i.SystemVersion) })
		}
	}()
	device.KeepAwake(true)
	signal.OnCleanup(func() { device.KeepAwake(false) })
	return ui.Text(info)
}
```

## Signing in on the web: asuka/webauth

`webauth.Start` shows a sign-in page in the system's own sign-in sheet:
`ASWebAuthenticationSession` on iOS (SwiftUI's
`webAuthenticationSession`), a Custom Tab on Android. It's the
browser's, so the user's sessions, passkeys and password autofill work
in it, and providers that refuse an app's own `WebView` (Google's
sign-in) don't refuse it. Use it for OAuth rather than a `ui.WebView`.

The Mac has `ASWebAuthenticationSession` too. Windows and Linux have no
system sheet: the host shows the page in a web view of its own over the
window (WebView2, WebKitGTK), which has the app's web profile but not the
user's browser's sessions, and which a provider that refuses embedded
web views refuses there. A browser opens the page in a popup, done when
it comes back to a page of the app's own origin (a custom scheme can't
open a page).

```go
s := webauth.Start(webauth.Options{URL: authorizeURL, CallbackScheme: "myapp"},
	func(callback string, err error) {
		if errors.Is(err, webauth.ErrCanceled) {
			return // the user closed it
		}
		// callback is myapp://…?code=…: exchange the code.
	})
```

It ends in one of two ways:

- **A callback scheme.** When the page goes to a URL of
  `CallbackScheme` (the app's own), the sheet closes and `done` gets the
  URL. On Android the scheme must be the app's, asuka.json's `scheme`; it
  goes to `done` rather than `ui.OnOpenURL`.
- **The app ends it.** Without a scheme, the page redirects somewhere
  the app hears from: an OAuth server that allows a redirect to the
  loopback interface (`http://localhost:port/callback`, RFC 8252) sends
  the code to a server the app runs on that port, which calls
  `s.Cancel()` to close the sheet. On Android, cancelling brings the app
  back over the tab.

`done` gets `webauth.ErrCanceled` when the user closes the sheet, or
`Cancel` does. `Ephemeral` asks for a private session, without the
browser's cookies (iOS's `prefersEphemeralWebBrowserSession`; Android's
Custom Tabs keep them). [`examples/usage`](../examples/usage) signs in
with Claude this way, with a loopback server.

## Copy, share and open: ui.Copy, ui.Share, ui.OpenURL

Three calls in `ui` hand text to the system. All run on the main thread.

```go
ui.Button(ui.ButtonProps{Title: ui.Val("Copy link"), OnPress: func() { ui.Copy("https://example.com/s/42") }}),
ui.Button(ui.ButtonProps{Title: ui.Val("Share"), OnPress: func() { ui.Share("https://example.com/s/42") }}),
ui.Button(ui.ButtonProps{Title: ui.Val("Open in browser"), OnPress: func() { ui.OpenURL("https://example.com") }}),
ui.Button(ui.ButtonProps{Title: ui.Val("Call"), OnPress: func() { ui.OpenURL("tel:+15555550100") }}),
```

- `ui.Copy(text)` puts text on the clipboard.
- `ui.Share(text)` shows the system's share sheet, to send it to an app, a
  contact, AirDrop or Nearby Share. A URL is shared as a link. The Mac
  shows its sharing services picker, Windows its share UI, a browser
  the Web Share sheet where it has one; Linux has none, so the text is
  copied to the clipboard, with a toast (as a browser without one
  copies it).
- `ui.OpenURL(url)` asks the system to open a URL: a web address in the
  browser, another scheme (`tel:`, `mailto:`, another app's) in the app
  that handles it.

`ui.ShareFiles(paths, options, done)` shows the share sheet for files: a
video to send, a PDF to save to Files, photos for AirDrop. `done` runs on
the main thread once the sheet is gone, whether the user shared or not;
its error says when the sheet couldn't show (a file that isn't there).

```go
var share ui.Node
share = ui.Button(ui.ButtonProps{Title: ui.Val("Share"), OnPress: func() {
	ui.ShareFiles([]string{report}, ui.ShareOptions{MIME: "application/pdf", Title: "Send the report", From: share}, func(err error) {
		if err != nil {
			status.Set(err.Error())
		}
	})
}})
```

`ShareOptions` are expo-sharing's:

| Option | |
| --- | --- |
| `MIME` | The files' type, which the apps on the sheet go by. Android sets the intent's type to it. On iOS it's the type when `UTI` is empty. Default: from the first file's extension. |
| `UTI` | iOS: the files' Uniform Type Identifier (`com.adobe.pdf`), ahead of `MIME`. |
| `Title` | The sheet's title: Android's chooser, and a mail's subject on iOS. |
| `From` | The view the sheet points at where it's a popover (iPad, the Mac), usually the button that shares. Default: the screen's middle. |

On iOS a file whose extension doesn't match its type is shared under a
name that does, as expo-sharing does: `image.pdf` shared as a JPEG goes
as `image.pdf.jpeg`, a link to the file, not a copy. On Android the files
go through the app's FileProvider; one outside the app's own folders is
copied to its caches first. On the Mac it's the sharing services
picker. On Windows it's the share UI (`done` is called once it shows:
Windows doesn't say when it's gone).
Linux has none: the files go on the clipboard, as a shared text does. A
browser shares the files with the Web Share sheet where it can, else
downloads each.

To be opened with URLs yourself, see `asuka.json`'s `scheme` and `links` in
[Shipping](shipping.md#asukajson), and `ui.OnOpenURL`.

## Previewing files: asuka/preview

`preview.Show(sources, options, done)` shows files with the system's
viewer, as a mail app shows an attachment: PDFs, images, video, audio,
Office and iWork documents, text. A source is a file's path, an asset's
(as `ui.Image` takes it), or an http(s) URL, which the host downloads to
the app's caches first, under its name (or with the extension of the type
the server gives). Main thread.

```go
var open ui.Node
open = ui.Button(ui.ButtonProps{Title: ui.Val("Open the invoice"), OnPress: func() {
	preview.Show([]string{invoice, "docs/terms.pdf"}, preview.Options{From: open}, func(err error) {
		if errors.Is(err, preview.ErrNoViewer) {
			status.Set("Nothing on this device opens it")
		}
	})
}})
```

| Option | |
| --- | --- |
| `Start` | The index of the file shown first. |
| `From` | The view the viewer grows from (iOS), usually what the user tapped. |
| `Edit` | Lets the user mark up PDFs and images with Markup, which saves into the files (iOS). |

`done` runs on the main thread once the viewer is gone, with
`preview.ErrNoViewer` if nothing can show the files, or another error (no
such file, a download that failed).

| | Viewer | `done` |
| --- | --- | --- |
| iOS | Quick Look (`QLPreviewController`) over the screen: swipe between the files; share, Markup, search | when it's dismissed |
| Android | `ACTION_VIEW` of the `Start` file in the app the user picks for its type (Drive's PDF viewer, Photos), through the app's FileProvider with the right to read it alone | when the user is back |
| macOS | Quick Look's panel (`QLPreviewPanel`), its arrows between the files | when the panel closes |
| Windows | the file's default app (`ShellExecuteEx`), or the "How do you want to open this?" dialog | once it's handed over |
| Linux | the file's default app (`GtkFileLauncher`, the portal's in a sandbox) | once it's handed over |
| Web | a new tab, for what a browser shows (images, video, audio, text, PDF, HTML); another type is `ErrNoViewer` | once it's opened |

Android has no system viewer, nor one that pages through several files:
it opens one, as Android's mail apps do with each attachment.

## Printing and PDFs: asuka/print

`asuka/print` shows the system's print dialog, with its printers and
options (copies, pages, paper, orientation; Save as PDF on Android and the
Mac), for a file, HTML markup or a WebView's page; and lays HTML out on
pages as a PDF. Each call but `PDF` runs on the main thread, and `done`
gets a `print.Result` once the dialog is closed:

```go
var printButton ui.Node
printButton = ui.Button(ui.ButtonProps{Title: ui.Val("Print"), OnPress: func() {
	print.HTML(receipt, print.Options{Job: "Receipt", From: printButton}, func(r print.Result, err error) {
		if err == nil && r == print.Printed {
			status.Set("Sent to the printer")
		}
	})
}})
```

- `print.File(source, options, done)` prints a PDF or an image (a path, an
  asset, an http(s) URL, as `preview.Show` takes them). Another type fails
  with `print.ErrUnsupported`, but on Windows and Linux (where the
  printer takes what CUPS can print).
- `print.HTML(markup, options, done)` prints markup, paginated as a browser
  prints a page: CSS's `@media print` and `break-before` apply. Images and
  styles come from absolute URLs (`https:`, `data:`).
- `print.WebView(web, options, done)` prints what a `ui.WebView` shows.

`Options` has `Job`, the job's name in the queue (default: the file's
name, the page's title, else the app's name), `Orientation`, which the
dialog starts with (`print.Portrait`, `print.Landscape`), and `From`, the
view the dialog points at where it's a popover (the iPad).

A `Result` is `print.Printed` (the job is in the printer's queue, or saved
as a PDF from the dialog), `print.Cancelled`, or `print.Unknown` where
the platform doesn't say. A job that fails is an error.
`print.ErrUnavailable` says the device can't print.

| | Dialog | File | HTML, WebView | Result |
| --- | --- | --- | --- | --- |
| iOS | `UIPrintInteractionController`, a popover on the iPad | PDFs and images (`printingItem`) | WebKit's print formatter | printed or cancelled |
| Android | `PrintManager`'s print screen | a PDF written through; an image fit to the page, upright (as `PrintHelper`) | the WebView's print adapter | printed, cancelled or failed (the job's state) |
| macOS | `NSPrintOperation`'s panel, a sheet on the window, with a preview | PDFKit (a PDF; an image on a page) | WebKit's print operation | printed or cancelled |
| Windows | WebView2's: the system's print dialog for HTML, the browser's preview for a WebView | the file's app prints it (the shell's `print` verb), whatever its type | WebView2 | unknown |
| Linux | GTK's print dialog (the print portal's for a file) | the printer takes it (CUPS: PDFs, images) | WebKitGTK's print operation | printed or cancelled |
| Web | the browser's print dialog | PDFs in the browser's viewer, images | the page's own web views | unknown |

An image wider than tall starts in landscape on Android and iOS, as their
photo printing does.

### PDFs from HTML

`print.PDF(markup, options)` lays markup out on pages, as `print.HTML`
prints it but without a dialog, and writes a PDF in the app's caches,
named after the page's `<title>` ("Receipt.pdf"; "Document.pdf" without
one). It returns its path, to share, mail or move. Its text stays text:
searchable, selectable. Not on the main thread.

```go
go func() {
	path, err := print.PDF(receipt, print.PDFOptions{Page: print.A4, Margins: print.Uniform(48)})
	ui.Post(func() {
		if err == nil {
			ui.ShareFiles([]string{path}, ui.ShareOptions{Title: "Receipt"}, nil)
		}
	})
}()
```

| Option | |
| --- | --- |
| `Page` | The paper, in points: `print.A3`, `A4`, `A5`, `Letter`, `Legal`, or a size of your own. Default: the user's region's, Letter in the Americas that use it (CLDR's paper sizes), A4 elsewhere. |
| `Landscape` | Turns the page: its width the longer side. |
| `Margins` | The space around each page's content, in points. Default: half an inch (36 points); `print.Uniform(0)` for none. |

iOS lays it out with WebKit (`UIPrintPageRenderer` with the web view's
print formatter), Android with its WebView's print adapter, the Mac with
WebKit's print operation, Windows with WebView2 (Chromium's
`Page.printToPDF`), Linux with WebKitGTK (printed to a file). A browser
makes PDFs only through its print dialog: there `PDF` fails, and
`print.HTML` lets the user Save as PDF.

## Mail and messages: asuka/compose

`asuka/compose` opens the system's mail and message composers, filled in
for the user to send. Main thread; `done` gets a `compose.Result`:
`compose.Sent`, `Saved` (a draft), `Cancelled`, or `Unknown` where
another app has the mail and doesn't say.

```go
compose.Mail(compose.MailMessage{
	To:      []string{"support@example.com"},
	Subject: "Order 1042",
	Body:    "<p>Hello,</p><p>The receipt is attached.</p>",
	HTML:    true,
	Attachments: []compose.Attachment{{Source: receiptPDF, Name: "Receipt 1042.pdf"}},
}, func(r compose.Result, err error) {
	if errors.Is(err, compose.ErrUnavailable) {
		status.Set("Set up a mail account to send it")
	}
})

compose.Message(compose.TextMessage{To: []string{"+15555550100"}, Body: "On my way"}, nil)
```

A `MailMessage` has `To`, `CC` and `BCC` addresses, a `Subject`, a `Body`
(markup if `HTML`) and `Attachments`; a `TextMessage` has `To` (phone
numbers, or iMessage addresses), a `Subject` (where Messages has subjects
on; elsewhere it goes before the body), a `Body` and `Attachments`. An
`Attachment` is a `Source` (a path, an asset, an http(s) URL), its `Type`
(default: its extension's) and the `Name` the recipient sees (default:
the file's). Where a mail can't carry markup (a `mailto:` link, Simple
MAPI), its body goes as text, as `compose.PlainText` makes it.

`compose.Check()` says what the device can do (not on the main thread):
`Mail`, a mail account or app; `MailComposer`, iOS's own composer, which
says what the user did and takes attachments; `Text`, text messages; and
`TextAttachments`, messages with attachments. `CanSendMail`,
`CanSendText` and `CanSendAttachments` read one each. A call the device
can't do fails with `compose.ErrUnavailable`.

| | Mail | Messages | Result |
| --- | --- | --- | --- |
| iOS | Mail's composer (`MFMailComposeViewController`) over the app; without a Mail account, a `mailto:` link for the mail app the user set (without attachments) | Messages' composer (`MFMessageComposeViewController`) | sent, saved, cancelled; unknown for a link |
| Android | the mail apps only: `ACTION_SENDTO` `mailto:`, or with attachments `ACTION_SEND` with `mailto:` as its selector, the files through the app's FileProvider; markup as spans (`EXTRA_TEXT`) and as `EXTRA_HTML_TEXT` | the default SMS app: `smsto:` with `sms_body`, or with attachments `ACTION_SEND` | unknown, once the user is back |
| macOS | a `mailto:` link for the user's mail app; with attachments, the mail share service (`NSSharingService`), which takes no CC or BCC (they fail with `ErrUnavailable`) | Messages (`NSSharingService`) | unknown, or cancelled |
| Windows | a `mailto:` link; with attachments, Simple MAPI (`MAPISendMailW`: Outlook, Thunderbird; the new Outlook and Windows's Mail app don't take it: `ErrUnavailable`) | `ErrUnavailable` | unknown, or cancelled (MAPI) |
| Linux | the email portal (the mail app's composer, with the attachments), else `xdg-email`, else a `mailto:` link | `ErrUnavailable` | unknown, or cancelled (the portal) |
| Web | a `mailto:` link; with attachments, the Web Share sheet | an `sms:` link; with attachments, the Web Share sheet | unknown |

The iOS simulator has no Mail account and no Messages: try the composers
on a device.

## Location: asuka/location

`asuka/location` is where the device is, now and as it moves, with the
user's permission, which it asks for the first time. It's Core Location
on iOS and the Mac, and Android's location manager, which needs no Google
Play services; WinRT's Geolocator on Windows, the location portal (or
GeoClue) on Linux, and the Geolocation API in a browser. It asks for the location while the app is in use, not in the
background.

`location.Current()` returns where the device is. Not on the main thread:
it waits for the user to answer, then for a fix. If the user hasn't
allowed it, the error is `location.ErrDenied`.

```go
ui.Button(ui.ButtonProps{Title: ui.Val("Where am I?"), OnPress: func() {
	go func() {
		p, err := location.Current()
		ui.Post(func() {
			switch {
			case errors.Is(err, location.ErrDenied):
				where.Set("Allow location in Settings")
			case err != nil:
				where.Set(err.Error())
			default:
				where.Set(fmt.Sprintf("%.4f, %.4f (within %.0f m)", p.Latitude, p.Longitude, p.Accuracy))
			}
		})
	}()
}})
```

`location.Watch(f)` calls `f` on the main thread with each position as the
device moves, the first being where it is now, until `stop` is called. It
asks as `Current` does; if the user refuses, `f` isn't called. Stop it
with the component:

```go
func Track() ui.Node {
	here := signal.New(location.Position{})
	stop := location.Watch(func(p location.Position) { here.Set(p) })
	signal.OnCleanup(stop)
	return ui.Text(signal.Func(func() string {
		p := here.Get()
		return fmt.Sprintf("%.5f, %.5f", p.Latitude, p.Longitude)
	}))
}
```

`location.PlaceName(lat, lon)` is what a place is called ("Mission District, San
Francisco"), from the platform's geocoder, which asks its servers: ask only when
the user wants the name. Not on the main thread. Windows asks
OpenStreetMap's Nominatim, Linux geocode-glib where it's installed; a
browser has no geocoder, and it fails there.

A `location.Position` has `Latitude`, `Longitude`, `Accuracy` (meters:
the radius the device is within), `Altitude` (meters above sea level; 0 if
not known) and `Time`.

**Android**: `Current` gives a fix from the last minute if there is one;
otherwise it waits up to 10 seconds for a new one, then gives the last
known, or fails with `"no location"`.

## Notifications: asuka/notify

`asuka/notify` shows local notifications, now or at a time the app sets,
whether or not the app is running then. Tapping one opens the app with
its URL. It also has what a messaging app needs, for `asuka/push`'s
notifications too: actions and replies typed in the notification,
threads, Android's channels, messages in conversations, the badge, the
notifications showing, and which ones show while the app is open.

```go
go func() {
	ok, err := notify.Request()
	if err != nil || !ok {
		return
	}
	err = notify.Schedule(notify.Notification{
		ID:    "water",
		Title: "Water the plants",
		Body:  "It's been a day.",
		URL:   "myapp://plants",
		After: 24 * time.Hour,
	})
	if err != nil {
		fmt.Println(err)
	}
}()
```

- `notify.Request()` asks the user to allow notifications the first
  time, and reports whether they're allowed: their answer then, or what
  they've set in Settings since. Not on the main thread.
- `notify.Schedule(n)` sets `n` to show `n.After` from now (at least a
  second); `notify.Show(n)` shows it now. One with the `ID` of one
  waiting or showing replaces it. Not on the main thread.
- `notify.Cancel(id)` takes the notification away, waiting or shown. On
  the main thread.

A `Notification`'s `URL` should be one of the app's scheme (`asuka.json`'s
`scheme`): `ui.OnOpenURL` gets it when the user taps the notification.
Its other fields:

| Field | What it does |
|---|---|
| `Subtitle` | A line under the title (iOS); Android's sub text beside the app's name |
| `Data` | The app's own keys, given back in `Showing`, `OnAction` and `Present` |
| `Category` | A category's ID: the actions it shows (below) |
| `Thread` | Groups it with the thread's others: iOS's thread, an Android group with a summary notification Asuka keeps |
| `Summary` | The line under an Android group's summary ("3 new messages"); iOS writes its own |
| `Channel` | The Android channel it's in (below); iOS has none |
| `Badge` | The app icon's number when it shows (iOS, macOS); Android's notification number |
| `Silent` | No sound |
| `Image` | A picture, as `ui.Image`'s sources (an asset, a file, an http(s) URL): iOS's attachment, Android's big picture |

**iOS** shows a notification while the app is open too, as a banner,
unless `Present` says otherwise. **Android** posts a scheduled one from an
inexact alarm (no exact-alarm permission needed), so it may come a
little after its time.

### Actions and replies

A category is a kind of notification and its buttons. Set the app's
categories as it starts, in its root component, before notifications
that name them come (a push's too: `aps.category` on iOS, the data's
`"category"` on Android). They hold across launches.

```go
notify.SetCategories(notify.Category{ID: "message", Actions: []notify.Action{
	{ID: "reply", Title: "Reply", Icon: "arrowshape.turn.up.left",
		Reply: &notify.Reply{Button: "Send", Placeholder: "Message"}},
	{ID: "read", Title: "Mark as Read"},
	{ID: "delete", Title: "Delete", Destructive: true, Unlock: true},
	{ID: "open", Title: "Open", Open: true},
}})

notify.OnAction(func(r notify.Response) {
	switch r.Action {
	case "": // a tap on the notification
		openChat(r.Notification.Thread)
	case "reply":
		_, done := r.Defer() // sending takes a moment
		go func() {
			defer done()
			api.Send(r.Notification.Thread, r.Reply)
		}()
	case "read":
		notify.RemoveThread(r.Notification.Thread)
	}
})
```

- An `Action` has its `ID` and `Title`, an `Icon` (an SF Symbol; iOS
  15 and macOS 12 show it, Android shows actions without icons),
  `Destructive` (iOS shows it red), `Unlock` (the device unlocks first:
  iOS, Android 12 and later), `Open` (it opens the app) and `Reply` (a
  text field, with its send button's title and placeholder).
- `notify.OnAction(f)` calls `f` on the main thread with each response,
  until `stop`: a tap (`Action` `""`) or an action, `Reply` the text
  typed, `Notification` the one answered (`asuka/push`'s too). A response
  that came before any `f` (the one that launched the app, or woke it)
  waits for the first, so set it as the app starts.
- An action without `Open` runs in the background: the app's Go code
  runs (started without a screen if the app isn't running: `main`, then
  the root component, which must call `OnAction`), and the app doesn't
  come to the front. Work that goes on after `f` returns calls
  `r.Defer()` in `f`, and `done` when it's finished: the system keeps the
  app running until then, or `ctx`'s deadline, a little before its own
  (iOS gives about 30 seconds, Android about 10).

**Android** takes a background action's notification away, as iOS does,
unless the app shows it again (`Show`, with its `ID`) by the time it's
done. After a reply, Android wants the notification posted again (its
spinner turns until then): Asuka adds the reply to a conversation's
notification, under the user's name, and takes another away.

### Threads and the notifications showing

A `Thread` groups a chat's notifications: iOS stacks them, Android puts
them in a group under a summary notification, which Asuka keeps as they
come and go.

- `notify.Showing()` lists the app's notifications that show (in
  Notification Center, the shade), its pushes' too, newest first: each
  `Delivered` has its `ID`, `Title`, `Subtitle`, `Body`, `URL`, `Data`,
  `Category`, `Thread`, `Time`, and `Remote` for a push. Not on the main
  thread.
- `notify.Remove(ids...)`, `notify.RemoveThread(thread)` (a chat that's
  read) and `notify.RemoveAll()` take them away. On the main thread.

### What shows while the app is open

```go
notify.Present(func(n notify.Delivered) notify.Presentation {
	if n.Thread == openChat.Peek() {
		return notify.Hide // the chat is on screen
	}
	return notify.Banner
})
```

`notify.Present(f)` has `f` say how each notification that comes while
the app is in the foreground shows: `Banner` (as with the app closed),
`Quiet` (in Notification Center, the shade, without a banner or sound)
or `Hide`. It runs on the main thread as the notification comes, and
must answer at once: the host waits a second at most, then shows it.
`nil` shows them all again. On the main thread.

### Messages in conversations

A message shows as the platform shows messages, with the sender's
picture: iOS's communication notification, Android's conversation (the
shade's Conversations section, with the conversation's shortcut).

```go
ana := notify.Person{ID: "u7", Name: "Ana", Image: "https://example.com/ana.jpg"}
chat := notify.Conversation{ID: "climbing", Title: "Climbing", Group: true}
go notify.Show(notify.Notification{Body: "Who's in for Saturday?", Category: "message"}.From(ana, chat))
```

- `n.From(sender, c)` makes `n` a message: `sender` said `n.Body` in
  `c`. Its `ID` and `Thread` default to the conversation's `ID` (a 1:1
  conversation's is the sender's), its `Title` to the sender's name (the
  group's title, for a group).
- A `Person` has an `ID` (the same each time), a `Name`, an `Image` (as
  `ui.Image`'s sources) and `Bot`. A `Conversation` has its `ID`, a
  group's `Title` and `Image`, `Group`, `Me` (the user as the
  conversation shows them; Android names their replies with it, "You" by
  default) and `Earlier` messages.
- **Android** shows a conversation as one notification, its messages in
  it: a message adds to the conversation's notification that shows (or
  `Earlier` sets what comes before it). **iOS** shows each message as a
  notification of its own, in the conversation's thread.
- **iOS** needs the Communication Notifications capability on the App
  ID: an app that calls `From` is given its entitlement and Info.plist's
  `NSUserActivityTypes` (`INSendMessageIntent`), and a release build's
  profile must have it. Without it, a message shows as a plain
  notification. **macOS** shows a message as a plain notification.

### Android's channels

A channel is a kind of notification the user can turn off or quiet in
the app's settings. Android shows a notification with its channel's
sound, vibration and importance; iOS has no channels, and ignores them.

```go
notify.SetChannelGroups(notify.ChannelGroup{ID: "chats", Name: "Chats"})
notify.SetChannels(
	notify.Channel{ID: "messages", Name: "Messages", Importance: notify.ImportanceHigh, Group: "chats"},
	notify.Channel{ID: "digest", Name: "Daily digest", Importance: notify.ImportanceLow, NoBadge: true},
)
```

- `notify.SetChannels(cs...)` makes the channels, renames the ones made
  before, and deletes those it made before that aren't in `cs`. A
  channel's `Importance` (`ImportanceDefault`, `ImportanceHigh`: heads-up,
  `ImportanceLow`: no sound, `ImportanceMin`), `Silent`, `NoVibration`
  and `NoBadge` are its when it's made; after, they're the user's to
  change. `notify.SetChannelGroups(gs...)` does the same for groups
  (deleting a group deletes its channels). On the main thread.
- A notification without a `Channel`, or with one that isn't, is in
  the default channel ("Notifications"), which stays. A push names its
  channel with FCM's `android.notification.channel_id`, or a data
  message's `"channel"`.

### The badge

`notify.SetBadge(n)` sets the number on the app's icon; `0` takes it
away. **iOS** and **macOS** (the Dock) show it as set. **Android** has no
badge of its own: launchers that badge count the app's notifications (or
show a dot), so the badge goes as they're read (`Remove`,
`RemoveThread`), and `SetBadge` does nothing; a notification's `Badge`
is the number a launcher may show. A channel with `NoBadge` doesn't
count.

### On each platform

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| Show, schedule, cancel | ✓ | ✓ | ✓ | ✓ (toasts) | ✓ (scheduled: a systemd user timer, whether the app runs or not) | ✓ (scheduled: while a page of the app is open) |
| Actions, replies | ✓ | ✓ | ✓ | ✓ (five buttons) | actions (a reply opens the app) | buttons where the browser shows them (Chrome, Edge: two); replies where it has inline replies |
| Background actions | ✓ (about 25 s) | ✓ (about 9 s) | ✓ | ✓ (about 25 s; started for it) | while the app runs | in a page of the app (the worker opens one) |
| Threads | ✓ | ✓ (a group and its summary) | ✓ | not grouped; `RemoveThread` | ✓ | not grouped; `RemoveThread` |
| Channels | ignored | ✓ | ignored | ignored | ignored | ignored |
| Conversations | ✓ (communication notifications) | ✓ (MessagingStyle, shortcuts) | plain | plain | plain | plain, the sender's picture its icon |
| Badge | ✓ | the launcher's | ✓ (the Dock) | the taskbar button's overlay | the launcher API's (Dash to Dock, Plasma) | the installed app's (`navigator.setAppBadge`) |
| Showing, removing | ✓ | ✓ | ✓ | ✓ (the notification center's) | what the app showed this run | ✓ (the service worker's) |
| Present | ✓ | ✓ | ✓ | ✓ (not scheduled ones) | ✓ | ✓ |

**macOS** needs the app's bundle, which `asuka dev` and `asuka build` run it
from: an executable run on its own (`go run`) has no bundle ID, and its
calls fail with an error that says so. **Windows** shows toasts, now or scheduled,
with their image, a category's actions as buttons and a reply as a text
box. The app registers itself the first time (a Start menu shortcut with
its AppUserModelID, asuka.json's `id`, and a COM activator: its executable,
in the user's registry), so that a tap or a button reaches the app,
which Windows starts for it if it isn't running; for a background
action it stays out of sight and quits once `OnAction` is done. The
badge is a count drawn on the app's taskbar button (a packaged app's,
the system's badge). Windows shows a scheduled toast however `Present`
would. **Linux** sends them as GNotifications
when the app has its desktop file (or runs in a sandbox), else to the
desktop's notification server itself (GNOME Shell turns away an app it
has no desktop file for); taps and buttons reach the app while it runs.
A notification due later is a systemd user timer (`systemd-run --user`),
which runs the app's executable without a window to show it, hands it
to the app if it runs, and launches the app for a tap; without systemd
(a Flatpak, `asuka dev`) it shows while the app runs. **A browser** shows
them through the app's service worker, with Web Push for `asuka/push`:
[the web](web.md) has the details.

## Sound: asuka/audio

`asuka/audio` plays sounds and music with the platform's player (AVPlayer
on iOS and the Mac; Media3's ExoPlayer on Android; MCI on Windows;
GStreamer's playbin on Linux; an audio element in a browser, which plays
only after the user has tapped or clicked in the page). A source is a file in the app's
`assets/`, a file the app wrote, or an http(s) URL, a file or a stream.

`audio.Play(source)` plays a sound once, from the start, and lets it go
when it ends: for a chime, a click. For more, `audio.Load` makes a
`*audio.Player`:

```go
func Sounds() ui.Node {
	music := audio.Load("music/theme.m4a")
	music.SetLoop(true)
	music.SetVolume(0.5)
	signal.OnCleanup(music.Release)
	return ui.Column(nil,
		ui.Button(ui.ButtonProps{Title: ui.Val("Ding"), OnPress: func() { audio.Play("sounds/ding.wav") }}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Play"), OnPress: music.Play}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Pause"), OnPress: music.Pause}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Restart"), OnPress: func() { music.Seek(0); music.Play() }}),
	)
}
```

A `Player`'s methods, all on the main thread: `Play` (from where it is;
after its end, from the start), `Pause`, `Seek(seconds)`, `SetVolume` (0 to
1, the default), `SetRate` (0.25 to 4, keeping the pitch), `SetLoop`,
`Status(done)` (its `Position`, `Duration`, `Playing` and `Ended`: read it
a few times a second to follow it) and `Release`, which stops it and frees
what it holds. Release a player when it's no longer needed.

The sound plays with iOS's silent switch on, as a music or game app's
does, and doesn't mix with other apps' audio.

## Network state: asuka/network

`asuka/network` is the device's network: whether it's online, over Wi-Fi or
cellular, and whether the connection costs. Use it to download large
files on Wi-Fi only, or to say the app is offline. It's NWPathMonitor on
iOS and the Mac, ConnectivityManager on Android, the Network List
Manager on Windows, GNetworkMonitor and NetworkManager on Linux, and
`navigator.onLine` in a browser, which says what carries it only where
it can (Chrome on Android).

`network.Use()` is the network as a signal, for a component: it follows
changes while the component lives.

```go
func Status() ui.Node {
	net := network.Use()
	return ui.Text(signal.Func(func() string {
		n := net.Get()
		if !n.Connected {
			return "Offline"
		}
		if n.Metered || n.Constrained {
			return n.Type.String() + ": large downloads wait for Wi-Fi"
		}
		return n.Type.String()
	}))
}
```

A `network.State` has:

- `Connected`.
- `Type`: `network.None`, `WiFi`, `Cellular`, `Ethernet` or `Other` (a
  VPN alone, Bluetooth...). Its `String` is `"none"`, `"wifi"`,
  `"cellular"`, `"ethernet"` or `"other"`.
- `Metered`: the connection costs or is limited, as mobile data or a
  phone's hotspot.
- `Constrained`: the user asked apps to use less data (iOS's Low Data
  Mode, Android's Data Saver, a browser's Data Saver), or, on Windows,
  the connection is near or over its data limit. Never on Linux.

Until the platform first answers, `Use`'s value is `Connected` with
`Type` `Other`.

`network.Current()` is the network now (not on the main thread), and
`network.Watch(f)` calls `f` on the main thread with the network now and
each time it changes, until `stop` is called.

## Saving to the library: asuka/library

`asuka/library` saves files where the user and other apps find them: a
video to the gallery, a song to the music, anything to Downloads.

```go
library.Save(file, library.Options{Collection: library.Movies, Folder: "My App", Move: true},
	func(where string, err error) {
		if err != nil {
			status.Set("Couldn't save: " + err.Error())
			return
		}
		status.Set("Saved")
	})
```

`library.Save(path, o, done)` copies the file into `o.Collection`, off the
main thread (a large video takes a while), then calls `done` on the main
thread with where it went. It asks for what it needs the first time.
`done` may be nil.

`library.Options`:

- `Collection`: `library.Downloads` (the default), `Movies`, `Music` or
  `Pictures`.
- `Name`: the file's name there (default: its own). A name taken gets a
  number.
- `Folder`: a folder of the collection's for the app's files; empty: the
  collection itself.
- `MIME`: its type (default: from its extension).
- `Move`: remove the file once it's saved, for a download in the app's
  caches: no second copy.

Where each collection goes:

| Collection | Android | iOS | macOS | Windows, Linux |
| --- | --- | --- | --- | --- |
| `Downloads` | Downloads | the app's Documents, in the Files app | `~/Downloads` | the user's Downloads |
| `Movies` | the gallery's videos | Photos | Photos | the user's Videos |
| `Music` | Music | the app's Documents, in the Files app | `~/Music` | the user's Music |
| `Pictures` | the gallery's pictures | Photos | Photos | the user's Pictures |

On Windows and Linux, and in `~/Downloads` and `~/Music` on a Mac, the
file goes under `Folder` in the user's folder, answering with its path;
Linux's folders are XDG's. A browser downloads the file, whatever the
collection: it lands where the browser puts downloads.

**Android** uses the MediaStore collections from Android 10, with no
permission, and answers with a `content://` URI. Before Android 10, it
writes to the shared folders after asking for the storage permission.

**iOS** asks to add to Photos (add only: the app can't read the library)
and answers with the asset's identifier; `Folder` doesn't apply there.
Other collections go to the app's Documents, under `Folder`, answering
with the file's path; Asuka makes Documents visible in the Files app.

## Background work: asuka/background

`asuka/background` keeps the app working while it's out of sight: a
download, an upload, an export the user started, which takes minutes.

`background.Start` starts a task: the app keeps running, with a
notification showing its `Options`, until `End`. `onAction` gets the ID
of a notification button the user taps, on the main thread.
`task.Update` shows new options, its progress as it goes. Start a task
while the app is on screen, as the user asks for the work.

```go
var cancelled atomic.Bool
task := background.Start(background.Options{
	Title:    "Exporting",
	Progress: -1,
	Actions:  []background.Action{{ID: "cancel", Title: "Cancel"}},
}, func(action string) {
	if action == "cancel" {
		cancelled.Store(true)
	}
})
go func() {
	for i := 1; i <= 10 && !cancelled.Load(); i++ {
		time.Sleep(time.Second) // the work
		p := float64(i) / 10
		ui.Post(func() {
			task.Update(background.Options{Title: ui.Val("Exporting"), Text: fmt.Sprintf("%.0f%%", p*100), Progress: p,
				Actions: []background.Action{{ID: "cancel", Title: "Cancel"}}})
		})
	}
	ui.Post(func() {
		task.End()
		if cancelled.Load() {
			status.Set("Cancelled")
		}
	})
}()
```

`background.Options`:

- `Title` and `Text`.
- `Progress`: 0 to 1; negative: under way, how far not known.
- `Actions`: the notification's buttons, each an `ID` and a `Title`.

`Start`, `Update` and `End` run on the main thread; do the work in a
goroutine. Each `Update` replaces all the options, so give the `Actions`
again. [`examples/downloader`](../examples/downloader) downloads in a
task, with Pause, Resume and Cancel.

**Android** stops a backgrounded app within minutes. A task keeps it
running in a foreground service (of the data sync type), with an ongoing
notification for each task, showing its progress and up to three
buttons, and keeps the CPU awake. On Android 13 and later, starting a
task asks the user to allow notifications if they haven't; the work goes
on either way. Android doesn't let an app in the background start one.
Android 15 limits a data sync service to six hours a day. Google Play
asks, in the Play Console, what the app's data sync foreground service is
for: declare it before you upload.

**iOS** gives a backgrounded app about 30 seconds more while a task
lives, and shows no notification: `Update` does nothing there, and
`onAction` is never called. Out of time, iOS ends the task and the work
may stop.

**A desktop** app keeps running out of sight: a task keeps the system
from slowing or suspending it (App Nap on a Mac, sleep on Windows and
Linux), with no notification: `Update` does nothing, and `onAction` is
never called. **A browser** runs the page while it's open, with or
without a task.

## Shares from other apps: ui.OnShare

`ui.OnShare(f)` calls `f` with what another app shares with this one
from its share sheet: a link from YouTube or a browser, a text from
Notes, photos from the gallery, a PDF from Files. It's iOS's and
Android's: on the desktops and in a browser the app isn't offered in
other apps' share menus, and `f` isn't called. `asuka.json`'s
`shareTarget` says what the app is offered for:

```json
{ "shareTarget": true }
```

takes texts and links. An object takes files too, by MIME type, and at
most `max` at once (default 1):

```json
{ "shareTarget": { "types": ["text", "image/*", "video/*"], "max": 10 } }
```

A type is `"text"` (texts and web links) or a MIME type: `"image/*"`,
`"video/*"`, `"audio/*"`, `"application/pdf"`, `"*/*"` for any file.

```go
func App() ui.Node {
	link := signal.New("")
	photos := signal.New([]string(nil))
	ui.OnShare(func(items []ui.SharedItem) {
		for _, it := range items {
			switch it.Kind {
			case ui.SharedLink:
				link.Set(it.Text)
			case ui.SharedImage:
				photos.Set(append(photos.Peek(), it.Path))
			}
		}
	})
	return ui.Text(link)
}
```

Each `ui.SharedItem` has a `Kind`: `SharedText`, `SharedLink`,
`SharedImage`, `SharedVideo`, `SharedAudio` or `SharedFile`. A text or a
link is in `Text`. A file is at `Path`, the app's own copy in its caches:
move it where the app keeps it, or remove it when you're done. It comes
with `Name`, its name in the app it came from (`IMG_0042.HEIC`), its
`MIME` type and its `Size` (-1 if not known). The items come in the
order the other app gave them. A page shared from a browser comes as its
title, a text, and its link.

What came before any handler, the share that launched the app, goes to
the first one. The handler stops with the component's owner. To show a
screen of the app's for what's shared, without opening it, see the
share sheet below; to open one in the app, see [Navigation](navigation.md).

**Android** opens the app itself with the share (`ACTION_SEND`, and
`ACTION_SEND_MULTIPLE` when `max` is over 1), for the types' intent
filters. The app copies the files to its caches before `OnShare` gets
them, while its access to them still holds.

**iOS** has a share extension in the app, which opens the app with what's
shared, as Android opens it: the extension shows no screen of its own.
Its activation rule comes from the types: Apple's keys for texts,
links, images, videos and any file, which keep `max`; a predicate on the
attachments' types for others (audio, a PDF), which doesn't. The
extension takes whatever it's given, so check `Kind` and `MIME`. Files
go from the extension to the app through an App Group,
`group.<app ID>`, which a device build's profiles must have (see
[Shipping](shipping.md#ios)).

Under `asuka dev`, the player takes shares of anything, whatever the
app's `shareTarget`. On Android the files' paths are on the device, not
the Mac the Go code runs on: try files with `asuka dev -platform android -once`.

What a link leads to (a page, an image, a video) is the app's to find
out, with `net/http`: expo-sharing's "resolved" payloads, in Go.

### The app's own share sheet: app.ShareSheet

By default a share opens the app. `app.ShareSheet(screen)` shows the
app's own screen instead, over the app that shares, as "Save to
Raindrop" or "Add to Notes" does: the user saves, and is back where they
were. Call it before `app.Run`.

```go
func ShareSheet(s *ui.ShareSheet) ui.Node {
	note := signal.New("")
	return ui.Column(ui.Style{Grow: 1, Padding: ui.All(16), Gap: 12},
		ui.Text(signal.Static(s.Items[0].Text)),
		ui.TextField(ui.TextFieldProps{Value: note, Placeholder: ui.Val("Note"), Multiline: true}),
		ui.Row(ui.Style{Gap: 12},
			ui.Button(ui.ButtonProps{Title: ui.Val("Cancel"), OnPress: s.Cancel}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonFilled, OnPress: func() {
				save(s.Items, note.Peek())
				s.Close()
			}}),
		),
	)
}

func main() {
	app.ShareSheet(ShareSheet)
	app.Run(App)
}
```

`s.Items` is what's shared, as `OnShare` has it. The sheet ends with
`s.Close()` (done), `s.Cancel()`, or `s.OpenApp()`, which opens the app
with the items for its `OnShare`, as without a sheet. Until the items
have come (a video being copied) it shows a spinner.

The sheet is a root of its own, in a process of its own, as iOS runs
share extensions: on **iOS** the app's share extension, which iOS shows
as a sheet, running the app's Go code and the host from the framework it
shares with the app; on
**Android** an Activity in a `:share` process, shown as a Material
bottom sheet over the other app (dragging it down, tapping outside and
back cancel it). The process ends with the sheet: what's pending in
`asuka/store` is written as it closes, and `app.OnLifecycle` hears
`Background`. A file's `Path` is the sheet's until it closes: copy what
the app keeps.

What the app keeps is the same in its sheet: `asuka/store`, SQLite files
in `store.Dir()` (`sqlite.Open("app.db")`), `asuka/secure`. Android's
two processes share the app's files. On iOS the app's data moves to the
App Group (`group.<app ID>`), once, the first time it runs with a sheet,
and its keychain group is shared with the extension. The app doesn't
hear what its sheet wrote while it was running: look again when it
comes back (`app.OnLifecycle`, `db.Changed()`).

The CLI sees that the app's Go code calls `app.ShareSheet` and builds
the sheet instead of the extension that opens the app. On iOS the app's
Go code and the host are then a framework in the app,
`Frameworks/AsukaApp.framework`, which the app and its extension both
load, as Xcode shares code with an extension: the app isn't bigger for
its sheet. Under `asuka dev` the player's sheet runs the app's sheet from
the Mac too. [`examples/bookmarks`](../examples/bookmarks) is a
sheet as Raindrop's: the page's title, description and image, a note, a
collection and tags.

## JavaScript: asuka/js

`asuka/js` runs JavaScript off screen, in the platform's own engine: a web
view that's never shown (WKWebView on iOS and the Mac, Android's WebView,
WebView2 on Windows, WebKitGTK on Linux), or in a browser a hidden,
sandboxed frame. It's for code
that only has a JavaScript form, as a site's player whose functions sign
its video URLs.

`js.Eval(ctx, script)` runs a script, statements as a `<script>`'s, in a
fresh page of its own, and returns its value as JSON: the last
statement's value, or what the promise it returns resolves to (`null` for
`undefined`). A thrown error, or a rejected promise, is the error, a
`*js.Error`. The page is gone after: what one script sets isn't there for
the next. Not on the main thread.

```go
go func() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	v, err := js.Eval(ctx, `const xs = [1, 2, 3].map(x => x * 2); xs`)
	fmt.Println(string(v), err) // [2,4,6] <nil>

	s, err := js.EvalString(ctx, `new Promise(done => setTimeout(() => done("later"), 100))`)
	fmt.Println(s, err) // later <nil>
}()
```

`js.EvalString` is `Eval` for a script whose value is a string. A script
may run until `ctx`'s deadline, or for `js.DefaultTimeout` (a minute) if
it has none; one that runs out fails with `"timeout"`. On Android a
thrown error's message has its stack.

For script in a page the user sees, use `ui.WebView`
([Web pages](components.md#web-pages)).

## Push notifications: asuka/push

`push.Register()` asks the user to allow notifications (the first time),
registers the device with Apple's (APNs) or Google's (Firebase Cloud
Messaging) service, and reports its `Token`, its `Service` (`"apns"`,
`"fcm"`) and whether notifications are `Allowed`. Send the token to your
server, which sends notifications to it; register at each launch, since
the token can change. Not on the main thread. A Mac registers with APNs
as iOS does (with a provisioning profile: see [On a Mac](#on-a-mac)). On
the web it's Web Push (`"webpush"`, the token the browser's
subscription; see [the web](web.md)). Windows and Linux have no push
service for an app: `Register` fails there.

```go
func App() ui.Node {
	push.OnOpen(func(m push.Message) { openThread(m.Data["thread"]) })
	go func() {
		if r, err := push.Register(); err == nil {
			api.SaveDeviceToken(r.Service, r.Token)
		}
	}()
	return home()
}
```

- `push.OnReceive(f)` gets each message that comes while the app runs.
  It's shown as a notification too, in the foreground as well, if it has
  a title or body. Data messages (silent pushes) come here only.
- `push.OnOpen(f)` gets each notification the user taps, including the
  one that launched the app, if `f` is set as the app starts. A
  message's data `"url"` also goes to `ui.OnOpenURL`, as
  `asuka/notify`'s do.
- A `push.Message` has its `ID`, `Title`, `Body` and `Data` (iOS: the
  payload's keys beside `aps`; Android: the message's data).

iOS needs the Push Notifications capability on the App ID for a release
build; the simulator gets a token too. Android needs a Firebase project:
add the Android app to it, download its `google-services.json`, and name
it in `asuka.json`:

```json
{ "android": { "googleServices": "google-services.json" } }
```

Asuka writes its settings into the app as Google's Gradle plugin would.
Without it, `Register` fails with what to do.

### Categories, threads and channels in a push

A push uses `asuka/notify`'s categories (its actions, `notify.OnAction`),
threads, channels, badge and `Present`, named in its payload:

- **iOS**: `aps`'s `category`, `thread-id` and `badge`, as APNs has them.
- **Android**: a notification message's `android.notification.channel_id`
  and `image`; a data message's `"title"`, `"body"`, `"subtitle"`,
  `"category"`, `"thread"`, `"summary"`, `"channel"`, `"image"`, `"badge"`
  and `"url"`. A data message with a `"title"` or `"body"` shows as a
  notification (Asuka builds it, with its actions); one without is
  silent (`OnReceive` only). A notification message that comes while
  the app is in the background is shown by Firebase, without actions or
  a thread: send data messages for them.

A `push.Message` has these too: `Subtitle`, `Category`, `Thread`,
`Channel`, `Image`, `Badge`, `Summary`.

### Rewriting a push before it shows

`push.Rewrite(f)` has the app's Go code change each push before it
shows: decrypt an end-to-end encrypted payload, set its title and body,
attach an image, make it a message in a conversation with the sender's
picture, set its thread and category. Call it in `main`, before
`app.Run`.

```go
func main() {
	push.Rewrite(func(m *push.Message) error {
		text, from, err := decrypt(m.Data["box"]) // with a key in asuka/secure
		if err != nil {
			return err // shows the push as it came
		}
		m.Body = text
		m.Category = "message"
		m.From(notify.Person{ID: from.ID, Name: from.Name, Image: from.Avatar},
			notify.Conversation{ID: m.Data["chat"]})
		return nil
	})
	app.Run(App)
}
```

`f` runs on its own goroutine, not the main thread. An error, or a
panic, shows the push as it came. `m.Image` is downloaded and attached;
`m.From` makes it a message, as `notify.Notification`'s `From` does.

**iOS** rewrites in a Notification Service Extension, which `asuka build`
makes for an app that calls `push.Rewrite`: it runs the app's Go code in
a process of its own for each push with `"mutable-content": 1` in its
`aps` (and an `alert`, which shows if the rewrite fails). `main` runs,
then `f`; the root component doesn't, so keep `main` light. iOS gives it
about 30 seconds and 24 MB, and shows the push as it came when time runs
out. What the app keeps is the same there: its data (`asuka/store`, files
in `store.Dir`) is in the App Group the two share, its keychain items
(`asuka/secure`) in the app's group. A release build needs a provisioning
profile for the extension's bundle ID, the app's plus `.notification`
(`asuka.json`'s `ios.notificationProfile` names one; by default, the best
match installed), with the App Groups capability and the app's group.
A rewrite can't hide a push on iOS.

**Android** rewrites FCM's data messages in the app's process: the Go
code starts without a screen if the app isn't running, rewrites within
about ten seconds, and the notification shows. A data message whose
rewrite leaves no title or body doesn't show.

Under `asuka dev`'s player there's no extension (the app's Go code runs on
the computer): pushes show as they came. `asuka dev -platform ios -once` and a
build have it. The simulator runs the extension only for pushes that
come from APNs, not for `xcrun simctl push`.

## Sign in with Apple: asuka/appleauth

`appleauth.Button` is Apple's own button (its title, logo and colors, in
the user's language), and `appleauth.SignIn` shows the system's sheet.
The App Store requires Sign in with Apple of an app that offers another
company's sign-in for its accounts.

```go
appleauth.Button(appleauth.ButtonProps{
	Type:  appleauth.ContinueButton,
	Style: appleauth.Automatic, // black in light mode, white in dark
	OnPress: func() {
		go func() {
			c, err := appleauth.SignIn(appleauth.Options{Scopes: appleauth.FullName | appleauth.Email, Nonce: nonce})
			if err == nil {
				api.SignInWithApple(c.IdentityToken, c.AuthorizationCode)
			}
		}()
	},
})
```

The `Credential`'s `User` is the account's stable ID; send its
`IdentityToken` (a JWT Apple signs) and `AuthorizationCode` to your
server, which checks them with Apple. `Email`, `GivenName` and
`FamilyName` come only the first time the user signs in to the app: keep
them then. `ErrCanceled` is the user closing the sheet.
`appleauth.CredentialState(user)` says whether the user still uses Sign
in with Apple for the app (`Revoked`: sign them out). On Android there's
none: `Button` is nothing, `Available` is false and `SignIn` fails with
`ErrNotAvailable`; so on Windows, Linux and the web. Sign in with Apple
there on the web (`asuka/webauth`). The Mac has it as iOS does, with a
provisioning profile (see [On a Mac](#on-a-mac)).

## Face ID and fingerprints: asuka/biometric

`biometric.Authenticate(options)` shows the system's prompt (Face ID,
Touch ID, Android's BiometricPrompt; Touch ID on a Mac, Windows Hello on
Windows, a passkey's WebAuthn prompt in a browser) and returns nil if it's the device's
owner, or `ErrCanceled`, `ErrNotAvailable` (no biometry and no passcode,
or locked out) or `ErrFailed`. Not on the main thread.

```go
go func() {
	err := biometric.Authenticate(biometric.Options{Reason: "Unlock your notes"})
	ui.Post(func() { unlocked.Set(err == nil) })
}()
```

- `Reason` says why (iOS's subtitle; Android's title); `Cancel` is the
  cancel button's title.
- The user can use the device's passcode, PIN or pattern instead, unless
  `NoPasscode`: biometry only (on Android, a strong one, Class 3).
- `biometric.Check()` says what the device has: its `Kind` (`Face`,
  `Fingerprint`, `Iris`, `Optic`), whether one is `Enrolled`, and whether
  it has a `Passcode`.

Linux has no system prompt for the owner: `Check` says there's nothing,
and `Authenticate` fails with `ErrNotAvailable`.

## Recording sound: asuka/audio/record

`record.Start(options, done)` records from the microphone until
`record.Stop` (or `MaxDuration`), then calls `done` with the file: AAC in
an MPEG-4 container (`.m4a`), in the app's caches, which `asuka/audio`
plays. AVAudioRecorder records on iOS and the Mac, Media Foundation on
Windows, GStreamer on Linux, and `MediaRecorder` in a browser (Opus in
WebM where it can't record AAC). It asks for the microphone the first time; refused, `done` gets
`"not allowed"`.

```go
record.Start(record.Options{Quality: record.Voice, MaxDuration: time.Minute}, func(path string, err error) {
	if err == nil {
		memo.Set(path)
	}
})
// later
record.Stop()
```

`Voice` is mono at 64 kbit/s; `Music`, stereo at 128 kbit/s. `Pause` and
`Resume` pause and continue the same file. `record.Level()`, from a
goroutine a few times a second, is the sound's level from 0 to 1, for a
meter. `record.CurrentStatus(done)`, from the main thread, is the level,
whether sound is being kept and how much has been: what a timer should
show, as the system can pause a recording (a call), and iOS goes on after
when it says it may. On iOS the audio session plays and records while it
records (other apps' sound stops), and goes back after.

`Options.Stream` records an ADTS stream (`.aac`), whose frames are each whole,
so what's recorded survives the app or phone dying, where an MPEG-4 file is
unreadable until it's closed. Give it a `Path` to find the file again after a
crash, and make it an `.m4a` for keeping with `record.Finalize(stream, dst)`,
which seeks properly: after a stop or a crash alike.

`Options.Background` keeps it recording out of sight and with the screen
locked. iOS needs `"ios": {"backgroundModes": ["audio"]}` in `asuka.json`;
Android runs a foreground service of the microphone type, with an ongoing
notification (`BackgroundTitle`) and a button to stop. Google Play asks
what the service is for: declare it before you upload. A desktop app
records out of sight anyway: there `Background` changes nothing.

## Speech to text: asuka/transcribe

`transcribe.File(path, options)` turns a recording into text, on the
device: the sound never leaves it. Where the device can't do that, it says
`ErrNotAvailable`, rather than using a server. Not on the main thread.

```go
go func() {
	r, err := transcribe.File(path, transcribe.Options{Language: "en-US"})
	ui.Post(func() {
		if err == nil {
			text.Set(r.Text)
		}
	})
}()
```

The `Result` has its `Text`, its `Language`, and `Segments`, words with
their `Start` and `Duration` in seconds, on iOS and Android 14. A recording
with no speech is an empty `Text`. `transcribe.Check(language)` says what
the device can do, without asking anything of the user. iOS uses the Speech
framework (its models are in iOS, not on the simulator's every runtime); it
asks for speech recognition the first time. Android's on-device recognizer
needs Android 13; it needs the language's model, which it fetches when it's
missing (`ErrDownloading`: try again later). The Mac uses the Speech
framework too, on the device only. Windows, Linux and the web have no
on-device recognizer an app can use: `File` fails with `ErrNotAvailable`.

## The screen: asuka/screen

```go
screen.Lock(screen.Landscape) // a video player
screen.SetBars(screen.Bars{Style: screen.Light, HideStatus: true})
screen.PreventCapture(true) // a ticket's code
stop := screen.OnScreenshot(func() { note.Set("That screenshot shows your balance") })
```

- `screen.Lock(o)` turns the app to `o` and keeps it there: `Portrait`,
  `PortraitUp`, `Landscape`, `LandscapeLeft`, `LandscapeRight`; `Any`
  unlocks it. Only the orientations `asuka.json` allows. iPads in Split
  View or Stage Manager, and Android 16's large screens, ignore it.
- `screen.SetBars(bars)` sets the status bar's `Style` (`Auto` follows
  dark mode; `Light` is light icons, for a dark picture under them;
  `Dark`, dark ones), and hides the status bar (`HideStatus`) or
  Android's navigation bar (`HideNavigation`; a swipe from the edge shows
  them a moment). The safe area follows.
- `screen.PreventCapture(on)` keeps the app out of screenshots and
  recordings. Android makes the window secure: captures are black, and
  the recent apps show it blank. iOS can't stop a screenshot: it covers
  the app while the screen is recorded, mirrored or on AirPlay.
- `screen.OnScreenshot(f)` calls `f` when the user takes a screenshot
  (iOS; Android 14 and later), until `stop`.

A desktop window has no orientation or system bars: `Lock` and `SetBars`
do nothing there. `PreventCapture` keeps the app's windows out of
screenshots and recordings on the Mac and Windows (not on Linux), and
`OnScreenshot` hears of screenshots as they're saved (Spotlight's on a
Mac, the Screenshots folder on Windows and Linux). A browser locks the
orientation only full screen or in an installed app, hides the bars by
going full screen, and has neither `PreventCapture` nor `OnScreenshot`.

These are the app's, not a screen's: a screen that sets one sets it back
as it goes (`signal.OnCleanup`).

## The launch screen: asuka/splash

`asuka.json`'s `splash` is the launch screen: an image (a square PNG, a
logo), `size` points wide (default 120, at most 160), in the middle of a
color, in light and dark mode.

```json
{ "splash": { "image": "logo.png", "size": 96, "background": "#4A63B5", "darkBackground": "#1B2440" } }
```

It shows from the tap on the icon until the app's first frame: iOS's
launch screen, then a copy of it over the app until the first frame is
ready; Android 12's splash screen (the image in the middle of the icon's
circle) and, before, the window's background. The desktops and the web
have no launch screen: there `Hold` and `Hide` do nothing. To keep it up
while the app loads, call `splash.Hold()` as it starts (in the root component) and
`splash.Hide()` once it's ready:

```go
func App() ui.Node {
	splash.Hold()
	go func() {
		load()
		ui.Post(splash.Hide)
	}()
	return home()
}
```

## Maps: asuka/maps

`maps.View` is a map: Apple Maps on iOS and the Mac, Google Maps on
Android, OpenStreetMap's tiles on Windows and in a browser (drawn by the
host) and on Linux (libshumate).

```go
region := signal.New(maps.Region{Center: maps.Coordinate{Lat: 37.77, Lng: -122.42}, LatDelta: 0.05, LngDelta: 0.05})
maps.View(maps.Props{
	Region:         region,
	Markers:        ui.Val([]maps.Marker{{ID: "hq", At: maps.Coordinate{Lat: 37.79, Lng: -122.39}, Title: "HQ"}}),
	OnRegionChange: func(r maps.Region) { region.Set(r) },
	OnMarkerPress:  func(id string) { show(id) },
	Style:          ui.Style{Grow: 1},
})
```

- `Region` is where it shows, its center and the degrees it spans;
  setting it moves the map there. `OnRegionChange` gets the region once
  the user has moved it.
- `Markers` are pins, with a `Title`, `Subtitle` and `Color`;
  `OnMarkerPress` gets a tapped one's `ID`, and `OnPress` where the user
  taps the map.
- `Type` is `Standard`, `Satellite`, `Hybrid` or `Muted` (iOS);
  `ShowsUser` shows the user's location, with the location permission
  (import `asuka/location` and ask for it first); `NoInteraction` makes
  it a still preview.
- A map has no size of its own: give it one, or `Grow`.

Android needs a Google Maps Platform API key with the Maps SDK for
Android, in `asuka.json`: `{"android": {"mapsKey": "..."}}`. Without it,
the map says so, and `OnError` gets why. On Windows, Linux and the web,
Satellite and Hybrid show the streets (no free imagery); `ShowsUser`
shows nothing on Windows and Linux, and the user's location in a browser
once the page has the location permission. `Muted` is quieter streets
on iOS and the web. Linux needs libshumate (`libshumate-1.0-1`). The
tiles come from `tile.openstreetmap.org`, as its usage policy asks, with
its attribution on the map; a web app can take them from a server of its
own ([the web](web.md)).

## Background refresh: asuka/periodic

`asuka/periodic` runs the app's Go code now and then while it isn't on
screen: a sync, a fetch of what's new. `Handle` sets a task's code, in
the root component; `Schedule` asks the system to run it.

```go
func App() ui.Node {
	periodic.Handle("sync", func(ctx context.Context) error {
		return syncInbox(ctx)
	})
	periodic.Schedule("sync", periodic.Options{Every: time.Hour, Network: true})
	return home()
}
```

The system decides when, from `Every` (15 minutes at least), the
battery, the network, and how the user uses the app; it may wait much
longer, and the user can turn it off (iOS's Background App Refresh).
`Network` runs it only online and `Charging` only while charging. The
`ctx` ends before the system's deadline: about 25 seconds on iOS (a
processing task, for `Charging`, has minutes), 9 minutes on Android
(WorkManager). An error asks Android to retry it.

When the system runs a task with the app closed, the app's Go code
starts without a screen: `main` runs, and the root component (which
calls `Handle`), then the task. If the user opens the app then, its
screen appears. `Cancel(name)` stops a task.

A desktop doesn't launch an app for its tasks: on macOS
(`NSBackgroundActivityScheduler`), Windows and Linux they run on a timer
while the app runs, and one that wants power waits while on the battery.
A browser runs no page's tasks while it's closed: `Schedule` fails there.

## The stores: asuka/review, asuka/tracking, asuka/integrity, asuka/agerange

`review.Request(done)` asks the store for its rating prompt (the App
Store's, Google Play's in-app review). The stores decide whether it
shows, on their own quotas, and don't say; ask after a success, never
from a button. For a "Rate us" button, `ui.OpenURL(review.AppStoreURL(id))`
or `review.PlayStoreURL(id)`.

`tracking.Request()` shows iOS's App Tracking Transparency prompt, which
an app must before it reads the advertising identifier or shares data
with ad networks, and reports `Allowed`, `Denied`, `Restricted` or
`NotAsked`; `tracking.Current()` reads it without asking. Android has no
such prompt: it's `Allowed`. Not on the main thread.

`asuka/integrity` proves to your server that a request comes from your
app on a real device. iOS: App Attest. `GenerateKey` makes a key (keep
its ID), `AttestKey(id, challenge)` has Apple attest it for your server's
challenge, once, and `Assert(id, data)` signs each request after. Android:
Play Integrity. `Prepare(cloudProjectNumber)` at launch, then
`Token(requestHash)` for each request. The server checks what they
return with Apple or Google. `Supported` is false on the iOS simulator.

`agerange.Request(13, 18)` asks for the user's age range without their
birth date, for the laws that ask apps to know a child: Apple's Declared
Age Range (iOS 26) and Play's Age Signals. The platform may ask the user
or their parent to share. The `Result` is `Shared` with a `Lower` and
`Upper` age (0: open), `Declined`, or `VerificationRequired` (Play); its
`Source` says how it's known. `ErrNotAvailable` where the platform can't
answer.

Off the phones: the Mac has the App Store's rating prompt, the tracking
prompt, and App Attest (with a provisioning profile: see
[On a Mac](#on-a-mac)), but not Declared Age Range. Windows, Linux and
the web have no store prompt (`review.Request` shows nothing; in a
browser `done` gets an error), no attestation (`integrity.Supported` is
false) and no age range (`ErrNotAvailable`). Tracking needs no prompt
there: Windows answers `Allowed` while the advertising ID is on in
Settings, Linux `Allowed`, a browser `Denied` when it sends Global
Privacy Control or Do Not Track.

## Motion and sensors: asuka/sensors

```go
stop := sensors.WatchMotion(50*time.Millisecond, func(m sensors.MotionReading) {
	level.Set(m.Attitude.Roll) // a spirit level
})
```

- `WatchAccelerometer`, `WatchGyroscope` and `WatchMagnetometer` give a
  `Vector` (x, y, z) about every interval: acceleration in g, rotation in
  radians per second, the magnetic field in microteslas (calibrated).
- `WatchMotion` is the fused device motion: `Gravity` and the user's
  `Acceleration` (in g), `Rotation`, and the `Attitude`: a `Matrix` that
  turns device axes into East-North-Up, the `Heading` (degrees from
  magnetic north; -1 without a magnetometer), and `Pitch` and `Roll`.
- `WatchPressure` is the barometer (hectopascals, and the change in
  altitude), `WatchLight` the ambient light in lux (Android), and
  `WatchSteps` the steps since watching began. `StepsBetween(from, to)`
  is iOS's history (a week); Android keeps none.
- `Available(sensor)` says whether the device has one; the simulator and
  emulators have few. A Mac and a PC have none; Linux has the light
  sensor and the accelerometer's orientation on convertibles and tablets
  (iio-sensor-proxy); a browser has the Generic Sensor API or device
  motion events, without a barometer or pedometer.

The values are the same on both platforms: device axes as Android and the
W3C define them (x to the right of the screen, y up it, z out of it),
acceleration as the accelerometer measures it (a device lying face up
reads z = +1 g; iOS's are turned to match), and the attitude computed
the same way on both. Up to 200 readings a second; readings come on the
main thread, in batches as the platform delivers them.

## Editing images and video frames: asuka/imageedit

`imageedit.Edit(source, actions, options)` reads an image (HEIC, JPEG,
PNG, WebP: what the camera and the photo picker give), turns it upright
as its orientation says, does the actions in order and writes a JPEG or
PNG to the app's caches. Not on the main thread.

```go
go func() {
	r, err := imageedit.Edit(photo, []imageedit.Action{
		imageedit.Resize(1080, 0), // 1080 wide; 0 keeps the aspect ratio
		imageedit.Crop(0, 0, 1080, 1080),
	}, imageedit.Options{Quality: 0.8})
	if err == nil {
		upload(r.Path, r.Width, r.Height)
	}
}()
```

The actions are `Resize`, `Crop`, `Rotate` (clockwise, any angle),
`FlipHorizontal`, `FlipVertical`, `Fill` and `Pixelate`. `Fill(x, y, w, h, r, g,
b)` paints a rectangle a solid color, and what was there is gone from the file;
`Pixelate(x, y, w, h, block)` turns one into squares, each the average of what's
under it, aligned to the image, so rectangles that touch look as one. Text under
small squares can sometimes be read back: for what must stay hidden, use `Fill`.
The file written has none of the source's metadata: no location, no camera. A first `Resize` decodes the image
near that size, which saves memory on large photos. `imageedit.Size`
reads an image's size without decoding it. `imageedit.VideoFrame(source,
at, actions, options)` takes a video's frame at a time (a thumbnail, a
poster), upright, and edits it the same way; a remote video is read only
as far as the frame needs. A source is what `ui.Image` takes: a path, a
`file://` or `http(s)` URL, or an asset.

Each platform decodes with its own codecs: ImageIO on iOS and the Mac,
Android's, WIC on Windows, GdkPixbuf's loaders on Linux (HEIC and WebP
where their codecs or loaders are installed), a canvas in a browser
(JPEG or PNG; a remote image needs CORS).

## Text to speech: asuka/speech

`speech.Speak(text, options, done)` queues text to be read aloud with the
platform's voices; `done` gets nil once it's spoken, or `ErrStopped`.

```go
speech.Speak("Turn left in 200 meters", speech.Options{Rate: 1.1}, nil)
```

`Options` set the `Language` (a BCP 47 tag), a `Voice` (an ID from
`speech.Voices()`, with its name, language and quality), `Pitch`, `Rate`
and `Volume`. `Stop` drops the queue, `Pause` and `Resume` pause at a
word (on Android too, which can't pause: Asuka speaks on from the word).
On iOS other apps' sound ducks while it speaks, and it speaks with the
silent switch on; Android ducks them likewise. The voices are
AVSpeechSynthesizer's on iOS and the Mac, SAPI's on Windows, Speech
Dispatcher's on Linux, and `speechSynthesis`'s in a browser.

## Contacts: asuka/contacts

- `contacts.Pick(done)` shows the system's contact picker: the user picks
  one, and the app sees only that one, with no permission asked (on
  Android, without the permission, its name only).
- `contacts.Add(contact, done)` shows the system's new contact screen,
  filled in, where the user saves it; no permission asked.
- `contacts.Request()` asks for access, and `contacts.List(query)` reads
  the address book (by name, up to a limit): names, organization, phones
  and emails with their labels, birthdays. iOS 18's limited access lists
  the contacts the user chose.

On a Mac it's the Contacts framework: `Pick` is the system's picker, a
popover from the window's top, and `Add` asks in an alert over the
window (macOS has no new contact screen), then saves. Linux reads and
writes Evolution Data Server's address books, as [On Linux](#on-linux)
says. Windows gives the contacts to packaged apps only: `Request` says
denied, `Pick` fails, and `Add` opens a vCard in the user's contacts
app. A browser picks one where it has the Contact Picker (Chrome on
Android), and `Add` downloads a vCard.

## Calendars: asuka/calendar

- `calendar.Add(event, done)` shows the system's event editor, filled in,
  where the user saves the event; no permission asked (iOS 17 and later).
  iOS says whether it was saved; Android's calendar apps don't.
- `calendar.Request()` asks for full access; then `calendar.Calendars()`
  lists the calendars and `calendar.Events(from, to, ids...)` the events
  between two times, each occurrence of a repeating one.
- An all-day event's `Start` and `End` are days: midnight UTC of the
  first, and of the day after the last.

On a Mac it's EventKit: `Add` asks in an alert over the window (macOS
has no event editor for apps), then saves. Linux reads and writes
Evolution Data Server's calendars. Windows gives the calendars to
packaged apps only: `Request` says denied, and `Add` opens an iCalendar
file in the user's calendar app (`saved` is false: it doesn't say). A
browser can't read calendars; `Add` downloads an `.ics` file.

## Home screen widgets: asuka/widget

`asuka.json`'s `widgets` declares the app's widgets; `widget.Set` fills
them, from the app's Go code:

```json
{ "widgets": [{ "kind": "today", "name": "Today", "description": "What's next", "sizes": ["small", "medium", "rectangular"] }] }
```

```go
widget.Set("today", widget.Entry{
	Title: "Next", Symbol: "calendar", Value: "3 tasks", Body: "Call Ana at 4",
	Progress: 0.4, ShowProgress: true, Until: meeting, URL: "myapp://today",
})
```

A widget shows its `Entry` with the platform's template, laid out for
its size: the `Title` (with its `Symbol`, in the `Tint`), a large `Value`,
the `Body`, a `Progress` bar, a live countdown to `Until`, a `Caption`.
It keeps showing it until the app sets another, whether or not the app
runs; tapping it opens the app with its `URL` (`ui.OnOpenURL`). Several
entries, each with its `At`, are a timeline: each shows from its time.
`Clear` empties it.

Sizes: `small`, `medium`, `large`, and iOS's `extraLarge` (iPad) and
lock screen `circular`, `rectangular` and `inline`. iOS: a WidgetKit
extension the CLI builds into the app, sharing an App Group with it
(a release needs the App Groups capability, and a profile for the
extension's ID, the app's plus `.widgets`). Android: an App Widget for
each kind, Material You colors, sized to the launcher's cells. The
desktops and the web have none: `widget.Set` does nothing there.

## Shortcuts: Siri, Spotlight, the launcher

`asuka.json`'s `shortcuts` are actions the system offers for the app.
Each opens the app with its URL, for `ui.OnOpenURL`:

```json
{ "shortcuts": [{ "id": "newNote", "title": "New Note", "url": "notes://new", "symbol": "square.and.pencil", "phrases": ["New note in ${app}"] }] }
```

On iOS they're App Shortcuts: Siri hears their phrases (each with
`${app}`, the app's name), and they're in Spotlight, the Shortcuts app
and on the Action button, with no setup by the user (up to ten). The CLI
compiles them as App Intents and extracts their metadata as Xcode does.
On Android they're the launcher's shortcuts, shown on a long press of
the app's icon, with the symbol in the accent color. The desktops and
the web don't add them.

Simulator builds are signed as Xcode signs them, with the keychain's
Apple Development identity (`asuka.json`'s team's): App Intents only run
in an app with a team. Without one, the build is signed ad hoc, and the
CLI says the shortcuts won't run.

## Haptics: ui.Haptic

`ui.Haptic(kind)` plays haptic feedback (UIFeedbackGenerator;
`performHapticFeedback` on Android): `ui.HapticSelection`,
`HapticLight`, `HapticMedium`, `HapticHeavy`, `HapticSuccess`,
`HapticWarning` or `HapticError`. A Mac plays the trackpad's feedback
(a Force Touch trackpad: an alignment for `HapticSelection`, a level
change for the rest); a browser vibrates where it can (Chrome on
Android); Windows and Linux have no haptics, and do nothing.

```go
ui.Haptic(ui.HapticSuccess)
```

See [Motion](motion.md) for feedback that goes with animation.

## Crash reports: asuka/crash

An app that imports `asuka/crash` keeps a report of each crash, and reads
it on its next launch, to send where it likes: its own server, Sentry,
Crashlytics. Asuka uploads nothing.

```go
func App() ui.Node {
	crash.OnReport(func(r crash.Report) {
		go func() {
			if err := send(r); err == nil { // your upload: r is JSON
				crash.Delete(r.ID)
			}
		}()
	})
	crash.Set("user", userID)
	// ...
}

// Elsewhere, as the app goes:
crash.Breadcrumb("opened the cart")
crash.Record(err) // an error handled, reported too
```

`OnReport(f)` calls `f` on the main thread with each report kept, then
with each that comes (a diagnostic iOS delivers a moment after launch,
an error recorded), once each, until its `stop`. `Reports()` reads them
all, oldest first (not on the main thread). A report stays until
`Delete(id)`; past `MaxReports` (50) the oldest go. Its `ID` is the same
each time it's read, so a server can tell one sent twice.

What's reported, as a `Kind`, on each platform:

| | iOS | Android | macOS | Windows | Linux | Web |
|---|---|---|---|---|---|---|
| `Go`: a panic not recovered, on any goroutine; a fatal error (a concurrent map write, a deadlock, out of memory) | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ (the host keeps the runtime's output) |
| `Exception`: uncaught | NSException (Objective-C, Swift's `NSException`s) | a JVM `Throwable` | — | — | — | an uncaught JavaScript error (not fatal) |
| `Native`: a signal, a Mach or Windows exception | MetricKit | the system's tombstone | MetricKit | the runtime's report | the runtime's report (a signal in native code) | — |
| `Hang` | MetricKit's hangs (not fatal), the watchdog | ANRs, with their traces | MetricKit | — | — | — |
| `OutOfMemory` | the memory limit, when MetricKit says | low-memory kills | — | — | — | — |
| `Resource` | CPU and disk-write exceptions (not fatal) | excessive resource use | MetricKit | — | — | — |
| `NonFatal`: `Record(err)` | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |

A Go crash is kept by Go's runtime itself (`runtime/debug.SetCrashOutput`,
set as the app starts, before its `main`), with every goroutine's stack.
An uncaught exception is written by the host's handler as the app ends
(a handler set before, a crash SDK's, still runs after). The rest the
system hands over on a later launch: iOS and macOS's MetricKit
diagnostics, and Android's record of how the app's processes ended
(`getHistoricalProcessExitReasons`, Android 11 and later; a native
crash's tombstone from Android 12). A crash the system reports again
after the host's handler did (iOS's SIGABRT of an uncaught exception,
Android's `REASON_CRASH`) is left out.

Each report has its `Context`: the app's version and build, the device's
model and system, the process and how long it had run, whether it was in
the foreground, and the last `Breadcrumb`s (`MaxBreadcrumbs`, 100) and
the values `Set`. They're written to the app's data directory as they
change, a line appended each (no sync, so as cheap as a write), the file
rewritten when it's twice what it needs: they're on disk before the
crash, whatever kills the app. The lifecycle's changes are breadcrumbs
too ("app: background").

A report's `Text` is what the platform gives (Go's crash output, a stack
trace, a tombstone, an ANR's traces); its `Threads` are those stacks
read into frames, the crashed one first. A Go frame has its function,
file and line. A native one has its binary, the binary's UUID (Mach-O)
or build ID (ELF), the address and its offset in the binary: what a
service symbolicates with the build's symbols (see
[Shipping](shipping.md#symbols-for-crash-reports)).

**Sending to Sentry or Crashlytics.** For Sentry, make an event of each
report (its envelope endpoint, `https://<host>/api/<project>/envelope/`
with your DSN's key): `exception.values[0]` with the `Kind` as its
`type`, the `Message` as its `value`, and the crashed thread's frames as
its `stacktrace.frames`, outermost first (Sentry's order, the reverse of
a `Thread`'s), each with `function`, `filename` and `lineno`, or
`instruction_addr` and `package` for a native frame; and in
`debug_meta.images` each binary of the frames, `{"type": "macho",
"debug_id": BinaryID, "image_addr": Address-Offset}` (`"elf"` on
Android), so the dSYMs or symbols you upload with `sentry-cli` apply.
The `Context` goes in `contexts`, `tags` and `breadcrumbs`. Crashlytics
has no public endpoint to send to: send the reports to your server, or
record them in Crashlytics' SDK as non-fatal errors from your own native
module ([Native code](native.md)). Either way, App Store review asks
about crash data: if you send it, say so (`ios.collected` in
`asuka.json`, `{"type": "CrashData", "purposes": ["AppFunctionality"]}`).

**On each platform.** iOS links MetricKit only into an app that reads
its reports (`crash.Reports` or `crash.OnReport`), and delivers its
diagnostics to the app, not its share extension. An app with its own
share sheet (`app.ShareSheet`) keeps the sheet's crashes with its own,
in their App Group, and either reads them. MetricKit has a crash's process ID from iOS 17 on, which ties it
to its run's context; before, it's the run that was last before it. It
doesn't deliver on the simulator. Android needs nothing more; before
Android 11 there's no record of native crashes and ANRs. macOS: MetricKit
from macOS 12; an uncaught Objective-C exception that ends the app is
MetricKit's crash (AppKit catches those of the main thread and logs
them). Windows: Go's runtime handles every exception the process
doesn't (its last handler): it prints the exception and the goroutines,
and exits. Without a console (a release, built for the GUI), Asuka sends
what the runtime prints to a file of the run's, which the next launch
reads into a `Native` report. Asuka sets no unhandled exception filter of
its own: Go's handler runs first, and replacing it would break Go's.
Windows Error Reporting keeps its record as ever. In a browser, Go's
runtime can't write its crash output to a file (`SetCrashOutput` needs
`dup`), so the host keeps what the runtime prints while Go runs and, as
the program ends with a panic or a fatal error, writes it to the run's
record, in the app's files (IndexedDB): the next launch reads it. An
uncaught JavaScript error, the host's or the app's `web/*.js`, is an
`Exception` report at once, not fatal (the page goes on), with the run's
context; at most 20 a run, each once. A page marks its run alive in the
origin's `localStorage`, so the app's other open pages leave its record
alone.

**Under `asuka dev`** the Go code runs on your Mac (or PC): its crashes
print in the terminal, as ever, and are kept there, so `crash.Reports`
on the next run has them. The player's own native crashes aren't the
app's, and aren't reported to it; a build of the app (`asuka dev -once`,
`asuka build`) reports everything.
