package vet

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"slices"
	"sort"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Routes reports a link to an href no route of the app's takes, which
// would show the not-found screen: a constant ui.Href (a Navigate's,
// Push's or Replace's, a ViewProps', ButtonProps' or NavLinkProps' Href,
// a RouterProps' Initial, what a Guard returns), a Route's Redirect, and
// ui.HrefOf's pattern, checked against the routes the app declares
// (ui.Route literals, their Children in place or in a variable), in the
// package or in those it imports. A link in a package that sees no routes
// is checked where they're declared, and reported there with its place.
//
// It reports only what's certain: nothing when a route's Path or Children
// can't be known from the code (built by a function, a Path that isn't a
// constant), or the app has a catch-all of its own. And ui.HrefOf given
// as many values as its pattern has parameters.
var Routes = &analysis.Analyzer{
	Name:      "routes",
	Doc:       "report a link to an href no route takes, and ui.HrefOf given the wrong number of values\n\n" + routesDoc,
	URL:       "https://github.com/egoist/asuka/blob/main/docs/navigation.md#typed-links",
	Run:       runRoutes,
	FactTypes: []analysis.Fact{new(RoutesFact)},
}

const routesDoc = `A Router shows its not-found screen for an href no route takes: a link
with a typo, or to a route that moved, does so at run time. The app's
routes are known from its ui.Route literals, so a constant href is checked
against them before the app runs.`

// RoutesFact is what a package declares and uses of routes: the patterns
// of its routes' screens (Open: some can't be known), and the links it
// uses that no routes it sees could check.
type RoutesFact struct {
	Patterns []string
	Open     bool
	Links    []RouteLink
}

// RouteLink is a constant href, where it is.
type RouteLink struct {
	Href    string
	Pos     string
	Pattern bool // a pattern itself (HrefOf's, a Redirect): its parameters match any segment
}

func (*RoutesFact) AFact() {}

func (f *RoutesFact) String() string {
	return fmt.Sprintf("routes %v open %v links %d", f.Patterns, f.Open, len(f.Links))
}

func isUIType(t types.Type, name string) bool {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = types.Unalias(p.Elem())
	}
	n, ok := t.(*types.Named)
	return ok && n.Obj().Name() == name && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == uiPath
}

type routeLit struct {
	lit      *ast.CompositeLit
	children []*ast.CompositeLit
	path     string
	leaf     bool
}

