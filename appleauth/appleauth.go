// Package appleauth is Sign in with Apple (expo-apple-authentication):
// the system's sheet, Face ID and the Apple Account the device is signed
// in with, and Apple's button for it. The App Store requires it of an
// app that offers another company's sign-in (Google, Facebook) for its
// accounts (guideline 4.8).
//
// It's iOS's: on Android, Available is false and SignIn fails with
// ErrNotAvailable; sign in with Apple there on the web (ryui/webauth).
// An app that imports it is given the Sign in with Apple entitlement;
// its App ID needs the capability (developer.apple.com) for a release
// build.
//
// Send the credential's IdentityToken (a JWT Apple signs) and
// AuthorizationCode to the app's server, which checks them with Apple.
package appleauth

import (
	"encoding/json"
	"errors"

	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// Scope is what the app asks for, besides who the user is.
type Scope uint8

const (
	FullName Scope = 1 << iota // the user's name
	Email                      // their email, or a relay address of Apple's
)

// Options are the sign-in's.
type Options struct {
	Scopes Scope
	// Nonce is a value the app's server made, echoed in the identity
	// token: a token for this sign-in only. (Firebase wants it SHA-256
	// hashed here, and the plain one sent with the token.)
	Nonce string
	// State is echoed in the credential.
	State string
}

// Credential is who signed in.
type Credential struct {
	// User is the user's stable ID for this app's team: the account's key.
	User string `json:"user"`
	// IdentityToken is a JWT Apple signs, for the app's server to check.
	IdentityToken string `json:"identityToken"`
	// AuthorizationCode is short-lived, for the server to exchange with
	// Apple for tokens.
	AuthorizationCode string `json:"authorizationCode"`
	State             string `json:"state"`
	// Email, GivenName and FamilyName come only the first time the user
	// signs in to the app (if asked for): keep them then.
	Email      string `json:"email"`
	GivenName  string `json:"givenName"`
	FamilyName string `json:"familyName"`
	// RealUser: Apple is confident it's a person (not a bot).
	RealUser bool `json:"realUser"`
}

// Errors SignIn reports.
var (
	ErrCanceled     = errors.New("appleauth: canceled")
	ErrNotAvailable = errors.New("appleauth: not available")
)

// Available reports whether Sign in with Apple is on this device (iOS).
// Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Available() bool {
	b, err := ui.CallWait("appleAuth", "available", nil)
	return err == nil && len(b) > 0 && b[0] == 1
}

// SignIn shows the system's Sign in with Apple sheet and reports the
// credential, or ErrCanceled. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func SignIn(o Options) (Credential, error) {
	args, _ := json.Marshal(map[string]any{"fullName": o.Scopes&FullName != 0, "email": o.Scopes&Email != 0, "nonce": o.Nonce, "state": o.State})
	b, err := ui.CallWait("appleAuth", "signIn", args)
	if err != nil {
		switch err.Error() {
		case "canceled":
			return Credential{}, ErrCanceled
		case "not available", "no module method appleAuth.signIn":
			return Credential{}, ErrNotAvailable
		}
		return Credential{}, err
	}
	var c Credential
	err = json.Unmarshal(b, &c)
	return c, err
}

// State is an Apple ID's standing with the app.
type State uint8

const (
	Revoked     State = iota // the user stopped using Sign in with Apple for the app: sign them out
	Authorized               // signed in
	NotFound                 // no such user for the app
	Transferred              // the app moved to another team
)

// CredentialState is user's standing (Credential.User): check it at
// launch, and sign out a revoked user. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func CredentialState(user string) (State, error) {
	b, err := ui.CallWait("appleAuth", "state", []byte(user))
	if err != nil || len(b) == 0 {
		return NotFound, err
	}
	return State(b[0]), nil
}

// ButtonType is the button's title.
type ButtonType uint8

const (
	SignInButton   ButtonType = iota // "Sign in with Apple"
	ContinueButton                   // "Continue with Apple"
	SignUpButton                     // "Sign up with Apple"
)

// ButtonStyle is the button's colors.
type ButtonStyle uint8

const (
	Black        ButtonStyle = iota // on light backgrounds
	White                           // on dark ones
	WhiteOutline                    // white, outlined, on white
	Automatic                       // black in light mode, white in dark
)

// ButtonProps are the button's.
type ButtonProps struct {
	Type  ButtonType
	Style ButtonStyle
	// CornerRadius rounds it (default: the system's).
	CornerRadius float64
	// OnPress is the tap: call SignIn (from a goroutine).
	OnPress func()
	// Layout is the button's box: it's as wide as it's given, 44 points
	// tall unless the style says.
	Layout ui.Styler
}

// Button is Apple's own Sign in with Apple button
// (ASAuthorizationAppleIDButton), which the guidelines ask for: its
// title, logo and colors are Apple's, in the user's language. On Android,
// where there's no Sign in with Apple, it's nothing.
func Button(p ButtonProps) ui.Node {
	if ui.UseEnvironment().Get().Platform != ui.PlatformIOS {
		return ui.Show(signal.Static(false), nil)
	}
	np := ui.NativeProps{Style: p.Layout, Props: map[string]any{
		"type": int(p.Type), "style": int(p.Style), "cornerRadius": p.CornerRadius,
	}, On: map[string]func(ui.NativeEvent){}}
	if p.OnPress != nil {
		np.On["press"] = func(ui.NativeEvent) { p.OnPress() }
	}
	return ui.Native("ryui.appleSignIn", np)
}
