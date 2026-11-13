package nativegen

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// KotlinFile is the generated Kotlin file's path in the package's
// android/ module.
func KotlinFile(p *Package) string {
	return filepath.Join(append([]string{"src", "main", "kotlin"}, append(strings.Split(KotlinPackage(p.Path), "."), upperCamel(p.Name)+".asuka.kt")...)...)
}

// KotlinErrorType is the Kotlin enum of the package's declared errors;
// KotlinException, the exception that throws one.
func KotlinErrorType(p *Package) string { return upperCamel(p.Name) + "Error" }
func KotlinException(p *Package) string { return upperCamel(p.Name) + "Exception" }

var kotlinOf = map[Kind]string{
	Bool: "Boolean", Int8: "Byte", Int16: "Short", Int32: "Int", Int64: "Long", Int: "Long",
	Uint8: "UByte", Uint16: "UShort", Uint32: "UInt", Uint64: "ULong", Uint: "ULong",
	Float32: "Float", Float64: "Double", String: "String", Bytes: "ByteArray",
	Time: "java.time.Instant", Duration: "kotlin.time.Duration",
}

var kotlinZero = map[Kind]string{
	Bool: "false", Int8: "0", Int16: "0", Int32: "0", Int64: "0L", Int: "0L",
	Uint8: "0u", Uint16: "0u", Uint32: "0u", Uint64: "0uL", Uint: "0uL",
	Float32: "0f", Float64: "0.0", String: `""`, Bytes: "ByteArray(0)",
	Time: "java.time.Instant.EPOCH", Duration: "kotlin.time.Duration.ZERO",
}

type kotlinGen struct {
	p       *Package
	b       bytes.Buffer
	helpers map[string]*Type
	enums   map[string]*EnumDecl
	structs map[string]*StructDecl
	ifaces  map[string]*Interface
}

func (g *kotlinGen) printf(format string, args ...any) { fmt.Fprintf(&g.b, format, args...) }

func (g *kotlinGen) typ(t *Type) string {
	switch t.Kind {
	case Slice:
		return "List<" + g.typ(t.Elem) + ">"
	case Map:
		return "Map<String, " + g.typ(t.Elem) + ">"
	case Optional:
		return g.typ(t.Elem) + "?"
	case Struct, Enum, Object:
		return t.Name
	case Func:
		var ps []string
		for _, p := range t.Func.Params {
			ps = append(ps, g.typ(p.Type))
		}
		r := "kotlin.Unit"
		if t.Func.Result != nil {
			r = g.typ(t.Func.Result)
		}
		return "(" + strings.Join(ps, ", ") + ") -> " + r
	}
	return kotlinOf[t.Kind]
}

func (g *kotlinGen) paramType(t *Type) string {
	if t.Kind == Object {
		return t.Name + "?"
	}
	return g.typ(t)
}

func (g *kotlinGen) zero(t *Type) string {
	switch t.Kind {
	case Slice:
		return "emptyList()"
	case Map:
		return "emptyMap()"
	case Optional:
		return "null"
	case Struct:
		return t.Name + "()"
	case Enum:
		return t.Name + "(" + kotlinZero[g.enums[t.Name].Basic] + ")"
	case Object:
		return ""
	}
	return kotlinZero[t.Kind]
}

func (g *kotlinGen) enc(t *Type, w, v string) string {
	switch t.Kind {
	case Enum:
		return fmt.Sprintf("%s.%s(%s.value)", w, swiftWire[g.enums[t.Name].Basic], v)
	case Slice, Map, Optional, Struct, Object:
		g.need(t)
		return fmt.Sprintf("Wire.write%s(%s, %s)", mangle(t), w, v)
	}
	return fmt.Sprintf("%s.%s(%s)", w, swiftWire[t.Kind], v)
}

func (g *kotlinGen) dec(t *Type, r string) string {
	switch t.Kind {
	case Enum:
		return fmt.Sprintf("%s(%s.%s())", t.Name, r, swiftWire[g.enums[t.Name].Basic])
	case Slice, Map, Optional, Struct, Object:
		g.need(t)
		return fmt.Sprintf("Wire.read%s(%s)", mangle(t), r)
	}
	return fmt.Sprintf("%s.%s()", r, swiftWire[t.Kind])
}

