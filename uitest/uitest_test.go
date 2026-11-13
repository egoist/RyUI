package uitest_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/egoist/asuka/uitest"
)

func counter() ui.Node {
	count := signal.New(0)
	return ui.Column(ui.Style{Gap: 8, Padding: ui.All(16)},
		ui.Text(signal.Func(func() string { return fmt.Sprintf("Count: %d", count.Get()) })),
		ui.Button(ui.ButtonProps{
			Title:   ui.Val("Add"),
			OnPress: func() { count.Update(func(n int) int { return n + 1 }) },
		}),
		ui.Button(ui.ButtonProps{
			Title:    ui.Val("Reset"),
			Disabled: signal.Func(func() bool { return count.Get() == 0 }),
			OnPress:  func() { count.Set(0) },
		}),
		ui.Show(signal.Func(func() bool { return count.Get() > 2 }),
			func() ui.Node { return ui.StaticText("That's a lot") }),
	)
}

func TestTapAndExpect(t *testing.T) {
	a := uitest.Mount(t, counter)
	a.Expect("Count: 0", "Add")
	a.ExpectNo("That's a lot")
	for range 3 {
		a.Tap("Add")
	}
	a.Expect("Count: 3", "That's a lot")
	if got := a.Texts(); strings.Join(got, "|") != "Count: 3|Add|Reset|That's a lot" {
		t.Fatal(got)
	}
	a.Tap("Reset")
	a.Expect("Count: 0")
	if v := a.Get("Add"); v.Kind != ops.KindButton || v.Frame[3] != 44 {
		t.Fatal(v.Kind, v.Frame)
	}
}

func TestFailures(t *testing.T) {
	for _, c := range []struct {
		name string
		do   func(a *uitest.App)
		want string
	}{
		{"missing", func(a *uitest.App) { a.Tap("Remove") }, `doesn't show "Remove"`},
		{"disabled", func(a *uitest.App) { a.Tap("Reset") }, `"Reset" is disabled`},
		{"expect", func(a *uitest.App) { a.Expect("Count: 9") }, "Button 16,44 358x44 \"Add\""},
	} {
		ft := &fakeT{T: t}
		func() {
			defer func() { recover() }() // Fatal's Goexit, as a panic here
			a := uitest.Mount(ft, counter)
			c.do(a)
		}()
		ft.cleanup()
		if !strings.Contains(ft.msg, c.want) {
			t.Errorf("%s: %q, want %q in it", c.name, ft.msg, c.want)
		}
	}
}

// fakeT records a test's failure instead of failing.
type fakeT struct {
	*testing.T
	msg      string
	cleanups []func()
}

func (f *fakeT) Helper()                   {}
func (f *fakeT) Cleanup(c func())          { f.cleanups = append(f.cleanups, c) }
func (f *fakeT) Errorf(s string, a ...any) { f.msg += fmt.Sprintf(s, a...) }
func (f *fakeT) Fatalf(s string, a ...any) { f.msg += fmt.Sprintf(s, a...); panic("fatal") }
func (f *fakeT) Fatal(a ...any)            { f.msg += fmt.Sprint(a...); panic("fatal") }
func (f *fakeT) cleanup() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
}

func TestNavigation(t *testing.T) {
	var detail func(n int) func() ui.Node
	detail = func(n int) func() ui.Node {
		return func() ui.Node {
			nav := ui.UseNavigator()
			return ui.Column(ui.Style{},
				ui.StaticText(fmt.Sprintf("Page %d", n)),
				ui.Button(ui.ButtonProps{Title: ui.Val("Next"), OnPress: func() {
					nav.Push(detail(n+1), ui.ScreenOptions{Title: ui.Val(fmt.Sprintf("Title %d", n+1))})
				}}),
			)
		}
	}
	a := uitest.Mount(t, func() ui.Node {
		return ui.Stack(detail(1), ui.ScreenOptions{Title: ui.Val("Home")})
	})
	a.Expect("Home", "Page 1")
	a.Tap("Next")
	a.Tap("Next")
	a.Expect("Title 3", "Page 3")
	a.ExpectNo("Page 1", "Page 2", "Home") // under the top screen
	a.Back()
	a.Expect("Page 2")
	a.ExpectNo("Page 3")
	a.Back()
	a.Expect("Home", "Page 1")
}

