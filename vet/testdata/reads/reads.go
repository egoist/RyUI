package reads

import (
	"fmt"
	"log"
	"strconv"

	"example.com/vettest/msgs"
	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/markdown"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Reported: a read whose value goes into the tree a component builds.

func Constant() ui.Node {
	count := signal.New(0)
	return ui.Column(nil,
		ui.StaticText(fmt.Sprintf("%d points", count.Get())),                // want `count.Get\(\) is read once, while Constant builds its views, so the constant made from it never changes: give the prop signal.Func\(func\(\) T \{ ... \}\) instead, or use count.Peek\(\) to read it once on purpose`
		ui.Button(ui.ButtonProps{Title: ui.Val(strconv.Itoa(count.Get()))}), // want `count.Get\(\) is read once, while Constant builds its views, so the constant`
	)
}

func Branch(on *signal.Signal[bool]) ui.Node {
	if on.Get() { // want `on.Get\(\) is read once, while Branch builds its views, so the branch is chosen once: choose it with ui.Show`
		return ui.StaticText("on")
	}
	return ui.StaticText("off")
}

func SwitchOn(n *signal.Signal[int]) ui.Node {
	switch n.Get() { // want `so the branch is chosen once`
	case 0:
		return ui.StaticText("none")
	}
	return ui.StaticText("some")
}

func Styled(dim *signal.Memo[bool]) ui.Node {
	c := colors.Label
	return ui.View(ui.ViewProps{Style: ui.Style{Color: pick(dim.Get(), c)}}) // want `dim.Get\(\) is read once, while Styled builds its views, so the style made from it never changes: make the style a ui.StyleFunc, or use dim.Peek\(\)`
}

func pick(dim bool, c ui.Color) ui.Color {
	if dim {
		return colors.SecondaryLabel
	}
	return c
}

func Rows(items *signal.Signal[[]string]) ui.Node {
	var kids []ui.Node
	for _, it := range items.Get() { // want `so the views made for it never change: repeat them with ui.For`
		kids = append(kids, ui.StaticText(it))
	}
	return ui.Column(nil, kids...)
}

// Through local variables.
func Stored(count *signal.Signal[int]) ui.Node {
	n := count.Get() // want `count.Get\(\) is read once, while Stored builds its views, so the constant`
	label := fmt.Sprint(n)
	return ui.StaticText(label)
}

// An accessor, which has no Peek.
func Badge(n signal.Accessor[int]) ui.Node {
	return ui.StaticText(strconv.Itoa(n.Get())) // want `n.Get\(\) is read once, while Badge builds its views, .* or signal.Untrack\(n.Get\) to read it once on purpose`
}

// What's returned is the tree.
func Kids(nodes *signal.Signal[[]ui.Node]) []ui.Node {
	return nodes.Get() // want `nodes.Get\(\) is read once, while Kids builds its views, and never again`
}

// Given to a component, as a plain value.
func Passed(name *signal.Signal[string]) ui.Node {
	return Card(name.Get()) // want `name.Get\(\) is read once, while Passed builds its views, and never again`
}

func Card(title string) ui.Node { return ui.StaticText(title) }

type screen struct{ title *signal.Signal[string] }

func (s *screen) View() ui.Node {
	return ui.StaticText(s.title.Get()) // want `s.title.Get\(\) is read once, while View builds its views`
}

// What runs once: a branch, a row, a screen, a sheet's content.
func Callbacks(open *signal.Signal[bool], name *signal.Signal[string], items signal.Accessor[[]item], nav *ui.Navigator) ui.Node {
	return ui.Column(nil,
		ui.Show(open, func() ui.Node {
			return ui.StaticText(name.Get()) // want `name.Get\(\) is read once, while this ui.Show branch builds its views`
		}),
		ui.For(items, func(it item) int { return it.id }, func(it signal.Accessor[item], i signal.Accessor[int]) ui.Node {
			return ui.Row(nil,
				ui.StaticText(it.Get().title),        // want `it.Get\(\) is read once, while this ui.For row builds its views`
				ui.StaticText(strconv.Itoa(i.Get())), // want `i.Get\(\) is read once, while this ui.For row builds its views`
			)
		}),
		ui.Sheet(ui.SheetProps{Open: open, Content: func() ui.Node {
			return ui.StaticText(name.Get()) // want `name.Get\(\) is read once, while this SheetProps.Content builds its views`
		}}),
		ui.Button(ui.ButtonProps{OnPress: func() {
			nav.Push(func() ui.Node {
				return ui.StaticText(name.Get()) // want `name.Get\(\) is read once, while this screen builds its views`
			}, ui.ScreenOptions{})
		}}),
	)
}

type item struct {
	id    int
	title string
}

// A closure of the component is part of it: calling it reads.
func Closure(count *signal.Signal[int]) ui.Node {
	label := func() string { return fmt.Sprint(count.Get()) }
	return ui.Column(nil,
		ui.StaticText(label()), // want `label\(\) reads count.Get\(\), once, while Closure builds its views`
		ui.Text(signal.Func(label)),
	)
}

// The language the app shows, directly, from a catalog, through a helper.
func Language() ui.Node {
	return ui.Column(nil,
		ui.StaticText(ui.Locale()),  // want `ui.Locale\(\) is called once, while Language builds its views, so the constant made from it won.t follow the language the app shows: give the prop signal.Func\(func\(\) T \{ ... \}\) instead \(a catalog.s Live messages are accessors\), or call it in signal.Untrack if once is meant`
		ui.StaticText(msgs.Title()), // want `msgs.Title\(\) is called once, while Language builds its views`
		ui.StaticText(greeting()),   // want `greeting\(\) is called once, while Language builds its views`
		ui.Text(msgs.Live.Title()),
		ui.Text(signal.Func(msgs.Title)),
	)
}

func greeting() string { return "Hello in " + ui.Locale() } // want greeting:"reads the language the app shows"

func direction() ui.Node {
	if ui.IsRTL() { // want `ui.IsRTL\(\) is called once, while direction builds its views, so the branch it chooses won.t follow`
		return ui.StaticText("rtl")
	}
	return ui.StaticText("ltr")
}

// Code that runs right where it is, as part of the component.
var ctx = signal.NewContext(0)

func Provided(name *signal.Signal[string]) ui.Node {
	var n ui.Node
	ctx.Provide(1, func() {
		n = ui.StaticText(name.Get()) // want `name.Get\(\) is read once, while Provided builds its views`
	})
	return n
}

// Not reported.

func Reactive(count *signal.Signal[int], dim *signal.Memo[bool], name signal.Accessor[string]) ui.Node {
	double := signal.NewMemo(func() int { return count.Get() * 2 })
	signal.Effect(func() { log.Println(count.Get()) })
	signal.RenderEffect(func() { _ = double.Get() })
	return ui.Column(ui.StyleFunc(func() ui.Style {
		if dim.Get() {
			return ui.Style{Opacity: 0.5}
		}
		return ui.Style{}
	}),
		ui.Text(signal.Func(func() string { return fmt.Sprint(count.Get()) })),
		ui.Text(name),
		ui.Show(signal.Func(func() bool { return count.Get() > 10 }), func() ui.Node { return ui.StaticText("a lot") }),
		ui.Button(ui.ButtonProps{Title: ui.Val("Add"), OnPress: func() {
			count.Set(count.Get() + 1)
			fmt.Println(name.Get())
		}}),
	)
}

// On purpose: Peek, Untrack, a new signal's first value, a log.
func Once(count *signal.Signal[int], name signal.Accessor[string]) ui.Node {
	draft := signal.New(name.Get())
	start := count.Get()
	from := signal.New(start)
	fmt.Println("mounted with", count.Get())
	log.Printf("mounted with %d", count.Get())
	_, _ = draft, from
	return ui.Column(nil,
		ui.StaticText(fmt.Sprint(count.Peek())),
		ui.StaticText(signal.Untrack(name.Get)),
		ui.StaticText(signal.Untrack(func() string { return name.Get() })),
	)
}

// A value used for something else than the views: an action, a handler.
func Elsewhere(count *signal.Signal[int]) ui.Node {
	save(count.Get())
	n := count.Get()
	return ui.Button(ui.ButtonProps{Title: ui.Val("Save"), OnPress: func() { save(n) }})
}

func save(int) {}

// A function that reads a signal isn't followed: what's used of what it
// returns is often what can't change, as a record's ID.
func Record(id int) ui.Node {
	r := find(id)
	return ui.StaticText(strconv.Itoa(r.id))
}

var records = signal.NewFunc([]item(nil), nil)

func find(id int) item {
	for _, r := range records.Get() {
		if r.id == id {
			return r
		}
	}
	return item{}
}

// The platform never changes while the app runs.
func Platform() ui.Node {
	if ui.UseEnvironment().Get().Platform == ui.PlatformIOS {
		return ui.StaticText("iOS")
	}
	env := ui.UseEnvironment().Get()
	if env.Platform == ui.PlatformAndroid {
		return ui.StaticText("Android")
	}
	return ui.StaticText("other")
}

// A row whose item can't change: a pointer, or an item that's its own key.
func StableRows(todos *signal.Signal[[]*todo], names signal.Accessor[[]string]) ui.Node {
	return ui.Column(nil,
		ui.For(todos, func(t *todo) int { return t.id }, func(item signal.Accessor[*todo], _ signal.Accessor[int]) ui.Node {
			t := item.Get()
			return ui.StaticText(t.title)
		}),
		ui.List(ui.ListProps{}, names, func(s string) string { return s }, func(name signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
			return ui.StaticText(name.Get())
		}),
	)
}

type todo struct {
	id    int
	title string
}

// ui.Dynamic builds again when what it read changes, as do the components
// it alone runs.
func Rebuilt(page *signal.Signal[int]) ui.Node {
	return ui.Column(nil,
		ui.Dynamic(func() ui.Node {
			if page.Get() == 0 {
				return ui.StaticText("first")
			}
			return pageView(page)
		}),
		ui.Dynamic(wholePage),
		markdown.View(markdown.Props{Text: ui.Val("```go\nx\n```"), Code: func(language, code string) ui.Node {
			return ui.StaticText(fmt.Sprint(page.Get()) + code)
		}}),
	)
}

func pageView(page *signal.Signal[int]) ui.Node {
	return ui.StaticText(strconv.Itoa(page.Get()))
}

var current = signal.New("")

func wholePage() ui.Node { return ui.StaticText(current.Get()) }

// What runs later, not as the component builds.
func Later(count *signal.Signal[int]) ui.Node {
	go func() { fmt.Println(count.Get()) }()
	defer func() { fmt.Println(count.Get()) }()
	return ui.StaticText("later")
}

// A package's variables are made before anything is built.
var initial = records.Get()
