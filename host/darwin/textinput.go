//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"io"
	"os"
	"path/filepath"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
)

// A TextField's selection, insertions and pastes of content the app
// takes (TextFieldProps.Selection, InsertText, PasteTypes). A one-line
// field is edited by its window's field editor, an NSTextView; the host
// gives fields a field editor of its own class (RyUIFieldEditor), whose
// paste: hands the app what it takes, and a multiline field's text view
// is of that class too. Offsets are UTF-16, as NSString's are.

// fieldEditorMethods are RyUIFieldEditor's: Paste for the content the
// field takes, and paste: taking it.
func fieldEditorMethods() []objc.MethodDef {
	return []objc.MethodDef{
		Method("paste:", func(self ID, _ SEL, sender ID) {
			if n := theHost.editorNode(self); n != nil && theHost.pasteContent(n) {
				return
			}
			SendSuper(self, "RyUIFieldEditor", "paste:", uintptr(sender))
		}),
		Method("validateUserInterfaceItem:", func(self ID, _ SEL, item ID) bool {
			if Send(item, "action") == ID(Sel("paste:")) {
				if n := theHost.editorNode(self); n != nil && len(n.pasteTypes) > 0 && pasteboardHas(n.pasteTypes) {
					return true
				}
			}
			return byte(SendSuper(self, "RyUIFieldEditor", "validateUserInterfaceItem:", uintptr(item))) != 0
		}),
	}
}

// editorNode is the field a text view edits: a multiline field's own, or
// the one-line field the field editor is editing.
func (h *Host) editorNode(tv ID) *node {
	if n := h.textViewNode(tv); n != nil {
		return n
	}
	if SendBool(tv, "isFieldEditor") {
		if n := h.byView[Send(tv, "delegate")]; n != nil && n.kind == ops.KindTextField {
			return n
		}
	}
	return nil
}

// fieldEditor is windowWillReturnFieldEditor:toObject:: a field of the
// app's gets the window's RyUIFieldEditor; anything else the window's own.
func (h *Host) fieldEditor(w, obj ID) ID {
	if n := h.byView[obj]; n == nil || n.kind != ops.KindTextField {
		return 0
	}
	if e := h.editors[w]; e != 0 {
		return e
	}
	e := Send(Send(Class("RyUIFieldEditor"), "alloc"), "init")
	Send(e, "setFieldEditor:", 1)
	h.editors[w] = e
	return e
}

// editing is the text view editing a field now, if one is.
func (h *Host) editing(n *node) ID {
	if n.tracking != 0 {
		return n.tracking
	}
	return Send(n.view, "currentEditor")
}

// setSelection puts the cursor (or the selection) at UTF-16 offsets of
// the field's text, clamped; a one-line field not being edited takes it
// as it's edited next.
func (h *Host) setSelection(n *node, b []byte) {
	if len(b) < 8 {
		return
	}
	s, e := uint(binary.LittleEndian.Uint32(b)), uint(binary.LittleEndian.Uint32(b[4:]))
	tv := h.editing(n)
	if tv == 0 {
		n.selection = [2]uint{s, e}
		n.selPending = true
		return
	}
	length := uint(SendInt(Send(tv, "string"), "length"))
	s, e = min(s, length), min(e, length)
	if e < s {
		s, e = e, s
	}
	n.selection = [2]uint{s, e}
	Send(tv, "setSelectedRange:", uintptr(s), uintptr(e-s))
}

// applyPendingSelection puts a field's selection where the core set it
// while it wasn't being edited, as its editing begins.
func (h *Host) applyPendingSelection(n *node) {
	if !n.selPending {
		return
	}
	n.selPending = false
	b := binary.LittleEndian.AppendUint32(nil, uint32(n.selection[0]))
	b = binary.LittleEndian.AppendUint32(b, uint32(n.selection[1]))
	h.setSelection(n, b)
}

// selectionChanged is NSTextViewDidChangeSelectionNotification: the
// field's selection, reported after what moved it (typing reports its
// text first), unless it's where the core put it.
func (h *Host) selectionChanged(tv ID) {
	n := h.editorNode(tv)
	if n == nil {
		return
	}
	if n.field != nil && n.tracking == tv {
		h.reportFormats(n)
	}
	if !n.listening(ops.EventSelectionChanged) || n.selQueued {
		return
	}
	n.selQueued = true
	h.Post(func() {
		n.selQueued = false
		if !h.alive(n) {
			return
		}
		tv := h.editing(n)
		if tv == 0 {
			return
		}
		r := MsgRange(tv, Sel("selectedRange"))
		sel := [2]uint{r.Location, r.Location + r.Length}
		if sel == n.selection {
			return
		}
		n.selection = sel
		b := binary.LittleEndian.AppendUint32(nil, uint32(sel[0]))
		b = binary.LittleEndian.AppendUint32(b, uint32(sel[1]))
		h.emit(n, ops.Event{Kind: ops.EventSelectionChanged, Bytes: b})
	})
}

// insertText puts text in place of the selection as typing does:
// undoably, and reported as typing is. A one-line field not being
// edited is edited first, so its field editor does it.
func (h *Host) insertText(n *node, s string) {
	tv := h.editing(n)
	if tv == 0 {
		Send(Send(n.view, "window"), "makeFirstResponder:", uintptr(n.view))
		tv = h.editing(n)
		if tv == 0 {
			return
		}
		// Where the core last put (or heard of) the selection.
		length := uint(SendInt(Send(tv, "string"), "length"))
		a, b := min(n.selection[0], length), min(n.selection[1], length)
		if b < a {
			a, b = b, a
		}
		Send(tv, "setSelectedRange:", uintptr(a), uintptr(b-a))
	}
	Send(tv, "insertText:", uintptr(String(s)))
}

// pasteboardHas reports whether the general pasteboard has content of
// the types.
func pasteboardHas(types []string) bool {
	pb := Send(Class("NSPasteboard"), "generalPasteboard")
	for _, reps := range readDrag(pb) {
		for _, r := range reps {
			if r.kind != repText && r.kind != repPromise && ops.MatchMIME(types, r.mime) {
				return true
			}
		}
	}
	return false
}

// pasteContent takes the pasteboard's items of the field's types, if it
// has any (files and image data copied into the caches), and reports
// them with Pasted; false: none, the field pastes as it would.
func (h *Host) pasteContent(n *node) bool {
	if len(n.pasteTypes) == 0 || !n.listening(ops.EventPasted) {
		return false
	}
	pb := Send(Class("NSPasteboard"), "generalPasteboard")
	items, _ := h.readItems(pb, n.pasteTypes)
	var out []ops.Item
	for _, it := range items {
		if !it.ok {
			continue
		}
		if it.Path != "" && !it.copied {
			// Another app's file: the app's own copy, as a paste's are.
			p, err := copyToDrops(it.Path)
			if err != nil {
				continue
			}
			it.Path = p
		}
		out = append(out, it.Item)
	}
	if len(out) == 0 {
		return false
	}
	h.emit(n, ops.Event{Kind: ops.EventPasted, Bytes: ops.AppendItems(nil, out)})
	return true
}

// copyToDrops copies a file into a new folder of the app's caches.
func copyToDrops(src string) (string, error) {
	dir, err := dropDir()
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, filepath.Base(src))
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	return dst, out.Close()
}
