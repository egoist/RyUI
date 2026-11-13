package main

import (
	"encoding/json"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/asuka/internal/filetype"
)

const fileTypesJSON = `{"name":"My Notes","id":"com.example.notes","icon":"icon.png","fileTypes":[
	{"name":"Markdown Document","extensions":["md","markdown"],"role":"viewer"},
	{"name":"Notes Document","extensions":[".note"],"icon":"note.png","conformsTo":["public.plain-text"]},
	{"extensions":["mydb"],"uti":"org.example.mydb","mimeTypes":["application/x-mydb"]}
]}`

// fileTypesApp is an app with three file types: one the systems know
// (Markdown), the app's own (.note, with an icon), and another's the
// systems don't know (org.example.mydb).
func fileTypesApp(t *testing.T) *goApp {
	t.Helper()
	app := linuxTestApp(t, fileTypesJSON)
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	for _, p := range []string{app.cfg.Icon, filepath.Join(app.dir, "note.png")} {
		if err := writePNG(p, img); err != nil {
			t.Fatal(err)
		}
	}
	return app
}

// The types are resolved: names, UTIs, MIME types, whose own they are.
func TestFileTypes(t *testing.T) {
	app := fileTypesApp(t)
	got := app.cfg.fileTypes()
	want := []fileType{
		{name: "Markdown Document", extensions: []string{"md", "markdown"}, mimeTypes: []string{"text/markdown"},
			uti: "net.daringfireball.markdown", conformsTo: []string{"public.data", "public.content"}, role: "Viewer", known: true},
		{name: "Notes Document", extensions: []string{"note"}, mimeTypes: []string{"application/x-my-notes-note"},
			uti: "com.example.notes.note", conformsTo: []string{"public.plain-text"}, role: "Editor",
			icon: filepath.Join(app.dir, "note.png"), own: true},
		{name: "MYDB Document", extensions: []string{"mydb"}, mimeTypes: []string{"application/x-mydb"},
			uti: "org.example.mydb", conformsTo: []string{"public.data", "public.content"}, role: "Editor"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fileTypes:\n%+v\nwant\n%+v", got, want)
	}
	for _, bad := range []string{
		`{"fileTypes":[{"name":"x"}]}`,
		`{"fileTypes":[{"extensions":["m d"]}]}`,
		`{"fileTypes":[{"extensions":["md"],"role":"owner"}]}`,
		`{"fileTypes":[{"extensions":["md"],"mimeTypes":["text"]}]}`,
	} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "asuka.json"), []byte(bad), 0o644)
		if _, err := loadConfig(dir); err == nil || !strings.Contains(err.Error(), "fileTypes[0]") {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

// Info.plist declares the types by UTI: the app owns its own, exports
// it, imports another's the system may not know, and leaves the
// system's alone; iOS opens documents in place.
func TestDocumentTypes(t *testing.T) {
	app := fileTypesApp(t)
	m := app.cfg.documentTypes(app.cfg.fileTypes(), false, map[int]string{1: "DocumentIcon2"})
	docs := m["CFBundleDocumentTypes"].([]any)
	if len(docs) != 3 {
		t.Fatalf("CFBundleDocumentTypes: %v", docs)
	}
	md, note := docs[0].(map[string]any), docs[1].(map[string]any)
	if md["LSHandlerRank"] != "Default" || md["CFBundleTypeRole"] != "Viewer" || !reflect.DeepEqual(md["LSItemContentTypes"], []string{"net.daringfireball.markdown"}) {
		t.Errorf("Markdown: %v", md)
	}
	if note["LSHandlerRank"] != "Owner" || note["CFBundleTypeIconFile"] != "DocumentIcon2" || !reflect.DeepEqual(note["LSItemContentTypes"], []string{"com.example.notes.note"}) {
		t.Errorf("the app's own: %v", note)
	}
	exp := m["UTExportedTypeDeclarations"].([]any)
	imp := m["UTImportedTypeDeclarations"].([]any)
	if len(exp) != 1 || len(imp) != 1 {
		t.Fatalf("exported %v, imported %v", exp, imp)
	}
	e := exp[0].(map[string]any)
	if e["UTTypeIdentifier"] != "com.example.notes.note" || !reflect.DeepEqual(e["UTTypeConformsTo"], []string{"public.plain-text"}) ||
		!reflect.DeepEqual(e["UTTypeTagSpecification"], map[string]any{"public.filename-extension": []string{"note"}, "public.mime-type": []string{"application/x-my-notes-note"}}) {
		t.Errorf("exported: %v", e)
	}
	if imp[0].(map[string]any)["UTTypeIdentifier"] != "org.example.mydb" {
		t.Errorf("imported: %v", imp)
	}
	if _, ok := m["LSSupportsOpeningDocumentsInPlace"]; ok {
		t.Error("a Mac's Info.plist opens in place")
	}
	if app.cfg.documentTypes(app.cfg.fileTypes(), true, nil)["LSSupportsOpeningDocumentsInPlace"] != true {
		t.Error("iOS doesn't open documents in place")
	}
	// iOS's Info.plist has them (dev and release take plistExtras).
	x := app.cfg.plistExtras()
	if x["CFBundleDocumentTypes"] == nil || x["LSSupportsOpeningDocumentsInPlace"] != true {
		t.Errorf("plistExtras: %v", x)
	}
	// asuka/sdk/library has the key already: once in the plist.
	app.cfg.iosTrue = []string{"LSSupportsOpeningDocumentsInPlace"}
	if _, ok := app.cfg.plistExtras()["LSSupportsOpeningDocumentsInPlace"]; ok {
		t.Error("LSSupportsOpeningDocumentsInPlace twice")
	}
	if p := plist(app.cfg.plistExtras()); !strings.Contains(p, "<string>com.example.notes.note</string>") {
		t.Errorf("the plist:\n%s", p)
	}
}

// A Mac build's bundle declares the types, each icon an .icns.
func TestMacBundleFileTypes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("iconutil is macOS's")
	}
	if _, err := exec.LookPath("iconutil"); err != nil {
		t.Skip(err)
	}
	app := fileTypesApp(t)
	bundle := filepath.Join(t.TempDir(), "My Notes.app")
	if _, err := writeMacBundle(app, bundle, t.TempDir(), false, macRunner(false)); err != nil {
		t.Fatal(err)
	}
	info, _ := os.ReadFile(filepath.Join(bundle, "Contents", "Info.plist"))
	for _, want := range []string{"<key>CFBundleDocumentTypes</key>", "<key>UTExportedTypeDeclarations</key>", "<string>DocumentIcon2</string>"} {
		if !strings.Contains(string(info), want) {
			t.Errorf("Info.plist has no %s", want)
		}
	}
	if _, err := os.Stat(filepath.Join(bundle, "Contents", "Resources", "DocumentIcon2.icns")); err != nil {
		t.Error(err)
	}
	if out, err := exec.Command("plutil", "-lint", filepath.Join(bundle, "Contents", "Info.plist")).CombinedOutput(); err != nil {
		t.Errorf("plutil: %s", out)
	}
}

