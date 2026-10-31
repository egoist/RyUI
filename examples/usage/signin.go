package main

import (
	"errors"
	"strings"
	"time"

	"github.com/egoist/ryui/secure"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
	"github.com/egoist/ryui/webauth"
)

// addSheet signs in with Claude: to add an account, or, with id, that
// account again. Or it adds a demo account.
//
// The sign-in is the system's sheet (ryui/webauth): Claude's page, with
// the browser's sessions, Google's and Apple's sign-in. It redirects to
// a server of the app's on localhost, which gets the code. Signing in
// with a code instead redirects to a page that shows it, to paste.
func addSheet(open *signal.Signal[bool], id string) ui.Node {
	return ui.Sheet(ui.SheetProps{Open: open, Detents: ui.DetentLarge, Grabber: true, Content: func() ui.Node {
		working := signal.New(false)
		failed := signal.New("")
		pasting := signal.New(false)
		pasted := signal.New("")
		var session *webauth.Session
		var server *loopback
		manual := newLogin(manualRedirect)
		stop := func() {
			if session != nil {
				session.Cancel()
				session = nil
			}
			if server != nil {
				server.Close()
				server = nil
			}
		}
		signal.OnCleanup(stop)
		finish := func(l login, code string) {
			if working.Peek() {
				return
			}
			working.Set(true)
			failed.Set("")
			go func() {
				in, err := l.exchange(code)
				ui.Post(func() {
					if err != nil {
						working.Set(false)
						failed.Set("Couldn't sign in: " + err.Error())
						return
					}
					signIn(id, in, func(err error) {
						working.Set(false)
						if err != nil {
							failed.Set("Couldn't keep the sign-in: " + err.Error())
							return
						}
						open.Set(false)
					})
				})
			}()
		}
		start := func() {
			stop()
			failed.Set("")
			srv, err := listen()
			if err != nil {
				failed.Set("Couldn't sign in: " + err.Error())
				return
			}
			server = srv
			l := newLogin(srv.URL)
			s := webauth.Start(webauth.Options{URL: l.URL()}, func(_ string, err error) {
				// The user closed it before it redirected: nothing to say.
				if err != nil && !errors.Is(err, webauth.ErrCanceled) {
					failed.Set("Couldn't sign in: " + err.Error())
				}
			})
			session = s
			go func() {
				var u string
				select {
				case u = <-srv.codes:
				case <-srv.done:
					return // closed, or another sign-in started
				}
				ui.Post(func() {
					if session != s {
						return // another sign-in's, since
					}
					stop() // closes the sheet: the code is here
					code, ok := l.codeFrom(u)
					if !ok {
						failed.Set("Couldn't sign in: the page didn't give a code.")
						return
					}
					finish(l, code)
				})
			}()
		}
		title := "Sign in with Claude"
		if id != "" {
			title = "Sign in again"
		}
		paste := func() {
			code, ok := manual.codeFrom(pasted.Peek())
			if !ok {
				failed.Set("That isn't this sign-in's code: copy the whole code the page shows.")
				return
			}
			finish(manual, code)
		}
		return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.SafeArea(ui.Style{Padding: ui.Edges{Top: 40, Left: 24, Right: 24, Bottom: 24}, Gap: 16, Align: ui.AlignCenter},
				badge("claude", 72),
				ui.StaticText(title, ui.Style{Font: ui.Title2.Weighted(ui.Bold), TextAlign: ui.TextCenter}),
				ui.StaticText("Sign in with your Claude account to follow its plan's limits. This app only reads your usage and your profile: it can't send messages or see your chats.",
					ui.Style{Color: ui.SecondaryLabel, TextAlign: ui.TextCenter}),
				ui.Column(ui.Style{Gap: 12, AlignSelf: ui.AlignStretch, Margin: ui.Edges{Top: 8}},
					ui.Button(ui.ButtonProps{Title: ui.Val("Continue with Claude"), Variant: ui.ButtonFilled, OnPress: start,
						Disabled: working}),
					ui.Show(pasting, func() ui.Node {
						return ui.Column(ui.Style{Gap: 12, Margin: ui.Edges{Top: 8}},
							note("Sign in on the page that opened. At the end it shows a code: copy it, close the page, and paste it here."),
							ui.TextField(ui.TextFieldProps{Value: pasted, Placeholder: ui.Val("Code"), Autocapitalize: ui.CapitalizeNone,
								Autocorrect: ui.AutocorrectNo, ReturnKey: ui.ReturnGo, OnSubmit: paste}),
							ui.Button(ui.ButtonProps{Title: ui.Val("Sign In"), Variant: ui.ButtonGray, OnPress: paste,
								Disabled: signal.Func(func() bool { return strings.TrimSpace(pasted.Get()) == "" || working.Get() })}),
						)
					}, func() ui.Node {
						return ui.Button(ui.ButtonProps{Title: ui.Val("Sign In with a Code"), OnPress: func() {
							stop()
							failed.Set("")
							pasting.Set(true)
							session = webauth.Start(webauth.Options{URL: manual.URL()}, nil)
						}})
					}),
				),
				ui.Show(signal.Func(func() bool { return id == "" && !pasting.Get() }), func() ui.Node {
					return ui.Button(ui.ButtonProps{Title: ui.Val("Try a Demo Account"), OnPress: func() {
						save(demo(time.Now()))
						open.Set(false)
					}})
				}),
				ui.Show(working, func() ui.Node { return ui.Activity() }),
				ui.Show(signal.Func(func() bool { return failed.Get() != "" }), func() ui.Node {
					return ui.Text(failed, ui.Style{Font: ui.Footnote, Color: ui.Red, TextAlign: ui.TextCenter})
				}),
			),
		)
	}})
}

// signIn keeps a sign-in's tokens and adds its account, or updates it:
// the one with id, or the one signed in to the same email before.
func signIn(id string, in signedIn, done func(error)) {
	if id == "" {
		for _, a := range accounts.Peek() {
			if !a.Demo && a.Email != "" && a.Email == in.Email {
				id = a.ID
			}
		}
	}
	if id == "" {
		id = time.Now().Format("20060102150405.000")
	}
	secure.Save("token."+id, in.tokens.String(), func(err error) {
		if err != nil {
			done(err)
			return
		}
		a, ok := find(id)
		if !ok {
			a = Account{ID: id, Service: "claude"}
		}
		if in.Email != "" {
			a.Email = in.Email
		}
		if in.Org != "" {
			a.Org = in.Org
		}
		a.Error = ""
		save(a)
		done(nil)
		refresh(id)
	})
}