func TestTabs(t *testing.T) {
	sel := signal.New(0)
	a := uitest.Mount(t, func() ui.Node {
		return ui.Tabs(ui.TabsProps{Selected: sel},
			ui.Tab{Title: ui.Val("Inbox"), Content: func() ui.Node { return ui.StaticText("No mail") }},
			ui.Tab{Title: ui.Val("Settings"), Content: func() ui.Node { return ui.StaticText("Account") }},
		)
	})
	a.Expect("Inbox", "Settings", "No mail")
	a.ExpectNo("Account")
	a.Tap("Settings")
	if sel.Peek() != 1 {
		t.Fatal(sel.Peek())
	}
	a.Expect("Account")
	a.ExpectNo("No mail")
}

func TestSheetTakesTheTouches(t *testing.T) {
	open := signal.New(false)
	pressed := 0
	a := uitest.Mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Button(ui.ButtonProps{Title: ui.Val("Open"), OnPress: func() { open.Set(true) }}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Under"), OnPress: func() { pressed++ }}),
			ui.Sheet(ui.SheetProps{Open: open, Content: func() ui.Node {
				return ui.Column(ui.Style{},
					ui.StaticText("In the sheet"),
					ui.Button(ui.ButtonProps{Title: ui.Val("Close"), OnPress: func() { open.Set(false) }}),
				)
			}}),
		)
	})
	a.Tap("Open")
	a.Expect("In the sheet", "Under") // still seen, under the sheet
	ft := &fakeT{T: t}
	func() {
		defer func() { recover() }()
		a.SetT(ft)
		a.Tap("Under")
	}()
	a.SetT(t)
	if pressed != 0 || !strings.Contains(ft.msg, "out of reach") {
		t.Fatal(pressed, ft.msg)
	}
	if f := a.Get("In the sheet").Frame; f[2] != 390 {
		t.Fatal("the sheet's content isn't laid out at the screen's width:", f)
	}
	a.Tap("Close")
	a.ExpectNo("In the sheet")
	a.Tap("Open")
	a.Dismiss()
	if open.Peek() {
		t.Fatal("dismissing didn't close it")
	}
	a.Tap("Under")
	if pressed != 1 {
		t.Fatal(pressed)
	}
}

func TestAlert(t *testing.T) {
	name := signal.New("")
	var got string
	a := uitest.Mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("Rename"), OnPress: func() {
			ui.Alert(ui.AlertProps{
				Title:  "Rename",
				Fields: []ui.AlertField{{Text: name, Placeholder: "Name"}},
				Actions: []ui.Action{
					{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
					{Title: ui.Val("Save"), Role: ui.ActionDefault,
						Disabled: signal.Func(func() bool { return name.Get() == "" }),
						OnPress:  func() { got = name.Peek() }},
				},
			})
		}})
	})
	a.Tap("Rename")
	a.Expect("Cancel", "Save")
	a.Type("Name", "Ada")
	a.Tap("Save")
	if got != "Ada" {
		t.Fatal(got)
	}
	a.ExpectNo("Save")
}

type row struct {
	ID    int
	Title string
}

func TestListBindsWhatShows(t *testing.T) {
	var all []row
	for i := range 1000 {
		all = append(all, row{i, fmt.Sprint("Row ", i)})
	}
	items := signal.NewFunc(all, nil)
	var deleted []int
	var picked []int
	a := uitest.Mount(t, func() ui.Node {
		return ui.List(ui.ListProps{
			SwipeActions: func(i int) []ui.Action {
				return []ui.Action{{Title: ui.Val("Delete"), Role: ui.ActionDestructive, OnPress: func() { deleted = append(deleted, i) }}}
			},
			OnSelect: func(i int, on bool) { picked = append(picked, i) },
		}, items, func(r row) int { return r.ID },
			func(it signal.Accessor[row], _ signal.Accessor[int]) ui.Node {
				return ui.Text(signal.Func(func() string { return it.Get().Title }), ui.Style{Padding: ui.All(12)})
			})
	})
	// 844 points of 44-point rows: 20 and one more line.
	a.Expect("Row 0", "Row 19")
	a.ExpectNo("Row 30")
	if n := len(a.All(ops.KindText)); n != 20 {
		t.Fatal(n, "rows bound")
	}
	a.Tap("Row 3")
	a.Swipe("Row 5", "Delete")
	if !a.In(uitest.Under(a.Row("Row 4"))).Has("Row 4") || a.In(uitest.Under(a.Row("Row 4"))).Has("Row 5") {
		t.Fatal("a row holds another's text")
	}
	if fmt.Sprint(picked, deleted) != "[3] [5]" {
		t.Fatal(picked, deleted)
	}
	a.ScrollTo("Row 500")
	a.ExpectNo("Row 0")
	// The items change: the rows showing follow.
	items.Set(all[:3])
	a.Settle()
	a.ScrollTo("Row 2")
	a.ExpectNo("Row 500", "Row 3")
}

