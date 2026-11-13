//go:build darwin && !ios

package darwin

import (
	"strings"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

var classesDone bool

// registerClasses makes the Objective-C classes the host uses. Their
// methods are made once and route by the object (self, the sender) to
// its node, never per view.
func registerClasses() {
	if classesDone {
		return
	}
	classesDone = true
	selSetFrame = Sel("setFrame:")

	flipped := Method("isFlipped", func(self ID, _ SEL) bool { return true })
	firstMouse := Method("acceptsFirstMouse:", func(self ID, _ SEL, ev ID) bool { return true })

	// A container: Asuka places its subviews itself.
	DefineClass("AsukaView", "NSView", []string{"NSDraggingDestination", "NSDraggingSource"}, append([]objc.MethodDef{
		flipped, firstMouse,
		Method("accessibilityPerformPress", func(self ID, _ SEL) bool {
			n := theHost.byView[self]
			if n == nil || !n.listening(ops.EventPress) {
				return false
			}
			theHost.emit(n, ops.Event{Kind: ops.EventPress})
			return true
		}),
		// A pressable view is read as its contents' texts, as VoiceOver
		// reads a row on iOS.
		Method("accessibilityLabel", func(self ID, _ SEL) ID {
			n := theHost.byView[self]
			if n == nil {
				return 0
			}
			if n.a11yLabel != "" {
				return String(n.a11yLabel)
			}
			if !n.listening(ops.EventPress) {
				return 0
			}
			var parts []string
			collectLabels(self, &parts)
			if len(parts) == 0 {
				return 0
			}
			return String(strings.Join(parts, ", "))
		}),
		// OnAdjust: VoiceOver's increment and decrement.
		Method("accessibilityPerformIncrement", func(self ID, _ SEL) bool { return theHost.adjust(self, 1) }),
		Method("accessibilityPerformDecrement", func(self ID, _ SEL) bool { return theHost.adjust(self, -1) }),
		Method("menuForEvent:", func(self ID, _ SEL, ev ID) ID {
			n := theHost.byView[self]
			if n != nil && n.cell != nil && n.menu == 0 {
				return theHost.cellMenu(n.cell) // a row's swipe actions
			}
			if n == nil || n.menu == 0 || n.menuTrigger != int32(ops.MenuTriggerLongPress) {
				return 0
			}
			return n.menu
		}),
		// A trackpad's swipes: three fingers (swipeWithEvent:, when the
		// system leaves them to apps), or a two-finger flick (swipe.go).
		Method("swipeWithEvent:", func(self ID, _ SEL, ev ID) {
			if !theHost.swipeEvent(self, ev) {
				SendSuper(self, "AsukaView", "swipeWithEvent:", uintptr(ev))
			}
		}),
		Method("scrollWheel:", func(self ID, _ SEL, ev ID) {
			if !theHost.wheel(self, ev) {
				SendSuper(self, "AsukaView", "scrollWheel:", uintptr(ev))
			}
		}),
		Method("cancelOperation:", func(self ID, _ SEL, sender ID) {
			// Escape in a sheet's content dismisses it.
			theHost.cancel(self)
		}),
	}, interactionMethods()...))
	// Text fields that report focus.
	focus := func(cls string) objc.MethodDef {
		return Method("becomeFirstResponder", func(self ID, _ SEL) bool {
			ok := byte(SendSuper(self, cls, "becomeFirstResponder")) != 0
			if n := theHost.byView[self]; ok && n != nil {
				theHost.emit(n, ops.Event{Kind: ops.EventFocus})
				theHost.applyPendingSelection(n)
			}
			return ok
		})
	}
	DefineClass("AsukaTextField", "NSTextField", nil, []objc.MethodDef{focus("AsukaTextField"), keepsShadow("AsukaTextField")})
	// A one-line field's cell: its text, placeholder and field editor
	// inside the field's padding (ContentInset), as AppKit lays them out
	// in what this returns.
	DefineClass("AsukaTextFieldCell", "NSTextFieldCell", nil, []objc.MethodDef{
		Method("drawingRectForBounds:", func(self ID, _ SEL, b Rect) Rect {
			r := SendSuperRect(self, "AsukaTextFieldCell", "drawingRectForBounds:", b)
			if n := theHost.byView[Send(self, "controlView")]; n != nil {
				in := n.inset
				r.Origin.X += float64(in[3])
				r.Origin.Y += float64(in[0])
				r.Size.Width = max(r.Size.Width-float64(in[1]+in[3]), 0)
				r.Size.Height = max(r.Size.Height-float64(in[0]+in[2]), 0)
			}
			return r
		}),
	})
	DefineClass("AsukaFieldEditor", "NSTextView", nil, fieldEditorMethods())
	DefineClass("AsukaSecureField", "NSSecureTextField", nil, []objc.MethodDef{focus("AsukaSecureField"), keepsShadow("AsukaSecureField")})
	DefineClass("AsukaLabel", "NSTextField", nil, []objc.MethodDef{
		keepsShadow("AsukaLabel"),
		// Clicks go to the view under the label (a pressable row).
		Method("hitTest:", func(self ID, _ SEL, p Point) ID { return 0 }),
		// Its text inside its padding (ContentInset), as NSTextField draws
		// its cell, in its bounds less the inset.
		Method("drawRect:", func(self ID, _ SEL, dirty Rect) {
			b := MsgRect(self, Sel("bounds"))
			if n := theHost.byView[self]; n != nil {
				in := n.inset
				b.Origin.X += float64(in[3])
				b.Origin.Y += float64(in[0])
				if !SendBool(self, "isFlipped") {
					b.Origin.Y = b.Origin.Y - float64(in[0]) + float64(in[2])
				}
				b.Size.Width = max(b.Size.Width-float64(in[1]+in[3]), 0)
				b.Size.Height = max(b.Size.Height-float64(in[0]+in[2]), 0)
			}
			MsgRectIDVoid(Send(self, "cell"), Sel("drawWithFrame:inView:"), b, self)
		}),
	})
	DefineClass("AsukaImageView", "NSImageView", nil, []objc.MethodDef{
		keepsShadow("AsukaImageView"),
		Method("hitTest:", func(self ID, _ SEL, p Point) ID { return 0 }),
	})
	registerRichClass(flipped)
	registerShapeClass(flipped)
	registerPageDotsClass(flipped)
	registerCanvasClass(flipped)
	registerSplitClasses()
	registerTarget()
}

// targetMethods are more of AsukaTarget's methods (the application
// delegate's), each file adding its own (from its init).
var targetMethods []objc.MethodDef

// registerTarget makes AsukaTarget, the one object that takes the views'
// actions, delegate calls and notifications.
func registerTarget() {
	h := func() *Host { return theHost }
	methods := []objc.MethodDef{
		// The application.
		Method("applicationShouldTerminateAfterLastWindowClosed:", func(self ID, _ SEL, app ID) bool { return true }),
		Method("applicationWillTerminate:", func(self ID, _ SEL, n ID) {
			h().lifecycle(ops.LifecycleBackground)
			saveState()
		}),
		Method("applicationDidHide:", func(self ID, _ SEL, n ID) {
			h().lifecycle(ops.LifecycleBackground)
			h().windowsChanged()
		}),
		Method("applicationDidUnhide:", func(self ID, _ SEL, n ID) {
			h().lifecycle(ops.LifecycleForeground)
			h().windowsChanged()
		}),
		Method("applicationDidResignActive:", func(self ID, _ SEL, n ID) {
			h().appActive(false)
			h().windowsChanged()
		}),
		Method("applicationDidBecomeActive:", func(self ID, _ SEL, n ID) {
			h().appActive(true)
			h().windowsChanged()
		}),
		Method("application:openURLs:", func(self ID, _ SEL, app, urls ID) {
			for _, u := range ArrayItems(urls) {
				h().openURL(GoString(Send(u, "absoluteString")))
			}
		}),
		Method("applicationSupportsSecureRestorableState:", func(self ID, _ SEL, app ID) bool { return true }),

		// The window.
		Method("windowDidResize:", func(self ID, _ SEL, n ID) {
			h().environmentChanged()
			h().windowsChanged()
		}),
		Method("windowDidChangeBackingProperties:", func(self ID, _ SEL, n ID) { h().environmentChanged() }),
		Method("windowDidMiniaturize:", func(self ID, _ SEL, n ID) {
			h().lifecycle(ops.LifecycleBackground)
			h().windowsChanged()
		}),
		Method("windowDidDeminiaturize:", func(self ID, _ SEL, n ID) {
			h().lifecycle(ops.LifecycleForeground)
			h().windowsChanged()
		}),
		Method("observeValueForKeyPath:ofObject:change:context:", func(self ID, _ SEL, keyPath, object, change ID, ctx uintptr) {
			switch GoString(keyPath) {
			case "presentationSize":
				h().videoSized(object)
				return
			case "URL", "title", "loading", "estimatedProgress", "canGoBack", "canGoForward":
				h().queueWebState(object)
				return
			}
			h().appearanceChanged()
		}),
		Method("videoEnded:", func(self ID, _ SEL, n ID) { h().videoEnded(Send(n, "object")) }),
		Method("videoFailed:", func(self ID, _ SEL, n ID) { h().videoFailed(Send(n, "object")) }),
		Method("contentResized:", func(self ID, _ SEL, n ID) { h().environmentChanged() }),
		Method("systemChanged:", func(self ID, _ SEL, n ID) {
			h().refreshLocale()
			h().environmentChanged()
		}),

		// Controls.
		Method("act:", func(self ID, _ SEL, sender ID) { h().act(sender) }),
		Method("longPress:", func(self ID, _ SEL, g ID) { h().gesture(g, ops.EventLongPress) }),
		Method("pan:", func(self ID, _ SEL, g ID) { h().gesture(g, ops.EventPan) }),
		// A pan doesn't begin while the mouse went down on a draggable
		// view: moving it drags the view.
		Method("gestureRecognizerShouldBegin:", func(self ID, _ SEL, g ID) bool {
			return h().mouse.source == nil && h().drag == nil
		}),
		Method("magnify:", func(self ID, _ SEL, g ID) { h().gesture(g, ops.EventPinch) }),
		Method("rotate:", func(self ID, _ SEL, g ID) { h().gesture(g, ops.EventRotate) }),
		Method("menuItem:", func(self ID, _ SEL, item ID) { h().menuItem(item) }),
		Method("a11yAction:", func(self ID, _ SEL, a ID) bool {
			x, ok := h().a11yActions[a]
			if ok {
				h().emit(x.n, ops.Event{Kind: ops.EventA11yAction, Int: int64(x.index)})
			}
			return ok
		}),
		Method("barItem:", func(self ID, _ SEL, item ID) { h().barItem(item) }),
		Method("back:", func(self ID, _ SEL, sender ID) { h().back(h().senderWin(sender)) }),
		Method("appMenuItem:", func(self ID, _ SEL, item ID) { h().appMenuItem(item) }),
		Method("validateMenuItem:", func(self ID, _ SEL, item ID) bool { return h().validateMenuItem(item) }),
		Method("boundsChanged:", func(self ID, _ SEL, n ID) { h().scrolled(Send(n, "object")) }),
		Method("refreshScrolled:", func(self ID, _ SEL, n ID) { h().refreshScrolled(Send(n, "object")) }),
		Method("refreshScrollEnded:", func(self ID, _ SEL, n ID) { h().refreshScrollEnded(Send(n, "object")) }),
		Method("liveScrollEnded:", func(self ID, _ SEL, n ID) { h().settlePage(Send(n, "object")) }),
		Method("shaderTick:", func(self ID, _ SEL, t ID) { h().shaderTick() }),
		Method("clipboardTick:", func(self ID, _ SEL, t ID) { h().clipboardTick() }),
		Method("locationManagerDidChangeAuthorization:", func(self ID, _ SEL, m ID) { h().locationAuthorized() }),
		Method("locationManager:didUpdateLocations:", func(self ID, _ SEL, m, ls ID) { h().locationsUpdated(ls) }),
		Method("locationManager:didFailWithError:", func(self ID, _ SEL, m, err ID) { h().locationFailed(err) }),
		Method("audioEnded:", func(self ID, _ SEL, n ID) { h().audioEnded(Send(n, "object")) }),
		Method("speechSynthesizer:didFinishSpeechUtterance:", func(self ID, _ SEL, s, u ID) { h().spoken(u, true) }),
		Method("speechSynthesizer:didCancelSpeechUtterance:", func(self ID, _ SEL, s, u ID) { h().spoken(u, false) }),
		Method("popoverShouldClose:", func(self ID, _ SEL, p ID) bool { return h().popoverShouldClose(p) }),
		Method("popoverDidClose:", func(self ID, _ SEL, n ID) { h().popoverClosed(Send(n, "object")) }),
		Method("frameTick:", func(self ID, _ SEL, t ID) { h().tick() }),

		// Text fields.
		Method("controlTextDidChange:", func(self ID, _ SEL, n ID) { h().textChanged(Send(n, "object")) }),
		Method("controlTextDidEndEditing:", func(self ID, _ SEL, n ID) {
			if v := h().byView[Send(n, "object")]; v != nil {
				h().emit(v, ops.Event{Kind: ops.EventBlur})
			}
		}),
		Method("control:textView:doCommandBySelector:", func(self ID, _ SEL, control, tv ID, cmd SEL) bool {
			return h().textCommand(control, tv, cmd)
		}),
		Method("textDidChange:", func(self ID, _ SEL, n ID) { h().textViewChanged(Send(n, "object")) }),
		Method("textSelectionChanged:", func(self ID, _ SEL, n ID) { h().selectionChanged(Send(n, "object")) }),
		Method("windowWillReturnFieldEditor:toObject:", func(self ID, _ SEL, w, obj ID) ID { return h().fieldEditor(w, obj) }),
		Method("textDidBeginEditing:", func(self ID, _ SEL, n ID) {
			if v := h().textViewNode(Send(n, "object")); v != nil {
				h().emit(v, ops.Event{Kind: ops.EventFocus})
			}
		}),
		Method("textDidEndEditing:", func(self ID, _ SEL, n ID) {
			if v := h().textViewNode(Send(n, "object")); v != nil {
				h().emit(v, ops.Event{Kind: ops.EventBlur})
			}
		}),
		Method("textView:clickedOnLink:atIndex:", func(self ID, _ SEL, tv, link ID, i uint) bool {
			return h().linkClicked(tv, link)
		}),

		// Tabs.
		Method("tabView:didSelectTabViewItem:", func(self ID, _ SEL, tv, item ID) { h().tabSelected(tv, item) }),
	}
	methods = append(methods, webMethods()...)
	methods = append(methods, navMethods()...)
	methods = append(methods, previewMethods()...)
	methods = append(methods, targetMethods...)
	DefineClass("AsukaTarget", "NSObject", []string{
		"NSApplicationDelegate", "NSWindowDelegate", "NSTextFieldDelegate", "NSTextViewDelegate",
		"NSTabViewDelegate", "NSMenuDelegate", "NSGestureRecognizerDelegate",
		"WKNavigationDelegate", "WKScriptMessageHandler", "WKUIDelegate", "NSToolbarDelegate", "NSSearchFieldDelegate",
	}, methods)
}

// collectLabels gathers the texts under a view, in order.
func collectLabels(v ID, out *[]string) {
	for _, s := range ArrayItems(Send(v, "subviews")) {
		if SendBool(s, "isHidden") {
			continue
		}
		if n := theHost.byView[s]; n != nil {
			switch {
			case n.a11yLabel != "":
				*out = append(*out, n.a11yLabel)
				continue
			case n.kind == ops.KindText && n.text != "":
				*out = append(*out, n.text)
				continue
			}
		}
		collectLabels(s, out)
	}
}

func (h *Host) adjust(v ID, by int64) bool {
	n := h.byView[v]
	if n == nil || !n.adjustable {
		return false
	}
	h.emit(n, ops.Event{Kind: ops.EventAdjust, Int: by})
	return true
}
