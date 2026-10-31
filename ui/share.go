package ui

import (
	"encoding/binary"
	"errors"
	"mime"
	"path/filepath"
	"slices"

	"github.com/egoist/ryui/internal/boot"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
)

// Share shows the system's share sheet for text (a URL is shared as a
// link): send it to an app, a contact, AirDrop, Nearby Share. Main
// thread.
func Share(text string) {
	if r := activeRoot(); r != nil {
		r.batch.Command(r.el.id, ops.CmdShare, []byte(text))
	}
}

// ShareOptions say how ShareFiles shares (expo-sharing's options).
type ShareOptions struct {
	// MIME is the files' type, which apps on the sheet go by (Android's
	// intent type; on iOS, the type when UTI is empty). Default: from the
	// first file's extension.
	MIME string
	// UTI is the files' Uniform Type Identifier on iOS
	// ("com.adobe.pdf"), ahead of MIME.
	UTI string
	// Title is the share sheet's title (Android's chooser, a Mac's
	// picker has none; iOS shows none).
	Title string
	// From is the view the sheet points at where it's a popover (iPad,
	// the Mac): the button that shares. Default: the screen's middle.
	From Node
}

// ShareFiles shows the system's share sheet for the files at paths: send
// them to an app, a contact, AirDrop, Nearby Share, save them to Files.
// done (if not nil) is called on the main thread once the sheet is gone,
// shared or not, with an error if it couldn't show (no such file;
// Windows has no share sheet for files). Main thread.
//
// On Android the files go through the app's FileProvider; one outside
// the app's own folders is copied to its caches first.
func ShareFiles(paths []string, o ShareOptions, done func(err error)) {
	if len(paths) == 0 {
		if done != nil {
			done(errors.New("ui: ShareFiles with no files"))
		}
		return
	}
	if o.MIME == "" {
		o.MIME = mime.TypeByExtension(filepath.Ext(paths[0]))
	}
	var from uint32
	if o.From.e != nil && o.From.e.real {
		from = uint32(o.From.e.id)
	}
	le := binary.LittleEndian
	str := func(b []byte, s string) []byte {
		b = le.AppendUint32(b, uint32(len(s)))
		return append(b, s...)
	}
	// u32 view (0: none), string MIME, string UTI, string title, u32
	// count, then the paths.
	b := le.AppendUint32(nil, from)
	b = str(b, o.MIME)
	b = str(b, o.UTI)
	b = str(b, o.Title)
	b = le.AppendUint32(b, uint32(len(paths)))
	for _, p := range paths {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		b = str(b, p)
	}
	Call("share", "files", b, func(_ []byte, err error) {
		if done != nil {
			done(err)
		}
	})
}

// ShareKind is what a shared item is.
type ShareKind = ops.ShareKind

const (
	SharedText  = ops.ShareKindText  // text: Text
	SharedLink  = ops.ShareKindLink  // a web link: its URL in Text
	SharedImage = ops.ShareKindImage // the rest are files, at Path
	SharedVideo = ops.ShareKindVideo
	SharedAudio = ops.ShareKindAudio
	SharedFile  = ops.ShareKindFile
)

// SharedItem is one thing another app shared with this one.
type SharedItem struct {
	Kind ShareKind
	// Text is a text's text, or a link's URL.
	Text string
	// Path is a file's: the app's own copy, in its caches, which the app
	// moves where it keeps it, or removes when it's done.
	Path string
	// Name is the file's name in the app it came from ("IMG_0042.HEIC").
	Name string
	// MIME is its type ("image/jpeg"; "text/plain" for a text).
	MIME string
	// Size is the file's size in bytes, -1 if it isn't known.
	Size int64
}

// IsFile reports whether the item is a file, at Path.
func (s SharedItem) IsFile() bool { return s.Kind >= SharedImage }

// shareHandler is an OnShare handler, and the root it was made in.
type shareHandler struct {
	r *Root
	f func([]SharedItem)
}

// shareHandlers get what other apps share with this one; pendingShare is
// what came before any handler (the share that launched the app), by
// the root it was sent to.
var (
	shareHandlers []*shareHandler
	pendingShare  = map[ops.NodeID][][]SharedItem{}
)

