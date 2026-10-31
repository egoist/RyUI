package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/sqlite"
	"github.com/egoist/ryui/ui"
)

// draft is a bookmark being made: from what another app shared (the
// share sheet), or a link pasted in the app.
type draft struct {
	URL, Title, Description, Note, Cover *signal.Signal[string]
	Collection                           *signal.Signal[int64]
	Tags                                 *signal.Signal[[]string]
	fetched                              string // the link whose page was read
}

func newDraft() *draft {
	return &draft{
		URL: signal.New(""), Title: signal.New(""), Description: signal.New(""), Note: signal.New(""), Cover: signal.New(""),
		Collection: signal.New[int64](0), Tags: signal.NewFunc[[]string](nil, nil),
	}
}

// fromShare fills the draft with what another app shared: its link (a
// text's, if it has none of its own), its title (a text that isn't the
// link: a page's title from a browser), or an image, kept as the cover.
func (d *draft) fromShare(items []ui.SharedItem) {
	for _, it := range items {
		switch {
		case it.Kind == ui.SharedLink && d.URL.Peek() == "":
			d.URL.Set(it.Text)
		case it.Kind == ui.SharedText:
			if u := linkRE.FindString(it.Text); u != "" && d.URL.Peek() == "" {
				d.URL.Set(u)
				if t := strings.TrimSpace(strings.Replace(it.Text, u, "", 1)); t != "" && d.Title.Peek() == "" {
					d.Title.Set(t)
				}
			} else if d.Title.Peek() == "" {
				d.Title.Set(it.Text)
			}
		case it.Kind == ui.SharedImage && d.Cover.Peek() == "":
			if p, err := keepFile(it.Path); err == nil {
				d.Cover.Set(p)
				if d.Title.Peek() == "" {
					d.Title.Set(it.Name)
				}
			}
		}
	}
}

// fetch reads the link's page for what the user hasn't written: its
// title, description and image, as they come.
func (d *draft) fetch() {
	link := d.URL.Peek()
	if link == "" || link == d.fetched || !strings.HasPrefix(link, "http") {
		return
	}
	d.fetched = link
	go func() {
		p, err := fetchPage(link)
		if err != nil {
			return
		}
		ui.Post(func() {
			if d.URL.Peek() != link {
				return
			}
			if d.Title.Peek() == "" || d.Title.Peek() == link {
				d.Title.Set(p.Title)
			}
			if d.Description.Peek() == "" {
				d.Description.Set(p.Description)
			}
			if d.Cover.Peek() == "" {
				d.Cover.Set(p.Image)
			}
		})
	}()
}

// save writes the bookmark; done runs once it's written. A share sheet
// closes then, and its process ends: waiting for the write keeps it.
func (d *draft) save(done func()) {
	title := strings.TrimSpace(d.Title.Peek())
	if title == "" {
		title = host(d.URL.Peek())
	}
	db.ExecAsync(`insert into bookmarks (url, title, description, note, cover, collection, tags, created) values (?, ?, ?, ?, ?, ?, ?, ?)`,
		[]any{strings.TrimSpace(d.URL.Peek()), title, strings.TrimSpace(d.Description.Peek()), strings.TrimSpace(d.Note.Peek()),
			d.Cover.Peek(), d.Collection.Peek(), strings.Join(d.Tags.Peek(), ","), time.Now().Unix()},
		func(_ sql.Result, err error) {
			if err != nil {
				ui.Alert(ui.AlertProps{Title: "Couldn't save", Message: err.Error()})
				return
			}
			done()
		})
}

