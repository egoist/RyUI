// Package nativegen reads a Go package's typed native declarations
// (//asuka:native interfaces, //asuka:view props) and writes the code
// that connects them to each platform: the package's Go client
// (<pkg>_native.go) and the Swift, Kotlin and JavaScript glue its
// implementations are compiled with. One model makes them all, so the
// sides always agree on the wire (package native's format).
package nativegen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Kind is a type's kind on the wire.
type Kind int

const (
	Bool Kind = iota
	Int8
	Int16
	Int32
	Int64
	Int // Go's int: 8 bytes
	Uint8
	Uint16
	Uint32
	Uint64
	Uint
	Float32
	Float64
	String
	Bytes
	Time
	Duration
	Slice
	Map // map[string]T
	Optional
	Struct
	Enum
	Object // a native interface of the package
	Func
)

// Type is a parameter's, a result's or a field's type.
type Type struct {
	Kind Kind
	Elem *Type     // Slice, Map, Optional
	Name string    // Struct, Enum, Object: the Go type's name
	Func *FuncType // Func
	// Go is the type as the package's Go writes it.
	Go string
}

// FuncType is a callback: its parameters, and its result (a question)
// or none (a notification).
type FuncType struct {
	Params []*Param
	Result *Type
}

// Param is a parameter.
type Param struct {
	Name string
	Type *Type
}

// Method is a native interface's method.
type Method struct {
	Name       string
	Index      uint32 // from 1, in declaration order
	Context    bool   // takes a context.Context first: asynchronous
	Params     []*Param
	Result     *Type // nil: none
	Error      bool  // returns an error
	MainThread bool
	Close      bool // Close() error: releases the object after
	Doc        string
}

// Interface is a //asuka:native interface.
type Interface struct {
	Name       string
	Index      uint32 // from 0, in declaration order
	Methods    []*Method
	MainThread bool
	Doc        string
}

// Field is a struct's field, or a view's prop or event.
type Field struct {
	Name string // Go's
	Type *Type
	// Accessor: a view's prop given as a signal.Accessor of Type.
	Accessor bool
	Doc      string
}

// StructDecl is a struct of the package that crosses.
type StructDecl struct {
	Name   string
	Fields []*Field
	Doc    string
}

// EnumValue is one of an enum's constants.
type EnumValue struct {
	Name  string // Go's constant
	Value string // its value, as Go writes it (a quoted string, or a number)
}

// EnumDecl is a named type of the package over a string or an integer, with
// its constants.
type EnumDecl struct {
	Name   string
	Basic  Kind // String or an integer kind
	Values []EnumValue
	Doc    string
}

// ErrorDecl is a declared error: var ErrX = native.NewError("...").
type ErrorDecl struct {
	Name    string // ErrLowPower
	Message string
	Index   uint32
}

// View is a //asuka:view props struct.
type View struct {
	Name   string // the view's: the struct's name without Props
	Props  []*Field
	Events []*Field // func fields: Type.Func
	Style  bool     // has a Style ui.Style (or ui.Styler: a StyleFunc too) field
	Doc    string
}

// Package is a package's typed native code.
type Package struct {
	Path, Name, Dir string
	Interfaces      []*Interface
	Views           []*View
	Structs         []*StructDecl
	Enums           []*EnumDecl
	Errors          []*ErrorDecl
	// Hash is the declarations', which every generated file records.
	Hash string
}

// Empty reports whether the package declares nothing native.
func (p *Package) Empty() bool { return len(p.Interfaces) == 0 && len(p.Views) == 0 }

// Error is a declaration Asuka can't connect, at its position.
type Error struct {
	Pos token.Position
	Msg string
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Pos, e.Msg) }

// GoFile is the generated Go file's name for package name.
func GoFile(name string) string { return name + "_native.go" }

