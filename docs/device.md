# Device and services

RyUI's packages reach what the phone has beyond views: the camera, codes,
photos, location, notifications, sound, the network's state, the shared
library, work in the background, shares from other apps and JavaScript.
This page covers each package: what it does, its API, the permissions it
brings and how it behaves on each platform.

## Permissions and purpose strings

An app that imports a package is given what the package needs: Android
permissions in the manifest, iOS purpose strings in Info.plist, and the
platform code itself. An app that doesn't import it goes without, so a
photo app doesn't ask for the microphone.

| Package | Android | iOS |
| --- | --- | --- |
| `ryui/camera` | `CAMERA` | `NSCameraUsageDescription` |
| `ryui/camera/record` | `RECORD_AUDIO` | `NSMicrophoneUsageDescription` |
| `ryui/location` | `ACCESS_COARSE_LOCATION`, `ACCESS_FINE_LOCATION` | `NSLocationWhenInUseUsageDescription` |
| `ryui/notify` | `POST_NOTIFICATIONS` | |
| `ryui/network` | `ACCESS_NETWORK_STATE` | |
| `ryui/library` | `WRITE_EXTERNAL_STORAGE` (Android 9 and earlier) | `NSPhotoLibraryAddUsageDescription`; the app's Documents shown in Files |
| `ryui/background` | `FOREGROUND_SERVICE`, `FOREGROUND_SERVICE_DATA_SYNC`, `POST_NOTIFICATIONS`, `WAKE_LOCK`; a foreground service | |
| `ryui/scan` | ML Kit's barcode model (about 2 MB) | |

`ryui/device`, `ryui/audio`, `ryui/js` and `ryui/secure` need none.

The purpose strings RyUI writes are generic ("To take a photo."). iOS
shows them when it asks the user, so write your own in `ryui.json`'s
`ios.usage`:

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

## The camera: ryui/camera

`camera.Take` shows the system's camera (iOS's camera screen, Android's
camera app) and calls `done` with the photo's file, a JPEG, or `""` if the
user cancelled. On a device without a camera (the iOS simulator) the error
is `camera.ErrNoCamera`.

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
sees, live, filling its box (AVFoundation's preview on iOS, CameraX's
`PreviewView` on Android), and `camera.Capture` takes a photo from it:

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
  with `ryui/camera/record`, which takes photos too.
- `Style`: its box.
- `OnError`: called when the camera can't show: `"not allowed"` if the
  user refused it, `"no camera"` if there's none.
- `Scan`: reads codes in what it sees; see `ryui/scan`.

The view asks for the camera the first time it shows, and the camera is
on while the view is on screen. Show one at a time: `Capture` and
recording use the one on screen, and fail with `"no camera view"` if
there's none.

