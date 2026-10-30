# On real devices: what to check

What the simulators and emulators couldn't show. Each item says what to
do and what you should see; note anything that doesn't match.

## Getting the apps on a phone

- **Android**: plug the phone in with USB debugging on, then
  `go run ./cmd/nui dev -android examples/gallery` (the player; your
  code runs on the Mac and reloads on save), or add `-once` for a build
  with the Go code in it.
- **iPhone**: `go run ./cmd/nui build -ios examples/gallery` makes a
  signed `.ipa` (it needs your team in `nui.json`'s `ios.team` and a
  provisioning profile); install it with Xcode's Devices window or
  `xcrun devicectl device install app`.
- The benchmarks: `go run ./bench/run -android` runs on a connected
  phone as it does on the emulator. On iPhone, build `bench/nui` and
  `bench/swiftui` and run each from Xcode with the console open: they
  print `NUIBENCH` lines.

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
- [ ] **Links**: in the notes example, open `nuinotes://add?text=hello`
      from Safari / a browser: a note "hello" is added.

## Performance

- [ ] The gallery scrolls without stutter, top to bottom, including the
      photo grid (gallery → Open the photos).
- [ ] `go run ./bench/run -android` on the phone: the update and scroll
      rows' "dropped frames" are near 0 for nui (the emulator's GPU made
      them high).
- [ ] Startup: the counter example shows within about half a second of
      tapping its icon.

Send back what you saw (or screenshots of what looks wrong), and I'll
fix what didn't match.
