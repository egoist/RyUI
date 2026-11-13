//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
)

// More of the system's modules on a Mac: asuka/sdk/background (an activity
// that keeps App Nap off), asuka/sdk/sensors (a Mac has none of the motion
// sensors: none available), asuka/sdk/widget, asuka/sdk/splash, asuka/sdk/screen
// (capture kept out of screenshots and recordings; screenshots seen by
// Spotlight), asuka/sdk/tracking (App Tracking Transparency), asuka/sdk/integrity
// (App Attest) and asuka/sdk/agerange.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["background"] = h.backgroundModule()
		m["sensors"] = sensorsModule()
		m["widget"] = widgetModule()
		m["splash"] = splashModule()
		m["screen"] = h.screenModule()
		m["tracking"] = trackingModule()
		m["integrity"] = h.integrityModule()
		m["ageRange"] = ageRangeModule()
	})
}

// answerNil answers at once, with nothing.
func answerNil(_ []byte, reply func([]byte, error)) { reply(nil, nil) }

// Background.

// bgTasks are asuka/sdk/background's tasks: a Mac app keeps running out of
// sight, so a task holds an activity (NSProcessInfo's) that keeps App Nap
// from slowing it and the system from ending it to free memory, until
// the last one ends. There's no notification to show (iOS shows none
// either): "update" does nothing, and "next" (actions tapped) waits
// until the last task ends, then fails "ended".
var bgTasks struct {
	ids      map[uint32]bool
	activity ID
	next     []func([]byte, error)
}

