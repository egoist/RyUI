package fake

import (
	"encoding/json"
	"errors"
	"slices"

	spec "github.com/egoist/asuka/internal/nfcspec"
)

// NFC is the device's NFC, as the hosts' "nfc" module has it (package
// nfc), with simulated tags, for tests: Host.NFC installs it, on, with no
// session running.
//
//	n := h.NFC()
//	tag := &fake.NFCTag{NDEF: nfc.Message{nfc.URI("https://example.com")}.Encode()}
//	n.Tap(tag) // the session running finds it
type NFC struct {
	// Supported and Enabled are what the device says (SetEnabled changes
	// Enabled for the watchers).
	Supported, Enabled bool
	// Session is the session running, nil when none.
	Session *NFCSession
	// Sessions counts the sessions started; Settings, the times the
	// settings were opened.
	Sessions, Settings int

	h          *Host
	last       *NFCSession // the one that ended last: a "next" after gets why
	handle     uint32
	held       *NFCTag // the session's tag found, while it's held
	heldHandle uint32
	pending    *NFCTag // tapped while the session holds another
	// The state's watcher ("nextState"), and opened tags ("nextOpened").
	stateWait  func([]byte, error)
	stateLast  []byte
	opened     []*NFCTag
	openWait   func([]byte, error)
	openHandle map[uint32]*NFCTag
}

// NFCSession is a session as the app started it.
type NFCSession struct {
	ID uint32
	// Alert is what iOS's sheet says (Options.Alert, SetAlert).
	Alert             string
	Poll              uint32
	SkipNDEF, NoSound bool
	// PresenceDelay is in milliseconds.
	PresenceDelay int
	// Restarts counts the app's Next.
	Restarts int
	// Failure is what it was stopped with (Session.Fail), once it ended.
	Failure string

	waiting func([]byte, error)
	queue   []*NFCTag
	looking bool   // no tag answered since the last restart
	end     string // why it ended: an nfcspec word, "" while it runs
}

// Ended reports whether the session ended.
func (s *NFCSession) Ended() bool { return s.end != "" }

// NFCTag is a simulated tag.
type NFCTag struct {
	ID []byte
	// Techs are its technologies (default: an NTAG's: ndef, mifare,
	// nfcA, mifareUltralight).
	Techs []string
	// NDEF is its NDEF message's bytes (nfc.Message's Encode), nil if it
	// isn't formatted for NDEF; Formatable, then, says Write formats it.
	NDEF       []byte
	Formatable bool
	// Capacity is the largest message it holds (0: 137 bytes, an
	// NTAG213's).
	Capacity int
	// ReadOnly: it can't be written; MakeReadOnly sets it (unless
	// NoLock: a tag that can't be locked).
	ReadOnly, NoLock bool
	// Transceive answers raw commands (nil: none, "not supported").
	Transceive func(tech string, data []byte) ([]byte, error)
	// Classic answers MIFARE Classic operations (nil: none).
	Classic func(NFCClassicOp) ([]byte, error)
	// Writes counts the messages written, Locks the times it was locked.
	Writes, Locks int

	gone bool
}

// NFCClassicOp is a MIFARE Classic operation the app asked for: Op is
// "auth", "read", "write", "increment", "decrement", "transfer" or
// "restore".
type NFCClassicOp struct {
	Op            string
	Sector, Block int
	Key           []byte
	KeyB          bool
	Data          []byte
	Value         int
}

// NFC gives the host NFC (the "nfc" module), as a phone's.
func (h *Host) NFC() *NFC {
	n := &NFC{h: h, Supported: true, Enabled: true, openHandle: map[uint32]*NFCTag{}}
	if h.AsyncModules == nil {
		h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){}
	}
	h.AsyncModules["nfc"] = n.call
	return n
}

func (n *NFC) state() []byte {
	b, _ := json.Marshal(spec.State{Supported: n.Supported, Enabled: n.Enabled})
	return b
}

