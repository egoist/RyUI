package nativegen

import (
	"strings"
	"unicode"
)

// lowerCamel is a Go name as Swift, Kotlin and JavaScript name members:
// Level → level, ID → id, URLString → urlString, HTTPServer → httpServer.
func lowerCamel(s string) string {
	r := []rune(s)
	n := 0
	for n < len(r) && unicode.IsUpper(r[n]) {
		n++
	}
	switch {
	case n == 0:
		return s
	case n == len(r):
		return strings.ToLower(s)
	case n > 1:
		n-- // the last capital starts the next word: URLString → url + String
	}
	return strings.ToLower(string(r[:n])) + string(r[n:])
}

// upperCamel is a name as a type: battery → Battery, go-battery → GoBattery.
func upperCamel(s string) string {
	var b strings.Builder
	up := true
	for _, c := range s {
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) {
			up = true
			continue
		}
		if up {
			b.WriteRune(unicode.ToUpper(c))
			up = false
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}

// upperSnake is a name as a Kotlin enum entry: LowPower → LOW_POWER.
func upperSnake(s string) string {
	var b strings.Builder
	r := []rune(s)
	for i, c := range r {
		if i > 0 && unicode.IsUpper(c) && (unicode.IsLower(r[i-1]) || i+1 < len(r) && unicode.IsLower(r[i+1]) && unicode.IsUpper(r[i-1])) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToUpper(c))
	}
	return b.String()
}

// enumCase is an enum constant's name without its type's: UnitCelsius
// (of Unit) → Celsius.
func enumCase(typ, constant string) string {
	if c, ok := strings.CutPrefix(constant, typ); ok && c != "" && unicode.IsUpper([]rune(c)[0]) {
		return c
	}
	return constant
}

// errorCase is a declared error's name as an error case: ErrLowPower → LowPower.
func errorCase(name string) string { return strings.TrimPrefix(name, "Err") }

var swiftKeywords = set("associatedtype class deinit enum extension fileprivate func import init inout internal let open operator private precedencegroup protocol public rethrows static struct subscript typealias var break case catch continue default defer do else fallthrough for guard if in repeat return throw switch where while Any as await false is nil self Self super throws true try")

var kotlinKeywords = set("as break class continue do else false for fun if in interface is null object package return super this throw true try typealias typeof val var when while")

var jsKeywords = set("arguments await break case catch class const continue debugger default delete do else enum eval export extends false finally for function if implements import in instanceof interface let new null package private protected public return static super switch this throw true try typeof var void while with yield")

func set(s string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

// swiftName escapes a Swift keyword.
func swiftName(s string) string {
	if swiftKeywords[s] {
		return "`" + s + "`"
	}
	return s
}

// kotlinName escapes a Kotlin keyword.
func kotlinName(s string) string {
	if kotlinKeywords[s] {
		return "`" + s + "`"
	}
	return s
}

// jsName escapes a JavaScript reserved word.
func jsName(s string) string {
	if jsKeywords[s] {
		return s + "_"
	}
	return s
}

// SwiftClass is the Objective-C name of a module's Swift side, which the
// hosts find it by: AsukaModule_ and its import path's letters and
// digits.
func SwiftClass(importPath string) string { return "AsukaModule_" + Sanitize(importPath) }

// Sanitize is s with only its letters and digits, the rest _.
func Sanitize(s string) string { return sanitize(s) }

func sanitize(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c < 128 && (unicode.IsLetter(c) || unicode.IsDigit(c)) {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// KotlinPackage is the package of a module's Kotlin, which the package's
// own Kotlin is in too: its import path's host reversed, then its path:
// github.com/acme/go-battery → com.github.acme.go_battery.
func KotlinPackage(importPath string) string {
	parts := strings.Split(importPath, "/")
	var segs []string
	if strings.Contains(parts[0], ".") {
		host := strings.Split(parts[0], ".")
		for i := len(host) - 1; i >= 0; i-- {
			segs = append(segs, host[i])
		}
		parts = parts[1:]
	}
	segs = append(segs, parts...)
	for i, s := range segs {
		s = strings.ToLower(sanitize(s))
		if s == "" || unicode.IsDigit([]rune(s)[0]) {
			s = "_" + s
		}
		if kotlinKeywords[s] || javaKeywords[s] {
			s += "_"
		}
		segs[i] = s
	}
	return strings.Join(segs, ".")
}

var javaKeywords = set("abstract assert boolean break byte case catch char class const continue default do double else enum extends final finally float for goto if implements import instanceof int interface long native new package private protected public return short static strictfp super switch synchronized this throw throws transient try void volatile while")

// KotlinClass is the class of a module's Kotlin side, which the Android
// host finds it by.
func KotlinClass(importPath string) string { return KotlinPackage(importPath) + ".AsukaModule" }