func runRoutes(pass *analysis.Pass) (any, error) {
	if !usesAsuka(pass.Pkg) || pass.Pkg.Path() == uiPath || strings.HasPrefix(pass.Pkg.Path(), module+"/ui/") {
		return nil, nil
	}
	info := pass.TypesInfo
	fact := &RoutesFact{}

	// The package's routes.
	lits := map[*ast.CompositeLit]*routeLit{}
	var order []*ast.CompositeLit
	claimed := map[*ast.CompositeLit]bool{}
	sliceOf := map[types.Object]*ast.CompositeLit{} // a []ui.Route variable's literal
	var routers []ast.Node
	for _, f := range pass.Files {
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.ValueSpec:
				for i, name := range n.Names {
					if i < len(n.Values) {
						if cl, ok := unparen(n.Values[i]).(*ast.CompositeLit); ok && isRouteSlice(info.TypeOf(cl)) {
							sliceOf[info.Defs[name]] = cl
						}
					}
				}
			case *ast.AssignStmt:
				for i, lhs := range n.Lhs {
					id, ok := lhs.(*ast.Ident)
					if !ok || i >= len(n.Rhs) {
						if sel, ok := lhs.(*ast.SelectorExpr); ok && isUIType(info.TypeOf(sel.X), "Route") {
							switch sel.Sel.Name {
							case "Path", "Children", "Redirect":
								fact.Open = true // a route changed after it's made
							}
						}
						continue
					}
					if cl, ok := unparen(n.Rhs[i]).(*ast.CompositeLit); ok && isRouteSlice(info.TypeOf(cl)) {
						if o := info.Defs[id]; o != nil {
							sliceOf[o] = cl
						} else if o := info.Uses[id]; o != nil {
							sliceOf[o] = cl
						}
					}
				}
			case *ast.CallExpr:
				if fn := callee(info, n); fn != nil && fn.Pkg() != nil && fn.Pkg().Path() == uiPath && fn.Name() == "Router" {
					routers = append(routers, n)
				}
			case *ast.CompositeLit:
				if !isUIType(info.TypeOf(n), "Route") {
					return true
				}
				rl := &routeLit{lit: n}
				lits[n] = rl
				order = append(order, n)
				for _, e := range n.Elts {
					kv, ok := e.(*ast.KeyValueExpr)
					if !ok {
						fact.Open = true // positional: unusual, not followed
						continue
					}
					key, _ := kv.Key.(*ast.Ident)
					if key == nil {
						continue
					}
					switch key.Name {
					case "Path":
						tv := info.Types[kv.Value]
						if tv.Value == nil || tv.Value.Kind() != constant.String {
							fact.Open = true
							continue
						}
						rl.path = constant.StringVal(tv.Value)
					case "Screen":
						rl.leaf = true
					case "Redirect":
						rl.leaf = true
					}
				}
			}
			return true
		})
	}
	// Each literal's children: in place, or a variable's literal.
	for _, cl := range order {
		rl := lits[cl]
		for _, e := range cl.Elts {
			kv, ok := e.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			if key, _ := kv.Key.(*ast.Ident); key == nil || key.Name != "Children" {
				continue
			}
			var slice *ast.CompositeLit
			switch v := unparen(kv.Value).(type) {
			case *ast.CompositeLit:
				slice = v
			case *ast.Ident:
				slice = sliceOf[info.Uses[v]]
			}
			if slice == nil {
				fact.Open = true
				continue
			}
			for _, el := range slice.Elts {
				if c, ok := unparen(el).(*ast.CompositeLit); ok && lits[c] != nil {
					rl.children = append(rl.children, c)
					claimed[c] = true
				} else {
					fact.Open = true
				}
			}
		}
	}
	var walk func(cl *ast.CompositeLit, prefix []string)
	walk = func(cl *ast.CompositeLit, prefix []string) {
		rl := lits[cl]
		segs := splitSegs(rl.path)
		if !strings.HasPrefix(rl.path, "/") {
			segs = append(slices.Clone(prefix), segs...)
		}
		if rl.leaf {
			fact.Patterns = append(fact.Patterns, "/"+strings.Join(segs, "/"))
		}
		for _, c := range rl.children {
			walk(c, segs)
		}
	}
	for _, cl := range order {
		if !claimed[cl] {
			walk(cl, nil)
		}
	}

	// The links.
	var links []RouteLink
	at := func(n ast.Node) string { return pass.Fset.Position(n.Pos()).String() }
	linkPos := map[string]token.Pos{}
	visit := func(n, parent ast.Node) bool {
		switch n := n.(type) {
		case *ast.KeyValueExpr:
			// A Route's Redirect: a pattern.
			if key, _ := n.Key.(*ast.Ident); key != nil && key.Name == "Redirect" {
				if cl, ok := parent.(*ast.CompositeLit); ok && lits[cl] != nil {
					if tv := info.Types[n.Value]; tv.Value != nil && tv.Value.Kind() == constant.String {
						s := constant.StringVal(tv.Value)
						links = append(links, RouteLink{Href: s, Pos: at(n.Value), Pattern: true})
						linkPos[at(n.Value)] = n.Value.Pos()
					}
				}
			}
		case ast.Expr:
			// A constant href: a literal given where an Href goes, a
			// conversion, a constant of the type.
			if tv, ok := info.Types[n]; ok && tv.Value != nil && tv.Value.Kind() == constant.String && isUIType(tv.Type, "Href") {
				if pe, ok := parent.(ast.Expr); ok {
					if ptv, ok := info.Types[pe]; ok && ptv.Value != nil && isUIType(ptv.Type, "Href") {
						return true // part of a larger constant
					}
				}
				if s := constant.StringVal(tv.Value); strings.HasPrefix(s, "/") {
					links = append(links, RouteLink{Href: s, Pos: at(n)})
					linkPos[at(n)] = n.Pos()
				}
				return false
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn := callee(info, call)
			if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != uiPath || fn.Name() != "HrefOf" || len(call.Args) == 0 {
				return true
			}
			tv := info.Types[call.Args[0]]
			if tv.Value == nil || tv.Value.Kind() != constant.String {
				return true
			}
			p := constant.StringVal(tv.Value)
			if want := countPatternParams(p); call.Ellipsis == token.NoPos && len(call.Args)-1 != want {
				pass.Reportf(call.Pos(), "ui.HrefOf(%q) given %d values for its %d parameters: it panics", p, len(call.Args)-1, want)
			}
			links = append(links, RouteLink{Href: p, Pos: at(call.Args[0]), Pattern: true})
			linkPos[at(call.Args[0])] = call.Args[0].Pos()
			return true
		}
		return true
	}
	for _, f := range pass.Files {
		var stack []ast.Node
		ast.Inspect(f, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			var parent ast.Node
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			if !visit(n, parent) {
				return false // no f(nil) comes for it
			}
			stack = append(stack, n)
			return true
		})
	}

	// What's visible: the package's routes and its imports'.
	patterns := slices.Clone(fact.Patterns)
	open := fact.Open
	var deps []RouteLink
	for _, pf := range pass.AllPackageFacts() {
		if f, ok := pf.Fact.(*RoutesFact); ok && pf.Package != pass.Pkg {
			patterns = append(patterns, f.Patterns...)
			open = open || f.Open
			deps = append(deps, f.Links...)
		}
	}
	known := len(patterns) > 0 && !open
	for _, p := range patterns {
		if segs := splitSegs(p); len(segs) > 0 && strings.HasPrefix(segs[0], "*") {
			known = false // a catch-all of the app's own takes every href
		}
	}
	sort.Strings(patterns)
	patterns = slices.Compact(patterns)
	if known {
		for _, l := range links {
			if !routeTakes(patterns, l) {
				pass.Reportf(linkPos[l.Pos], "no route takes %q: the Router shows its not-found screen (its routes: %s)", l.Href, listPatterns(patterns))
			}
		}
		// Links of the packages it imports that saw no routes, where the
		// routes are declared.
		if len(fact.Patterns) > 0 {
			report := token.NoPos
			if len(routers) > 0 {
				report = routers[0].Pos()
			} else if len(order) > 0 {
				report = order[0].Pos()
			}
			for _, l := range deps {
				if !routeTakes(patterns, l) {
					pass.Reportf(report, "no route takes %q (at %s): the Router shows its not-found screen (its routes: %s)", l.Href, l.Pos, listPatterns(patterns))
				}
			}
		}
	} else if len(patterns) == 0 {
		fact.Links = links // to be checked where the routes are
	}
	if len(fact.Patterns) > 0 || fact.Open || len(fact.Links) > 0 {
		pass.ExportPackageFact(fact)
	}
	return nil, nil
}

