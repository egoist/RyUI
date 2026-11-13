package nativegen

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// SwiftFile is the generated Swift file's name, in the package's Swift
// (swift/).
func SwiftFile(p *Package) string { return upperCamel(p.Name) + ".asuka.swift" }

// SwiftErrorType is the Swift enum of the package's declared errors.
func SwiftErrorType(p *Package) string { return upperCamel(p.Name) + "Error" }

type swiftGen struct {
	p       *Package
	b       bytes.Buffer
	helpers map[string]*Type
	enums   map[string]*EnumDecl
	structs map[string]*StructDecl
	ifaces  map[string]*Interface
	class   string
}

func (g *swiftGen) printf(format string, args ...any) { fmt.Fprintf(&g.b, format, args...) }

var swiftOf = map[Kind]string{
	Bool: "Bool", Int8: "Int8", Int16: "Int16", Int32: "Int32", Int64: "Int64", Int: "Int",
	Uint8: "UInt8", Uint16: "UInt16", Uint32: "UInt32", Uint64: "UInt64", Uint: "UInt",
	Float32: "Float", Float64: "Double", String: "String", Bytes: "Data", Time: "Date", Duration: "TimeInterval",
}

var swiftWire = map[Kind]string{
	Bool: "bool", Int8: "int8", Int16: "int16", Int32: "int32", Int64: "int64", Int: "int",
	Uint8: "uint8", Uint16: "uint16", Uint32: "uint32", Uint64: "uint64", Uint: "uint",
	Float32: "float32", Float64: "float64", String: "string", Bytes: "data", Time: "time", Duration: "duration",
}

// typ is t as Swift writes it.
func (g *swiftGen) typ(t *Type) string {
	switch t.Kind {
	case Slice:
		return "[" + g.typ(t.Elem) + "]"
	case Map:
		return "[String: " + g.typ(t.Elem) + "]"
	case Optional:
		return g.typ(t.Elem) + "?"
	case Struct, Enum:
		return t.Name
	case Object:
		return "any " + t.Name
	case Func:
		var ps []string
		for _, p := range t.Func.Params {
			ps = append(ps, g.typ(p.Type))
		}
		r := "Void"
		if t.Func.Result != nil {
			r = g.typ(t.Func.Result)
		}
		return "@escaping (" + strings.Join(ps, ", ") + ") -> " + r
	}
	return swiftOf[t.Kind]
}

// paramType is a parameter's type: a native object may be nil.
func (g *swiftGen) paramType(t *Type) string {
	if t.Kind == Object {
		return "(any " + t.Name + ")?"
	}
	return g.typ(t)
}

// zero is t's default in an initializer.
func (g *swiftGen) zero(t *Type) string {
	switch t.Kind {
	case Bool:
		return "false"
	case String:
		return `""`
	case Bytes:
		return "Data()"
	case Time:
		return "Date(timeIntervalSince1970: 0)"
	case Slice:
		return "[]"
	case Map:
		return "[:]"
	case Optional:
		return "nil"
	case Struct:
		return t.Name + "()"
	case Enum:
		if g.enums[t.Name].Basic == String {
			return t.Name + `(rawValue: "")`
		}
		return t.Name + "(rawValue: 0)"
	case Object:
		return ""
	}
	return "0"
}

func (g *swiftGen) enc(t *Type, w, v string) string {
	switch t.Kind {
	case Enum:
		return fmt.Sprintf("%s.%s(%s.rawValue)", w, swiftWire[g.enums[t.Name].Basic], v)
	case Slice, Map, Optional, Struct, Object:
		g.need(t)
		return fmt.Sprintf("Wire.write%s(&%s, %s)", mangle(t), w, v)
	}
	return fmt.Sprintf("%s.%s(%s)", w, swiftWire[t.Kind], v)
}