// Android's activity views the types' MIME types, and the app's own by
// extension.
func TestAndroidFileTypes(t *testing.T) {
	app := fileTypesApp(t)
	m := app.cfg.androidManifest(false)
	for _, want := range []string{
		`<data android:mimeType="text/markdown" />`,
		`<data android:mimeType="application/x-mydb" />`,
		`<data android:pathPattern=".*\\.note" />`,
		`<data android:pathPattern=".*\\..*\\.note" />`,
		`<data android:scheme="content" />`,
	} {
		if !strings.Contains(m, want) {
			t.Errorf("the manifest has no %s:\n%s", want, m)
		}
	}
	if strings.Contains(m, `pathPattern=".*\\.md"`) {
		t.Error("a type the providers know by MIME type is matched by extension")
	}
	activity := m[strings.Index(m, "AsukaActivity"):]
	if end := strings.Index(activity, "</activity>"); !strings.Contains(activity[:end], "text/markdown") {
		t.Error("the filters aren't the main activity's")
	}
}

// Linux: the desktop file takes the types' MIME types and the files'
// URIs; the app defines its own types for shared-mime-info, with icons.
func TestLinuxFileTypes(t *testing.T) {
	app := fileTypesApp(t)
	out := filepath.Join(t.TempDir(), "my-notes")
	if err := writeLinuxShare(app, out, "my-notes"); err != nil {
		t.Fatal(err)
	}
	desktop, _ := os.ReadFile(filepath.Join(out, "share/applications/com.example.notes.desktop"))
	for _, want := range []string{"\nExec=my-notes %U\n", "\nMimeType=text/markdown;application/x-my-notes-note;application/x-mydb;\n"} {
		if !strings.Contains(string(desktop), want) {
			t.Errorf("the desktop file has no %q:\n%s", want, desktop)
		}
	}
	pkg, err := os.ReadFile(filepath.Join(out, "share/mime/packages/com.example.notes.xml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<mime-type type="application/x-my-notes-note">`, `<comment>Notes Document</comment>`,
		`<sub-class-of type="text/plain"/>`, `<icon name="application-x-my-notes-note"/>`, `<glob pattern="*.note"/>`,
		`<mime-type type="application/x-mydb">`, `<glob pattern="*.mydb"/>`,
	} {
		if !strings.Contains(string(pkg), want) {
			t.Errorf("the MIME package has no %s:\n%s", want, pkg)
		}
	}
	if strings.Contains(string(pkg), "text/markdown") {
		t.Errorf("the app defines a MIME type the system has:\n%s", pkg)
	}
	if _, err := os.Stat(filepath.Join(out, "share/icons/hicolor/48x48/mimetypes/application-x-my-notes-note.png")); err != nil {
		t.Error(err)
	}
}

// Windows: the build carries the types, which the host registers; a
// release's icons are the executable's resources.
func TestWindowsFileTypes(t *testing.T) {
	app := fileTypesApp(t)
	types := ldflagsFileTypes(t, windowsLdflags(app, true))
	want := []filetype.Type{
		{Name: "Markdown Document", Extensions: []string{"md", "markdown"}},
		{Name: "Notes Document", Extensions: []string{"note"}, Own: true, Icon: 102},
		{Name: "MYDB Document", Extensions: []string{"mydb"}},
	}
	if !reflect.DeepEqual(types, want) {
		t.Fatalf("boot.FileTypes: %+v", types)
	}
	if dev := ldflagsFileTypes(t, windowsLdflags(app, false)); dev[1].Icon != 0 {
		t.Error("a dev build's types name icons it hasn't")
	}
	b, err := winresSyso(app, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	// The type's icon group is resource 102 (fileTypeIconID(1)): its
	// directory entry's ID.
	if !strings.Contains(string(b), "\x66\x00\x00\x00") {
		t.Error("no group icon 102 in the resources")
	}
}

// ldflagsFileTypes are the types -ldflags sets boot.FileTypes to.
func ldflagsFileTypes(t *testing.T, flags string) []filetype.Type {
	t.Helper()
	_, enc, ok := strings.Cut(flags, "boot.FileTypes=")
	if !ok {
		t.Fatalf("no boot.FileTypes in %s", flags)
	}
	enc, _, _ = strings.Cut(enc, `"`)
	return filetype.Decode(enc)
}

// The web manifest's file handlers accept the types.
func TestWebFileHandlers(t *testing.T) {
	app := fileTypesApp(t)
	fh := webFileHandlers(app.cfg.fileTypes())
	b, _ := json.Marshal(fh)
	want := `[{"accept":{"application/x-my-notes-note":[".note"],"application/x-mydb":[".mydb"],"text/markdown":[".md",".markdown"]},"action":"./","launch_type":"single-client"}]`
	if string(b) != want {
		t.Errorf("file_handlers:\n%s\nwant\n%s", b, want)
	}
	if webFileHandlers(nil) != nil {
		t.Error("file handlers without types")
	}
}
