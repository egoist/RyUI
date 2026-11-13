//go:build linux && !android

package linux

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// asuka/sdk/speech on Linux: Speech Dispatcher (the desktop's speech service,
// which Orca speaks through), over its socket in SSIP, its text protocol:
// no library to load, and the daemon is started as libspeechd starts it
// when it isn't running. The connection's reads and writes are a
// goroutine's; replies come back to the main thread.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["speech"] = h.speechModule()
	})
}

// ssipConn is a connection to Speech Dispatcher.
type ssipConn struct {
	conn    net.Conn
	r       *bufio.Reader
	mu      sync.Mutex      // one command at a time
	replies chan ssipReply  // command replies, in order
	events  func(ssipReply) // 7xx notifications (BEGIN, END...), from the reader
	closed  chan struct{}
}

// ssipReply is a reply: its code and its lines (the last's text after
// the code).
type ssipReply struct {
	code  int
	lines []string
}

func (r ssipReply) ok() bool { return r.code >= 200 && r.code < 300 }

func (r ssipReply) err() error {
	if r.ok() {
		return nil
	}
	msg := ""
	if len(r.lines) > 0 {
		msg = r.lines[len(r.lines)-1]
	}
	return fmt.Errorf("speech-dispatcher: %d %s", r.code, msg)
}

// ssipSocket is where the daemon listens.
func ssipSocket() string {
	if a := os.Getenv("SPEECHD_ADDRESS"); strings.HasPrefix(a, "unix_socket:") {
		return strings.TrimPrefix(a, "unix_socket:")
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("speech-dispatcher-%d", os.Getuid()))
	}
	return filepath.Join(dir, "speech-dispatcher", "speechd.sock")
}

