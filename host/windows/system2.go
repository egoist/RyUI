//go:build windows

package windows

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	stdimage "image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	. "github.com/egoist/ryui/internal/windows"
)

// More of the system's modules on Windows, answering as host/darwin's do
// where Windows has the same, and saying it hasn't where it hasn't:
//
//   - splash: a Windows app has no launch screen (its window shows once
//     its first frame is laid out): hold and hide do nothing.
//   - screen: no orientation to lock nor bars to style; PreventCapture
//     keeps the app's windows (those that open later too) out of
//     screenshots and recordings (WDA_EXCLUDEFROMCAPTURE); a screenshot
//     is seen as a file appears in the user's Screenshots folder, where
//     Print Screen and the Snipping Tool keep them (Windows has no
//     notification for them, as macOS hasn't).
//   - sensors: none an app reads here, as on a Mac.
//   - widget: Windows's widgets are packaged apps' (a widget provider);
//     not available.
//   - tracking: Windows has no prompt; the user's choice is Settings'
//     advertising ID (Privacy & security, General): allowed while it's
//     on, denied while it's off.
//   - integrity: App Attest is Apple's, Play Integrity Android's.
//   - ageRange, appleAuth: Apple's.
//   - review: the Microsoft Store's rating prompt is a Store app's; it
//     shows nothing (stores never say whether they did).
//   - push: WNS gives channels to packaged apps only.
//   - transcribe: Windows has no recognizer of files on the device that
//     apps can count on: not available.
//   - contacts, calendar: Windows gives the user's contacts and
//     calendars to packaged apps only (denied); adding opens the
//     contact's vCard or the event's iCalendar file in the user's
//     contacts or calendar app, where they save it (as on Android, which
//     doesn't say whether they did).
//   - background: a Windows app keeps running out of sight; a task keeps
//     the system from sleeping (SetThreadExecutionState) until the last
//     one ends, as an activity does on a Mac.
//   - clipboard (ryui/device's): its image, copying one, and its changes.
//   - library: files go to the user's Downloads, Videos, Music and
//     Pictures folders.
//   - ryui/maps' view: the host's own, of OpenStreetMap's tiles
//     (maps.go).

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) {
		m["splash"] = map[string]asyncMethod{"hold": answerNil, "hide": answerNil}
		m["screen"] = h.screenModule()
		m["sensors"] = sensorsModule()
		m["widget"] = map[string]asyncMethod{"set": failing("not available: widgets are iOS's and Android's")}
		m["tracking"] = trackingModule()
		m["integrity"] = integrityModule()
		m["ageRange"] = map[string]asyncMethod{"request": failing("not available")}
		m["appleAuth"] = map[string]asyncMethod{
			"available": func(_ []byte, reply func([]byte, error)) { reply([]byte{0}, nil) },
			"signIn":    failing("not available"),
			"state":     failing("not available"),
		}
		m["review"] = map[string]asyncMethod{"request": answerNil}
		m["push"] = pushModule()
		m["transcribe"] = map[string]asyncMethod{
			"check": func(args []byte, reply func([]byte, error)) {
				b, err := json.Marshal(map[string]any{"onDevice": false, "language": string(args)})
				reply(b, err)
			},
			"file": failing("not available"),
		}
		m["contacts"] = h.contactsModule()
		m["calendar"] = h.calendarModule()
		m["background"] = h.backgroundModule()
		m["clipboard"] = h.clipboardModule()
		m["library"] = h.libraryModule()
	})
}

// Screen.

var screenState struct {
	preventing  bool
	stopShown   func()
	screenshots []func([]byte, error)
	stopWatch   func()
}

func (h *Host) screenModule() map[string]asyncMethod {
	s := &screenState
	return map[string]asyncMethod{
		"lock": answerNil,
		"bars": answerNil,
		"preventCapture": func(args []byte, reply func([]byte, error)) {
			s.preventing = len(args) > 0 && args[0] == 1
			for _, w := range h.topLevelWindows() {
				ExcludeFromCapture(w, s.preventing)
			}
			if s.preventing && s.stopShown == nil {
				// The windows that show later too (a sheet, a ui.Window).
				s.stopShown = WatchWindowsShown(func(hwnd uintptr) {
					if s.preventing && TopLevel(hwnd) == hwnd {
						ExcludeFromCapture(hwnd, true)
					}
				})
			} else if !s.preventing && s.stopShown != nil {
				s.stopShown()
				s.stopShown = nil
			}
			reply(nil, nil)
		},
		"screenshot": func(_ []byte, reply func([]byte, error)) {
			if err := h.watchScreenshots(); err != nil {
				reply(nil, err)
				return
			}
			s.screenshots = append(s.screenshots, reply)
		},
		"stopScreenshots": func(_ []byte, reply func([]byte, error)) {
			ws := s.screenshots
			s.screenshots = nil
			for _, w := range ws {
				w(nil, errors.New("stopped"))
			}
			if s.stopWatch != nil {
				s.stopWatch()
				s.stopWatch = nil
			}
			reply(nil, nil)
		},
	}
}

