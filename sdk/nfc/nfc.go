// Package nfc reads and writes NFC tags (react-native-nfc-manager, Web
// NFC): their NDEF messages, the tags themselves, and raw commands to
// them. iOS's Core NFC, with the system's sheet; Android's reader mode
// for the app's screen in front; Web NFC in Chrome on Android. A Mac, a
// PC and Linux answer ErrNotAvailable.
//
//	s := nfc.Start(nfc.Options{Alert: "Hold your iPhone near the tag."}, func(t *nfc.Tag) {
//		if u, ok := t.Message.URI(); ok {
//			link.Set(u)
//		}
//		s.Stop()
//	})
//
// A session looks for tags until it's stopped, its owner is disposed,
// or (iOS) the user closes the sheet or it times out. It hands the app
// one tag at a time; Next looks for another. While the app holds a tag,
// it can read and write its message, make it read-only and send it
// commands: Tag's methods, which wait for the tag (not on the main
// thread).
//
// Tags read while the app isn't open come to it too: on iOS, a tag
// whose URI is one of the app's universal links (asuka.json's links)
// opens it, as any of its links do (ui.OnOpenURL); on Android, the tags
// of asuka.json's nfc filters, the URIs to ui.OnOpenURL and the others
// to OnOpen.
package nfc

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/egoist/asuka/internal/nfcspec"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// The errors the platforms' answers come as (the platform's own words
// after them, as "nfc: canceled: ...").
var (
	// ErrNotAvailable: the device has no NFC, or the platform gives apps
	// none (a Mac, a PC, Linux; a browser without Web NFC; the simulator).
	ErrNotAvailable = errors.New("nfc: not available")
	// ErrDisabled: NFC is turned off (Android's settings: OpenSettings).
	ErrDisabled = errors.New("nfc: turned off")
	// ErrDenied: the user didn't allow it (the web's permission).
	ErrDenied = errors.New("nfc: not allowed")
	// ErrCanceled: the user closed iOS's sheet.
	ErrCanceled = errors.New("nfc: canceled")
	// ErrTimeout: iOS's session ended after its sixty seconds.
	ErrTimeout = errors.New("nfc: timed out")
	// ErrTagLost: the tag left, or its session ended, or another tag took
	// its place.
	ErrTagLost = errors.New("nfc: the tag was lost")
	// ErrReadOnly: the tag can't be written.
	ErrReadOnly = errors.New("nfc: the tag is read-only")
	// ErrTooLarge: the message is larger than the tag holds.
	ErrTooLarge = errors.New("nfc: the message is too large for the tag")
	// ErrNotSupported: the tag (or the platform) doesn't do that: no
	// NDEF, no such technology, no making it read-only.
	ErrNotSupported = errors.New("nfc: not supported by the tag")
)

var codes = []struct {
	word string
	err  error
}{
	{nfcspec.ErrNotAvailable, ErrNotAvailable},
	{nfcspec.ErrDisabled, ErrDisabled},
	{nfcspec.ErrDenied, ErrDenied},
	{nfcspec.ErrCanceled, ErrCanceled},
	{nfcspec.ErrTimeout, ErrTimeout},
	{nfcspec.ErrTagLost, ErrTagLost},
	{nfcspec.ErrEnded, ErrTagLost}, // a tag's session ended
	{nfcspec.ErrReadOnly, ErrReadOnly},
	{nfcspec.ErrTooLarge, ErrTooLarge},
	{nfcspec.ErrNotSupported, ErrNotSupported},
}

// hostError is a host's error as the package's: its word's, with the
// rest of its message after.
func hostError(err error) error {
	if err == nil {
		return nil
	}
	s := err.Error()
	if strings.HasPrefix(s, "no module method nfc.") || strings.HasPrefix(s, "asuka: nfc.") || s == `no module "nfc"` {
		return ErrNotAvailable // a host without the module (an app built without it)
	}
	for _, c := range codes {
		if s == c.word {
			return c.err
		}
		if rest, ok := strings.CutPrefix(s, c.word+": "); ok {
			return fmt.Errorf("%w: %s", c.err, rest)
		}
	}
	return fmt.Errorf("nfc: %s", s)
}

