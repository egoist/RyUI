# The web

The same app runs in a browser: its Go code compiled to WebAssembly, its
views the page's DOM. The web is one of RyUI's platforms beside iOS,
Android, the Mac, Windows and Linux, from the same Go code. Each view the core makes is an element placed at
the frame the core laid out, as each native host places its widgets, so
the layout is RyUI's own (flexbox and grid, in Go), the same on every
platform; leaves are measured by the real elements, as the native hosts
measure theirs. Controls are the browser's own (`<button>`, `<input>`,
`<select>`, `<progress>`, `<dialog>`), so keyboard handling, focus,
autofill, spelling and screen readers come with them.

```sh
ryui dev -platform web examples/counter       # serves it and opens it in your browser; reloads as you save
ryui build -platform web examples/counter     # a static site, in examples/counter/dist/web
```

What follows says what the web does, and, where a browser can't do what
a phone does, what happens instead.

## ryui dev -platform web

`ryui dev -platform web` builds the app for the browser (`GOOS=js GOARCH=wasm`),
serves it at `http://localhost:8790/` (or a free port) and opens it. Each
save rebuilds it, in well under a second for the examples, and the page
loads the new build with the app's signals' values kept, as the other
platforms' reloads keep them. The app's output (`fmt.Println`, `log`, a
panic) and the page's errors print in the terminal; a build that fails
shows its errors over the page until the next one builds.

The flags, beside `ryui dev`'s own (`-lang`, `-dir`, `-vet`, `-v`):

- `-port`: the server's port.
- `-lan`: serve to your network too, and print its addresses: open the
  app in a phone's browser.
- `-no-open`: don't open the browser.
- `-once`: build and serve, without watching.

## ryui build -platform web

`ryui build -platform web` makes a static site in `dist/web`:

- `index.html` and `manifest.webmanifest` (a web app manifest, so a
  browser offers to install the app; its icons from `ryui.json`'s
  `icon`, with a favicon and iOS's home screen icon);
- the app, `app.<digest>.wasm`, built stripped and with `-trimpath`;
- the web host's files in `ryui/<digest>/` (with SQLite's WebAssembly
  build, about 0.9 MB, 0.4 MB gzipped, for an app that imports
  `ryui/sqlite`), and the app's own scripts (`web/*.js`) in
  `web/<digest>/`;
