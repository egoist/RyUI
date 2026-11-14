# Asuka and MyGo

[MyGo](https://mygo.run) makes desktop apps in Go. Both are Go alone,
and both call the desktop without cgo. They differ in where an app runs
and, above all, in what shows its interface: MyGo draws it, or shows a
web page; Asuka shows the platform's own widgets.

## What shows the interface

A MyGo window shows one of two things:

- **UI that MyGo draws itself**, on the GPU (Metal on the Mac, Direct3D 11
  on Windows, OpenGL), in a look of MyGo's theme. Its text comes from the
  system's text engines, and it builds an accessibility tree of what it
  draws.
- **A web page**, in the system's web view (WKWebView, WebView2,
  WebKitGTK), which calls the app's Go through a TypeScript client MyGo
  generates.

An Asuka app shows the platform's own widgets. A `ui.Button` is a
`UIButton` on iOS, a `MaterialButton` on Android, an `NSButton` on the
Mac, a Win32 `BUTTON` drawn as Windows 11's on Windows, a `GtkButton` on
Linux and a `<button>` in a browser; [Components](components.md) has
them all. Asuka lays them out, with one layout engine in Go for every
platform, and sets their properties; the platform draws them, and they
work as its own: its text input and autocorrection, its input methods,
its screen readers, its menus, scrolling and focus.

What follows from it:

- **The look.** An Asuka app that sets no style looks as each platform's
  apps do, and follows the platform when it changes (a new iOS restyles
  its buttons, a new libadwaita its switches). A MyGo window looks as
  MyGo's theme says, drawn the same way on each system.
- **Text, input and accessibility.** Asuka's are the platform's, done by
  the platform. MyGo's are MyGo's, made over what it draws.
- **What no widget shows.** MyGo draws anything as it draws the rest.
  Asuka has packages for what has no widget, drawing with the platform:
  `asuka/sdk/chart`, `asuka/sdk/svg`, and `ui.Shader` for shaders.
- **Images without a screen.** MyGo has a software renderer: it draws
  on the CPU, in memory, with no window, display or GPU, so a test or a
  server renders a view to an image on any machine (`ui.NewTester`,
  `ui.Render`). Asuka's widgets are drawn by the platform, so an image
  of them needs the platform running: a window on the desktop
  (`ASUKA_SNAPSHOT`; Xvfb on a Linux server), the simulator, an emulator
  or a browser. Without one, `uitest` describes the screen as text: each
  view's kind, frame and what it shows ([Testing](testing.md)).

## Where it runs

| | MyGo | Asuka |
|---|---|---|
| Phones | — | iOS and Android |
| Desktops | Mac, Windows, Linux | Mac, Windows, Linux |
| Browsers | its drawn UI, on a canvas with WebGL 2 | the page's DOM, from WebAssembly ([The web](web.md)) |

## How a view updates

A MyGo view is a function of the app's state, built again from it for
every frame. An Asuka component runs once: it makes its views and wires
them to signals, and a signal's change updates only the views that read
it, in one batch for the frame ([Concepts](concepts.md)).

## When to use each

Use Asuka when:

- the app is for phones, or for phones, desktops and the web from one
  code base;
- it should look and work as each platform's apps do: its controls, its
  text input, its accessibility, its navigation.

Use MyGo when:

- the app is for the desktop and its interface is a web frontend:
  existing web code, HTML and CSS, rich documents, what only a browser
  has;
- it should have one look of its own, drawn the same on every desktop,
  rather than each platform's: an editor, an inspector, a game;
- its views should render to images headlessly: snapshot tests that
  compare pixels, or images made on a server.
