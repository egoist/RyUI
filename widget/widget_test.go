package widget

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/egoist/ryui/ui"
)

// A timeline goes sorted by time, its times in ms and its tint as #rrggbbaa.
func TestEncode(t *testing.T) {
	at := time.UnixMilli(2_000_000)
	b := encode("today", []Entry{
		{Title: "later", At: at},
		{Title: "now", Value: "3", Tint: ui.Hex(0x336699), Until: time.UnixMilli(5_000), ShowProgress: true, Progress: 0.5},
	})
	var got struct {
		Kind    string           `json:"kind"`
		Entries []map[string]any `json:"entries"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Kind != "today" || len(got.Entries) != 2 {
		t.Fatalf("%s", b)
	}
	now, later := got.Entries[0], got.Entries[1]
	if now["title"] != "now" || now["tint"] != "#336699ff" || now["untilMs"] != 5000.0 || now["progress"] != 0.5 || now["showProgress"] != true {
		t.Errorf("now: %v", now)
	}
	if later["title"] != "later" || later["atMs"] != 2_000_000.0 || later["tint"] != nil {
		t.Errorf("later: %v", later)
	}
	if b := encode("x", nil); string(b) != `{"entries":[],"kind":"x"}` {
		t.Errorf("clear: %s", b)
	}
}