// Editor is the bookmark's form, as Raindrop's share sheet: a header with
// Close and Save; a card with the title, description and cover, and a
// note; the collection, the tags; the link.
func Editor(d *draft, heading string, onClose func(), onSave func()) ui.Node {
	saving := signal.New(false)
	pickCollection := signal.New(false)
	pickTags := signal.New(false)
	collections := sqlite.Watch[collection](db, `select * from collections order by name`)
	collectionName := signal.Func(func() string {
		id := d.Collection.Get()
		for _, c := range collections.Get().Rows {
			if c.ID == id {
				return c.Name
			}
		}
		return "Unsorted"
	})
	d.fetch()

	return ui.Column(ui.Style{Grow: 1, Background: ui.GroupedBackground},
		// The header.
		ui.Row(ui.Style{Padding: ui.Symmetric(12, 16), Gap: 12, Align: ui.AlignCenter},
			ui.Button(ui.ButtonProps{Icon: "xmark", Label: "Close", Variant: ui.ButtonGlass, OnPress: onClose}),
			ui.StaticText(heading, ui.Style{Grow: 1, Font: ui.Headline, TextAlign: ui.TextCenter}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonGlassProminent,
				Disabled: signal.Func(func() bool { return saving.Get() || (d.URL.Get() == "" && d.Cover.Get() == "") }),
				OnPress: func() {
					saving.Set(true)
					d.save(onSave)
				}}),
		),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}, ContentStyle: ui.Style{Padding: ui.Edges{Top: 8, Right: 16, Bottom: 32, Left: 16}, Gap: 20}},
			// Title, description and cover; the note.
			card(
				ui.Row(ui.Style{Padding: ui.Edges{Top: 14, Right: 16, Bottom: 10, Left: 16}, Gap: 12},
					ui.Column(ui.Style{Grow: 1, Shrink: 1, Gap: 2},
						ui.TextField(ui.TextFieldProps{Value: d.Title, Placeholder: ui.Val("Title"), Multiline: true, Plain: true,
							Style: ui.Style{Font: ui.Title3.Weighted(ui.Semibold), Lines: 4}}),
						ui.TextField(ui.TextFieldProps{Value: d.Description, Placeholder: ui.Val("Add description"), Multiline: true, Plain: true,
							Style: ui.Style{Font: ui.Subheadline, Color: ui.SecondaryLabel, Lines: 4}}),
					),
					ui.Image(ui.ImageProps{Source: d.Cover, Mode: ui.ImageFill, Label: "Cover",
						Style: ui.Style{Width: ui.Pt(72), MinWidth: ui.Pt(72), Height: ui.Pt(72), CornerRadius: 10, Clip: true, Background: ui.GroupedBackground}}),
				),
				divider(),
				ui.TextField(ui.TextFieldProps{Value: d.Note, Placeholder: ui.Val("Note"), Multiline: true, Plain: true,
					Style: ui.Style{MinHeight: ui.Pt(96), Margin: ui.Symmetric(12, 16)}}),
			),
			// The collection.
			card(row("tray", signal.Func(func() string { return collectionName.Get() }), nil, func() { pickCollection.Set(true) })),
			// The tags.
			card(row("tag", signal.Func(func() string {
				if t := d.Tags.Get(); len(t) > 0 {
					return "#" + strings.Join(t, "  #")
				}
				return "Tags"
			}), signal.Func(func() bool { return len(d.Tags.Get()) == 0 }), func() { pickTags.Set(true) })),
			// The link.
			card(ui.Row(ui.Style{Padding: ui.Symmetric(14, 16), Gap: 14, Align: ui.AlignCenter},
				ui.Symbol("link", ui.Style{Color: ui.SecondaryLabel, Width: ui.Pt(24)}),
				ui.TextField(ui.TextFieldProps{Value: d.URL, Placeholder: ui.Val("https://"), Keyboard: ui.KeyboardURL,
					Autocapitalize: ui.CapitalizeNone, Autocorrect: ui.AutocorrectNo, OnBlur: d.fetch, OnSubmit: d.fetch, Plain: true,
					Style: ui.Style{Grow: 1, Color: ui.SecondaryLabel}}),
			)),
		),
		ui.Sheet(ui.SheetProps{Open: pickCollection, Detents: ui.DetentMedium | ui.DetentLarge, Grabber: true, Content: func() ui.Node {
			return CollectionPicker(collections, d.Collection, func() { pickCollection.Set(false) })
		}}),
		ui.Sheet(ui.SheetProps{Open: pickTags, Detents: ui.DetentMedium | ui.DetentLarge, Grabber: true, Content: func() ui.Node {
			return TagPicker(d.Tags)
		}}),
	)
}

// card groups rows on the grouped background.
func card(children ...ui.Node) ui.Node {
	return ui.Column(ui.Style{Background: ui.SecondaryGroupedBackground, CornerRadius: 24, Clip: true}, children...)
}

func divider() ui.Node {
	return ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(0.5), Background: ui.Separator, Margin: ui.Edges{Left: 16}}})
}

// row is a card's row that opens a picker: an icon, a value (secondary
// while it's a placeholder) and a chevron.
func row(icon ui.Icon, text signal.Accessor[string], placeholder signal.Accessor[bool], onPress func()) ui.Node {
	return ui.View(ui.ViewProps{OnPress: onPress, Role: ui.RoleButton,
		Style: ui.Style{Direction: ui.RowDir, Padding: ui.Symmetric(16, 16), Gap: 14, Align: ui.AlignCenter}},
		ui.Symbol(icon, ui.Style{Color: ui.SecondaryLabel, Width: ui.Pt(24)}),
		ui.Text(text, ui.StyleFunc(func() ui.Style {
			s := ui.Style{Grow: 1, Shrink: 1, Font: ui.Body, Lines: 1}
			if placeholder != nil && placeholder.Get() {
				s.Color = ui.SecondaryLabel
			}
			return s
		})),
		ui.Symbol("chevron.right", ui.Style{Color: ui.TertiaryLabel}),
	)
}