func (n *NFC) call(method string, args []byte, reply func([]byte, error)) {
	fail := func(word string) { reply(nil, errors.New(word)) }
	switch method {
	case "state":
		reply(n.state(), nil)
	case "nextState":
		if s := n.state(); string(s) != string(args) {
			reply(s, nil)
			return
		}
		n.stateWait, n.stateLast = reply, args
	case "stopState":
		if w := n.stateWait; w != nil {
			n.stateWait = nil
			w(nil, errors.New(spec.ErrStopped))
		}
		reply(nil, nil)
	case "settings":
		n.Settings++
		reply(nil, nil)
	case "start":
		var st spec.Start
		if err := json.Unmarshal(args, &st); err != nil {
			reply(nil, err)
			return
		}
		if !n.Supported {
			fail(spec.ErrNotAvailable)
			return
		}
		if !n.Enabled {
			fail(spec.ErrDisabled)
			return
		}
		n.endSession(spec.ErrEnded)
		n.Session = &NFCSession{ID: st.ID, Alert: st.Alert, Poll: st.Poll, SkipNDEF: st.SkipNDEF, NoSound: st.NoSound,
			PresenceDelay: st.PresenceDelay, looking: true}
		n.Sessions++
		reply(nil, nil)
	case "next", "restart", "alert", "stop":
		var a spec.Session
		json.Unmarshal(args, &a)
		s := n.Session
		if s == nil || s.ID != a.ID {
			if method == "next" {
				if l := n.last; l != nil && l.ID == a.ID {
					fail(l.end)
				} else {
					fail(spec.ErrEnded)
				}
			} else {
				reply(nil, nil)
			}
			return
		}
		switch method {
		case "next":
			if len(s.queue) > 0 {
				t := s.queue[0]
				s.queue = s.queue[1:]
				reply(n.found(t), nil)
				return
			}
			s.waiting = reply
			return
		case "restart":
			s.Restarts++
			n.held, n.heldHandle = nil, 0
			s.looking = true
			if p := n.pending; p != nil {
				n.pending = nil
				n.deliver(p)
			}
		case "alert":
			s.Alert = a.Alert
		case "stop":
			s.Failure = a.Error
			n.endSession(spec.ErrEnded)
		}
		reply(nil, nil)
	case "read", "write", "lock", "status", "transceive", "classic":
		var op spec.Op
		if err := json.Unmarshal(args, &op); err != nil {
			reply(nil, err)
			return
		}
		t := n.tag(op.Handle)
		if t == nil {
			fail(spec.ErrTagLost)
			return
		}
		reply(n.op(t, method, op))
	case "nextOpened":
		if len(n.opened) > 0 {
			t := n.opened[0]
			n.opened = n.opened[1:]
			reply(n.openedTag(t), nil)
			return
		}
		n.openWait = reply
	case "stopOpened":
		if w := n.openWait; w != nil {
			n.openWait = nil
			w(nil, errors.New(spec.ErrStopped))
		}
		reply(nil, nil)
	default:
		reply(nil, errors.New("no module method nfc."+method))
	}
}

// endSession ends the session running, its waiting "next" failing with
// word.
func (n *NFC) endSession(word string) {
	s := n.Session
	if s == nil {
		return
	}
	n.Session, n.last = nil, s
	n.held, n.heldHandle, n.pending = nil, 0, nil
	s.end = word
	if w := s.waiting; w != nil {
		s.waiting = nil
		w(nil, errors.New(word))
	}
}

// tag is the tag a handle names, while it's held and near.
func (n *NFC) tag(h uint32) *NFCTag {
	t := n.openHandle[h]
	if h != 0 && h == n.heldHandle {
		t = n.held
	}
	if t == nil || t.gone {
		return nil
	}
	return t
}

func (t *NFCTag) techs() []string {
	if t.Techs != nil {
		return t.Techs
	}
	return []string{"ndef", "mifare", "nfcA", "mifareUltralight"}
}

func (t *NFCTag) capacity() int {
	if t.Capacity > 0 {
		return t.Capacity
	}
	return 137
}

func (t *NFCTag) ndefStatus() spec.NDEF {
	switch {
	case t.NDEF != nil && t.ReadOnly:
		return spec.NDEF{Format: spec.FormatReadOnly, Capacity: t.capacity()}
	case t.NDEF != nil:
		return spec.NDEF{Format: spec.FormatReadWrite, Capacity: t.capacity(), CanLock: !t.NoLock}
	case t.Formatable:
		return spec.NDEF{Format: spec.FormatFormatable, Capacity: -1}
	}
	return spec.NDEF{Format: spec.FormatNone, Capacity: -1}
}

func (n *NFC) encode(t *NFCTag, handle uint32, skipNDEF bool) []byte {
	w := spec.Tag{Handle: handle, ID: t.ID, Techs: t.techs(), NDEF: t.ndefStatus()}
	if skipNDEF {
		w.NDEF = spec.NDEF{Format: spec.FormatNone, Capacity: -1}
	} else if t.NDEF != nil {
		w.Message = t.NDEF
	}
	b, _ := json.Marshal(w)
	return b
}