func (g *swiftGen) dec(t *Type, r string) string {
	switch t.Kind {
	case Enum:
		return fmt.Sprintf("%s(rawValue: %s.%s())", t.Name, r, swiftWire[g.enums[t.Name].Basic])
	case Slice, Map, Optional, Struct, Object:
		g.need(t)
		return fmt.Sprintf("Wire.read%s(&%s)", mangle(t), r)
	}
	return fmt.Sprintf("%s.%s()", r, swiftWire[t.Kind])
}

func (g *swiftGen) need(t *Type) {
	if _, ok := g.helpers[mangle(t)]; ok {
		return
	}
	g.helpers[mangle(t)] = t
	if t.Elem != nil {
		switch t.Elem.Kind {
		case Slice, Map, Optional, Struct, Object:
			g.need(t.Elem)
		}
	}
}

// recursive reports whether a struct holds itself, other than through a
// slice or a map: Swift's are a class then.
func (g *swiftGen) recursive(s *StructDecl) bool {
	seen := map[string]bool{}
	var holds func(t *Type) bool
	holds = func(t *Type) bool {
		switch t.Kind {
		case Optional:
			return holds(t.Elem)
		case Struct:
			if t.Name == s.Name {
				return true
			}
			if seen[t.Name] {
				return false
			}
			seen[t.Name] = true
			for _, f := range g.structs[t.Name].Fields {
				if holds(f.Type) {
					return true
				}
			}
		}
		return false
	}
	for _, f := range s.Fields {
		if holds(f.Type) {
			return true
		}
	}
	return false
}

func swiftDoc(doc, indent string) string {
	if doc == "" {
		return ""
	}
	var b strings.Builder
	for _, l := range strings.Split(doc, "\n") {
		b.WriteString(strings.TrimRight(indent+"/// "+l, " ") + "\n")
	}
	return b.String()
}

