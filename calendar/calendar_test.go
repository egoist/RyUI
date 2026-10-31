package calendar_test

import (
	"testing"
	"time"

	"github.com/egoist/ryui/calendar"
	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/ui"
)

// Events read the hosts' ms; an all-day event's days are UTC midnights.
func TestEvents(t *testing.T) {
	h := fake.New()
	h.Modules = map[string]func(string, []byte) ([]byte, error){"calendar": func(m string, a []byte) ([]byte, error) {
		return []byte(`[{"id":"1","title":"Lunch","startMs":1700000000000,"endMs":1700003600000},
			{"id":"2","title":"Holiday","allDay":true,"startMs":1700006400000,"endMs":1700092800000}]`), nil
	}}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	done := make(chan []calendar.Event, 1)
	go func() {
		es, _ := calendar.Events(time.Now(), time.Now().Add(time.Hour))
		done <- es
	}()
	var es []calendar.Event
	for range 5000 { // a safety net, not what passes
		h.RunPosted()
		r.Flush()
		select {
		case es = <-done:
		default:
			time.Sleep(time.Millisecond)
			continue
		}
		break
	}
	if len(es) != 2 || !es[0].Start.Equal(time.UnixMilli(1700000000000)) || es[0].End.Sub(es[0].Start) != time.Hour {
		t.Fatalf("%+v", es)
	}
	if !es[1].AllDay || es[1].Start.Location() != time.UTC || es[1].Start.Hour() != 0 {
		t.Fatalf("all day: %+v", es[1])
	}
}