// Load reads the native declarations of the package in dir (nil when it
// has none).
func Load(dir string) (*Package, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo,
		Dir:  abs,
		// The generated file is what's being written: read without it (an
		// old one may not compile against the declarations now).
		ParseFile: nil,
	}
	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return nil, err
	}
	if len(pkgs) != 1 {
		return nil, fmt.Errorf("%s: not one package", dir)
	}
	lp := pkgs[0]
	if lp.Types == nil {
		return nil, fmt.Errorf("%s: %v", dir, lp.Errors)
	}
	gen := GoFile(lp.Name)
	if hasGenerated(lp, gen) {
		// Load again with the generated file empty.
		cfg.Overlay = map[string][]byte{filepath.Join(abs, gen): []byte("package " + lp.Name + "\n")}
		if pkgs, err = packages.Load(cfg, "."); err != nil {
			return nil, err
		}
		lp = pkgs[0]
	}
	l := &loader{pkg: lp, fset: lp.Fset, out: &Package{Path: lp.PkgPath, Name: lp.Name, Dir: abs},
		structs: map[string]*StructDecl{}, enums: map[string]*EnumDecl{}, ifaces: map[string]*Interface{}}
	if err := l.load(); err != nil {
		return nil, err
	}
	if l.out.Empty() {
		return nil, nil
	}
	l.out.Hash = l.out.hash()
	return l.out, nil
}

func hasGenerated(p *packages.Package, name string) bool {
	for _, f := range p.CompiledGoFiles {
		if filepath.Base(f) == name {
			return true
		}
	}
	return false
}

type loader struct {
	pkg     *packages.Package
	fset    *token.FileSet
	out     *Package
	structs map[string]*StructDecl
	enums   map[string]*EnumDecl
	ifaces  map[string]*Interface
	native  map[string]bool // the //asuka:native interfaces' names
}

func (l *loader) errorf(pos token.Pos, format string, args ...any) error {
	return &Error{Pos: l.fset.Position(pos), Msg: fmt.Sprintf(format, args...)}
}

// directive reports whether a doc comment has //asuka:<name>.
func directive(docs []*ast.CommentGroup, name string) bool {
	for _, d := range docs {
		if d == nil {
			continue
		}
		for _, c := range d.List {
			if strings.TrimSpace(c.Text) == "//asuka:"+name {
				return true
			}
		}
	}
	return false
}

