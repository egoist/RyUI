package vet

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Reads reports a signal read while a component builds its tree. A
// component runs once, so a value it reads there is what it shows for
// good: the read belongs in an accessor (signal.Func), a ui.StyleFunc,
// ui.Show or an effect, which run again when what they read changes.
//
// What's built is a function that returns a ui.Node (or []ui.Node), and a
// func literal returning one that's given to what runs it once: ui.Show's
// branches, ui.For's and ui.List's rows, a screen, a tab's or a sheet's
// Content. Not ui.Dynamic's, which builds again when what it read
// changes, nor the components it runs.
//
// A read is a Get of a signal, a memo or an accessor; a call of what says
// the language the app shows (ui.Locale, ui.IsRTL, a catalog's messages,
// and any function, of any package, that calls one); and a call of a
// closure of the component that reads. A function that reads a signal
// isn't followed: what's used of what it returns is often what can't
// change, as a record's ID.
//
// It's reported when what it reads goes into the tree: an argument of
// what builds nodes, a constant (ui.Val), a style, what chooses a branch,
// what a loop makes views for, what's returned; through the expressions
// around the read and the local variables that hold it. Not, as meant: a
// read in an event handler or another func literal that runs later; Peek
// and signal.Untrack; a new signal's first value (signal.New(x.Get()));
// what's logged; what can't change while the app runs (the environment's
// Platform), and a row's item in ui.For or ui.List when it can't change: a
// pointer, or an item that is its own key.
var Reads = &analysis.Analyzer{
	Name:      "reads",
	Doc:       "report a signal read while a component builds its tree, which runs once\n\n" + readsDoc,
	URL:       "https://github.com/egoist/ryui/blob/main/docs/concepts.md#what-updates",
	Run:       runReads,
	FactTypes: []analysis.Fact{new(ReadsFact)},
}

const readsDoc = `A component runs once, so a signal it reads while it builds its tree is
read once: what it shows from it never changes. Read it in signal.Func, a
ui.StyleFunc, ui.Show or an effect, or with Peek (signal.Untrack for an
accessor) when once is meant.`

// ReadsFact marks a function that reads reactive state as it runs, so a
// component that calls it while it builds reads that state once.
type ReadsFact struct {
	What    string // the read, as "count.Get()" or "ui.Locale()"
	Ambient bool   // the language the app shows, not a signal
}

func (*ReadsFact) AFact() {}

func (f *ReadsFact) String() string {
	if f.Ambient {
		return "reads the language the app shows"
	}
	return "reads " + f.What
}

// ctxKind is how a function's body runs, as far as reads go.
type ctxKind int

const (
	ctxOpaque      ctxKind = iota // later, or who knows: an event handler, an effect, an accessor
	ctxTransparent                // right where it is, as part of what's around it
	ctxBuild                      // once, building a tree
	ctxTracked                    // in an effect that builds again: ui.Dynamic
	ctxHelper                     // where it's called: a function, a closure
)

// buildArgs run the func (returning a node) given them once, untracked.
var buildArgs = map[string]bool{
	uiPath + ".Show":                      true,
	uiPath + ".For":                       true,
	uiPath + ".List":                      true,
	uiPath + ".Stack":                     true,
	uiPath + ".Navigator.Push":            true,
	uiPath + ".WithDirection":             true,
	uiPath + ".Embed":                     true,
	uiPath + ".ShareSheetRoot":            true,
	uiPath + ".Mount":                     true,
	appPath + ".Run":                      true,
	appPath + ".RunWindow":                true,
	appPath + ".ShareSheet":               true,
	module + "/host/darwin.Run":           true,
	module + "/host/windows.Run":          true,
	module + "/internal/remote.Run":       true,
	module + "/internal/remote.NewServer": true,
}

// buildFields are likewise, as a composite literal's fields.
var buildFields = map[string]bool{
	uiPath + ".Tab.Content":         true,
	uiPath + ".SheetProps.Content":  true,
	uiPath + ".WindowProps.Content": true,
	uiPath + ".Inline.Build":        true,
}

