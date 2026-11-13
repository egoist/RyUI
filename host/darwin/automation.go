//go:build darwin && !ios

package darwin

import (
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/egoist/asuka/internal/darwin"
)

// automation starts the debug automation (ASUKA_SNAPSHOT: write the
// window as a PNG once it has shown, then quit).
func (h *Host) automation() {
	if os.Getenv("ASUKA_PRINT_WINDOW") != "" {
		println("asuka: window", SendInt(h.window, "windowNumber"))
	}
	if path := os.Getenv("ASUKA_SNAPSHOT"); path != "" {
		go func() {
			time.Sleep(1500 * time.Millisecond) // images and fonts load
			h.Post(func() {
				WithPool(func() {
					h.snapshot(path)
					if os.Getenv("ASUKA_DUMP") != "" {
						os.WriteFile(path+".txt", []byte(h.dump()+trayDump), 0o644)
					}
				})
				if os.Getenv("ASUKA_SNAPSHOT_STAY") == "" {
					os.Exit(0)
				}
			})
		}()
	}
}

// snapshot writes the window's content as a PNG, and each tray icon's
// beside it (tray.go).
func (h *Host) snapshot(path string) {
	w := h.window
	if !SendBool(w, "isVisible") {
		w = h.keyWin().window // the main window hidden: the one in use
	}
	v := Send(w, "contentView")
	if f := Send(Send(w, "contentView"), "superview"); f != 0 && !strings.HasSuffix(path, ".content.png") {
		v = f // the frame view: the title bar and toolbar too
	}
	writeViewPNG(v, path)
	trayDump = h.traySnapshots(path)
}

// trayDump is what the tray icons show, for the dump.
var trayDump string

// writeViewPNG writes what a view draws as a PNG.
func writeViewPNG(v ID, path string) {
	b := MsgRect(v, Sel("bounds"))
	rep := MsgRectToID(v, Sel("bitmapImageRepForCachingDisplayInRect:"), b)
	MsgRectIDVoid(v, Sel("cacheDisplayInRect:toBitmapImageRep:"), b, rep)
	data := Send(rep, "representationUsingType:properties:", 4, uintptr(Dict())) // PNG
	os.WriteFile(path, Bytes(data), 0o644)
}

// dump describes the native view tree: each node's kind, frame and what
// it shows.
func (h *Host) dump() string {
	var b strings.Builder
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		b.WriteString(strings.Repeat("  ", depth))
		fmt.Fprintf(&b, "%d %s", n.id, n.kind)
		if n.view != 0 {
			f := MsgRect(n.view, Sel("frame"))
			fmt.Fprintf(&b, " [%.0f,%.0f %.0fx%.0f]", f.Origin.X, f.Origin.Y, f.Size.Width, f.Size.Height)
			if SendBool(n.view, "isHidden") {
				b.WriteString(" hidden")
			}
			if Send(n.view, "superview") == 0 {
				b.WriteString(" detached")
			}
			if Send(n.view, "window") == 0 {
				b.WriteString(" nowindow")
			}
			if SendInt(n.view, "userInterfaceLayoutDirection") == 1 {
				b.WriteString(" rtl")
			}
		}
		if n.text != "" {
			fmt.Fprintf(&b, " %q", n.text)
		}
		if n.title != "" {
			fmt.Fprintf(&b, " title=%q", n.title)
		}
		b.WriteByte('\n')
		for _, c := range n.children {
			walk(c, depth+1)
		}
		if n.list != nil {
			for r, rv := range n.list.shown {
				for _, c := range rv.cells {
					b.WriteString(strings.Repeat("  ", depth+1))
					fmt.Fprintf(&b, "row %d:\n", r)
					walk(c.n, depth+2)
				}
			}
		}
	}
	if h.root != nil {
		walk(h.root, 0)
	}
	b.WriteString(h.dumpWindows())
	return b.String()
}

// dumpWindows describes the app's windows that show, front to back: their
// titles, the document's dot and file, and the File menu.
func (h *Host) dumpWindows() string {
	var b strings.Builder
	app := Send(Class("NSApplication"), "sharedApplication")
	for _, nw := range ArrayItems(Send(app, "orderedWindows")) {
		if h.winOf(nw) == nil || !SendBool(nw, "isVisible") {
			continue
		}
		fmt.Fprintf(&b, "window %q", GoString(Send(nw, "title")))
		if SendBool(nw, "isDocumentEdited") {
			b.WriteString(" edited")
		}
		if u := Send(nw, "representedURL"); u != 0 {
			fmt.Fprintf(&b, " file=%q", GoString(Send(u, "path")))
		}
		b.WriteByte('\n')
	}
	bar := Send(app, "mainMenu")
	if file := Send(bar, "itemAtIndex:", 1); file != 0 {
		m := Send(file, "submenu")
		b.WriteString("File:")
		for i := range SendInt(m, "numberOfItems") {
			it := Send(m, "itemAtIndex:", uintptr(i))
			if SendBool(it, "isSeparatorItem") {
				b.WriteString(" |")
				continue
			}
			fmt.Fprintf(&b, " %q", GoString(Send(it, "title")))
			if k := GoString(Send(it, "keyEquivalent")); k != "" {
				fmt.Fprintf(&b, "(%s)", k)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
