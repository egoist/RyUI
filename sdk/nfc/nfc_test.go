package nfc_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/sdk/nfc"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func mount(t *testing.T, root func() ui.Node) (*fake.Host, *fake.NFC, *ui.Root) {
	t.Helper()
	h := fake.New()
	n := h.NFC()
	r := ui.Mount(h, root)
	t.Cleanup(func() {
		r.Dispose()
		h.RunPosted() // the stops' answers: the followers end
	})
	return h, n, r
}

// until runs what's posted (the package's goroutines' calls and their
// answers) until cond holds.
func until(t *testing.T, h *fake.Host, r *ui.Root, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		h.WaitPost(10 * time.Millisecond)
		h.RunPosted()
		r.Flush()
	}
}

// off runs a call that waits for the host off the main thread, pumping
// the host until it's answered.
func off[T any](t *testing.T, h *fake.Host, r *ui.Root, f func() T) T {
	t.Helper()
	ch := make(chan T, 1)
	go func() { ch <- f() }()
	var v T
	done := false
	until(t, h, r, "an answer", func() bool {
		select {
		case v = <-ch:
			done = true
		default:
		}
		return done
	})
	return v
}

// A session finds a tag with its message; the app writes the tag and
// stops; OnEnd gets nil.
func TestReadWrite(t *testing.T) {
	var found []*nfc.Tag
	var ended []error
	var s *nfc.Session
	h, n, r := mount(t, func() ui.Node { return ui.StaticText("x") })
	s = nfc.Start(nfc.Options{Alert: "Hold it near", OnEnd: func(err error) { ended = append(ended, err) }},
		func(tag *nfc.Tag) { found = append(found, tag) })
	until(t, h, r, "a session", func() bool { return n.Session != nil })
	if n.Session.Alert != "Hold it near" || n.Session.Poll != 0 {
		t.Fatalf("session %+v", n.Session)
	}
	tag := &fake.NFCTag{ID: []byte{4, 1, 2, 3}, NDEF: nfc.Message{nfc.URI("https://example.com/a")}.Encode()}
	if !n.Tap(tag) {
		t.Fatal("no session looked")
	}
	until(t, h, r, "the tag", func() bool { return len(found) == 1 })
	got := found[0]
	if u, ok := got.Message.URI(); !ok || u != "https://example.com/a" {
		t.Fatalf("its URI %q", u)
	}
	if !bytes.Equal(got.ID, []byte{4, 1, 2, 3}) || !got.Has(nfc.NDEF) || !got.Has(nfc.MIFARE) || got.Has(nfc.ISO7816) {
		t.Fatalf("tag %+v", got)
	}
	if got.NDEF.Format != nfc.ReadWrite || got.NDEF.Capacity != 137 || !got.NDEF.CanMakeReadOnly || !got.NDEF.Writable() {
		t.Fatalf("ndef %+v", got.NDEF)
	}
	s.SetAlert("Writing…")
	err := off(t, h, r, func() error { return got.Write(nfc.Message{nfc.URI("https://example.com/b"), nfc.Text("b", "en")}) })
	if err != nil {
		t.Fatal(err)
	}
	if n.Session.Alert != "Writing…" {
		t.Errorf("alert %q", n.Session.Alert)
	}
	m, err := nfc.Decode(tag.NDEF)
	if u, _ := m.URI(); err != nil || u != "https://example.com/b" || tag.Writes != 1 {
		t.Fatalf("written %v %q %v", m, u, err)
	}
	read := off(t, h, r, func() nfc.Message { m, _ := got.Read(); return m })
	if s, _, _ := read.Text(); s != "b" {
		t.Fatalf("read back %v", read)
	}
	if err := off(t, h, r, got.MakeReadOnly); err != nil {
		t.Fatal(err)
	}
	st := off(t, h, r, func() nfc.NDEFInfo { st, _ := got.Status(); return st })
	if st.Format != nfc.ReadOnly || st.Writable() {
		t.Fatalf("status after the lock %+v", st)
	}
	if err := off(t, h, r, func() error { return got.Write(nfc.Message{nfc.Text("c", "")}) }); !errors.Is(err, nfc.ErrReadOnly) {
		t.Fatalf("writing a locked tag: %v", err)
	}
	s.Stop()
	until(t, h, r, "the end", func() bool { return n.Session == nil })
	if len(ended) != 1 || ended[0] != nil || s.Active() {
		t.Fatalf("ended %v, active %v", ended, s.Active())
	}
	// The tag's gone with its session.
	if err := off(t, h, r, func() error { _, err := got.Read(); return err }); !errors.Is(err, nfc.ErrTagLost) {
		t.Fatalf("after the session: %v", err)
	}
}