// dialSSIP connects, starting the daemon if it isn't running. Not on the
// main thread.
func dialSSIP(events func(ssipReply)) (*ssipConn, error) {
	path := ssipSocket()
	c, err := net.Dial("unix", path)
	if err != nil {
		exe, lerr := exec.LookPath("speech-dispatcher")
		if lerr != nil {
			return nil, errors.New("asuka/sdk/speech: speech-dispatcher isn't installed")
		}
		// As libspeechd autospawns it: it forks, and exits once it listens.
		cmd := exec.Command(exe, "--spawn", "--communication-method", "unix_socket", "--socket-path", path)
		cmd.Run()
		for i := 0; i < 50; i++ {
			if c, err = net.Dial("unix", path); err == nil {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if err != nil {
			return nil, fmt.Errorf("asuka/sdk/speech: speech-dispatcher doesn't start: %v", err)
		}
	}
	s := &ssipConn{conn: c, r: bufio.NewReader(c), replies: make(chan ssipReply, 4), events: events, closed: make(chan struct{})}
	go s.read()
	user := os.Getenv("USER")
	if user == "" {
		user = "user"
	}
	app := strings.ReplaceAll(filepath.Base(os.Args[0]), ":", "_")
	for _, cmd := range []string{
		"SET self CLIENT_NAME " + user + ":" + app + ":main",
		"SET self NOTIFICATION ALL on",
	} {
		if r, err := s.command(cmd); err != nil || !r.ok() {
			c.Close()
			if err == nil {
				err = r.err()
			}
			return nil, err
		}
	}
	return s, nil
}

// read reads replies: notifications go to events, the rest to replies.
func (s *ssipConn) read() {
	defer close(s.closed)
	var lines []string
	for {
		line, err := s.r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if len(line) < 4 {
			continue
		}
		code, _ := strconv.Atoi(line[:3])
		lines = append(lines, line[4:])
		if line[3] == '-' {
			continue
		}
		r := ssipReply{code: code, lines: lines}
		lines = nil
		if code >= 700 && code < 800 {
			s.events(r)
			continue
		}
		s.replies <- r
	}
}

// command sends a command and waits for its reply. Not on the main
// thread.
func (s *ssipConn) command(cmd string) (ssipReply, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.commandLocked(cmd)
}

func (s *ssipConn) commandLocked(cmd string) (ssipReply, error) {
	if _, err := s.conn.Write([]byte(cmd + "\r\n")); err != nil {
		return ssipReply{}, err
	}
	select {
	case r := <-s.replies:
		return r, nil
	case <-s.closed:
		return ssipReply{}, errors.New("speech-dispatcher: the connection closed")
	}
}

// speak sends SPEAK and the text; the reply has the message's ID.
func (s *ssipConn) speak(settings []string, text string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range settings {
		if r, err := s.commandLocked(c); err != nil {
			return 0, err
		} else if !r.ok() && !strings.Contains(c, "SYNTHESIS_VOICE") && !strings.Contains(c, "LANGUAGE") {
			return 0, r.err()
		}
	}
	r, err := s.commandLocked("SPEAK")
	if err != nil {
		return 0, err
	}
	if r.code != 230 {
		return 0, r.err()
	}
	// The text: lines, a lone "." escaped as "..", then ".".
	var b strings.Builder
	for _, l := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(l, ".") {
			b.WriteString(".")
		}
		b.WriteString(l)
		b.WriteString("\r\n")
	}
	b.WriteString(".")
	r, err = s.commandLocked(b.String())
	if err != nil {
		return 0, err
	}
	if !r.ok() || len(r.lines) == 0 {
		return 0, r.err()
	}
	id, _ := strconv.Atoi(r.lines[0])
	return id, nil
}

// speechState is the module's, on the main thread.
type speechState struct {
	conn    *ssipConn
	dialing []func(*ssipConn, error)
	replies map[int]func([]byte, error) // by message ID
	early   map[int]bool                // ended before its ID came back
	pending int                         // speaks sent, their IDs not back yet
	paused  bool
}

var speech = &speechState{replies: map[int]func([]byte, error){}, early: map[int]bool{}}

// connection calls f with the connection, connecting the first time
// (off the main thread).
func (h *Host) speechConn(f func(*ssipConn, error)) {
	s := speech
	if s.conn != nil {
		f(s.conn, nil)
		return
	}
	s.dialing = append(s.dialing, f)
	if len(s.dialing) > 1 {
		return
	}
	go func() {
		c, err := dialSSIP(func(r ssipReply) { h.Post(func() { h.speechEvent(r) }) })
		h.Post(func() {
			if err == nil {
				s.conn = c
				go func() {
					<-c.closed
					h.Post(func() {
						if s.conn == c {
							s.conn = nil
							s.ended(errors.New("speech-dispatcher: the connection closed"))
						}
					})
				}()
			}
			fs := s.dialing
			s.dialing = nil
			for _, f := range fs {
				f(c, err)
			}
		})
	}()
}

// speechEvent is a notification: 702 END, 703 CANCELED end a message.
func (h *Host) speechEvent(r ssipReply) {
	s := speech
	if len(r.lines) < 1 {
		return
	}
	id, _ := strconv.Atoi(r.lines[0])
	switch r.code {
	case 702, 703:
		reply := s.replies[id]
		if reply == nil {
			if s.pending > 0 {
				s.early[id] = r.code == 702
			}
			return
		}
		delete(s.replies, id)
		if r.code == 702 {
			reply(nil, nil)
		} else {
			reply(nil, errors.New("stopped"))
		}
	case 704:
		s.paused = true
	case 705:
		s.paused = false
	}
}

// ended fails every message waiting.
func (s *speechState) ended(err error) {
	rs := s.replies
	s.replies = map[int]func([]byte, error){}
	for _, r := range rs {
		r(nil, err)
	}
}

// ssipScale is a multiplier (0.5 to 2, 1 normal) as SSIP's -100 to 100.
func ssipScale(x float64) int {
	if x <= 0 {
		x = 1
	}
	return int(math.Round(min(max(100*math.Log2(x), -100), 100)))
}

// speechModule is asuka/sdk/speech's: "speak" (JSON: text, language, voice,
// pitch, rate, volume) answers once spoken, or fails with "stopped";
// "stop", "pause", "resume", "speaking" (u8), "voices" (JSON).
func (h *Host) speechModule() map[string]method {
	s := speech
	// do runs a command off the main thread, answering on it.
	do := func(cmd string, reply func([]byte, error)) {
		h.speechConn(func(c *ssipConn, err error) {
			if err != nil {
				reply(nil, err)
				return
			}
			go func() {
				r, err := c.command(cmd)
				if err == nil {
					err = r.err()
				}
				h.Post(func() { reply(nil, err) })
			}()
		})
	}
	return map[string]method{
		"speak": func(args []byte, reply func([]byte, error)) {
			var o struct {
				Text, Language, Voice string
				Pitch, Rate, Volume   float64
			}
			if err := json.Unmarshal(args, &o); err != nil {
				reply(nil, errors.New("bad args"))
				return
			}
			settings := []string{
				fmt.Sprintf("SET self RATE %d", ssipScale(o.Rate)),
				fmt.Sprintf("SET self PITCH %d", ssipScale(o.Pitch)),
				fmt.Sprintf("SET self VOLUME %d", int(math.Round(min(max(o.Volume, 0), 1)*200-100))),
			}
			lang := o.Language
			if lang == "" {
				lang = h.locale
			}
			if lang != "" {
				settings = append(settings, "SET self LANGUAGE "+strings.ToLower(strings.ReplaceAll(lang, "_", "-")))
			}
			if o.Voice != "" {
				settings = append(settings, "SET self SYNTHESIS_VOICE "+o.Voice)
			}
			h.speechConn(func(c *ssipConn, err error) {
				if err != nil {
					reply(nil, err)
					return
				}
				s.pending++
				go func() {
					id, err := c.speak(settings, o.Text)
					h.Post(func() {
						s.pending--
						if err != nil {
							reply(nil, err)
							return
						}
						if done, ok := s.early[id]; ok {
							delete(s.early, id)
							if done {
								reply(nil, nil)
							} else {
								reply(nil, errors.New("stopped"))
							}
							return
						}
						s.replies[id] = reply
						if s.pending == 0 {
							s.early = map[int]bool{}
						}
					})
				}()
			})
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			s.ended(errors.New("stopped"))
			s.paused = false
			if s.conn == nil {
				reply(nil, nil)
				return
			}
			do("CANCEL self", reply)
		},
		"pause": func(_ []byte, reply func([]byte, error)) {
			if s.conn == nil {
				reply(nil, nil)
				return
			}
			s.paused = true
			do("PAUSE self", reply)
		},
		"resume": func(_ []byte, reply func([]byte, error)) {
			if s.conn == nil {
				reply(nil, nil)
				return
			}
			s.paused = false
			do("RESUME self", reply)
		},
		"speaking": func(_ []byte, reply func([]byte, error)) {
			reply([]byte{b2u(len(s.replies) > 0 || s.pending > 0)}, nil)
		},
		"voices": func(_ []byte, reply func([]byte, error)) {
			h.speechConn(func(c *ssipConn, err error) {
				if err != nil {
					reply(nil, err)
					return
				}
				go func() {
					r, err := c.command("LIST SYNTHESIS_VOICES")
					if err == nil {
						err = r.err()
					}
					var b []byte
					if err == nil {
						b, err = json.Marshal(ssipVoices(r.lines))
					}
					h.Post(func() { reply(b, err) })
				}()
			})
		},
	}
}

type speechVoice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Language string `json:"language"`
	Quality  int    `json:"quality"`
	Network  bool   `json:"network"`
}

// ssipVoices is LIST SYNTHESIS_VOICES's lines (name, language, variant
// by tabs; the last line the status) as the module's voices.
func ssipVoices(lines []string) []speechVoice {
	vs := []speechVoice{}
	for i, l := range lines {
		if i == len(lines)-1 {
			break // "OK VOICE LIST SENT"
		}
		f := strings.Split(l, "\t")
		if len(f) < 2 || f[0] == "" {
			continue
		}
		lang := f[1]
		if parts := strings.SplitN(lang, "-", 2); len(parts) == 2 {
			lang = strings.ToLower(parts[0]) + "-" + strings.ToUpper(parts[1])
		}
		vs = append(vs, speechVoice{ID: f[0], Name: f[0], Language: lang})
	}
	return vs
}
