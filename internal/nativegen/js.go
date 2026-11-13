package nativegen

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// JSFile is the generated JavaScript's name in the package's web/;
// JSImpl, the JavaScript it imports, which implements the package.
func JSFile(p *Package) string { return p.Name + ".asuka.js" }
func JSImpl(p *Package) string { return p.Name + ".js" }

// JSErrorType is the class of the package's declared errors.
func JSErrorType(p *Package) string { return upperCamel(p.Name) + "Error" }

var jsZero = map[Kind]string{
	Bool: "false", Int8: "0", Int16: "0", Int32: "0", Int64: "0n", Int: "0",
	Uint8: "0", Uint16: "0", Uint32: "0", Uint64: "0n", Uint: "0",
	Float32: "0", Float64: "0", String: `""`, Bytes: "new Uint8Array(0)", Time: "new Date(0)", Duration: "0",
}

type jsGen struct {
	p       *Package
	b       bytes.Buffer
	helpers map[string]*Type
	enums   map[string]*EnumDecl
	structs map[string]*StructDecl
	ifaces  map[string]*Interface
}

func (g *jsGen) printf(format string, args ...any) { fmt.Fprintf(&g.b, format, args...) }

func (g *jsGen) zero(t *Type) string {
	switch t.Kind {
	case Slice:
		return "[]"
	case Map:
		return "{}"
	case Optional, Object:
		return "null"
	case Struct:
		return "{}"
	case Enum:
		return jsZero[g.enums[t.Name].Basic]
	}
	return jsZero[t.Kind]
}

func (g *jsGen) enc(t *Type, w, v string) string {
	switch t.Kind {
	case Enum:
		return fmt.Sprintf("%s.%s(%s)", w, swiftWire[g.enums[t.Name].Basic], v)
	case Slice, Map, Optional, Struct, Object:
		g.need(t)
		return fmt.Sprintf("Wire.write%s(%s, %s)", mangle(t), w, v)
	}
	return fmt.Sprintf("%s.%s(%s)", w, swiftWire[t.Kind], v)
}

func (g *jsGen) dec(t *Type, r string) string {
	switch t.Kind {
	case Enum:
		return fmt.Sprintf("%s.%s()", r, swiftWire[g.enums[t.Name].Basic])
	case Slice, Map, Optional, Struct, Object:
		g.need(t)
		return fmt.Sprintf("Wire.read%s(%s)", mangle(t), r)
	}
	return fmt.Sprintf("%s.%s()", r, swiftWire[t.Kind])
}

