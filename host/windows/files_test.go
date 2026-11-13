package windows

import (
	"reflect"
	"testing"

	"github.com/egoist/asuka/internal/filetype"
)

// A second launch's directory and arguments reach the first as they were,
// spaces, quotes and an empty argument too.
func TestInstancePayload(t *testing.T) {
	args := []string{"--new-window", `C:\Users\a\My Notes\a "b".md`, "", "myapp://x"}
	dir, got := parseInstancePayload(instancePayload(`C:\Users\a`, args))
	if dir != `C:\Users\a` || !reflect.DeepEqual(got, args) {
		t.Fatalf("got %q, %q", dir, got)
	}
	dir, got = parseInstancePayload(instancePayload(`C:\`, nil))
	if dir != `C:\` || len(got) != 0 {
		t.Fatalf("no arguments: %q, %q", dir, got)
	}
}

// Each type is a ProgID with its icon and command, among its extensions'
// Open With programs and the app's supported types; an own type's
// extensions open with it unless they have a default.
func TestFileTypeValues(t *testing.T) {
	exe := `C:\Program Files\Notes\Notes.exe`
	got := fileTypeValues("com.example.notes", exe, []filetype.Type{
		{Name: "Note", Extensions: []string{"note", "NOTES"}, Own: true, Icon: 2},
		{Name: "Markdown", Extensions: []string{"md"}},
		{Name: "Nothing"},
	})
	want := []fileTypeKey{
		{key: `Software\Classes\com.example.notes.note`, value: "Note"},
		{key: `Software\Classes\com.example.notes.note\DefaultIcon`, value: `"C:\Program Files\Notes\Notes.exe",-2`},
		{key: `Software\Classes\com.example.notes.note\shell\open\command`, value: `"C:\Program Files\Notes\Notes.exe" "%1"`},
		{key: `Software\Classes\.note\OpenWithProgids`, name: "com.example.notes.note"},
		{key: `Software\Classes\.note`, value: "com.example.notes.note", ifUnset: true},
		{key: `Software\Classes\Applications\Notes.exe\SupportedTypes`, name: ".note"},
		{key: `Software\Classes\.notes\OpenWithProgids`, name: "com.example.notes.note"},
		{key: `Software\Classes\.notes`, value: "com.example.notes.note", ifUnset: true},
		{key: `Software\Classes\Applications\Notes.exe\SupportedTypes`, name: ".notes"},
		{key: `Software\Classes\com.example.notes.md`, value: "Markdown"},
		{key: `Software\Classes\com.example.notes.md\DefaultIcon`, value: `"C:\Program Files\Notes\Notes.exe",0`},
		{key: `Software\Classes\com.example.notes.md\shell\open\command`, value: `"C:\Program Files\Notes\Notes.exe" "%1"`},
		{key: `Software\Classes\.md\OpenWithProgids`, name: "com.example.notes.md"},
		{key: `Software\Classes\Applications\Notes.exe\SupportedTypes`, name: ".md"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fileTypeValues:\n%+v\nwant\n%+v", got, want)
	}
}

// A document opened again moves up; the list keeps the newest ten.
func TestAddRecent(t *testing.T) {
	var list []string
	for _, p := range []string{`C:\a.md`, `C:\b.md`, `c:\A.MD`} {
		list = addRecent(list, p)
	}
	if !reflect.DeepEqual(list, []string{`c:\A.MD`, `C:\b.md`}) {
		t.Fatalf("list = %q", list)
	}
	for i := range 20 {
		list = addRecent(list, string(rune('a'+i)))
	}
	if len(list) != maxRecent || list[0] != "t" {
		t.Fatalf("list = %q", list)
	}
}

// The Run value starts the executable with the login argument; it's the
// app's whatever the path's case, and Task Manager turns it off with an
// odd first byte.
func TestLogin(t *testing.T) {
	exe := `C:\Program Files\Notes\Notes.exe`
	cmd := loginCommand(exe, "--asuka-opened-at-login")
	if cmd != `"C:\Program Files\Notes\Notes.exe" --asuka-opened-at-login` {
		t.Fatalf("loginCommand = %q", cmd)
	}
	if !loginNamesExe(cmd, `c:\program files\notes\notes.exe`) || loginNamesExe(cmd, `C:\Other.exe`) {
		t.Fatal("loginNamesExe of a quoted command")
	}
	if !loginNamesExe(`C:\Notes.exe --x`, `C:\Notes.exe`) || loginNamesExe("", exe) || loginNamesExe(`"broken`, exe) {
		t.Fatal("loginNamesExe of an unquoted or broken command")
	}
	if !startupDisabled([]byte{3, 0}) || startupDisabled([]byte{2, 0}) || startupDisabled([]byte{6}) || startupDisabled(nil) {
		t.Fatal("startupDisabled")
	}
}
