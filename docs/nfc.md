# NFC tags

`asuka/sdk/nfc` reads and writes NFC tags: their NDEF messages (links,
texts, data of the app's own), the tags themselves (their IDs and
technologies), and raw commands to them. It's what
react-native-nfc-manager and Web NFC do, on each platform's own API: iOS's
Core NFC with the system's sheet, Android's reader mode, and Web NFC in
Chrome on Android. A Mac, a PC and Linux answer that they have none.

```go
s := nfc.Start(nfc.Options{Alert: "Hold your iPhone near the tag."}, func(t *nfc.Tag) {
	if u, ok := t.Message.URI(); ok {
		link.Set(u)
	}
	s.Stop()
})
```

[examples/nfc](../examples/nfc) reads the link on a tag and writes one
to it.

## Is NFC there and on

`nfc.Check` says whether the device has NFC (`Supported`) and whether
it's on (`Enabled`); not on the main thread. `nfc.Use` is the same as a
signal for a component, which follows Android's switch while the
component lives; `nfc.Watch` calls a function with each change.

```go
state := nfc.Use()
ui.Show(signal.Func(func() bool { s := state.Get(); return s.Supported && !s.Enabled }), func() ui.Node {
	return ui.Button(ui.ButtonProps{Title: ui.Val("Turn NFC On"), OnPress: func() { go nfc.OpenSettings() }})
})
```

iPhones have NFC from the iPhone 7, and no switch for it: `Enabled` is
`Supported` there. iPads and the simulator have none. On Android,
`OpenSettings` opens the system's NFC settings for the user to turn it
on; elsewhere it's `ErrNotAvailable`. A browser can't tell whether NFC
is off until a session finds it off (`ErrDisabled`).

## Reading tags: sessions

`nfc.Start` begins a session that looks for tags and calls its function,
on the main thread, with each one it finds. Called in a component (its
body, an effect), the session ends with the component's owner; from an
event handler, it lasts until `Stop`, `Fail`, or (iOS) the user closes
the sheet or it times out. One session runs at a time: starting one ends
the one before.

`Options.OnEnd` is called as the session ends: `nil` when the app ended
it, `nfc.ErrCanceled` when the user closed iOS's sheet, `nfc.ErrTimeout`
after iOS's sixty seconds, and `ErrNotAvailable`, `ErrDisabled` or
`ErrDenied` when it couldn't begin.

A `Tag` is what the tag said as it was found: its `ID`, its `Techs`, its
NDEF status (`NDEF`: `Format`, `Capacity`, `CanMakeReadOnly`, Android's
`Type`) and its `Message` (nil if none was read), with the details of
its technology where the platform gives them (`ISO7816`, `FeliCa`,
`ISO15693`, `MIFARE`, `NfcA`, `NfcB`, `Barcode`, `MaxTransceive`).

### One tag at a time

A session hands the app one tag, and holds it while the app works with
it: reads it again, writes it, sends it commands. Then `Next` lets it go
and looks for another. A tag that comes near while the app holds one
waits for `Next`. On iOS, `Next` restarts polling, which finds a tag
that's still near again; Android finds a tag only as it comes near.

```go
nfc.Start(nfc.Options{OnEnd: ended}, func(t *nfc.Tag) {
	count.Update(func(n int) int { return n + 1 })
	s.Next() // the next tag
})
```

### iOS's sheet

On iOS a session shows the system's sheet. `Options.Alert` is what it
says while it looks; `SetAlert` changes it (`"Writing…"`). `Stop` closes
the sheet with its tick and the alert's message; `Fail("Couldn't write
the tag.")` closes it with that failure. Android and the web show no
sheet: the app shows its own, and the alert does nothing there.

`Options.Poll` chooses the session. Without it, iOS's
`NFCNDEFReaderSession` reads tags with an NDEF message: its tags have no
ID and take no raw commands. With it (`nfc.PollA`, `PollB`, `PollF`,
`PollV`, `PollPACE`, or `PollAll`), iOS's `NFCTagReaderSession` polls for
those kinds of tags, with their IDs and commands. ISO 7816 cards need
their applications in asuka.json's `nfc.iso7816`, and FeliCa cards their
system codes in `nfc.felica`: iOS finds no others.

