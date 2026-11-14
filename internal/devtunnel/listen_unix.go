//go:build unix

package devtunnel

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// listenShared listens on addr with SO_REUSEPORT: the player's app and
// its share extension listen on the same ports, and the newest
// listener takes what comes (XNU's), the app's again once the
// extension's closes.
func listenShared(addr string) (net.Listener, error) {
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		})
		if err != nil {
			return err
		}
		return serr
	}}
	return lc.Listen(context.Background(), "tcp", addr)
}
