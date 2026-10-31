//go:build darwin && !ios

package darwin

import (
	"strings"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/nui/internal/darwin"
	"github.com/egoist/nui/ops"
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

	// A container: nui places its subviews itself.
	DefineClass("NuiView", "NSView", nil, []objc.MethodDef{
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
		Method("menuForEvent:", func(self ID, _ SEL, ev ID) ID {
			n := theHost.byView[self]
			if n == nil || n.menu == 0 || n.menuTrigger != int32(ops.MenuTriggerLongPress) {
				return 0
			}
			return n.menu
		}),
		Method("cancelOperation:", func(self ID, _ SEL, sender ID) {
			// Escape in a sheet's content dismisses it.
			theHost.cancel(self)
		}),
	})
	// Text fields that report focus.
	focus := func(cls string) objc.MethodDef {
		return Method("becomeFirstResponder", func(self ID, _ SEL) bool {
			ok := byte(SendSuper(self, cls, "becomeFirstResponder")) != 0
			if n := theHost.byView[self]; ok && n != nil {
				theHost.emit(n, ops.Event{Kind: ops.EventFocus})
			}
			return ok
		})
	}
	DefineClass("NuiTextField", "NSTextField", nil, []objc.MethodDef{focus("NuiTextField")})
	DefineClass("NuiSecureField", "NSSecureTextField", nil, []objc.MethodDef{focus("NuiSecureField")})
	DefineClass("NuiLabel", "NSTextField", nil, []objc.MethodDef{
		// Clicks go to the view under the label (a pressable row).
		Method("hitTest:", func(self ID, _ SEL, p Point) ID { return 0 }),
	})
	DefineClass("NuiImageView", "NSImageView", nil, []objc.MethodDef{
		Method("hitTest:", func(self ID, _ SEL, p Point) ID { return 0 }),
	})
	registerRichClass(flipped)
	registerShapeClass(flipped)
	registerTarget()
}

// registerTarget makes NuiTarget, the one object that takes the views'
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
		Method("applicationDidHide:", func(self ID, _ SEL, n ID) { h().lifecycle(ops.LifecycleBackground) }),
		Method("applicationDidUnhide:", func(self ID, _ SEL, n ID) { h().lifecycle(ops.LifecycleForeground) }),
		Method("application:openURLs:", func(self ID, _ SEL, app, urls ID) {
			for _, u := range ArrayItems(urls) {
				h().openURL(GoString(Send(u, "absoluteString")))
			}
		}),
		Method("applicationSupportsSecureRestorableState:", func(self ID, _ SEL, app ID) bool { return true }),

		// The window.
		Method("windowDidResize:", func(self ID, _ SEL, n ID) { h().environmentChanged() }),
		Method("windowDidChangeBackingProperties:", func(self ID, _ SEL, n ID) { h().environmentChanged() }),
		Method("windowDidMiniaturize:", func(self ID, _ SEL, n ID) { h().lifecycle(ops.LifecycleBackground) }),
		Method("windowDidDeminiaturize:", func(self ID, _ SEL, n ID) { h().lifecycle(ops.LifecycleForeground) }),
		Method("observeValueForKeyPath:ofObject:change:context:", func(self ID, _ SEL, keyPath, object, change ID, ctx uintptr) {
			if GoString(keyPath) == "presentationSize" {
				h().videoSized(object)
				return
			}
			h().appearanceChanged()
		}),
		Method("videoEnded:", func(self ID, _ SEL, n ID) { h().videoEnded(Send(n, "object")) }),
		Method("videoFailed:", func(self ID, _ SEL, n ID) { h().videoFailed(Send(n, "object")) }),
		Method("contentResized:", func(self ID, _ SEL, n ID) { h().environmentChanged() }),
		Method("systemChanged:", func(self ID, _ SEL, n ID) { h().environmentChanged() }),

		// Controls.
		Method("act:", func(self ID, _ SEL, sender ID) { h().act(sender) }),
		Method("click:", func(self ID, _ SEL, g ID) { h().gesture(g, ops.EventPress) }),
		Method("longPress:", func(self ID, _ SEL, g ID) { h().gesture(g, ops.EventLongPress) }),
		Method("pan:", func(self ID, _ SEL, g ID) { h().gesture(g, ops.EventPan) }),
		Method("magnify:", func(self ID, _ SEL, g ID) { h().gesture(g, ops.EventPinch) }),
		Method("rotate:", func(self ID, _ SEL, g ID) { h().gesture(g, ops.EventRotate) }),
		Method("menuItem:", func(self ID, _ SEL, item ID) { h().menuItem(item) }),
		Method("barItem:", func(self ID, _ SEL, item ID) { h().barItem(item) }),
		Method("back:", func(self ID, _ SEL, sender ID) { h().back() }),
		Method("boundsChanged:", func(self ID, _ SEL, n ID) { h().scrolled(Send(n, "object")) }),
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
	DefineClass("NuiTarget", "NSObject", []string{
		"NSApplicationDelegate", "NSWindowDelegate", "NSTextFieldDelegate", "NSTextViewDelegate",
		"NSTabViewDelegate", "NSMenuDelegate",
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
