package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/egoist/ryui/store"
)

// Window is a quota that resets: Claude's 5-hour window, its week, its
// week for one model.
type Window struct {
	Key     string        // which: five_hour, seven_day, seven_day_fable
	Name    string        // the server's name for it; empty: Label(Key)
	Span    time.Duration // how long it runs; 0: Period(Key)
	Used    float64       // percent of the quota used, 0 to 100
	Resets  time.Time     // zero: not started (nothing used yet)
	History []Sample      // what was used, as the window went
}

// Title is the window's name.
func (w Window) Title() string {
	if w.Name != "" {
		return w.Name
	}
	return Label(w.Key)
}

// Period is how long the window runs.
func (w Window) Period() time.Duration {
	if w.Span > 0 {
		return w.Span
	}
	return Period(w.Key)
}

// Sample is how much of a window was used at a time.
type Sample struct {
	At   time.Time
	Used float64
}

// Account is a subscription whose quotas the app follows.
type Account struct {
	ID         string
	Service    string // "claude"
	Name       string // the user's name for it; empty: the service's
	Demo       bool   // made-up numbers, for trying the app
	Email, Org string
	Extra      bool // extra usage (pay as you go past the quota) is on
	Windows    []Window
	Hidden     []string // windows not shown on the home screen
	Updated    time.Time
	Error      string // why the last refresh failed
}

func (a Account) Title() string {
	if a.Name != "" {
		return a.Name
	}
	return services[a.Service].Name
}

// Shown is whether the window shows on the home screen's card.
func (a Account) Shown(key string) bool { return !slices.Contains(a.Hidden, key) }

// Period is how long a window runs, from its key.
func Period(key string) time.Duration {
	if strings.HasPrefix(key, "five_hour") {
		return 5 * time.Hour
	}
	return 7 * 24 * time.Hour
}

// Label names a window: "Current 5 hours", "This week", "This week · Opus".
func Label(key string) string {
	switch {
	case key == "five_hour":
		return "Current 5 hours"
	case key == "seven_day":
		return "This week"
	case strings.HasPrefix(key, "seven_day_"):
		m := strings.ReplaceAll(strings.TrimPrefix(key, "seven_day_"), "_", " ")
		if m == "oauth apps" {
			m = "apps"
		}
		return "This week · " + strings.ToUpper(m[:1]) + m[1:]
	}
	return key
}

// Pace is how a window's use is going, at the rate it has gone so far.
type Pace int

const (
	PaceUnknown Pace = iota // too early to tell
	PaceOK                  // ends with a fifth or more left
	PaceTight               // ends with little left
	PaceOver                // runs out before it resets
	PaceUsedUp              // all used, until it resets
)

func (p Pace) String() string {
	return [...]string{"Too early", "On track", "Tight", "Runs out", "Used up"}[p]
}

// Forecast is where a window will be when it resets.
type Forecast struct {
	Pace Pace
	// AtReset is the percent used when it resets, at the average rate
	// since it started; over 100 when it runs out first.
	AtReset float64
	// Out is when it runs out, for PaceOver.
	Out time.Time
}

// Forecast projects w's use to its reset from its average rate since it
// started: busy and quiet hours both change it.
func (w Window) Forecast(now time.Time) Forecast {
	period := w.Period()
	if w.Used >= 100 {
		return Forecast{Pace: PaceUsedUp, AtReset: w.Used}
	}
	if w.Resets.IsZero() || !now.Before(w.Resets) {
		return Forecast{AtReset: w.Used}
	}
	elapsed := now.Sub(w.Resets.Add(-period))
	// Under a twentieth of the window in, a rate says little.
	if elapsed < period/20 || w.Used <= 0 {
		return Forecast{AtReset: w.Used}
	}
	rate := w.Used / elapsed.Hours() // percent an hour
	at := w.Used + rate*w.Resets.Sub(now).Hours()
	f := Forecast{AtReset: at, Pace: PaceOK}
	switch {
	case at > 100:
		f.Pace = PaceOver
		f.Out = now.Add(time.Duration((100 - w.Used) / rate * float64(time.Hour)))
	case at > 80:
		f.Pace = PaceTight
	}
	return f
}

// service is a provider the app knows how to read.
type service struct {
	Name   string
	Manage string // where the user manages the plan
	// Refresh reads an account's usage with its secret (the keychain's),
	// returning the secret to keep: the same, or refreshed tokens.
	Refresh func(a *Account, secret string) (string, error)
}

var services = map[string]service{
	"claude": {Name: "Claude", Manage: "https://claude.ai/settings/usage", Refresh: refreshClaude},
}

var client = &http.Client{Timeout: 20 * time.Second}

// refreshClaude reads the usage Claude Code shows in /usage, with the
// account's OAuth tokens: refreshed first when they're about to expire,
// or when the API refuses them.
func refreshClaude(a *Account, secret string) (string, error) {
	t := parseTokens(secret)
	if t.stale(time.Now()) {
		fresh, err := refreshed(t)
		if err != nil {
			return secret, signedOut(err)
		}
		t = fresh
	}
	err := readClaude(a, t.Access)
	if unauthorized(err) && t.Refresh != "" {
		fresh, rerr := refreshed(t)
		if rerr != nil {
			return secret, signedOut(rerr)
		}
		t = fresh
		err = readClaude(a, t.Access)
	}
	return t.String(), err
}