// State is whether the device has NFC, and whether it's on.
type State struct {
	// Supported: the device has NFC, and the platform lets apps read
	// tags (iOS's readingAvailable; Android's adapter; the web's
	// NDEFReader).
	Supported bool
	// Enabled: it's on. iPhones have no switch for it; Android's is in
	// its settings (OpenSettings); a browser says nothing until a session
	// finds it off (ErrDisabled), so it's true there.
	Enabled bool
}

func decodeState(b []byte, err error) (State, error) {
	if err != nil {
		if hostError(err) == ErrNotAvailable {
			return State{}, nil
		}
		return State{}, hostError(err)
	}
	var s nfcspec.State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, err
	}
	return State{Supported: s.Supported, Enabled: s.Supported && s.Enabled}, nil
}

// Check is whether the device has NFC and whether it's on. Not on the
// main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Check() (State, error) {
	return decodeState(ui.CallWait("nfc", "state", nil))
}

// OpenSettings opens the system's NFC settings, for the user to turn it
// on (Android); elsewhere ErrNotAvailable. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func OpenSettings() error {
	_, err := ui.CallWait("nfc", "settings", nil)
	return hostError(err)
}

var (
	stateMu       sync.Mutex
	stateWatchers = map[*func(State)]bool{}
	stateRunning  bool
)

// Watch calls f, on the main thread, with the state now and each time
// it changes (Android's NFC turned on or off), until stop. Main thread,
// and stop too.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Watch(f func(State)) (stop func()) {
	fp := &f
	stateMu.Lock()
	stateWatchers[fp] = true
	start := !stateRunning
	stateRunning = true
	stateMu.Unlock()
	if start {
		go followState()
	} else {
		go func() {
			s, err := Check()
			if err != nil {
				return
			}
			ui.Post(func() {
				stateMu.Lock()
				live := stateWatchers[fp]
				stateMu.Unlock()
				if live {
					f(s)
				}
			})
		}()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			stateMu.Lock()
			delete(stateWatchers, fp)
			last := len(stateWatchers) == 0
			stateMu.Unlock()
			if last {
				// Now, not posted: in a cleanup, the root's disposal sends it.
				ui.Call("nfc", "stopState", nil, nil)
			}
		})
	}
}

// followState gives the watchers the state, then each change ("nextState"
// answers once it isn't the one given), until no one watches or the
// platform's never changes ("not available").
func followState() {
	b, err := ui.CallWait("nfc", "state", nil)
	for {
		s, derr := decodeState(b, err)
		stateMu.Lock()
		fs := make([]func(State), 0, len(stateWatchers))
		for f := range stateWatchers {
			fs = append(fs, *f)
		}
		if len(fs) == 0 || derr != nil {
			stateRunning = false
			stateMu.Unlock()
			return
		}
		stateMu.Unlock()
		ui.Post(func() {
			for _, f := range fs {
				f(s)
			}
		})
		last, _ := json.Marshal(nfcspec.State{Supported: s.Supported, Enabled: s.Enabled})
		b, err = ui.CallWait("nfc", "nextState", last)
		if err == nil {
			continue
		}
		// Stopped as another began watching: the state again, for it.
		stateMu.Lock()
		again := err.Error() == nfcspec.ErrStopped && len(stateWatchers) > 0
		if !again {
			// A platform whose state never changes: done (a watcher
			// that comes later starts again).
			stateRunning = false
		}
		stateMu.Unlock()
		if !again {
			return
		}
		b, err = ui.CallWait("nfc", "state", nil)
	}
}

// Use is the NFC state as a signal, for a component: it follows changes
// while the component lives. Main thread.
func Use() signal.Accessor[State] {
	s := signal.New(State{})
	stop := Watch(func(n State) { s.Set(n) })
	signal.OnCleanup(stop)
	return s
}

// Poll is what kinds of tags a session looks for.
type Poll uint32

const (
	PollA Poll = nfcspec.PollA // ISO 14443-A: NTAG, MIFARE, most ISO 7816 cards
	PollB Poll = nfcspec.PollB // ISO 14443-B: some ISO 7816 cards (identity cards)
	PollF Poll = nfcspec.PollF // FeliCa (ISO 18092): Suica, nanaco, Octopus...
	PollV Poll = nfcspec.PollV // ISO 15693 (NFC-V): library and ski tags, ICODE
	// PollBarcode: Android's NFC barcodes (Thinfilm, Kovio).
	PollBarcode Poll = nfcspec.PollBarcode
	// PollPACE: iOS's identity documents that use PACE (iOS 16; before
	// iOS 26.4 it looks for those alone).
	PollPACE Poll = nfcspec.PollPACE
	// PollAll is every kind of tag both platforms read.
	PollAll = PollA | PollB | PollF | PollV
)