func TestMenus(t *testing.T) {
	var chose []string
	act := func(s string) ui.Action {
		return ui.Action{Title: ui.Val(s), OnPress: func() { chose = append(chose, s) }}
	}
	a := uitest.Mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node {
			return ui.Column(ui.Style{},
				ui.Menu(ui.MenuProps{Actions: []ui.Action{act("Name"), ui.Submenu(ui.Val("More"), "", act("Date"))}},
					ui.Button(ui.ButtonProps{Title: ui.Val("Sort")})),
				ui.Menu(ui.MenuProps{LongPress: true, Actions: []ui.Action{act("Copy")}},
					ui.StaticText("Message")),
			)
		}, ui.ScreenOptions{Title: ui.Val("Home"), Actions: []ui.Action{
			{Title: ui.Val("Add"), Children: []ui.Action{act("Note"), act("Folder")}},
			act("Edit"),
		}})
	})
	a.Tap("Sort")
	a.Expect("Name", "More")
	a.Tap("More")
	a.Tap("Date")
	a.LongPress("Message")
	a.Tap("Copy")
	a.Tap("Edit")
	a.Tap("Add")
	a.Tap("Folder")
	a.Tap("Sort")
	a.Back() // closes the menu
	a.ExpectNo("Name")
	if strings.Join(chose, ",") != "Date,Copy,Edit,Folder" {
		t.Fatal(chose)
	}
}

func TestForm(t *testing.T) {
	name := signal.New("")
	news := signal.New(false)
	vol := signal.New(0.0)
	size := signal.New(0)
	sent := ""
	a := uitest.Mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.TextField(ui.TextFieldProps{Value: name, Placeholder: ui.Val("Your name"),
				OnSubmit: func() { sent = name.Peek() }}),
			ui.Row(ui.Style{}, ui.StaticText("Newsletter"), ui.Toggle(ui.ToggleProps{Value: news})),
			ui.Row(ui.Style{}, ui.StaticText("Volume"), ui.Slider(ui.SliderProps{Value: vol, Max: 10})),
			ui.Segmented(ui.SegmentedProps{Segments: ui.Val([]string{"S", "M", "L"}), Selected: size}),
		)
	})
	a.Type("Your name", "Ada")
	a.Submit("")
	a.Toggle("Newsletter")
	a.Slide("Volume", 7)
	a.Select("L")
	a.Tap("M")
	if sent != "Ada" || !news.Peek() || vol.Peek() != 7 || size.Peek() != 1 {
		t.Fatal(sent, news.Peek(), vol.Peek(), size.Peek())
	}
	a.Expect("Ada") // the field's text
}

func TestWaitsForGoroutines(t *testing.T) {
	a := uitest.Mount(t, func() ui.Node {
		status := signal.New("Loading…")
		go func() { // a request
			ui.Post(func() { status.Set("Loaded 3 items") })
		}()
		return ui.Text(status)
	})
	a.WaitFor("Loaded 3 items")
	a.WaitGone("Loading…")
}

func TestSnapshot(t *testing.T) {
	a := uitest.Mount(t, counter, uitest.Platform(ui.PlatformAndroid))
	a.Tap("Add")
	a.MatchSnapshot("one")
}

func TestPlatformOptions(t *testing.T) {
	var p ui.PlatformKind
	var dark bool
	a := uitest.Mount(t, func() ui.Node {
		p, dark = ui.Platform(), ui.UseEnvironment().Get().Dark
		return ui.StaticText("hi")
	}, uitest.Platform(ui.PlatformMacOS), uitest.Dark(), uitest.Size(1000, 700))
	if p != ui.PlatformMacOS || !dark || a.Host.Root().Frame != [4]float32{0, 0, 1000, 700} {
		t.Fatal(p, dark, a.Host.Root().Frame)
	}
}

