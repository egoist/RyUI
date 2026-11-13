package windows

import "strings"

// Links into the app, apart from the host (it builds anywhere, so it's
// tested anywhere). An app with an ID (asuka.json's, set by the Asuka CLI)
// runs once: a second launch hands the first its URL, or brings it to the
// front, and quits (deeplink_windows.go). An app with a scheme
// (asuka.json's "scheme") is its URLs' handler: registered for the user as
// it starts, its command line `"app.exe" "myapp://..."`.

// instanceTag is the WM_COPYDATA dwData of a launch handed to the running
// instance: "Asuka".
const instanceTag = 0x52795549

// instanceNames are the mutex that says an app of id runs, and the title
// of its message window, which a second launch finds by.
func instanceNames(id string) (mutex, title string) {
	// A mutex's name takes no backslash past its namespace.
	safe := strings.ReplaceAll(id, `\`, "_")
	return `Local\Asuka.` + safe, "Asuka instance " + id
}

// singleInstance reports whether an app of id runs once: one with an ID,
// run as itself.
func singleInstance(id string, getenv func(string) string) bool {
	return id != "" && !notItself(getenv)
}

// notItself reports whether the app runs for something else than its
// user, so that it neither hands its launch to an instance that runs, nor
// takes the user's links: automation (snapshots, scripts, commands: tests
// and the e2e runs, which start it again and again) and asuka dev (whose
// runs keep their state, ASUKA_STATE: one beside an installed copy of the
// app mustn't hand itself over to it, nor take its scheme), as on Linux.
func notItself(getenv func(string) string) bool {
	for _, k := range []string{"ASUKA_SNAPSHOT", "ASUKA_SCRIPT", "ASUKA_COMMANDS", "ASUKA_STATE"} {
		if getenv(k) != "" {
			return true
		}
	}
	return false
}

// launchURL is the URL of scheme's the app was started with (args:
// os.Args), or "".
func launchURL(args []string, scheme string) string {
	if scheme == "" {
		return ""
	}
	for _, a := range args[min(1, len(args)):] {
		if len(a) > len(scheme) && strings.EqualFold(a[:len(scheme)], scheme) && a[len(scheme)] == ':' {
			return a
		}
	}
	return ""
}

// urlSchemeValues are the registry values, under HKEY_CURRENT_USER, that
// make exe the handler of scheme's URLs, named name: each a key, a
// value's name ("": the key's default) and its value.
func urlSchemeValues(scheme, name, exe string) [][3]string {
	key := `Software\Classes\` + scheme
	return [][3]string{
		{key, "", "URL:" + name},
		{key, "URL Protocol", ""},
		{key + `\DefaultIcon`, "", `"` + exe + `",0`},
		{key + `\shell\open\command`, "", `"` + exe + `" "%1"`},
	}
}