// Options are a session's.
type Options struct {
	// Alert is what iOS's sheet says while it looks ("Hold your iPhone
	// near the tag."): shown once, a string. Session.SetAlert changes it.
	// Android and the web show no sheet: the app shows its own.
	Alert string
	// Poll is what the session looks for. None: tags with an NDEF
	// message (iOS's NFCNDEFReaderSession, whose tags have no ID and take
	// no raw commands; elsewhere every kind). Any: those kinds, with
	// their IDs and raw commands (iOS's NFCTagReaderSession, which needs
	// asuka.json's nfc.iso7816 for ISO 7816 cards and nfc.felica for
	// FeliCa; Android's reader mode flags). The web reads NDEF only.
	Poll Poll
	// SkipNDEF: don't read the NDEF message as a tag is found (Android's
	// FLAG_READER_SKIP_NDEF_CHECK, faster for tags that are only sent
	// commands): Tag.Message is nil and Tag.NDEF unknown; Read still
	// reads it. iOS: a tag session's tags aren't asked.
	SkipNDEF bool
	// NoSound: Android plays no sound as it finds a tag
	// (FLAG_READER_NO_PLATFORM_SOUNDS), for the app's own (ui.Haptic).
	NoSound bool
	// PresenceDelay is how often Android checks the tag is still there
	// (EXTRA_READER_PRESENCE_CHECK_DELAY; 0: the platform's, 125 ms).
	PresenceDelay time.Duration
	// OnEnd is called on the main thread as the session ends: nil when
	// the app ended it (Stop, Fail, its owner disposed, another session
	// begun); ErrCanceled when the user closed iOS's sheet, ErrTimeout
	// after iOS's sixty seconds; ErrNotAvailable, ErrDisabled or
	// ErrDenied if it couldn't begin.
	OnEnd func(error)
}

// Session looks for tags: Start makes one.
type Session struct {
	id    uint32
	o     Options
	found func(*Tag)
	// Main thread: active until it ends; ended once OnEnd was called.
	active, ended bool
}

var (
	sessionID uint32
	current   *Session // main thread
)

// Start begins a session that calls found, on the main thread, with
// each tag it finds, one at a time (Next looks for the next one). One
// session runs at a time: starting one ends the one before. Called in a
// component (its body, an effect), it ends with the component's owner;
// otherwise (an event handler) when it's stopped, or ends by itself.
// Main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Start(o Options, found func(*Tag)) *Session {
	if current != nil {
		current.Stop()
	}
	sessionID++
	s := &Session{id: sessionID, o: o, found: found, active: true}
	current = s
	signal.OnCleanup(s.Stop)
	args, _ := json.Marshal(nfcspec.Start{ID: s.id, Alert: o.Alert, Poll: uint32(o.Poll), SkipNDEF: o.SkipNDEF,
		NoSound: o.NoSound, PresenceDelay: int(o.PresenceDelay / time.Millisecond)})
	ui.Call("nfc", "start", args, func(_ []byte, err error) {
		if err != nil {
			s.finish(hostError(err))
			return
		}
		go s.follow()
	})
	return s
}

func (s *Session) args(alert, failure string) []byte {
	b, _ := json.Marshal(nfcspec.Session{ID: s.id, Alert: alert, Error: failure})
	return b
}

// follow waits for the session's tags ("next"), until it ends.
func (s *Session) follow() {
	args := s.args("", "")
	for {
		b, err := ui.CallWait("nfc", "next", args)
		if err != nil {
			e := err
			if e.Error() == nfcspec.ErrEnded {
				e = nil
			}
			ui.Post(func() { s.finish(hostError(e)) })
			return
		}
		t, err := decodeTag(b)
		if err != nil {
			continue
		}
		ui.Post(func() {
			if s.active && s.found != nil {
				s.found(t)
			}
		})
	}
}

// finish ends the session (main thread): OnEnd, once.
func (s *Session) finish(err error) {
	s.active = false
	if current == s {
		current = nil
	}
	if s.ended {
		return
	}
	s.ended = true
	if s.o.OnEnd != nil {
		s.o.OnEnd(err)
	}
}

