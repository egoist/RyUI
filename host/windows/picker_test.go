//go:build windows

package windows

import (
	"math"
	"slices"
	"testing"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/layout"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A Picker is a drop-down list combo box: empty, showing its label, while
// none is chosen; the user's choice (CBN_SELCHANGE) writes the signal, and
// code's selects the item.
func TestPicker(t *testing.T) {
	pick := signal.New(-1)
	items := signal.NewFunc([]string{"Name", "Date", "Size"}, slices.Equal[[]string])
	h, r := mount(t, func() ui.Node {
		return ui.Row(ui.Style{Align: ui.AlignStart, Padding: ui.All(16)},
			ui.Picker(ui.PickerProps{Items: items, Selected: pick, Label: ui.Val("Sort by")}))
	})
	onMain(t, func() {
		p := find(h, ops.KindPicker)[0]
		if p.ctl == 0 || ClassName(p.ctl) != "ComboBox" {
			fatalf("not a combo box")
		}
		if n := SendMessage(p.ctl, CB_GETCOUNT, 0, 0); n != 3 {
			fatalf("%d items", n)
		}
		if i := int32(SendMessage(p.ctl, CB_GETCURSEL, 0, 0)); i != -1 {
			fatalf("none chosen, yet item %d is", i)
		}
		if p.pickerLabel != "Sort by" {
			fatalf("label %q", p.pickerLabel)
		}

		// The user chooses Size.
		SendMessage(p.ctl, CB_SETCURSEL, 2, 0)
		h.controlCommand(p, p.ctl, CBN_SELCHANGE)
		if pick.Peek() != 2 {
			fatalf("after choosing Size: %d", pick.Peek())
		}

		// Code's choice, then none again.
		h.enter(func() { pick.Set(0); r.Flush() })
		if i := SendMessage(p.ctl, CB_GETCURSEL, 0, 0); i != 0 {
			fatalf("after code's choice: item %d", i)
		}
		h.enter(func() { pick.Set(5); r.Flush() })
		if i := int32(SendMessage(p.ctl, CB_GETCURSEL, 0, 0)); i != -1 {
			fatalf("out of range: item %d", i)
		}

		// Right to left, it's made again, keeping its items, choice and label.
		h.enter(func() { pick.Set(1); r.Flush() })
		setLocale(h, true, "ar")
		if n, i := SendMessage(p.ctl, CB_GETCOUNT, 0, 0), SendMessage(p.ctl, CB_GETCURSEL, 0, 0); n != 3 || i != 1 {
			fatalf("made again right to left: %d items, item %d chosen", n, i)
		}
	})
}

// A Picker is drawn as WinUI's ComboBox: an owner-drawn combo box that
// keeps its items' strings, filling its view, measured as it's drawn; its
// closed box in Fluent's control fill (another under the pointer), its
// list's items on the flyout surface, the chosen one with its backplate
// and accent pill at the start (the end right to left).
func TestPickerFluent(t *testing.T) {
	pick := signal.New(-1)
	items := signal.NewFunc([]string{"Name", "Date", "Size"}, slices.Equal[[]string])
	h, r := mount(t, func() ui.Node {
		return ui.Row(ui.Style{Align: ui.AlignStart, Padding: ui.All(16)},
			ui.Picker(ui.PickerProps{Items: items, Selected: pick, Label: ui.Val("Sort by")}))
	})
	onMain(t, func() {
		p := find(h, ops.KindPicker)[0]
		const owner = CBS_OWNERDRAWFIXED | CBS_HASSTRINGS
		if s := WindowLong(p.ctl, GWL_STYLE); s&owner != owner || s&CBS_DROPDOWNLIST != CBS_DROPDOWNLIST {
			fatalf("style %#x: not an owner-drawn drop-down list with strings", s)
		}
		if p.comboList == 0 || p.comboList != ComboList(p.ctl) || ClassName(p.comboList) != "ComboLBox" {
			fatalf("list window %#x, the combo box's %#x", p.comboList, ComboList(p.ctl))
		}

		// Measured as drawn: 12 + the widest text (the label here) + the
		// chevron's 38 across, at least 64; its line with 5 and 7 down, at
		// least 32. The box fills its view; the items are WinUI's height.
		nan := float32(math.NaN())
		size := h.measure(p, layout.Constraints{Width: nan, Height: nan, AvailW: layout.Inf, AvailH: layout.Inf})
		dc := h.measureDC
		old := SelectObject(dc, h.nodeFont(p))
		widest := int32(0)
		for _, s := range []string{"Name", "Date", "Size", "Sort by"} {
			w, _ := TextExtent(dc, UTF16Units(s))
			widest = max(widest, w)
		}
		SelectObject(dc, old)
		wantW := max(12+h.pt(p, widest)+38, 64)
		wantH := max(12+h.pt(p, h.comboLineHeight(p)), 32)
		if d := size.W - wantW; d < -1 || d > 1 || size.H < 32 || size.H-wantH < -1 || size.H-wantH > 1 {
			fatalf("measured %vx%v, drawn %vx%v", size.W, size.H, wantW, wantH)
		}
		if v, c := ClientRect(p.hwnd), WindowRect(p.ctl); c.W() != v.W() || c.H() != v.H() {
			fatalf("a %dx%d box in a %dx%d view", c.W(), c.H(), v.W(), v.H())
		}
		if ih := int32(SendMessage(p.ctl, CB_GETITEMHEIGHT, 0, 0)); ih != h.comboItemHeight(p) {
			fatalf("items %d high, not %d", ih, h.comboItemHeight(p))
		}

		// None chosen: the label, as a placeholder.
		if s, ph := p.comboText(); s != "Sort by" || !ph {
			fatalf("none chosen shows %q (placeholder %v)", s, ph)
		}
		if HighContrast() {
			return // the system's colors: what follows checks Fluent's
		}

		// The closed box's fill, between the chevron and its end; under
		// the pointer, the hover fill.
		boxFill := func() (got, want rgba) {
			px, w, ht := comboBoxPixels(h, p)
			return pixelAt(px, w, w-h.px(p, 4), ht/2), h.comboLook(p, h.comboState(p, p.ctl)).fill
		}
		if got, want := boxFill(); got != want {
			fatalf("box fill %v, not %v", got, want)
		}
		p.comboHot = true
		got, want := boxFill()
		p.comboHot = false
		if got != want || want != h.theme.controlHot {
			fatalf("hovered box fill %v, not %v", got, want)
		}

		// The chosen item: its text in the box, and in the list its
		// backplate and the accent pill at the start; another item has
		// neither, unless under the pointer (its backplate alone).
		h.enter(func() { pick.Set(1); r.Flush() })
		if s, ph := p.comboText(); s != "Date" || ph {
			fatalf("Date chosen shows %q (placeholder %v)", s, ph)
		}
		checkItem(h, p, 1, 0, true, true, false)
		checkItem(h, p, 2, 0, false, false, false)
		checkItem(h, p, 2, ODS_SELECTED, true, false, false)

		// Right to left: the pill at the right, and the list again with
		// the combo box made again.
		setLocale(h, true, "ar")
		if p.comboList == 0 || p.comboList != ComboList(p.ctl) {
			fatalf("made again right to left: list %#x, the combo box's %#x", p.comboList, ComboList(p.ctl))
		}
		if s := WindowLong(p.ctl, GWL_STYLE); s&owner != owner {
			fatalf("made again right to left: style %#x", s)
		}
		checkItem(h, p, 1, 0, true, true, true)
	})
}

// checkItem draws a Picker's list item and checks its backplate (near its
// end) and its pill (at its start: the right, rtl).
func checkItem(h *Host, p *node, item int, state uint32, plate, pill, rtl bool) {
	w, ht := h.px(p, 200), h.comboItemHeight(p)
	px := comboItemPixels(h, p, item, state, w, ht)
	bg := h.comboListBg()
	wantPlate := bg
	if plate {
		wantPlate = h.theme.subtleHot.over(bg)
	}
	plateX := w - h.px(p, 8)
	pillX := h.px(p, comboItemMarX) + h.px(p, comboPillWidth)/2
	if rtl {
		plateX = h.px(p, 8)
		pillX = w - 1 - pillX
	}
	if got := pixelAt(px, w, plateX, ht/2); got != wantPlate {
		fatalf("item %d (state %#x): backplate %v, not %v", item, state, got, wantPlate)
	}
	wantPill := wantPlate
	if pill {
		wantPill = h.theme.accent
	}
	if got := pixelAt(px, w, pillX, ht/2); got != wantPill {
		fatalf("item %d (state %#x): at the pill %v, not %v", item, state, got, wantPill)
	}
}

// comboItemPixels draws a combo box's list item as its list would ask
// (WM_DRAWITEM) into a w by ht bitmap.
func comboItemPixels(h *Host, p *node, item int, state uint32, w, ht int32) []byte {
	dc := CreateCompatibleDC(0)
	bmp, bits := DIB(w, ht)
	old := SelectObject(dc, bmp)
	d := DrawItem{CtlType: ODT_COMBOBOX, ItemID: uint32(item), ItemState: state, HwndItem: p.ctl, DC: dc, Item: Rect{Right: w, Bottom: ht}}
	h.drawComboItem(p, &d)
	out := slices.Clone(bits)
	SelectObject(dc, old)
	DeleteObject(bmp)
	DeleteDC(dc)
	return out
}

// comboBoxPixels paints a combo box's closed box into a bitmap.
func comboBoxPixels(h *Host, p *node) (px []byte, w, ht int32) {
	r := ClientRect(p.ctl)
	dc := CreateCompatibleDC(0)
	bmp, bits := DIB(r.W(), r.H())
	old := SelectObject(dc, bmp)
	h.paintCombo(p, p.ctl, dc)
	out := slices.Clone(bits)
	SelectObject(dc, old)
	DeleteObject(bmp)
	DeleteDC(dc)
	return out, r.W(), r.H()
}

// pixelAt is a BGRA bitmap's pixel, opaque.
func pixelAt(px []byte, w, x, y int32) rgba {
	i := (y*w + x) * 4
	return rgba{px[i+2], px[i+1], px[i], 255}
}
