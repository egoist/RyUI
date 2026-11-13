package main

import (
	"fmt"
	"html"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/egoist/asuka/internal/filetype"
)

// asuka.json's fileTypes: the kinds of files the app opens, which
// ui.OnOpenFiles gets. Each build declares them as its platform does:
//
//   - macOS and iOS: Info.plist's CFBundleDocumentTypes (by UTI), the
//     app's own types exported (UTExportedTypeDeclarations), others the
//     system doesn't know imported; on iOS, LSSupportsOpeningDocumentsInPlace
//     (Files opens a document where it is).
//   - Android: intent filters for ACTION_VIEW of their MIME types, and of
//     their extensions for the app's own types, which no provider knows.
//   - Windows: a ProgID per type and the extensions' OpenWithProgids,
//     registered for the user as the app starts (as its scheme is), the
//     types' icons in the executable's resources.
//   - Linux: the desktop file's MimeType, the app's own types defined for
//     shared-mime-info (share/mime/packages/<id>.xml), their icons in the
//     hicolor theme's mimetypes.
//   - The web: the manifest's file_handlers (an installed app's, in
//     Chromium), which the host reads from the launch queue.
//
//	"fileTypes": [
//	  {"name": "Markdown Document", "extensions": ["md", "markdown"]},
//	  {"name": "Notes Document", "extensions": ["note"], "icon": "note.png"}
//	]

// fileTypeConfig is one of asuka.json's fileTypes.
type fileTypeConfig struct {
	// Name is what the files are, as the system shows it ("Markdown
	// Document"). Default: the first extension in capitals and "Document".
	Name string `json:"name"`
	// Extensions are the files' name extensions, without the dot.
	Extensions []string `json:"extensions"`
	// MIMETypes are their MIME types (default: those Asuka knows for the
	// extensions; for the app's own types, application/x-<app>-<ext>).
	MIMETypes []string `json:"mimeTypes"`
	// UTI is their Uniform Type Identifier on Apple's platforms (default:
	// the system's for an extension Asuka knows, else <id>.<extension>).
	// One under the app's ID is the app's own type: exported, and the app
	// is its files' owner everywhere. Another the system may not know is
	// imported (declared, with its extensions and MIME types).
	UTI string `json:"uti"`
	// ConformsTo are the UTIs an exported or imported type conforms to
	// (default: public.data and public.content; public.plain-text's
	// conformance makes Quick Look and text editors take it).
	ConformsTo []string `json:"conformsTo"`
	// Role is what the app does with them: "editor" (default) or "viewer"
	// (macOS's and iOS's CFBundleTypeRole).
	Role string `json:"role"`
	// Icon is a square PNG of the files' icon (default: the system's for
	// the type, or the app's). macOS, Windows and Linux show it.
	Icon string `json:"icon"`
}

// knownType is what the systems call a file type Asuka knows by its
// extension: its UTI (declared by Apple's platforms), MIME type
// (shared-mime-info's, IANA's) and the UTI it conforms to.
type knownType struct{ uti, mime string }

var knownTypes = map[string]knownType{
	"md":       {"net.daringfireball.markdown", "text/markdown"},
	"markdown": {"net.daringfireball.markdown", "text/markdown"},
	"txt":      {"public.plain-text", "text/plain"},
	"text":     {"public.plain-text", "text/plain"},
	"json":     {"public.json", "application/json"},
	"csv":      {"public.comma-separated-values-text", "text/csv"},
	"tsv":      {"public.tab-separated-values-text", "text/tab-separated-values"},
	"pdf":      {"com.adobe.pdf", "application/pdf"},
	"png":      {"public.png", "image/png"},
	"jpg":      {"public.jpeg", "image/jpeg"},
	"jpeg":     {"public.jpeg", "image/jpeg"},
	"gif":      {"com.compuserve.gif", "image/gif"},
	"heic":     {"public.heic", "image/heic"},
	"webp":     {"org.webmproject.webp", "image/webp"},
	"svg":      {"public.svg-image", "image/svg+xml"},
	"html":     {"public.html", "text/html"},
	"htm":      {"public.html", "text/html"},
	"xml":      {"public.xml", "application/xml"},
	"yaml":     {"public.yaml", "application/yaml"},
	"yml":      {"public.yaml", "application/yaml"},
	"rtf":      {"public.rtf", "text/rtf"},
	"zip":      {"public.zip-archive", "application/zip"},
	"epub":     {"org.idpf.epub-container", "application/epub+zip"},
	"mp3":      {"public.mp3", "audio/mpeg"},
	"m4a":      {"com.apple.m4a-audio", "audio/mp4"},
	"wav":      {"com.microsoft.waveform-audio", "audio/wav"},
	"mp4":      {"public.mpeg-4", "video/mp4"},
	"mov":      {"com.apple.quicktime-movie", "video/quicktime"},
	"ics":      {"com.apple.ical.ics", "text/calendar"},
	"vcf":      {"public.vcard", "text/vcard"},
	"sqlite":   {"org.sqlite.sqlite3", "application/vnd.sqlite3"},
}