// Active reports whether the session still looks for tags (or holds
// one). Main thread.
func (s *Session) Active() bool { return s.active }

// SetAlert changes what iOS's sheet says ("Writing…", "Hold it
// still"); elsewhere nothing. Main thread.
func (s *Session) SetAlert(msg string) {
	if s.active {
		ui.Call("nfc", "alert", s.args(msg, ""), nil)
	}
}

// Next lets the tag held go and looks for another (iOS restarts polling,
// and finds a tag that's still near again). Main thread.
func (s *Session) Next() {
	if s.active {
		ui.Call("nfc", "restart", s.args("", ""), nil)
	}
}

// Stop ends the session: iOS's sheet shows its tick (with SetAlert's
// message) as it closes. OnEnd gets nil. Main thread.
func (s *Session) Stop() { s.stop("") }

// Fail ends the session with a failure: iOS's sheet shows msg as it
// closes. OnEnd gets nil. Main thread.
func (s *Session) Fail(msg string) {
	if msg == "" {
		msg = "Failed"
	}
	s.stop(msg)
}

func (s *Session) stop(failure string) {
	if !s.active {
		return
	}
	ui.Call("nfc", "stop", s.args("", failure), nil)
	s.finish(nil)
}

// Tech is a technology a tag has: an NDEF message, or the commands it
// takes.
type Tech string

// The technologies both platforms report (Transceive takes them).
const (
	NDEF Tech = "ndef"
	// ISO7816: an ISO 7816 card (ISO 14443-4): APDUs (Command).
	ISO7816 Tech = "iso7816"
	// MIFARE: an ISO 14443-3A tag (NTAG, MIFARE Ultralight, DESFire...):
	// its native commands, as frames without their CRC.
	MIFARE Tech = "mifare"
	// FeliCa: FeliCa commands, without the length byte (the platform
	// adds it, and takes the answer's off).
	FeliCa Tech = "felica"
	// ISO15693: requests as flags, a command code, then its parameters
	// (an addressed one's UID among them); answers as their flags, then
	// the data.
	ISO15693 Tech = "iso15693"
)

// Android's own technologies (android.nfc.tech): Transceive sends their
// frames as Android's classes take them.
const (
	NfcA             Tech = "nfcA"
	NfcB             Tech = "nfcB"
	NfcF             Tech = "nfcF" // frames with their length byte
	NfcV             Tech = "nfcV"
	IsoDep           Tech = "isoDep"
	MifareClassic    Tech = "mifareClassic" // ClassicAuth, ClassicRead...
	MifareUltralight Tech = "mifareUltralight"
	NDEFFormatable   Tech = "ndefFormatable" // Write formats it
	NfcBarcode       Tech = "nfcBarcode"     // Tag.Barcode
)

// Format is whether a tag holds an NDEF message.
type Format uint8

const (
	// NotNDEF: the tag has no NDEF, and the platform can't make it.
	NotNDEF Format = nfcspec.FormatNone
	// Formatable: no NDEF yet; Write formats the tag with its message
	// (Android's NdefFormatable).
	Formatable Format = nfcspec.FormatFormatable
	ReadWrite  Format = nfcspec.FormatReadWrite
	ReadOnly   Format = nfcspec.FormatReadOnly
)

func (f Format) String() string {
	return [...]string{"not NDEF", "formatable", "read-write", "read-only", "?"}[min(int(f), 4)]
}

// NDEFInfo is a tag's NDEF status.
type NDEFInfo struct {
	Format Format
	// Capacity is the largest message the tag holds, in bytes (the
	// encoded message's: Message.Encode); -1 if not known (the web, a
	// session with SkipNDEF).
	Capacity int
	// CanMakeReadOnly: MakeReadOnly can lock it (Android says; iOS tries).
	CanMakeReadOnly bool
	// Type is Android's NFC Forum type ("org.nfcforum.ndef.type2",
	// "com.nxp.ndef.mifareclassic"...).
	Type string
}

// Writable reports whether Write can write the tag.
func (n NDEFInfo) Writable() bool { return n.Format == ReadWrite || n.Format == Formatable }

