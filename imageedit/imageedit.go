// Package imageedit edits images and takes frames from videos
// (expo-image-manipulator, expo-video-thumbnails), with the platform's
// own codecs: it reads what the camera and the photo picker give (HEIC,
// JPEG, PNG, WebP...), turned upright as their orientation says, and
// writes JPEG or PNG, for an upload, a cache, ui.Image.
//
//	r, err := imageedit.Edit(photo, []imageedit.Action{imageedit.Resize(1080, 0)}, imageedit.Options{Quality: 0.8})
//
// A source is what ui.Image takes: a file's path, a file:// or http(s)
// URL, or a file in the app's assets. Calls wait for the work: not on the
// main thread.
package imageedit

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/egoist/ryui/ui"
)

// Action is a step of an edit, done in order.
type Action struct {
	op            string
	width, height int
	x, y          int
	degrees       float64
}

// Resize scales the image to width×height pixels; a 0 keeps the aspect
// ratio from the other (Resize(1080, 0): 1080 wide).
func Resize(width, height int) Action { return Action{op: "resize", width: width, height: height} }

// Crop keeps the rectangle at x, y (from the top left), width×height
// pixels, within the image.
func Crop(x, y, width, height int) Action {
	return Action{op: "crop", x: x, y: y, width: width, height: height}
}

// Rotate turns the image clockwise by degrees; an angle that isn't a
// multiple of 90 makes the image the size of the turned one, its corners
// transparent (in PNG; white in JPEG).
func Rotate(degrees float64) Action { return Action{op: "rotate", degrees: degrees} }

// FlipHorizontal mirrors the image left to right.
func FlipHorizontal() Action { return Action{op: "flipH"} }

// FlipVertical mirrors the image top to bottom.
func FlipVertical() Action { return Action{op: "flipV"} }

// Format is the file written.
type Format uint8

const (
	JPEG Format = iota
	PNG         // lossless, with transparency
)

// Options are how the result is written.
type Options struct {
	Format Format
	// Quality is JPEG's, from 0 to 1 (default 0.9).
	Quality float64
}

// Result is the image written: a file in the app's caches (move it to
// keep it), and its size in pixels.
type Result struct {
	Path   string `json:"path"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// ErrNoImage: the source isn't there, or isn't an image (or a video the
// platform plays, for VideoFrame).
var ErrNoImage = errors.New("imageedit: no image")

// Edit reads the image source, does the actions in order and writes the
// result. With none, it turns the image upright and writes it in the
// format asked: a HEIC photo as a JPEG, say. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Edit(source string, actions []Action, o Options) (Result, error) {
	return call("edit", request(source, actions, o, 0))
}

// Size is the image source's size in pixels, upright, without decoding
// it. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Size(source string) (width, height int, err error) {
	r, err := call("size", request(source, nil, Options{}, 0))
	return r.Width, r.Height, err
}

// VideoFrame takes the video source's frame at the time at (its first
// for 0), upright, as an image, and does the actions on it: a thumbnail,
// a poster. A remote video is read only as far as the frame needs. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func VideoFrame(source string, at time.Duration, actions []Action, o Options) (Result, error) {
	return call("videoFrame", request(source, actions, o, at))
}

// request is the call's JSON: the source, the actions, the format,
// quality and, for a video, the time in milliseconds.
func request(source string, actions []Action, o Options, at time.Duration) []byte {
	type action struct {
		Op      string  `json:"op"`
		Width   int     `json:"width,omitempty"`
		Height  int     `json:"height,omitempty"`
		X       int     `json:"x,omitempty"`
		Y       int     `json:"y,omitempty"`
		Degrees float64 `json:"degrees,omitempty"`
	}
	as := make([]action, len(actions))
	for i, a := range actions {
		as[i] = action{a.op, a.width, a.height, a.x, a.y, a.degrees}
	}
	q := o.Quality
	if q <= 0 || q > 1 {
		q = 0.9
	}
	format := "jpeg"
	if o.Format == PNG {
		format = "png"
	}
	b, _ := json.Marshal(map[string]any{"source": ui.AssetURL(source), "actions": as, "format": format, "quality": q, "at": at.Milliseconds()})
	return b
}

func call(method string, args []byte) (Result, error) {
	b, err := ui.CallWait("imageEdit", method, args)
	if err != nil {
		if err.Error() == "no image" {
			return Result{}, ErrNoImage
		}
		return Result{}, err
	}
	var r Result
	err = json.Unmarshal(b, &r)
	return r, err
}