func (g *kotlinGen) need(t *Type) {
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

func kotlinDoc(doc, indent string) string {
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

// kotlinString is a Go constant's string value as a Kotlin literal.
func kotlinString(goLit string) string {
	s, err := strconv.Unquote(goLit)
	if err != nil {
		s = goLit
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\', '$':
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
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// GenerateKotlin writes the package's Kotlin side, for its android/
// module.
func GenerateKotlin(p *Package) []byte {
	g := &kotlinGen{p: p, helpers: map[string]*Type{}, enums: map[string]*EnumDecl{}, structs: map[string]*StructDecl{}, ifaces: map[string]*Interface{}}
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
	g.printf("// What the package's Go declares (package %s), as its Kotlin implements\n", p.Name)
	g.printf("// it. The Kotlin (in this package) writes the functions that make each:\n//\n")
	for _, it := range p.Interfaces {
		g.printf("//\tfun make%s(): %s\n", it.Name, it.Name)
	}
	for _, v := range p.Views {
		g.printf("//\tfun make%s(events: %sEvents): %s\n", v.Name, v.Name, v.Name)
	}
	g.printf("@file:Suppress(\"unused\", \"RedundantVisibilityModifier\")\n\npackage %s\n\n", KotlinPackage(p.Path))
	g.printf("import dev.asuka.AsukaModuleProvider\nimport dev.asuka.AsukaModules\nimport dev.asuka.AsukaNativeContext\nimport dev.asuka.AsukaNativeReader\nimport dev.asuka.AsukaNativeView\nimport dev.asuka.AsukaNativeWriter\nimport dev.asuka.AsukaDeclaredError\nimport dev.asuka.AsukaUnsupportedException\nimport kotlinx.coroutines.Dispatchers\nimport kotlinx.coroutines.Job\nimport kotlinx.coroutines.launch\n\n")
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
		g.iface(it)
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

func (g *kotlinGen) enum(e *EnumDecl) {
	raw := kotlinOf[e.Basic]
	g.printf("%s@JvmInline\npublic value class %s(val value: %s) {\n    companion object {\n", kotlinDoc(e.Doc, ""), e.Name, raw)
	for _, v := range e.Values {
		val := v.Value
		switch e.Basic {
		case String:
			val = kotlinString(val)
		case Int64, Int:
			val += "L"
		case Uint8, Uint16, Uint32:
			val += "u"
		case Uint64, Uint:
			val += "uL"
		case Int8:
			val = "(" + val + ").toByte()"
		case Int16:
			val = "(" + val + ").toShort()"
		}
		g.printf("        val %s = %s(%s)\n", kotlinName(upperSnake(enumCase(e.Name, v.Name))), e.Name, val)
	}
	g.printf("    }\n}\n\n")
}

func (g *kotlinGen) structDecl(s *StructDecl) {
	var fields []string
	for _, f := range s.Fields {
		fl := fmt.Sprintf("%s    val %s: %s", kotlinDoc(f.Doc, "    "), kotlinName(lowerCamel(f.Name)), g.typ(f.Type))
		if z := g.zero(f.Type); z != "" {
			fl += " = " + z
		}
		fields = append(fields, fl)
	}
	g.printf("%spublic data class %s(\n%s,\n)\n\n", kotlinDoc(s.Doc, ""), s.Name, strings.Join(fields, ",\n"))
}

func (g *kotlinGen) errors() {
	t, x := KotlinErrorType(g.p), KotlinException(g.p)
	g.printf("/** The errors the package declares (native.NewError), which its Go gets as those values. */\n")
	g.printf("public enum class %s(val message: String) {\n", t)
	for i, e := range g.p.Errors {
		sep := ","
		if i == len(g.p.Errors)-1 {
			sep = ";"
		}
		g.printf("    %s(%s)%s\n", upperSnake(errorCase(e.Name)), kotlinString(strconv.Quote(e.Message)), sep)
	}
	g.printf("}\n\n/** Throws one of the package's declared errors. */\npublic class %s(val error: %s) : Exception(error.message), AsukaDeclaredError {\n    override val asukaIndex: Int get() = error.ordinal\n}\n\n", x, t)
}

func (g *kotlinGen) signature(m *Method) string {
	var ps []string
	for _, p := range m.Params {
		ps = append(ps, kotlinName(p.Name)+": "+g.paramType(p.Type))
	}
	s := "fun " + kotlinName(lowerCamel(m.Name)) + "(" + strings.Join(ps, ", ") + ")"
	if m.Context {
		s = "suspend " + s
	}
	if m.Result != nil {
		s += ": " + g.typ(m.Result)
	}
	return s
}

func (g *kotlinGen) iface(it *Interface) {
	g.printf("%spublic interface %s {\n", kotlinDoc(it.Doc, ""), it.Name)
	for _, m := range it.Methods {
		doc := m.Doc
		if m.MainThread {
			if doc != "" {
				doc += "\n\n"
			}
			doc += "Called on the main thread."
		}
		g.printf("%s    %s\n", kotlinDoc(doc, "    "), g.signature(m))
	}
	g.printf("}\n\n")
}

func (g *kotlinGen) viewDecl(v *View) {
	g.printf("%spublic interface %s {\n    /** The platform's view, which the host shows. */\n    val view: android.view.View\n", kotlinDoc(v.Doc, ""), v.Name)
	for _, f := range v.Props {
		g.printf("%s    fun set%s(value: %s)\n", kotlinDoc(f.Doc, "    "), f.Name, g.typ(f.Type))
	}
	g.printf("}\n\n")
	g.printf("/** Sends a %s's events to its Go, and has the app's themed context to make views with. */\n", v.Name)
	g.printf("public class %sEvents internal constructor(private val ctx: AsukaNativeContext) {\n    val context: android.content.Context get() = ctx.context\n", v.Name)
	for _, e := range v.Events {
		var ps []string
		for _, a := range e.Type.Func.Params {
			ps = append(ps, kotlinName(a.Name)+": "+g.typ(a.Type))
		}
		g.printf("\n%s    fun %s(%s) {\n        val w = AsukaNativeWriter()\n", kotlinDoc(e.Doc, "    "), kotlinName(lowerCamel(e.Name)), strings.Join(ps, ", "))
		for _, a := range e.Type.Func.Params {
			g.printf("        %s\n", g.enc(a.Type, "w", kotlinName(a.Name)))
		}
		g.printf("        ctx.emit(%q, mapOf(\"v\" to w.toByteArray()))\n    }\n", e.Name)
	}
	g.printf("}\n\n")
	g.printf("private class AsukaView%s(ctx: AsukaNativeContext) : AsukaNativeView {\n    private val impl = make%s(%sEvents(ctx))\n", v.Name, v.Name, v.Name)
	g.printf("    override val view: android.view.View get() = impl.view\n\n")
	g.printf("    override fun set(name: String, value: Any) {\n        val b = value as? ByteArray ?: return\n        val r = AsukaNativeReader(b)\n        when (name) {\n")
	for _, f := range v.Props {
		g.printf("            %q -> {\n                val v = %s\n                if (!r.failed) impl.set%s(v)\n            }\n", f.Name, g.dec(f.Type, "r"), f.Name)
	}
	g.printf("        }\n    }\n}\n\n")
}

func (g *kotlinGen) provider() {
	g.printf("/** The package's native code as the host finds it (by its class's name): what makes, calls and shows what the package declares. */\n")
	g.printf("public object AsukaModule : AsukaModuleProvider {\n")
	g.printf("    override fun make(iface: Int): Any = when (iface) {\n")
	for _, it := range g.p.Interfaces {
		if it.MainThread {
			g.printf("        %d -> AsukaModules.onMain { make%s() }\n", it.Index, it.Name)
		} else {
			g.printf("        %d -> make%s()\n", it.Index, it.Name)
		}
	}
	g.printf("        else -> throw AsukaUnsupportedException(\"no interface $iface\")\n    }\n\n")
	g.printf("    override fun call(obj: Any, iface: Int, method: Int, r: AsukaNativeReader, w: AsukaNativeWriter) {\n        when (iface) {\n")
	for _, it := range g.p.Interfaces {
		g.printf("            %d -> {\n                val o = obj as %s\n                when (method) {\n", it.Index, it.Name)
		for _, m := range it.Methods {
			if m.Context {
				continue
			}
			g.printf("                    %d -> { // %s\n", m.Index, m.Name)
			args := g.decodeArgs(m, "                        ")
			g.printf("                        r.check()\n")
			call := fmt.Sprintf("o.%s(%s)", kotlinName(lowerCamel(m.Name)), args)
			if m.MainThread {
				call = fmt.Sprintf("AsukaModules.onMain { %s }", call)
			}
			if m.Result != nil {
				g.printf("                        val v = %s\n                        %s\n", call, g.enc(m.Result, "w", "v"))
			} else {
				g.printf("                        %s\n", call)
			}
			g.printf("                    }\n")
		}
		g.printf("                    else -> throw AsukaUnsupportedException(\"no method $iface.$method\")\n                }\n            }\n")
	}
	g.printf("            else -> throw AsukaUnsupportedException(\"no interface $iface\")\n        }\n    }\n\n")
	g.printf("    override fun start(obj: Any, iface: Int, method: Int, r: AsukaNativeReader, done: (Result<AsukaNativeWriter>) -> kotlin.Unit): Job? {\n        when (iface) {\n")
	for _, it := range g.p.Interfaces {
		var async []*Method
		for _, m := range it.Methods {
			if m.Context {
				async = append(async, m)
			}
		}
		if len(async) == 0 {
			continue
		}
		g.printf("            %d -> {\n                val o = obj as %s\n                when (method) {\n", it.Index, it.Name)
		for _, m := range async {
			g.printf("                    %d -> { // %s\n", m.Index, m.Name)
			args := g.decodeArgs(m, "                        ")
			g.printf("                        r.check()\n")
			ctx := ""
			if m.MainThread {
				ctx = "Dispatchers.Main"
			}
			g.printf("                        return AsukaModules.scope.launch(%s) {\n                            try {\n                                val w = AsukaNativeWriter.ok()\n", ctx)
			call := fmt.Sprintf("o.%s(%s)", kotlinName(lowerCamel(m.Name)), args)
			if m.Result != nil {
				g.printf("                                val v = %s\n                                %s\n", call, g.enc(m.Result, "w", "v"))
			} else {
				g.printf("                                %s\n", call)
			}
			g.printf("                                done(Result.success(w))\n                            } catch (e: Throwable) {\n                                done(Result.failure(e))\n                            }\n                        }\n                    }\n")
		}
		g.printf("                }\n            }\n")
	}
	g.printf("        }\n        throw AsukaUnsupportedException(\"no method $iface.$method\")\n    }\n\n")
	g.printf("    override fun makeView(name: String, ctx: AsukaNativeContext): AsukaNativeView? = when (name) {\n")
	for _, v := range g.p.Views {
		g.printf("        %q -> AsukaView%s(ctx)\n", v.Name, v.Name)
	}
	g.printf("        else -> null\n    }\n}\n\n")
}

func (g *kotlinGen) decodeArgs(m *Method, indent string) string {
	var args []string
	for _, p := range m.Params {
		n := kotlinName(p.Name)
		if p.Type.Kind == Func {
			g.closure(p.Name, p.Type.Func, indent)
		} else {
			g.printf("%sval %s = %s\n", indent, n, g.dec(p.Type, "r"))
		}
		args = append(args, n)
	}
	return strings.Join(args, ", ")
}

func (g *kotlinGen) closure(name string, ft *FuncType, indent string) {
	var ps []string
	for i := range ft.Params {
		ps = append(ps, "a"+strconv.Itoa(i))
	}
	id := strings.Trim(name, "`") + "ID"
	g.printf("%sval %s = r.callback()\n", indent, id)
	t := &Type{Kind: Func, Func: ft}
	in := ""
	if len(ps) > 0 {
		in = " " + strings.Join(ps, ", ") + " ->"
	}
	g.printf("%sval %s: %s = {%s\n%s    val w = AsukaNativeWriter()\n", indent, kotlinName(name), g.typ(t), in, indent)
	for i, p := range ft.Params {
		g.printf("%s    %s\n", indent, g.enc(p.Type, "w", "a"+strconv.Itoa(i)))
	}
	if ft.Result != nil {
		g.printf("%s    val a = AsukaModules.ask(%s, w)\n%s    %s\n", indent, id, indent, g.dec(ft.Result, "a"))
	} else {
		g.printf("%s    AsukaModules.notify(%s, w)\n", indent, id)
	}
	g.printf("%s}\n", indent)
}

func (g *kotlinGen) wire() {
	g.printf("/** The package's values on the wire (package native's format). */\nprivate object Wire {\n")
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

func (g *kotlinGen) helper(t *Type) {
	n, kt := mangle(t), g.typ(t)
	switch t.Kind {
	case Struct:
		s := g.structs[t.Name]
		g.printf("    fun write%s(w: AsukaNativeWriter, v: %s) {\n", n, kt)
		for _, f := range s.Fields {
			g.printf("        %s\n", g.enc(f.Type, "w", "v."+kotlinName(lowerCamel(f.Name))))
		}
		g.printf("    }\n\n    fun read%s(r: AsukaNativeReader): %s {\n", n, kt)
		var args []string
		for i, f := range s.Fields {
			g.printf("        val f%d = %s\n", i, g.dec(f.Type, "r"))
			args = append(args, fmt.Sprintf("%s = f%d", kotlinName(lowerCamel(f.Name)), i))
		}
		g.printf("        return %s(%s)\n    }\n\n", t.Name, strings.Join(args, ", "))
	case Slice:
		g.printf("    fun write%s(w: AsukaNativeWriter, v: %s) {\n        w.len(v.size)\n        for (e in v) %s\n    }\n\n", n, kt, g.enc(t.Elem, "w", "e"))
		g.printf("    fun read%s(r: AsukaNativeReader): %s {\n        val n = r.len()\n        return List(n) { %s }\n    }\n\n", n, kt, g.dec(t.Elem, "r"))
	case Map:
		g.printf("    fun write%s(w: AsukaNativeWriter, v: %s) {\n        w.len(v.size)\n        for ((k, e) in v) {\n            w.string(k)\n            %s\n        }\n    }\n\n", n, kt, g.enc(t.Elem, "w", "e"))
		g.printf("    fun read%s(r: AsukaNativeReader): %s {\n        val n = r.len()\n        val v = LinkedHashMap<String, %s>(n)\n        repeat(n) {\n            val k = r.string()\n            v[k] = %s\n        }\n        return v\n    }\n\n", n, kt, g.typ(t.Elem), g.dec(t.Elem, "r"))
	case Optional:
		g.printf("    fun write%s(w: AsukaNativeWriter, v: %s) {\n        w.present(v != null)\n        if (v != null) %s\n    }\n\n", n, kt, g.enc(t.Elem, "w", "v"))
		g.printf("    fun read%s(r: AsukaNativeReader): %s = if (r.present()) %s else null\n\n", n, kt, g.dec(t.Elem, "r"))
	case Object:
		it := g.ifaces[t.Name]
		g.printf("    fun write%s(w: AsukaNativeWriter, v: %s?) {\n        w.int64(if (v == null) 0L else AsukaModules.retain(v, AsukaModule, %d))\n    }\n\n", n, t.Name, it.Index)
		g.printf("    fun read%s(r: AsukaNativeReader): %s? = AsukaModules.obj(r.int64()) as? %s\n\n", n, t.Name, t.Name)
	}
}