### Android's reader mode

On Android a session is reader mode (`enableReaderMode`) for the screen
in front, so tags come to the app and not to other apps, and Android
doesn't show its own tag screen. It's taken again for an Activity that
comes back to the front. `Options.Poll` is reader mode's flags (none:
every kind); `SkipNDEF` doesn't read the NDEF message as a tag is found
(`FLAG_READER_SKIP_NDEF_CHECK`: faster for tags that only take
commands); `NoSound` turns off the platform's sound, for the app's own
(`ui.Haptic`); `PresenceDelay` is how often Android checks the tag is
still there.

## NDEF messages and records

An NDEF message is a `nfc.Message`: its `Record`s, each a `TNF`, a
`Type`, an `ID` and a `Payload`. These make the records apps use most:

| Make | Read | What it is |
| --- | --- | --- |
| `nfc.URI("https://example.com")` | `r.URI()` | A link (any URI: `tel:`, `mailto:`, `geo:`), its prefix abbreviated as the platforms do |
| `nfc.Text("Hello", "en")` | `r.Text()` | A text and its language (UTF-8 or UTF-16 read) |
| `nfc.MIME("text/vcard", data)` | `r.MediaType()` | Data of a MIME type |
| `nfc.External("example.com", "ticket", data)` | `r.ExternalType()` | Data of the app's own type (`example.com:ticket`) |
| `nfc.SmartPoster(nfc.Poster{...})` | `r.SmartPoster()` | A link with titles in languages, an action, an icon, a size and a type |
| `nfc.AndroidApp("com.example.app")` | | An Android Application Record: Android opens that app for the tag |

`Message.URI` is the first record's URI (a URI record, an absolute URI,
a smart poster's), the one Android opens the app with; `Message.Text`
the first text. `Message.Encode` and `nfc.Decode` are the message's
bytes, as a tag holds them (`Decode` joins chunked records); an empty
tag's message is one empty record, which decodes to an empty `Message`.

## Writing and making tags read-only

While the app holds a tag, `Write` replaces its message, and
`MakeReadOnly` locks it for good: no one can write it again. Both wait
for the tag: call them from a goroutine, then go back to the main thread
for the session.

```go
nfc.Start(nfc.Options{Alert: "Hold your iPhone near a tag to write it."}, func(t *nfc.Tag) {
	if !t.NDEF.Writable() {
		s.Fail("This tag can't be written.")
		return
	}
	s.SetAlert("Writing…")
	go func() {
		err := t.Write(nfc.Message{nfc.URI("https://example.com/t/42")})
		ui.Post(func() {
			if err != nil {
				s.Fail("Couldn't write the tag.")
				return
			}
			s.Stop()
		})
	}()
})
```