// The technologies' details, where the platform gives them.
type (
	// NfcAInfo is an ISO 14443-3A tag's (Android).
	NfcAInfo struct {
		ATQA []byte
		SAK  int
	}
	// NfcBInfo is an ISO 14443-3B tag's (Android).
	NfcBInfo struct {
		ApplicationData []byte
		ProtocolInfo    []byte
	}
	// ISO7816Info is an ISO 7816 card's.
	ISO7816Info struct {
		// AID is the application iOS selected as it found the card: one
		// of asuka.json's nfc.iso7816, in hex.
		AID string
		// Historical are its historical bytes (from the ATS: ISO 14443-A);
		// HiLayer its higher layer response (ISO 14443-B).
		Historical []byte
		HiLayer    []byte
		// ApplicationData and Proprietary are iOS's (ISO 14443-B's
		// application data, its coding proprietary).
		ApplicationData []byte
		Proprietary     bool
	}
	// FeliCaInfo is a FeliCa card's.
	FeliCaInfo struct {
		SystemCode []byte
		IDm        []byte // its manufacture ID
		PMm        []byte // its manufacture parameter
	}
	// ISO15693Info is an ISO 15693 tag's.
	ISO15693Info struct {
		Manufacturer int // its IC manufacturer code (ISO 7816-6)
		Serial       []byte
		DSFID        int
		AFI          int
	}
	// MIFAREInfo is a MIFARE tag's.
	MIFAREInfo struct {
		// Family is "ultralight", "plus", "desfire", "classic" or
		// "unknown".
		Family string
		// Type is Android's: MifareClassic's "classic", "plus", "pro";
		// MifareUltralight's "ultralight", "ultralightC".
		Type string
		// A MIFARE Classic's size in bytes, its sectors and blocks
		// (Android).
		Size, Sectors, Blocks int
	}
)

// Tag is a tag found: what it said as it was found, and the methods
// that talk to it while the app holds it (until its session ends, Next
// is called, or another tag takes its place: ErrTagLost after).
type Tag struct {
	// ID is its identifier (UID), as the platform gives it (Android's
	// NFC-V, least significant byte first); none in iOS's NDEF sessions.
	ID []byte
	// Techs are the technologies it has (Has).
	Techs []Tech
	// NDEF is its NDEF status, Message its message as it was found (nil
	// if none was read: not NDEF, SkipNDEF; empty if the tag's is).
	NDEF    NDEFInfo
	Message Message
	// MaxTransceive is the longest frame Transceive takes (Android; 0:
	// not known).
	MaxTransceive int
	NfcA          *NfcAInfo
	NfcB          *NfcBInfo
	ISO7816       *ISO7816Info
	FeliCa        *FeliCaInfo
	ISO15693      *ISO15693Info
	MIFARE        *MIFAREInfo
	// Barcode is an NFC barcode's data (Android).
	Barcode []byte

	handle uint32
}

func decodeTag(b []byte) (*Tag, error) {
	var w nfcspec.Tag
	if err := json.Unmarshal(b, &w); err != nil {
		return nil, err
	}
	t := &Tag{handle: w.Handle, ID: w.ID, MaxTransceive: w.MaxTransceive, Barcode: w.Barcode,
		NDEF: NDEFInfo{Format: Format(w.NDEF.Format), Capacity: w.NDEF.Capacity, CanMakeReadOnly: w.NDEF.CanLock, Type: w.NDEF.Type}}
	for _, s := range w.Techs {
		t.Techs = append(t.Techs, Tech(s))
	}
	if w.Message != nil {
		if m, err := Decode(w.Message); err == nil {
			t.Message = m
			if t.Message == nil {
				t.Message = Message{}
			}
		}
	}
	if w.NfcA != nil {
		t.NfcA = &NfcAInfo{ATQA: w.NfcA.ATQA, SAK: w.NfcA.SAK}
	}
	if w.NfcB != nil {
		t.NfcB = &NfcBInfo{ApplicationData: w.NfcB.ApplicationData, ProtocolInfo: w.NfcB.ProtocolInfo}
	}
	if w.ISO7816 != nil {
		i := ISO7816Info(*w.ISO7816)
		t.ISO7816 = &i
	}
	if w.FeliCa != nil {
		f := FeliCaInfo(*w.FeliCa)
		t.FeliCa = &f
	}
	if w.ISO15693 != nil {
		v := ISO15693Info(*w.ISO15693)
		t.ISO15693 = &v
	}
	if w.MIFARE != nil {
		m := MIFAREInfo(*w.MIFARE)
		t.MIFARE = &m
	}
	return t, nil
}

