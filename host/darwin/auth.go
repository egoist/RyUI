//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/native"
)

// Signing in on a Mac, with AuthenticationServices as on iOS: asuka/
// appleauth (ASAuthorizationController, and its button,
// ASAuthorizationAppleIDButton) and asuka/webauth
// (ASWebAuthenticationSession). Sign in with Apple needs the app signed
// with its entitlement (com.apple.developer.applesignin), which only a
// provisioning profile grants: without it, the controller fails.

var authState struct {
	delegate ID
	pending  map[ID]func([]byte, error) // by ASAuthorizationController
	sessions map[uint32]*webAuthSession
	buttons  map[ID]*appleSignInButton // by their box, and by their button
}

type webAuthSession struct {
	session ID
	reply   func([]byte, error)
	block   objc.Block
}

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["appleAuth"] = h.appleAuthModule()
		m["webAuth"] = h.webAuthModule()
	})
	builtinViews["asuka.appleSignIn"] = newAppleSignInButton
}

func loadAuthServices() bool {
	return framework("AuthenticationServices") != 0 && HasClass("ASAuthorizationController")
}

// authDelegate is the controllers' and sessions' delegate and
// presentation context: the key window.
func authDelegate() ID {
	if authState.delegate != 0 {
		return authState.delegate
	}
	anchor := func(self ID, _ SEL, _ ID) ID { return theHost.keyWin().window }
	cls := DefineClass("AsukaAuthDelegate", "NSObject", []string{
		"ASAuthorizationControllerDelegate", "ASAuthorizationControllerPresentationContextProviding",
		"ASWebAuthenticationPresentationContextProviding",
	}, []objc.MethodDef{
		Method("presentationAnchorForAuthorizationController:", anchor),
		Method("presentationAnchorForWebAuthenticationSession:", anchor),
		Method("authorizationController:didCompleteWithAuthorization:", func(self ID, _ SEL, c, auth ID) {
			appleAuthDone(c, Send(auth, "credential"), 0)
		}),
		Method("authorizationController:didCompleteWithError:", func(self ID, _ SEL, c, err ID) {
			appleAuthDone(c, 0, err)
		}),
	})
	authState.delegate = Send(cls, "new")
	return authState.delegate
}

// appleAuthModule is asuka/appleauth's: "available"; "signIn" (JSON:
// fullName, email, nonce, state) shows Sign in with Apple and answers the
// credential as JSON, or fails with "canceled"; "state" (the user ID)
// answers u8: revoked, authorized, not found, transferred.
func (h *Host) appleAuthModule() map[string]method {
	return map[string]method{
		"available": func(_ []byte, reply func([]byte, error)) {
			reply([]byte{b2u(loadAuthServices())}, nil)
		},
		"signIn": func(args []byte, reply func([]byte, error)) {
			if !loadAuthServices() {
				reply(nil, errors.New("not available"))
				return
			}
			var o struct {
				FullName, Email bool
				Nonce, State    string
			}
			json.Unmarshal(args, &o)
			req := Send(Autorelease(New("ASAuthorizationAppleIDProvider")), "createRequest")
			lib := framework("AuthenticationServices")
			var scopes []ID
			if o.FullName {
				scopes = append(scopes, Const(lib, "ASAuthorizationScopeFullName"))
			}
			if o.Email {
				scopes = append(scopes, Const(lib, "ASAuthorizationScopeEmail"))
			}
			Send(req, "setRequestedScopes:", uintptr(Array(scopes...)))
			if o.Nonce != "" {
				Send(req, "setNonce:", uintptr(String(o.Nonce)))
			}
			if o.State != "" {
				Send(req, "setState:", uintptr(String(o.State)))
			}
			c := Send(Send(Class("ASAuthorizationController"), "alloc"), "initWithAuthorizationRequests:", uintptr(Array(req)))
			d := authDelegate()
			Send(c, "setDelegate:", uintptr(d))
			Send(c, "setPresentationContextProvider:", uintptr(d))
			if authState.pending == nil {
				authState.pending = map[ID]func([]byte, error){}
			}
			authState.pending[c] = reply
			Send(c, "performRequests")
		},
		"state": func(args []byte, reply func([]byte, error)) {
			if !loadAuthServices() {
				reply(nil, errors.New("not available"))
				return
			}
			p := New("ASAuthorizationAppleIDProvider")
			var done objc.Block
			done = NewBlock(func(_ objc.Block, state int, err ID) {
				var e error
				if err != 0 && state != 2 {
					e = errorText(err, "no state")
				}
				h.Post(func() {
					done.Release()
					Release(p)
					if e != nil {
						reply(nil, e)
						return
					}
					reply([]byte{appleCredentialState(state)}, nil)
				})
			})
			Send(p, "getCredentialStateForUserID:completion:", uintptr(String(string(args))), uintptr(done))
		},
	}
}

