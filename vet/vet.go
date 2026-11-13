// Package vet is Asuka's analyzer, which `asuka vet` runs, as do `asuka dev`
// and `asuka build` before they build. Its checks find the mistakes the
// compiler can't, where code that builds and runs does the wrong thing:
//
//   - reads: a signal read while a component builds its tree, which runs
//     once, so what it shows never follows the signal.
//   - mainthread: a signal written from a goroutine, which must reach the
//     main thread with ui.Post.
//   - memowrites: a signal written inside a memo, which panics.
//
// Each reports only what's certain from the code: a check that cries wolf
// gets turned off. Its analyzers run under go vet too:
//
//	go vet -vettool=$(which asuka) ./...
package vet

import (
	"go/ast"
	"go/token"
	"go/types"
	"slices"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/typeutil"
)

// Analyzers are the checks, in the order they report.
var Analyzers = []*analysis.Analyzer{Reads, MainThread, MemoWrites}

const (
	module     = "github.com/egoist/asuka"
	signalPath = module + "/signal"
	uiPath     = module + "/ui"
	i18nPath   = module + "/i18n"
	appPath    = module + "/app"
)

// usesAsuka reports whether pkg is one of Asuka's packages or imports one,
// at any depth: no other package can read or write a signal, so the
// others (the standard library's) are skipped.
func usesAsuka(pkg *types.Package) bool {
	seen := map[*types.Package]bool{}
	var walk func(p *types.Package) bool
	walk = func(p *types.Package) bool {
		if seen[p] {
			return false
		}
		seen[p] = true
		if p.Path() == module || strings.HasPrefix(p.Path(), module+"/") {
			return true
		}
		for _, q := range p.Imports() {
			if walk(q) {
				return true
			}
		}
		return false
	}
	return walk(pkg)
}

// funcName is fn's package path and name, with its receiver's type for a
// method: "github.com/egoist/asuka/ui.Show", ".../signal.Signal.Set".
func funcName(fn *types.Func) string {
	if fn == nil || fn.Pkg() == nil {
		return ""
	}
	fn = fn.Origin()
	sig, _ := fn.Type().(*types.Signature)
	if sig != nil && sig.Recv() != nil {
		t := types.Unalias(sig.Recv().Type())
		if p, ok := t.(*types.Pointer); ok {
			t = types.Unalias(p.Elem())
		}
		if n, ok := t.(*types.Named); ok {
			return fn.Pkg().Path() + "." + n.Obj().Name() + "." + fn.Name()
		}
		if _, ok := t.Underlying().(*types.Interface); ok {
			// An interface's method: its receiver is the interface itself,
			// which may be unnamed in an instantiation.
			return fn.Pkg().Path() + ".?." + fn.Name()
		}
		return ""
	}
	return fn.Pkg().Path() + "." + fn.Name()
}

// shortName is fn as a call names it: "ui.WithDirection", "app.Run".
func shortName(fn *types.Func) string {
	if fn == nil || fn.Pkg() == nil {
		return "it"
	}
	name := funcName(fn)
	path := fn.Pkg().Path()
	return fn.Pkg().Name() + name[len(path):]
}

// callee returns the function or method call calls, if known statically
// (an interface's method included), else nil: a call of a func value, a
// conversion, a builtin.
func callee(info *types.Info, call *ast.CallExpr) *types.Func {
	fn, _ := typeutil.Callee(info, call).(*types.Func)
	return fn
}

// isNode reports whether t is ui.Node or []ui.Node.
func isNode(t types.Type) bool {
	t = types.Unalias(t)
	if s, ok := t.(*types.Slice); ok {
		t = types.Unalias(s.Elem())
	}
	n, ok := t.(*types.Named)
	return ok && n.Obj().Name() == "Node" && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == uiPath
}