// docText is a doc comment's text without directives.
func docText(docs ...*ast.CommentGroup) string {
	for _, d := range docs {
		if d == nil {
			continue
		}
		var lines []string
		for _, l := range strings.Split(d.Text(), "\n") {
			if !strings.HasPrefix(l, "asuka:") {
				lines = append(lines, l)
			}
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return ""
}

type decl struct {
	spec *ast.TypeSpec
	doc  []*ast.CommentGroup
}

func (l *loader) load() error {
	var natives, views []decl
	l.native = map[string]bool{}
	files := slices.Clone(l.pkg.Syntax)
	sort.Slice(files, func(i, j int) bool {
		return l.fset.Position(files[i].Pos()).Filename < l.fset.Position(files[j].Pos()).Filename
	})
	for _, f := range files {
		if filepath.Base(l.fset.Position(f.Pos()).Filename) == GoFile(l.pkg.Name) {
			continue
		}
		for _, d := range f.Decls {
			switch gd := d.(type) {
			case *ast.GenDecl:
				switch gd.Tok {
				case token.TYPE:
					for _, s := range gd.Specs {
						ts := s.(*ast.TypeSpec)
						docs := []*ast.CommentGroup{ts.Doc}
						if len(gd.Specs) == 1 {
							docs = append(docs, gd.Doc)
						}
						switch {
						case directive(docs, "native"):
							if _, ok := ts.Type.(*ast.InterfaceType); !ok {
								return l.errorf(ts.Pos(), "//asuka:native is for an interface")
							}
							natives = append(natives, decl{ts, docs})
							l.native[ts.Name.Name] = true
						case directive(docs, "view"):
							if _, ok := ts.Type.(*ast.StructType); !ok {
								return l.errorf(ts.Pos(), "//asuka:view is for a struct of the view's props and events")
							}
							views = append(views, decl{ts, docs})
						}
					}
				case token.VAR:
					if err := l.errorDecls(gd); err != nil {
						return err
					}
				}
			}
		}
	}
	for i, d := range natives {
		it, err := l.iface(d, uint32(i))
		if err != nil {
			return err
		}
		l.out.Interfaces = append(l.out.Interfaces, it)
	}
	for _, d := range views {
		v, err := l.view(d)
		if err != nil {
			return err
		}
		l.out.Views = append(l.out.Views, v)
	}
	for i, e := range l.out.Errors {
		e.Index = uint32(i)
	}
	return nil
}

// errorDecls finds var ErrX = native.NewError("...").
func (l *loader) errorDecls(gd *ast.GenDecl) error {
	for _, s := range gd.Specs {
		vs := s.(*ast.ValueSpec)
		for i, v := range vs.Values {
			call, ok := v.(*ast.CallExpr)
			if !ok {
				continue
			}
			fn := typeutilCallee(l.pkg.TypesInfo, call)
			if fn == nil || fn.Pkg() == nil || fn.Pkg().Path() != "github.com/egoist/asuka/native" || fn.Name() != "NewError" {
				continue
			}
			if i >= len(vs.Names) || len(call.Args) != 1 {
				return l.errorf(v.Pos(), "a declared error is var ErrX = native.NewError(\"message\")")
			}
			tv := l.pkg.TypesInfo.Types[call.Args[0]]
			if tv.Value == nil {
				return l.errorf(call.Args[0].Pos(), "native.NewError's message is a constant string")
			}
			msg, _ := strconv.Unquote(tv.Value.ExactString())
			name := vs.Names[i].Name
			if !strings.HasPrefix(name, "Err") || !ast.IsExported(name) {
				return l.errorf(vs.Names[i].Pos(), "a declared error is named ErrX, exported")
			}
			l.out.Errors = append(l.out.Errors, &ErrorDecl{Name: name, Message: msg})
		}
	}
	return nil
}

func typeutilCallee(info *types.Info, call *ast.CallExpr) *types.Func {
	var id *ast.Ident
	switch f := call.Fun.(type) {
	case *ast.Ident:
		id = f
	case *ast.SelectorExpr:
		id = f.Sel
	default:
		return nil
	}
	fn, _ := info.Uses[id].(*types.Func)
	return fn
}

func (l *loader) iface(d decl, index uint32) (*Interface, error) {
	it := &Interface{Name: d.spec.Name.Name, Index: index, MainThread: directive(d.doc, "mainthread"), Doc: docText(d.doc...)}
	if !ast.IsExported(it.Name) {
		return nil, l.errorf(d.spec.Pos(), "a native interface is exported")
	}
	if d.spec.TypeParams != nil {
		return nil, l.errorf(d.spec.Pos(), "a native interface isn't generic")
	}
	l.ifaces[it.Name] = it
	ast_ := d.spec.Type.(*ast.InterfaceType)
	n := uint32(0)
	for _, m := range ast_.Methods.List {
		if len(m.Names) == 0 {
			// An embedded interface: io.Closer only.
			t := l.pkg.TypesInfo.TypeOf(m.Type)
			if named, ok := t.(*types.Named); ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "io" && named.Obj().Name() == "Closer" {
				n++
				it.Methods = append(it.Methods, &Method{Name: "Close", Index: n, Error: true, Close: true, MainThread: it.MainThread})
				continue
			}
			return nil, l.errorf(m.Pos(), "a native interface embeds io.Closer, or nothing")
		}
		for _, name := range m.Names {
			fn, ok := l.pkg.TypesInfo.Defs[name].(*types.Func)
			if !ok {
				return nil, l.errorf(name.Pos(), "not a method")
			}
			n++
			meth, err := l.method(fn, name, n, it.MainThread || directive([]*ast.CommentGroup{m.Doc}, "mainthread"))
			if err != nil {
				return nil, err
			}
			meth.Doc = docText(m.Doc)
			it.Methods = append(it.Methods, meth)
		}
	}
	return it, nil
}

func (l *loader) method(fn *types.Func, id *ast.Ident, index uint32, main bool) (*Method, error) {
	sig := fn.Type().(*types.Signature)
	m := &Method{Name: fn.Name(), Index: index, MainThread: main}
	if sig.Variadic() {
		return nil, l.errorf(id.Pos(), "a native method isn't variadic")
	}
	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		p := params.At(i)
		if isNamed(p.Type(), "context", "Context") {
			if i != 0 {
				return nil, l.errorf(p.Pos(), "a context.Context is the first parameter")
			}
			m.Context = true
			continue
		}
		t, err := l.typ(p.Type(), p.Pos(), true)
		if err != nil {
			return nil, err
		}
		m.Params = append(m.Params, &Param{Name: paramName(p.Name(), i), Type: t})
	}
	res := sig.Results()
	switch {
	case res.Len() > 2:
		return nil, l.errorf(id.Pos(), "a native method returns a value, an error, or a value and an error")
	case res.Len() == 2:
		if !isError(res.At(1).Type()) {
			return nil, l.errorf(id.Pos(), "a native method's second result is an error")
		}
		m.Error = true
		fallthrough
	case res.Len() == 1:
		if isError(res.At(0).Type()) && res.Len() == 1 {
			m.Error = true
			break
		}
		t, err := l.typ(res.At(0).Type(), id.Pos(), false)
		if err != nil {
			return nil, err
		}
		m.Result = t
	}
	if m.Name == "Close" && len(m.Params) == 0 && !m.Context && m.Result == nil && m.Error {
		m.Close = true
	}
	return m, nil
}

func paramName(name string, i int) string {
	if name == "" || name == "_" {
		return "arg" + strconv.Itoa(i)
	}
	return name
}

func isError(t types.Type) bool {
	return types.Identical(t, types.Universe.Lookup("error").Type())
}

func isNamed(t types.Type, pkg, name string) bool {
	n, ok := types.Unalias(t).(*types.Named)
	return ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == pkg && n.Obj().Name() == name
}

var basics = map[types.BasicKind]Kind{
	types.Bool: Bool, types.Int8: Int8, types.Int16: Int16, types.Int32: Int32, types.Int64: Int64, types.Int: Int,
	types.Uint8: Uint8, types.Uint16: Uint16, types.Uint32: Uint32, types.Uint64: Uint64, types.Uint: Uint,
	types.Float32: Float32, types.Float64: Float64, types.String: String,
}

// typ maps a Go type; param says it's a parameter (which may be a func).
func (l *loader) typ(t types.Type, pos token.Pos, param bool) (*Type, error) {
	goName := types.TypeString(t, types.RelativeTo(l.pkg.Types))
	t = types.Unalias(t)
	switch x := t.(type) {
	case *types.Basic:
		if k, ok := basics[x.Kind()]; ok {
			return &Type{Kind: k, Go: goName}, nil
		}
	case *types.Slice:
		if b, ok := x.Elem().(*types.Basic); ok && b.Kind() == types.Uint8 {
			return &Type{Kind: Bytes, Go: goName}, nil
		}
		e, err := l.elem(x.Elem(), pos, goName)
		if err != nil {
			return nil, err
		}
		return &Type{Kind: Slice, Elem: e, Go: goName}, nil
	case *types.Map:
		if k, ok := x.Key().(*types.Basic); !ok || k.Kind() != types.String {
			return nil, l.errorf(pos, "a map's keys are strings: %s", goName)
		}
		e, err := l.elem(x.Elem(), pos, goName)
		if err != nil {
			return nil, err
		}
		return &Type{Kind: Map, Elem: e, Go: goName}, nil
	case *types.Pointer:
		if _, ok := types.Unalias(x.Elem()).(*types.Pointer); ok {
			return nil, l.errorf(pos, "a pointer to a pointer: %s", goName)
		}
		e, err := l.elem(x.Elem(), pos, goName)
		if err != nil {
			return nil, err
		}
		return &Type{Kind: Optional, Elem: e, Go: goName}, nil
	case *types.Signature:
		if !param {
			return nil, l.errorf(pos, "a func is a parameter only (a callback): %s", goName)
		}
		return l.funcType(x, pos, goName)
	case *types.Named:
		obj := x.Obj()
		switch {
		case obj.Pkg() != nil && obj.Pkg().Path() == "time" && obj.Name() == "Time":
			return &Type{Kind: Time, Go: goName}, nil
		case obj.Pkg() != nil && obj.Pkg().Path() == "time" && obj.Name() == "Duration":
			return &Type{Kind: Duration, Go: goName}, nil
		case obj.Pkg() == nil || obj.Pkg().Path() != l.pkg.PkgPath:
			return nil, l.errorf(pos, "%s: a type of another package (but time.Time and time.Duration)", goName)
		case x.TypeParams() != nil:
			return nil, l.errorf(pos, "%s: a generic type", goName)
		}
		switch u := x.Underlying().(type) {
		case *types.Struct:
			if err := l.structDecl(obj, u, pos); err != nil {
				return nil, err
			}
			return &Type{Kind: Struct, Name: obj.Name(), Go: goName}, nil
		case *types.Basic:
			k, ok := basics[u.Kind()]
			if !ok || k == Bool || k == Float32 || k == Float64 {
				return nil, l.errorf(pos, "%s: an enum is over a string or an integer", goName)
			}
			l.enumDecl(obj, k)
			return &Type{Kind: Enum, Name: obj.Name(), Go: goName}, nil
		case *types.Interface:
			if !l.native[obj.Name()] {
				return nil, l.errorf(pos, "%s: an interface that isn't //asuka:native", goName)
			}
			return &Type{Kind: Object, Name: obj.Name(), Go: goName}, nil
		}
	}
	return nil, l.errorf(pos, "%s can't cross to native code (bools, numbers, strings, []byte, time.Time, time.Duration, slices, maps of strings, pointers, the package's structs, enums and native interfaces, and funcs as parameters)", goName)
}

// elem maps a slice's, a map's or a pointer's element: not a native
// object, which crosses as a parameter or a result itself.
func (l *loader) elem(t types.Type, pos token.Pos, goName string) (*Type, error) {
	e, err := l.typ(t, pos, false)
	if err != nil {
		return nil, err
	}
	if e.Kind == Object {
		return nil, l.errorf(pos, "%s: a native object crosses as a parameter or a result, not in a slice, a map or a pointer", goName)
	}
	return e, nil
}

func (l *loader) funcType(sig *types.Signature, pos token.Pos, goName string) (*Type, error) {
	ft := &FuncType{}
	for i := 0; i < sig.Params().Len(); i++ {
		p := sig.Params().At(i)
		t, err := l.typ(p.Type(), pos, false)
		if err != nil {
			return nil, err
		}
		ft.Params = append(ft.Params, &Param{Name: paramName(p.Name(), i), Type: t})
	}
	switch sig.Results().Len() {
	case 0:
	case 1:
		t, err := l.typ(sig.Results().At(0).Type(), pos, false)
		if err != nil {
			return nil, err
		}
		ft.Result = t
	default:
		return nil, l.errorf(pos, "%s: a callback returns a value or nothing", goName)
	}
	if sig.Variadic() {
		return nil, l.errorf(pos, "%s: a callback isn't variadic", goName)
	}
	return &Type{Kind: Func, Func: ft, Go: goName}, nil
}

func (l *loader) structDecl(obj *types.TypeName, s *types.Struct, pos token.Pos) error {
	if _, ok := l.structs[obj.Name()]; ok {
		return nil
	}
	st := &StructDecl{Name: obj.Name(), Doc: l.typeDoc(obj)}
	l.structs[obj.Name()] = st // before its fields: a struct may refer to itself through a pointer or a slice
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		if !f.Exported() {
			return l.errorf(f.Pos(), "%s.%s: a struct that crosses has exported fields only", obj.Name(), f.Name())
		}
		if f.Embedded() {
			return l.errorf(f.Pos(), "%s.%s: a struct that crosses embeds nothing", obj.Name(), f.Name())
		}
		t, err := l.elem(f.Type(), f.Pos(), obj.Name()+"."+f.Name())
		if err != nil {
			return err
		}
		st.Fields = append(st.Fields, &Field{Name: f.Name(), Type: t, Doc: l.fieldDoc(f)})
	}
	l.out.Structs = append(l.out.Structs, st)
	return nil
}