// CollectionPicker picks the bookmark's collection, or makes one.
func CollectionPicker(collections signal.Accessor[sqlite.Result[collection]], chosen *signal.Signal[int64], done func()) ui.Node {
	name := signal.New("")
	add := func() {
		n := strings.TrimSpace(name.Peek())
		if n == "" {
			return
		}
		db.ExecAsync(`insert or ignore into collections (name) values (?)`, []any{n}, func(_ sql.Result, err error) {
			if err == nil {
				if c, ok, _ := sqlite.QueryOne[collection](db, `select * from collections where name = ?`, n); ok {
					chosen.Set(c.ID)
					done()
				}
			}
		})
		name.Set("")
	}
	all := signal.Func(func() []collection {
		return append([]collection{{ID: 0, Name: "Unsorted"}}, collections.Get().Rows...)
	})
	return ui.Column(ui.Style{Grow: 1, Background: ui.GroupedBackground, Padding: ui.Edges{Top: 24, Right: 16, Left: 16}, Gap: 16},
		ui.StaticText("Collection", ui.Style{Font: ui.Title3.Weighted(ui.Bold)}),
		card(ui.For(all, func(c collection) int64 { return c.ID }, func(c signal.Accessor[collection], i signal.Accessor[int]) ui.Node {
			return ui.Column(ui.Style{},
				ui.Show(signal.Func(func() bool { return i.Get() > 0 }), divider),
				ui.View(ui.ViewProps{OnPress: func() { chosen.Set(signal.Untrack(c.Get).ID); done() }, Role: ui.RoleButton,
					Style: ui.Style{Direction: ui.RowDir, Padding: ui.Symmetric(14, 16), Gap: 14, Align: ui.AlignCenter}},
					ui.Symbol(ui.Icon(map[bool]string{true: "tray", false: "folder"}[signal.Untrack(c.Get).ID == 0]), ui.Style{Color: ui.Accent, Width: ui.Pt(24)}),
					ui.Text(signal.Func(func() string { return c.Get().Name }), ui.Style{Grow: 1}),
					ui.Show(signal.Func(func() bool { return chosen.Get() == c.Get().ID }), func() ui.Node {
						return ui.Symbol("checkmark", ui.Style{Color: ui.Accent})
					}),
				),
			)
		})),
		card(ui.Row(ui.Style{Padding: ui.Symmetric(10, 16), Gap: 12, Align: ui.AlignCenter},
			ui.Symbol("plus", ui.Style{Color: ui.Accent, Width: ui.Pt(24)}),
			ui.TextField(ui.TextFieldProps{Value: name, Placeholder: ui.Val("New collection"), ReturnKey: ui.ReturnDone, OnSubmit: add, Plain: true,
				Style: ui.Style{Grow: 1}}),
		)),
	)
}

// TagPicker adds and removes the bookmark's tags: those of other
// bookmarks to pick from, and new ones.
func TagPicker(tags *signal.Signal[[]string]) ui.Node {
	typed := signal.New("")
	used := sqlite.Watch[struct{ Tags string }](db, `select tags from bookmarks where tags != ''`)
	known := signal.Func(func() []string {
		var all []string
		for _, r := range used.Get().Rows {
			all = append(all, splitTags(r.Tags)...)
		}
		return splitTags(strings.Join(append(all, tags.Get()...), ","))
	})
	toggle := func(t string) {
		cur := tags.Peek()
		for i, x := range cur {
			if x == t {
				tags.Set(append(cur[:i:i], cur[i+1:]...))
				return
			}
		}
		tags.Set(append(cur[:len(cur):len(cur)], t))
	}
	add := func() {
		for _, t := range splitTags(strings.ReplaceAll(typed.Peek(), "#", "")) {
			if !strings.Contains(","+strings.Join(tags.Peek(), ",")+",", ","+t+",") {
				toggle(t)
			}
		}
		typed.Set("")
	}
	return ui.Column(ui.Style{Grow: 1, Background: ui.GroupedBackground, Padding: ui.Edges{Top: 24, Right: 16, Left: 16}, Gap: 16},
		ui.StaticText("Tags", ui.Style{Font: ui.Title3.Weighted(ui.Bold)}),
		card(ui.Row(ui.Style{Padding: ui.Symmetric(10, 16), Gap: 12, Align: ui.AlignCenter},
			ui.Symbol("tag", ui.Style{Color: ui.SecondaryLabel, Width: ui.Pt(24)}),
			ui.TextField(ui.TextFieldProps{Value: typed, Placeholder: ui.Val("Add a tag"), ReturnKey: ui.ReturnDone, OnSubmit: add, Plain: true,
				Autocapitalize: ui.CapitalizeNone, Style: ui.Style{Grow: 1}}),
		)),
		ui.Row(ui.Style{Wrap: true, Gap: 8},
			ui.For(known, func(t string) string { return t }, func(t signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
				on := signal.Func(func() bool {
					for _, x := range tags.Get() {
						if x == t.Get() {
							return true
						}
					}
					return false
				})
				return ui.View(ui.ViewProps{OnPress: func() { toggle(signal.Untrack(t.Get)) }, Role: ui.RoleButton,
					Style: ui.StyleFunc(func() ui.Style {
						s := ui.Style{Padding: ui.Symmetric(8, 14), CornerRadius: 16, Background: ui.SecondaryGroupedBackground}
						if on.Get() {
							s.Background = ui.Accent
						}
						return s
					})},
					ui.Text(signal.Func(func() string { return fmt.Sprintf("#%s", t.Get()) }), ui.StyleFunc(func() ui.Style {
						if on.Get() {
							return ui.Style{Color: ui.RGB(255, 255, 255)}
						}
						return ui.Style{}
					})),
				)
			}),
		),
	)
}
