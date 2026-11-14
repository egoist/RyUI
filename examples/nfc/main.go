// NFC Tags reads the link on an NFC tag, and writes one to it
// (asuka/sdk/nfc): iOS's sheet, Android's reader mode, Web NFC in Chrome
// on Android. On Android, a tag with a link under https://asuka.dev/nfc/
// opens the app (asuka.json's nfc), which shows it.
//
//	go run ./cmd/asuka dev --platform android examples/nfc
package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/sdk/nfc"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func Tags() ui.Node {
	state := nfc.Use()
	status := signal.New("Hold a tag near the phone after tapping Read or Write.")
	link := signal.New("")  // the link read
	about := signal.New("") // the tag's ID and technologies
	write := signal.New("https://asuka.dev")
	var session *nfc.Session
	signal.OnCleanup(func() {
		if session != nil {
			session.Stop()
		}
	})
	// A tag that opened the app: its link (Android's filter, iOS's
	// universal links), or another kind.
	ui.OnOpenURL(func(u string) {
		link.Set(u)
		status.Set("Opened with a tag's link.")
	})
	nfc.OnOpen(func(t *nfc.Tag) { show(t, link, about, status) })

	ended := func(err error) {
		switch {
		case errors.Is(err, nfc.ErrCanceled):
			status.Set("Canceled.")
		case errors.Is(err, nfc.ErrTimeout):
			status.Set("No tag came near in time.")
		case errors.Is(err, nfc.ErrNotAvailable):
			status.Set("This device can't read NFC tags.")
		case errors.Is(err, nfc.ErrDisabled):
			status.Set("Turn NFC on to read tags.")
		case err != nil:
			status.Set(err.Error())
		}
	}

	read := func() {
		status.Set("Hold a tag near the phone…")
		session = nfc.Start(nfc.Options{Alert: "Hold your iPhone near a tag to read it.", OnEnd: ended}, func(t *nfc.Tag) {
			show(t, link, about, status)
			session.SetAlert("Read.")
			session.Stop()
		})
	}

	writeTag := func() {
		u := strings.TrimSpace(write.Peek())
		if u == "" {
			status.Set("Type a link to write.")
			return
		}
		status.Set("Hold a tag near the phone to write it…")
		var s *nfc.Session
		s = nfc.Start(nfc.Options{Alert: "Hold your iPhone near a tag to write it.", OnEnd: ended}, func(t *nfc.Tag) {
			if !t.NDEF.Writable() {
				s.Fail("This tag can't be written.")
				status.Set(fmt.Sprintf("This tag is %s: it can't be written.", t.NDEF.Format))
				return
			}
			s.SetAlert("Writing…")
			go func() {
				err := t.Write(nfc.Message{nfc.URI(u)})
				ui.Post(func() {
					if err != nil {
						s.Fail("Couldn't write the tag.")
						status.Set("Couldn't write: " + err.Error())
						return
					}
					s.SetAlert("Written.")
					s.Stop()
					link.Set(u)
					status.Set("Written to the tag.")
				})
			}()
		})
		session = s
	}

	secondary := ui.Style{Color: colors.SecondaryLabel}
	return ui.SafeArea(ui.Style{Gap: 16, Padding: ui.All(24)},
		ui.Text(ui.Val("NFC Tags"), ui.Style{Font: fonts.LargeTitle.Weighted(fonts.Bold)}),
		ui.Text(signal.Func(func() string {
			s := state.Get()
			switch {
			case !s.Supported:
				return "This device can't read NFC tags."
			case !s.Enabled:
				return "NFC is off."
			}
			return "NFC is on."
		}), secondary),
		ui.Show(signal.Func(func() bool { s := state.Get(); return s.Supported && !s.Enabled }), func() ui.Node {
			return ui.Button(ui.ButtonProps{Title: ui.Val("Turn NFC On"), OnPress: func() {
				go nfc.OpenSettings()
			}})
		}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Read a Tag"), Variant: ui.ButtonFilled, OnPress: read}),
		ui.Show(signal.Func(func() bool { return link.Get() != "" }), func() ui.Node {
			return ui.Column(ui.Style{Gap: 4},
				ui.Text(link, ui.Style{Font: fonts.Headline}),
				ui.Text(about, secondary),
			)
		}),
		ui.Text(ui.Val("Link to write"), ui.Style{Font: fonts.Subheadline}),
		ui.TextField(ui.TextFieldProps{Value: write, Placeholder: ui.Val("https://…"), Keyboard: ui.KeyboardURL,
			Autocapitalize: ui.CapitalizeNone, ContentType: ui.ContentURL}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Write to a Tag"), Variant: ui.ButtonTinted, OnPress: writeTag}),
		ui.Text(status, secondary),
	)
}

// show shows what a tag holds: its link, or what else it has.
func show(t *nfc.Tag, link, about, status *signal.Signal[string]) {
	techs := make([]string, len(t.Techs))
	for i, x := range t.Techs {
		techs[i] = string(x)
	}
	id := ""
	if len(t.ID) > 0 {
		id = fmt.Sprintf("ID % X · ", t.ID)
	}
	about.Set(id + strings.Join(techs, ", "))
	if u, ok := t.Message.URI(); ok {
		link.Set(u)
		status.Set("Read a link.")
		return
	}
	if s, _, ok := t.Message.Text(); ok {
		link.Set("“" + s + "”")
		status.Set("Read a text, not a link.")
		return
	}
	link.Set("No link on this tag")
	if t.Message == nil {
		status.Set(fmt.Sprintf("Not an NDEF tag (%s).", t.NDEF.Format))
	} else {
		status.Set(fmt.Sprintf("%d records, none a link.", len(t.Message)))
	}
}

func main() { app.Run(Tags) }