// OnShare calls f with what another app shares with this one from its
// share sheet ("Share" in YouTube, a browser, Photos), which offers this
// app with ryui.json's "shareTarget": texts, links and files, in the
// order the other app gave them. What came before any handler (the share
// that launched the app) goes to the first. It stops with the
// component's owner. Main thread.
//
// On iOS a share extension takes the share and opens the app with it
// (files through an App Group); on Android the share (ACTION_SEND,
// ACTION_SEND_MULTIPLE) opens the app, which copies the files. To show a
// screen over the app that shares instead, see app.ShareSheet.
func OnShare(f func(items []SharedItem)) {
	h := &shareHandler{r: activeRoot(), f: f}
	shareHandlers = append(shareHandlers, h)
	signal.OnCleanup(func() {
		shareHandlers = slices.DeleteFunc(shareHandlers, func(x *shareHandler) bool { return x == h })
	})
	for _, id := range []ops.NodeID{h.r.el.id, 0} {
		pending := pendingShare[id]
		delete(pendingShare, id)
		for _, items := range pending {
			f(items)
		}
	}
}

// shared delivers a Shared event's items to the handlers of the root it
// was sent to (every root's, sent to none of them).
func shared(to ops.NodeID, b []byte) {
	items := decodeShared(b)
	if len(items) == 0 {
		return
	}
	if !slices.ContainsFunc(roots, func(r *Root) bool { return r.el != nil && r.el.id == to }) {
		to = 0
	}
	var hs []*shareHandler
	for _, h := range shareHandlers {
		if to == 0 || h.r.el.id == to {
			hs = append(hs, h)
		}
	}
	if len(hs) == 0 {
		pendingShare[to] = append(pendingShare[to], items)
		return
	}
	for _, h := range hs {
		h.f(items)
	}
}

// ShareSheet is what another app shares, shown in the app's share sheet
// (app.ShareSheet): over the app that shares, which the sheet closes
// back to.
type ShareSheet struct {
	// Items are what's shared (see OnShare). A file's Path is the sheet's
	// to read until it closes: copy what the app keeps.
	Items []SharedItem
	r     *Root
	done  bool
}

// Close closes the sheet, done: back to the app that shared. The sheet's
// process ends after: what the app keeps is written first.
func (s *ShareSheet) Close() { s.close(0) }

// Cancel closes the sheet, cancelled: the app that shared hears so (iOS).
func (s *ShareSheet) Cancel() { s.close(1) }

// OpenApp closes the sheet and opens the app with what's shared: its
// ui.OnShare gets the items, as without a sheet.
func (s *ShareSheet) OpenApp() { s.close(2) }

func (s *ShareSheet) close(how byte) {
	if s.done {
		return
	}
	s.done = true
	// The sheet's process ends with it, as an app in the background may:
	// what's pending is written now (ryui/store), and app.OnLifecycle
	// hears it.
	for _, f := range boot.Lifecycle {
		f(int(ops.LifecycleBackground))
	}
	s.r.batch.Command(s.r.el.id, ops.CmdShareClose, []byte{how})
}

// ShareSheetRoot is the root of a share sheet, a root of its own (an iOS
// share extension's, an Android activity over the app that shares): it
// shows screen once what's shared has come, a spinner until then; with
// no screen, it opens the app with it. For app.ShareSheet.
func ShareSheetRoot(screen func(s *ShareSheet) Node) func() Node {
	return func() Node {
		share := signal.New[*ShareSheet](nil)
		r := activeRoot()
		OnShare(func(items []SharedItem) {
			if share.Peek() != nil {
				return // one share per sheet
			}
			s := &ShareSheet{Items: items, r: r}
			if screen == nil {
				s.OpenApp()
				return
			}
			share.Set(s)
		})
		return Dynamic(func() Node {
			s := share.Get()
			if s == nil {
				return View(ViewProps{Style: Style{Grow: 1, Justify: JustifyCenter, Align: AlignCenter}},
					Progress(nil, Style{Width: Pt(120)}))
			}
			return signal.Untrack(func() Node { return screen(s) })
		})
	}
}

// decodeShared reads a Shared event's payload (see ops' EventShared); a
// short one ends the list where it's cut.
func decodeShared(b []byte) []SharedItem {
	le := binary.LittleEndian
	if len(b) < 4 {
		return nil
	}
	n := le.Uint32(b)
	b = b[4:]
	str := func() (string, bool) {
		if len(b) < 4 || uint64(len(b)-4) < uint64(le.Uint32(b)) {
			return "", false
		}
		l := le.Uint32(b)
		s := string(b[4 : 4+l])
		b = b[4+l:]
		return s, true
	}
	var items []SharedItem
	for range n {
		if len(b) < 1 {
			break
		}
		it := SharedItem{Kind: ShareKind(b[0])}
		b = b[1:]
		var ok1, ok2, ok3, ok4 bool
		it.Text, ok1 = str()
		it.Path, ok2 = str()
		it.Name, ok3 = str()
		it.MIME, ok4 = str()
		if !ok1 || !ok2 || !ok3 || !ok4 || len(b) < 8 {
			break
		}
		it.Size = int64(le.Uint64(b))
		b = b[8:]
		items = append(items, it)
	}
	return items
}