// buildsNodes reports whether a function of type sig returns a node (or
// nodes): a component, or what makes part of one.
func buildsNodes(sig *types.Signature) bool {
	if sig == nil {
		return false
	}
	for v := range sig.Results().Variables() {
		if isNode(v.Type()) {
			return true
		}
	}
	return false
}

// isSignalMethod reports whether fn is the method name of signal's type
// typ (Signal, Memo, Context), or of any of its types for typ "".
func isSignalMethod(fn *types.Func, typ, name string) bool {
	if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != signalPath || fn.Name() != name {
		return false
	}
	sig := fn.Type().(*types.Signature)
	if sig.Recv() == nil {
		return false
	}
	if typ == "" {
		return true
	}
	return strings.HasSuffix(funcName(fn), "."+typ+"."+name)
}

// isWrite reports whether fn writes a signal: Signal's Set or Update.
func isWrite(fn *types.Func) bool {
	return isSignalMethod(fn, "Signal", "Set") || isSignalMethod(fn, "Signal", "Update")
}

// unparen returns e without the parentheses around it.
func unparen(e ast.Expr) ast.Expr {
	for {
		p, ok := e.(*ast.ParenExpr)
		if !ok {
			return e
		}
		e = p.X
	}
}

// tree is a package's syntax with every node's parent, and its local
// variables that hold one func literal for good: those are followed
// where they're called, as a function declared is.
type tree struct {
	pass    *analysis.Pass
	parents map[ast.Node]ast.Node
	decls   map[*types.Func]*ast.FuncDecl
	// closures are the local variables assigned a func literal once and
	// never again, nor taken the address of.
	closures map[*types.Var]*ast.FuncLit
	// uses are where each of those variables is used, other than where
	// it's assigned.
	uses map[*types.Var][]*ast.Ident
	// varUses are where every local variable is used.
	varUses map[*types.Var][]*ast.Ident
}

func newTree(pass *analysis.Pass) *tree {
	t := &tree{
		pass:     pass,
		parents:  map[ast.Node]ast.Node{},
		decls:    map[*types.Func]*ast.FuncDecl{},
		closures: map[*types.Var]*ast.FuncLit{},
		uses:     map[*types.Var][]*ast.Ident{},
		varUses:  map[*types.Var][]*ast.Ident{},
	}
	assigned := map[*types.Var]int{}
	bad := map[*types.Var]bool{}
	lit := map[*types.Var]*ast.FuncLit{}
	lhs := map[*ast.Ident]bool{}
	local := func(id *ast.Ident) *types.Var {
		v, _ := pass.TypesInfo.ObjectOf(id).(*types.Var)
		if v == nil || v.IsField() || v.Parent() == nil || v.Parent() == v.Pkg().Scope() {
			return nil
		}
		return v
	}
	assign := func(names []ast.Expr, values []ast.Expr) {
		for i, n := range names {
			id, ok := unparen(n).(*ast.Ident)
			if !ok {
				continue
			}
			lhs[id] = true
			v := local(id)
			if v == nil {
				continue
			}
			if len(values) != len(names) {
				// v, err := f(): not a func literal.
				assigned[v] += 2
				continue
			}
			assigned[v]++
			if fl, ok := unparen(values[i]).(*ast.FuncLit); ok {
				lit[v] = fl
			}
		}
	}
	for _, f := range pass.Files {
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			if len(stack) > 0 {
				t.parents[n] = stack[len(stack)-1]
			}
			stack = append(stack, n)
			switch n := n.(type) {
			case *ast.FuncDecl:
				if fn, ok := pass.TypesInfo.Defs[n.Name].(*types.Func); ok && n.Body != nil {
					t.decls[fn] = n
				}
			case *ast.AssignStmt:
				if n.Tok == token.DEFINE || n.Tok == token.ASSIGN {
					assign(n.Lhs, n.Rhs)
				} else {
					for _, l := range n.Lhs {
						if id, ok := unparen(l).(*ast.Ident); ok {
							lhs[id] = true
							if v := local(id); v != nil {
								bad[v] = true
							}
						}
					}
				}
			case *ast.ValueSpec:
				names := make([]ast.Expr, len(n.Names))
				for i, id := range n.Names {
					names[i] = id
				}
				if len(n.Values) > 0 {
					assign(names, n.Values)
				} else {
					for _, id := range n.Names {
						lhs[id] = true
					}
				}
			case *ast.RangeStmt:
				for _, e := range []ast.Expr{n.Key, n.Value} {
					if id, ok := e.(*ast.Ident); ok {
						if v := local(id); v != nil {
							bad[v] = true
						}
					}
				}
			case *ast.IncDecStmt:
				if id, ok := unparen(n.X).(*ast.Ident); ok {
					if v := local(id); v != nil {
						bad[v] = true
					}
				}
			case *ast.UnaryExpr:
				if n.Op == token.AND {
					if id, ok := unparen(n.X).(*ast.Ident); ok {
						if v := local(id); v != nil {
							bad[v] = true
						}
					}
				}
			}
			return true
		})
	}
	for v, fl := range lit {
		if assigned[v] == 1 && !bad[v] {
			t.closures[v] = fl
		}
	}
	for id, obj := range pass.TypesInfo.Uses {
		v, ok := obj.(*types.Var)
		if !ok || v.IsField() || v.Parent() == nil || v.Parent() == v.Pkg().Scope() || lhs[id] {
			continue
		}
		t.varUses[v] = append(t.varUses[v], id)
		if t.closures[v] != nil {
			t.uses[v] = append(t.uses[v], id)
		}
	}
	byPos := func(a, b *ast.Ident) int { return int(a.Pos() - b.Pos()) }
	for _, ids := range t.varUses {
		slices.SortFunc(ids, byPos)
	}
	for _, ids := range t.uses {
		slices.SortFunc(ids, byPos)
	}
	return t
}

