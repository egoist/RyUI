//go:build windows

package windows

import (
	"encoding/binary"
	"errors"
	"math"
	"time"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
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
	case ops.CmdInsertText:
		if n.kind == ops.KindTextField {
			h.insertText(n, string(args))
		}
	case ops.CmdFormat:
		h.formatRich(n, args)
	case ops.CmdSetLink:
		h.setRichLink(n, string(args))
	case ops.CmdPresent:
		h.present(n)
	case ops.CmdHaptic:
		// A PC has no haptics to play.
	case ops.CmdAnimateBegin:
		h.beginAnimation(args)
	case ops.CmdAnimateEnd:
		if h.batchFrames {
			h.runMatches() // the frames are applied: they step with the rest
		}
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
		h.announce(string(args))
	case ops.CmdA11yFocus:
		// Narrator follows the focus event to the view.
		if n.hwnd != 0 {
			NotifyWinEvent(EVENT_OBJECT_FOCUS, n.hwnd, OBJID_CLIENT, CHILDID_SELF)
		}
	case ops.CmdShare:
		h.shareText(string(args)) // Windows's share UI (services.go)
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
		h.windowOptions(n, args)
	case ops.CmdWindowAction:
		h.windowAction(n, args)
	case ops.CmdWindowTitle:
		h.setWindowTitle(string(args))
	case ops.CmdLocation:
		h.location(args)
	case ops.CmdLocale:
		h.setLocale(args)
	case ops.CmdSaveLocale:
		h.saveLocale(string(args))
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
		rtl := h.isRTL(n)
		m, om := h.actionMenu(n.menu, rtl)
		p := CursorPos()
		flags := uint32(TPM_LEFTALIGN | TPM_TOPALIGN | TPM_RIGHTBUTTON)
		if rtl {
			flags |= TPM_LAYOUTRTL // from the point leftward, itself mirrored
		}
		if below {
			r := WindowRect(anchor)
			p = Point{X: r.Left, Y: r.Bottom + h.px(n, 4)}
			if rtl {
				p.X = r.Right
			}
		}
		h.modal++
		if len(om) > 0 {
			h.menuLoop(true) // before the menu window is made
		}
		got := TrackPopupMenu(m, flags, p.X, p.Y, RootWindow(anchor))
		h.menuLoop(false)
		h.modal--
		DestroyMenu(m)
		for _, o := range om {
			h.forget(o)
		}
		if got > 0 {
			if n := h.nodes[id]; n != nil {
				h.emit(n, ops.Event{Kind: ops.EventAction, Int: int64(got - 1)})
			}
		}
	})
}

// popUpActions shows actions as a menu below anchor, a control of the
// node at (its direction's); the item chosen is reported to target with
// Action. It shows once the core has returned.
func (h *Host) popUpActions(target *node, anchor uintptr, at *node, actions []action) {
	h.popUpActionsFrom(target, anchor, at, actions, 0)
}

// popUpActionsFrom is popUpActions for a menu whose Action events carry
// base + an item's number (a screen's Toolbar's and Primary's).
func (h *Host) popUpActionsFrom(target *node, anchor uintptr, at *node, actions []action, base int64) {
	id, atID := target.id, at.id
	h.afterCore(func() {
		at := h.nodes[atID]
		if h.nodes[id] == nil || at == nil || !IsWindow(anchor) {
			return
		}
		rtl := h.isRTL(at)
		m, om := h.actionMenu(actions, rtl)
		r := WindowRect(anchor)
		x := r.Left
		flags := uint32(TPM_LEFTALIGN | TPM_TOPALIGN | TPM_RIGHTBUTTON)
		if rtl {
			x = r.Right
			flags |= TPM_LAYOUTRTL
		}
		h.modal++
		if len(om) > 0 {
			h.menuLoop(true)
		}
		got := TrackPopupMenu(m, flags, x, r.Bottom+h.px(at, 4), RootWindow(anchor))
		h.menuLoop(false)
		h.modal--
		DestroyMenu(m)
		for _, o := range om {
			h.forget(o)
		}
		if got > 0 {
			if n := h.nodes[id]; n != nil {
				h.emit(n, ops.Event{Kind: ops.EventAction, Int: base + int64(got-1)})
			}
		}
	})
}

// animation moves views to their new frames over a duration (AnimateBegin
// to AnimateEnd), a step per timer tick: child windows have no animator.
type animation struct {
	duration float64
	curve    ops.AnimationCurve
	delay    float64
	damping  float64 // a spring's damping ratio
	velocity float64 // a spring's initial velocity, the change's fraction per second
	active   bool
	start    time.Time
	tweens   map[*node]*tween
	running  bool
}

