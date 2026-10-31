package imageedit

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRequest(t *testing.T) {
	var r map[string]any
	json.Unmarshal(request("/tmp/a.heic", []Action{Resize(1080, 0), Crop(1, 2, 3, 4), Rotate(90), FlipHorizontal()}, Options{}, 1500*time.Millisecond), &r)
	if r["source"] != "/tmp/a.heic" || r["format"] != "jpeg" || r["quality"] != 0.9 || r["at"] != 1500.0 {
		t.Fatalf("%v", r)
	}
	as := r["actions"].([]any)
	if len(as) != 4 || as[0].(map[string]any)["width"] != 1080.0 || as[1].(map[string]any)["y"] != 2.0 ||
		as[2].(map[string]any)["degrees"] != 90.0 || as[3].(map[string]any)["op"] != "flipH" {
		t.Fatalf("actions %v", as)
	}
	json.Unmarshal(request("x.png", nil, Options{Format: PNG, Quality: 0.5}, 0), &r)
	if r["format"] != "png" || r["quality"] != 0.5 {
		t.Fatalf("%v", r)
	}
}