// trackedArgs and trackedFields run the func given them in an effect,
// which builds again when what it read changes.
var (
	trackedArgs   = map[string]bool{uiPath + ".Dynamic": true}
	trackedFields = map[string]bool{module + "/markdown.Props.Code": true}
)

// fixedFields are what a read reads that can't change while the app
// runs: an environment's platform.
var fixedFields = map[string]bool{
	module + "/ops.Environment.Platform": true,
}

// fieldName is a selected field's "pkg.Type.Field".
func fieldName(sel *types.Selection) string {
	return structName(sel.Recv()) + "." + sel.Obj().Name()
}

// ambientReads say the language the app shows. The ones of package ui
// call signal.Ambient, which is found as any read is; i18n's reach ui's
// through a func value.
var ambientReads = map[string]bool{
	signalPath + ".Ambient":    true,
	i18nPath + ".Locale":       true,
	i18nPath + ".FormatLocale": true,
}

type readEvent struct {
	call  *ast.CallExpr
	frame ast.Node
	what  string
	recv  ast.Expr   // the receiver of a Get, or nil
	peek  bool       // a signal or memo, which has Peek
	amb   bool       // the language the app shows
	via   ast.Node   // the helper's syntax, for a call of one; nil for a read
	fact  *ReadsFact // what a helper of another package reads
}

type readers struct {
	*tree
	ctx     map[*ast.FuncLit]ctxKind
	busy    map[*ast.FuncLit]bool
	exempt  map[ast.Node]bool       // components only ui.Dynamic runs
	reads   map[ast.Node]*ReadsFact // the helpers that read, by syntax
	rowVars map[*types.Var]bool     // rows' items that can't change
}

func runReads(pass *analysis.Pass) (any, error) {
	if !usesRyUI(pass.Pkg) || pass.Pkg.Path() == signalPath {
		return nil, nil
	}
	r := &readers{
		tree:    newTree(pass),
		ctx:     map[*ast.FuncLit]ctxKind{},
		busy:    map[*ast.FuncLit]bool{},
		exempt:  map[ast.Node]bool{},
		reads:   map[ast.Node]*ReadsFact{},
		rowVars: map[*types.Var]bool{},
	}
	info := pass.TypesInfo

	var events []readEvent
	type compCall struct {
		frame ast.Node
		comp  ast.Node
	}
	var compCalls []compCall
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CallExpr:
				r.row(n)
				frame, kind := r.frame(n)
				if kind == ctxOpaque {
					return true // it runs later: what it reads is followed, or meant
				}
				if e, ok := r.read(n); ok {
					e.frame = frame
					events = append(events, e)
					return true
				}
				obj, syn := r.calledFunc(n)
				if obj == nil {
					return true
				}
				sig := objSignature(obj)
				if buildsNodes(sig) {
					if syn != nil {
						compCalls = append(compCalls, compCall{frame, syn})
					}
					return true
				}
				if syn != nil {
					events = append(events, readEvent{call: n, frame: frame, via: syn})
					return true
				}
				var fact ReadsFact
				if fn, ok := obj.(*types.Func); ok && pass.ImportObjectFact(fn, &fact) {
					events = append(events, readEvent{call: n, frame: frame, fact: &fact, amb: fact.Ambient})
				}
			case *ast.Ident, *ast.SelectorExpr:
				// A component given to ui.Dynamic as a value.
				var id *ast.Ident
				if s, ok := n.(*ast.SelectorExpr); ok {
					id = s.Sel
				} else {
					id = n.(*ast.Ident)
				}
				fn, ok := info.Uses[id].(*types.Func)
				if !ok {
					return true
				}
				d := r.decls[fn.Origin()]
				if d == nil || !buildsNodes(fn.Type().(*types.Signature)) {
					return true
				}
				if e, ok := n.(ast.Expr); ok && r.trackedSite(r.site(e)) {
					r.exempt[d] = true
				}
			}
			return true
		})
	}

	// The components ui.Dynamic runs, and those they call, are built again
	// when what they read changes: their reads aren't reported.
	for changed := true; changed; {
		changed = false
		for _, c := range compCalls {
			if r.exempt[c.comp] {
				continue
			}
			_, ok := c.frame.(*ast.FuncLit)
			if r.exempt[c.frame] || (ok && r.litCtx(c.frame.(*ast.FuncLit)) == ctxTracked) {
				r.exempt[c.comp] = true
				changed = true
			}
		}
	}

	// The helpers that read, at any depth within the package: a closure
	// that reads anything, a function that says the language the app
	// shows. (A function that reads a signal is often only used for
	// something that can't change, as a record's ID: following it would
	// report what isn't wrong.)
	for changed := true; changed; {
		changed = false
		for _, e := range events {
			if r.reads[e.frame] != nil || r.kind(e.frame) != ctxHelper {
				continue
			}
			var f *ReadsFact
			switch {
			case e.via != nil:
				f = r.reads[e.via]
			case e.fact != nil:
				f = e.fact
			default:
				f = &ReadsFact{What: e.what, Ambient: e.amb}
			}
			if _, closure := e.frame.(*ast.FuncLit); f != nil && (f.Ambient || closure) {
				r.reads[e.frame] = f
				changed = true
			}
		}
	}
	for syn, f := range r.reads {
		if d, ok := syn.(*ast.FuncDecl); ok {
			if fn, ok := info.Defs[d.Name].(*types.Func); ok {
				pass.ExportObjectFact(fn, f)
			}
		}
	}

	for _, e := range events {
		if r.kind(e.frame) != ctxBuild || r.exempt[e.frame] {
			continue
		}
		if e.via != nil {
			f := r.reads[e.via]
			if f == nil {
				continue
			}
			e.fact, e.amb = f, f.Ambient
		}
		if e.via == nil && e.fact == nil && e.recv != nil && r.rowItem(e.recv) {
			continue
		}
		hint, ok := r.reaches(e.call, e.frame, map[*types.Var]bool{})
		if !ok {
			continue
		}
		pass.Report(analysis.Diagnostic{Pos: e.call.Pos(), End: e.call.End(), Category: "reads", Message: r.message(e, hint)})
	}
	return nil, nil
}