`Write` fails with `ErrReadOnly` on a locked tag and `ErrTooLarge` for a
message larger than `NDEF.Capacity`; a `Formatable` tag (not NDEF yet,
Android's `NdefFormatable`) is formatted with the message. `Read` reads
the message again, `Status` the NDEF status. A tag that leaves, or is
let go, fails with `ErrTagLost`.

## The tag itself: IDs, technologies, commands

`Tag.Techs` are the technologies the tag has. Both platforms report
`nfc.NDEF`, `ISO7816` (ISO 7816 cards: ISO 14443-4), `MIFARE` (ISO
14443-3A tags: NTAG, MIFARE Ultralight, DESFire), `FeliCa` and
`ISO15693`; Android also reports its own (`NfcA`, `NfcB`, `NfcF`,
`NfcV`, `IsoDep`, `MifareClassic`, `MifareUltralight`, `NDEFFormatable`,
`NfcBarcode`). `ID` is the tag's identifier as the platform gives it
(Android's NFC-V IDs least significant byte first).

`Transceive` sends a raw command and answers with the response. iOS
needs a session with `Poll`; the web has none (`ErrNotSupported`).

| Tech | What it sends |
| --- | --- |
| `ISO7816` | An APDU; the answer ends with its status words (`Command` reads them) |
| `MIFARE` | An ISO 14443-3A frame, without its CRC (the platform adds it) |
| `FeliCa` | A command, without its length byte (the platform adds it, and takes the answer's off) |
| `ISO15693` | A request: flags, a command code, then its parameters (an addressed one's UID among them); the answer is its flags, then its data |
| Android's `NfcA`, `NfcB`, `NfcF`, `NfcV`, `IsoDep`, `MifareUltralight`, `MifareClassic` | Their frames as Android's classes take them (`NfcF` with its length byte) |

```go
r, err := t.Command(nfc.SelectAID(aid)) // SELECT by AID
if err == nil && r.OK() {
	r, err = t.Command(nfc.APDU{CLA: 0x00, INS: 0xB0, P1: 0, P2: 0, Le: 256}) // READ BINARY
}
```

`nfc.APDU` encodes a command, short or extended as its data and `Le`
need; `Response` is its `Data`, `SW1` and `SW2` (`OK`: 90 00).

MIFARE Classic's sectors are read and written on Android only (iOS
reads no MIFARE Classic): `ClassicAuth` with a sector's key A or B, then
`ClassicRead` and `ClassicWrite` of its 16-byte blocks, and
`ClassicValue` for value blocks (`Increment`, `Decrement`, `Restore`,
`Transfer`).

## Tags that open the app

A tag can open the app when it isn't open, or bring it to the front:

- **iOS** reads tags in the background (iPhone XS and later) and offers
  to open an app for a tag whose first record is one of its universal
  links: asuka.json's `links`, whose domains serve the
  apple-app-site-association file. The link comes to `ui.OnOpenURL`, as
  any link does.
- **Android** opens the app for the tags of asuka.json's `nfc.ndef`
  (NDEF_DISCOVERED, by the message's first record: a URI, a MIME type,
  an external type) and `nfc.tech` (TECH_DISCOVERED, by technologies). A
  URI's tag comes to `ui.OnOpenURL`; the others to `nfc.OnOpen`, as a
  `Tag` the app can still read, write and send commands while it's
  near. An https tag of one of the app's `links` opens it as an App
  Link too.

```go
ui.OnOpenURL(func(u string) { link.Set(u) })
nfc.OnOpen(func(t *nfc.Tag) {
	if mt, ok := t.Message[0].MediaType(); ok && mt == "application/vnd.example.ticket" {
		ticket.Set(string(t.Message[0].Payload))
	}
})
```

A tag that comes before a handler is set (the one the app launched
with) waits for the first. `nfc.OnOpen` stops with the component's
owner, and with the function it returns. A tag that comes while a
session runs goes to the session, not here.

## asuka.json and permissions

An app that imports `asuka/sdk/nfc` gets Android's `NFC` permission and
`uses-feature android.hardware.nfc` (not required: phones without NFC
can still install it), and on iOS `NFCReaderUsageDescription` (write
your own in `ios.usage`) and Core NFC's entitlement,
`com.apple.developer.nfc.readersession.formats` (`NDEF` and `TAG`). A
release's App ID needs the NFC Tag Reading capability: `asuka build`
says so if its profile lacks it.

asuka.json's `nfc` adds what the app's tags need:

```json
"nfc": {
  "iso7816": ["D2760000850101"],
  "felica": ["8008"],
  "ndef": [{ "uri": "https://example.com/t/" }, { "mime": "application/vnd.example.ticket" }, { "external": "example.com:ticket" }],
  "tech": [["IsoDep"], ["NfcA", "MifareUltralight"]],
  "required": true
}
```

| Key | What it is |
| --- | --- |
| `iso7816` | The ISO 7816 applications (AIDs, hex) iOS selects as it finds a card, in order (Info.plist's `com.apple.developer.nfc.readersession.iso7816.select-identifiers`) |
| `felica` | The FeliCa system codes (4 hex digits) iOS polls for (`com.apple.developer.nfc.readersession.felica.systemcodes`) |
| `ndef` | Android's tags that open the app, by their first record: `uri` (a scheme, host and path prefix), `mime` (text records are `text/plain`), `external` (`domain:type`) |
| `tech` | Android's tags that open the app by technologies: each list, those a tag must all have (`android.nfc.tech`'s classes) |
| `required` | The app is for phones with NFC alone: Google Play offers it to no others |

They're added only for an app that imports the package. See
[asuka.json](asuka-json.md#nfc).

## In asuka dev

The simulator and the emulator have no NFC: `Check` says unsupported.
On an Android phone, the player has the `NFC` permission, so sessions,
reading, writing and commands all work under `asuka dev`; tags that
open the app don't (the filters are the app's own build's: try them with
`--no-hot`; `asuka dev` says so). On an iPhone the player's profile needs
the NFC Tag Reading capability, and ISO 7816 cards and FeliCa need the
app's own build (their AIDs and system codes are its Info.plist's), so
`asuka dev` builds an app with `nfc.iso7816` or `nfc.felica` instead of
running it in the player. Web NFC needs a secure context, which
`--lan`'s addresses aren't: run `asuka dev --platform web`, forward its
port to the phone (`adb reverse tcp:<port> tcp:<port>`), and open
`http://localhost:<port>` in Chrome on the phone.

## On each platform

| Platform | What it does |
| --- | --- |
| iOS | Core NFC (iOS 16): `NFCNDEFReaderSession` without `Poll`, `NFCTagReaderSession` with it; the system's sheet and its alert; ISO 7816, MIFARE (Ultralight, Plus, DESFire), FeliCa and ISO 15693 commands; background tag reading through universal links. No MIFARE Classic, no NFC switch, no tags of the app's own type opening it |
| Android | Reader mode for the Activity in front; every technology of `android.nfc.tech`, MIFARE Classic's sectors where the phone's NFC controller reads them (NXP's); NDEF_DISCOVERED and TECH_DISCOVERED opening the app; `OpenSettings` |
| The web | Web NFC's `NDEFReader` (Chrome on Android, https): NDEF reading and writing, `MakeReadOnly` (Chrome 100), the tag's serial number. No raw commands, no capacity (`-1`), no tags opening the page; `Read` gives the message as it was read. Other browsers have no Web NFC: `ErrNotAvailable` |
| macOS, Windows, Linux | `ErrNotAvailable`: `Check` says unsupported |

A desktop's USB NFC readers are smart card readers (PC/SC): they read
cards' APDUs, not tags' NDEF messages in a way that works across
readers (each vendor's commands differ), and have no tag to tap
from a session's sheet. Asuka leaves them out; a native package can
reach them (`winscard.dll`, pcsc-lite, CryptoTokenKit).

## Testing with simulated tags

`host/fake`'s `NFC` simulates the phone's NFC for an app's UI tests
([Testing](testing.md)): `Tap` brings a tag near the session running,
`Cancel` and `Timeout` end it as iOS does, `Remove` takes the tag away,
`Open` opens the app with one, and `SetEnabled` turns NFC off. A
`fake.NFCTag` holds its NDEF message's bytes, which the app's writes
change.

```go
func TestRead(t *testing.T) {
	var n *fake.NFC
	a := uitest.Mount(t, Tags, uitest.Setup(func(h *fake.Host) { n = h.NFC() }))
	a.Tap("Read a Tag")
	a.Until("a session", func() bool { return n.Session != nil })
	n.Tap(&fake.NFCTag{NDEF: nfc.Message{nfc.URI("https://example.com/hello")}.Encode()})
	a.WaitFor("https://example.com/hello")
}
```

`n.Session` is the session running, with its `Alert`, its `Poll` and
the rest of its options; a tag's `Transceive` and `Classic` answer its
commands.
