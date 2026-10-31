//go:build darwin && !ios

package darwin

import (
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/egoist/ryui/internal/darwin"
)

// automation starts the debug automation (RYUI_SNAPSHOT: write the
// window as a PNG once it has shown, then quit).
func (h *Host) automation() {
	if os.Getenv("RYUI_PRINT_WINDOW") != "" {
		println("ryui: window", SendInt(h.window, "windowNumber"))
	}
	if path := os.Getenv("RYUI_SNAPSHOT"); path != "" {
		go func() {
			time.Sleep(1500 * time.Millisecond) // images and fonts load
			h.Post(func() {
				WithPool(func() {
					h.snapshot(path)
					if os.Getenv("RYUI_DUMP") != "" {
						os.WriteFile(path+".txt", []byte(h.dump()), 0o644)
					}
				})
				if os.Getenv("RYUI_SNAPSHOT_STAY") == "" {
					os.Exit(0)
				}
			})
		}()
	}
}

// snapshot writes the window's content as a PNG.
func (h *Host) snapshot(path string) {
	v := Send(h.window, "contentView")
	if f := Send(Send(h.window, "contentView"), "superview"); f != 0 && !strings.HasSuffix(path, ".content.png") {
		v = f // the frame view: the title bar and toolbar too
	}
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
	return b.String()
}