func TestScopes(t *testing.T) {
	var pressed []string
	act := func(s string) ui.Action {
		return ui.Action{Title: ui.Val("Star"), OnPress: func() { pressed = append(pressed, s) }}
	}
	a := uitest.Mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node {
			return ui.Column(ui.Style{},
				ui.Row(ui.Style{}, ui.StaticText("Ada"), ui.Button(ui.ButtonProps{Title: ui.Val("Star"), OnPress: func() { pressed = append(pressed, "ada") }})),
				ui.Row(ui.Style{}, ui.StaticText("Ken"), ui.Button(ui.ButtonProps{Title: ui.Val("Star"), OnPress: func() { pressed = append(pressed, "ken") }})),
			)
		}, ui.ScreenOptions{Title: ui.Val("People"), Actions: []ui.Action{act("bar")}, Toolbar: []ui.Action{act("toolbar")}})
	})
	ft := &fakeT{T: t}
	func() {
		defer func() { recover() }()
		a.SetT(ft)
		a.Tap("Star")
	}()
	a.SetT(t)
	if !strings.Contains(ft.msg, "4 things show \"Star\"; narrow it with a.In(uitest.NavigationBar), a.In(uitest.Toolbar), a.In(uitest.Content)") {
		t.Fatal(ft.msg)
	}
	a.In(uitest.NavigationBar).Tap("Star")
	a.In(uitest.Toolbar).Tap("Star")
	if !a.In(uitest.NavigationBar).Has("People") || a.In(uitest.Content).Has("People") {
		t.Fatal("the title isn't the bar's")
	}
	// A row's button: a Row makes no view, so none holds the two.
	a.In(uitest.NextTo("Ken")).Tap("Star")
	a.In(uitest.NextTo("Ada")).Tap("Star")
	if !a.In(uitest.Under(a.Get("Ada").Parent)).Has("Ken") {
		t.Fatal("Under: the column holds Ken")
	}
	if strings.Join(pressed, ",") != "bar,toolbar,ken,ada" {
		t.Fatal(pressed)
	}
}

func TestLabelsNextToControls(t *testing.T) {
	wifi, bt, plane := signal.New(false), signal.New(false), signal.New(false)
	name := signal.New("")
	a := uitest.Mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(16), Gap: 12},
			// Rows make no view of their own: the labels and switches are
			// siblings, told apart by where they are.
			ui.Row(ui.Style{}, ui.StaticText("Wi-Fi", ui.Style{Grow: 1}), ui.Toggle(ui.ToggleProps{Value: wifi})),
			ui.Row(ui.Style{}, ui.StaticText("Bluetooth", ui.Style{Grow: 1}), ui.Toggle(ui.ToggleProps{Value: bt})),
			ui.View(ui.ViewProps{Label: ui.Val("Airplane mode")}, ui.Toggle(ui.ToggleProps{Value: plane})),
			ui.StaticText("Name"), // a label over its field
			ui.TextField(ui.TextFieldProps{Value: name}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Disabled: signal.Func(func() bool { return name.Get() == "" })}),
		)
	})
	a.Toggle("Bluetooth")
	a.Toggle("Airplane mode")
	if wifi.Peek() || !bt.Peek() || !plane.Peek() {
		t.Fatal(wifi.Peek(), bt.Peek(), plane.Peek())
	}
	if !a.Disabled("Save") {
		t.Fatal("Save enabled with no name")
	}
	a.Type("Name", "Ada")
	if name.Peek() != "Ada" || a.Disabled("Save") {
		t.Fatal(name.Peek())
	}
}

func TestBackAsksTheSheet(t *testing.T) {
	open := signal.New(false)
	draft := signal.New("")
	a := uitest.Mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Button(ui.ButtonProps{Title: ui.Val("Write"), OnPress: func() { open.Set(true) }}),
			ui.Sheet(ui.SheetProps{Open: open, FullScreen: true, Content: func() ui.Node {
				ui.OnBack(func() bool {
					if draft.Peek() == "" {
						return false
					}
					ui.Alert(ui.AlertProps{Title: "Discard?", Actions: []ui.Action{
						{Title: ui.Val("Keep"), Role: ui.ActionCancel},
						{Title: ui.Val("Discard"), Role: ui.ActionDestructive, OnPress: func() { open.Set(false) }},
					}})
					return true
				})
				return ui.TextField(ui.TextFieldProps{Value: draft, Placeholder: ui.Val("Message")})
			}}),
		)
	})
	a.Tap("Write")
	a.Back() // nothing written: it closes
	a.ExpectNo("Message")
	a.Tap("Write")
	a.Type("Message", "Hi")
	a.Back()
	a.Expect("Discard?")
	a.Back() // the alert's cancel
	a.ExpectNo("Discard?")
	a.Expect("Message")
	a.Back()
	a.Tap("Discard")
	a.ExpectNo("Message")
}
