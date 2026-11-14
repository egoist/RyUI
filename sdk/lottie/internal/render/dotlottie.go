package render

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
)

// isZip reports whether data is a zip file (a dotLottie).
func isZip(data []byte) bool { return len(data) >= 4 && string(data[:4]) == "PK\x03\x04" }

// DotLottieJSON is the Lottie JSON of one of a dotLottie's animations:
// the one id names, or its manifest's active one, else its first.
// dotLottie 1 keeps them in animations/, version 2 in a/.
func DotLottieJSON(data []byte, id string) ([]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	files := map[string]*zip.File{}
	for _, f := range z.File {
		files[path.Clean(f.Name)] = f
	}
	read := func(name string) ([]byte, bool) {
		f := files[name]
		if f == nil {
			return nil, false
		}
		rc, err := f.Open()
		if err != nil {
			return nil, false
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		return b, err == nil
	}
	var manifest struct {
		Active     string `json:"activeAnimationId"`
		Animations []struct {
			ID string `json:"id"`
		} `json:"animations"`
	}
	if b, ok := read("manifest.json"); ok {
		json.Unmarshal(b, &manifest)
	}
	if id == "" {
		id = manifest.Active
	}
	if id == "" && len(manifest.Animations) > 0 {
		id = manifest.Animations[0].ID
	}
	for _, dir := range []string{"a", "animations"} {
		if id != "" {
			if b, ok := read(dir + "/" + id + ".json"); ok {
				return b, nil
			}
		}
	}
	if id != "" {
		return nil, fmt.Errorf("lottie: the dotLottie has no animation %q", id)
	}
	// No manifest: its first animation.
	for _, f := range z.File {
		if d := path.Dir(f.Name); (d == "a" || d == "animations") && path.Ext(f.Name) == ".json" {
			if b, ok := read(path.Clean(f.Name)); ok {
				return b, nil
			}
		}
	}
	return nil, ErrNotLottie
}

// ParseDotLottie reads one of a dotLottie's animations (id: as
// DotLottieJSON chooses).
func ParseDotLottie(data []byte, id string) (*Animation, error) {
	b, err := DotLottieJSON(data, id)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}
