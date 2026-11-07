package rtf

import (
	"reflect"
	"testing"
)

var doc = Doc{Face: "Segoe UI", MonoFace: "Consolas", Size: 10.5, Color: Color{0x1b, 0x1b, 0x1b}}

func TestRoundTrip(t *testing.T) {
	blue := &Color{0, 0, 255}
	runs := []Run{
		{Text: "Hi,\n\nThe "},
		{Text: "1.0", Bold: true},
		{Text: " build is "},
		{Text: "ready", Italic: true, Underline: true},
		{Text: ". Notes: "},
		{Text: "here", Link: `https://example.com/a?b="c"`},
		{Text: " — ", Strike: true},
		{Text: `code{}\`, Mono: true},
		{Text: " 👋 colored", Color: blue},
		{Text: "\nend"},
	}
	b := Write(runs, doc)
	got := Parse(b, doc)
	runs[5].Link = "https://example.com/a?b=%22c%22"
	if !reflect.DeepEqual(got, runs) {
		t.Fatalf("got\n%#v\nwant\n%#v\n%s", got, runs, b)
	}
}

// What a rich edit control streams out: its header, a font and color
// table, friendly links, \'hh and \u escapes, a final \par.
func TestRichEditOutput(t *testing.T) {
	bs := `\` // \ + u escapes, written apart
	src := `{\rtf1\ansi\ansicpg1252\deff0\nouicompat\deflang1033{\fonttbl{\f0\fnil\fcharset0 Segoe UI;}{\f1\fmodern\fprq1\fcharset0 Consolas;}}
{\colortbl ;\red27\green27\blue27;\red0\green102\blue204;\red255\green255\blue0;}
{\*\generator Riched20 10.0.22621}\viewkind4\uc1
\pard\cf1\f0\fs21\lang9 Caf\'e9 \b bold\b0  and \i it\i0 , \ul under\ulnone  \strike gone\strike0  \f1 mono\f0  \cf2 blue\cf1  \highlight3 hi\highlight0\par
{\field{\*\fldinst{HYPERLINK "https://go.dev" }}{\fldrslt{go}}}\f0\fs21  ` + bs + `u20320?` + bs + `u22909?\par
}`
	got := Parse([]byte(src), doc)
	want := []Run{
		{Text: "Café "},
		{Text: "bold", Bold: true},
		{Text: " and "},
		{Text: "it", Italic: true},
		{Text: ", "},
		{Text: "under", Underline: true},
		{Text: " "},
		{Text: "gone", Strike: true},
		{Text: " "},
		{Text: "mono", Mono: true},
		{Text: " "},
		{Text: "blue", Color: &Color{0, 102, 204}},
		{Text: " "},
		{Text: "hi", Background: &Color{255, 255, 0}},
		{Text: "\n"},
		{Text: "go", Link: "https://go.dev"},
		{Text: " 你好"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got\n%#v", got)
	}
}

// A font named as the document's monospaced face is monospaced, with or
// without \fmodern.
func TestMonoByName(t *testing.T) {
	got := Parse([]byte(`{\rtf1{\fonttbl{\f0\fnil Segoe UI;}{\f1\fnil Consolas;}}\f0 a \f1 b}`), doc)
	if len(got) != 2 || got[1].Text != "b" || !got[1].Mono || got[0].Mono {
		t.Fatalf("%#v", got)
	}
}