// Has reports whether the tag has tech.
func (t *Tag) Has(tech Tech) bool {
	for _, x := range t.Techs {
		if x == tech {
			return true
		}
	}
	return false
}

func (t *Tag) call(method string, op nfcspec.Op) ([]byte, error) {
	op.Handle = t.handle
	args, _ := json.Marshal(op)
	b, err := ui.CallWait("nfc", method, args)
	return b, hostError(err)
}

// Read reads the tag's NDEF message again (empty if it has none). Not
// on the main thread.
func (t *Tag) Read() (Message, error) {
	b, err := t.call("read", nfcspec.Op{})
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return Message{}, nil
	}
	m, err := Decode(b)
	if m == nil && err == nil {
		m = Message{}
	}
	return m, err
}

// Write writes m to the tag, in place of its message (formatting a
// Formatable tag). Not on the main thread.
func (t *Tag) Write(m Message) error {
	if err := m.Check(); err != nil {
		return err
	}
	b := m.Encode()
	if t.NDEF.Capacity > 0 && len(b) > t.NDEF.Capacity {
		return fmt.Errorf("%w: %d bytes, the tag holds %d", ErrTooLarge, len(b), t.NDEF.Capacity)
	}
	_, err := t.call("write", nfcspec.Op{Message: b})
	return err
}

// MakeReadOnly locks the tag: its message can't be written again, by
// anyone. It can't be undone. Not on the main thread.
func (t *Tag) MakeReadOnly() error {
	_, err := t.call("lock", nfcspec.Op{})
	return err
}

// Status asks the tag for its NDEF status again (after a write, a
// lock). Not on the main thread.
func (t *Tag) Status() (NDEFInfo, error) {
	b, err := t.call("status", nfcspec.Op{})
	if err != nil {
		return NDEFInfo{}, err
	}
	var n nfcspec.NDEF
	if err := json.Unmarshal(b, &n); err != nil {
		return NDEFInfo{}, err
	}
	return NDEFInfo{Format: Format(n.Format), Capacity: n.Capacity, CanMakeReadOnly: n.CanLock, Type: n.Type}, nil
}

// Transceive sends the tag a raw command of tech's and answers with its
// response: ISO7816 an APDU (Command builds one), MIFARE and NfcA a
// frame without its CRC, FeliCa a command without its length byte,
// ISO15693 a request; Android's techs their frames as Android's
// classes take them. iOS needs a session with Poll; the web has none
// (ErrNotSupported). Not on the main thread.
func (t *Tag) Transceive(tech Tech, data []byte) ([]byte, error) {
	return t.call("transceive", nfcspec.Op{Tech: string(tech), Data: data})
}

// APDU is an ISO 7816-4 command.
type APDU struct {
	CLA, INS, P1, P2 byte
	Data             []byte
	// Le is the response's expected length: 0 none, 256 (or 65536,
	// extended) as many as there are.
	Le int
}

// Encode is the command's bytes: short, or extended where its data or
// Le need it.
func (a APDU) Encode() []byte {
	b := []byte{a.CLA, a.INS, a.P1, a.P2}
	extended := len(a.Data) > 255 || a.Le > 256
	if len(a.Data) > 0 {
		if extended {
			b = append(b, 0, byte(len(a.Data)>>8), byte(len(a.Data)))
		} else {
			b = append(b, byte(len(a.Data)))
		}
		b = append(b, a.Data...)
	}
	if a.Le > 0 {
		switch {
		case extended && len(a.Data) > 0:
			b = append(b, byte(a.Le>>8), byte(a.Le)) // 65536: 00 00
		case extended:
			b = append(b, 0, byte(a.Le>>8), byte(a.Le))
		default:
			b = append(b, byte(a.Le)) // 256: 00
		}
	}
	return b
}

// Response is an APDU's answer: its data, and its status words (0x9000:
// done).
type Response struct {
	Data     []byte
	SW1, SW2 byte
}

// OK reports whether the command succeeded (90 00).
func (r Response) OK() bool { return r.SW1 == 0x90 && r.SW2 == 0x00 }

// Status is SW1 and SW2 together: 0x9000, 0x6A82...
func (r Response) Status() uint16 { return uint16(r.SW1)<<8 | uint16(r.SW2) }

