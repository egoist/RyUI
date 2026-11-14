package main

import (
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/sdk/nfc"
	"github.com/egoist/asuka/uitest"
)

func mount(t *testing.T) (*uitest.App, *fake.NFC) {
	var n *fake.NFC
	a := uitest.Mount(t, Tags, uitest.Setup(func(h *fake.Host) { n = h.NFC() }))
	a.WaitFor("NFC is on.")
	return a, n
}

func TestRead(t *testing.T) {
	a, n := mount(t)
	a.Tap("Read a Tag")
	a.Until("a session", func() bool { return n.Session != nil })
	if n.Session.Alert != "Hold your iPhone near a tag to read it." {
		t.Errorf("alert %q", n.Session.Alert)
	}
	n.Tap(&fake.NFCTag{ID: []byte{0x04, 0xa1}, NDEF: nfc.Message{nfc.URI("https://example.com/hello")}.Encode()})
	a.WaitFor("https://example.com/hello")
	a.Expect("ID 04 A1 · ndef, mifare, nfcA, mifareUltralight", "Read a link.")
	a.Until("the session's end", func() bool { return n.Session == nil })

	// A tag without a link.
	a.Tap("Read a Tag")
	a.Until("a session", func() bool { return n.Session != nil })
	n.Tap(&fake.NFCTag{NDEF: nfc.Message{nfc.Text("hi", "en")}.Encode()})
	a.WaitFor("“hi”")
	a.Expect("Read a text, not a link.")

	// Canceled.
	a.Tap("Read a Tag")
	a.Until("a session", func() bool { return n.Session != nil })
	n.Cancel()
	a.WaitFor("Canceled.")
}

func TestWrite(t *testing.T) {
	a, n := mount(t)
	a.Type("Link to write", "https://asuka.dev/nfc/42")
	a.Tap("Write to a Tag")
	a.Until("a session", func() bool { return n.Session != nil })
	tag := &fake.NFCTag{NDEF: nfc.Message{}.Encode()}
	n.Tap(tag)
	a.WaitFor("Written to the tag.")
	m, err := nfc.Decode(tag.NDEF)
	if u, _ := m.URI(); err != nil || u != "https://asuka.dev/nfc/42" || tag.Writes != 1 {
		t.Fatalf("the tag holds %v (%v)", m, err)
	}
	a.Expect("https://asuka.dev/nfc/42")

	// A locked tag isn't written.
	a.Tap("Write to a Tag")
	a.Until("a session", func() bool { return n.Session != nil })
	n.Tap(&fake.NFCTag{NDEF: nfc.Message{}.Encode(), ReadOnly: true})
	a.WaitFor("This tag is read-only: it can't be written.")
	if n.Session != nil || n.Sessions != 2 {
		t.Fatalf("the session after the locked tag: %+v", n.Session)
	}
}

func TestOff(t *testing.T) {
	a, n := mount(t)
	n.SetEnabled(false)
	a.WaitFor("NFC is off.")
	a.Tap("Turn NFC On")
	a.Until("the settings", func() bool { return n.Settings == 1 })
	a.Tap("Read a Tag")
	a.WaitFor("Turn NFC on to read tags.")
}

// A tag the system opens the app with: its link through ui.OnOpenURL, the
// others through nfc.OnOpen.
func TestOpened(t *testing.T) {
	a, n := mount(t)
	a.OpenURL("https://asuka.dev/nfc/7")
	a.WaitFor("https://asuka.dev/nfc/7")
	n.Open(&fake.NFCTag{ID: []byte{1}, Techs: []string{"iso7816", "isoDep"}})
	a.WaitFor("No link on this tag")
	a.Expect("ID 01 · iso7816, isoDep", "Not an NDEF tag (not NDEF).")
}
