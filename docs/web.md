# The web

The same app runs in a browser: its Go code compiled to WebAssembly, its
views the page's DOM. Each view the core makes is an element placed at
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

The web is early, like macOS and Windows: what follows says what it does
and what it doesn't yet.

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
- the web host's files in `ryui/<digest>/`, and the app's own scripts
  (`web/*.js`) in `web/<digest>/`;
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
- A `List` is virtualized by the host, as on the desktop: only the rows
  in view (and half a screen around them) are bound; sections' headers
  stick; a chat's list stays at its end. Its rows' swipe actions are
  their context menu (a right-click, or a long press).
- `Image` is an `<img>` (decoded off the main thread, cached by the
  browser), `Video` a `<video>`, `WebView` an `<iframe>`, `Canvas` a 2D
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
  elsewhere); the page shows no menu bar of its own.

## The app's files

`ryui/store`, crash records and the files your app writes in `store.Dir`
are kept: the page gives Go a file system in memory, the data directory's
files loaded from the browser's storage (IndexedDB) before the app starts
and written back as they change. What the user drops, pastes or picks is
copied into the app's caches (`/tmp`), as on iOS and Android, and comes
to the app as a path.

## Native code

An app's own views and modules for the web are JavaScript, in `web/*.js`
beside its Go code, as its Swift is in `ios/` and its Kotlin in
`android/`. The page loads them before the app starts; they register with
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

`examples/media` has a native view with its Swift, Kotlin and JavaScript.

## Embedding

The page `ryui build -platform web` makes runs the app full page. To run it in an
element of a page of your own, load the host's loader and start the app
in it; it then follows the element's size:

```html
<link rel="stylesheet" href="ryui/<digest>/style.css">
<div id="app" style="width: 390px; height: 700px"></div>
<script type="module">
  import { start } from "./ryui/<digest>/ryui.js";
  start({ container: document.getElementById("app"), wasm: "app.<digest>.wasm", name: "com.example.app" });
</script>
```

Several apps can run on one page, each in its element.

## What it doesn't do yet

- `ryui/sqlite`: its SQLite (`modernc.org/sqlite`) has no WebAssembly
  build, so an app that imports it doesn't build for the web.
- Native modules: of RyUI's packages, `ryui/secure` (in the page's
  storage, as a web app keeps its tokens: a browser has no keychain),
  `device` (its info, the clipboard, keeping the screen awake), the
  in-app browser (a new tab), the photo picker and sharing files work;
  the others' calls answer with an error that says the web host has no
  such module.
- A `WebView` of another site only loads it: browsers keep sites apart,
  so it can't run script there (`ui.EvalJS`), take its messages or block
  its requests, and the page can't set its user agent. A page of the
  app's own (HTML, or one of its assets) does all of that.
- A text field's accessory bar shows only where there's an on-screen
  keyboard (a touch screen).
