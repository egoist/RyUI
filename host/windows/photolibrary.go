//go:build windows

package windows

import (
	"crypto/sha256"
	hexenc "encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	. "github.com/egoist/asuka/internal/windows"
)

// asuka/sdk/photos' library on Windows ("photoLibrary"): the user's Pictures
// folder, which the Photos app shows, read as the library is on a phone.
// A Windows app needs no permission to read it: "current" and "request"
// answer "allowed", "manage" does nothing. "list" (JSON screenshots,
// offset, limit) answers its images newest first (id: the path in
// Pictures; width, height upright; created in ms; screenshot: in the
// Screenshots folder, as on Android); "thumbnail" a JPEG's path in the
// caches (kept for the next time); "export" JSON (path, name) of a copy
// in the caches.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) { m["photoLibrary"] = h.photoLibraryModule() })
}

// photoRoot is the library's folder; a test replaces it.
var photoRoot = func() (string, error) { return KnownFolder(FolderPictures) }

var photoExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".heic": true, ".heif": true, ".webp": true,
	".gif": true, ".bmp": true, ".tif": true, ".tiff": true, ".avif": true, ".jxr": true, ".dng": true}

type photoFile struct {
	rel     string
	path    string
	created int64 // ms
	shot    bool
}

// photoFiles are the library's images, newest first.
func photoFiles(screenshotsOnly bool) ([]photoFile, error) {
	root, err := photoRoot()
	if err != nil {
		return nil, err
	}
	shots, _ := KnownFolder(FolderScreenshots)
	var out []photoFile
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !photoExts[strings.ToLower(filepath.Ext(p))] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		created := info.ModTime().UnixMilli()
		if a, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
			created = a.CreationTime.Nanoseconds() / 1e6
		}
		rel, _ := filepath.Rel(root, p)
		dir := strings.ToLower(filepath.Dir(p))
		shot := (shots != "" && (dir == strings.ToLower(shots) || strings.HasPrefix(dir, strings.ToLower(shots)+`\`))) ||
			strings.EqualFold(filepath.Base(filepath.Dir(p)), "Screenshots")
		if screenshotsOnly && !shot {
			return nil
		}
		out = append(out, photoFile{rel: filepath.ToSlash(rel), path: p, created: created, shot: shot})
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].created > out[j].created })
	return out, nil
}

// photoPath is a photo's file, by its id (never outside the library).
func photoPath(id string) (string, error) {
	root, err := photoRoot()
	if err != nil {
		return "", err
	}
	p := filepath.Join(root, filepath.FromSlash(id))
	if rel, err := filepath.Rel(root, p); err != nil || rel == ".." || strings.HasPrefix(rel, `..\`) || id == "" {
		return "", errors.New("no such photo")
	}
	if _, err := os.Stat(p); err != nil {
		return "", errors.New("no such photo")
	}
	return p, nil
}

func (h *Host) photoLibraryModule() map[string]asyncMethod {
	allowed := func(_ []byte, reply func([]byte, error)) { reply([]byte("allowed"), nil) }
	return map[string]asyncMethod{
		"current": allowed,
		"request": allowed,
		"manage":  answerNil,
		"list": func(args []byte, reply func([]byte, error)) {
			o := struct {
				Screenshots bool `json:"screenshots"`
				Offset      int  `json:"offset"`
				Limit       int  `json:"limit"`
			}{Limit: 100}
			json.Unmarshal(args, &o)
			h.offMain(func() (b []byte, err error) {
				WithCOM(func() {
					var files []photoFile
					if files, err = photoFiles(o.Screenshots); err != nil {
						return
					}
					out := []map[string]any{}
					for i := max(o.Offset, 0); i < min(o.Offset+max(o.Limit, 1), len(files)); i++ {
						f := files[i]
						w, ht, _ := ImageFileSize(f.path)
						out = append(out, map[string]any{"id": f.rel, "width": w, "height": ht, "created": f.created, "screenshot": f.shot})
					}
					b, err = json.Marshal(out)
				})
				return b, err
			}, reply)
		},
		"thumbnail": func(args []byte, reply func([]byte, error)) {
			o := struct {
				ID   string `json:"id"`
				Size int    `json:"size"`
			}{Size: 300}
			json.Unmarshal(args, &o)
			h.offMain(func() (b []byte, err error) {
				p, err := photoPath(o.ID)
				if err != nil {
					return nil, err
				}
				info, _ := os.Stat(p)
				sum := sha256.Sum256([]byte(fmt.Sprint(o.ID, info.ModTime().UnixNano())))
				dir := filepath.Join(h.cacheDir(), "asuka-photo-thumbs")
				out := filepath.Join(dir, fmt.Sprintf("%s-%d.jpg", hexenc.EncodeToString(sum[:16]), o.Size))
				if _, err := os.Stat(out); err == nil {
					return []byte(out), nil
				}
				WithCOM(func() {
					w, ht, e := ImageFileSize(p)
					if e != nil {
						err = errors.New("no thumbnail")
						return
					}
					// At least size on its short side.
					long := o.Size
					if short := min(w, ht); short > o.Size && short > 0 {
						long = (max(w, ht)*o.Size + short - 1) / short
					} else {
						long = 0 // small already: whole
					}
					img, e := DecodeImageFile(p, long)
					if e != nil {
						err = errors.New("no thumbnail")
						return
					}
					os.MkdirAll(dir, 0o755)
					tmp := out + ".tmp"
					f, e := os.Create(tmp)
					if e != nil {
						err = e
						return
					}
					e = jpeg.Encode(f, onWhite(img), &jpeg.Options{Quality: 80})
					f.Close()
					if e == nil {
						e = os.Rename(tmp, out)
					}
					err = e
				})
				if err != nil {
					return nil, err
				}
				return []byte(out), nil
			}, reply)
		},
		"export": func(args []byte, reply func([]byte, error)) {
			var o struct {
				ID string `json:"id"`
			}
			json.Unmarshal(args, &o)
			h.offMain(func() ([]byte, error) {
				p, err := photoPath(o.ID)
				if err != nil {
					return nil, err
				}
				dir := filepath.Join(h.cacheDir(), "asuka-photo-exports")
				os.MkdirAll(dir, 0o755)
				into, err := os.MkdirTemp(dir, "")
				if err != nil {
					return nil, err
				}
				name := filepath.Base(p)
				dst, err := libSaveFile(p, into, "", name, false)
				if err != nil {
					return nil, err
				}
				return json.Marshal(map[string]string{"path": dst, "name": name})
			}, reply)
		},
	}
}
