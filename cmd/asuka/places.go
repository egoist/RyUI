package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// devPlaces reads the app's Go source, as asuka dev builds it, for the
// names of the places where it makes closures and signals, which the run
// knows its kept state by (signal.KeepPlaces): Go names a closure by its
// place among its function's (Screen.func2), so adding one renumbers
// those after it, and the state made under them would start over. Here a
// closure is named by what it is in its function: the variable it's
// assigned to, the field it's for, the call and argument it's passed as,
// a go or defer; and a signal by the variable or field it's made for.
//
// It reads the app's own packages: those of its module (in Asuka's own,
// only those in the app's directory) and of modules replaced by a
// directory. go list says which they are, once, and again only when the
// source imports a package it didn't list; each build parses the files
// that changed.
type devPlaces struct {
	app          *goApp
	goos, goarch string
	pkgs         []placesPkg
	listed       map[string]bool // every import path in the build, as of the last go list
	parsed       map[string]parsedFile
}

type placesPkg struct {
	path, name, dir string
}

type parsedFile struct {
	mod  time.Time
	size int64
	fset *token.FileSet
	file *ast.File
	src  []byte
}

const asukaModule = "github.com/egoist/asuka"

func newDevPlaces(app *goApp, goos, goarch string) *devPlaces {
	if goos == "" {
		goos = build.Default.GOOS
	}
	if goarch == "" {
		goarch = build.Default.GOARCH
	}
	return &devPlaces{app: app, goos: goos, goarch: goarch, parsed: map[string]parsedFile{}}
}

// table is the table for signal.KeepPlaces, from the source as it is.
func (d *devPlaces) table() (string, error) {
	if d.listed == nil {
		if err := d.list(); err != nil {
			return "", err
		}
	}
	t, imports := d.read()
	for _, p := range imports {
		if !d.listed[p] {
			// A package it didn't list: perhaps one of the app's.
			if err := d.list(); err != nil {
				return "", err
			}
			t, _ = d.read()
			break
		}
	}
	return t, nil
}

// list asks go list for the packages in the app's build.
func (d *devPlaces) list() error {
	c := exec.Command("go", "list", "-deps", "-json=ImportPath,Name,Dir,Module,Standard", ".")
	c.Dir = d.app.dir
	c.Env = goEnv(d.app.dir, "GOOS="+d.goos, "GOARCH="+d.goarch, "CGO_ENABLED=0")
	out, err := c.Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("go list: %v: %s", err, strings.TrimSpace(string(e.Stderr)))
		}
		return fmt.Errorf("go list: %v", err)
	}
	d.listed = map[string]bool{}
	d.pkgs = nil
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for dec.More() {
		var p struct {
			ImportPath, Name, Dir string
			Standard              bool
			Module                *struct {
				Path    string
				Main    bool
				Replace *struct{ Path string }
			}
		}
		if err := dec.Decode(&p); err != nil {
			return fmt.Errorf("go list: %v", err)
		}
		d.listed[p.ImportPath] = true
		if p.Standard || p.Module == nil {
			continue
		}
		local := p.Module.Main || p.Module.Replace != nil && modfileLocal(p.Module.Replace.Path)
		if !local || p.Module.Path == asukaModule && !within(p.Dir, d.app.dir) {
			continue // a dependency's, or Asuka's own
		}
		d.pkgs = append(d.pkgs, placesPkg{path: p.ImportPath, name: p.Name, dir: p.Dir})
	}
	return nil
}

// modfileLocal reports whether a replace directive's path is a directory.
func modfileLocal(path string) bool {
	return strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") || filepath.IsAbs(path)
}

// goEnv is the environment of a go command run in dir: this one's, with
// PWD dir, so that the go command knows dir by that path, as the overlays
// and the places' table name its files, not by another that reaches it
// through a symbolic link (macOS's /var is /private/var); then more.
func goEnv(dir string, more ...string) []string {
	return append(append(os.Environ(), "PWD="+dir), more...)
}

