package periodic

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/ui"
)

// Schedule sends the task's name, interval (15 minutes at least) and
// constraints; a run the host hands over runs its handler, with a budget
// short of the host's, and is reported done.
func TestScheduleAndRun(t *testing.T) {
	h := fake.New()
	var scheduled []byte
	runs := [][]byte{run("sync", 30), run("unknown", 30)}
	var done []string
	h.Modules = map[string]func(string, []byte) ([]byte, error){"periodic": func(m string, args []byte) ([]byte, error) {
		switch m {
		case "schedule":
			scheduled = args
		case "next":
			if len(runs) == 0 {
				return nil, errors.New("no more") // ends the poll
			}
			b := runs[0]
			runs = runs[1:]
			return b, nil
		case "done":
			n := binary.LittleEndian.Uint32(args)
			done = append(done, string(args[4:4+n])+map[byte]string{0: " failed", 1: " ok"}[args[4+n]])
		}
		return nil, nil
	}}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	var budget time.Duration
	ui.Post(func() {
		Schedule("sync", Options{Every: time.Minute, Network: true})
		Handle("sync", func(ctx context.Context) error {
			d, _ := ctx.Deadline()
			budget = time.Until(d)
			return nil
		})
	})
	for range 5000 { // a safety net, not what passes
		h.RunPosted()
		r.Flush()
		if len(done) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	le := binary.LittleEndian
	if string(scheduled[4:8]) != "sync" || le.Uint32(scheduled[8:]) != 15*60 || scheduled[12] != 1 {
		t.Fatalf("schedule %v", scheduled)
	}
	if len(done) != 2 || done[0] != "sync ok" || done[1] != "unknown ok" {
		t.Fatalf("done %v", done)
	}
	if budget <= 20*time.Second || budget > 27*time.Second {
		t.Fatalf("budget %v", budget)
	}
}

func run(name string, secs uint32) []byte {
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(name)))
	return binary.LittleEndian.AppendUint32(append(b, name...), secs)
}

func TestDecodeRun(t *testing.T) {
	if _, _, ok := decodeRun([]byte{9, 0, 0, 0, 'a'}); ok {
		t.Fatal("short: ok")
	}
	n, d, ok := decodeRun(run("a", 2))
	if !ok || n != "a" || d != time.Second {
		t.Fatal(n, d, ok)
	}
}