type tween struct {
	from, to [4]float32
	// A transition's (transition.go): the view's own drawing fades in or
	// out from its opacity; one going out is let go of at the end.
	fade, enter, exit bool
	opacity           float32
	// A transform's (transform.go): from one to the other.
	xf         bool
	xfrom, xto xform
}

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
	h.anim.delay, h.anim.damping, h.anim.velocity = 0, 0.85, 0
	if len(args) >= 22 {
		h.anim.delay = float64(f32(args, 10))
		h.anim.damping = float64(f32(args, 14))
		h.anim.velocity = float64(f32(args, 18))
	}
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
	a.start = time.Now().Add(time.Duration(a.delay * float64(time.Second)))
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
	t := max(time.Since(a.start).Seconds()/a.duration, 0) // waits out its delay at 0
	if t > 1 {
		t = 1
	}
	e := ease(a.curve, t)
	if a.curve == ops.AnimationCurveSpring {
		e = spring(t, a.duration, a.damping, a.velocity)
	}
	for n, tw := range a.tweens {
		if n.hwnd == 0 {
			continue
		}
		var f [4]float32
		for i := range f {
			f[i] = tw.from[i] + (tw.to[i]-tw.from[i])*float32(e)
		}
		if tw.xf {
			h.applyXform(n, lerpXform(tw.xfrom, tw.xto, float32(e)))
		}
		h.placeAt(n, f)
		h.stepFade(n, tw, float32(min(max(e, 0), 1)))
	}
	if t >= 1 {
		for n, tw := range a.tweens {
			if tw.exit {
				h.exited(n)
			}
		}
		KillTimer(h.msgWin, timerAnimate)
		a.running = false
		if !a.active {
			h.anim = nil
		} else {
			a.tweens = map[*node]*tween{}
		}
	}
}

// spring is a damped spring's progress at t (0 to 1) of its duration, as
// UIKit's usingSpringWithDamping: settled by the end, overshooting when
// underdamped; v0 is its initial velocity, the change's fraction per
// second.
func spring(t, duration, damping, v0 float64) float64 {
	if t >= 1 {
		return 1
	}
	dur := max(duration, 0.001)
	zeta := min(max(damping, 0.05), 1)
	w0 := max(6.9/(zeta*dur), 2*math.Pi/dur)
	s := t * dur
	decay := math.Exp(-zeta * w0 * s)
	if zeta >= 0.999 {
		return 1 - decay*(1+(w0-v0)*s)
	}
	wd := w0 * math.Sqrt(1-zeta*zeta)
	return 1 - decay*(math.Cos(wd*s)+(zeta*w0-v0)/wd*math.Sin(wd*s))
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
	if m := h.asyncModule(module)[method]; m != nil {
		id := root.id
		m(rest, func(res []byte, err error) {
			out := binary.LittleEndian.AppendUint32(nil, callID)
			if err != nil {
				out = append(append(out, 0), err.Error()...)
			} else {
				out = append(append(out, 1), res...)
			}
			ev := ops.Event{ID: id, Kind: ops.EventResult, Bytes: out}
			h.Post(func() { h.core.Dispatch(ev) })
		})
		return
	}
	var res []byte
	var err error
	if f := h.module(module); f != nil {
		res, err = f(method, rest)
	} else {
		err = errors.New("asuka: " + module + " isn't on Windows yet")
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
		h.modules = h.syncModules()
	}
	return h.modules[name]
}

// actionMenu is a popup menu of actions (but the cancel one), the
// command of each item 1 + its number: dimmed when disabled, checked,
// submenus, and sections between separators under a header. Owner-drawn
// (each of its menus that has a destructive item or a header): the
// owner-drawn ones are returned, to forget once the menu's gone.
func (h *Host) actionMenu(actions []action, rtl bool) (uintptr, []*ownerMenu) {
	order := uint32(0)
	if rtl {
		order = MF_RIGHTORDER
	}
	b := &menuBuild{h: h, base: 1, order: order}
	return b.popup(actionMenuItems(actions)), b.owned
}

// menuHeader marks a section's title among the items menuBuild makes of
// actions: shown as a header, which isn't chosen.
const menuHeader ops.MenuFlags = 1 << 7

// actionMenuItems are a menu's items for actions: a section's set off by
// separators, under a header with its title; none at the ends or
// doubled.
func actionMenuItems(actions []action) []ops.MenuItem {
	var out []ops.MenuItem
	var add func(as []action)
	add = func(as []action) {
		for i := range as {
			a := &as[i]
			if a.role == ops.AlertButtonRoleCancel {
				continue
			}
			if a.is(ops.MenuSubmenu | ops.MenuInline) {
				out = append(out, ops.MenuItem{Flags: ops.MenuSeparator})
				if a.title != "" {
					out = append(out, ops.MenuItem{Title: a.title, Flags: menuHeader | ops.MenuDisabled, Index: -1})
				}
				add(a.items)
				out = append(out, ops.MenuItem{Flags: ops.MenuSeparator})
				continue
			}
			it := ops.MenuItem{Index: int(a.index), Title: a.title, Flags: a.flags & (ops.MenuDisabled | ops.MenuChecked)}
			if a.role == ops.AlertButtonRoleDestructive {
				it.Flags |= ops.MenuDestructive
			}
			if a.is(ops.MenuSubmenu) {
				it.Flags |= ops.MenuSubmenu
				it.Items = actionMenuItems(a.items)
				if len(it.Items) == 0 {
					it.Flags |= ops.MenuDisabled // nothing to open
				}
			}
			out = append(out, it)
		}
	}
	add(actions)
	// No separator at the ends, nor one after another.
	trimmed := out[:0]
	for _, it := range out {
		if it.Is(ops.MenuSeparator) && (len(trimmed) == 0 || trimmed[len(trimmed)-1].Is(ops.MenuSeparator)) {
			continue
		}
		trimmed = append(trimmed, it)
	}
	if n := len(trimmed); n > 0 && trimmed[n-1].Is(ops.MenuSeparator) {
		trimmed = trimmed[:n-1]
	}
	return trimmed
}