// parent returns n's parent, past any parentheses.
func (t *tree) parent(n ast.Node) ast.Node {
	p := t.parents[n]
	for {
		if _, ok := p.(*ast.ParenExpr); !ok {
			return p
		}
		p = t.parents[p]
	}
}

// enclosing returns the function n is in: an *ast.FuncLit or
// *ast.FuncDecl, or nil at a package's top level.
func (t *tree) enclosing(n ast.Node) ast.Node {
	for p := t.parents[n]; p != nil; p = t.parents[p] {
		switch p.(type) {
		case *ast.FuncLit, *ast.FuncDecl:
			return p
		}
	}
	return nil
}

// siteKind is what's done with a function value.
type siteKind int

const (
	siteOther siteKind = iota
	siteCall           // called right there, or by a go or defer statement
	siteArg            // passed to a function
	siteField          // a field of a composite literal
	siteVar            // held by a local variable (tree.closures)
)

// site is where a function value (a literal, a function's name, a
// variable) is used.
type site struct {
	kind  siteKind
	call  *ast.CallExpr
	stmt  ast.Stmt    // the go or defer statement of a siteCall
	fn    *types.Func // siteArg's callee, nil if not known statically
	arg   int         // siteArg's argument
	field string      // siteField's "pkg.Type.Field"
	v     *types.Var  // siteVar's variable
}