// appleCredentialState is ASAuthorizationAppleIDProviderCredentialState
// as appleauth.State: the same numbers, any other one not found.
func appleCredentialState(s int) byte {
	if s < 0 || s > 3 {
		return 2
	}
	return byte(s)
}

// appleAuthDone answers a controller's sign-in.
func appleAuthDone(c, cred, err ID) {
	reply := authState.pending[c]
	if reply == nil {
		return
	}
	delete(authState.pending, c)
	defer Release(c)
	switch {
	case err != 0:
		const canceled = 1001 // ASAuthorizationErrorCanceled
		if SendInt(err, "code") == canceled {
			reply(nil, errors.New("canceled"))
			return
		}
		reply(nil, errorText(err, "failed"))
	case cred == 0 || !SendBool(cred, "isKindOfClass:", uintptr(Class("ASAuthorizationAppleIDCredential"))):
		reply(nil, errors.New("not an Apple ID credential"))
	default:
		name := Send(cred, "fullName")
		var given, family string
		if name != 0 {
			given, family = GoString(Send(name, "givenName")), GoString(Send(name, "familyName"))
		}
		b, _ := json.Marshal(appleCredential{
			User: GoString(Send(cred, "user")), IdentityToken: string(Bytes(Send(cred, "identityToken"))),
			AuthorizationCode: string(Bytes(Send(cred, "authorizationCode"))), State: GoString(Send(cred, "state")),
			Email: GoString(Send(cred, "email")), GivenName: given, FamilyName: family,
			RealUser: SendInt(cred, "realUserStatus") == 2, // ASUserDetectionStatusLikelyReal
		})
		reply(b, nil)
	}
}

// appleCredential is appleauth.Credential's JSON.
type appleCredential struct {
	User              string `json:"user"`
	IdentityToken     string `json:"identityToken"`
	AuthorizationCode string `json:"authorizationCode"`
	State             string `json:"state"`
	Email             string `json:"email"`
	GivenName         string `json:"givenName"`
	FamilyName        string `json:"familyName"`
	RealUser          bool   `json:"realUser"`
}

// webAuthModule is asuka/webauth's: "start" (u32 session, u8 flags: 1
// ephemeral, string URL, string callback scheme, empty for none) answers
// the callback URL, or the error "canceled"; "cancel" (u32 session)
// closes it, its start answering "canceled".
func (h *Host) webAuthModule() map[string]method {
	return map[string]method{
		"start": func(args []byte, reply func([]byte, error)) {
			id, flags, link, scheme, good := decodeWebAuth(args)
			if !good {
				reply(nil, errors.New("bad args"))
				return
			}
			if !strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://") {
				reply(nil, errors.New("not a web URL"))
				return
			}
			u := URL(link)
			if u == 0 || !loadAuthServices() || !HasClass("ASWebAuthenticationSession") {
				reply(nil, errors.New("not a web URL"))
				return
			}
			if authState.sessions == nil {
				authState.sessions = map[uint32]*webAuthSession{}
			}
			s := &webAuthSession{reply: reply}
			s.block = NewBlock(func(_ objc.Block, callback, err ID) {
				link := ""
				if callback != 0 {
					link = GoString(Send(callback, "absoluteString"))
				}
				var e error
				if callback == 0 {
					const canceledLogin = 1 // ASWebAuthenticationSessionErrorCodeCanceledLogin
					if err == 0 || SendInt(err, "code") == canceledLogin {
						e = errors.New("canceled")
					} else {
						e = errorText(err, "canceled")
					}
				}
				h.Post(func() { webAuthDone(id, s, []byte(link), e) })
			})
			var sch uintptr
			if scheme != "" {
				sch = uintptr(String(scheme))
			}
			s.session = Send(Send(Class("ASWebAuthenticationSession"), "alloc"), "initWithURL:callbackURLScheme:completionHandler:",
				uintptr(u), sch, uintptr(s.block))
			Send(s.session, "setPresentationContextProvider:", uintptr(authDelegate()))
			Send(s.session, "setPrefersEphemeralWebBrowserSession:", Bool(flags&1 != 0))
			authState.sessions[id] = s
			if !SendBool(s.session, "start") {
				webAuthDone(id, s, nil, errors.New("couldn't start the sign-in"))
			}
		},
		"cancel": func(args []byte, reply func([]byte, error)) {
			if len(args) >= 4 {
				id := binary.LittleEndian.Uint32(args)
				if s := authState.sessions[id]; s != nil {
					Send(s.session, "cancel")
					webAuthDone(id, s, nil, errors.New("canceled"))
				}
			}
			reply(nil, nil)
		},
	}
}

