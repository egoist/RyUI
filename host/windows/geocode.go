//go:build windows

package windows

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/asuka/internal/boot"
)

// Place names on Windows: OpenStreetMap's Nominatim, as GNOME's
// geocode-glib asks it (Windows's own geocoder, Windows.Services.Maps,
// needs a Bing Maps key an app would have to get). Its usage policy:
// one request at a time and no more than one a second, a User-Agent that
// names the application, results cached. Both ways: a place's name
// (location's "name", reverse), and the places a name is (forward, the
// module's "places"). The names are in the user's languages (the app's
// first).

// nominatimURL is the server (ASUKA_NOMINATIM: another, tests' or one of
// the app's own).
func nominatimURL() string {
	if u := os.Getenv("ASUKA_NOMINATIM"); u != "" {
		return strings.TrimSuffix(u, "/")
	}
	return "https://nominatim.openstreetmap.org"
}

// geocoder asks Nominatim, a request at a time, a second apart at
// least, and keeps what it answered (in memory, and in the caches'
// asuka-geocode.json across launches).
type geocoder struct {
	mu     sync.Mutex // held for a request: one at a time
	last   time.Time  // when the last request went
	cache  map[string]string
	loaded bool
	path   string
	agent  string
	client *http.Client
	// sleep and now are time's (tests count instead of waiting).
	sleep func(time.Duration)
	now   func() time.Time
	// requests counts what was sent.
	requests int
}

var geo = &geocoder{client: &http.Client{Timeout: 20 * time.Second}, sleep: time.Sleep, now: time.Now}

// geocodeCacheMax is how many answers are kept.
const geocodeCacheMax = 500

func (h *Host) geocoder() *geocoder {
	g := geo
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.agent == "" {
		app := boot.ID
		if app == "" {
			app = h.appUserModelID()
		}
		g.agent = "Asuka/1 (" + app + "; Windows; +https://github.com/egoist/asuka)"
	}
	if g.path == "" {
		g.path = filepath.Join(h.cacheDir(), "asuka-geocode.json")
	}
	return g
}