// Command sends an ISO 7816 card an APDU. Not on the main thread.
func (t *Tag) Command(a APDU) (Response, error) {
	b, err := t.Transceive(ISO7816, a.Encode())
	if err != nil {
		return Response{}, err
	}
	if len(b) < 2 {
		return Response{}, fmt.Errorf("nfc: an answer of %d bytes, without its status", len(b))
	}
	return Response{Data: b[:len(b)-2], SW1: b[len(b)-2], SW2: b[len(b)-1]}, nil
}

// SelectAID is the SELECT command of an application by its AID (ISO
// 7816-4: by DF name, the first or only one).
func SelectAID(aid []byte) APDU {
	return APDU{CLA: 0x00, INS: 0xA4, P1: 0x04, P2: 0x00, Data: aid, Le: 256}
}

// ClassicAuth authenticates a MIFARE Classic sector with a key (key A,
// or key B), for ClassicRead and ClassicWrite of its blocks (Android:
// iOS reads no MIFARE Classic). Not on the main thread.
func (t *Tag) ClassicAuth(sector int, key []byte, keyB bool) error {
	_, err := t.call("classic", nfcspec.Op{Op: "auth", Sector: sector, Key: key, KeyB: keyB})
	return err
}

// ClassicRead reads a MIFARE Classic block (16 bytes). Not on the main
// thread.
func (t *Tag) ClassicRead(block int) ([]byte, error) {
	return t.call("classic", nfcspec.Op{Op: "read", Block: block})
}

// ClassicWrite writes a MIFARE Classic block (16 bytes). Not on the
// main thread.
func (t *Tag) ClassicWrite(block int, data []byte) error {
	_, err := t.call("classic", nfcspec.Op{Op: "write", Block: block, Data: data})
	return err
}

// ValueOp is a MIFARE Classic value block's operation.
type ValueOp string

const (
	Increment ValueOp = "increment" // the block's value plus n, into the transfer buffer
	Decrement ValueOp = "decrement" // minus n
	Restore   ValueOp = "restore"   // the block's value, as it is
	Transfer  ValueOp = "transfer"  // the transfer buffer into the block
)

// ClassicValue runs a value block operation on a MIFARE Classic block (n
// is Increment's and Decrement's). Not on the main thread.
func (t *Tag) ClassicValue(op ValueOp, block, n int) error {
	_, err := t.call("classic", nfcspec.Op{Op: string(op), Block: block, Value: n})
	return err
}

// Opened tags: the stream of "nextOpened".

var (
	openMu       sync.Mutex
	openWatchers = map[*func(*Tag)]bool{}
	openRunning  bool
)

// OnOpen calls f, on the main thread, with each tag the system opens the
// app with, at launch or since (Android's NDEF_DISCOVERED and
// TECH_DISCOVERED, of asuka.json's nfc filters), until stop; a tag that
// came before any f waits for the first. A tag whose URI opened the app
// goes to ui.OnOpenURL instead, and iOS opens apps only with those (its
// background tag reading of the app's universal links). The tag can be
// talked to while it's still near. Called in a component, it stops with
// the component's owner. Main thread, and stop too.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func OnOpen(f func(*Tag)) (stop func()) {
	fp := &f
	openMu.Lock()
	openWatchers[fp] = true
	start := !openRunning
	openRunning = true
	openMu.Unlock()
	if start {
		go followOpened()
	}
	var once sync.Once
	stop = func() {
		once.Do(func() {
			openMu.Lock()
			delete(openWatchers, fp)
			last := len(openWatchers) == 0
			openMu.Unlock()
			if last {
				ui.Call("nfc", "stopOpened", nil, nil)
			}
		})
	}
	signal.OnCleanup(stop)
	return stop
}

func followOpened() {
	for {
		b, err := ui.CallWait("nfc", "nextOpened", nil)
		openMu.Lock()
		fs := make([]func(*Tag), 0, len(openWatchers))
		for f := range openWatchers {
			fs = append(fs, *f)
		}
		// Stopped as another began watching: wait on for that one.
		if len(fs) == 0 || err != nil && err.Error() != nfcspec.ErrStopped {
			openRunning = false
			openMu.Unlock()
			return
		}
		openMu.Unlock()
		if err != nil {
			continue
		}
		t, err := decodeTag(b)
		if err != nil {
			continue
		}
		ui.Post(func() {
			for _, f := range fs {
				f(t)
			}
		})
	}
}