// mimeOfUTI is the MIME type of a UTI Asuka knows (a type's parent, for
// shared-mime-info's sub-class-of).
func mimeOfUTI(uti string) string {
	switch uti {
	case "public.plain-text", "public.text", "public.utf8-plain-text":
		return "text/plain"
	case "public.data", "public.content", "public.item":
		return ""
	}
	for _, k := range knownTypes {
		if k.uti == uti {
			return k.mime
		}
	}
	return ""
}

var fileExtension = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_+-]*(\.[A-Za-z0-9_+-]+)*$`)

// fileType is a fileTypeConfig resolved: every field set.
type fileType struct {
	name       string
	extensions []string
	mimeTypes  []string
	uti        string
	conformsTo []string
	role       string // Editor, Viewer
	icon       string // a PNG's path, or ""
	own        bool   // the app's own type: exported
	known      bool   // the system's (Asuka knows its UTI): declared by none
}

// checkFileTypes checks asuka.json's fileTypes, their icons made absolute
// against dir.
func (c *appConfig) checkFileTypes(dir string) error {
	for i := range c.FileTypes {
		t := &c.FileTypes[i]
		if len(t.Extensions) == 0 {
			return fmt.Errorf("fileTypes[%d]: no extensions (want [\"md\"], without the dot)", i)
		}
		for j, e := range t.Extensions {
			e = strings.TrimPrefix(e, ".")
			if !fileExtension.MatchString(e) {
				return fmt.Errorf("fileTypes[%d]: extension %q (want letters and digits, as \"md\" or \"tar.gz\")", i, e)
			}
			t.Extensions[j] = e
		}
		for _, m := range t.MIMETypes {
			if typ, sub, ok := strings.Cut(m, "/"); !ok || typ == "" || sub == "" || strings.ContainsAny(m, " ;*") {
				return fmt.Errorf("fileTypes[%d]: MIME type %q (want type/subtype, as text/markdown)", i, m)
			}
		}
		switch strings.ToLower(t.Role) {
		case "", "editor", "viewer":
		default:
			return fmt.Errorf("fileTypes[%d]: role %q (want editor or viewer)", i, t.Role)
		}
		if t.Icon != "" && !filepath.IsAbs(t.Icon) {
			t.Icon = filepath.Join(dir, t.Icon)
		}
	}
	return nil
}

// fileTypes are asuka.json's fileTypes, resolved. The app's own MIME
// types are named for it as its Linux executable is
// (application/x-<name>-<extension>).
func (c *appConfig) fileTypes() []fileType {
	if c == nil {
		return nil
	}
	slug := exeSlug(c.Name, c.ID)
	var out []fileType
	for _, t := range c.FileTypes {
		ext := strings.ToLower(t.Extensions[0])
		known, isKnown := knownTypes[ext]
		r := fileType{name: t.Name, extensions: t.Extensions, mimeTypes: t.MIMETypes, uti: t.UTI,
			conformsTo: t.ConformsTo, icon: t.Icon, role: "Editor"}
		if strings.EqualFold(t.Role, "viewer") {
			r.role = "Viewer"
		}
		if r.name == "" {
			r.name = strings.ToUpper(ext) + " Document"
		}
		if r.uti == "" {
			if isKnown {
				r.uti = known.uti
			} else {
				r.uti = c.ID + "." + ext
			}
		}
		r.own = strings.HasPrefix(r.uti, c.ID+".")
		r.known = !r.own && isKnown && r.uti == known.uti
		if len(r.mimeTypes) == 0 {
			for _, e := range t.Extensions {
				if k, ok := knownTypes[strings.ToLower(e)]; ok && !slices.Contains(r.mimeTypes, k.mime) {
					r.mimeTypes = append(r.mimeTypes, k.mime)
				}
			}
			if len(r.mimeTypes) == 0 {
				r.mimeTypes = []string{"application/x-" + slug + "-" + strings.ReplaceAll(ext, ".", "-")}
			}
		}
		if len(r.conformsTo) == 0 {
			r.conformsTo = []string{"public.data", "public.content"}
		}
		out = append(out, r)
	}
	return out
}

// definesMIME reports whether the app defines t's MIME types (Linux's
// shared-mime-info, which knows the registered ones): its own type, of
// a MIME type no one registered (x-, or the one Asuka made).
func (t fileType) definesMIME(m string) bool {
	if t.known {
		return false
	}
	_, sub, _ := strings.Cut(m, "/")
	return t.own || strings.HasPrefix(sub, "x-")
}

// documentTypes are Info.plist's keys for the file types:
// CFBundleDocumentTypes, and the types' exported and imported
// declarations. icons are the .icns names the Mac bundle has for the
// types' icons (by index; iOS takes the system's icons).
func (c *appConfig) documentTypes(types []fileType, ios bool, icons map[int]string) map[string]any {
	if len(types) == 0 {
		return nil
	}
	var docs, exported, imported []any
	for i, t := range types {
		rank := "Default"
		if t.own {
			rank = "Owner"
		}
		doc := map[string]any{
			"CFBundleTypeName":   t.name,
			"CFBundleTypeRole":   t.role,
			"LSHandlerRank":      rank,
			"LSItemContentTypes": []string{t.uti},
		}
		if icon := icons[i]; icon != "" {
			doc["CFBundleTypeIconFile"] = icon
		}
		docs = append(docs, doc)
		if t.known {
			continue // the system declares it
		}
		decl := map[string]any{
			"UTTypeIdentifier":  t.uti,
			"UTTypeDescription": t.name,
			"UTTypeConformsTo":  t.conformsTo,
			"UTTypeTagSpecification": map[string]any{
				"public.filename-extension": t.extensions,
				"public.mime-type":          t.mimeTypes,
			},
		}
		if icon := icons[i]; icon != "" {
			decl["UTTypeIconFile"] = icon
		}
		if t.own {
			exported = append(exported, decl)
		} else {
			imported = append(imported, decl)
		}
	}
	m := map[string]any{"CFBundleDocumentTypes": docs}
	if exported != nil {
		m["UTExportedTypeDeclarations"] = exported
	}
	if imported != nil {
		m["UTImportedTypeDeclarations"] = imported
	}
	if ios {
		// Files opens a document where it is (another app's, iCloud
		// Drive's), not a copy in the app's Inbox; App Store Connect wants
		// it said either way for an app with document types.
		m["LSSupportsOpeningDocumentsInPlace"] = true
	}
	return m
}

// fileTypeFilters are the Android activity's intent filters for the file
// types: ACTION_VIEW of a content:// or file:// URI of their MIME types;
// for the app's own types, which no content provider knows (it gives
// application/octet-stream), of their extensions too.
func fileTypeFilters(types []fileType) string {
	var b strings.Builder
	e := html.EscapeString
	var mimes []string
	for _, t := range types {
		for _, m := range t.mimeTypes {
			if !slices.Contains(mimes, m) {
				mimes = append(mimes, m)
			}
		}
	}
	if len(mimes) > 0 {
		b.WriteString("            <intent-filter>\n")
		b.WriteString("                <action android:name=\"android.intent.action.VIEW\" />\n")
		b.WriteString("                <category android:name=\"android.intent.category.DEFAULT\" />\n")
		b.WriteString("                <data android:scheme=\"content\" />\n                <data android:scheme=\"file\" />\n")
		for _, m := range mimes {
			fmt.Fprintf(&b, "                <data android:mimeType=\"%s\" />\n", e(m))
		}
		b.WriteString("            </intent-filter>\n")
	}
	for _, t := range types {
		if !t.own {
			continue
		}
		b.WriteString("            <intent-filter>\n")
		b.WriteString("                <action android:name=\"android.intent.action.VIEW\" />\n")
		b.WriteString("                <category android:name=\"android.intent.category.DEFAULT\" />\n")
		b.WriteString("                <data android:scheme=\"content\" />\n                <data android:scheme=\"file\" />\n")
		b.WriteString("                <data android:host=\"*\" />\n")
		b.WriteString("                <data android:mimeType=\"application/octet-stream\" />\n")
		b.WriteString("                <data android:mimeType=\"*/*\" />\n")
		for _, x := range t.extensions {
			// The path's last dot is the extension's: a pattern for each
			// count of dots before it (Android's patterns don't backtrack).
			pat := ""
			for range 4 {
				pat += `.*\\.`
				fmt.Fprintf(&b, "                <data android:pathPattern=\"%s%s\" />\n", pat, e(strings.ReplaceAll(x, ".", `\\.`)))
			}
		}
		b.WriteString("            </intent-filter>\n")
	}
	return b.String()
}

// mimePackage is the shared-mime-info package defining the MIME types
// the app's file types make up (share/mime/packages/<id>.xml), or "".
func mimePackage(types []fileType) string {
	var b strings.Builder
	e := html.EscapeString
	for _, t := range types {
		for _, m := range t.mimeTypes {
			if !t.definesMIME(m) {
				continue
			}
			fmt.Fprintf(&b, "  <mime-type type=\"%s\">\n", e(m))
			fmt.Fprintf(&b, "    <comment>%s</comment>\n", e(t.name))
			for _, p := range t.conformsTo {
				if parent := mimeOfUTI(p); parent != "" {
					fmt.Fprintf(&b, "    <sub-class-of type=\"%s\"/>\n", e(parent))
				}
			}
			if t.icon != "" {
				fmt.Fprintf(&b, "    <icon name=\"%s\"/>\n", e(mimeIconName(m)))
			}
			for _, x := range t.extensions {
				fmt.Fprintf(&b, "    <glob pattern=\"*.%s\"/>\n", e(strings.ToLower(x)))
			}
			b.WriteString("  </mime-type>\n")
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<mime-info xmlns=\"http://www.freedesktop.org/standards/shared-mime-info\">\n" + b.String() + "</mime-info>\n"
}

// mimeIconName is a MIME type's icon's name in an icon theme: its slash a
// hyphen (application/x-notes-note: application-x-notes-note).
func mimeIconName(m string) string { return strings.ReplaceAll(m, "/", "-") }

// desktopMimeTypes are the desktop file's MimeType values: the scheme's
// handler and the file types'.
func desktopMimeTypes(app *goApp, types []fileType) []string {
	var out []string
	if app.cfg.Scheme != "" {
		out = append(out, "x-scheme-handler/"+app.cfg.Scheme)
	}
	for _, t := range types {
		for _, m := range t.mimeTypes {
			if !slices.Contains(out, m) {
				out = append(out, m)
			}
		}
	}
	return out
}

// webFileHandlers is the web manifest's file_handlers: an installed web
// app (Chromium) opens the types' files, which the host gets from the
// launch queue.
func webFileHandlers(types []fileType) []map[string]any {
	if len(types) == 0 {
		return nil
	}
	accept := map[string][]string{}
	for _, t := range types {
		var exts []string
		for _, x := range t.extensions {
			exts = append(exts, "."+x)
		}
		for _, m := range t.mimeTypes {
			for _, x := range exts {
				if !slices.Contains(accept[m], x) {
					accept[m] = append(accept[m], x)
				}
			}
		}
	}
	return []map[string]any{{"action": "./", "accept": accept, "launch_type": "single-client"}}
}

// windowsFileTypes are the types for the Windows host (boot.FileTypes):
// iconIDs are their icons' group resource IDs in the executable.
func windowsFileTypes(types []fileType, iconIDs map[int]int) []filetype.Type {
	var out []filetype.Type
	for i, t := range types {
		out = append(out, filetype.Type{Name: t.name, Extensions: t.extensions, Own: t.own, Icon: iconIDs[i]})
	}
	return out
}

// fileTypeIconID is the group icon resource ID of the i-th file type's
// icon in a Windows executable (the app's is 1).
func fileTypeIconID(i int) int { return 101 + i }
