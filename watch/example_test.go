package watch_test

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/egoist/asuka/watch"
)

// readNames lists dir's names. Not on the main thread.
func readNames(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// A folder's names, read again as it changes.
func ExamplePath() {
	_ = func(dir signal.Accessor[string]) ui.Node {
		names := signal.NewFunc[[]string](nil, slices.Equal)
		reload := func() {
			d := signal.Untrack(dir.Get)
			go func() {
				list := readNames(d) // off the main thread
				ui.Post(func() { names.Set(list) })
			}()
		}
		signal.Effect(func() { dir.Get(); reload() }) // and as dir changes
		w := watch.Path(dir, watch.Options{Ignore: watch.Patterns(".*")}, func([]watch.Event) {
			reload()
		})
		return ui.Column(nil,
			ui.Show(signal.Func(func() bool { return w.Err() != nil }), func() ui.Node {
				return ui.Text(signal.Func(func() string { return w.Err().Error() }))
			}),
			ui.For(names, func(n string) string { return n }, func(n signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
				return ui.Text(n)
			}),
		)
	}
}

// A tree followed from a goroutine.
func ExampleNew() {
	follow := func(ctx context.Context, dir string) error {
		w, err := watch.New(ctx, dir, watch.Options{Recursive: true, Ignore: watch.Patterns("node_modules/", ".git/")})
		if err != nil {
			return err
		}
		defer w.Close()
		for {
			events, err := w.Next(ctx)
			if err != nil {
				return err // io.EOF once closed, or once dir went away
			}
			for _, e := range events {
				fmt.Println(e) // "create src/a.go", "rename a.txt -> b.txt", ...
			}
		}
	}
	_ = follow
}