// swiftString is a Go constant's string value as a Swift literal.
func swiftString(goLit string) string {
	s, err := strconv.Unquote(goLit)
	if err != nil {
		s = goLit
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u{%x}`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// GenerateSwift writes the package's Swift side, for its ios/ and
// macos/ targets.
func GenerateSwift(p *Package) []byte {
	g := &swiftGen{p: p, helpers: map[string]*Type{}, enums: map[string]*EnumDecl{}, structs: map[string]*StructDecl{}, ifaces: map[string]*Interface{}, class: SwiftClass(p.Path)}
	for _, e := range p.Enums {
		g.enums[e.Name] = e
	}
	for _, s := range p.Structs {
		g.structs[s.Name] = s
	}
	for _, it := range p.Interfaces {
		g.ifaces[it.Name] = it
	}
	g.printf("%s\n", header("//", p.Hash))
	g.printf("// What the package's Go declares (package %s), as its Swift implements\n", p.Name)
	g.printf("// it. The Swift writes the functions that make each:\n//\n")
	for _, it := range p.Interfaces {
		main := ""
		if it.MainThread {
			main = "@MainActor "
		}
		g.printf("//\t%sfunc make%s() throws -> any %s\n", main, it.Name, it.Name)
	}
	for _, v := range p.Views {
		g.printf("//\t@MainActor func make%s(_ events: %sEvents) -> any %s\n", v.Name, v.Name, v.Name)
	}
	g.printf("\nimport Foundation\nimport Asuka\n\n")

	for _, e := range p.Enums {
		g.enum(e)
	}
	for _, s := range p.Structs {
		g.structDecl(s)
	}
	if len(p.Errors) > 0 {
		g.errors()
	}
	for _, it := range p.Interfaces {
		g.protocol(it)
	}
	for _, v := range p.Views {
		g.viewDecl(v)
	}
	g.provider()
	main := g.b.String()
	g.b.Reset()
	g.wire()
	return []byte(main + g.b.String())
}

func (g *swiftGen) enum(e *EnumDecl) {
	raw := swiftOf[e.Basic]
	g.printf("%spublic struct %s: RawRepresentable, Hashable, Sendable {\n", swiftDoc(e.Doc, ""), e.Name)
	g.printf("    public var rawValue: %s\n    public init(rawValue: %s) { self.rawValue = rawValue }\n", raw, raw)
	for _, v := range e.Values {
		val := v.Value
		if e.Basic == String {
			val = swiftString(val)
		}
		g.printf("    public static let %s = %s(rawValue: %s)\n", swiftName(lowerCamel(enumCase(e.Name, v.Name))), e.Name, val)
	}
	g.printf("}\n\n")
}

func (g *swiftGen) structDecl(s *StructDecl) {
	kind := "struct"
	if g.recursive(s) {
		kind = "final class"
	}
	g.printf("%spublic %s %s {\n", swiftDoc(s.Doc, ""), kind, s.Name)
	var params, sets []string
	for _, f := range s.Fields {
		n := swiftName(lowerCamel(f.Name))
		g.printf("%s    public var %s: %s\n", swiftDoc(f.Doc, "    "), n, g.typ(f.Type))
		p := n + ": " + g.typ(f.Type)
		if z := g.zero(f.Type); z != "" {
			p += " = " + z
		}
		params = append(params, p)
		sets = append(sets, fmt.Sprintf("        self.%s = %s", n, n))
	}
	g.printf("\n    public init(%s) {\n%s\n    }\n}\n\n", strings.Join(params, ", "), strings.Join(sets, "\n"))
}

func (g *swiftGen) errors() {
	t := SwiftErrorType(g.p)
	g.printf("/// The errors the package declares (native.NewError), which its Go gets\n/// as those values.\n")
	g.printf("public enum %s: Error, AsukaDeclaredError, LocalizedError {\n", t)
	for _, e := range g.p.Errors {
		g.printf("    case %s\n", swiftName(lowerCamel(errorCase(e.Name))))
	}
	g.printf("\n    public var asukaIndex: UInt32 {\n        switch self {\n")
	for _, e := range g.p.Errors {
		g.printf("        case .%s: return %d\n", lowerCamel(errorCase(e.Name)), e.Index)
	}
	g.printf("        }\n    }\n\n    public var errorDescription: String? {\n        switch self {\n")
	for _, e := range g.p.Errors {
		g.printf("        case .%s: return %s\n", lowerCamel(errorCase(e.Name)), swiftString(strconv.Quote(e.Message)))
	}
	g.printf("        }\n    }\n}\n\n")
}

// signature is a method's Swift declaration, without its name's func.
func (g *swiftGen) signature(m *Method) string {
	var ps []string
	for _, p := range m.Params {
		ps = append(ps, swiftName(p.Name)+": "+g.paramType(p.Type))
	}
	s := swiftName(lowerCamel(m.Name)) + "(" + strings.Join(ps, ", ") + ")"
	if m.Context {
		s += " async"
	}
	if m.Error {
		s += " throws"
	}
	if m.Result != nil {
		s += " -> " + g.typ(m.Result)
	}
	return s
}

func (g *swiftGen) protocol(it *Interface) {
	main := ""
	if it.MainThread {
		main = "@MainActor "
	}
	g.printf("%s%spublic protocol %s: AnyObject {\n", swiftDoc(it.Doc, ""), main, it.Name)
	for _, m := range it.Methods {
		mm := ""
		if m.MainThread && !it.MainThread {
			mm = "@MainActor "
		}
		g.printf("%s    %sfunc %s\n", swiftDoc(m.Doc, "    "), mm, g.signature(m))
	}
	g.printf("}\n\n")
}

func (g *swiftGen) viewDecl(v *View) {
	g.printf("%s@MainActor public protocol %s: AnyObject {\n", swiftDoc(v.Doc, ""), v.Name)
	g.printf("    /// The platform's view, which the host shows.\n    var view: AsukaPlatformView { get }\n")
	for _, f := range v.Props {
		g.printf("%s    func set%s(_ value: %s)\n", swiftDoc(f.Doc, "    "), f.Name, g.typ(f.Type))
	}
	g.printf("}\n\n")
	g.printf("/// Sends a %s's events to its Go.\n@MainActor public struct %sEvents {\n    let ctx: AsukaNativeContext\n\n", v.Name, v.Name)
	for _, e := range v.Events {
		var ps []string
		for _, a := range e.Type.Func.Params {
			ps = append(ps, swiftName(a.Name)+": "+g.typ(a.Type))
		}
		g.printf("%s    public func %s(%s) {\n        var w = AsukaNativeWriter()\n", swiftDoc(e.Doc, "    "), swiftName(lowerCamel(e.Name)), strings.Join(ps, ", "))
		for _, a := range e.Type.Func.Params {
			g.printf("        %s\n", g.enc(a.Type, "w", swiftName(a.Name)))
		}
		g.printf("        ctx.emit(%q, [\"v\": .bytes(Data(w.bytes))])\n    }\n\n", e.Name)
	}
	g.printf("}\n\n")
	g.printf("/// What the host shows for a %s: the Swift's, its props decoded.\n", v.Name)
	g.printf("@MainActor final class AsukaView%s: AsukaNativeView {\n    let impl: any %s\n\n", v.Name, v.Name)
	g.printf("    init(_ ctx: AsukaNativeContext) { impl = make%s(%sEvents(ctx: ctx)) }\n\n", v.Name, v.Name)
	g.printf("    var view: AsukaPlatformView { impl.view }\n\n")
	g.printf("    func set(_ name: String, _ value: AsukaNativeValue) {\n        guard case .bytes(let d) = value else { return }\n        var r = AsukaNativeReader(d)\n        switch name {\n")
	for _, f := range v.Props {
		g.printf("        case %q:\n            let v = %s\n            if !r.failed { impl.set%s(v) }\n", f.Name, g.dec(f.Type, "r"), f.Name)
	}
	g.printf("        default: break\n        }\n    }\n")
	g.printf("    #if os(macOS)\n    func sizeThatFits(_ size: CGSize) -> CGSize { (impl as? AsukaSizing)?.sizeThatFits(size) ?? view.fittingSize }\n")
	g.printf("    func destroy() { (impl as? AsukaDestroying)?.destroy() }\n    #endif\n}\n\n")
}

func (g *swiftGen) provider() {
	g.printf("/// The package's native code as the host finds it (by its Objective-C\n/// name): what makes, calls and shows what the package declares.\n")
	g.printf("@objc(%s) public final class %s: NSObject, AsukaModuleProvider {\n", g.class, g.class)
	// make
	g.printf("    public static func make(_ iface: UInt32) throws -> AnyObject {\n        switch iface {\n")
	for _, it := range g.p.Interfaces {
		if it.MainThread {
			g.printf("        case %d: return try AsukaNative.onMain { try make%s() }\n", it.Index, it.Name)
		} else {
			g.printf("        case %d: return try make%s()\n", it.Index, it.Name)
		}
	}
	g.printf("        default: throw AsukaNativeError.unsupported(\"no interface \\(iface)\")\n        }\n    }\n\n")
	// call
	g.printf("    public static func call(_ object: AnyObject, _ iface: UInt32, _ method: UInt32, _ r: inout AsukaNativeReader, _ w: inout AsukaNativeWriter) throws {\n")
	g.printf("        switch (iface, method) {\n")
	for _, it := range g.p.Interfaces {
		for _, m := range it.Methods {
			if m.Context {
				continue
			}
			g.printf("        case (%d, %d): // %s.%s\n            let o = object as! any %s\n", it.Index, m.Index, it.Name, m.Name, it.Name)
			args := g.decodeArgs(m, "            ")
			g.printf("            try r.check()\n")
			call := fmt.Sprintf("o.%s(%s)", swiftName(lowerCamel(m.Name)), args)
			if m.Error {
				call = "try " + call
			}
			if m.MainThread {
				call = fmt.Sprintf("try AsukaNative.onMain { %s }", call)
			}
			if m.Result != nil {
				g.printf("            let v = %s\n            %s\n", call, g.enc(m.Result, "w", "v"))
			} else {
				g.printf("            %s\n", call)
			}
		}
	}
	g.printf("        default: throw AsukaNativeError.unsupported(\"no method \\(iface).\\(method)\")\n        }\n    }\n\n")
	// start
	g.printf("    public static func start(_ object: AnyObject, _ iface: UInt32, _ method: UInt32, _ input: AsukaNativeReader, _ done: @escaping (Result<AsukaNativeWriter, Error>) -> Void) -> Task<Void, Never>? {\n")
	g.printf("        var r = input\n        switch (iface, method) {\n")
	for _, it := range g.p.Interfaces {
		for _, m := range it.Methods {
			if !m.Context {
				continue
			}
			g.printf("        case (%d, %d): // %s.%s\n            let o = object as! any %s\n", it.Index, m.Index, it.Name, m.Name, it.Name)
			args := g.decodeArgs(m, "            ")
			g.printf("            guard !r.failed else { done(.failure(AsukaNativeError.malformed)); return nil }\n")
			actor := ""
			if m.MainThread {
				actor = " @MainActor in"
			}
			call := fmt.Sprintf("await o.%s(%s)", swiftName(lowerCamel(m.Name)), args)
			if m.Error {
				call = "try " + call
			}
			g.printf("            return Task {%s\n                do {\n                    var w = AsukaNativeWriter.ok()\n", actor)
			if m.Result != nil {
				g.printf("                    let v = %s\n                    %s\n", call, g.enc(m.Result, "w", "v"))
			} else {
				g.printf("                    %s\n", call)
			}
			g.printf("                    done(.success(w))\n                } catch {\n                    done(.failure(error))\n                }\n            }\n")
		}
	}
	g.printf("        default:\n            done(.failure(AsukaNativeError.unsupported(\"no method \\(iface).\\(method)\")))\n            return nil\n        }\n    }\n\n")
	// views
	g.printf("    @MainActor public static func makeView(_ name: String, _ ctx: AsukaNativeContext) -> AsukaNativeView? {\n        switch name {\n")
	for _, v := range g.p.Views {
		g.printf("        case %q: return AsukaView%s(ctx)\n", v.Name, v.Name)
	}
	g.printf("        default: return nil\n        }\n    }\n}\n\n")
}

// decodeArgs decodes a method's arguments into lets, and returns its
// call's arguments.
func (g *swiftGen) decodeArgs(m *Method, indent string) string {
	var args []string
	for _, p := range m.Params {
		n := swiftName(p.Name)
		if p.Type.Kind == Func {
			g.closure(n, p.Type.Func, indent)
		} else {
			g.printf("%slet %s = %s\n", indent, n, g.dec(p.Type, "r"))
		}
		args = append(args, n+": "+n)
	}
	return strings.Join(args, ", ")
}

// closure decodes a callback: a Swift closure that calls back Go's func.
func (g *swiftGen) closure(n string, ft *FuncType, indent string) {
	var ps, ts []string
	for i, p := range ft.Params {
		ps = append(ps, "a"+strconv.Itoa(i))
		ts = append(ts, g.typ(p.Type))
	}
	res := "Void"
	if ft.Result != nil {
		res = g.typ(ft.Result)
	}
	id := strings.Trim(n, "`") + "ID"
	g.printf("%slet %s = r.callback()\n", indent, id)
	in := ""
	if len(ps) > 0 {
		in = " " + strings.Join(ps, ", ") + " in"
	}
	g.printf("%slet %s: (%s) -> %s = {%s\n%s    var w = AsukaNativeWriter()\n", indent, n, strings.Join(ts, ", "), res, in, indent)
	for i, p := range ft.Params {
		g.printf("%s    %s\n", indent, g.enc(p.Type, "w", "a"+strconv.Itoa(i)))
	}
	if ft.Result != nil {
		g.printf("%s    var a = AsukaNative.ask(%s, w)\n%s    return %s\n", indent, id, indent, g.dec(ft.Result, "a"))
	} else {
		g.printf("%s    AsukaNative.notify(%s, w)\n", indent, id)
	}
	g.printf("%s}\n", indent)
}

func (g *swiftGen) wire() {
	g.printf("/// The package's values on the wire (package native's format).\nfileprivate enum Wire {\n")
	done := map[string]bool{}
	for {
		var names []string
		for n := range g.helpers {
			if !done[n] {
				names = append(names, n)
			}
		}
		if len(names) == 0 {
			break
		}
		sort.Strings(names)
		for _, n := range names {
			done[n] = true
			g.helper(g.helpers[n])
		}
	}
	g.printf("}\n")
}

func (g *swiftGen) helper(t *Type) {
	n, st := mangle(t), g.typ(t)
	switch t.Kind {
	case Struct:
		s := g.structs[t.Name]
		g.printf("    static func write%s(_ w: inout AsukaNativeWriter, _ v: %s) {\n", n, st)
		for _, f := range s.Fields {
			g.printf("        %s\n", g.enc(f.Type, "w", "v."+swiftName(lowerCamel(f.Name))))
		}
		g.printf("    }\n\n    static func read%s(_ r: inout AsukaNativeReader) -> %s {\n", n, st)
		var args []string
		for i, f := range s.Fields {
			g.printf("        let f%d = %s\n", i, g.dec(f.Type, "r"))
			args = append(args, fmt.Sprintf("%s: f%d", swiftName(lowerCamel(f.Name)), i))
		}
		g.printf("        return %s(%s)\n    }\n\n", t.Name, strings.Join(args, ", "))
	case Slice:
		g.printf("    static func write%s(_ w: inout AsukaNativeWriter, _ v: %s) {\n        w.len(v.count)\n        for e in v { %s }\n    }\n\n", n, st, g.enc(t.Elem, "w", "e"))
		g.printf("    static func read%s(_ r: inout AsukaNativeReader) -> %s {\n        let n = r.len()\n        var v: %s = []\n        v.reserveCapacity(n)\n", n, st, st)
		g.printf("        for _ in 0..<n { v.append(%s) }\n        return v\n    }\n\n", g.dec(t.Elem, "r"))
	case Map:
		g.printf("    static func write%s(_ w: inout AsukaNativeWriter, _ v: %s) {\n        w.len(v.count)\n        for (k, e) in v {\n            w.string(k)\n            %s\n        }\n    }\n\n", n, st, g.enc(t.Elem, "w", "e"))
		g.printf("    static func read%s(_ r: inout AsukaNativeReader) -> %s {\n        let n = r.len()\n        var v: %s = [:]\n        v.reserveCapacity(n)\n", n, st, st)
		g.printf("        for _ in 0..<n {\n            let k = r.string()\n            v[k] = %s\n        }\n        return v\n    }\n\n", g.dec(t.Elem, "r"))
	case Optional:
		g.printf("    static func write%s(_ w: inout AsukaNativeWriter, _ v: %s) {\n        w.present(v != nil)\n        if let v { %s }\n    }\n\n", n, st, g.enc(t.Elem, "w", "v"))
		g.printf("    static func read%s(_ r: inout AsukaNativeReader) -> %s {\n        r.present() ? %s : nil\n    }\n\n", n, st, g.dec(t.Elem, "r"))
	case Object:
		it := g.ifaces[t.Name]
		g.printf("    static func write%s(_ w: inout AsukaNativeWriter, _ v: (any %s)?) {\n        w.uint64(v.map { AsukaNative.retain($0, %s.self, %d) } ?? 0)\n    }\n\n", n, t.Name, g.class, it.Index)
		g.printf("    static func read%s(_ r: inout AsukaNativeReader) -> (any %s)? {\n        AsukaNative.object(r.uint64()) as? any %s\n    }\n\n", n, t.Name, t.Name)
	}
}
