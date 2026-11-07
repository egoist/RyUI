//go:build windows && amd64

package windows

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unsafe"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

// A TextField's selection, insertions and pastes of content the app
// takes (TextFieldProps.Selection, InsertText, PasteTypes). An EDIT
// counts UTF-16 units as the core does, but a multiline one's lines end
// in \r\n where the core's end in \n: offsets are mapped between them.

// editOffset is the EDIT's offset for the core's UTF-16 offset u.
func editOffset(n *node, u int) int {
	if !n.multiline || n.richText { // a rich edit's line breaks are one \r

		return u
	}
	units, lines := 0, 0
	for _, r := range n.text {
		if units >= u {
			break
		}
		if r == '\n' {
			lines++
		}
		units += utf16.RuneLen(r)
	}
	return u + lines
}

// coreOffset is the core's UTF-16 offset for the EDIT's offset e.
func coreOffset(n *node, e int) int {
	if !n.multiline || n.richText {
		return e
	}
	units, edit := 0, 0
	for _, r := range n.text {
		step := utf16.RuneLen(r)
		if r == '\n' {
			step = 2 // \r\n in the EDIT
		}
		if edit+step > e {
			break
		}
		edit += step
		units += utf16.RuneLen(r)
	}
	return units
}

// editSelection is the EDIT's selection, in its own offsets (whole
// DWORDs: the packed result is 16-bit).
func editSelection(ctl uintptr) (uint32, uint32) {
	var s, e uint32
	SendMessage(ctl, EM_GETSEL, uintptr(unsafe.Pointer(&s)), uintptr(unsafe.Pointer(&e)))
	return s, e
}

// setSelection puts the cursor (or the selection) at the core's UTF-16
// offsets, clamped to the text.
func (h *Host) setSelection(n *node, b []byte) {
	if len(b) < 8 || n.ctl == 0 {
		return
	}
	s, e := binary.LittleEndian.Uint32(b), binary.LittleEndian.Uint32(b[4:])
	length := uint32(len(utf16.Encode([]rune(n.text))))
	s, e = min(s, length), min(e, length)
	n.selection = [2]uint32{s, e}
	SendMessage(n.ctl, EM_SETSEL, uintptr(editOffset(n, int(s))), uintptr(editOffset(n, int(e))))
}

// checkSelection reports the field's selection after what may have
// moved it (a key, a click, a paste), once its text change is reported,
// unless it's where the core put it.
func (h *Host) checkSelection(n *node) {
	if n.selQueued || !n.listening(ops.EventSelectionChanged) {
		return
	}
	n.selQueued = true
	h.Post(func() {
		n.selQueued = false
		if n.ctl == 0 || h.nodes[n.id] != n {
			return
		}
		es, ee := editSelection(n.ctl)
		sel := [2]uint32{uint32(coreOffset(n, int(es))), uint32(coreOffset(n, int(ee)))}
		if sel == n.selection {
			return
		}
		n.selection = sel
		out := binary.LittleEndian.AppendUint32(nil, sel[0])
		out = binary.LittleEndian.AppendUint32(out, sel[1])
		h.emit(n, ops.Event{Kind: ops.EventSelectionChanged, Bytes: out})
	})
}

// movesSelection reports whether an EDIT's message can move its
// selection.
func movesSelection(m uint32, w uintptr) bool {
	switch m {
	case WM_KEYDOWN, WM_KEYUP, WM_CHAR, WM_LBUTTONDOWN, WM_LBUTTONUP, WM_LBUTTONDBLCLK,
		WM_PASTE, WM_CUT, WM_CLEAR, WM_UNDO, EM_REPLACESEL:
		return true
	case WM_MOUSEMOVE:
		return w&MK_LBUTTON != 0
	}
	return false
}

// insertText puts text in place of the selection as typing does:
// undoably, and reported as typing is (EN_CHANGE).
func (h *Host) insertText(n *node, s string) {
	if n.ctl == 0 {
		return
	}
	if n.multiline {
		s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\n", "\r\n")
	}
	SendMessage(n.ctl, EM_REPLACESEL, 1, Str(s))
	h.checkSelection(n)
}

// pasteContent takes the clipboard's content of the field's types, if
// it has any (files, PNG and bitmap images, copied into the app's
// caches), and reports it with Pasted; false: none, the EDIT pastes its
// text as it would.
func (h *Host) pasteContent(n *node) bool {
	if len(n.pasteTypes) == 0 || !n.listening(ops.EventPasted) {
		return false
	}
	data := OleGetClipboard()
	if data == 0 {
		return false
	}
	defer Release(data)
	var out []ops.Item
	dir := ""
	place := func(name string) string {
		if dir == "" {
			if dir = h.dropDir(); dir == "" {
				return ""
			}
		}
		return filepath.Join(dir, name)
	}
	items, copied := h.droppedItems(data, n.pasteTypes)
	for _, it := range items {
		if it.Path != "" && !copied {
			// Another app's file: the app's own copy, as a paste's are.
			p := place(filepath.Base(it.Path))
			if p == "" || copyFile(it.Path, p) != nil {
				continue
			}
			it.Path = p
		}
		out = append(out, it)
	}
	if !hasFiles(out) {
		if img, name, mime := clipboardImage(data); img != nil && ops.MatchMIME(n.pasteTypes, mime) {
			if p := place(name); p != "" && os.WriteFile(p, img, 0o644) == nil {
				out = append(out, ops.Item{Kind: ops.ShareKindImage, Path: p, Name: name, MIME: mime, Size: int64(len(img))})
			}
		}
	}
	// Text alone the EDIT pastes itself, unless the app takes text.
	if len(out) == 0 || !hasFiles(out) && !ops.MatchMIME(n.pasteTypes, "text/plain") {
		return false
	}
	h.emit(n, ops.Event{Kind: ops.EventPasted, Bytes: ops.AppendItems(nil, out)})
	return true
}

func hasFiles(items []ops.Item) bool {
	for _, it := range items {
		if it.Path != "" {
			return true
		}
	}
	return false
}

// clipboardImage is an image on the clipboard: a PNG (as browsers and
// Office put one), else the bitmap as a .bmp file.
func clipboardImage(data uintptr) ([]byte, string, string) {
	if b, ok := GetData(data, RegisterClipboardFormat("PNG")); ok && len(b) > 8 {
		return b, "Image.png", "image/png"
	}
	for _, cf := range []uint16{CF_DIBV5, CF_DIB} {
		b, ok := GetData(data, cf)
		if !ok || len(b) < 40 {
			continue
		}
		return bmpFile(b), "Image.bmp", "image/bmp"
	}
	return nil, "", ""
}

// bmpFile makes a packed DIB a .bmp file: a BITMAPFILEHEADER before it,
// its pixels after the header and the color table.
func bmpFile(dib []byte) []byte {
	le := binary.LittleEndian
	header := le.Uint32(dib)
	bits := uint32(le.Uint16(dib[14:]))
	compression := le.Uint32(dib[16:])
	colors := le.Uint32(dib[32:])
	if colors == 0 && bits <= 8 {
		colors = 1 << bits
	}
	masks := uint32(0)
	if header == 40 && compression == 3 { // BI_BITFIELDS after a BITMAPINFOHEADER
		masks = 12
	}
	offset := 14 + header + masks + colors*4
	out := make([]byte, 14, 14+len(dib))
	out[0], out[1] = 'B', 'M'
	le.PutUint32(out[2:], uint32(14+len(dib)))
	le.PutUint32(out[10:], offset)
	return append(out, dib...)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
