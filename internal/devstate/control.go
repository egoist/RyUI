package devstate

import (
	"bufio"
	"net"
	"os"
	"strconv"
	"sync"
)

// Under asuka dev, a new build starts while the one before it still runs:
// asuka dev starts the new process as soon as it's built, and the process
// waits (Wait) until asuka dev tells it to go on, which it does once the
// old one has stopped and saved its state. So the new build's start (the
// runtime, its packages' init, the platform's frameworks loaded, and on
// macOS the system's check of a program it hasn't seen run) happens while
// the old one is still on screen, and a build that crashes as it starts
// leaves the old one running.
//
// The two talk over a connection to the address ASUKA_CONTROL names
// (asuka dev's, on this machine's loopback): the process connects and
// sends its process ID, asuka dev answers one byte to let it go on, and
// later closes the connection to stop it.

var (
	waitOnce sync.Once
	stopped  chan struct{}
)

// Wait holds this run until asuka dev lets it go on, when ASUKA_CONTROL is
// set; call it before the app makes a window, listens or reads its kept
// state. It returns a channel closed when asuka dev asks this run to stop
// (it saves its state and exits), or nil when asuka dev doesn't control it.
// Stopped before it was let go on, the process exits at once, with nothing
// saved: it never ran. Later calls return the same channel.
func Wait() <-chan struct{} {
	waitOnce.Do(func() {
		addr := os.Getenv("ASUKA_CONTROL")
		if addr == "" {
			return
		}
		ch, ok := hold(addr)
		if !ok {
			os.Exit(0)
		}
		stopped = ch
	})
	return stopped
}

// hold connects to asuka dev at addr and waits for its go-ahead: ok is
// false when it closed the connection instead. Without a connection (asuka
// dev gone, or never there), the run goes on uncontrolled.
func hold(addr string) (stop chan struct{}, ok bool) {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, true
	}
	if _, err := c.Write([]byte(strconv.Itoa(os.Getpid()) + "\n")); err != nil {
		c.Close()
		return nil, true
	}
	r := bufio.NewReader(c)
	if _, err := r.ReadByte(); err != nil {
		c.Close()
		return nil, false
	}
	stop = make(chan struct{})
	go func() {
		// Nothing more comes: the connection's end is the stop.
		for {
			if _, err := r.ReadByte(); err != nil {
				break
			}
		}
		c.Close()
		close(stop)
	}()
	return stop, true
}