// One tag at a time: the next comes once the app asks for it, and the
// one held before is let go.
func TestNext(t *testing.T) {
	var found []*nfc.Tag
	var s *nfc.Session
	h, n, r := mount(t, func() ui.Node { return ui.StaticText("x") })
	s = nfc.Start(nfc.Options{Poll: nfc.PollA | nfc.PollV, SkipNDEF: true, NoSound: true, PresenceDelay: 250 * time.Millisecond},
		func(tag *nfc.Tag) { found = append(found, tag) })
	until(t, h, r, "a session", func() bool { return n.Session != nil })
	if ss := n.Session; ss.Poll != uint32(nfc.PollA|nfc.PollV) || !ss.SkipNDEF || !ss.NoSound || ss.PresenceDelay != 250 {
		t.Fatalf("session %+v", ss)
	}
	a := &fake.NFCTag{ID: []byte{1}, NDEF: nfc.Message{nfc.Text("a", "")}.Encode()}
	b := &fake.NFCTag{ID: []byte{2}}
	n.Tap(a)
	until(t, h, r, "a", func() bool { return len(found) == 1 })
	if found[0].Message != nil || found[0].NDEF.Capacity != -1 {
		t.Errorf("skipped NDEF: %+v", found[0])
	}
	n.Tap(b)
	for range 5 {
		h.WaitPost(5 * time.Millisecond)
		h.RunPosted()
	}
	if len(found) != 1 {
		t.Fatal("b found before Next")
	}
	s.Next()
	until(t, h, r, "b", func() bool { return len(found) == 2 })
	if !bytes.Equal(found[1].ID, []byte{2}) || n.Session.Restarts != 1 {
		t.Fatalf("second %+v", found[1])
	}
	// a is let go; b's read fails: no NDEF.
	if err := off(t, h, r, func() error { _, err := found[0].Read(); return err }); !errors.Is(err, nfc.ErrTagLost) {
		t.Fatalf("a after Next: %v", err)
	}
	if err := off(t, h, r, func() error { _, err := found[1].Read(); return err }); !errors.Is(err, nfc.ErrNotSupported) {
		t.Fatalf("b's read: %v", err)
	}
	// Taken away, b is lost.
	n.Remove()
	if err := off(t, h, r, func() error { _, err := found[1].Status(); return err }); !errors.Is(err, nfc.ErrTagLost) {
		t.Fatalf("b removed: %v", err)
	}
	s.Fail("Not this one")
	until(t, h, r, "the end", func() bool { return n.Session == nil })
}

// Raw commands go to the tag's technology; Command reads an APDU's
// status words.
func TestTransceive(t *testing.T) {
	var tag *nfc.Tag
	h, n, r := mount(t, func() ui.Node { return ui.StaticText("x") })
	nfc.Start(nfc.Options{Poll: nfc.PollA}, func(t *nfc.Tag) { tag = t })
	until(t, h, r, "a session", func() bool { return n.Session != nil })
	var sent [][]byte
	n.Tap(&fake.NFCTag{Techs: []string{"iso7816", "isoDep", "nfcA", "mifare"}, Transceive: func(tech string, data []byte) ([]byte, error) {
		sent = append(sent, append([]byte(tech+":"), data...))
		if tech == "iso7816" {
			return []byte{0xca, 0xfe, 0x90, 0x00}, nil
		}
		return []byte{0x0a}, nil
	}})
	until(t, h, r, "the tag", func() bool { return tag != nil })
	resp := off(t, h, r, func() nfc.Response { r, _ := tag.Command(nfc.SelectAID([]byte{0xa0, 0, 0, 2, 0x47})); return r })
	if !resp.OK() || !bytes.Equal(resp.Data, []byte{0xca, 0xfe}) {
		t.Fatalf("response %+v", resp)
	}
	b := off(t, h, r, func() []byte { b, _ := tag.Transceive(nfc.MIFARE, []byte{0x30, 4}); return b })
	if !bytes.Equal(b, []byte{0x0a}) || string(sent[1][:7]) != "mifare:" {
		t.Fatalf("mifare %x, sent %q", b, sent)
	}
	if err := off(t, h, r, func() error { _, err := tag.Transceive(nfc.FeliCa, []byte{6}); return err }); !errors.Is(err, nfc.ErrNotSupported) {
		t.Fatalf("a technology it lacks: %v", err)
	}
	if err := off(t, h, r, func() error { return tag.ClassicAuth(1, make([]byte, 6), false) }); !errors.Is(err, nfc.ErrNotSupported) {
		t.Fatalf("classic: %v", err)
	}
}