// webAuthDone answers a session's start, once.
func webAuthDone(id uint32, s *webAuthSession, b []byte, err error) {
	if authState.sessions[id] != s {
		return
	}
	delete(authState.sessions, id)
	Release(s.session)
	s.reply(b, err)
}

func decodeWebAuth(args []byte) (id uint32, flags byte, link, scheme string, ok bool) {
	if len(args) < 13 {
		return
	}
	id, flags = binary.LittleEndian.Uint32(args), args[4]
	link, rest := readString(args[5:])
	if rest == nil {
		return
	}
	scheme, rest = readString(rest)
	return id, flags, link, scheme, rest != nil
}

// The button.

// appleSignInButton is appleauth.Button: an ASAuthorizationAppleIDButton
// in a box of the host's, made again when its type or style changes
// (they're fixed as it's made), and for Automatic as the appearance
// changes. Props: type (sign in, continue, sign up), style (black,
// white, white outline, automatic), cornerRadius; event: press.
type appleSignInButton struct {
	ctx         native.Context
	box, button ID
	typ, style  int
	radius      float64
}

var appleSignInClasses bool

func newAppleSignInButton(ctx native.Context) native.View {
	if !loadAuthServices() || !HasClass("ASAuthorizationAppleIDButton") {
		return nil
	}
	if !appleSignInClasses {
		appleSignInClasses = true
		DefineClass("AsukaAppleSignInBox", "NSView", nil, []objc.MethodDef{
			Method("isFlipped", func(self ID, _ SEL) bool { return true }),
			Method("viewDidChangeEffectiveAppearance", func(self ID, _ SEL) {
				if b := authState.buttons[self]; b != nil && b.style == 3 {
					b.make()
				}
			}),
			Method("pressed:", func(self ID, _ SEL, sender ID) {
				if b := authState.buttons[self]; b != nil {
					b.ctx.Emit("press", nil)
				}
			}),
		})
	}
	if authState.buttons == nil {
		authState.buttons = map[ID]*appleSignInButton{}
	}
	b := &appleSignInButton{ctx: ctx, radius: -1}
	b.box = MsgInitRect(Send(Class("AsukaAppleSignInBox"), "alloc"), Sel("initWithFrame:"), Rect{Size: Size{Width: 200, Height: 32}})
	authState.buttons[b.box] = b
	b.make()
	return b
}

func (b *appleSignInButton) Handle() uintptr { return uintptr(b.box) }

func (b *appleSignInButton) Set(name string, v any) {
	switch name {
	case "type":
		n, _ := v.(int64)
		b.typ = int(n)
	case "style":
		n, _ := v.(int64)
		b.style = int(n)
	case "cornerRadius":
		f, _ := v.(float64)
		b.radius = -1
		if f > 0 {
			b.radius = f
		}
		if b.radius >= 0 && b.button != 0 {
			MsgSetFloat(b.button, Sel("setCornerRadius:"), b.radius)
		}
		return
	default:
		return
	}
	b.make()
}

// appleButtonStyle is a style prop (black, white, white outline,
// automatic) as ASAuthorizationAppleIDButtonStyle (white 0, white
// outline 1, black 2).
func appleButtonStyle(style int, dark bool) uintptr {
	switch style {
	case 1:
		return 0
	case 2:
		return 1
	case 3:
		if dark {
			return 0
		}
	}
	return 2
}

func (b *appleSignInButton) make() {
	if b.button != 0 {
		Send(b.button, "removeFromSuperview")
		Release(b.button)
	}
	dark := strings.Contains(GoString(Send(Send(b.box, "effectiveAppearance"), "name")), "Dark")
	typ := uintptr(min(max(b.typ, 0), 2))
	btn := Retain(Send(Class("ASAuthorizationAppleIDButton"), "buttonWithType:style:", typ, appleButtonStyle(b.style, dark)))
	if b.radius >= 0 {
		MsgSetFloat(btn, Sel("setCornerRadius:"), b.radius)
	}
	Send(btn, "setTarget:", uintptr(b.box))
	Send(btn, "setAction:", uintptr(Sel("pressed:")))
	MsgSetRect(btn, Sel("setFrame:"), MsgRect(b.box, Sel("bounds")))
	Send(btn, "setAutoresizingMask:", 18)
	Send(b.box, "addSubview:", uintptr(btn))
	b.button = btn
}

// Measure: as wide as it's given (at least Apple's 140, at most 375),
// 32 points tall, a Mac's large control.
func (b *appleSignInButton) Measure(w, _ float32) (float32, float32) {
	if !finite(w) {
		w = 200
	}
	return max(min(w, 375), 140), 32
}

func (b *appleSignInButton) Destroy() {
	delete(authState.buttons, b.box)
	if b.button != 0 {
		Send(b.button, "setTarget:", 0)
		Release(b.button)
		b.button = 0
	}
	Release(b.box)
}
