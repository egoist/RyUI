package app // want package:"routes \\[/ /old/:id /gone/:id /login /chats /chats/:id /chats/:id/files/\\*path\\] open false links 0"

import (
	"strconv"

	"example.com/vettest/routes/screens"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

var signedIn = signal.New(false)

var chatRoutes = []ui.Route{
	{Screen: list},
	{Path: ":id", Screen: chat},
	{Path: ":id/files/*path", Screen: chat},
}

func list(ui.Params) ui.Node { return screens.Inbox() }
func chat(ui.Params) ui.Node { return ui.StaticText("chat") }

func App() ui.Node {
	h := ui.NewRouterHandle()
	return ui.Router(ui.RouterProps{Handle: h, Initial: "/chats"}, // want `no route takes "/chtas/7" \(at .*screens.go:10:.*\)`
		ui.Route{Path: "/", Redirect: "/chats"},
		ui.Route{Path: "/old/:id", Redirect: "/chats/:id"},
		ui.Route{Path: "/gone/:id", Redirect: "/chat/:id"}, // want `no route takes "/chat/:id"`
		ui.Route{Path: "/login", Screen: chat, Guard: func(to ui.Href) ui.Href {
			if signedIn.Get() {
				return "/chats"
			}
			return ""
		}},
		ui.Route{Path: "/chats", Layout: ui.LayoutStack, Children: chatRoutes, Guard: func(to ui.Href) ui.Href {
			if !signedIn.Get() {
				return ui.Href("/login").With("next", string(to))
			}
			return ""
		}},
	)
}

// Reported: hrefs no route takes, HrefOf's values.

func Links(h *ui.RouterHandle, id int) {
	h.Navigate("/chats/7")
	h.Navigate("/chats/7/files/a/b.md")
	h.Navigate("/chats/7/info")                // want `no route takes "/chats/7/info": the Router shows its not-found screen \(its routes: /, /chats, /chats/:id, /chats/:id/files/\*path, /gone/:id, /login, /old/:id\)`
	ui.Navigate("/setings")                    // want `no route takes "/setings"`
	h.Push(ui.HrefOf("/chats/:id", id))        // a route's pattern
	h.Push(ui.HrefOf("/chats/:id/info", id))   // want `no route takes "/chats/:id/info"`
	h.Push(ui.HrefOf("/chats/:id", id, 2))     // want `ui.HrefOf\("/chats/:id"\) given 2 values for its 1 parameters: it panics`
	h.Replace("/login?next=%2Fchats")          // the query isn't the route's
	_ = ui.NavLinkProps{Href: "/chats/" + "9"} // a constant made of constants
	_ = ui.ViewProps{Href: "/nowhere"}         // want `no route takes "/nowhere"`
}

// Not reported: hrefs that aren't constants, or aren't the app's paths.

func NotConstants(h *ui.RouterHandle, id int) {
	h.Navigate(ui.Href("/chats/" + strconv.Itoa(id)))
	h.Navigate(ui.Href("relative"))
	ui.OpenURL("https://example.com/whatever")
}