func (l *loader) enumDecl(obj *types.TypeName, k Kind) {
	if _, ok := l.enums[obj.Name()]; ok {
		return
	}
	e := &EnumDecl{Name: obj.Name(), Basic: k, Doc: l.typeDoc(obj)}
	scope := l.pkg.Types.Scope()
	type cv struct {
		name string
		val  string
		pos  token.Pos
	}
	var vals []cv
	for _, n := range scope.Names() {
		c, ok := scope.Lookup(n).(*types.Const)
		if !ok || !c.Exported() || !types.Identical(c.Type(), obj.Type()) {
			continue
		}
		vals = append(vals, cv{n, c.Val().ExactString(), c.Pos()})
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i].pos < vals[j].pos })
	for _, v := range vals {
		e.Values = append(e.Values, EnumValue{Name: v.name, Value: v.val})
	}
	l.enums[obj.Name()] = e
	l.out.Enums = append(l.out.Enums, e)
}

// typeDoc and fieldDoc are a declaration's doc comment, found by its
// position.
func (l *loader) typeDoc(obj types.Object) string {
	for _, f := range l.pkg.Syntax {
		if f.Pos() > obj.Pos() || obj.Pos() > f.End() {
			continue
		}
		var doc string
		ast.Inspect(f, func(n ast.Node) bool {
			if gd, ok := n.(*ast.GenDecl); ok {
				for _, s := range gd.Specs {
					if ts, ok := s.(*ast.TypeSpec); ok && ts.Name.Pos() == obj.Pos() {
						docs := []*ast.CommentGroup{ts.Doc}
						if len(gd.Specs) == 1 {
							docs = append(docs, gd.Doc)
						}
						doc = docText(docs...)
					}
				}
			}
			return doc == ""
		})
		return doc
	}
	return ""
}

