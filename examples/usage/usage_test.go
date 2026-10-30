package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestForecast(t *testing.T) {
	now := time.Date(2026, 10, 31, 0, 58, 0, 0, time.UTC)
	week := Window{Key: "seven_day", Used: 15, Resets: now.Add(-29 * time.Hour).Add(7 * 24 * time.Hour)}
	// 15% in 29 hours: 87% by the reset, 13% left.
	f := week.Forecast(now)
	if f.Pace != PaceTight || math.Round(100-f.AtReset) != 13 {
		t.Fatalf("the week: %+v", f)
	}
	// 15% in 10 hours runs out before the reset, about 57 hours from now.
	fast := Window{Key: "seven_day", Used: 15, Resets: now.Add(-10 * time.Hour).Add(7 * 24 * time.Hour)}
	if f := fast.Forecast(now); f.Pace != PaceOver || f.Out.Sub(now).Round(time.Hour) != 57*time.Hour {
		t.Fatalf("fast: %+v", f)
	}
	// Five minutes into a week says nothing yet.
	early := Window{Key: "seven_day", Used: 2, Resets: now.Add(-5 * time.Minute).Add(7 * 24 * time.Hour)}
	if f := early.Forecast(now); f.Pace != PaceUnknown {
		t.Fatalf("early: %+v", f)
	}
	slow := Window{Key: "five_hour", Used: 10, Resets: now.Add(3 * time.Hour)}
	if f := slow.Forecast(now); f.Pace != PaceOK || math.Round(f.AtReset) != 25 {
		t.Fatalf("slow: %+v", f)
	}
}

func TestParseUsage(t *testing.T) {
	parse := func(body string) ([]Window, *bool) {
		var usage map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &usage); err != nil {
			t.Fatal(err)
		}
		ws, extra, err := parseUsage(usage)
		if err != nil {
			t.Fatal(err)
		}
		return ws, extra
	}
	titles := func(ws []Window) (out []string) {
		for _, w := range ws {
			out = append(out, w.Title())
		}
		return out
	}
	// Without the server's list: the windows Claude Code knows; a
	// codename isn't one.
	ws, extra := parse(`{
		"five_hour": {"utilization": 21.0, "resets_at": "2026-10-31T05:00:00.000+00:00"},
		"seven_day": {"utilization": 15.0, "resets_at": "2026-11-05T12:00:00+00:00"},
		"seven_day_oauth_apps": null,
		"seven_day_opus": {"utilization": 0.0, "resets_at": null},
		"iguana_necktie": {"utilization": 100.0, "resets_at": "2026-11-05T07:59:00+00:00"},
		"extra_usage": {"is_enabled": false}
	}`)
	if got := titles(ws); !slices.Equal(got, []string{"Current 5 hours", "This week", "This week · Opus"}) {
		t.Fatalf("windows %v", got)
	}
	if ws[1].Used != 15 || ws[1].Resets.Day() != 5 || !ws[2].Resets.IsZero() || extra == nil || *extra {
		t.Fatalf("the week %+v, opus %+v, extra %v", ws[1], ws[2], extra)
	}
	// With it: its rows, named by kind and scope. A codename's window
	// (here a dollar budget, all used) isn't Fable's, which is unused.
	ws, _ = parse(`{
		"five_hour": {"utilization": 7.0, "resets_at": "2026-10-31T05:50:00Z"},
		"iguana_necktie": {"utilization": 100.0, "resets_at": "2026-11-05T07:59:00Z", "limit_dollars": 250, "used_dollars": 250.14},
		"limits": [
			{"kind": "session", "group": "session", "percent": 7, "resets_at": "2026-10-31T05:50:00Z", "severity": "normal", "is_active": true},
			{"kind": "weekly_all", "group": "weekly", "percent": 19, "resets_at": "2026-11-05T12:00:00Z", "severity": "normal", "is_active": false},
			{"kind": "weekly_scoped", "group": "weekly", "percent": 0, "resets_at": "2026-11-05T12:00:00Z", "severity": "normal", "is_active": false,
			 "scope": {"model": {"id": null, "display_name": "Fable"}, "surface": null}}
		]
	}`)
	if got := titles(ws); !slices.Equal(got, []string{"Current 5 hours", "This week", "This week · Fable"}) {
		t.Fatalf("limits %v", got)
	}
	if ws[2].Key != "seven_day_fable" || ws[2].Used != 0 || ws[2].Period() != 7*24*time.Hour || ws[0].Period() != 5*time.Hour || ws[1].Used != 19 {
		t.Fatalf("limits %+v", ws)
	}
	if f := (Window{Key: "seven_day", Used: 100}).Forecast(time.Now()); f.Pace != PaceUsedUp {
		t.Fatalf("all used: %+v", f)
	}
	if Label("seven_day_opus") != "This week · Opus" {
		t.Fatal(Label("seven_day_opus"))
	}
}