The files from `Take` and `Capture` are in the app's caches, which the
system may clear: move one into `store.Dir()` to keep it, or save it to
the photo library with [`ryui/library`](#saving-to-the-library-ryuilibrary).

**Android**: as `ryui/camera` declares the camera permission (the view
needs it), Android requires it of the camera app too: `Take` asks for it
first, and fails with `"not allowed"` if the user refuses.

## Recording video: ryui/camera/record

`ryui/camera/record` records video from the `camera.View` on screen, whose
`Mode` is `camera.Video`. `record.Start` records until `record.Stop`, or
until `MaxDuration` if set, then calls `done` with the video's file (in
the app's caches: `.mov` on iOS, `.mp4` on Android).

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
not with `ryui/camera`: an app that only takes photos goes without them.

## Barcodes and QR codes: ryui/scan

`ryui/scan` reads barcodes and QR codes, live in what a `camera.View`
sees, or in an image file. iOS reads them with AVFoundation and Vision;
Android with ML Kit, which only the apps that import `ryui/scan` are built
with.

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
RyUI gives it as a UPC-A again.

## Photos, the browser, the clipboard: ryui/device

`ryui/device` is what the platform offers beyond views.

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
  (PHPicker on iOS, the Photo Picker on Android) for up to `limit` images
  (1 to 255). It asks for no permission: the user picks, and the app sees
  only what was picked. `done` gets the images' files, which `ui.Image`
  shows, or none if the user cancelled. The files are copies in the
  app's temporary files or caches: move one to keep it.
- `device.Browse(url)` shows a web page in the app (SFSafariViewController,
  a Custom Tab), over the app's screens. The URL must be http or https.
  `ui.OpenURL` leaves the app for the browser instead.
- `device.Clipboard()` reads the clipboard's text; `ok` is false if it has
  none. Not on the main thread. iOS asks the user the first time the app
  reads what another app put there. `ui.Copy` writes it.
- `device.KeepAwake(on)` keeps the screen on while `on` is true: a video,
  a recipe, a map. Turn it off when the screen that wants it goes.
- `device.GetInfo()` is the device and the app: `Model` ("iPhone", "Pixel
  9"), `System` ("iOS", "Android"), `SystemVersion`, and `AppVersion` and
  `Build`, `ryui.json`'s `version` and `build`. Not on the main thread.

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

## Signing in on the web: ryui/webauth

`webauth.Start` shows a sign-in page in the system's own sign-in sheet:
`ASWebAuthenticationSession` on iOS (SwiftUI's
`webAuthenticationSession`), a Custom Tab on Android. It's the
browser's, so the user's sessions, passkeys and password autofill work
in it, and providers that refuse an app's own `WebView` (Google's
sign-in) don't refuse it. Use it for OAuth rather than a `ui.WebView`.

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
  URL. On Android the scheme must be the app's, ryui.json's `scheme`; it
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
  contact, AirDrop or Nearby Share. A URL is shared as a link.
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
picker. Windows has no share sheet for files yet: `done` gets an error.

To be opened with URLs yourself, see `ryui.json`'s `scheme` and `links` in
[Shipping](shipping.md#ryuijson), and `ui.OnOpenURL`.

## Location: ryui/location

`ryui/location` is where the device is, now and as it moves, with the
user's permission, which it asks for the first time. It's Core Location
on iOS, and Android's location manager, which needs no Google Play
services. It asks for the location while the app is in use, not in the
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

A `location.Position` has `Latitude`, `Longitude`, `Accuracy` (meters:
the radius the device is within), `Altitude` (meters above sea level; 0 if
not known) and `Time`.

**Android**: `Current` gives a fix from the last minute if there is one;
otherwise it waits up to 10 seconds for a new one, then gives the last
known, or fails with `"no location"`.

## Notifications: ryui/notify

`ryui/notify` shows local notifications at a time the app sets, whether or
not the app is running then. Tapping one opens the app with its URL.

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
  second). Scheduling one with the `ID` of one waiting replaces it. Not on
  the main thread.
- `notify.Cancel(id)` takes the notification away, waiting or shown. On
  the main thread.

A `Notification`'s `URL` should be one of the app's scheme (`ryui.json`'s
`scheme`): `ui.OnOpenURL` gets it when the user taps the notification.

```go
ui.OnOpenURL(func(url string) {
	if url == "myapp://plants" {
		notify.Cancel("water")
	}
})
```

**iOS** shows a notification while the app is open too, as a banner.
**Android** posts it from an inexact alarm (no exact-alarm permission
needed), so it may come a little after its time.

## Sound: ryui/audio

`ryui/audio` plays sounds and music with the platform's player (AVPlayer;
Media3's ExoPlayer on Android). A source is a file in the app's
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

A `Player`'s methods, all on the main thread: `Play` (from where it is),
`Pause`, `Seek(seconds)`, `SetVolume` (0 to 1, the default), `SetLoop`,
and `Release`, which stops it and frees what it holds. Release a player
when it's no longer needed.

The sound plays with iOS's silent switch on, as a music or game app's
does, and doesn't mix with other apps' audio.

## Network state: ryui/network

`ryui/network` is the device's network: whether it's online, over Wi-Fi or
cellular, and whether the connection costs. Use it to download large
files on Wi-Fi only, or to say the app is offline. It's NWPathMonitor on
iOS and ConnectivityManager on Android.

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
  Mode, Android's Data Saver).

Until the platform first answers, `Use`'s value is `Connected` with
`Type` `Other`.

`network.Current()` is the network now (not on the main thread), and
`network.Watch(f)` calls `f` on the main thread with the network now and
each time it changes, until `stop` is called.

## Saving to the library: ryui/library

`ryui/library` saves files where the user and other apps find them: a
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

| Collection | Android | iOS |
| --- | --- | --- |
| `Downloads` | Downloads | the app's Documents, in the Files app |
| `Movies` | the gallery's videos | Photos |
| `Music` | Music | the app's Documents, in the Files app |
| `Pictures` | the gallery's pictures | Photos |

**Android** uses the MediaStore collections from Android 10, with no
permission, and answers with a `content://` URI. Before Android 10, it
writes to the shared folders after asking for the storage permission.

**iOS** asks to add to Photos (add only: the app can't read the library)
and answers with the asset's identifier; `Folder` doesn't apply there.
Other collections go to the app's Documents, under `Folder`, answering
with the file's path; RyUI makes Documents visible in the Files app.

## Background work: ryui/background

`ryui/background` keeps the app working while it's out of sight: a
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
			task.Update(background.Options{Title: "Exporting", Text: fmt.Sprintf("%.0f%%", p*100), Progress: p,
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

## Shares from other apps: ui.OnShare

`ui.OnShare(f)` calls `f` with what another app shares with this one
from its share sheet: a link from YouTube or a browser, a text from
Notes, photos from the gallery, a PDF from Files. `ryui.json`'s
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

Under `ryui dev`, the player takes shares of anything, whatever the
app's `shareTarget`. On Android the files' paths are on the device, not
the Mac the Go code runs on: try files with `ryui dev -android -once`.

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
`ryui/store` is written as it closes, and `app.OnLifecycle` hears
`Background`. A file's `Path` is the sheet's until it closes: copy what
the app keeps.

What the app keeps is the same in its sheet: `ryui/store`, SQLite files
in `store.Dir()` (`sqlite.Open("app.db")`), `ryui/secure`. Android's
two processes share the app's files. On iOS the app's data moves to the
App Group (`group.<app ID>`), once, the first time it runs with a sheet,
and its keychain group is shared with the extension. The app doesn't
hear what its sheet wrote while it was running: look again when it
comes back (`app.OnLifecycle`, `db.Changed()`).

The CLI sees that the app's Go code calls `app.ShareSheet` and builds
the sheet instead of the extension that opens the app. On iOS the app's
Go code and the host are then a framework in the app,
`Frameworks/RyUIApp.framework`, which the app and its extension both
load, as Xcode shares code with an extension: the app isn't bigger for
its sheet. Under `ryui dev` the player's sheet runs the app's sheet from
the Mac too. [`examples/bookmarks`](../examples/bookmarks) is a
sheet as Raindrop's: the page's title, description and image, a note, a
collection and tags.

## JavaScript: ryui/js

`ryui/js` runs JavaScript off screen, in the platform's own engine: a web
view that's never shown (WKWebView, Android's WebView). It's for code
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

## Haptics: ui.Haptic

`ui.Haptic(kind)` plays haptic feedback (UIFeedbackGenerator;
`performHapticFeedback` on Android): `ui.HapticSelection`,
`HapticLight`, `HapticMedium`, `HapticHeavy`, `HapticSuccess`,
`HapticWarning` or `HapticError`.

```go
ui.Haptic(ui.HapticSuccess)
```

See [Motion](motion.md) for feedback that goes with animation.