- the app's service worker, `sw.<digest>.js`, beside `index.html` (its
  scope is the site's folder): it shows the app's notifications and
  pushes while no page of it is open
  ([Notifications and Web Push](#notifications-and-web-push));
- the app's `assets/`;
- a gzipped copy of each script and of the wasm (`.gz`), for servers that
  serve precompressed files;
- `_headers`, which Cloudflare and Netlify read: `index.html` and the
  manifest `no-cache`, the files named by their digests cached for good.

Serve the folder from any web server or CDN: the files named by their
content's digest change name when they change, so they can be cached for
good, and only `index.html` must be fetched again. Serve `.wasm` as
`application/wasm`. A counter is about 6.5 MB of WebAssembly, 1.7 MB
gzipped (1.3 MB with brotli, which your CDN can do); the app's own code
and the packages it imports add to that.

## What the page reports

The environment a web app sees (`ui.UseEnvironment`) is the browser's:

- `Platform` is `ui.PlatformWeb`.
- The size is the page's (or the element's the app is in), and changes
  as the window does; the size classes are Material's, from the width.
- The safe area is the page's (`env(safe-area-inset-*)`: a phone's notch
  and home indicator, with the page's `viewport-fit=cover`), and the
  keyboard is how much of the page a phone's keyboard covers.
- Dark mode is `prefers-color-scheme` (or the app's `ui.SetColorScheme`),
  Reduce Motion `prefers-reduced-motion`, the text size the browser's
  default font size (`FontScale`, which text follows), the scale the
  display's pixel ratio.
- The locale, languages, time zone, first weekday and hour cycle are the
  browser's (`navigator.languages`, `Intl`).

## How views map

- Text is the browser's text in the system's font (`system-ui`: San
  Francisco on Apple's systems, Segoe UI on Windows, Roboto on Android),
  on a type ramp of the web's (body 16). The app's fonts in
  `assets/fonts` load as the app names them (`Font.Family`).
- A `Stack` shows its top screen under a header bar of its own: a back
  button, the screen's title, its bar buttons, its search field. Pushes
  and pops slide. Going back is the browser's too: its back button (and
  a phone's back gesture) goes back in the app while there's somewhere to
  go back to, as do Escape, Alt+Left (⌘[ on a Mac), and a swipe from the
  leading edge on a touch screen. `ui.OnBack` takes them as on the other
  platforms.
- `Tabs` are a tab bar at the bottom; a `SplitView` a sidebar beside its
  detail, or a drawer the detail's header opens on a narrow window.
- A `Sheet` is a modal `<dialog>`: a bottom sheet on a narrow window
  (medium is half the height; the grabber drags it up or away), a
  centered card on a wide one. Escape, a click outside or a swipe down
  dismisses it. An `Alert` is a modal `<dialog>`, an action sheet's
  buttons a list. A `Popover` and a menu are popovers next to what they
  point at. A `ui.Window` is a panel in the page the user can move and
  resize.
- A `List` is virtualized by the host, as on the Mac, Windows and
  Linux: only the rows
  in view (and half a screen around them) are bound; sections' headers
  stick; a chat's list stays at its end. Its rows' swipe actions are
  their context menu (a right-click, or a long press).
- `Image` is an `<img>` (decoded off the main thread, cached by the
  browser), `Video` a `<video>`, `WebView` an `<iframe>` (see
  [Web views](#web-views)), `Canvas` a 2D
  canvas, `Shape` an SVG path, `Shader` WebGL 2 (the Kage program
  translated to GLSL ES 3.00), `Symbol` the Material Symbol the Android
  host draws for the SF Symbol name.
- `ui.Animate` animates frames and visual props with the Web Animations
  API (springs as CSS `linear()` curves); enter and exit transitions and
  matched views (`Style.Match`) work as on iOS.
- Presses, long presses (and right-clicks), hover, pressed states, pans,
  swipes, pinches (two fingers, or a trackpad's pinch) and rotations work
  with a mouse, a pen or touch; drag and drop is the browser's, files
  from other apps included.
- `MenuBar`'s items' shortcuts work in the page (⌘ on a Mac, Ctrl
  elsewhere); the page shows no menu bar of its own, where the desktop
  apps show the app's menus: the Mac's menu bar, the window's menu bar
  on Windows, the header bar's main menu on Linux.

## The app's files

`ryui/store`, crash records and the files your app writes in `store.Dir`
are kept: the page gives Go a file system in memory, the data directory's
files loaded from the browser's storage (IndexedDB) before the app starts
and written back a moment after they change (a large file, such as a
database, in chunks of 64 KiB: only the chunks that changed), and as the
page is hidden or closed. What the user drops, pastes or picks is copied
into the app's caches (`/tmp`), as on iOS and Android, and comes to the
app as a path.

The browser keeps an origin's storage until the user clears it, or, when
the device runs out of space, until it evicts the origin's (Safari also
does after seven days without a visit, for a site that isn't installed
to the home screen).

## SQLite

`ryui/sqlite` works as on the other platforms, live queries included.
Its SQLite is SQLite's own WebAssembly build (from sqlite.org, which
`ryui build -platform web` and `ryui dev -platform web` ship with the
apps that import `ryui/sqlite`, and load before the app starts), driven
from Go synchronously. A database is a file in the app's data directory,
like any other, so Go's `os` sees it (a backup is `os.ReadFile`), and it
is kept with the app's other files: each call SQLite answers runs to its
end before the page does anything else, so what's kept is always a
state between two calls, which SQLite's rollback journal recovers from,
as after a crash.

What differs:

- The journal is a rollback journal, not write-ahead logging (which
  needs memory shared between connections), and a database has one
  connection, so reads wait for a write in progress rather than running
  beside it. The page has one thread: a write blocks it while it runs,
  as on any platform's main thread; `ExecAsync`, `TxAsync` and `Watch`
  run between the page's events.
- Two `sqlite.Open`s of one file lock each other out, as two processes
  would; a statement that finds the file locked is run again once the
  other has had its turn, for the busy timeout's 5 seconds.
- A transaction committed in the last moment before the tab closes may
  not be kept (the browser may close it before IndexedDB has written
  it).

A page of your own that embeds an app (below) passes `sqlite`, the URL
of the directory with `sqlite3.mjs` and `sqlite3.wasm`, to `start`.

## Native modules

RyUI's packages call the host's native modules (`ui.Call`). In a browser:

- `ryui/secure`: the values are encrypted (AES-GCM) with a key the
  browser keeps and won't let out (a non-extractable WebCrypto key), in
  IndexedDB. It isn't a keychain: script running in the app's origin
  can't read the key, but it can ask it to decrypt. It needs `https`
  (or `localhost`).
- `device`: `GetInfo` from the user agent (Chromium's client hints give
  the system's version and an Android phone's model); `Clipboard`, its
  images (PNG, saved to the caches) and `OnClipboardChange` (the app's
  own copies, and other apps' where the user lets the page read the
  clipboard); `KeepAwake` (the Screen Wake Lock, taken again when the
  page shows); `Browse` (a new tab); `PickPhotos` and `PickFiles` (the
  browser's file picker; the files copied to the caches).
- `ShareFiles`: the share sheet with the files (`navigator.share`),
  else each file downloaded. `ryui/preview`: the file opened in a new
  tab (images, video, audio, text, PDF, HTML); another type is
  `ErrNoViewer`.
- `ryui/print`: HTML, files (PDFs in the browser's viewer) and the app's
  own web views, with the browser's print dialog; the result is always
  `Unknown` (a browser doesn't say). `PDF` isn't on the web: a browser
  makes PDFs only through its print dialog (Save as PDF).
- `ryui/compose`: `mailto:` and `sms:` links; with attachments, the share
  sheet.
- `ryui/notify` and `ryui/push`: the Notification API and Web Push,
  through the app's service worker: see
  [Notifications and Web Push](#notifications-and-web-push).
- `ryui/network`: online from `navigator.onLine`; Wi-Fi, cellular or
  Ethernet where the browser says (Chrome on Android), metered for
  cellular, constrained for Data Saver.
- `ryui/location`: the Geolocation API; `PlaceName` isn't on the web (a
  browser has no geocoder).
- `ryui/sensors`: the Generic Sensor API (Chrome) or device motion and
  orientation events (Safari asks on a tap), in Android's axes and
  units; no barometer or pedometer.
- `ryui/speech`: `speechSynthesis`, the browser's voices.
- `ryui/audio`: an audio element per player (the browser's autoplay
  rules apply: play from a tap). `ryui/audio/record`: `MediaRecorder`,
  AAC in MPEG-4 where the browser records it, else Opus in WebM.
- `ryui/camera`: `Take` opens a phone's camera, or a dialog with the live
  camera and a shutter; `camera.View` is the camera in a `<video>`
  (`Capture`, `record`, and scanning codes where the browser has
  `BarcodeDetector`). `ryui/scan` and `ryui/vision`'s codes use
  `BarcodeDetector`, faces and text only where the browser has them.
- `ryui/imageedit`: a canvas (JPEG or PNG; a remote image needs CORS).
- `ryui/biometric`: the platform's authenticator through WebAuthn (Touch
  ID, Face ID, Windows Hello, Android's screen lock), user verification
  required: the first `Authenticate` makes a passkey for the site, named
  after the app. The prompt is the browser's, so the reason and cancel
  texts don't show.
- `ryui/webauth`: a popup, done when it comes back to a page of the
  app's own origin (a custom scheme can't open a page).
- `ryui/screen`: `Lock` (only full screen, or an installed app); hiding
  the bars is full screen.
- `ryui/library`: `Save` downloads the file. `ryui/contacts`: `Pick`
  where the browser has the Contact Picker (Chrome on Android); `Add`
  downloads a vCard, and `ryui/calendar`'s `Add` an `.ics` file.
- `ryui/tracking` answers Denied when the browser sends Global Privacy
  Control or Do Not Track, else Allowed, without a prompt.
- Not on the web, with an error that says so (or the package's own
  "not available"): `ryui/review`, `ryui/integrity`, `ryui/agerange`,
  `ryui/appleauth`, `ryui/periodic`, `ryui/widget`, `ryui/photos` (Denied:
  `device.PickPhotos` picks photos), reading contacts and calendars,
  `ryui/transcribe`'s files, `ryui/screen`'s `PreventCapture` and
  `OnScreenshot`.

## Notifications and Web Push

Every web app has a service worker, which the CLI writes (`sw.js` under
`ryui dev`, `sw.<digest>.js` in a build) and the page registers as it
starts, scoped to the app's folder. It has no fetch handler, so the page
loads as it would without it; it shows the app's notifications and
pushes, and answers their clicks, whether or not a page of the app is
open. A browser runs service workers only for pages served over HTTPS,
or from `localhost` (`ryui dev`'s, but not `-lan`'s addresses).

### ryui/notify

- `Request` asks with the browser's prompt; browsers ask only from a tap
  or a click, so call it from one.
- `Show` shows it through the worker, so it keeps its buttons and its
  click works after the page closes. (A page without the worker, an
  embedding page that doesn't register it, shows the page's own
  notifications, without buttons; their clicks reach only that page.)
- A notification's ID is its tag: one with the same ID replaces it, and
  alerts again unless `Silent`. `Subtitle` is the body's first line,
  `Image` the picture (an `https` URL or an asset), a message's sender's
  picture (`From`) the icon. `Badge` and `SetBadge` set the installed
  app's badge (`navigator.setAppBadge`).
- A category's actions (`SetCategories`) are the notification's buttons
  (Chrome and Edge show two; Firefox and Safari none), a `Reply` action a
  text field where the browser has inline replies (Chrome on Android and
  desktop: `Response.Reply` has the text; elsewhere it's a button). The
  categories are kept for the worker, so a push that names one has them
  too.
- A tap focuses the app's page, goes to its `URL` (`ui.OnOpenURL`) and
  reaches `OnAction` (`Action ""`); a button reaches `OnAction` with its
  ID, in the page in front, else any page of the app. With no page open,
  the worker opens one (a browser runs the app's code only in a page),
  and the app gets the response as it starts, as an app launched by a tap
  does: set `OnAction` in the root component. An action without `Open`
  doesn't bring a page that's open to the front, but with none open it
  opens one, in front. `Response.Defer` has nothing to hold: the page
  runs on.
- `Present` asks the app how to show one that comes while the page is in
  front, a push's too: `Banner`, `Quiet` (no sound), `Hide`.
- `Showing`, `Remove`, `RemoveThread`, `RemoveAll` and `Cancel` reach the
  worker's notifications, the ones from earlier pages and pushes too.
- Android's channels don't exist here: accepted and ignored, as on iOS.

#### Scheduled notifications

A browser can't show a notification at a time while no page of the app
is open: there's no API for it (Chrome's Notification Triggers, which
did, were abandoned in 2021), and a service worker runs only when an
event wakes it, not on a timer. So `Schedule` does what a browser can:

- What's scheduled is kept in the app's store (Cache Storage, which the
  app's pages and its worker share), not in the page.
- While any page of the app is open, it shows at its time: each page
  keeps a timer for the next one (a page that schedules or cancels one
  tells the others), and the first to find it due shows it, once (a Web
  Lock). A page in the background may be late: browsers slow hidden
  pages' timers to about once a minute; it shows when the page comes
  back in front at the latest.
- One that came due while no page was open shows when the next one
  opens, at once, with its due time passed.
- Before that, as a best effort, the worker shows the due ones whenever
  something wakes it with no page open: a push, a click, and Chrome's
  periodic background sync, which the page asks for while anything is
  scheduled. Chrome grants that only to an installed app (added to the
  home screen or installed from the address bar), runs it at most about
  every 12 hours, as the browser sees fit (how much the user engages with
  the app, the battery, the network), and other browsers don't have it:
  never count on it.

For a notification that must show at its time with the app closed, send
a push from your server at that time.

### ryui/push: Web Push

Web Push needs a VAPID key pair: the app's server signs its pushes with
the private key, and the browser subscribes with the public one, which
goes in `ryui.json`:

```json
{ "web": { "vapidPublicKey": "BKpYsAT0ADj2pHQQ..." } }
```

Make the pair once, and keep the private key on the server; a new pair
needs new subscriptions. `webpush.GenerateKeys` (below) makes one, as
does `npx web-push generate-vapid-keys`.

`push.Register()` asks to allow notifications (from a tap: browsers ask
only then), subscribes the app's worker with the key (a subscription
made with another key is replaced) and answers with:

- `Token`: the subscription's JSON, as `PushSubscription.toJSON()` has
  it: `{"endpoint": "https://...", "expirationTime": null, "keys":
  {"p256dh": "...", "auth": "..."}}`. Send it to your server as it is:
  Web Push libraries take it as it is. The browser may change it
  (`pushsubscriptionchange`): register at each start, as on the other
  platforms, and send it when it's not the one the server has.
- `Service`: `"webpush"`.
- `Allowed`: true. Without permission, `Register` fails: a browser
  subscribes only a site that shows its pushes (`userVisibleOnly`).

It fails, saying why, without `web.vapidPublicKey`, without the worker
(an insecure page), and where the browser has no Web Push: Safari on iOS
and iPadOS has it only for an app added to the Home Screen (16.4 and
later).

A push shows as a notification, whether or not a page is open. A page
of the app that is open gets it first (`push.OnReceive`; every open
page), may rewrite it (`push.Rewrite`, which runs in the page in front,
within about ten seconds; with no page open, a push shows as it came)
and says how it shows (`notify.Present`). Its tap reaches
`push.OnOpen`, `notify.OnAction` (`Remote` set) and, with a `"url"` in
its data, `ui.OnOpenURL`, as a local notification's does.

#### What the server sends

The payload is JSON, encrypted for the subscription (RFC 8291's
`aes128gcm`) and sent with a VAPID token (RFC 8292), as every Web Push
library does. The worker reads these keys, the same a `push.Message`
has on the other platforms:

| Key | | 
| --- | --- |
| `title`, `body`, `subtitle` | what shows (the subtitle is the body's first line) |
| `data` | the app's own keys, `push.Message`'s `Data` (a value that isn't a string comes as its JSON); `"url"` is what a tap opens the app with (`ui.OnOpenURL`) |
| `id` | the notification's tag: a push with the same `id` replaces it |
| `category` | one of `notify.SetCategories`' categories: its actions are the notification's buttons |
| `thread`, `summary` | `push.Message`'s, for `notify.RemoveThread` and the app |
| `image` | a picture: an `https` URL, or one of the app's assets |
| `badge` | the installed app's badge |
| `silent` | shown without a sound |

```json
{
  "id": "chat-7",
  "title": "Ada",
  "body": "Lunch?",
  "category": "message",
  "thread": "chat-7",
  "data": { "url": "myapp://chat/7", "chat": "7" }
}
```

A push without a `title` or `body` is a data message: the open pages get
it (`OnReceive`), nothing shows. Send those only while the app is open:
a browser expects each push to show a notification, and when none
shows with no page in front, Chrome shows one of its own ("This site has
been updated in the background") and Safari ends the subscription after
a few. A payload that isn't JSON shows as the body. A payload is at most
3993 bytes.

From Go, `github.com/egoist/ryui/push/webpush` encrypts, signs and sends,
with the standard library only:

```go
import "github.com/egoist/ryui/push/webpush"

var sender = &webpush.Sender{
	Keys:    webpush.Keys{Public: os.Getenv("VAPID_PUBLIC_KEY"), Private: os.Getenv("VAPID_PRIVATE_KEY")},
	Subject: "mailto:ops@example.com", // where the push service can reach you
}

func notifyChat(ctx context.Context, token string) error {
	sub, err := webpush.ParseSubscription(token) // the app's push.Register Token
	if err != nil {
		return err
	}
	err = sender.Send(ctx, sub, webpush.Notification{
		ID: "chat-7", Title: "Ada", Body: "Lunch?", Category: "message",
		Data: map[string]string{"url": "myapp://chat/7"},
	}, &webpush.Options{Urgency: "high"})
	if errors.Is(err, webpush.ErrGone) {
		return forgetSubscription(token) // it ended: the user, or the browser
	}
	return err
}
```

`Options` set the push's `TTL` (how long the push service keeps it for
a browser that's offline; four weeks by default), `Urgency` and `Topic`
(a later push with the same topic replaces one still waiting).
`webpush.Keys.Authorization` and `webpush.Encrypt` are the two halves,
for a server that sends its own requests.

From Node, with the [`web-push`](https://github.com/web-push-libs/web-push)
library:

```js
import webpush from "web-push";

webpush.setVapidDetails("mailto:ops@example.com", process.env.VAPID_PUBLIC_KEY, process.env.VAPID_PRIVATE_KEY);

// token: the app's push.Register Token
await webpush.sendNotification(JSON.parse(token), JSON.stringify({
  title: "Ada", body: "Lunch?", category: "message", data: { url: "myapp://chat/7" },
}), { urgency: "high" });
// A 404 or 410 (err.statusCode): the subscription ended; forget it.
```

## Web views

A `WebView` is an `<iframe>`, made anew for each page it loads, so it
keeps its own history and adds nothing to the tab's.

A page of the app's own site (its assets, HTML, a file it wrote, any URL
of the page's origin) gets everything: the host loads it into the frame
with its real URL, `window.ryui.postMessage` and `Preload` there before
the page's scripts; `EvalJS` and `EvalJSResult` (a promise awaited); its
load, failure and state events (URL, title, `pushState`, progress, back
and forward); back, forward, reload and stop; its links, forms and
`location` changes load through the view; `Block` (its markup, `fetch`,
XHR, `sendBeacon`, WebSocket, EventSource, elements' `src` and `href`;
a request already under way when it's caught may still go out);
`OnRequest` (Resource Timing); `OnNewWindow` (`window.open`,
`target=_blank`); `OnDownload` (an attachment, a type a frame can't
show, `<a download>`); `WebCookies` (`document.cookie`: not `HttpOnly`
ones). HTML's relative links resolve against the app's assets.

Another site's page only loads, because browsers keep sites apart:
`Navigated` once it has loaded, `Failed` when the site can't be reached.
The view can't tell when a site refuses to be framed
(`X-Frame-Options`, `frame-ancestors`): the browser shows its own error
page. It can't follow where the page's links lead or read its title, and
can't run script there, `Block` or `Preload` it, report its requests,
read its cookies or set the user agent (script and cookies answer with an
error, the others warn once). The frame is sandboxed without top-level
navigation, so the page can't take the app away.

A page of another site that cooperates talks with the view through
`postMessage` (the view accepts messages only from its own frame, and
sends script only to the origin that said it's ready):

```js
// In the page: messages to the app (OnMessage), its URL and title, and EvalJS.
const app = (m) => parent.postMessage({ ryui: m.ryui ?? true, ...m }, "*");
addEventListener("message", async (e) => {
  if (e.source !== parent || !e.data?.ryui) return;
  if (e.data.ryui === "hello") app({ ryui: "ready", url: location.href, title: document.title });
  if (e.data.ryui === "eval") {
    try { app({ ryui: "result", id: e.data.id, value: JSON.stringify(await (0, eval)(e.data.script)) }); }
    catch (err) { app({ ryui: "result", id: e.data.id, error: String(err) }); }
  }
});
app({ ryui: "ready", url: location.href, title: document.title });
app({ text: "Hello from the page" }); // OnMessage
```

`ryui/js` runs its script in a hidden sandboxed frame of an origin of its
own, so it can't reach the app's page or storage.

## Native code

An app's own views and modules for the web are JavaScript, in `web/*.js`
beside its Go code, as its Swift is in `ios/`, its Kotlin in
`android/`, and its desktop views and modules are Go (package `native`):
see [Native code](native.md). The page loads them before the app starts; they register with
the global `ryui`:

```js
// web/natives.js
ryui.registerNative("Stepper", (ctx) => {
  const el = document.createElement("input");
  el.type = "number";
  el.oninput = () => ctx.emit("changed", { value: Number(el.value) });
  return {
    el, // the view, sized to the frame the core gives it
    set(name, value) { if (name === "value") el.value = value; },
    measure(c) { return [120, 36]; }, // its size within c ({width, height, availW, availH}, NaN when free)
  };
});

ryui.registerModule("greeter", {
  // a method takes the call's bytes and returns (or resolves to) the result's
  hello: (args) => new TextEncoder().encode("Hello, " + new TextDecoder().decode(args)),
});
```

`examples/media` has a native view, its JavaScript in
`examples/media/web/natives.js`.

## Embedding

The page `ryui build -platform web` makes runs the app full page. To run it in an
element of a page of your own, load the host's loader and start the app
in it; it then follows the element's size:

```html
<link rel="stylesheet" href="ryui/<digest>/style.css">
<div id="app" style="width: 390px; height: 700px"></div>
<script type="module">
  import { start } from "./ryui/<digest>/ryui.js";
  start({ container: document.getElementById("app"), wasm: "app.<digest>.wasm", name: "com.example.app", serviceWorker: "sw.<digest>.js" });
</script>
```

Several apps can run on one page, each in its element. `serviceWorker`
registers the app's worker (the build's `sw.<digest>.js`, from the
folder whose pages it serves): without it, notifications are the page's
own and `ryui/push` can't subscribe. Pass `vapidPublicKey` too for
`ryui/push`, as `index.html` does.

## What it doesn't do yet

- A text field's accessory bar shows only where there's an on-screen
  keyboard (a touch screen).
- A notification scheduled with `ryui/notify` can't show at its time
  while no page of the app is open: browsers have no way to schedule one
  ([above](#scheduled-notifications)).
