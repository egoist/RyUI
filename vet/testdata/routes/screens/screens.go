// Package screens sees no routes: its links are checked where they're
// declared (the app).
package screens

import "github.com/egoist/asuka/ui"

func Inbox() ui.Node {
	return ui.Column(ui.Style{},
		ui.Button(ui.ButtonProps{Title: ui.Val("Open"), Href: "/chats/7"}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Typo"), Href: "/chtas/7"}),
	)
}
