package vet

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// MainThread reports a signal written from a goroutine. Signals, as the
// rest of the core, live on the main thread: a goroutine reaches it with
// ui.Post, whose func runs there.
//
// It reports a Set or Update in a func literal run by a go statement (or
// by what runs right there, as signal.Batch), a go statement that runs
// one, and a call there of a function of the package that writes as it
// runs, at any depth. A write in a func literal that runs later, as
// ui.Post's, isn't the goroutine's.
var MainThread = &analysis.Analyzer{
	Name: "mainthread",
	Doc:  "report a signal written from a goroutine, not through ui.Post\n\nSignals are written on the main thread: a goroutine reaches it with ui.Post.",
	URL:  "https://github.com/egoist/ryui/blob/main/docs/concepts.md#the-main-thread",
	Run:  func(pass *analysis.Pass) (any, error) { return runWrites(pass, false) },
}

// MemoWrites reports a signal written inside a memo, which panics: a memo
// derives a value, and an effect is what writes.
var MemoWrites = &analysis.Analyzer{
	Name: "memowrites",
	Doc:  "report a signal written inside a memo, which panics\n\nA memo derives a value; write signals in an effect or an event handler.",
	URL:  "https://github.com/egoist/ryui/blob/main/docs/concepts.md#derived-values",
	Run:  func(pass *analysis.Pass) (any, error) { return runWrites(pass, true) },
}

// runAt are signal's functions that run the func given them right away.
var runAt = map[string]bool{
	signalPath + ".Untrack":         true,
	signalPath + ".Batch":           true,
	signalPath + ".Root":            true,
	signalPath + ".RunWithOwner":    true,
	signalPath + ".Context.Provide": true,
}

type wctx int

const (
	wOpaque wctx = iota
	wTransparent
	wHelper    // runs where it's called
	wGoroutine // a go statement's
	wMemo      // a memo's
)

type writeEvent struct {
	node  ast.Node // the call, or the go statement
	frame ast.Node
	what  string
	via   ast.Node // the helper called, for a call of one
}

type writers struct {
	*tree
	writes map[ast.Node]string // the helpers that write, and the write
}

func runWrites(pass *analysis.Pass, memo bool) (any, error) {
	if !usesRyUI(pass.Pkg) || pass.Pkg.Path() == signalPath {
		return nil, nil
	}
	w := &writers{tree: newTree(pass), writes: map[ast.Node]string{}}
	info := pass.TypesInfo
	var events []writeEvent
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.GoStmt:
				// go s.Set(v), go load(): what runs is the goroutine's.
				if fn := callee(info, n.Call); isWrite(fn) {
					events = append(events, writeEvent{node: n, frame: n, what: render(n.Call.Fun)})
				} else if _, syn := w.calledFunc(n.Call); syn != nil {
					events = append(events, writeEvent{node: n, frame: n, via: syn})
				}
			case *ast.CallExpr:
				if _, ok := w.parent(n).(*ast.GoStmt); ok {
					return true // the go statement's, above
				}
				frame := w.frame(n)
				if frame == nil {
					return true
				}
				if fn := callee(info, n); isWrite(fn) {
					events = append(events, writeEvent{node: n, frame: frame, what: render(n.Fun)})
				} else if _, syn := w.calledFunc(n); syn != nil {
					events = append(events, writeEvent{node: n, frame: frame, via: syn})
				}
				// signal.NewMemo(compute): compute runs in the memo.
				if fn := callee(info, n); memo && isMemo(fn) && len(n.Args) > 0 {
					if obj, ok := info.Uses[identOf(n.Args[0])].(*types.Func); ok {
						if d := w.decls[obj.Origin()]; d != nil {
							events = append(events, writeEvent{node: n.Args[0], frame: n, via: d})
						}
					}
				}
			}
			return true
		})
	}

	for changed := true; changed; {
		changed = false
		for _, e := range events {
			if w.writes[e.frame] != "" || w.kind(e.frame) != wHelper {
				continue
			}
			what := e.what
			if e.via != nil {
				what = w.writes[e.via]
			}
			if what != "" {
				w.writes[e.frame] = what
				changed = true
			}
		}
	}

	for _, e := range events {
		k := w.kind(e.frame)
		if _, ok := e.frame.(*ast.CallExpr); ok {
			k = wMemo // signal.NewMemo(compute)
		}
		if memo && k != wMemo || !memo && k != wGoroutine {
			continue
		}
		var msg string
		if e.via != nil {
			write := w.writes[e.via]
			if write == "" {
				continue
			}
			name := "it"
			switch n := e.node.(type) {
			case *ast.CallExpr:
				name = render(n.Fun)
			case *ast.GoStmt:
				name = render(n.Call.Fun)
			case ast.Expr:
				name = render(n)
			}
			if memo {
				msg = fmt.Sprintf("%s writes a signal (%s) inside a memo, which panics: a memo only derives a value; write in an effect or an event handler", name, write)
			} else {
				msg = fmt.Sprintf("%s writes a signal (%s) on a goroutine, not the main thread: call it in ui.Post(func() { ... })", name, write)
			}
		} else if memo {
			msg = fmt.Sprintf("%s inside a memo panics: a memo only derives a value; write in an effect or an event handler", e.what)
		} else {
			msg = fmt.Sprintf("%s on a goroutine writes a signal off the main thread: write it in ui.Post(func() { ... })", e.what)
		}
		cat := "mainthread"
		if memo {
			cat = "memowrites"
		}
		pass.Report(analysis.Diagnostic{Pos: e.node.Pos(), End: e.node.End(), Category: cat, Message: msg})
	}
	return nil, nil
}

func identOf(e ast.Expr) *ast.Ident {
	switch e := unparen(e).(type) {
	case *ast.Ident:
		return e
	case *ast.SelectorExpr:
		return e.Sel
	}
	return nil
}

func isMemo(fn *types.Func) bool {
	n := funcName(fn)
	return n == signalPath+".NewMemo" || n == signalPath+".NewMemoFunc"
}

// frame returns the function whose run runs n, past those that run right
// where they are, on the same goroutine.
func (w *writers) frame(n ast.Node) ast.Node {
	for f := w.enclosing(n); f != nil; f = w.enclosing(f) {
		if w.kind(f) != wTransparent {
			return f
		}
	}
	return nil
}

func (w *writers) kind(f ast.Node) wctx {
	switch f := f.(type) {
	case *ast.FuncDecl:
		return wHelper
	case *ast.GoStmt:
		return wGoroutine
	case *ast.FuncLit:
		s := w.site(f)
		switch s.kind {
		case siteCall:
			if _, ok := s.stmt.(*ast.GoStmt); ok {
				return wGoroutine
			}
			return wTransparent // now, or deferred: the same goroutine
		case siteArg:
			name := funcName(s.fn)
			if runAt[name] {
				return wTransparent
			}
			if isMemo(s.fn) && s.arg == 0 {
				return wMemo
			}
		case siteVar:
			return wHelper
		}
	}
	return wOpaque
}