// signedOut is a refresh that failed: the user signs in again.
func signedOut(err error) error {
	return fmt.Errorf("signed out (%v): sign in again", err)
}

// readClaude reads the account's usage and profile.
func readClaude(a *Account, token string) error {
	var usage map[string]json.RawMessage
	if err := getJSON("https://api.anthropic.com/api/oauth/usage", token, &usage); err != nil {
		return err
	}
	// The last reply, as the server sent it: to see what it says when a
	// number looks wrong. It holds no secrets, only the percentages.
	if b, err := json.MarshalIndent(usage, "", "  "); err == nil {
		os.WriteFile(filepath.Join(store.Dir(), "usage-"+a.ID+".json"), b, 0o600)
	}
	ws, extra, err := parseUsage(usage)
	if err != nil {
		return err
	}
	a.Windows = merge(a.Windows, ws, time.Now())
	if extra != nil {
		a.Extra = *extra
	}
	var profile struct {
		Account struct {
			Email string `json:"email"`
		} `json:"account"`
		Organization struct {
			Name string `json:"name"`
		} `json:"organization"`
	}
	// The profile is extra: without it the numbers still show.
	if getJSON("https://api.anthropic.com/api/oauth/profile", token, &profile) == nil {
		a.Email, a.Org = profile.Account.Email, profile.Organization.Name
	}
	return nil
}

func getJSON(url, token string, v any) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return apiError(res)
	}
	return json.NewDecoder(res.Body).Decode(v)
}

// httpError is a refused request: its status, and what the server said.
type httpError struct {
	Status int
	Msg    string
}

func (e *httpError) Error() string { return e.Msg }

// apiError is what a refused request says: the API's message
// ({"error": {"type": "authentication_error", "message": "..."}}), or
// OAuth's ({"error": "invalid_grant", "error_description": "..."}).
func apiError(res *http.Response) error {
	var body struct {
		Error       json.RawMessage `json:"error"`
		Description string          `json:"error_description"`
	}
	json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body)
	var api struct{ Message string }
	var code string
	msg := body.Description
	if json.Unmarshal(body.Error, &api) == nil && api.Message != "" {
		msg = api.Message
	} else if json.Unmarshal(body.Error, &code) == nil && msg == "" {
		msg = code
	}
	if msg == "" {
		msg = res.Status
	}
	switch {
	case strings.Contains(msg, "scope"):
		// A token for inference alone (claude setup-token's) can't read
		// the account's usage.
		msg += " (this token can't read usage: sign in with Claude)"
	case res.StatusCode == http.StatusUnauthorized:
		msg = strings.TrimSuffix(msg, ".") + ". Sign in again."
	}
	return &httpError{Status: res.StatusCode, Msg: msg}
}

// cleanToken is a pasted token without the spaces and line breaks a
// terminal's wrapping puts in it.
func cleanToken(t string) string { return strings.Join(strings.Fields(t), "") }

// parseUsage reads the usage endpoint's windows, and whether extra
// usage is on (nil: it doesn't say). The server's list of meters,
// "limits", names them: a row's kind says what it is ("session",
// "weekly_all", "weekly_scoped"), its scope which model a scoped one is
// for. Without it, the windows Claude Code knows by key ({"five_hour":
// {"utilization": 21, "resets_at": "..."}, ...}). Other keys are the
// server's own, under codenames ("iguana_necktie", a dollar budget):
// Claude Code doesn't show them, nor does this.
func parseUsage(usage map[string]json.RawMessage) ([]Window, *bool, error) {
	var extra *bool
	var e struct {
		Enabled *bool `json:"is_enabled"`
	}
	if json.Unmarshal(usage["extra_usage"], &e) == nil {
		extra = e.Enabled
	}
	ws := parseLimits(usage["limits"])
	if len(ws) == 0 {
		for _, key := range []string{"five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet"} {
			var w struct {
				Utilization *float64 `json:"utilization"`
				ResetsAt    *string  `json:"resets_at"`
			}
			if json.Unmarshal(usage[key], &w) != nil || w.Utilization == nil {
				continue
			}
			win := Window{Key: key, Used: *w.Utilization}
			if w.ResetsAt != nil {
				win.Resets, _ = time.Parse(time.RFC3339Nano, *w.ResetsAt)
			}
			ws = append(ws, win)
		}
	}
	if len(ws) == 0 {
		return nil, extra, errors.New("no usage in the reply")
	}
	sortWindows(ws)
	return ws, extra, nil
}