func (t *tree) site(e ast.Expr) site {
	info := t.pass.TypesInfo
	switch p := t.parent(e).(type) {
	case *ast.CallExpr:
		if unparen(p.Fun) == e {
			s := site{kind: siteCall, call: p}
			switch st := t.parent(p).(type) {
			case *ast.GoStmt:
				s.stmt = st
			case *ast.DeferStmt:
				s.stmt = st
			}
			return s
		}
		for i, a := range p.Args {
			if unparen(a) == e {
				return site{kind: siteArg, call: p, fn: callee(info, p), arg: i}
			}
		}
	case *ast.KeyValueExpr:
		if unparen(p.Value) != e {
			break
		}
		cl, ok := t.parent(p).(*ast.CompositeLit)
		key, ok2 := p.Key.(*ast.Ident)
		if !ok || !ok2 {
			break
		}
		if name := structName(info.TypeOf(cl)); name != "" {
			return site{kind: siteField, field: name + "." + key.Name}
		}
	case *ast.CompositeLit:
		st, name := structOf(info.TypeOf(p))
		if st == nil {
			break
		}
		for i, el := range p.Elts {
			if unparen(el) == e && i < st.NumFields() {
				return site{kind: siteField, field: name + "." + st.Field(i).Name()}
			}
		}
	case *ast.AssignStmt:
		for i, r := range p.Rhs {
			if unparen(r) == e && len(p.Lhs) == len(p.Rhs) {
				if id, ok := unparen(p.Lhs[i]).(*ast.Ident); ok {
					if v, ok := info.ObjectOf(id).(*types.Var); ok && t.closures[v] != nil {
						return site{kind: siteVar, v: v}
					}
				}
			}
		}
	case *ast.ValueSpec:
		for i, r := range p.Values {
			if unparen(r) == e && len(p.Names) == len(p.Values) {
				if v, ok := info.Defs[p.Names[i]].(*types.Var); ok && t.closures[v] != nil {
					return site{kind: siteVar, v: v}
				}
			}
		}
	}
	return site{}
}

// structOf returns t's struct type and its name, "pkg.Type", past a
// pointer.
func structOf(t types.Type) (*types.Struct, string) {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	n, ok := t.(*types.Named)
	if !ok || n.Obj().Pkg() == nil {
		return nil, ""
	}
	st, _ := n.Underlying().(*types.Struct)
	return st, n.Obj().Pkg().Path() + "." + n.Obj().Name()
}

func structName(t types.Type) string {
	st, name := structOf(t)
	if st == nil {
		return ""
	}
	return name
}

// calledFunc returns the function a call runs, if it's a function
// declared in this package (with its body) or one of tree.closures: its
// object, and its syntax (*ast.FuncDecl or *ast.FuncLit). For a function
// of another package, it returns its object and nil.
func (t *tree) calledFunc(call *ast.CallExpr) (types.Object, ast.Node) {
	info := t.pass.TypesInfo
	if id, ok := unparen(call.Fun).(*ast.Ident); ok {
		if v, ok := info.Uses[id].(*types.Var); ok {
			if fl := t.closures[v]; fl != nil {
				return v, fl
			}
			return nil, nil
		}
	}
	fn := callee(info, call)
	if fn == nil {
		return nil, nil
	}
	if sig := fn.Type().(*types.Signature); sig.Recv() != nil {
		if _, ok := sig.Recv().Type().Underlying().(*types.Interface); ok {
			return nil, nil // dynamic
		}
	}
	fn = fn.Origin()
	if d := t.decls[fn]; d != nil {
		return fn, d
	}
	return fn, nil
}

// signature returns the type of a function's syntax.
func (t *tree) signature(f ast.Node) *types.Signature {
	switch f := f.(type) {
	case *ast.FuncLit:
		sig, _ := t.pass.TypesInfo.TypeOf(f).(*types.Signature)
		return sig
	case *ast.FuncDecl:
		if fn, ok := t.pass.TypesInfo.Defs[f.Name].(*types.Func); ok {
			return fn.Type().(*types.Signature)
		}
	}
	return nil
}

// objSignature returns the type of a function object or closure variable.
func objSignature(o types.Object) *types.Signature {
	if o == nil {
		return nil
	}
	sig, _ := o.Type().Underlying().(*types.Signature)
	return sig
}

// render shows an expression as the source does, shortened.
func render(e ast.Expr) string {
	s := types.ExprString(e)
	if len(s) > 40 {
		s = s[:37] + "..."
	}
	return s
}
