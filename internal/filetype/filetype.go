// Package filetype is asuka.json's fileTypes as a host needs them where
// no bundle declares them (Windows, which registers them for the user as
// the app starts): the Asuka CLI encodes them into the build
// (boot.FileTypes, -ldflags -X), the host decodes them.
package filetype

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// Type is a kind of file the app opens.
type Type struct {
	// Name is what the files are ("Markdown Document").
	Name string `json:"n"`
	// Extensions are the files' name extensions, without a dot.
	Extensions []string `json:"e"`
	// Own: the app defines the type (its own format): it's the files'
	// default app, where the user hasn't chosen another.
	Own bool `json:"o,omitempty"`
	// Icon is the files' icon's resource ID in the executable (Windows:
	// a group icon asuka build adds), 0 for the app's.
	Icon int `json:"i,omitempty"`
}

// Encode is types for an -ldflags -X value: base64 (URL-safe, no
// padding) of their JSON, which has no quote or space for the go
// command's flag splitting.
func Encode(types []Type) string {
	if len(types) == 0 {
		return ""
	}
	b, _ := json.Marshal(types)
	return base64.RawURLEncoding.EncodeToString(b)
}

// Decode is Encode's types, none if s is empty or broken.
func Decode(s string) []Type {
	if s == "" {
		return nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil
	}
	var types []Type
	if json.Unmarshal(b, &types) != nil {
		return nil
	}
	return types
}

// ProgID is the type's Windows programmatic identifier: the app's ID
// (letters, digits and dots) and the type's first extension,
// "com.example.notes.note". A ProgID is at most 39 characters: a long ID
// keeps its end.
func ProgID(appID string, t Type) string {
	id := strings.Map(func(r rune) rune {
		if r < 0x80 && (r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return r
		}
		return -1
	}, appID)
	ext := ""
	if len(t.Extensions) > 0 {
		ext = strings.ToLower(t.Extensions[0])
	}
	p := strings.Trim(id+"."+ext, ".")
	if len(p) > 39 {
		p = p[len(p)-39:]
		if i := strings.IndexByte(p, '.'); i >= 0 && i < len(p)-1-len(ext) {
			p = p[i+1:] // whole parts
		}
	}
	return p
}
