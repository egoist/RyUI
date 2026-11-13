// TextEdit is a plain text editor with documents (asuka/sdk/document): on the
// desktop a window per file, the File menu (New, Open…, Open Recent,
// Save, Duplicate, Rename…, Revert…), autosave and recovery copies, and a
// bar when another app changes the file being edited; on a phone or in a
// browser, a list of its documents and one open at a time.
//
//	go run ./cmd/asuka dev examples/textedit
//	go run ./cmd/asuka dev --platform ios examples/textedit
package main

import (
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/sdk/document"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func main() {
	document.New(document.Options[string]{
		Types: []document.Type{
			{Name: "Plain Text", Extensions: []string{"txt", "text"}, MIMETypes: []string{"text/plain"}},
			{Name: "Markdown Document", Extensions: []string{"md", "markdown"}, MIMETypes: []string{"text/markdown"}},
		},
		Read: func(r io.Reader) (string, error) {
			b, err := io.ReadAll(r)
			return string(b), err
		},
		Write: func(w io.Writer, text string) error {
			_, err := io.WriteString(w, text)
			return err
		},
		// Undoing back to the file's text leaves the document unedited.
		Equal:  func(a, b string) bool { return a == b },
		Width:  640,
		Height: 480,
		View:   editor,
	}).Run()
}

// editor is a document's window: the text, a bar while another app's
// change conflicts with the user's, and a status line.
func editor(doc *document.Document[string]) ui.Node {
	state := doc.State()
	return ui.Column(ui.Style{Grow: 1},
		ui.Show(signal.Func(func() bool { return state.Get().Conflict }), func() ui.Node {
			return ui.Row(ui.Style{Gap: 12, Padding: ui.Symmetric(8, 16), Align: ui.AlignCenter, Background: colors.SecondaryBackground},
				ui.StaticText("Another app changed this file.", ui.Style{Grow: 1}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Revert"), OnPress: func() { doc.Revert(nil) }}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Keep Mine"), Variant: ui.ButtonFilled, OnPress: func() { doc.SaveAnyway(nil) }}),
			)
		}),
		ui.TextField(ui.TextFieldProps{
			Value:     doc.Content(),
			Multiline: true,
			Plain:     true,
			Style:     ui.Style{Grow: 1, Padding: ui.All(16), Font: fonts.Custom("monospace", fonts.Body)},
		}),
		ui.Text(signal.Func(func() string {
			s := state.Get()
			words := len(strings.FieldsFunc(doc.Content().Get(), unicode.IsSpace))
			status := "Saved"
			switch {
			case s.Deleted:
				status = "Deleted"
			case s.ReadOnly:
				status = "Read-only"
			case s.Edited:
				status = "Edited"
			}
			return fmt.Sprintf("%d words · %s", words, status)
		}), ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel, Padding: ui.Symmetric(6, 16)}),
	)
}
