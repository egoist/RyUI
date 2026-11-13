# On real devices: what to check

What the simulators, emulators and automated tests couldn't show, on
phones and on desktop machines. Each item says what to do and what you
should see; note anything that doesn't match.

## Getting the apps on a phone

- **Android**: plug the phone in with USB debugging on, then
  `go run ./cmd/ryui dev -platform android examples/gallery` (the player; your
  code runs on the Mac and reloads on save), or add `-once` for a build
  with the Go code in it.
- **iPhone**: `go run ./cmd/ryui build -platform ios examples/gallery` makes a
  signed `.ipa` (it needs your team in `ryui.json`'s `ios.team` and a
  provisioning profile); install it with Xcode's Devices window or
  `xcrun devicectl device install app`.
- The benchmarks: `go run ./bench/run -android` runs on a connected
  phone as it does on the emulator. On iPhone, build `bench/ryui` and
  `bench/swiftui` and run each from Xcode with the console open: they
  print `RYUIBENCH` lines.

## VoiceOver (iPhone) and TalkBack (Android)

Turn the screen reader on (Settings → Accessibility), open the gallery.

- [ ] Swiping through the screen reads each item once, in order: section
      titles, texts, buttons with their titles ("Compose, button").
- [ ] The Motion card ("Tap me") is one item, a button, read as its
      texts; double-tapping it toggles it.
- [ ] The toast: tap "Show a toast" — the reader says "Saved" (ui.Announce),
      without the checkmark's name.
- [ ] Headings: in the markdown example, the rotor (VoiceOver) or
      "Headings" navigation (TalkBack) moves between the reply's headings.
- [ ] The inbox (gallery → Open the inbox): each row is one item, "Ada,
      Message 1"; its actions (VoiceOver: swipe up/down; TalkBack: the
      actions menu, three-finger tap or swipe up then right) offer
      Archive and Delete, and either removes the row.
