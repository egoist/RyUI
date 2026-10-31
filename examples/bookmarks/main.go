// Bookmarks saves links from other apps, as Raindrop does: "Share" in
// a browser, YouTube or Photos shows the app's own sheet over that app
// (app.ShareSheet), with the page's title, description and image read
// from it, a note, a collection and tags. Save, and the sheet closes back
// to the app you were in; the bookmark is in the app's list.
//
//	go run ./cmd/ryui dev -ios examples/bookmarks
package main

import (
	"strings"

	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/device"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/sqlite"
	"github.com/egoist/ryui/ui"
)

// ShareSheet is what "Share" in another app shows: the editor, filled
// with what's shared. It runs in a process of its own (the share
// extension, Android's share activity), over the other app.
func ShareSheet(s *ui.ShareSheet) ui.Node {
	d := newDraft()
	d.fromShare(s.Items)
	heading := "New bookmark"
	if d.URL.Peek() == "" && d.Cover.Peek() != "" {
		heading = "New image"
	}
	return Editor(d, heading, s.Cancel, s.Close)
}

func App() ui.Node {
	query := signal.New("")
	filter := signal.New[int64](-1) // -1: all
	adding := signal.New(false)
	var added *draft
	// The share sheet writes from its own process: look again on coming
	// back.
	app.OnLifecycle(func(st app.State) {
		if st == app.Foreground {
			db.Changed()
		}
	})
	return ui.Stack(func() ui.Node {
		marks := sqlite.WatchWith[bookmark](db,
			`select * from bookmarks
			 where (?1 < 0 or collection = ?1)
			   and (?2 = '' or instr(lower(title || ' ' || url || ' ' || tags || ' ' || note), lower(?2)) > 0)
			 order by created desc`,
			func() []any { return []any{filter.Get(), query.Get()} })
		collections := sqlite.Watch[collection](db, `select * from collections order by name`)
		rows := signal.Func(func() []bookmark { return marks.Get().Rows })

		chips := signal.Func(func() []collection {
			return append([]collection{{ID: -1, Name: "All"}, {ID: 0, Name: "Unsorted"}}, collections.Get().Rows...)
		})
		return ui.SafeArea(ui.Style{Grow: 1},
			// Collections, to filter by.
			ui.Scroll(ui.ScrollProps{Horizontal: true, ContentStyle: ui.Style{Direction: ui.RowDir, Gap: 8, Padding: ui.Symmetric(8, 16)}},
				ui.For(chips, func(c collection) int64 { return c.ID }, func(c signal.Accessor[collection], _ signal.Accessor[int]) ui.Node {
					on := signal.Func(func() bool { return filter.Get() == c.Get().ID })
					return ui.View(ui.ViewProps{OnPress: func() { filter.Set(signal.Untrack(c.Get).ID) }, Role: ui.RoleButton,
						Style: ui.StyleFunc(func() ui.Style {
							s := ui.Style{Padding: ui.Symmetric(7, 14), CornerRadius: 16, Background: ui.GroupedBackground}
							if on.Get() {
								s.Background = ui.Accent
							}
							return s
						})},
						ui.Text(signal.Func(func() string { return c.Get().Name }), ui.StyleFunc(func() ui.Style {
							if on.Get() {
								return ui.Style{Color: ui.RGB(255, 255, 255), Font: ui.Subheadline.Weighted(ui.Semibold)}
							}
							return ui.Style{Font: ui.Subheadline}
						})),
					)
				}),
			),
			ui.Show(signal.Func(func() bool { r := marks.Get(); return !r.Loading && len(r.Rows) == 0 }), func() ui.Node {
				return ui.Column(ui.Style{Grow: 1, Justify: ui.JustifyCenter, Align: ui.AlignCenter, Gap: 10, Padding: ui.All(32)},
					ui.Symbol("bookmark", ui.Style{Font: ui.LargeTitle, Color: ui.TertiaryLabel}),
					ui.StaticText("No bookmarks yet", ui.Style{Font: ui.Headline}),
					ui.StaticText("Share a page from your browser, or a video or photo from any app, and pick Bookmarks.",
						ui.Style{Color: ui.SecondaryLabel, TextAlign: ui.TextCenter}),
				)
			}),
			ui.List(ui.ListProps{SwipeActions: func(i int) []ui.Action {
				return []ui.Action{{Title: "Delete", Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() {
					if l := signal.Untrack(rows.Get); i < len(l) {
						write(`delete from bookmarks where id = ?`, l[i].ID)
					}
				}}}
			}}, rows, func(b bookmark) int64 { return b.ID }, func(b signal.Accessor[bookmark], _ signal.Accessor[int]) ui.Node {
				return ui.View(ui.ViewProps{
					OnPress: func() {
						if u := signal.Untrack(b.Get).URL; u != "" {
							device.Browse(u)
						}
					},
					Style: ui.Style{Direction: ui.RowDir, Gap: 14, Padding: ui.Symmetric(12, 16), Align: ui.AlignCenter},
				},
					ui.Image(ui.ImageProps{Source: signal.Func(func() string { return b.Get().Cover }), Mode: ui.ImageFill,
						Style: ui.Style{Width: ui.Pt(56), MinWidth: ui.Pt(56), Height: ui.Pt(56), CornerRadius: 10, Clip: true, Background: ui.GroupedBackground}}),
					ui.Column(ui.Style{Grow: 1, Shrink: 1, Gap: 3},
						ui.Text(signal.Func(func() string { return b.Get().Title }), ui.Style{Font: ui.Body.Weighted(ui.Semibold), Lines: 2}),
						ui.Text(signal.Func(func() string {
							m := b.Get()
							parts := []string{}
							if m.URL != "" {
								parts = append(parts, host(m.URL))
							}
							for _, t := range splitTags(m.Tags) {
								parts = append(parts, "#"+t)
							}
							return strings.Join(parts, " · ")
						}), ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel, Lines: 1}),
						ui.Show(signal.Func(func() bool { return b.Get().Note != "" }), func() ui.Node {
							return ui.Text(signal.Func(func() string { return b.Get().Note }), ui.Style{Font: ui.Footnote, Lines: 2})
						}),
					),
				)
			}),
			// A link added in the app: the same editor, in a sheet.
			ui.Sheet(ui.SheetProps{Open: adding, Content: func() ui.Node {
				return Editor(added, "New bookmark", func() { adding.Set(false) }, func() { adding.Set(false) })
			}}),
		)
	}, ui.ScreenOptions{
		Title:  ui.Val("Bookmarks"),
		Search: &ui.Search{Text: query, Placeholder: "Search bookmarks"},
		Actions: []ui.Action{{Title: "Add", Icon: ui.IconPlus, OnPress: func() {
			added = newDraft()
			// A link on the clipboard: the one to add, most likely.
			d := added
			go func() {
				text, ok, _ := device.Clipboard()
				ui.Post(func() {
					if u := linkRE.FindString(text); ok && u != "" {
						d.URL.Set(u)
					}
					adding.Set(true)
				})
			}()
		}}},
	})
}

func main() {
	openDB()
	app.ShareSheet(ShareSheet)
	app.Run(App)
}