// topLevelWindows are the app's top-level windows: its own, its
// ui.Windows, sheets and popovers.
func (h *Host) topLevelWindows() []uintptr {
	var out []uintptr
	seen := map[uintptr]bool{}
	add := func(w uintptr) {
		if w != 0 && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	add(h.window)
	for hwnd := range h.byHwnd {
		if t := TopLevel(hwnd); t != 0 && t != h.park {
			add(t)
		}
	}
	for hwnd := range ownWindows {
		if TopLevel(hwnd) == hwnd && hwnd != ownMsgWin {
			add(hwnd)
		}
	}
	return out
}

// watchScreenshots watches the Screenshots folder: a file added there is
// a screenshot.
func (h *Host) watchScreenshots() error {
	s := &screenState
	if s.stopWatch != nil {
		return nil
	}
	dir, err := KnownFolder(FolderScreenshots)
	if err != nil {
		return errors.New("not available")
	}
	count := func() int {
		es, _ := os.ReadDir(dir)
		return len(es)
	}
	last := count()
	stop, err := WatchFolder(dir, func() {
		n := count()
		added := n > last
		last = n
		if !added {
			return
		}
		h.Post(func() {
			ws := s.screenshots
			s.screenshots = nil
			for _, w := range ws {
				w(nil, nil)
			}
		})
	})
	if err != nil {
		return errors.New("not available")
	}
	s.stopWatch = stop
	return nil
}

// Sensors.

// sensorsModule is ryui/sensors': none available, as on a Mac. Starting
// one fails "not available", and so do "next" (which ends the Go side's
// watching) and "steps".
func sensorsModule() map[string]asyncMethod {
	na := failing("not available")
	return map[string]asyncMethod{
		"available": func(_ []byte, reply func([]byte, error)) { reply([]byte{0}, nil) },
		"start":     na,
		"stop":      answerNil,
		"stopAll":   answerNil,
		"next":      na,
		"steps":     na,
		"simulate":  na,
	}
}

// Tracking.

var iidAdvertisingManagerStatics = NewGUID("ADD3468C-A273-48CB-B346-3544522D5581")

// trackingModule is ryui/tracking's: u8 not asked, restricted, denied,
// allowed. Windows asks nothing: it's the advertising ID's setting.
func trackingModule() map[string]asyncMethod {
	status := func(_ []byte, reply func([]byte, error)) {
		st, err := ActivationFactory("Windows.System.UserProfile.AdvertisingManager", iidAdvertisingManagerStatics)
		if err != nil {
			reply([]byte{3}, nil) // no setting: as Android
			return
		}
		defer Release(st)
		var hs HString
		if Failed(ComCall(st, 6, uintptr(unsafe.Pointer(&hs)))) { // get_AdvertisingId
			reply([]byte{3}, nil)
			return
		}
		id := hs.String()
		hs.Delete()
		if id == "" {
			reply([]byte{2}, nil) // turned off
			return
		}
		reply([]byte{3}, nil)
	}
	return map[string]asyncMethod{"status": status, "request": status}
}

// Integrity.

func integrityModule() map[string]asyncMethod {
	notHere := failing("App Attest is Apple's: Windows has no app attestation")
	play := failing("Play Integrity is Android's")
	return map[string]asyncMethod{
		"supported":   func(_ []byte, reply func([]byte, error)) { reply([]byte{0}, nil) },
		"generateKey": notHere,
		"attestKey":   notHere,
		"assert":      notHere,
		"prepare":     play,
		"token":       play,
	}
}

// Push.

// pushQueues are the messages waiting for "nextReceived" and
// "nextOpened": none ever comes, but a wait ends with "stop".
var pushQueues = map[string][]func([]byte, error){}

func pushModule() map[string]asyncMethod {
	wait := func(kind string) asyncMethod {
		return func(_ []byte, reply func([]byte, error)) { pushQueues[kind] = append(pushQueues[kind], reply) }
	}
	return map[string]asyncMethod{
		"register":     failing("ryui/push: Windows gives push channels (WNS) to packaged apps only"),
		"nextReceived": wait("nextReceived"),
		"nextOpened":   wait("nextOpened"),
		"stop": func(args []byte, reply func([]byte, error)) {
			ws := pushQueues[string(args)]
			delete(pushQueues, string(args))
			for _, w := range ws {
				w(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
		"nextRewrite": failing("ryui/push: rewriting needs a notification service extension, which a Windows app doesn't have"),
		"rewritten":   answerNil,
	}
}

// Contacts and calendar.

// openFile opens a file in its app; a test replaces it.
var openFile = func(owner uintptr, path string) error { return ShellExecute(owner, "", path) }

// openForSaving writes a file for the user's app of its type (a vCard,
// an iCalendar event) to the caches and opens it there.
func (h *Host) openForSaving(name string, data []byte, none string, reply func([]byte, error)) {
	dir := filepath.Join(h.cacheDir(), "ryui-personal")
	os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		reply(nil, err)
		return
	}
	err := openFile(h.owner(), path)
	if errors.Is(err, ErrNoAssociation) {
		err = errors.New(none)
	}
	if err != nil {
		reply(nil, err)
		return
	}
	reply([]byte{0}, nil) // saved or not: the app doesn't say
}

func (h *Host) contactsModule() map[string]asyncMethod {
	return map[string]asyncMethod{
		"request": func(_ []byte, reply func([]byte, error)) { reply([]byte{1}, nil) }, // denied
		"list":    func(_ []byte, reply func([]byte, error)) { reply(nil, errNotAllowed) },
		"pick":    failing("no contact picker"),
		"add": func(args []byte, reply func([]byte, error)) {
			var c struct {
				Name, GivenName, FamilyName, Organization, JobTitle, Birthday, Note string
				Phones, Emails                                                      []struct{ Label, Value string }
			}
			if err := json.Unmarshal(args, &c); err != nil {
				reply(nil, errors.New("contacts.add: bad arguments"))
				return
			}
			esc := strings.NewReplacer(`\`, `\\`, ",", `\,`, ";", `\;`, "\n", `\n`).Replace
			var b strings.Builder
			b.WriteString("BEGIN:VCARD\r\nVERSION:3.0\r\n")
			fn := c.Name
			if fn == "" {
				fn = strings.TrimSpace(c.GivenName + " " + c.FamilyName)
			}
			if fn == "" {
				fn = c.Organization
			}
			fmt.Fprintf(&b, "N:%s;%s;;;\r\nFN:%s\r\n", esc(c.FamilyName), esc(c.GivenName), esc(fn))
			if c.Organization != "" {
				fmt.Fprintf(&b, "ORG:%s\r\n", esc(c.Organization))
			}
			if c.JobTitle != "" {
				fmt.Fprintf(&b, "TITLE:%s\r\n", esc(c.JobTitle))
			}
			label := func(l string) string {
				switch strings.ToLower(l) {
				case "mobile", "cell", "iphone":
					return "CELL"
				case "work":
					return "WORK"
				case "home":
					return "HOME"
				}
				return "OTHER"
			}
			for _, p := range c.Phones {
				fmt.Fprintf(&b, "TEL;TYPE=%s:%s\r\n", label(p.Label), esc(p.Value))
			}
			for _, e := range c.Emails {
				fmt.Fprintf(&b, "EMAIL;TYPE=INTERNET,%s:%s\r\n", label(e.Label), esc(e.Value))
			}
			if c.Birthday != "" {
				fmt.Fprintf(&b, "BDAY:%s\r\n", c.Birthday)
			}
			if c.Note != "" {
				fmt.Fprintf(&b, "NOTE:%s\r\n", esc(c.Note))
			}
			b.WriteString("END:VCARD\r\n")
			name := safeFileName(fn, "contact") + ".vcf"
			h.openForSaving(name, []byte(b.String()), "no contacts", reply)
		},
	}
}

func (h *Host) calendarModule() map[string]asyncMethod {
	return map[string]asyncMethod{
		"request":   func(_ []byte, reply func([]byte, error)) { reply([]byte{1}, nil) }, // denied
		"calendars": func(_ []byte, reply func([]byte, error)) { reply(nil, errNotAllowed) },
		"events":    func(_ []byte, reply func([]byte, error)) { reply(nil, errNotAllowed) },
		"add": func(args []byte, reply func([]byte, error)) {
			var e struct {
				Title, Location, Notes, URL string
				StartMs, EndMs              int64
				AllDay                      bool
			}
			if err := json.Unmarshal(args, &e); err != nil {
				reply(nil, errors.New("calendar.add: bad arguments"))
				return
			}
			ics := iCalendar(e.Title, e.Location, e.Notes, e.URL, e.StartMs, e.EndMs, e.AllDay)
			h.openForSaving(safeFileName(e.Title, "event")+".ics", []byte(ics), "no calendar", reply)
		},
	}
}

// iCalendar is an event as an iCalendar file (RFC 5545).
func iCalendar(title, location, notes, link string, startMs, endMs int64, allDay bool) string {
	esc := strings.NewReplacer(`\`, `\\`, ",", `\,`, ";", `\;`, "\n", `\n`).Replace
	start, end := time.UnixMilli(startMs).UTC(), time.UnixMilli(endMs).UTC()
	if !end.After(start) {
		end = start.Add(time.Hour)
		if allDay {
			end = start.AddDate(0, 0, 1)
		}
	}
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//RyUI//EN\r\nBEGIN:VEVENT\r\n")
	fmt.Fprintf(&b, "UID:%d@ryui\r\nDTSTAMP:%s\r\n", time.Now().UnixNano(), time.Now().UTC().Format("20060102T150405Z"))
	if allDay {
		fmt.Fprintf(&b, "DTSTART;VALUE=DATE:%s\r\nDTEND;VALUE=DATE:%s\r\n", start.Format("20060102"), end.Format("20060102"))
	} else {
		fmt.Fprintf(&b, "DTSTART:%s\r\nDTEND:%s\r\n", start.Format("20060102T150405Z"), end.Format("20060102T150405Z"))
	}
	fmt.Fprintf(&b, "SUMMARY:%s\r\n", esc(title))
	if location != "" {
		fmt.Fprintf(&b, "LOCATION:%s\r\n", esc(location))
	}
	if notes != "" {
		fmt.Fprintf(&b, "DESCRIPTION:%s\r\n", esc(notes))
	}
	if link != "" {
		fmt.Fprintf(&b, "URL:%s\r\n", link)
	}
	b.WriteString("END:VEVENT\r\nEND:VCALENDAR\r\n")
	return b.String()
}

// safeFileName is s as a file name (or def), without what Windows
// refuses in one.
func safeFileName(s, def string) string {
	s = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.TrimRight(s, ". ")
	if s == "" {
		return def
	}
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// Background.

// bgTasks are ryui/background's tasks: a task keeps the system awake
// until the last one ends; "update" does nothing (there's no
// notification to update), and "next" (actions tapped) waits until the
// last task ends, then fails "ended".
var bgTasks struct {
	ids  map[uint32]bool
	next []func([]byte, error)
}

func (h *Host) backgroundModule() map[string]asyncMethod {
	id := func(args []byte) uint32 {
		var o struct {
			ID uint32 `json:"id"`
		}
		json.Unmarshal(args, &o)
		return o.ID
	}
	return map[string]asyncMethod{
		"start": func(args []byte, reply func([]byte, error)) {
			if bgTasks.ids == nil {
				bgTasks.ids = map[uint32]bool{}
			}
			bgTasks.ids[id(args)] = true
			KeepSystemAwake(true)
			reply(nil, nil)
		},
		"update": answerNil,
		"end": func(args []byte, reply func([]byte, error)) {
			endBackgroundTask(id(args))
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			if len(bgTasks.ids) == 0 {
				reply(nil, errors.New("ended"))
				return
			}
			bgTasks.next = append(bgTasks.next, reply)
		},
	}
}

func endBackgroundTask(id uint32) {
	if !bgTasks.ids[id] {
		return
	}
	delete(bgTasks.ids, id)
	if len(bgTasks.ids) > 0 {
		return
	}
	KeepSystemAwake(false)
	ws := bgTasks.next
	bgTasks.next = nil
	for _, w := range ws {
		w(nil, errors.New("ended"))
	}
}

// The clipboard.

// clipWatch is the clipboard module's watching: "next" waits for the
// clipboard's sequence number to move, which a timer checks while one
// waits (as on a Mac).
var clipWatch struct {
	waiting []func([]byte, error)
	seen    uint32
	stop    func()
}

// clipboardModule is ryui/device's clipboard: "has" (u8 text, image,
// URL), "image" (the clipboard's image as a PNG in the caches; empty:
// none), "copyImage" (a file's path), and, while watching, "next"
// (answered at the next change) and "stop".
func (h *Host) clipboardModule() map[string]asyncMethod {
	c := &clipWatch
	return map[string]asyncMethod{
		"has": func(_ []byte, reply func([]byte, error)) {
			text, img, url := ClipboardHas()
			reply([]byte{b2u(text), b2u(img), b2u(url)}, nil)
		},
		"image": func(_ []byte, reply func([]byte, error)) {
			data := ClipboardImage(h.window)
			if data == nil {
				reply(nil, nil)
				return
			}
			dir := filepath.Join(h.cacheDir(), "ryui-clipboard")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				reply(nil, err)
				return
			}
			f, err := os.CreateTemp(dir, "*.png")
			if err != nil {
				reply(nil, err)
				return
			}
			_, err = f.Write(data)
			f.Close()
			if err != nil {
				reply(nil, err)
				return
			}
			reply([]byte(f.Name()), nil)
		},
		"copyImage": func(args []byte, reply func([]byte, error)) {
			path := string(args)
			b, err := os.ReadFile(path)
			if err != nil {
				reply(nil, errors.New("no file at "+path))
				return
			}
			img, format, err := decodeAnyImage(path, b)
			if err != nil {
				reply(nil, errors.New("not an image"))
				return
			}
			var pngData []byte
			if format == "png" {
				pngData = b
			} else {
				var buf bytes.Buffer
				if png.Encode(&buf, img) == nil {
					pngData = buf.Bytes()
				}
			}
			reply(nil, SetClipboardImage(h.window, img, pngData))
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			if len(c.waiting) == 0 {
				c.seen = ClipboardSequence()
			}
			c.waiting = append(c.waiting, reply)
			if c.stop == nil {
				c.stop = ownTimer(500, func() {
					if ClipboardSequence() == c.seen {
						return
					}
					ws := c.waiting
					c.waiting = nil
					c.stop()
					c.stop = nil
					for _, w := range ws {
						w(nil, nil)
					}
				})
			}
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			ws := c.waiting
			c.waiting = nil
			if c.stop != nil {
				c.stop()
				c.stop = nil
			}
			for _, w := range ws {
				w(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

// decodeAnyImage decodes an image file with Go's decoders, else Windows's
// (WIC: HEIC, WebP, TIFF...), upright.
func decodeAnyImage(path string, data []byte) (stdimage.Image, string, error) {
	if img, format, err := stdimage.Decode(bytes.NewReader(data)); err == nil {
		return img, format, nil
	}
	img, err := DecodeImageFile(path, 0)
	return img, "", err
}

// The library.

// libArgs is library.save's JSON.
type libArgs struct {
	Path       string `json:"path"`
	Collection int    `json:"collection"`
	Name       string `json:"name"`
	Folder     string `json:"folder"`
	MIME       string `json:"mime"`
	Move       bool   `json:"move"`
}

// libDir is the folder of a collection: Downloads (0), Videos (1), Music
// (2), Pictures (3). A test replaces it.
var libDir = func(collection int) (string, error) {
	id := FolderDownloads
	switch collection {
	case 1:
		id = FolderVideos
	case 2:
		id = FolderMusic
	case 3:
		id = FolderPictures
	}
	return KnownFolder(id)
}

func (h *Host) libraryModule() map[string]asyncMethod {
	return map[string]asyncMethod{
		"save": func(args []byte, reply func([]byte, error)) {
			var o libArgs
			if err := json.Unmarshal(args, &o); err != nil || o.Path == "" {
				reply(nil, errors.New("bad arguments"))
				return
			}
			if _, err := os.Stat(o.Path); err != nil {
				reply(nil, errors.New("no file at "+o.Path))
				return
			}
			name := o.Name
			if name == "" {
				name = filepath.Base(o.Path)
			}
			dir, err := libDir(o.Collection)
			if err != nil {
				reply(nil, err)
				return
			}
			h.offMain(func() ([]byte, error) {
				p, err := libSaveFile(o.Path, dir, o.Folder, safeFileName(name, "file"), o.Move)
				return []byte(p), err
			}, reply)
		},
	}
}

// libSaveFile copies (or moves) src into dir/folder as name, numbered
// "name (1).ext"... if it's taken, and answers the new path.
func libSaveFile(src, dir, folder, name string, move bool) (string, error) {
	if folder != "" {
		dir = filepath.Join(dir, safeFileName(folder, "RyUI"))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	dst := filepath.Join(dir, name)
	for i := 1; ; i++ {
		if _, err := os.Lstat(dst); os.IsNotExist(err) {
			break
		}
		dst = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
	if move {
		if err := os.Rename(src, dst); err == nil { // the same volume
			return dst, nil
		}
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := out.ReadFrom(in); err != nil {
		out.Close()
		os.Remove(dst)
		return "", err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return "", err
	}
	if move {
		in.Close()
		os.Remove(src)
	}
	return dst, nil
}
