package windows

import (
	"reflect"
	"testing"
)

// The scheme's handler is the app for the user: a URL protocol whose
// command passes the URL, quoted, to the executable.
func TestURLSchemeValues(t *testing.T) {
	got := urlSchemeValues("myapp", "My App", `C:\Program Files\My App\My App.exe`)
	want := [][3]string{
		{`Software\Classes\myapp`, "", "URL:My App"},
		{`Software\Classes\myapp`, "URL Protocol", ""},
		{`Software\Classes\myapp\DefaultIcon`, "", `"C:\Program Files\My App\My App.exe",0`},
		{`Software\Classes\myapp\shell\open\command`, "", `"C:\Program Files\My App\My App.exe" "%1"`},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("urlSchemeValues = %q, want %q", got, want)
	}
}

// An app with an ID runs once, unless automation or asuka dev runs it;
// its mutex is the session's.
func TestSingleInstance(t *testing.T) {
	env := func(kv ...string) func(string) string {
		return func(k string) string {
			for i := 0; i+1 < len(kv); i += 2 {
				if kv[i] == k {
					return kv[i+1]
				}
			}
			return ""
		}
	}
	if !singleInstance("com.example.app", env()) {
		t.Error("an app with an ID runs more than once")
	}
	if singleInstance("", env()) {
		t.Error("an app without an ID (go run) runs once")
	}
	for _, k := range []string{"ASUKA_SNAPSHOT", "ASUKA_SCRIPT", "ASUKA_COMMANDS", "ASUKA_STATE"} {
		if singleInstance("com.example.app", env(k, "x")) {
			t.Errorf("an app run with %s runs once", k)
		}
	}
	mutex, title := instanceNames(`com.example.app`)
	if mutex != `Local\Asuka.com.example.app` || title != "Asuka instance com.example.app" {
		t.Errorf("instanceNames = %q, %q", mutex, title)
	}
	if mutex, _ := instanceNames(`a\b`); mutex != `Local\Asuka.a_b` {
		t.Errorf("a backslash in the mutex's name: %q", mutex)
	}
}