- [ ] A long Scroll keeps working with the reader: in the markdown
      example, swipe to the last paragraph; every one is read (rows out
      of sight aren't taken out while a reader runs).
- [ ] Text size: at the largest accessibility size, texts and the
      Pacifico font grow, and nothing is cut off on the gallery's first
      screens.
- [ ] Display size (Android: Settings → Display → Display size): change
      it with the gallery open, then go back to it. Everything is resized,
      nothing cut off or out of place (the notes example's checkboxes too).
- [ ] Reduce Motion (iOS: Accessibility → Motion): "Show a toast" fades
      instead of sliding. Android's Remove animations: it appears at once.

## Things only a device has

- [ ] **Camera** (iPhone): Platform → "Take a photo": the camera opens;
      the photo appears under the buttons.
- [ ] **Camera view** (both): gallery → "Open the camera": after the
      permission prompt the camera shows live, filling the screen; the
      shutter puts the photo, upright, at the bottom left. Going back
      turns the camera off (iPhone: the green dot goes).
- [ ] **Video** (both): on that screen, "Record", speak, "Stop": the
      microphone's asked for once, then the video plays on its own screen,
      with your voice.
- [ ] **Scanning** (both): on that screen, point it at a QR code (any on
      a screen) and a product's barcode: the status says `qr: …` or
      `ean13: …` (a UPC-A in the US: `upca: …`) once, with a haptic, while
      the code stays in view.
- [ ] **Zoom** (both): gallery → "Open the photos", tap one: its screen
      grows out of the photo; back (and the back swipe, which follows your
      finger) shrinks it into the photo again.
- [ ] **Two-finger rotation** (iPhone): Motion → the purple square turns
      with two fingers and the label follows ("Turned 30°").
- [ ] **Sound**: Platform → "Play a chime": you hear it, with the
      silent switch on too (iPhone).
- [ ] **Haptics**: the navigation example's Settings → "Try a haptic":
      a tap is felt.
- [ ] **Location**: "Where am I?" asks once, then shows your position
      within some tens of meters.
- [ ] **Notifications**: "Remind me in 5 s", then lock the phone: the
      notification shows on the lock screen within a few seconds
      (Android's can be later: it's an inexact alarm).
- [ ] **Keychain / Keystore**: "Keep a secret" says "Kept: s3cr3t-…";
      after killing and reopening the app, pressing it again still works.
- [ ] **Links**: in the notes example, open `ryuinotes://add?text=hello`
      from Safari / a browser: a note "hello" is added.

## Performance

- [ ] The gallery scrolls without stutter, top to bottom, including the
      photo grid (gallery → Open the photos).
- [ ] `go run ./bench/run -android` on the phone: the update and scroll
      rows' "dropped frames" are near 0 for RyUI (the emulator's GPU made
      them high).
- [ ] Startup: the counter example shows within about half a second of
      tapping its icon.

Send back what you saw (or screenshots of what looks wrong), and I'll
fix what didn't match.

## Languages

- On an iPhone: choose the app's language in Settings; the app relaunches in it, with
  UIKit's own words. `ui.SetLocale`, then relaunch.
- iOS 16: the navigation bar's back chevron in a language forced against the system's.
- On Android 12 or earlier: `ui.SetLocale`, kill the app, relaunch (RyUI's preferences).
- Android, in Arabic: predictive back, a sheet, dragging selection handles, TalkBack.
- Windows: Arabic and English Windows, the mirrored title
  and menu bars, clicks landing on the right views, a runtime switch with a sheet and a
  window open, Arabic font fallback and punctuation in `DT_RTLREADING`, `TPM_LAYOUTRTL`
  menus opening on the right side, RichEdit's paragraph direction. Then
  `go test ./host/windows ./internal/windows`.

## Previewing, printing, composing

In the gallery's Documents section.

- [ ] iPhone and iPad: "Preview a PDF" grows Quick Look from the button;
      both pages show; closing it comes back to the gallery.
- [ ] Android: "Preview a PDF" asks which app opens PDFs (or opens the one
      set); back returns to the gallery. On a phone without a PDF app the
      status says nothing can show it.
- [ ] "Print a receipt": the print screen with the receipt (one page);
      printing to a printer, or Save as PDF on Android, then the status
      says "Print: printed"; backing out says "cancelled". On the iPad the
      sheet is a popover.
- [ ] The web page's Print button prints what the page shows.
- [ ] "Make a PDF and share it": the share sheet offers "Receipt.pdf"; it
      opens with its text selectable.
- [ ] "Mail the PDF" with a Mail account (iPhone): Mail's composer with
      the PDF attached and "gallery's" in bold; sending says "Mail: sent",
      deleting the draft "cancelled", saving it "saved". On Android: only
      mail apps are offered, and the PDF is attached.
- [ ] "Text a friend": Messages' composer (iPhone), the SMS app (Android),
      with the number and the text.
- [ ] Windows: Preview opens the PDF in its
      app; Print shows the system's print dialog; the PDF is made
      (`go test ./host/windows -run Documents`); Mail with Outlook classic
      or Thunderbird set as the mail app shows its composer with the PDF.

## On the desktop

Run the examples on each with `go run ./cmd/ryui dev examples/gallery`
(and `examples/notes`) on a Mac, a Windows PC and a Linux machine with
GNOME, on real hardware rather than a VM where you can.

- [ ] Screen readers: VoiceOver (macOS: ⌘F5), Narrator (Windows:
      Ctrl+Win+Enter), Orca (GNOME: Super+Alt+S). Tabbing and the
      reader's own navigation reach each control of the gallery, read
      with its title and role; `ui.Announce` (the toast) is spoken.
- [ ] Keyboard: Tab and Shift+Tab move through the controls in order,
      with a visible focus ring; Space and Enter press buttons; Escape
      closes a sheet, an alert and a menu.
- [ ] The menu bar (notes): File → Add Note, Edit → Clear Completed and
      Window → Statistics work from the menu and from their shortcuts
      (⌘ on a Mac, Ctrl elsewhere); Statistics opens a window of its
      own, which follows the notes as they change.
- [ ] Resizing: the window resizes smoothly to its least size (notes:
      320×360), and the gallery's layout follows; a width past 600 and
      840 points changes the size class (a sidebar beside the content
      where an example has one).
- [ ] Scaling: move the window between a 100% and a 150% or 200%
      display (Windows, Linux with fractional scaling): text and images
      stay sharp, and nothing moves or is cut off.
- [ ] Text size (Windows: Accessibility → Text size; GNOME: Large
      Text): texts grow, and nothing is cut off.
- [ ] Dark mode, switched with the app open: every screen, sheet and
      menu follows at once.
- [ ] Reduce motion (macOS: Accessibility → Display; Windows: Animation
      effects off; GNOME: Animations off): pushes and the toast don't
      slide.
- [ ] Trackpad and touch: two-finger scrolling with momentum, pinch and
      rotate on Motion's purple square, a touch screen's taps and pans
      (Windows, GNOME).
- [ ] A release build (`ryui build`), installed as a user would: the
      Mac app notarized and opened from the Finder, the Windows folder's
      `.exe` signed (SmartScreen names the publisher), the Linux
      `.tar.gz` extracted into `~/.local` and opened from the launcher,
      and the Flatpak. Each shows its name and icon in the Dock, taskbar
      or dash, and keeps its data across launches.
