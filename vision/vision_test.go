package vision

import (
	"encoding/json"
	"testing"
)

func TestRequest(t *testing.T) {
	var r map[string]any
	json.Unmarshal(request("/a.png", Options{NoFaces: true, Languages: []string{"zh-Hans"}}), &r)
	if r["path"] != "/a.png" || r["text"] != true || r["faces"] != false || r["codes"] != true || r["languages"].([]any)[0] != "zh-Hans" {
		t.Fatalf("%v", r)
	}
}

func TestDecode(t *testing.T) {
	var r Result
	err := json.Unmarshal([]byte(`{"width":10,"height":20,"lines":[{"text":"a b","box":{"x":0.1,"y":0.2,"w":0.3,"h":0.4},
		"words":[{"text":"b","box":{"x":0.2,"y":0.2,"w":0.1,"h":0.4},"start":2,"end":3}]}],
		"faces":[{"x":0,"y":0,"w":1,"h":1}],"codes":[{"kind":"qr","value":"v","box":{"x":0,"y":0,"w":0.5,"h":0.5}}]}`), &r)
	if err != nil {
		t.Fatal(err)
	}
	if r.Width != 10 || r.Lines[0].Words[0].Start != 2 || r.Lines[0].Box.W != 0.3 || len(r.Faces) != 1 || r.Codes[0].Kind != "qr" {
		t.Fatalf("%+v", r)
	}
}

func TestUnion(t *testing.T) {
	u := Box{0, 0, 1, 1}.Union(Box{2, 3, 1, 1})
	if u != (Box{0, 0, 3, 4}) {
		t.Fatalf("%+v", u)
	}
	if got := (Box{}).Union(Box{1, 1, 1, 1}); got != (Box{1, 1, 1, 1}) {
		t.Fatalf("%+v", got)
	}
	if got := (Box{1, 1, 1, 1}).Union(Box{}); got != (Box{1, 1, 1, 1}) {
		t.Fatalf("%+v", got)
	}
}