func (g *jsGen) need(t *Type) {
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

func jsDoc(doc, indent string) string {
	if doc == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(indent + "/**\n")
	for _, l := range strings.Split(doc, "\n") {
		b.WriteString(strings.TrimRight(indent+" * "+strings.ReplaceAll(l, "*/", "* /"), " ") + "\n")
	}
	b.WriteString(indent + " */\n")
	return b.String()
}

// GenerateJS writes the package's JavaScript side, for its web/.
func GenerateJS(p *Package) []byte {
	g := &jsGen{p: p, helpers: map[string]*Type{}, enums: map[string]*EnumDecl{}, structs: map[string]*StructDecl{}, ifaces: map[string]*Interface{}}
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
	g.printf("// What the package's Go declares (package %s), as its JavaScript\n", p.Name)
	g.printf("// implements it. %s exports the functions that make each:\n//\n", JSImpl(p))
	for _, it := range p.Interfaces {
		g.printf("//\texport function make%s() // returns its methods (lowerCamelCase)\n", it.Name)
	}
	for _, v := range p.Views {
		g.printf("//\texport function make%s(events) // returns { el, set<Prop>(value)..., measure?, destroy? }\n", v.Name)
	}
	g.printf("//\n// A method that takes a context in Go is async, and gets an AbortSignal\n// first. int64 and uint64 are BigInts; a time is a Date; a duration is\n// milliseconds; a []byte is a Uint8Array.\n\n")
	g.printf("import * as impl from \"./%s\";\n\n", JSImpl(p))
	g.printf("const { NativeWriter, NativeReader, DeclaredError, UnsupportedError, registerModule, retain, object } = globalThis.asuka.native;\n\n")
	for _, e := range p.Enums {
		g.printf("%sexport const %s = Object.freeze({\n", jsDoc(e.Doc, ""), e.Name)
		for _, v := range e.Values {
			val := v.Value
			if e.Basic == Int64 || e.Basic == Uint64 {
				val += "n"
			}
			g.printf("  %s: %s,\n", lowerCamel(enumCase(e.Name, v.Name)), val)
		}
		g.printf("});\n\n")
	}
	if len(p.Errors) > 0 {
		t := JSErrorType(p)
		g.printf("const errorIndex = { ")
		var parts, msgs []string
		for _, e := range p.Errors {
			parts = append(parts, fmt.Sprintf("%s: %d", lowerCamel(errorCase(e.Name)), e.Index))
			msgs = append(msgs, strconv.Quote(e.Message))
		}
		g.printf("%s };\nconst errorMessage = [%s];\n\n", strings.Join(parts, ", "), strings.Join(msgs, ", "))
		g.printf("/** One of the errors the package declares (native.NewError): throw new %s(\"%s\"). */\n", t, lowerCamel(errorCase(p.Errors[0].Name)))
		g.printf("export class %s extends DeclaredError {\n  constructor(name) {\n    const i = errorIndex[name];\n    if (i === undefined) throw new Error(`no error ${name}`);\n    super(i, errorMessage[i]);\n    this.name = %q;\n  }\n}\n\n", t, t)
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

func (g *jsGen) viewDecl(v *View) {
	g.printf("function view%s(ctx) {\n  const events = {\n", v.Name)
	for _, e := range v.Events {
		var ps []string
		for _, a := range e.Type.Func.Params {
			ps = append(ps, jsName(a.Name))
		}
		g.printf("    %s(%s) {\n      const w = new NativeWriter();\n", lowerCamel(e.Name), strings.Join(ps, ", "))
		for _, a := range e.Type.Func.Params {
			g.printf("      %s;\n", g.enc(a.Type, "w", jsName(a.Name)))
		}
		g.printf("      ctx.emit(%q, { v: w.bytes() });\n    },\n", e.Name)
	}
	g.printf("  };\n  const v = impl.make%s(events);\n  return {\n    el: v.el,\n    set(name, value) {\n      const r = new NativeReader(value);\n      switch (name) {\n", v.Name)
	for _, f := range v.Props {
		g.printf("        case %q: {\n          const x = %s;\n          if (!r.failed) v.set%s(x);\n          break;\n        }\n", f.Name, g.dec(f.Type, "r"), f.Name)
	}
	g.printf("      }\n    },\n    measure: v.measure && ((c) => v.measure(c)),\n    destroy: v.destroy && (() => v.destroy()),\n  };\n}\n\n")
}

func (g *jsGen) provider() {
	g.printf("const provider = {\n  make(iface) {\n    switch (iface) {\n")
	for _, it := range g.p.Interfaces {
		g.printf("      case %d: return impl.make%s();\n", it.Index, it.Name)
	}
	g.printf("    }\n    throw new UnsupportedError(`no interface ${iface}`);\n  },\n\n")
	g.printf("  call(o, iface, method, r, w) {\n    switch (iface * 65536 + method) {\n")
	for _, it := range g.p.Interfaces {
		for _, m := range it.Methods {
			if m.Context {
				continue
			}
			g.printf("      case %d: { // %s.%s\n", int(it.Index)*65536+int(m.Index), it.Name, m.Name)
			args := g.decodeArgs(m, "        ")
			g.printf("        r.check();\n")
			call := fmt.Sprintf("o.%s(%s)", lowerCamel(m.Name), args)
			if m.Result != nil {
				g.printf("        const v = %s;\n        %s;\n", call, g.enc(m.Result, "w", "v"))
			} else {
				g.printf("        %s;\n", call)
			}
			g.printf("        return;\n      }\n")
		}
	}
	g.printf("    }\n    throw new UnsupportedError(`no method ${iface}.${method}`);\n  },\n\n")
	g.printf("  start(o, iface, method, r, signal) {\n    switch (iface * 65536 + method) {\n")
	for _, it := range g.p.Interfaces {
		for _, m := range it.Methods {
			if !m.Context {
				continue
			}
			g.printf("      case %d: { // %s.%s\n", int(it.Index)*65536+int(m.Index), it.Name, m.Name)
			args := g.decodeArgs(m, "        ")
			g.printf("        r.check();\n        return (async () => {\n          const w = NativeWriter.ok();\n")
			all := "signal"
			if args != "" {
				all += ", " + args
			}
			call := fmt.Sprintf("await o.%s(%s)", lowerCamel(m.Name), all)
			if m.Result != nil {
				g.printf("          const v = %s;\n          %s;\n", call, g.enc(m.Result, "w", "v"))
			} else {
				g.printf("          %s;\n", call)
			}
			g.printf("          return w;\n        })();\n      }\n")
		}
	}
	g.printf("    }\n    throw new UnsupportedError(`no method ${iface}.${method}`);\n  },\n\n")
	g.printf("  makeView(name, ctx) {\n    switch (name) {\n")
	for _, v := range g.p.Views {
		g.printf("      case %q: return view%s(ctx);\n", v.Name, v.Name)
	}
	g.printf("    }\n    return null;\n  },\n};\n\nregisterModule(%q, provider);\n\n", g.p.Path)
}

func (g *jsGen) decodeArgs(m *Method, indent string) string {
	var args []string
	for _, p := range m.Params {
		n := jsName(p.Name)
		if p.Type.Kind == Func {
			var ps []string
			for i := range p.Type.Func.Params {
				ps = append(ps, "a"+strconv.Itoa(i))
			}
			g.printf("%sconst %sID = r.callback();\n%sconst %s = (%s) => {\n%s  const cw = new NativeWriter();\n", indent, n, indent, n, strings.Join(ps, ", "), indent)
			for i, a := range p.Type.Func.Params {
				g.printf("%s  %s;\n", indent, g.enc(a.Type, "cw", "a"+strconv.Itoa(i)))
			}
			if p.Type.Func.Result != nil {
				g.printf("%s  const a = r.ask(%sID, cw);\n%s  return %s;\n", indent, n, indent, g.dec(p.Type.Func.Result, "a"))
			} else {
				g.printf("%s  r.notify(%sID, cw);\n", indent, n)
			}
			g.printf("%s};\n", indent)
		} else {
			g.printf("%sconst %s = %s;\n", indent, n, g.dec(p.Type, "r"))
		}
		args = append(args, n)
	}
	return strings.Join(args, ", ")
}

func (g *jsGen) wire() {
	g.printf("// The package's values on the wire (package native's format).\nconst Wire = {\n")
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
	g.printf("};\n")
}

func (g *jsGen) helper(t *Type) {
	n := mangle(t)
	switch t.Kind {
	case Struct:
		s := g.structs[t.Name]
		g.printf("  write%s(w, v) {\n", n)
		for _, f := range s.Fields {
			g.printf("    %s;\n", g.enc(f.Type, "w", fmt.Sprintf("(v?.%s ?? %s)", lowerCamel(f.Name), g.zero(f.Type))))
		}
		g.printf("  },\n  read%s(r) {\n    const v = {};\n", n)
		for _, f := range s.Fields {
			g.printf("    v.%s = %s;\n", lowerCamel(f.Name), g.dec(f.Type, "r"))
		}
		g.printf("    return v;\n  },\n")
	case Slice:
		g.printf("  write%s(w, v) {\n    w.len(v.length);\n    for (const e of v) %s;\n  },\n", n, g.enc(t.Elem, "w", "e"))
		g.printf("  read%s(r) {\n    const n = r.len();\n    const v = new Array(n);\n    for (let i = 0; i < n; i++) v[i] = %s;\n    return v;\n  },\n", n, g.dec(t.Elem, "r"))
	case Map:
		g.printf("  write%s(w, v) {\n    const e = Object.entries(v);\n    w.len(e.length);\n    for (const [k, x] of e) {\n      w.string(k);\n      %s;\n    }\n  },\n", n, g.enc(t.Elem, "w", "x"))
		g.printf("  read%s(r) {\n    const n = r.len();\n    const v = {};\n    for (let i = 0; i < n; i++) {\n      const k = r.string();\n      v[k] = %s;\n    }\n    return v;\n  },\n", n, g.dec(t.Elem, "r"))
	case Optional:
		g.printf("  write%s(w, v) {\n    w.present(v != null);\n    if (v != null) %s;\n  },\n", n, g.enc(t.Elem, "w", "v"))
		g.printf("  read%s(r) {\n    return r.present() ? %s : null;\n  },\n", n, g.dec(t.Elem, "r"))
	case Object:
		it := g.ifaces[t.Name]
		g.printf("  write%s(w, v) {\n    w.uint64(v == null ? 0 : retain(v, provider, %d));\n  },\n", n, it.Index)
		g.printf("  read%s(r) {\n    return object(r.uint64());\n  },\n", n)
	}
}
