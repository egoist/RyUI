//go:build js

package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"syscall/js"
	"time"

	"github.com/egoist/asuka/internal/boot"
	iupdate "github.com/egoist/asuka/internal/update"
)

// The web: the browser loads the app's newest files from its server as the
// page loads, so an update is a reload. asuka build --platform web writes
// version.json beside index.html, the version it built and its release
// notes; Check reads it again (past caches), and a newer version is a
// Release whose Install has nothing to do and whose Relaunch reloads the
// page: the update window offers to relaunch it, as on the desktop.

// Supported is nil for a build of asuka build --platform web (it knows its
// version), ErrNotConfigured for asuka dev's, which reloads by itself.
func Supported() error {
	if boot.Version == "" {
		return ErrNotConfigured
	}
	return nil
}

func defaultSource() Source { return webSource{} }

// webSource reads version.json beside the page.
type webSource struct{}

func (webSource) Manifest(ctx context.Context, target, channel string) (io.ReadCloser, error) {
	base := js.Global().Get("document").Get("baseURI").String()
	u := js.Global().Get("URL").New("version.json?t="+strconv.FormatInt(time.Now().UnixNano(), 36), base).Call("toString").String()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // a build before version.json
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	var v struct {
		Version string `json:"version"`
		Notes   string `json:"notes"`
		Date    string `json:"date"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, iupdate.MaxManifestSize)).Decode(&v); err != nil {
		return nil, fmt.Errorf("reading version.json: %w", err)
	}
	// A manifest with nothing to download: Install doesn't.
	m, _ := json.Marshal(iupdate.Manifest{Version: v.Version, Notes: v.Notes, Date: v.Date, URL: "reload", Size: 1, Signature: "-"})
	return io.NopCloser(bytes.NewReader(m)), nil
}

func (webSource) Open(ctx context.Context, url string) (io.ReadCloser, error) {
	return nil, errors.New("update: the web has nothing to download")
}

// Install has nothing to download on the web: the page's next load is the
// new version.
func (r *Release) Install(ctx context.Context, progress func(downloaded, total int64)) error {
	if progress != nil {
		progress(1, 1)
	}
	return nil
}

// Relaunch reloads the page, whose files are then the newest the server
// has; the app's own files are written as the page goes (fs.js, on
// pagehide).
func Relaunch() { js.Global().Get("location").Call("reload") }