// read reports whether call reads a signal, a memo, an accessor or the
// language the app shows.
func (r *readers) read(call *ast.CallExpr) (readEvent, bool) {
	fn := callee(r.pass.TypesInfo, call)
	if fn == nil {
		return readEvent{}, false
	}
	if ambientReads[funcName(fn)] {
		return readEvent{call: call, what: render(call.Fun) + "()", amb: true}, true
	}
	if !isSignalMethod(fn, "", "Get") {
		return readEvent{}, false
	}
	sel, ok := unparen(call.Fun).(*ast.SelectorExpr)
	if !ok {
		return readEvent{}, false
	}
	name := funcName(fn)
	peek := strings.HasSuffix(name, ".Signal.Get") || strings.HasSuffix(name, ".Memo.Get")
	return readEvent{call: call, what: render(call.Fun) + "()", recv: sel.X, peek: peek}, true
}

// frame returns the function whose run runs n, past those that run right
// where they are, and how it runs.
func (r *readers) frame(n ast.Node) (ast.Node, ctxKind) {
	for f := r.enclosing(n); f != nil; f = r.enclosing(f) {
		k := r.kind(f)
		if k != ctxTransparent {
			return f, k
		}
	}
	return nil, ctxOpaque
}

func (r *readers) kind(f ast.Node) ctxKind {
	switch f := f.(type) {
	case *ast.FuncDecl:
		if buildsNodes(r.signature(f)) {
			return ctxBuild
		}
		return ctxHelper
	case *ast.FuncLit:
		return r.litCtx(f)
	}
	return ctxOpaque
}

// litCtx says how a func literal's body runs, from where it's used.
func (r *readers) litCtx(fl *ast.FuncLit) ctxKind {
	if k, ok := r.ctx[fl]; ok {
		return k
	}
	if r.busy[fl] {
		return ctxOpaque
	}
	r.busy[fl] = true
	k := r.litCtxOf(fl)
	delete(r.busy, fl)
	r.ctx[fl] = k
	return k
}