func within(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// read writes the table of the packages' files, and returns the import
// paths they name.
func (d *devPlaces) read() (string, []string) {
	ctx := build.Default
	ctx.GOOS, ctx.GOARCH, ctx.CgoEnabled = d.goos, d.goarch, false
	var b strings.Builder
	var imports []string
	seen := map[string]bool{}
	for _, pkg := range d.pkgs {
		bp, err := ctx.ImportDir(pkg.dir, 0)
		if err != nil {
			continue // no Go files for this build, or broken: the build says so
		}
		var files []parsedFile
		var paths []string
		for _, name := range bp.GoFiles { // sorted, as the compiler takes them
			path := filepath.Join(pkg.dir, name)
			f, ok := d.parse(path)
			if !ok {
				continue
			}
			files = append(files, f)
			paths = append(paths, path)
			for _, im := range f.file.Imports {
				if p, err := strconv.Unquote(im.Path.Value); err == nil && !seen[p] {
					seen[p] = true
					imports = append(imports, p)
				}
			}
		}
		writePlaces(&b, pkg, files, paths)
	}
	return b.String(), imports
}

func (d *devPlaces) parse(path string) (parsedFile, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return parsedFile{}, false
	}
	if f, ok := d.parsed[path]; ok && f.mod.Equal(info.ModTime()) && f.size == info.Size() {
		return f, true
	}
	src, err := os.ReadFile(path)
	if err != nil {
		return parsedFile{}, false
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		delete(d.parsed, path)
		return parsedFile{}, false // the build says what's wrong
	}
	f := parsedFile{mod: info.ModTime(), size: info.Size(), fset: fset, file: file, src: src}
	d.parsed[path] = f
	return f, true
}

// writePlaces writes a package's files' facts (signal.KeepPlaces).
func writePlaces(b *strings.Builder, pkg placesPkg, files []parsedFile, paths []string) {
	prefix := runtimePrefix(pkg)
	// Its init functions are numbered across its files, and so are the
	// closures in its variables' initial values, under init.
	inits := 0
	pkgInit := &scope{name: prefix + ".init"}
	for i, f := range files {
		fmt.Fprintf(b, "F\t%s\n", filepath.ToSlash(paths[i]))
		if resets(f.file) {
			sum := sha256.Sum256(f.src)
			fmt.Fprintf(b, "R\t%s\n", hex.EncodeToString(sum[:6]))
		}
		fmt.Fprintf(b, "D\t%s\n", pkgInit.name) // its variables' closures'
		w := &placesWalker{b: b, fset: f.fset, signal: signalNames(f.file)}
		for _, decl := range f.file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Body == nil {
					continue
				}
				name := prefix + "." + declName(decl)
				if decl.Recv == nil && decl.Name.Name == "init" {
					name = fmt.Sprintf("%s.init.%d", prefix, inits)
					inits++
				}
				fmt.Fprintf(b, "D\t%s\n", name)
				w.walk(decl.Body, &scope{name: name})
			case *ast.GenDecl:
				if decl.Tok == token.VAR {
					w.walk(decl, pkgInit)
				}
			}
		}
	}
}

// runtimePrefix is how the runtime names the package's functions: main,
// or its import path with the dots of its last element escaped.
func runtimePrefix(pkg placesPkg) string {
	if pkg.name == "main" {
		return "main"
	}
	i := strings.LastIndexByte(pkg.path, '/') + 1
	return pkg.path[:i] + strings.ReplaceAll(pkg.path[i:], ".", "%2e")
}

// declName is a function's or method's name as the runtime has it:
// F, T.M, (*T).M, generics' with [...].
func declName(d *ast.FuncDecl) string {
	name := d.Name.Name
	if d.Type.TypeParams != nil {
		name += "[...]"
	}
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return name
	}
	t := d.Recv.List[0].Type
	star := false
	if s, ok := t.(*ast.StarExpr); ok {
		star, t = true, s.X
	}
	recv := ""
	switch t := t.(type) {
	case *ast.Ident:
		recv = t.Name
	case *ast.IndexExpr:
		recv = types.ExprString(t.X) + "[...]"
	case *ast.IndexListExpr:
		recv = types.ExprString(t.X) + "[...]"
	}
	if star {
		return "(*" + recv + ")." + name
	}
	return recv + "." + name
}

// resets reports whether the file says //asuka:reset: its signals start
// over when it's edited.
func resets(f *ast.File) bool {
	for _, g := range f.Comments {
		for _, c := range g.List {
			if strings.TrimSpace(c.Text) == "//asuka:reset" {
				return true
			}
		}
	}
	return false
}

// signalNames are the names the file gives package signal ("." when it's
// dot-imported).
func signalNames(f *ast.File) map[string]bool {
	m := map[string]bool{}
	for _, im := range f.Imports {
		if p, _ := strconv.Unquote(im.Path.Value); p == asukaModule+"/signal" {
			if im.Name != nil {
				m[im.Name.Name] = true
			} else {
				m["signal"] = true
			}
		}
	}
	return m
}

// scope is a function, declared or literal, whose literals are being
// named: they're numbered (as the compiler does, for telling apart those
// on one line) and named after it.
type scope struct {
	name  string
	depth int
	n     int            // literals in it so far
	names map[string]int // literals so far by name, for a second's ~2
}

