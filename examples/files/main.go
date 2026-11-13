// Files shows a folder and keeps it up to date as files appear, change,
// are renamed and go (asuka/watch): the app's own folder at first, which
// New File and swiping a file away change, or, on a desktop, a folder
// chosen with Choose Folder…, which any app may change. What changed
// since the folder opened is listed below it.
//
//	go run ./cmd/asuka dev examples/files
//	go run ./cmd/asuka dev --platform ios examples/files
package main

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/dialog"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/store"
	"github.com/egoist/asuka/ui"
	"github.com/egoist/asuka/watch"
)

type entry struct {
	Name     string
	Dir      bool
	Size     int64
	Modified time.Time
}

// read lists dir, folders first. Not on the main thread.
func read(dir string) ([]entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var list []entry
	for _, de := range des {
		if de.Name()[0] == '.' {
			continue
		}
		e := entry{Name: de.Name(), Dir: de.IsDir()}
		if info, err := de.Info(); err == nil {
			e.Size, e.Modified = info.Size(), info.ModTime()
		}
		list = append(list, e)
	}
	slices.SortFunc(list, func(a, b entry) int {
		if a.Dir != b.Dir {
			if a.Dir {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return list, nil
}

// ownFolder is the app's folder of files, made if it isn't there.
func ownFolder() string {
	dir := filepath.Join(store.Dir(), "Files")
	os.MkdirAll(dir, 0o755)
	return dir
}

func describe(e watch.Event) string {
	switch e.Op {
	case watch.Rename:
		return fmt.Sprintf("Renamed %s to %s", e.OldPath, e.Path)
	case watch.Overflow:
		return "Looked at everything again"
	}
	what := map[watch.Op]string{watch.Create: "Created", watch.Write: "Changed", watch.Remove: "Removed", watch.Attrib: "Permissions of"}[e.Op]
	if e.Path == "." {
		return what + " the folder"
	}
	return what + " " + e.Path
}

func App() ui.Node {
	return ui.Stack(func() ui.Node {
		folder := signal.New(ownFolder())
		entries := signal.NewFunc[[]entry](nil, slices.Equal)
		problem := signal.New("")
		changes := signal.NewFunc[[]string](nil, slices.Equal)
		// reload reads the folder again, off the main thread.
		reload := func() {
			dir := folder.Peek()
			go func() {
				list, err := read(dir)
				ui.Post(func() {
					if folder.Peek() != dir {
						return // another folder since
					}
					entries.Set(list)
					problem.Set("")
					if err != nil {
						problem.Set(err.Error())
					}
				})
			}()
		}
		signal.Effect(func() {
			folder.Get()
			changes.Set(nil)
			reload()
		})
		// The folder's watch, as long as the screen is there: each batch of
		// changes reads it again. Hidden files (an editor's temporary
		// ones) are left out.
		w := watch.Path(folder, watch.Options{Ignore: watch.Patterns(".*")}, func(events []watch.Event) {
			log := slices.Clone(changes.Peek())
			for _, e := range events {
				log = append([]string{describe(e)}, log...)
			}
			changes.Set(log[:min(len(log), 20)])
			reload()
		})
		count := 0
		newFile := func() {
			count++
			p := filepath.Join(folder.Peek(), fmt.Sprintf("Note %d.txt", count))
			go func() {
				err := os.WriteFile(p, []byte(time.Now().Format(time.RFC1123)+"\n"), 0o644)
				if err != nil {
					ui.Post(func() { problem.Set(err.Error()) })
				}
			}()
		}
		chooseFolder := func() {
			dialog.Folder(dialog.FolderOptions{Button: "Show"}, func(paths []string, err error) {
				if err == nil && len(paths) == 1 {
					folder.Set(paths[0])
				}
			})
		}
		status := signal.Func(func() string {
			switch {
			case w.Err() != nil:
				return "Not watched: " + w.Err().Error()
			case problem.Get() != "":
				return problem.Get()
			case !w.Watching():
				return "…"
			}
			if n := len(entries.Get()); n != 1 {
				return fmt.Sprintf("%d items", n)
			}
			return "1 item"
		})
		return ui.SafeArea(ui.Style{Grow: 1},
			ui.Column(ui.Style{Padding: ui.Symmetric(8, 16), Gap: 8},
				ui.Text(folder, ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel, Lines: 2}),
				ui.Row(ui.Style{Gap: 8},
					ui.Button(ui.ButtonProps{Title: ui.Val("New File"), Icon: ui.IconPlus, Variant: ui.ButtonFilled, OnPress: newFile}),
					ui.Button(ui.ButtonProps{Title: ui.Val("Choose Folder…"), Icon: ui.IconFolder, OnPress: chooseFolder}),
				),
				ui.Text(status, ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel}),
			),
			ui.List(ui.ListProps{SwipeActions: func(i int) []ui.Action {
				return []ui.Action{{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() {
					if l := entries.Peek(); i < len(l) {
						p := filepath.Join(folder.Peek(), l[i].Name)
						go os.RemoveAll(p) // the watch shows it gone
					}
				}}}
			}}, entries, func(e entry) string { return e.Name }, func(e signal.Accessor[entry], _ signal.Accessor[int]) ui.Node {
				return ui.Row(ui.Style{Gap: 12, Padding: ui.Symmetric(12, 20), Align: ui.AlignCenter},
					ui.Dynamic(func() ui.Node {
						if e.Get().Dir {
							return ui.Symbol(ui.IconFolder, ui.Style{Color: ui.Accent})
						}
						return ui.Symbol(ui.IconDocument, ui.Style{Color: ui.SecondaryLabel})
					}),
					ui.Text(signal.Func(func() string { return e.Get().Name }), ui.Style{Grow: 1, Lines: 1}),
					ui.Text(signal.Func(func() string {
						if e.Get().Dir {
							return ""
						}
						return fmt.Sprintf("%d B", e.Get().Size)
					}), ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel}),
				)
			}),
			ui.Column(ui.Style{Padding: ui.Symmetric(8, 16), Gap: 4},
				ui.Text(ui.Val("Changes"), ui.Style{Font: ui.Headline}),
				ui.For(changes, func(s string) string { return s }, func(s signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
					return ui.Text(s, ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel, Lines: 1})
				}),
			),
		)
	}, ui.ScreenOptions{Title: ui.Val("Files")})
}

func main() {
	app.RunWindow(ui.WindowOptions{Width: 420, Height: 640, MinWidth: 320, MinHeight: 400}, App)
}
