package devstate

import (
	"bufio"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
)

type result struct {
	stop chan struct{}
	ok   bool
}

// devSide is asuka dev's end: it accepts the process's connection and
// reads its process ID.
func devSide(t *testing.T) (net.Listener, func() net.Conn) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return ln, func() net.Conn {
		c, err := ln.Accept()
		if err != nil {
			t.Fatal(err)
		}
		line, err := bufio.NewReader(c).ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if pid, _ := strconv.Atoi(strings.TrimSpace(line)); pid != os.Getpid() {
			t.Fatalf("pid %q, want %d", line, os.Getpid())
		}
		return c
	}
}

func TestHoldWaitsForTheGoAheadThenForTheStop(t *testing.T) {
	ln, accept := devSide(t)
	done := make(chan result)
	go func() {
		stop, ok := hold(ln.Addr().String())
		done <- result{stop, ok}
	}()
	c := accept()
	select {
	case <-done:
		t.Fatal("went on before asuka dev said so")
	default:
	}
	c.Write([]byte{'g'})
	r := <-done
	if !r.ok || r.stop == nil {
		t.Fatal(r)
	}
	select {
	case <-r.stop:
		t.Fatal("stopped while the connection is open")
	default:
	}
	c.Close()
	<-r.stop
}

func TestHoldEndsWhenStoppedBeforeTheGoAhead(t *testing.T) {
	ln, accept := devSide(t)
	done := make(chan result)
	go func() {
		stop, ok := hold(ln.Addr().String())
		done <- result{stop, ok}
	}()
	accept().Close()
	if r := <-done; r.ok {
		t.Fatal("went on, superseded")
	}
}

func TestHoldGoesOnWithoutAsukaDev(t *testing.T) {
	ln, _ := devSide(t)
	addr := ln.Addr().String()
	ln.Close()
	if stop, ok := hold(addr); !ok || stop != nil {
		t.Fatal(stop, ok)
	}
}
