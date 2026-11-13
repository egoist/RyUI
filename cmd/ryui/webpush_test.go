package main

import (
	"strings"
	"testing"
	"testing/fstest"
)

// The app's service worker is its settings, then the host's shared
// notification code, then the worker.
func TestWebWorker(t *testing.T) {
	src := fstest.MapFS{"notify-core.js": {Data: []byte("CORE")}, "sw.js": {Data: []byte("WORKER")}}
	b, err := webWorker(src, map[string]any{"name": "com.example.app", "icon": "icons/icon-192.png"})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	i, j, k := strings.Index(s, `globalThis.ryuiApp = {"icon":"icons/icon-192.png","name":"com.example.app"};`), strings.Index(s, "CORE"), strings.Index(s, "WORKER")
	if i < 0 || j < i || k < j {
		t.Fatalf("worker:\n%s", s)
	}
	if _, err := webWorker(fstest.MapFS{}, nil); err == nil {
		t.Fatal("no host files: no error")
	}
}

// ryui.json's web.vapidPublicKey is checked: a P-256 point, in base64url.
func TestWebVAPIDKey(t *testing.T) {
	const key = "BKpYsAT0ADj2pHQQZeNsXmA3fgij4OZ_7ZqkS63fvpscgjHLZys0UDBluROJdtDaGlE3WW1_IlFyzVzC2LlHyO0"
	app := &goApp{cfg: &appConfig{}}
	if k, err := webVAPIDKey(app); k != "" || err != nil {
		t.Fatalf("none: %q %v", k, err)
	}
	app.cfg.Web.VapidPublicKey = " " + key + "= "
	if k, err := webVAPIDKey(app); k != key || err != nil {
		t.Fatalf("%q %v", k, err)
	}
	for _, bad := range []string{"AAAA", key[:len(key)-4] + "AAAA", "not base64!"} {
		app.cfg.Web.VapidPublicKey = bad
		if _, err := webVAPIDKey(app); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}