func (r *readers) litCtxOf(fl *ast.FuncLit) ctxKind {
	nodes := buildsNodes(r.signature(fl))
	s := r.site(fl)
	switch s.kind {
	case siteCall:
		if s.stmt == nil {
			return ctxTransparent
		}
		return ctxOpaque
	case siteArg:
		if isSignalMethod(s.fn, "Context", "Provide") {
			return ctxTransparent
		}
		if nodes {
			return r.nodeSite(s)
		}
	case siteField:
		if nodes {
			return r.nodeSite(s)
		}
	case siteVar:
		if !nodes {
			return ctxHelper
		}
		// A closure that builds: as its uses run it, if they all agree.
		k := ctxOpaque
		for i, id := range r.uses[s.v] {
			us := r.site(id)
			uk := ctxOpaque
			if us.kind == siteCall && us.stmt == nil {
				if _, fk := r.frame(us.call); fk == ctxTracked {
					uk = ctxTracked
				} else {
					uk = ctxBuild
				}
			} else {
				uk = r.nodeSite(us)
			}
			if i == 0 {
				k = uk
			} else if uk != k {
				return ctxOpaque
			}
		}
		return k
	}
	return ctxOpaque
}

// nodeSite says how what's given a func returning a node runs it.
func (r *readers) nodeSite(s site) ctxKind {
	switch {
	case r.trackedSite(s):
		return ctxTracked
	case s.kind == siteArg && s.fn != nil && buildArgs[funcName(s.fn)]:
		return ctxBuild
	case s.kind == siteField && buildFields[s.field]:
		return ctxBuild
	}
	return ctxOpaque
}

func (r *readers) trackedSite(s site) bool {
	switch s.kind {
	case siteArg:
		return s.fn != nil && trackedArgs[funcName(s.fn)]
	case siteField:
		return trackedFields[s.field]
	}
	return false
}

// row notes the item of a ui.For or ui.List row that can't change: the
// row lives as long as its key, and a pointer stays the same pointer, as
// does an item that is its own key.
func (r *readers) row(call *ast.CallExpr) {
	fn := callee(r.pass.TypesInfo, call)
	if fn == nil {
		return
	}
	name := funcName(fn)
	var keyArg int
	switch name {
	case uiPath + ".For":
		keyArg = 1
	case uiPath + ".List":
		keyArg = 2
	default:
		return
	}
	if len(call.Args) <= keyArg+1 {
		return
	}
	row, ok := unparen(call.Args[keyArg+1]).(*ast.FuncLit)
	if !ok || len(row.Type.Params.List) == 0 || len(row.Type.Params.List[0].Names) == 0 {
		return
	}
	item, ok := r.pass.TypesInfo.Defs[row.Type.Params.List[0].Names[0]].(*types.Var)
	if !ok {
		return
	}
	sig := r.pass.TypesInfo.TypeOf(row).(*types.Signature)
	acc, ok := sig.Params().At(0).Type().(*types.Named)
	if !ok || acc.TypeArgs().Len() != 1 {
		return
	}
	switch acc.TypeArgs().At(0).Underlying().(type) {
	case *types.Pointer:
		r.rowVars[item] = true
		return
	}
	if identityKey(r.pass.TypesInfo, call.Args[keyArg]) {
		r.rowVars[item] = true
	}
}

// identityKey reports whether key is func(x T) T { return x }.
func identityKey(info *types.Info, key ast.Expr) bool {
	fl, ok := unparen(key).(*ast.FuncLit)
	if !ok || len(fl.Type.Params.List) != 1 || len(fl.Type.Params.List[0].Names) != 1 || len(fl.Body.List) != 1 {
		return false
	}
	ret, ok := fl.Body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	id, ok := unparen(ret.Results[0]).(*ast.Ident)
	return ok && info.Uses[id] != nil && info.Uses[id] == info.Defs[fl.Type.Params.List[0].Names[0]]
}

func (r *readers) rowItem(recv ast.Expr) bool {
	id, ok := unparen(recv).(*ast.Ident)
	if !ok {
		return false
	}
	v, ok := r.pass.TypesInfo.Uses[id].(*types.Var)
	return ok && r.rowVars[v]
}