// get asks the server for path?query (JSON), or answers from the cache.
// Off the main thread.
func (g *geocoder) get(path string, q url.Values, lang string) (string, error) {
	if lang != "" {
		q.Set("accept-language", lang)
	}
	q.Set("format", "jsonv2")
	key := path + "?" + q.Encode()
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.loaded {
		g.loaded = true
		g.cache = map[string]string{}
		if b, err := os.ReadFile(g.path); err == nil {
			json.Unmarshal(b, &g.cache)
		}
	}
	if v, ok := g.cache[key]; ok {
		return v, nil
	}
	if wait := time.Second - g.now().Sub(g.last); !g.last.IsZero() && wait > 0 {
		g.sleep(wait)
	}
	g.last = g.now()
	g.requests++
	req, err := http.NewRequest("GET", nominatimURL()+path+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", g.agent)
	if lang != "" {
		req.Header.Set("Accept-Language", lang)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("location: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("location: the geocoder: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("location: %w", err)
	}
	v := string(body)
	if len(g.cache) >= geocodeCacheMax {
		for k := range g.cache { // any: they're all as good
			delete(g.cache, k)
			if len(g.cache) < geocodeCacheMax*3/4 {
				break
			}
		}
	}
	g.cache[key] = v
	if g.path != "" && os.MkdirAll(filepath.Dir(g.path), 0o755) == nil {
		if out, err := json.Marshal(g.cache); err == nil {
			os.WriteFile(g.path, out, 0o644)
		}
	}
	return v, nil
}

// nominatimPlace is a place Nominatim answers.
type nominatimPlace struct {
	Lat         string            `json:"lat"`
	Lon         string            `json:"lon"`
	Name        string            `json:"name"`
	DisplayName string            `json:"display_name"`
	Address     map[string]string `json:"address"`
	Error       string            `json:"error"`
}

// placeName is a place's name as Core Location's is made: its area (or
// name), then its town (or state, or country).
func placeName(p nominatimPlace) string {
	a := p.Address
	var parts []string
	seen := map[string]bool{}
	for _, keys := range [][]string{
		{"neighbourhood", "suburb", "quarter", "city_district", "borough", "hamlet", "\x00name"},
		{"city", "town", "village", "municipality", "county", "state", "country"},
	} {
		for _, k := range keys {
			v := a[k]
			if k == "\x00name" {
				v = p.Name
			}
			if v != "" {
				if !seen[v] {
					seen[v] = true
					parts = append(parts, v)
				}
				break
			}
		}
	}
	return strings.Join(parts, ", ")
}

// reverse is the name of the place at lat, lon. Off the main thread.
func (g *geocoder) reverse(lat, lon float64, lang string) (string, error) {
	q := url.Values{}
	// About a meter: what's asked again of the same spot is the cache's.
	q.Set("lat", strconv.FormatFloat(lat, 'f', 5, 64))
	q.Set("lon", strconv.FormatFloat(lon, 'f', 5, 64))
	q.Set("zoom", "18")
	q.Set("addressdetails", "1")
	b, err := g.get("/reverse", q, lang)
	if err != nil {
		return "", err
	}
	var p nominatimPlace
	if err := json.Unmarshal([]byte(b), &p); err != nil {
		return "", errors.New("location: the geocoder's answer is bad")
	}
	if p.Error != "" {
		return "", errors.New("location: no name for this place")
	}
	name := placeName(p)
	if name == "" {
		return "", errors.New("location: no name for this place")
	}
	return name, nil
}

// geocodedPlace is a place a name is.
type geocodedPlace struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Name string  `json:"name"`
}

// forward is the places a name or an address is, the likeliest first
// (five at most). Off the main thread.
func (g *geocoder) forward(text, lang string) ([]geocodedPlace, error) {
	q := url.Values{}
	q.Set("q", strings.TrimSpace(text))
	q.Set("limit", "5")
	q.Set("addressdetails", "1")
	b, err := g.get("/search", q, lang)
	if err != nil {
		return nil, err
	}
	var ps []nominatimPlace
	if err := json.Unmarshal([]byte(b), &ps); err != nil {
		return nil, errors.New("location: the geocoder's answer is bad")
	}
	out := []geocodedPlace{}
	for _, p := range ps {
		lat, err1 := strconv.ParseFloat(p.Lat, 64)
		lon, err2 := strconv.ParseFloat(p.Lon, 64)
		if err1 != nil || err2 != nil {
			continue
		}
		name := p.DisplayName
		if n := placeName(p); n != "" {
			name = n
		}
		out = append(out, geocodedPlace{Lat: lat, Lon: lon, Name: name})
	}
	return out, nil
}

// placeNameMethod is location's "name" (args JSON: lat, lon): the place's
// name.
func (h *Host) placeNameMethod(args []byte, reply func([]byte, error)) {
	var o struct{ Lat, Lon float64 }
	if err := json.Unmarshal(args, &o); err != nil {
		reply(nil, errors.New("location.name: bad arguments"))
		return
	}
	g, lang := h.geocoder(), h.env.Languages
	h.offMain(func() ([]byte, error) {
		name, err := g.reverse(o.Lat, o.Lon, lang)
		return []byte(name), err
	}, reply)
}

// placesMethod is location's "places" (args: the text): JSON, the places
// it names, each's lat, lon and name.
func (h *Host) placesMethod(args []byte, reply func([]byte, error)) {
	if strings.TrimSpace(string(args)) == "" {
		reply([]byte("[]"), nil)
		return
	}
	g, lang := h.geocoder(), h.env.Languages
	text := string(args)
	h.offMain(func() ([]byte, error) {
		ps, err := g.forward(text, lang)
		if err != nil {
			return nil, err
		}
		return json.Marshal(ps)
	}, reply)
}