// The user closing the sheet ends the session with ErrCanceled; a
// session started while another runs ends that one; owners end theirs.
func TestEnds(t *testing.T) {
	var ended []error
	h, n, r := mount(t, func() ui.Node { return ui.StaticText("x") })
	nfc.Start(nfc.Options{OnEnd: func(err error) { ended = append(ended, err) }}, nil)
	until(t, h, r, "a session", func() bool { return n.Session != nil })
	n.Cancel()
	until(t, h, r, "canceled", func() bool { return len(ended) == 1 })
	if !errors.Is(ended[0], nfc.ErrCanceled) {
		t.Fatalf("canceled: %v", ended[0])
	}
	first := nfc.Start(nfc.Options{OnEnd: func(err error) { ended = append(ended, err) }}, nil)
	until(t, h, r, "a session", func() bool { return n.Session != nil })
	nfc.Start(nfc.Options{OnEnd: func(err error) { ended = append(ended, err) }}, nil)
	until(t, h, r, "the second", func() bool { return n.Sessions == 3 && n.Session != nil })
	if first.Active() || len(ended) != 2 || ended[1] != nil {
		t.Fatalf("the first: active %v, ended %v", first.Active(), ended)
	}
	n.Timeout()
	until(t, h, r, "timed out", func() bool { return len(ended) == 3 })
	if !errors.Is(ended[2], nfc.ErrTimeout) {
		t.Fatalf("timeout: %v", ended[2])
	}

	// Started in a component: it ends with its owner.
	show := signal.New(true)
	h2, n2, r2 := mount(t, func() ui.Node {
		return ui.Show(show, func() ui.Node {
			nfc.Start(nfc.Options{}, nil)
			return ui.StaticText("scanning")
		})
	})
	until(t, h2, r2, "a session", func() bool { return n2.Session != nil })
	show.Set(false)
	r2.Flush()
	until(t, h2, r2, "the owner's end", func() bool { return n2.Session == nil })

	// Off and missing NFC fail to begin.
	h3, n3, r3 := mount(t, func() ui.Node { return ui.StaticText("x") })
	n3.Enabled = false
	var why error
	nfc.Start(nfc.Options{OnEnd: func(err error) { why = err }}, nil)
	until(t, h3, r3, "disabled", func() bool { return why != nil })
	if !errors.Is(why, nfc.ErrDisabled) {
		t.Fatalf("off: %v", why)
	}
	n3.Supported = false
	why = nil
	nfc.Start(nfc.Options{OnEnd: func(err error) { why = err }}, nil)
	until(t, h3, r3, "not available", func() bool { return why != nil })
	if !errors.Is(why, nfc.ErrNotAvailable) {
		t.Fatalf("none: %v", why)
	}
}

// Check and Watch follow the state; a host without the module answers
// unsupported.
func TestState(t *testing.T) {
	var seen []nfc.State
	h, n, r := mount(t, func() ui.Node {
		s := nfc.Use()
		signal.Effect(func() { seen = append(seen, s.Get()) })
		return ui.StaticText("x")
	})
	until(t, h, r, "the state", func() bool { return len(seen) == 2 })
	if seen[1] != (nfc.State{Supported: true, Enabled: true}) {
		t.Fatalf("state %+v", seen)
	}
	n.SetEnabled(false)
	until(t, h, r, "off", func() bool { return len(seen) == 3 })
	if seen[2] != (nfc.State{Supported: true}) {
		t.Fatalf("off: %+v", seen[2])
	}
	if s := off(t, h, r, func() nfc.State { s, _ := nfc.Check(); return s }); s.Enabled {
		t.Fatalf("check %+v", s)
	}
	if err := off(t, h, r, nfc.OpenSettings); err != nil || n.Settings != 1 {
		t.Fatalf("settings %v %d", err, n.Settings)
	}

	h2 := fake.New() // no nfc module: an app built without it
	r2 := ui.Mount(h2, func() ui.Node { return ui.StaticText("x") })
	defer func() { r2.Dispose(); h2.RunPosted() }()
	type checked struct {
		s   nfc.State
		err error
	}
	if c := off(t, h2, r2, func() checked { s, err := nfc.Check(); return checked{s, err} }); c.s.Supported || c.err != nil {
		t.Fatalf("without the module: %+v", c)
	}
}

// Tags the app is opened with wait for the first handler, and come
// after.
func TestOnOpen(t *testing.T) {
	h, n, r := mount(t, func() ui.Node { return ui.StaticText("x") })
	early := &fake.NFCTag{ID: []byte{7}, Techs: []string{"nfcF", "felica"}}
	n.Open(early)
	var got []*nfc.Tag
	var stop func()
	ui.Post(func() { stop = nfc.OnOpen(func(t *nfc.Tag) { got = append(got, t) }) })
	until(t, h, r, "the early tag", func() bool { return len(got) == 1 })
	if !bytes.Equal(got[0].ID, []byte{7}) || !got[0].Has(nfc.FeliCa) {
		t.Fatalf("opened %+v", got[0])
	}
	n.Open(&fake.NFCTag{ID: []byte{8}, NDEF: nfc.Message{nfc.MIME("application/x-thing", []byte("1"))}.Encode()})
	until(t, h, r, "the second", func() bool { return len(got) == 2 })
	if mt, _ := got[1].Message[0].MediaType(); mt != "application/x-thing" {
		t.Fatalf("its message %+v", got[1].Message)
	}
	// It's held: the app can write it while it's near.
	if err := off(t, h, r, func() error { return got[1].Write(nfc.Message{nfc.Text("ok", "")}) }); err != nil {
		t.Fatal(err)
	}
	ui.Post(stop)
	h.RunPosted()
	n.Open(&fake.NFCTag{ID: []byte{9}})
	for range 5 {
		h.WaitPost(5 * time.Millisecond)
		h.RunPosted()
	}
	if len(got) != 2 {
		t.Fatal("a tag after stop")
	}
}
