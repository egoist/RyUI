//go:build windows && amd64

package windows

import (
	"encoding/binary"
	"errors"
	"math"
	"time"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

func (h *Host) command(n *node, c ops.CommandID, args []byte) {
	switch c {
	case ops.CmdFocus:
		if n.ctl != 0 {
			SetFocus(n.ctl)
		} else if n.hwnd != 0 {
			SetFocus(n.hwnd)
		}
	case ops.CmdBlur:
		if f := Focus(); f != 0 && (f == n.ctl || f == n.hwnd) {
			SetFocus(RootWindow(f))
		}
	case ops.CmdListUpdate:
		if n.list != nil {
			n.list.update(h, args)
		}
	case ops.CmdPresent:
		h.present(n)
	case ops.CmdHaptic:
		// A PC has no haptics to play.
	case ops.CmdAnimateBegin:
		h.beginAnimation(args)
	case ops.CmdAnimateEnd:
		h.endAnimation()
	case ops.CmdOpenURL:
		OpenURL(string(args))
	case ops.CmdCall:
		h.call(n, args)
	case ops.CmdCopy:
		SetClipboardText(h.window, string(args))
	case ops.CmdPlay:
		if n.web != nil && n.web.video != nil {
			n.web.play()
		}
	case ops.CmdPause:
		if n.web != nil && n.web.video != nil {
			n.web.pause()
		}
	case ops.CmdSeek:
		if n.web != nil && n.web.video != nil && len(args) >= 8 {
			n.web.seek(math.Float64frombits(binary.LittleEndian.Uint64(args)))
		}
	case ops.CmdEval:
		if n.web != nil {
			n.web.eval(string(args))
		}
	case ops.CmdWebNavigate:
		if n.web != nil && len(args) > 0 {
			n.web.navigate(args[0])
		}
	case ops.CmdFrames:
		h.setFrames(len(args) > 0 && args[0] != 0)
	case ops.CmdAnnounce:
		// Not yet on Windows (UI Automation's notifications).
	case ops.CmdShare:
		// Windows's share sheet needs a WinRT DataTransferManager: the
		// text is copied instead.
		SetClipboardText(h.window, string(args))
	case ops.CmdScrollTo:
		if n.list != nil && len(args) >= 5 {
			n.list.scrollToItem(h, int(binary.LittleEndian.Uint32(args)))
		}
	case ops.CmdScrollToOffset:
		if len(args) >= 9 {
			h.scrollToOffset(n, f32(args, 0), f32(args, 4))
		}
	case ops.CmdAccent:
		if len(args) >= 8 {
			h.setAccent(ops.Color(binary.LittleEndian.Uint64(args)))
		}
	case ops.CmdMenuBar:
		h.setMenuBar(args)
	case ops.CmdWindowOptions:
		h.windowOptions(args)
	case ops.CmdColorScheme:
		if len(args) > 0 {
			h.setColorScheme(ops.ColorScheme(args[0]))
		}
	}
}

// setAccent makes c the app's accent: Accent colors, filled buttons,
// switches, sliders, links.
func (h *Host) setAccent(c ops.Color) {
	if c == h.accent {
		return
	}
	h.accent = c
	h.theme = newPalette(h.dark, c)
	RedrawWindow(h.window, RDW_INVALIDATE|RDW_ALLCHILDREN)
}

var scheme ops.ColorScheme

// setColorScheme shows the app light or dark whatever the system's
// setting, or follows it again.
func (h *Host) setColorScheme(s ops.ColorScheme) {
	scheme = s
	dark := !AppsUseLightTheme()
	switch s {
	case ops.ColorSchemeLight:
		dark = false
	case ops.ColorSchemeDark:
		dark = true
	}
	if dark != h.dark {
		h.setDark(dark)
		h.environmentChanged()
	}
}

// popUp shows a view's menu: below the view (a button's), or at the
// mouse (a context menu). The menu runs a modal loop, so it shows once
// the core has returned; the item chosen is reported with Action.
func (h *Host) popUp(n *node, anchor uintptr, below bool) {
	id := n.id
	h.afterCore(func() {
		n := h.nodes[id]
		if n == nil || len(n.menu) == 0 || anchor == 0 {
			return
		}
		m, om := h.actionMenu(n.menu)
		p := CursorPos()
		flags := uint32(TPM_LEFTALIGN | TPM_TOPALIGN | TPM_RIGHTBUTTON)
		if below {
			r := WindowRect(anchor)
			p = Point{X: r.Left, Y: r.Bottom + h.px(n, 4)}
		}
		h.modal++
		if om != nil {
			h.menuLoop(true) // before the menu window is made
		}
		got := TrackPopupMenu(m, flags, p.X, p.Y, RootWindow(anchor))
		h.menuLoop(false)
		h.modal--
		DestroyMenu(m)
		h.forget(om)
		if got > 0 {
			if n := h.nodes[id]; n != nil {
				h.emit(n, ops.Event{Kind: ops.EventAction, Int: int64(got - 1)})
			}
		}
	})
}

// animation moves views to their new frames over a duration (AnimateBegin
// to AnimateEnd), a step per timer tick: child windows have no animator.
type animation struct {
	duration float64
	curve    ops.AnimationCurve
	active   bool
	start    time.Time
	tweens   map[*node]*tween
	running  bool
}

type tween struct{ from, to [4]float32 }

func (h *Host) beginAnimation(args []byte) {
	if len(args) < 5 {
		return
	}
	d := float64(f32(args, 0))
	if h.env.ReduceMotion || d <= 0 {
		return
	}
	if h.anim == nil || !h.anim.running {
		h.anim = &animation{tweens: map[*node]*tween{}}
	}
	h.anim.duration, h.anim.curve, h.anim.active = d, ops.AnimationCurve(args[4]), true
}

func (h *Host) endAnimation() {
	a := h.anim
	if a == nil || !a.active {
		return
	}
	a.active = false
	if len(a.tweens) == 0 {
		if !a.running {
			h.anim = nil
		}
		return
	}
	a.start = time.Now()
	if !a.running {
		a.running = true
		SetTimer(h.msgWin, timerAnimate, 10)
	}
}

func (a *animation) add(n *node, from, to [4]float32) {
	if t := a.tweens[n]; t != nil {
		t.to = to
		return
	}
	a.tweens[n] = &tween{from: from, to: to}
}

func (a *animation) step(h *Host) {
	t := time.Since(a.start).Seconds() / a.duration
	if t > 1 {
		t = 1
	}
	e := ease(a.curve, t)
	for n, tw := range a.tweens {
		if n.hwnd == 0 {
			continue
		}
		var f [4]float32
		for i := range f {
			f[i] = tw.from[i] + (tw.to[i]-tw.from[i])*float32(e)
		}
		h.placeAt(n, f)
	}
	if t >= 1 {
		KillTimer(h.msgWin, timerAnimate)
		a.running = false
		if !a.active {
			h.anim = nil
		} else {
			a.tweens = map[*node]*tween{}
		}
	}
}

func ease(c ops.AnimationCurve, t float64) float64 {
	switch c {
	case ops.AnimationCurveLinear:
		return t
	case ops.AnimationCurveEaseIn:
		return t * t * t
	case ops.AnimationCurveEaseOut, ops.AnimationCurveSpring:
		u := 1 - t
		return 1 - u*u*u
	}
	if t < 0.5 {
		return 4 * t * t * t
	}
	u := -2*t + 2
	return 1 - u*u*u/2
}

// setFrames sends Frame about each display refresh (a 16 ms timer: Win32
// has no vsync callback for a window) while it's on.
func (h *Host) setFrames(on bool) {
	if on == h.frames {
		return
	}
	h.frames = on
	if on {
		SetTimer(h.msgWin, timerFrame, 16)
	} else {
		KillTimer(h.msgWin, timerFrame)
	}
}

var monoStart = time.Now()

func (h *Host) tick() {
	if !h.frames || h.root == nil {
		return
	}
	h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventFrame, Float: time.Since(monoStart).Seconds()})
}