func (l *loader) fieldDoc(v *types.Var) string {
	for _, f := range l.pkg.Syntax {
		if f.Pos() > v.Pos() || v.Pos() > f.End() {
			continue
		}
		var doc string
		ast.Inspect(f, func(n ast.Node) bool {
			if fl, ok := n.(*ast.Field); ok {
				for _, name := range fl.Names {
					if name.Pos() == v.Pos() {
						doc = docText(fl.Doc, fl.Comment)
					}
				}
			}
			return doc == ""
		})
		return doc
	}
	return ""
}

func (l *loader) view(d decl) (*View, error) {
	name := d.spec.Name.Name
	if !strings.HasSuffix(name, "Props") || name == "Props" || !ast.IsExported(name) {
		return nil, l.errorf(d.spec.Pos(), "a view's props are an exported struct named <View>Props")
	}
	v := &View{Name: strings.TrimSuffix(name, "Props"), Doc: docText(d.doc...)}
	obj := l.pkg.TypesInfo.Defs[d.spec.Name]
	st := obj.Type().Underlying().(*types.Struct)
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		if !f.Exported() || f.Embedded() {
			return nil, l.errorf(f.Pos(), "%s.%s: a view's props are exported fields", name, f.Name())
		}
		ft := f.Type()
		if f.Name() == "Style" && (isNamed(ft, "github.com/egoist/asuka/ui", "Style") || isNamed(ft, "github.com/egoist/asuka/ui", "Styler")) {
			v.Style = true
			continue
		}
		if sig, ok := types.Unalias(ft).(*types.Signature); ok {
			t, err := l.funcType(sig, f.Pos(), types.TypeString(ft, types.RelativeTo(l.pkg.Types)))
			if err != nil {
				return nil, err
			}
			if t.Func.Result != nil {
				return nil, l.errorf(f.Pos(), "%s.%s: an event returns nothing", name, f.Name())
			}
			v.Events = append(v.Events, &Field{Name: f.Name(), Type: t, Doc: l.fieldDoc(f)})
			continue
		}
		accessor := false
		if n, ok := types.Unalias(ft).(*types.Named); ok && n.Obj().Pkg() != nil && n.Obj().Pkg().Path() == "github.com/egoist/asuka/signal" && n.Obj().Name() == "Accessor" {
			ft = n.TypeArgs().At(0)
			accessor = true
		}
		t, err := l.typ(ft, f.Pos(), false)
		if err != nil {
			return nil, err
		}
		if t.Kind == Object {
			return nil, l.errorf(f.Pos(), "%s.%s: a view's prop isn't a native object", name, f.Name())
		}
		v.Props = append(v.Props, &Field{Name: f.Name(), Type: t, Accessor: accessor, Doc: l.fieldDoc(f)})
	}
	return v, nil
}