type placesWalker struct {
	b      *strings.Builder
	fset   *token.FileSet
	signal map[string]bool
	stack  []ast.Node
}

// walk names the literals and signals in n, n's own in s.
func (w *placesWalker) walk(n ast.Node, s *scope) {
	ast.Inspect(n, func(n ast.Node) bool {
		if n == nil {
			w.stack = w.stack[:len(w.stack)-1]
			return false
		}
		switch n := n.(type) {
		case *ast.FuncLit:
			s.n++
			what := w.context(n)
			if what == "" {
				what = "lit"
			}
			if s.names == nil {
				s.names = map[string]int{}
			}
			s.names[what]++
			if k := s.names[what]; k > 1 {
				what += "~" + strconv.Itoa(k)
			}
			inner := &scope{name: s.name + ">" + what, depth: s.depth + 1}
			fmt.Fprintf(w.b, "L\t%d\t%d\t%d\t%d\t%s\n", w.line(n.Pos()), w.line(n.End()), inner.depth, s.n, clean(inner.name))
			w.stack = append(w.stack, n)
			w.walk(n.Body, inner)
			w.stack = w.stack[:len(w.stack)-1]
			return false // walked
		case *ast.CallExpr:
			if w.makesSignal(n) {
				if what := w.context(n); what != "" {
					fmt.Fprintf(w.b, "S\t%d\t%d\t%s\n", w.line(n.Pos()), w.line(n.End()), clean(what))
				}
			}
		}
		w.stack = append(w.stack, n)
		return true
	})
}

func (w *placesWalker) line(p token.Pos) int { return w.fset.Position(p).Line }

// makesSignal reports whether call is signal.New or signal.NewFunc.
func (w *placesWalker) makesSignal(call *ast.CallExpr) bool {
	fun := call.Fun
	switch f := fun.(type) {
	case *ast.IndexExpr:
		fun = f.X
	case *ast.IndexListExpr:
		fun = f.X
	}
	var name string
	switch f := fun.(type) {
	case *ast.SelectorExpr:
		x, ok := f.X.(*ast.Ident)
		if !ok || !w.signal[x.Name] {
			return false
		}
		name = f.Sel.Name
	case *ast.Ident:
		if !w.signal["."] {
			return false
		}
		name = f.Name
	default:
		return false
	}
	return name == "New" || name == "NewFunc"
}

// context names what n is for, from the nodes it's in: the variable or
// field it's assigned to, the field of a composite literal, the call
// and argument it's passed as, a go, defer or return ("" for none).
func (w *placesWalker) context(n ast.Node) string {
	child := n
	for i := len(w.stack) - 1; i >= 0; i-- {
		switch p := w.stack[i].(type) {
		case *ast.ParenExpr, *ast.UnaryExpr, *ast.StarExpr:
			child = p
			continue
		case *ast.AssignStmt:
			if j := indexOf(p.Rhs, child); j >= 0 && len(p.Lhs) == len(p.Rhs) {
				return types.ExprString(p.Lhs[j])
			}
			return ""
		case *ast.ValueSpec:
			if j := indexOf(p.Values, child); j >= 0 && j < len(p.Names) {
				return p.Names[j].Name
			}
			return ""
		case *ast.KeyValueExpr:
			if p.Value == child {
				return types.ExprString(p.Key)
			}
			return ""
		case *ast.CompositeLit:
			if j := indexOf(p.Elts, child); j >= 0 {
				return "#" + strconv.Itoa(j)
			}
			return ""
		case *ast.CallExpr:
			if p.Fun == child {
				if i > 0 {
					switch w.stack[i-1].(type) {
					case *ast.GoStmt:
						return "go"
					case *ast.DeferStmt:
						return "defer"
					}
				}
				return "call"
			}
			if j := indexOf(p.Args, child); j >= 0 {
				return types.ExprString(p.Fun) + "#" + strconv.Itoa(j)
			}
			return ""
		case *ast.ReturnStmt:
			if len(p.Results) > 1 {
				return "return#" + strconv.Itoa(indexOf(p.Results, child))
			}
			return "return"
		case *ast.SendStmt:
			return "send"
		}
		return ""
	}
	return ""
}

func indexOf(list []ast.Expr, n ast.Node) int {
	for i, e := range list {
		if ast.Node(e) == n {
			return i
		}
	}
	return -1
}

// clean keeps a name on its line and its field: no tabs or line breaks,
// and not too long (a call's text can be).
func clean(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 120 {
		sum := sha256.Sum256([]byte(s))
		s = s[:100] + "…" + hex.EncodeToString(sum[:4])
	}
	return s
}
