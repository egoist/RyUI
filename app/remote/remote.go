// Package remote serves the app to ryui dev's players: ryui dev runs the
// app's Go code on this machine and its views in a player app on a
// simulator, an emulator or a phone (internal/remote).
//
// ryui dev links it into the app it runs that way; an app never imports
// it. Release builds leave it out, and with it the network code it needs.
package remote

import (
	"github.com/egoist/ryui/internal/boot"
	"github.com/egoist/ryui/internal/remote"
	"github.com/egoist/ryui/ui"
)

func init() {
	boot.Remote = func(addr string, root, sheet any) error {
		return remote.Run(addr, root.(func() ui.Node), sheet.(func() ui.Node))
	}
}