func isRouteSlice(t types.Type) bool {
	s, ok := types.Unalias(t).(*types.Slice)
	return ok && isUIType(s.Elem(), "Route")
}

func splitSegs(p string) []string {
	p, _, _ = strings.Cut(p, "?")
	p, _, _ = strings.Cut(p, "#")
	var out []string
	for s := range strings.SplitSeq(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func countPatternParams(p string) int {
	n := 0
	for _, s := range splitSegs(p) {
		if s[0] == ':' && len(s) > 1 || s[0] == '*' {
			n++
		}
	}
	return n
}

// routeTakes reports whether a route's pattern takes the link: segment by
// segment, a parameter any one, a catch-all the rest; a pattern link's
// own parameters match whatever's there.
func routeTakes(patterns []string, l RouteLink) bool {
	link := splitSegs(l.Href)
	for _, p := range patterns {
		if segsMatch(splitSegs(p), link, l.Pattern) {
			return true
		}
	}
	return false
}

func segsMatch(pat, link []string, wild bool) bool {
	i := 0
	for _, s := range pat {
		switch {
		case s[0] == '*':
			return true
		case i >= len(link):
			return false
		case s[0] == ':' && len(s) > 1:
			i++
		default:
			if l := link[i]; l != s && !(wild && (l[0] == ':' || l[0] == '*')) {
				return false
			}
			i++
		}
	}
	if i < len(link) && wild && link[i][0] == '*' {
		return i == len(link)-1
	}
	return i == len(link)
}

func listPatterns(ps []string) string {
	if len(ps) > 8 {
		return strings.Join(ps[:8], ", ") + ", …"
	}
	return strings.Join(ps, ", ")
}