type hintKind int

const (
	hintAny    hintKind = iota
	hintConst           // in ui.Val, signal.Static, ui.StaticText
	hintStyle           // in a ui.Style
	hintBranch          // what an if or a switch chooses by
	hintRange           // what a loop makes views for
)

// reaches reports whether the value of e, an expression in frame, goes
// into the tree frame builds: an argument of what builds nodes, a
// constant prop, a style, what chooses a branch or how many views a loop
// makes, what frame returns; through the expressions around e and the
// local variables it's stored in. It's false for a value only given a
// new signal as its first value (signal.New), logged, read in
// signal.Untrack or used for something else (an action, an event
// handler's closure).
func (r *readers) reaches(e ast.Expr, frame ast.Node, seen map[*types.Var]bool) (hintKind, bool) {
	info := r.pass.TypesInfo
	var child ast.Node = e
	for p := r.parents[e]; p != nil && p != frame; child, p = p, r.parents[p] {
		switch p := p.(type) {
		case *ast.SelectorExpr:
			if sel := info.Selections[p]; sel != nil && sel.Kind() == types.FieldVal && fixedFields[fieldName(sel)] {
				return 0, false // what can't change while the app runs
			}
			continue
		case *ast.ParenExpr, *ast.IndexExpr, *ast.IndexListExpr, *ast.SliceExpr,
			*ast.StarExpr, *ast.UnaryExpr, *ast.BinaryExpr, *ast.KeyValueExpr, *ast.TypeAssertExpr:
			continue
		case *ast.CallExpr:
			if child == p.Fun {
				continue // a method of what's read: its result carries it
			}
			fn := callee(info, p)
			switch funcName(fn) {
			case signalPath + ".New", signalPath + ".NewFunc", signalPath + ".Untrack":
				return 0, false
			case uiPath + ".Val", signalPath + ".Static", uiPath + ".StaticText":
				return hintConst, true
			}
			if fn != nil && fn.Pkg() != nil {
				switch fn.Pkg().Path() {
				case "fmt":
					if strings.HasPrefix(fn.Name(), "Print") || strings.HasPrefix(fn.Name(), "Fprint") {
						return 0, false
					}
				case "log", "log/slog":
					return 0, false
				}
			}
			if sig, ok := info.TypeOf(p.Fun).(*types.Signature); ok && buildsNodes(sig) {
				return hintAny, true
			}
			continue // what it returns may carry it, as a conversion does
		case *ast.CompositeLit:
			if structName(info.TypeOf(p)) == uiPath+".Style" {
				return hintStyle, true
			}
			continue
		case *ast.IfStmt:
			if child == p.Cond {
				return hintBranch, true
			}
			return 0, false
		case *ast.SwitchStmt:
			if child == p.Tag {
				return hintBranch, true
			}
			return 0, false
		case *ast.CaseClause:
			for _, x := range p.List {
				if child == x {
					return hintBranch, true
				}
			}
			return 0, false
		case *ast.ForStmt:
			if child == p.Cond {
				return hintRange, true
			}
			return 0, false
		case *ast.RangeStmt:
			if child == p.X {
				return hintRange, true
			}
			return 0, false
		case *ast.ReturnStmt:
			return hintAny, true
		case *ast.AssignStmt:
			if p.Tok != token.DEFINE && p.Tok != token.ASSIGN {
				return 0, false
			}
			var lhs []ast.Expr
			if len(p.Lhs) == len(p.Rhs) {
				for i, x := range p.Rhs {
					if x == child {
						lhs = p.Lhs[i : i+1]
					}
				}
			} else {
				lhs = p.Lhs // a, ok := f()
			}
			return r.reachesVars(lhs, frame, seen)
		case *ast.ValueSpec:
			var lhs []ast.Expr
			for i, id := range p.Names {
				if len(p.Values) != len(p.Names) || p.Values[i] == child {
					lhs = append(lhs, id)
				}
			}
			return r.reachesVars(lhs, frame, seen)
		default:
			return 0, false
		}
	}
	return 0, false
}