// call runs a module's method (ui.Call) and answers with Result.
func (h *Host) call(root *node, args []byte) {
	if len(args) < 4 {
		return
	}
	callID := binary.LittleEndian.Uint32(args)
	module, rest := readString(args[4:])
	method, rest := readString(rest)
	var res []byte
	var err error
	if f := h.module(module); f != nil {
		res, err = f(method, rest)
	} else {
		err = errors.New("ryui: " + module + " isn't on Windows yet")
	}
	out := binary.LittleEndian.AppendUint32(nil, callID)
	if err != nil {
		out = append(out, 0)
		out = append(out, err.Error()...)
	} else {
		out = append(out, 1)
		out = append(out, res...)
	}
	ev := ops.Event{ID: root.id, Kind: ops.EventResult, Bytes: out}
	// Answered after the batch, as a host's modules do.
	h.Post(func() { h.core.Dispatch(ev) })
}

// module is a native module's methods (ui.Call), by name: the ones the
// Windows host has.
func (h *Host) module(name string) func(method string, args []byte) ([]byte, error) {
	if h.modules == nil {
		h.modules = map[string]func(method string, args []byte) ([]byte, error){}
	}
	return h.modules[name]
}

// actionMenu is a popup menu of actions (but the cancel one), item i+1
// action i's; owner-drawn if one is destructive.
func (h *Host) actionMenu(actions []action) (uintptr, *ownerMenu) {
	m := CreatePopupMenu()
	var items []menuItem
	destructive := false
	for i, a := range actions {
		if a.role == ops.AlertButtonRoleCancel {
			continue
		}
		AppendMenu(m, MF_STRING, uintptr(i+1), a.title)
		items = append(items, menuItem{text: a.title, destructive: a.role == ops.AlertButtonRoleDestructive})
		destructive = destructive || a.role == ops.AlertButtonRoleDestructive
	}
	if !destructive {
		return m, nil
	}
	return m, h.ownerDrawMenu(m, items)
}