// found answers the session with t: it holds it now.
func (n *NFC) found(t *NFCTag) []byte {
	n.handle++
	n.held, n.heldHandle = t, n.handle
	return n.encode(t, n.handle, n.Session.SkipNDEF)
}

func (n *NFC) deliver(t *NFCTag) {
	s := n.Session
	s.looking = false
	if w := s.waiting; w != nil {
		s.waiting = nil
		w(n.found(t), nil)
		return
	}
	s.queue = append(s.queue, t)
}

func (n *NFC) openedTag(t *NFCTag) []byte {
	n.handle++
	n.openHandle = map[uint32]*NFCTag{n.handle: t} // another opened tag takes its place
	return n.encode(t, n.handle, false)
}

func (n *NFC) op(t *NFCTag, method string, op spec.Op) ([]byte, error) {
	fail := func(word string) ([]byte, error) { return nil, errors.New(word) }
	switch method {
	case "read":
		if t.NDEF == nil {
			return fail(spec.ErrNotSupported + ": not NDEF")
		}
		return t.NDEF, nil
	case "write":
		switch {
		case t.NDEF == nil && !t.Formatable:
			return fail(spec.ErrNotSupported + ": not NDEF")
		case t.ReadOnly:
			return fail(spec.ErrReadOnly)
		case len(op.Message) > t.capacity():
			return fail(spec.ErrTooLarge)
		}
		t.NDEF = slices.Clone(op.Message)
		t.Writes++
		return nil, nil
	case "lock":
		if t.NDEF == nil || t.NoLock {
			return fail(spec.ErrNotSupported)
		}
		t.ReadOnly = true
		t.Locks++
		return nil, nil
	case "status":
		b, _ := json.Marshal(t.ndefStatus())
		return b, nil
	case "transceive":
		if t.Transceive == nil || !slices.Contains(t.techs(), op.Tech) {
			return fail(spec.ErrNotSupported)
		}
		return t.Transceive(op.Tech, op.Data)
	case "classic":
		if t.Classic == nil {
			return fail(spec.ErrNotSupported)
		}
		return t.Classic(NFCClassicOp{Op: op.Op, Sector: op.Sector, Block: op.Block, Key: op.Key, KeyB: op.KeyB, Data: op.Data, Value: op.Value})
	}
	return fail("no module method nfc." + method)
}

// Tap brings t near, as a user holding a tag to the phone: the session
// running finds it (or, while it holds another, once the app asks for
// the next: Session.Next). It reports whether a session looks for tags,
// and runs what it posts.
func (n *NFC) Tap(t *NFCTag) bool {
	defer n.h.RunPosted()
	s := n.Session
	if s == nil {
		return false
	}
	t.gone = false
	if !s.looking {
		n.pending = t
		return true
	}
	n.deliver(t)
	return true
}

// Remove takes the tag held away: what the app asks of it fails with
// ErrTagLost.
func (n *NFC) Remove() {
	if n.held != nil {
		n.held.gone = true
	}
	n.pending = nil
	n.h.RunPosted()
}

// Cancel closes iOS's sheet, as the user does: the session ends with
// ErrCanceled.
func (n *NFC) Cancel() {
	n.endSession(spec.ErrCanceled)
	n.h.RunPosted()
}

// Timeout ends the session as iOS does after sixty seconds: ErrTimeout.
func (n *NFC) Timeout() {
	n.endSession(spec.ErrTimeout)
	n.h.RunPosted()
}

// Open opens the app with t, as Android's tag dispatch does (an
// NDEF_DISCOVERED or TECH_DISCOVERED of the app's filters, whose message
// has no URI: a URI's goes to Host.OpenURL): nfc.OnOpen gets it, kept
// until it's asked for.
func (n *NFC) Open(t *NFCTag) {
	t.gone = false
	if w := n.openWait; w != nil {
		n.openWait = nil
		w(n.openedTag(t), nil)
	} else {
		n.opened = append(n.opened, t)
	}
	n.h.RunPosted()
}

// SetEnabled turns NFC on or off, as Android's settings do: the
// watchers (nfc.Watch) get the new state.
func (n *NFC) SetEnabled(on bool) {
	n.Enabled = on
	if w := n.stateWait; w != nil && string(n.state()) != string(n.stateLast) {
		n.stateWait = nil
		w(n.state(), nil)
	}
	n.h.RunPosted()
}
