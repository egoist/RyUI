//go:build windows

package windows

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	spec "github.com/egoist/asuka/internal/nowplayingspec"
	. "github.com/egoist/asuka/internal/windows"
)

// asuka/sdk/nowplaying on Windows: the SystemMediaTransportControls of the
// app's window (ISystemMediaTransportControlsInterop::GetForWindow, as a
// desktop app gets them; a hidden window of the host's when the app has
// none, a background app's), which the volume flyout (Windows 10), quick
// settings' media controls (Windows 11), the lock screen and the
// keyboard's media keys show and drive. Its buttons are Play, Pause,
// Stop, Next, Previous, FastForward and Rewind (the skips), and the
// timeline's position requests (seeks); the play/pause key sends Play or
// Pause as the status is. The artwork is read off the main thread and
// handed over as a stream. The module's methods are
// internal/nowplayingspec's.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) { m["nowPlaying"] = h.nowPlayingModule() })
}

// smtc is the module's state, main thread only.
var smtc struct {
	controls *MediaControls
	failed   error // why there are none
	info     *spec.Info
	artSrc   string
	art      []byte // the artwork's bytes, once read

	following bool
	waiting   func([]byte, error)
	queue     []byte
}

func (h *Host) nowPlayingModule() map[string]asyncMethod {
	return map[string]asyncMethod{
		"set": func(args []byte, reply func([]byte, error)) {
			var info spec.Info
			if err := json.Unmarshal(args, &info); err != nil {
				reply(nil, err)
				return
			}
			c, err := h.mediaControls()
			if err != nil {
				reply(nil, err)
				return
			}
			smtc.info = &info
			h.smtcArtwork(info.Artwork)
			c.Set(smtcInfo())
			reply(nil, nil)
		},
		"clear": func(_ []byte, reply func([]byte, error)) {
			smtc.info, smtc.artSrc, smtc.art = nil, "", nil
			if smtc.controls != nil {
				smtc.controls.Clear()
			}
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			if w := smtc.waiting; w != nil {
				w(nil, errors.New("stopped")) // one poll at a time
			}
			smtc.following, smtc.waiting = true, reply
			smtcFlush()
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			w := smtc.waiting
			smtc.following, smtc.waiting, smtc.queue = false, nil, nil
			if w != nil {
				w(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

// mediaControls are the controls, got (and hooked up) on first use.
func (h *Host) mediaControls() (*MediaControls, error) {
	if smtc.controls != nil || smtc.failed != nil {
		return smtc.controls, smtc.failed
	}
	hwnd := h.window
	if hwnd == 0 {
		// A background app's: a top-level window of its own, never shown.
		hwnd = ownWindow(0, h.appName, WS_OVERLAPPEDWINDOW, 0, 0, 0, 0, 0, nil)
	}
	c, err := GetMediaControls(hwnd, func(b MediaButton) {
		h.Post(func() { smtcButton(b) })
	}, func(at float64) {
		h.Post(func() { smtcCommand(spec.Seek, at) })
	})
	if err != nil {
		smtc.failed = err
		return nil, err
	}
	smtc.controls = c
	return c, nil
}

func smtcButton(b MediaButton) {
	switch b {
	case MediaButtonPlay:
		smtcCommand(spec.Play, 0)
	case MediaButtonPause:
		smtcCommand(spec.Pause, 0)
	case MediaButtonStop:
		smtcCommand(spec.Stop, 0)
	case MediaButtonNext:
		smtcCommand(spec.Next, 0)
	case MediaButtonPrevious:
		smtcCommand(spec.Previous, 0)
	case MediaButtonFastForward:
		smtcCommand(spec.SkipForward, 0)
	case MediaButtonRewind:
		smtcCommand(spec.SkipBackward, 0)
	}
}

// smtcCommand hands Go a command, if it's enabled.
func smtcCommand(c spec.Command, v float64) {
	if !smtc.following || smtc.info == nil || !smtc.info.Has(c) {
		return
	}
	if (c == spec.SkipForward || c == spec.SkipBackward) && v == 0 {
		v = smtc.info.Skip
	}
	smtc.queue = spec.AppendCommand(smtc.queue, c, v)
	smtcFlush()
}

func smtcFlush() {
	if smtc.waiting == nil || len(smtc.queue) == 0 {
		return
	}
	w, b := smtc.waiting, smtc.queue
	smtc.waiting, smtc.queue = nil, nil
	w(b, nil)
}

// smtcInfo is what the controls show of the module's state.
func smtcInfo() MediaInfo {
	i := smtc.info
	m := MediaInfo{Title: i.Title, Artist: i.Artist, Album: i.Album, Thumbnail: smtc.art,
		Duration: i.Duration, Position: i.Position, Rate: i.Rate,
		Play: i.Has(spec.Play), Pause: i.Has(spec.Pause), Stop: i.Has(spec.Stop),
		Next: i.Has(spec.Next), Previous: i.Has(spec.Previous),
		FastForward: i.Has(spec.SkipForward), Rewind: i.Has(spec.SkipBackward),
		Seek: i.Has(spec.Seek) && i.Duration > 0}
	switch i.State {
	case "playing":
		m.Status = MediaPlaying
	case "stopped":
		m.Status = MediaStopped
	default:
		m.Status = MediaPaused
	}
	return m
}

// smtcArtwork reads the artwork's bytes off the main thread, and shows
// them once they're in.
func (h *Host) smtcArtwork(src string) {
	if src == smtc.artSrc {
		return
	}
	smtc.artSrc, smtc.art = src, nil
	if src == "" {
		return
	}
	assets := h.assets
	go func() {
		data, err := readSource(src, assets)
		if err != nil {
			return
		}
		h.Post(func() {
			if smtc.artSrc != src || smtc.info == nil || smtc.controls == nil {
				return
			}
			smtc.art = data
			smtc.controls.Set(smtcInfo())
		})
	}()
}

// readSource reads an image's bytes: an http(s) URL's, a file's, an
// asset's.
func readSource(src, assets string) ([]byte, error) {
	switch {
	case strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://"):
		resp, err := http.Get(src)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("%s: %s", src, resp.Status)
		}
		return io.ReadAll(resp.Body)
	case strings.HasPrefix(src, "file://"):
		src = strings.TrimPrefix(strings.TrimPrefix(src, "file://"), "/")
		return os.ReadFile(filepath.FromSlash(src))
	case filepath.IsAbs(src):
		return os.ReadFile(src)
	}
	return os.ReadFile(filepath.Join(assets, filepath.FromSlash(src)))
}