func (h *Host) backgroundModule() map[string]method {
	id := func(args []byte) uint32 {
		var o struct {
			ID uint32 `json:"id"`
		}
		json.Unmarshal(args, &o)
		return o.ID
	}
	return map[string]method{
		"start": func(args []byte, reply func([]byte, error)) {
			if bgTasks.ids == nil {
				bgTasks.ids = map[uint32]bool{}
			}
			bgTasks.ids[id(args)] = true
			if bgTasks.activity == 0 {
				const userInitiatedAllowingIdleSystemSleep = 0x00FFFFFF &^ (1 << 20)
				const suddenTerminationDisabled = 1 << 14
				const automaticTerminationDisabled = 1 << 15
				pi := Send(Class("NSProcessInfo"), "processInfo")
				bgTasks.activity = Retain(Send(pi, "beginActivityWithOptions:reason:",
					userInitiatedAllowingIdleSystemSleep|suddenTerminationDisabled|automaticTerminationDisabled,
					uintptr(String("asuka/sdk/background: a task"))))
			}
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
	if bgTasks.activity != 0 {
		Send(Send(Class("NSProcessInfo"), "processInfo"), "endActivity:", uintptr(bgTasks.activity))
		Release(bgTasks.activity)
		bgTasks.activity = 0
	}
	ws := bgTasks.next
	bgTasks.next = nil
	for _, w := range ws {
		w(nil, errors.New("ended"))
	}
}

// Sensors.

// sensorsModule is asuka/sdk/sensors': a Mac has no accelerometer, gyroscope,
// magnetometer, barometer or pedometer an app can read (Core Motion's
// managers aren't on macOS), and its ambient light sensor has no public
// API: none is available, starting one fails "not available", and so do
// "next" (which ends the Go side's watching) and "steps".
func sensorsModule() map[string]method {
	na := func(_ []byte, reply func([]byte, error)) { reply(nil, errors.New("not available")) }
	return map[string]method{
		"available": func(_ []byte, reply func([]byte, error)) { reply([]byte{0}, nil) },
		"start":     na,
		"stop":      answerNil,
		"stopAll":   answerNil,
		"next":      na,
		"steps":     na,
		"simulate":  na,
	}
}

// Widget.

// widgetModule is asuka/sdk/widget's: a Mac app from asuka build has no widget
// extension (WidgetKit's widgets are an extension the iOS build makes),
// so there are none to give entries to.
func widgetModule() map[string]method {
	return map[string]method{
		"set": func(_ []byte, reply func([]byte, error)) {
			reply(nil, errors.New("not available: widgets are iOS's and Android's"))
		},
	}
}

// Splash.

// splashModule is asuka/sdk/splash's: a Mac app has no launch screen (its
// window shows once the app's first frame is laid out), so holding it and
// hiding it do nothing.
func splashModule() map[string]method {
	return map[string]method{"hold": answerNil, "hide": answerNil}
}

// Screen.

// screenState is asuka/sdk/screen's on a Mac. Locking the orientation and the
// bars' style are a phone's: a Mac's display doesn't turn with the
// window, and it has no status or navigation bar. PreventCapture keeps
// the app's windows out of screenshots and screen recordings
// (NSWindowSharingNone), those it opens later too; screenshots are
// seen as Spotlight finds the files they make (kMDItemIsScreenCapture),
// as macOS has no notification for them.
var screenState struct {
	preventing  bool
	observer    ID // the windows' NSWindowDidUpdate... observer
	query       ID // NSMetadataQuery of screen captures
	queryToken  ID // its updates' observer
	screenshots []func([]byte, error)
	blocks      []objc.Block
}

func (h *Host) screenModule() map[string]method {
	s := &screenState
	return map[string]method{
		"lock": answerNil,
		"bars": answerNil,
		"preventCapture": func(args []byte, reply func([]byte, error)) {
			s.preventing = len(args) > 0 && args[0] == 1
			h.applySharing()
			if s.observer == 0 && s.preventing {
				// Windows that open later (a sheet, a ui.Window) too.
				b := NewBlock(func(_ objc.Block, note ID) {
					if s.preventing {
						setSharing(Send(note, "object"), false)
					}
				})
				s.blocks = append(s.blocks, b)
				nc := Send(Class("NSNotificationCenter"), "defaultCenter")
				s.observer = Retain(Send(nc, "addObserverForName:object:queue:usingBlock:",
					uintptr(String("NSWindowDidBecomeKeyNotification")), 0, uintptr(Send(Class("NSOperationQueue"), "mainQueue")), uintptr(b)))
			}
			reply(nil, nil)
		},
		"screenshot": func(_ []byte, reply func([]byte, error)) {
			if err := watchScreenshots(); err != nil {
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
			if s.query != 0 {
				Send(s.query, "stopQuery")
				Send(Send(Class("NSNotificationCenter"), "defaultCenter"), "removeObserver:", uintptr(s.queryToken))
				Release(s.queryToken)
				Release(s.query)
				s.query, s.queryToken = 0, 0
			}
			reply(nil, nil)
		},
	}
}

// applySharing sets every window's sharing type as the app prevents
// capture or not.
func (h *Host) applySharing() {
	app := Send(Class("NSApplication"), "sharedApplication")
	for _, w := range ArrayItems(Send(app, "windows")) {
		setSharing(w, !screenState.preventing)
	}
}

func setSharing(w ID, shared bool) {
	if w == 0 || !RespondsTo(w, "setSharingType:") {
		return
	}
	t := uintptr(0) // NSWindowSharingNone
	if shared {
		t = 1 // NSWindowSharingReadOnly, the default
	}
	Send(w, "setSharingType:", t)
}

// watchScreenshots starts a Spotlight query for screen captures; its
// updates (new files, not those it finds at first) are screenshots.
func watchScreenshots() error {
	s := &screenState
	if s.query != 0 {
		return nil
	}
	q := New("NSMetadataQuery")
	pred := Send(Class("NSPredicate"), "predicateWithFormat:", uintptr(String("kMDItemIsScreenCapture = 1")))
	if pred == 0 {
		Release(q)
		return errors.New("not available")
	}
	Send(q, "setPredicate:", uintptr(pred))
	Send(q, "setSearchScopes:", uintptr(Array(String("NSMetadataQueryUserHomeScope"))))
	b := NewBlock(func(_ objc.Block, note ID) {
		info := Send(note, "userInfo")
		added := Send(info, "objectForKey:", uintptr(String("kMDQueryUpdateAddedItems")))
		if SendInt(added, "count") == 0 {
			return
		}
		ws := s.screenshots
		s.screenshots = nil
		for _, w := range ws {
			w(nil, nil)
		}
	})
	s.blocks = append(s.blocks, b)
	nc := Send(Class("NSNotificationCenter"), "defaultCenter")
	token := Retain(Send(nc, "addObserverForName:object:queue:usingBlock:", uintptr(String("NSMetadataQueryDidUpdateNotification")),
		uintptr(q), uintptr(Send(Class("NSOperationQueue"), "mainQueue")), uintptr(b)))
	if !SendBool(q, "startQuery") {
		Send(nc, "removeObserver:", uintptr(token))
		Release(token)
		Release(q)
		return errors.New("not available")
	}
	s.query, s.queryToken = q, token
	return nil
}

// Tracking.

// trackingModule is asuka/sdk/tracking's: App Tracking Transparency, as on
// iOS (macOS 11). The reply: u8 not asked, restricted, denied, allowed.
func trackingModule() map[string]method {
	load := func() bool {
		return framework("AppTrackingTransparency") != 0 && HasClass("ATTrackingManager")
	}
	code := func(s int) byte {
		switch s {
		case 1, 2, 3:
			return byte(s)
		}
		return 0
	}
	return map[string]method{
		"status": func(_ []byte, reply func([]byte, error)) {
			if !load() {
				reply([]byte{1}, nil) // restricted: the system can't ask
				return
			}
			reply([]byte{code(SendInt(Class("ATTrackingManager"), "trackingAuthorizationStatus"))}, nil)
		},
		"request": func(_ []byte, reply func([]byte, error)) {
			if !load() {
				reply([]byte{1}, nil)
				return
			}
			var done objc.Block
			done = NewBlock(func(_ objc.Block, status uint) {
				theHost.Post(func() {
					done.Release()
					reply([]byte{code(int(status))}, nil)
				})
			})
			Send(Class("ATTrackingManager"), "requestTrackingAuthorizationWithCompletionHandler:", uintptr(done))
		},
	}
}

// Integrity.

// integrityModule is asuka/sdk/integrity's: App Attest (DeviceCheck's
// DCAppAttestService, macOS 11), as on iOS: "supported" (u8),
// "generateKey" (the key's ID), "attestKey" and "assert" (string key ID,
// then the 32-byte client data hash). The app needs the App Attest
// entitlement, which only a provisioning profile grants; without it the
// service says it isn't supported. Play Integrity is Android's.
func (h *Host) integrityModule() map[string]method {
	service := func() ID {
		if framework("DeviceCheck") == 0 || !HasClass("DCAppAttestService") {
			return 0
		}
		return Send(Class("DCAppAttestService"), "sharedService")
	}
	supported := func() bool {
		s := service()
		return s != 0 && SendBool(s, "isSupported")
	}
	keyed := func(args []byte) (string, []byte, bool) {
		if len(args) < 36 {
			return "", nil, false
		}
		id, rest := readString(args)
		if len(rest) < 32 {
			return "", nil, false
		}
		return id, rest[len(rest)-32:], true
	}
	// A completion handler of (NSData or NSString, NSError).
	answer := func(reply func([]byte, error), what string, str bool) objc.Block {
		var b objc.Block
		b = NewBlock(func(_ objc.Block, v, err ID) {
			var out []byte
			if str {
				if v != 0 {
					out = []byte(GoString(v))
				}
			} else if v != 0 {
				out = Bytes(v)
			}
			e := error(nil)
			if v == 0 {
				e = errorText(err, what)
			}
			h.Post(func() {
				b.Release()
				reply(out, e)
			})
		})
		return b
	}
	notHere := errors.New("App Attest isn't on this Mac")
	return map[string]method{
		"supported": func(_ []byte, reply func([]byte, error)) { reply([]byte{b2u(supported())}, nil) },
		"generateKey": func(_ []byte, reply func([]byte, error)) {
			if !supported() {
				reply(nil, notHere)
				return
			}
			Send(service(), "generateKeyWithCompletionHandler:", uintptr(answer(reply, "no key", true)))
		},
		"attestKey": func(args []byte, reply func([]byte, error)) {
			id, hash, good := keyed(args)
			if !good {
				reply(nil, errors.New("bad args"))
				return
			}
			if !supported() {
				reply(nil, notHere)
				return
			}
			Send(service(), "attestKey:clientDataHash:completionHandler:", uintptr(String(id)), uintptr(Data(hash)), uintptr(answer(reply, "no attestation", false)))
		},
		"assert": func(args []byte, reply func([]byte, error)) {
			id, hash, good := keyed(args)
			if !good {
				reply(nil, errors.New("bad args"))
				return
			}
			if !supported() {
				reply(nil, notHere)
				return
			}
			Send(service(), "generateAssertion:clientDataHash:completionHandler:", uintptr(String(id)), uintptr(Data(hash)), uintptr(answer(reply, "no assertion", false)))
		},
		"prepare": func(_ []byte, reply func([]byte, error)) {
			reply(nil, errors.New("Play Integrity is Android's: use App Attest on macOS"))
		},
		"token": func(_ []byte, reply func([]byte, error)) {
			reply(nil, errors.New("Play Integrity is Android's: use App Attest on macOS"))
		},
	}
}

// Age range.

// ageRangeModule is asuka/sdk/agerange's: the Declared Age Range service is a
// Swift API (AgeRangeService, async), which an app driven through the
// Objective-C runtime can't call: not available, as before iOS 26.
func ageRangeModule() map[string]method {
	return map[string]method{
		"request": func(_ []byte, reply func([]byte, error)) { reply(nil, errors.New("not available")) },
	}
}
