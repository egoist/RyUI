package glsl

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/egoist/ryui/internal/shaderir"
)

// AGSL returns p's fragment entry point as an AGSL program, Android's
// RuntimeShader language (RyUI's): its uniforms U0, U1, ... (no textures),
// the functions it reaches, and the main that calls the entry point,
//
//	half4 main(float2 p)
//
// with the position p in the view's pixels (the target's, from its top
// left), and the varying the bridge passes, the position in points, from
// it: the uniform scaleUniform holds the pixels a point.
//
// AGSL is GLSL ES 1.00's language, strictly: an error says what of p it
// can't run (unsigned integers, bitwise operations and integer remainders,
// derivatives, images, loops between values that aren't constants), where
// the other backends could.
func AGSL(p *shaderir.Program, scaleUniform int) (string, error) {
	lib, entry, funcs := p.FragmentLibrary()
	if entry == nil {
		return "", fmt.Errorf("agsl: no fragment entry point")
	}
	if err := agslCheck(lib, funcs); err != nil {
		return "", err
	}
	if len(entry.InParams) != 2 {
		return "", fmt.Errorf("agsl: the entry point takes %d values, not the position and the varying", len(entry.InParams))
	}
	c := &compileContext{version: GLSLVersionES300, projectionMatrixUniformIndex: -1, structNames: map[string]string{}, library: true}
	lines := []string{"{{.Structs}}", ""}
	for i, t := range lib.Uniforms {
		lines = append(lines, fmt.Sprintf("uniform %s;", c.varDecl(lib, &t, fmt.Sprintf("U%d", i))))
	}
	lines = append(lines, "")
	for _, f := range funcs {
		lines = append(lines, c.function(lib, f, true)...)
	}
	for _, f := range funcs {
		lines = append(lines, "")
		lines = append(lines, c.function(lib, f, false)...)
	}
	lines = append(lines, "",
		"half4 main(float2 p) {",
		fmt.Sprintf("\treturn half4(F%d(vec4(p, 0.0, 1.0), p / U%d));", entry.Index, scaleUniform),
		"}")
	ls := strings.Join(lines, "\n")
	var st []string
	for i, t := range c.structTypes {
		st = append(st, fmt.Sprintf("struct S%d {", i))
		for j, m := range t.Sub {
			st = append(st, fmt.Sprintf("\t%s;", c.varDecl(lib, &m, fmt.Sprintf("M%d", j))))
		}
		st = append(st, "};")
	}
	ls = strings.ReplaceAll(ls, "{{.Structs}}", strings.Join(st, "\n"))
	ls = strings.ReplaceAll(ls, "(void)", "()") // a function of no parameters, as SkSL takes it surely
	ls = regexp.MustCompile(`\n\n+`).ReplaceAllString(ls, "\n\n")
	return strings.TrimSpace(ls) + "\n", nil
}

// agslCheck reports what of the functions AGSL can't run.
func agslCheck(p *shaderir.Program, funcs []*shaderir.Func) error {
	unsigned := func(t *shaderir.Type) bool {
		for {
			switch t.Main {
			case shaderir.Uint, shaderir.UVec2, shaderir.UVec3, shaderir.UVec4:
				return true
			case shaderir.Array:
				t = &t.Sub[0]
				continue
			case shaderir.Struct:
				for i := range t.Sub {
					if unsignedType(&t.Sub[i]) {
						return true
					}
				}
			}
			return false
		}
	}
	for i := range p.Uniforms {
		if unsigned(&p.Uniforms[i]) {
			return fmt.Errorf("agsl: %s is unsigned: AGSL has no unsigned integers", p.UniformNames[i])
		}
		if p.Uniforms[i].Main == shaderir.Bool {
			return fmt.Errorf("agsl: %s is a bool: AGSL has no bool uniforms (an int or a float can say it)", p.UniformNames[i])
		}
	}
	var expr func(e *shaderir.Expr) error
	expr = func(e *shaderir.Expr) error {
		switch {
		case e.Unsigned:
			return fmt.Errorf("agsl: an unsigned integer: AGSL has none")
		case e.Type == shaderir.TextureVariable || e.Type == shaderir.BuiltinFuncExpr && e.BuiltinFunc == shaderir.TexelAt:
			return fmt.Errorf("agsl: an image read: ui.Shader reads no images")
		case e.Type == shaderir.BuiltinFuncExpr && (e.BuiltinFunc == shaderir.Dfdx || e.BuiltinFunc == shaderir.Dfdy || e.BuiltinFunc == shaderir.Fwidth):
			return fmt.Errorf("agsl: %s: AGSL has no derivatives", e.BuiltinFunc)
		case e.Type == shaderir.BuiltinFuncExpr && e.BuiltinFunc == shaderir.FrontFacing:
			return fmt.Errorf("agsl: frontfacing: AGSL has none")
		case e.Type == shaderir.Unary && e.Op == shaderir.ComplementOp,
			e.Type == shaderir.Binary && (e.Op == shaderir.LeftShift || e.Op == shaderir.RightShift || e.Op == shaderir.And ||
				e.Op == shaderir.AndNot || e.Op == shaderir.Or || e.Op == shaderir.Xor):
			return fmt.Errorf("agsl: a bitwise operation: AGSL has none")
		case e.Type == shaderir.Binary && e.Op == shaderir.ModOp:
			return fmt.Errorf("agsl: an integer remainder (%%): AGSL has none (mod, of floats, does it)")
		}
		for i := range e.Exprs {
			if err := expr(&e.Exprs[i]); err != nil {
				return err
			}
		}
		return nil
	}
	var block func(b *shaderir.Block) error
	block = func(b *shaderir.Block) error {
		if b == nil {
			return nil
		}
		for i := range b.LocalVars {
			if unsigned(&b.LocalVars[i]) {
				return fmt.Errorf("agsl: a variable is unsigned: AGSL has no unsigned integers")
			}
		}
		for i := range b.Stmts {
			s := &b.Stmts[i]
			if s.Type == shaderir.For && (s.ForInit == nil || s.ForEnd == nil) {
				return fmt.Errorf("agsl: a loop between values that aren't constants: AGSL's loops run between constants")
			}
			for j := range s.Exprs {
				if err := expr(&s.Exprs[j]); err != nil {
					return err
				}
			}
			for _, sub := range s.Blocks {
				if err := block(sub); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, f := range funcs {
		for i := range f.InParams {
			if unsigned(&f.InParams[i]) {
				return fmt.Errorf("agsl: a function takes an unsigned integer: AGSL has none")
			}
		}
		if unsigned(&f.Return) {
			return fmt.Errorf("agsl: a function returns an unsigned integer: AGSL has none")
		}
		if err := block(f.Block); err != nil {
			return err
		}
	}
	return nil
}

func unsignedType(t *shaderir.Type) bool {
	switch t.Main {
	case shaderir.Uint, shaderir.UVec2, shaderir.UVec3, shaderir.UVec4:
		return true
	}
	return false
}