// reachesVars reports whether what's stored in the local variables lhs
// reaches the tree, where frame itself uses them (not a closure in it:
// an event handler's, an accessor's).
func (r *readers) reachesVars(lhs []ast.Expr, frame ast.Node, seen map[*types.Var]bool) (hintKind, bool) {
	for _, x := range lhs {
		id, ok := unparen(x).(*ast.Ident)
		if !ok {
			continue
		}
		v, ok := r.pass.TypesInfo.ObjectOf(id).(*types.Var)
		if !ok || seen[v] || v.Parent() == nil || v.Parent() == v.Pkg().Scope() {
			continue
		}
		seen[v] = true
		for _, u := range r.varUses[v] {
			if f, _ := r.frame(u); f != frame {
				continue
			}
			if h, ok := r.reaches(u, frame, seen); ok {
				return h, true
			}
		}
	}
	return 0, false
}

// builder names what's built in frame, for a message.
func (r *readers) builder(frame ast.Node) string {
	switch f := frame.(type) {
	case *ast.FuncDecl:
		return f.Name.Name
	case *ast.FuncLit:
		s := r.site(f)
		switch s.kind {
		case siteArg:
			switch funcName(s.fn) {
			case uiPath + ".Show":
				return "this ui.Show branch"
			case uiPath + ".For":
				return "this ui.For row"
			case uiPath + ".List":
				return "this ui.List row"
			case uiPath + ".Stack", uiPath + ".Navigator.Push":
				return "this screen"
			}
			return "the func given " + shortName(s.fn)
		case siteField:
			return "this " + s.field[strings.LastIndex(s.field[:strings.LastIndex(s.field, ".")], ".")+1:]
		case siteVar:
			return s.v.Name()
		}
	}
	return "this func"
}

func (r *readers) message(e readEvent, h hintKind) string {
	who := r.builder(e.frame)
	helper := e.via != nil || e.fact != nil
	var read, once string
	switch {
	case e.amb:
		name := e.what
		if helper {
			name = render(e.call.Fun) + "()"
		}
		read = fmt.Sprintf("%s is called once, while %s builds its views", name, who)
		once = "or call it in signal.Untrack if once is meant"
	case helper:
		name := render(e.call.Fun) + "()"
		read = fmt.Sprintf("%s reads %s, once, while %s builds its views", name, e.fact.What, who)
		once = "or call it in signal.Untrack to read it once on purpose"
	case e.peek:
		read = fmt.Sprintf("%s is read once, while %s builds its views", e.what, who)
		once = fmt.Sprintf("or use %s.Peek() to read it once on purpose", render(e.recv))
	default:
		read = fmt.Sprintf("%s is read once, while %s builds its views", e.what, who)
		once = fmt.Sprintf("or signal.Untrack(%s.Get) to read it once on purpose", render(e.recv))
	}
	stays, stay := "never changes", "never change"
	if e.amb {
		stays, stay = "won't follow the language the app shows", stays
	}
	switch h {
	case hintConst:
		fix := "give the prop signal.Func(func() T { ... }) instead"
		if e.amb {
			fix += " (a catalog's Live messages are accessors)"
		}
		return fmt.Sprintf("%s, so the constant made from it %s: %s, %s", read, stays, fix, once)
	case hintStyle:
		return fmt.Sprintf("%s, so the style made from it %s: make the style a ui.StyleFunc, %s", read, stays, once)
	case hintBranch:
		if e.amb {
			return fmt.Sprintf("%s, so the branch it chooses %s: choose it with ui.Show, %s", read, stays, once)
		}
		return fmt.Sprintf("%s, so the branch is chosen once: choose it with ui.Show, %s", read, once)
	case hintRange:
		if e.amb {
			stay = stays
		}
		return fmt.Sprintf("%s, so the views made for it %s: repeat them with ui.For, %s", read, stay, once)
	}
	if !e.amb {
		stays = "and never again"
	} else {
		stays = "so it " + stays
	}
	return fmt.Sprintf("%s, %s: read it in signal.Func, a ui.StyleFunc or an effect, %s", read, stays, once)
}
