package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/asuka/internal/devstate"
)

// The test binary is also a run of an app's build (ASUKA_TEST_RUN=echo):
// it waits for asuka dev's go-ahead, listens as internal/remote does (the
// descriptor it's handed, else ASUKA_REMOTE) and answers each connection
// with its process ID, then echoes it, until it's stopped.
func TestMain(m *testing.M) {
	if os.Getenv("ASUKA_TEST_RUN") == "echo" {
		echoRun()
		return
	}
	os.Exit(m.Run())
}

func echoRun() {
	stopped := devstate.Wait()
	var ln net.Listener
	var err error
	if fd := os.Getenv("ASUKA_REMOTE_FD"); fd != "" {
		n, _ := strconv.Atoi(fd)
		f := os.NewFile(uintptr(n), "remote")
		ln, err = net.FileListener(f)
		f.Close()
	} else {
		ln, err = net.Listen("tcp", os.Getenv("ASUKA_REMOTE"))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(2)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				fmt.Fprintf(c, "%d\n", os.Getpid())
				r := bufio.NewReader(c)
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					c.Write([]byte(l))
				}
			}()
		}
	}()
	if stopped != nil {
		<-stopped
	} else {
		select {}
	}
}

// hello connects to addr and reads the run's process ID, then checks
// that it echoes.
func hello(t *testing.T, addr string) (net.Conn, int) {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	c.SetDeadline(time.Now().Add(relayWait + 10*time.Second))
	r := bufio.NewReader(c)
	l, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("reading the run's ID: %v", err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(l))
	fmt.Fprintf(c, "ping\n")
	if l, err := r.ReadString('\n'); err != nil || l != "ping\n" {
		t.Fatalf("echo: %q, %v", l, err)
	}
	return c, pid
}

// The runs of an app's build reloaded by asuka dev keep the port the
// player connects to, handed their listener (macOS, Linux) or relayed to
// their own (Windows): a connection reaches the run that's running, one
// made between runs waits for the next, and a stopped run's end.
func TestRunsKeepTheirListener(t *testing.T) {
	for _, relay := range []bool{false, true} {
		t.Run(fmt.Sprintf("relay=%v", relay), func(t *testing.T) {
			if !relay && runtime.GOOS == "windows" {
				t.Skip("Windows can't hand a listener to a process")
			}
			was := relayListeners
			relayListeners = relay
			defer func() { relayListeners = was }()

			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			p := &hostProcess{app: &goApp{dir: t.TempDir()}, bin: exe, env: []string{"ASUKA_TEST_RUN=echo"}}
			if err := p.inherit("ASUKA_REMOTE_FD", "ASUKA_REMOTE", "127.0.0.1:0"); err != nil {
				t.Fatal(err)
			}
			addr := p.listeners[0].addr
			defer func() {
				if p.run != nil {
					p.stop()
				}
			}()

			if err := p.restart(); err != nil {
				t.Fatal(err)
			}
			first := p.run.proc.Pid
			c, pid := hello(t, addr)
			if pid != first {
				t.Fatalf("connected to %d, the run is %d", pid, first)
			}
			// A reload: the first run stops (its connection ends), the
			// next answers.
			if err := p.restart(); err != nil {
				t.Fatal(err)
			}
			if _, err := bufio.NewReader(c).ReadString('\n'); err == nil {
				t.Error("the stopped run's connection goes on")
			}
			c.Close()
			second := p.run.proc.Pid
			if second == first {
				t.Fatal("the same run after a reload")
			}
			c, pid = hello(t, addr)
			c.Close()
			if pid != second {
				t.Fatalf("connected to %d, the run is %d", pid, second)
			}

			// Between runs: a connection waits for the next.
			p.stop()
			between, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer between.Close()
			if err := p.restart(); err != nil {
				t.Fatal(err)
			}
			between.SetReadDeadline(time.Now().Add(relayWait + 10*time.Second))
			l, err := bufio.NewReader(between).ReadString('\n')
			if err != nil {
				t.Fatalf("the connection made between runs never reached the next: %v", err)
			}
			if pid, _ := strconv.Atoi(strings.TrimSpace(l)); pid != p.run.proc.Pid {
				t.Fatalf("connected to %d, the run is %d", pid, p.run.proc.Pid)
			}
		})
	}
}

// A relay's connection goes to the address it routes to when it's made,
// waiting for one to listen; a half-closed client still gets its answer.
func TestRelay(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r := newRelay(ln)
	defer ln.Close()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.Write([]byte("request"))
	c.(*net.TCPConn).CloseWrite() // the whole request

	// Nothing to route to yet; then a server that answers what it read.
	srv, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go func() {
		s, err := srv.Accept()
		if err != nil {
			return
		}
		defer s.Close()
		b := make([]byte, 64)
		n := 0
		for {
			m, err := s.Read(b[n:])
			n += m
			if err != nil {
				break
			}
		}
		s.Write([]byte("answer to " + string(b[:n])))
	}()
	r.route(srv.Addr().String())
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	var got strings.Builder
	buf := make([]byte, 64)
	for {
		n, err := c.Read(buf)
		got.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if got.String() != "answer to request" {
		t.Fatalf("got %q", got.String())
	}
}