// hash is what the declarations say, for generated files to record.
func (p *Package) hash() string {
	var b strings.Builder
	var typ func(t *Type) string
	typ = func(t *Type) string {
		if t == nil {
			return "-"
		}
		s := fmt.Sprintf("%d:%s", t.Kind, t.Name)
		if t.Elem != nil {
			s += "[" + typ(t.Elem) + "]"
		}
		if t.Func != nil {
			s += "("
			for _, p := range t.Func.Params {
				s += p.Name + " " + typ(p.Type) + ","
			}
			s += ")" + typ(t.Func.Result)
		}
		return s
	}
	fmt.Fprintln(&b, "v1", p.Path, p.Name)
	for _, it := range p.Interfaces {
		fmt.Fprintln(&b, "iface", it.Name, it.Index, it.MainThread)
		for _, m := range it.Methods {
			fmt.Fprint(&b, " method ", m.Name, m.Index, m.Context, m.Error, m.MainThread, m.Close, typ(m.Result))
			for _, p := range m.Params {
				fmt.Fprint(&b, " ", p.Name, " ", typ(p.Type))
			}
			fmt.Fprintln(&b, " //", m.Doc)
		}
		fmt.Fprintln(&b, "//", it.Doc)
	}
	for _, s := range p.Structs {
		fmt.Fprintln(&b, "struct", s.Name, s.Doc)
		for _, f := range s.Fields {
			fmt.Fprintln(&b, " ", f.Name, typ(f.Type), f.Doc)
		}
	}
	for _, e := range p.Enums {
		fmt.Fprintln(&b, "enum", e.Name, e.Basic, e.Values, e.Doc)
	}
	for _, e := range p.Errors {
		fmt.Fprintln(&b, "error", e.Name, e.Message)
	}
	for _, v := range p.Views {
		fmt.Fprintln(&b, "view", v.Name, v.Style, v.Doc)
		for _, f := range v.Props {
			fmt.Fprintln(&b, " prop", f.Name, typ(f.Type), f.Accessor, f.Doc)
		}
		for _, f := range v.Events {
			fmt.Fprintln(&b, " event", f.Name, typ(f.Type), f.Doc)
		}
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])[:16]
}

// exists reports whether path exists.
func exists(path string) bool { _, err := os.Stat(path); return err == nil }