func TestMergeKeepsHistory(t *testing.T) {
	now := time.Now()
	resets := now.Add(time.Hour)
	old := []Window{{Key: "five_hour", Used: 10, Resets: resets, History: []Sample{{now.Add(-time.Hour), 10}}}}
	ws := merge(old, []Window{{Key: "five_hour", Used: 12, Resets: resets}}, now)
	if len(ws[0].History) != 2 {
		t.Fatalf("history %v", ws[0].History)
	}
	// A new window starts a new history.
	ws = merge(ws, []Window{{Key: "five_hour", Used: 1, Resets: resets.Add(5 * time.Hour)}}, now)
	if len(ws[0].History) != 1 {
		t.Fatalf("after the reset %v", ws[0].History)
	}
}

func TestCleanToken(t *testing.T) {
	if got := cleanToken(" sk-ant-oat01-abc\n  def \r\nghi "); got != "sk-ant-oat01-abcdefghi" {
		t.Fatal(got)
	}
}

func TestAPIError(t *testing.T) {
	res := func(code int, body string) *http.Response {
		return &http.Response{StatusCode: code, Status: http.StatusText(code), Body: io.NopCloser(strings.NewReader(body))}
	}
	if err := apiError(res(401, `{"type":"error","error":{"type":"authentication_error","message":"OAuth access token is invalid."}}`)); err.Error() != "OAuth access token is invalid. Sign in again." {
		t.Fatal(err)
	}
	if err := apiError(res(403, `{"error":{"message":"OAuth token does not meet scope requirement user:profile"}}`)); !strings.Contains(err.Error(), "can't read usage") {
		t.Fatal(err)
	}
	if err := apiError(res(500, `oops`)); err.Error() != "Internal Server Error" {
		t.Fatal(err)
	}
	if err := apiError(res(400, `{"error":"invalid_grant","error_description":"Invalid code"}`)); err.Error() != "Invalid code" {
		t.Fatal(err)
	}
	if !unauthorized(apiError(res(401, `{}`))) || unauthorized(apiError(res(403, `{}`))) {
		t.Fatal("unauthorized")
	}
}

func TestLogin(t *testing.T) {
	l := newLogin(manualRedirect)
	u, err := url.Parse(l.URL())
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	sum := sha256.Sum256([]byte(l.verifier))
	if q.Get("client_id") != oauthClient || q.Get("redirect_uri") != manualRedirect || q.Get("scope") != "user:profile" ||
		q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) || q.Get("state") != l.state || q.Get("code") != "true" {
		t.Fatalf("authorize URL %s", u)
	}
	// The code, from the redirect or pasted from its page.
	if c, ok := l.codeFrom(manualRedirect + "?code=abc&state=" + l.state); !ok || c != "abc" {
		t.Fatal("from the redirect", c, ok)
	}
	if c, ok := l.codeFrom(" abc#" + l.state + "\n"); !ok || c != "abc" {
		t.Fatal("pasted", c, ok)
	}
	if _, ok := l.codeFrom(manualRedirect + "?code=abc&state=another"); ok {
		t.Fatal("another sign-in's code")
	}
	if _, ok := l.codeFrom("https://claude.ai/login"); ok {
		t.Fatal("a page on the way")
	}
}

func TestLoopback(t *testing.T) {
	lb, err := listen()
	if err != nil {
		t.Fatal(err)
	}
	defer lb.Close()
	l := newLogin(lb.URL)
	if !strings.Contains(l.URL(), url.QueryEscape(lb.URL)) {
		t.Fatalf("the redirect isn't the server: %s", l.URL())
	}
	res, err := http.Get(lb.URL + "?code=abc&state=" + l.state)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if code, ok := l.codeFrom(<-lb.codes); !ok || code != "abc" {
		t.Fatal("the code", code, ok)
	}
	lb.Close()
	lb.Close() // twice is fine
	<-lb.done
}

func TestTokens(t *testing.T) {
	if tk := parseTokens("sk-ant-oat01-x"); tk.Access != "sk-ant-oat01-x" || tk.Refresh != "" || tk.stale(time.Now()) {
		t.Fatalf("a pasted token %+v", tk)
	}
	tk := tokens{Access: "a", Refresh: "r", Expires: time.Now().Add(time.Hour)}
	if back := parseTokens(tk.String()); back.Access != "a" || back.Refresh != "r" || !back.Expires.Equal(tk.Expires.Round(0)) {
		t.Fatalf("kept %+v", back)
	}
	if tk.stale(time.Now()) || !tk.stale(time.Now().Add(57*time.Minute)) {
		t.Fatal("stale")
	}
}
