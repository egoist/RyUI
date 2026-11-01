package ops

import (
	"math"
	"strings"
)

// Item is a thing shared or dragged: a text, a link or a file. It's an
// item of a Shared or Drop event, or of a view's DragItems.
type Item struct {
	Kind ShareKind
	Text string // a text's text, a link's URL
	Path string // a file's
	Name string // the file's name where it came from
	MIME string
	Size int64 // Shared and Drop: the file's size in bytes, -1 if unknown
	// Value marks a DragItems item that carries an app's value, which
	// only the app's own drops get.
	Value bool
}

// AppendItems appends items as a Shared event (and a Drop event, after
// its header) carries them: u32 count, then per item u8 ShareKind,
// string text, string path, string name, string MIME type, i64 size.
func AppendItems(b []byte, items []Item) []byte {
	b = le.AppendUint32(b, uint32(len(items)))
	for i := range items {
		it := &items[i]
		b = append(b, byte(it.Kind))
		b = appendStr(b, it.Text)
		b = appendStr(b, it.Path)
		b = appendStr(b, it.Name)
		b = appendStr(b, it.MIME)
		b = le.AppendUint64(b, uint64(it.Size))
	}
	return b
}

// DecodeItems reads what AppendItems writes; a short list ends where
// it's cut.
func DecodeItems(b []byte) []Item {
	r := &reader{b: b}
	n := r.u32()
	var items []Item
	for i := uint32(0); i < n && r.err == nil; i++ {
		it := Item{Kind: ShareKind(r.u8())}
		it.Text = string(r.bytes())
		it.Path = string(r.bytes())
		it.Name = string(r.bytes())
		it.MIME = string(r.bytes())
		it.Size = int64(r.u64())
		if r.err != nil {
			break
		}
		items = append(items, it)
	}
	return items
}

// AppendDragItems appends items as a view's DragItems prop carries them:
// u32 count, then per item u8 ShareKind, string text, string path,
// string name, string MIME type, u8 flags (1: Value).
func AppendDragItems(b []byte, items []Item) []byte {
	b = le.AppendUint32(b, uint32(len(items)))
	for i := range items {
		it := &items[i]
		b = append(b, byte(it.Kind))
		b = appendStr(b, it.Text)
		b = appendStr(b, it.Path)
		b = appendStr(b, it.Name)
		b = appendStr(b, it.MIME)
		var f byte
		if it.Value {
			f |= 1
		}
		b = append(b, f)
	}
	return b
}

// DecodeDragItems reads what AppendDragItems writes; a short list ends
// where it's cut.
func DecodeDragItems(b []byte) []Item {
	r := &reader{b: b}
	n := r.u32()
	var items []Item
	for i := uint32(0); i < n && r.err == nil; i++ {
		it := Item{Kind: ShareKind(r.u8()), Size: -1}
		it.Text = string(r.bytes())
		it.Path = string(r.bytes())
		it.Name = string(r.bytes())
		it.MIME = string(r.bytes())
		it.Value = r.u8()&1 != 0
		if r.err != nil {
			break
		}
		items = append(items, it)
	}
	return items
}

// Drop is a Drop event's payload.
type Drop struct {
	X, Y float32 // the drop's place in the view, in points
	// Source is the view it was dragged from, for a drag from the app
	// (whose items the core has); 0 for another app's.
	Source NodeID
	// Copied: the files are the app's own copies, in its caches.
	Copied bool
	Items  []Item
}

// AppendDrop appends a Drop event's payload: f32 x, y, u32 source, u8
// flags (1: Copied), then the items (AppendItems).
func AppendDrop(b []byte, d *Drop) []byte {
	b = le.AppendUint32(b, math.Float32bits(d.X))
	b = le.AppendUint32(b, math.Float32bits(d.Y))
	b = le.AppendUint32(b, uint32(d.Source))
	var f byte
	if d.Copied {
		f |= 1
	}
	b = append(b, f)
	return AppendItems(b, d.Items)
}

// DecodeDrop reads a Drop event's payload; ok is false if its header is
// cut short.
func DecodeDrop(b []byte) (d Drop, ok bool) {
	if len(b) < 13 {
		return d, false
	}
	d.X = math.Float32frombits(le.Uint32(b))
	d.Y = math.Float32frombits(le.Uint32(b[4:]))
	d.Source = NodeID(le.Uint32(b[8:]))
	d.Copied = b[12]&1 != 0
	d.Items = DecodeItems(b[13:])
	return d, true
}

// AppendStrings appends a list of strings: u32 count, then each as a
// u32-length string (DropAccept's patterns).
func AppendStrings(b []byte, s []string) []byte {
	b = le.AppendUint32(b, uint32(len(s)))
	for _, x := range s {
		b = appendStr(b, x)
	}
	return b
}

// DecodeStrings reads what AppendStrings writes.
func DecodeStrings(b []byte) []string {
	r := &reader{b: b}
	n := r.u32()
	var out []string
	for i := uint32(0); i < n && r.err == nil; i++ {
		s := string(r.bytes())
		if r.err != nil {
			break
		}
		out = append(out, s)
	}
	return out
}

// MatchMIME reports whether a MIME type matches any of the patterns: an
// exact type, a type's family (image/*) or anything (*/*, *), without
// regard to case or parameters (text/plain; charset=utf-8).
func MatchMIME(patterns []string, mime string) bool {
	mime = baseMIME(mime)
	for _, p := range patterns {
		p = baseMIME(p)
		switch {
		case p == "*" || p == "*/*":
			return true
		case strings.HasSuffix(p, "/*"):
			if len(mime) > len(p)-1 && strings.EqualFold(mime[:len(p)-1], p[:len(p)-1]) {
				return true
			}
		case strings.EqualFold(p, mime):
			return true
		}
	}
	return false
}

// baseMIME is a MIME type without its parameters and spaces.
func baseMIME(m string) string {
	if i := strings.IndexByte(m, ';'); i >= 0 {
		m = m[:i]
	}
	return strings.TrimSpace(m)
}

func appendStr(b []byte, s string) []byte {
	b = le.AppendUint32(b, uint32(len(s)))
	return append(b, s...)
}
