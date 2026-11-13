//go:build windows

package windows

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/asuka/ui"
)

// Place names come from Nominatim: the request names Asuka and asks in
// the user's languages, a second after the last at least (counted, not
// waited for), one at a time; an answer is kept, so the same place
// isn't asked again. "places" is the other way.
func TestPlaceNames(t *testing.T) {
	var mu sync.Mutex
	var agents, langs, paths []string
	inFlight, most := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		agents, langs, paths = append(agents, r.UserAgent()), append(langs, r.URL.Query().Get("accept-language")), append(paths, r.URL.Path)
		inFlight++
		most = max(most, inFlight)
		mu.Unlock()
		time.Sleep(20 * time.Millisecond) // overlapping requests would be seen
		mu.Lock()
		inFlight--
		mu.Unlock()
		switch r.URL.Path {
		case "/reverse":
			if r.URL.Query().Get("lat") == "0.00000" {
				w.Write([]byte(`{"error":"Unable to geocode"}`))
				return
			}
			w.Write([]byte(`{"name":"Ferry Building","display_name":"Ferry Building, San Francisco, California, United States",
				"address":{"tourism":"Ferry Building","neighbourhood":"Financial District","city":"San Francisco","state":"California","country":"United States"}}`))
		case "/search":
			w.Write([]byte(`[{"lat":"37.7955","lon":"-122.3937","name":"Ferry Building","display_name":"Ferry Building, San Francisco",
				"address":{"city":"San Francisco"}},{"lat":"x","lon":"y"}]`))
		}
	}))
	defer srv.Close()
	t.Setenv("ASUKA_NOMINATIM", srv.URL)
	waited := time.Duration(0)
	saved := geo
	geo = &geocoder{client: srv.Client(), sleep: func(d time.Duration) { waited += d }, now: time.Now, path: filepath.Join(t.TempDir(), "asuka-geocode.json")}
	defer func() { geo = saved }()
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		h.env.Languages = "fr-FR,en-US"
		args, _ := json.Marshal(map[string]float64{"lat": 37.7955, "lon": -122.3937})
		// Two at once: one goes, then the other (the cache's).
		var names []string
		for range 2 {
			h.asyncModule("location")["name"](args, func(b []byte, err error) {
				if err != nil {
					fatalf("location.name: %v", err)
				}
				names = append(names, string(b))
			})
		}
		pumpUntil(func() bool { return len(names) == 2 })
		if names[0] != "Financial District, San Francisco" || names[1] != names[0] {
			fatalf("names: %q", names)
		}
		far, _ := json.Marshal(map[string]float64{"lat": 0, "lon": 0})
		if _, err, _ := await(h, "location", "name", far); err == nil || !strings.Contains(err.Error(), "no name") {
			fatalf("the sea's name: %v", err)
		}
		b, err, _ := await(h, "location", "places", []byte("ferry building"))
		var ps []geocodedPlace
		if err != nil || json.Unmarshal(b, &ps) != nil || len(ps) != 1 || ps[0].Name != "Ferry Building, San Francisco" || ps[0].Lat != 37.7955 {
			fatalf("location.places: %s, %v", b, err)
		}
	})
	mu.Lock()
	defer mu.Unlock()
	if len(paths) != 3 || geo.requests != 3 {
		t.Fatalf("requests: %v (%d)", paths, geo.requests)
	}
	if most != 1 {
		t.Fatalf("%d requests at once", most)
	}
	if waited < time.Second { // two waits after the first, nearly a second each
		t.Fatalf("waited %v between requests", waited)
	}
	for i := range agents {
		if !strings.HasPrefix(agents[i], "Asuka/") || langs[i] != "fr-FR,en-US" {
			t.Fatalf("request %d: agent %q, languages %q", i, agents[i], langs[i])
		}
	}
	if b, err := os.ReadFile(geo.path); err != nil || !strings.Contains(string(b), "Financial District") {
		t.Fatalf("the cache's file: %s, %v", b, err)
	}
}

// One real request, when asked for (ASUKA_NET=1): Nominatim names the
// Ferry Building's neighborhood.
func TestPlaceNameNominatim(t *testing.T) {
	if os.Getenv("ASUKA_NET") == "" {
		t.Skip("ASUKA_NET=1: asks openstreetmap.org")
	}
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		args, _ := json.Marshal(map[string]float64{"lat": 37.7955, "lon": -122.3937})
		b, err, _ := await(h, "location", "name", args)
		t.Logf("name: %q, %v", b, err)
		if err != nil || !strings.Contains(string(b), "San Francisco") {
			fatalf("location.name: %q, %v", b, err)
		}
	})
}