// parseLimits reads the server's meters: [{"kind": "weekly_scoped",
// "group": "weekly", "percent": 0, "resets_at": "...", "scope":
// {"model": {"display_name": "Fable"}}}, ...].
func parseLimits(raw json.RawMessage) []Window {
	var rows []struct {
		Kind     string   `json:"kind"`
		Group    string   `json:"group"`
		Percent  *float64 `json:"percent"`
		ResetsAt *string  `json:"resets_at"`
		Scope    *struct {
			Model, Surface *struct {
				DisplayName string `json:"display_name"`
			}
		} `json:"scope"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &rows) != nil {
		return nil
	}
	var ws []Window
	for _, r := range rows {
		if r.Percent == nil {
			continue
		}
		scope := ""
		if r.Scope != nil && r.Scope.Model != nil {
			scope = r.Scope.Model.DisplayName
		} else if r.Scope != nil && r.Scope.Surface != nil {
			scope = r.Scope.Surface.DisplayName
		}
		w := Window{Used: *r.Percent}
		switch {
		case r.Kind == "session":
			w.Key = "five_hour"
		case r.Kind == "weekly_all":
			w.Key = "seven_day"
		case scope != "":
			// A model's week, or a surface's: named as the server names it.
			w.Key = "seven_day_" + slug(scope)
			w.Name = "This week · " + scope
		default:
			w.Key, w.Name = r.Kind, upper(strings.ReplaceAll(r.Kind, "_", " "))
		}
		switch {
		case r.Group == "session" || r.Kind == "session":
			w.Span = 5 * time.Hour
		default:
			w.Span = 7 * 24 * time.Hour
		}
		if r.ResetsAt != nil {
			w.Resets, _ = time.Parse(time.RFC3339Nano, *r.ResetsAt)
		}
		ws = append(ws, w)
	}
	return ws
}

// slug is a name as a key: "Fable 5" is "fable_5".
func slug(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), "_")
}

// sortWindows puts the shortest first, then the week, then the models'.
func sortWindows(ws []Window) {
	sort.Slice(ws, func(i, j int) bool {
		pi, pj := ws[i].Period(), ws[j].Period()
		if pi != pj {
			return pi < pj
		}
		if (ws[i].Key == "seven_day") != (ws[j].Key == "seven_day") {
			return ws[i].Key == "seven_day"
		}
		return ws[i].Key < ws[j].Key
	})
}

// merge takes fresh windows and keeps each one's history, while it's the
// same window (the same reset); a sample is kept a quarter hour apart at
// most, and when the use changed.
func merge(old, fresh []Window, now time.Time) []Window {
	for i := range fresh {
		w := &fresh[i]
		for _, o := range old {
			if o.Key == w.Key && o.Resets.Equal(w.Resets) {
				w.History = o.History
			}
		}
		h := w.History
		if n := len(h); n == 0 || h[n-1].Used != w.Used || now.Sub(h[n-1].At) > 15*time.Minute {
			w.History = append(slices.Clip(h), Sample{At: now, Used: w.Used})
		}
		if n := len(w.History); n > 300 {
			w.History = w.History[n-300:]
		}
	}
	return fresh
}

// demo makes an account with numbers like a week's real ones.
func demo(now time.Time) Account {
	week := now.Add(-29 * time.Hour).Truncate(time.Hour)
	five := now.Add(-2*time.Hour - 20*time.Minute).Truncate(time.Minute)
	ws := []Window{
		{Key: "five_hour", Used: 21, Resets: five.Add(5 * time.Hour)},
		{Key: "seven_day", Used: 15, Resets: week.Add(7 * 24 * time.Hour)},
		{Key: "seven_day_opus", Used: 0, Resets: week.Add(7 * 24 * time.Hour)},
	}
	// A week's start: busy afternoons, quiet nights.
	for t, used := week, 0.0; t.Before(now); t = t.Add(time.Hour) {
		if h := t.Hour(); h >= 9 && h <= 22 {
			used += 0.6 + rand.Float64()*0.5
		}
		ws[1].History = append(ws[1].History, Sample{At: t, Used: math.Min(used, 15)})
	}
	ws[1].History = append(ws[1].History, Sample{At: now, Used: 15})
	ws[0].History = []Sample{{five, 0}, {five.Add(time.Hour), 9}, {now, 21}}
	return Account{
		ID: fmt.Sprintf("demo-%d", now.UnixNano()), Service: "claude", Demo: true,
		Email: "you@example.com", Org: "Personal", Windows: ws, Hidden: []string{"five_hour", "seven_day_opus"}, Updated: now,
	}
}

// refreshDemo moves a demo account along: some use since it was last
// refreshed, and windows that reset start again.
func refreshDemo(a *Account, now time.Time) {
	hours := now.Sub(a.Updated).Hours()
	fresh := make([]Window, len(a.Windows))
	for i, w := range a.Windows {
		for !w.Resets.IsZero() && !now.Before(w.Resets) {
			w = Window{Key: w.Key, Name: w.Name, Span: w.Span, Resets: w.Resets.Add(w.Period())}
		}
		if w.Key != "seven_day_opus" {
			step := math.Min(hours, 2) * (0.5 + rand.Float64()) * 100 / w.Period().Hours() * 1.6
			w.Used = math.Min(100, math.Round((w.Used+step+rand.Float64())*10)/10)
		}
		w.History = slices.Clone(w.History)
		fresh[i] = w
	}
	a.Windows = merge(a.Windows, fresh, now)
}
