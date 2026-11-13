package native

import "runtime"

// Typed native views (//asuka:view): the generated constructor shows one
// with ui.Native, of the kind ViewKind says, its props each a []byte of
// its values; each host finds the kind's view in the package's native
// code (its Swift, Kotlin or JavaScript side's makeView), or, where the
// package implements it in Go, in RegisterView's registry (GoView).

// ViewKind is the kind of a package's view, as this platform's host
// finds it: the module's Swift class on iOS and macOS, its Kotlin class
// on Android, its import path elsewhere; then # and the view's name.
func ViewKind(m *Module, view string) string {
	switch runtime.GOOS {
	case "ios", "darwin":
		return m.Swift + "#" + view
	case "android":
		return m.Kotlin + "#" + view
	}
	return m.ID + "#" + view
}

// GoView makes a typed view Go implements a View for the hosts: its
// handle is v's, set sets a prop from its encoded value, and v's
// Measure, Destroy and ParentMessage (if it has them) are the view's.
func GoView(v interface{ Handle() uintptr }, set func(name string, value []byte)) View {
	g := goView{v: v, set: set}
	if m, ok := v.(Measurer); ok {
		return &measuredGoView{g, m}
	}
	return &g
}

type goView struct {
	v   interface{ Handle() uintptr }
	set func(string, []byte)
}

func (g *goView) Handle() uintptr { return g.v.Handle() }

func (g *goView) Set(name string, value any) {
	b, _ := value.([]byte)
	g.set(name, b)
}

func (g *goView) Destroy() {
	if d, ok := g.v.(Destroyer); ok {
		d.Destroy()
	}
}

func (g *goView) ParentMessage(msg uint32, wparam, lparam uintptr) (uintptr, bool) {
	if p, ok := g.v.(ParentMessenger); ok {
		return p.ParentMessage(msg, wparam, lparam)
	}
	return 0, false
}

type measuredGoView struct {
	goView
	m Measurer
}

func (g *measuredGoView) Measure(width, height float32) (float32, float32) {
	return g.m.Measure(width, height)
}
