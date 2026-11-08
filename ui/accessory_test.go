package ui_test

import (
	"testing"

	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// A field's accessory is a root as wide as the screen and as tall as its
// content, named by the field, and goes with it.
func TestAccessory(t *testing.T) {
	shown := signal.New(true)
	text := signal.New("")
	h, r := mount(t, func() ui.Node {
		return ui.Show(shown, func() ui.Node {
			return ui.TextField(ui.TextFieldProps{Value: text, Accessory: func() ui.Node {
				return ui.Row(ui.Style{Padding: ui.All(8)},
					ui.Text(signal.Func(func() string { return string(rune('0' + len(text.Get()))) }), ui.Style{Grow: 1}),
					ui.Button(ui.ButtonProps{Title: ui.Val("Done")}))
			}})
		})
	})
	acc := h.OfKind(ops.KindAccessory)
	if len(acc) != 1 {
		t.Fatal("no accessory")
	}
	a := acc[0]
	field := h.OfKind(ops.KindTextField)[0]
	if ops.NodeID(field.Int(ops.PropTextFieldAccessory)) != a.ID {
		t.Fatal("the field doesn't name it")
	}
	if w, ht := a.Float(ops.PropAccessoryWidth), a.Float(ops.PropAccessoryHeight); w != 390 || ht != 44+16 {
		t.Fatalf("accessory %vx%v", w, ht)
	}
	if f := h.One(ops.KindButton, "Done").Frame; f[0]+f[2] != 382 {
		t.Fatalf("Done at %v", f)
	}
	h.Type(field, "abc")
	r.Flush()
	if h.One(ops.KindText, "3") == nil {
		t.Fatal("the accessory doesn't follow its signals")
	}
	shown.Set(false)
	r.Flush()
	if len(h.OfKind(ops.KindAccessory)) != 0 || len(h.OfKind(ops.KindButton)) != 0 {
		t.Fatal("the accessory outlived its field")
	}
}
