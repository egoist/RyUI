package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Signing in with Claude is OAuth 2 with PKCE, as Claude Code signs in,
// with its client. Its sign-in redirects to a server of the app's on the
// loopback interface (http://localhost:port/callback, RFC 8252), or to a
// page that shows the code to paste (manualRedirect). It asks only to
// read the account's profile and usage (user:profile), not to use it.
const (
	oauthClient    = "9d1c250a-e61b-44d9-88ed-5944d1962f5e" // Claude Code's
	authorizeURL   = "https://claude.com/cai/oauth/authorize"
	manualRedirect = "https://platform.claude.com/oauth/code/callback"
	tokenURL       = "https://platform.claude.com/v1/oauth/token"
	oauthScope     = "user:profile"
)

// tokens are an account's, kept in the keychain as JSON. A token pasted
// before sign-in existed is kept as itself: an access token alone.
type tokens struct {
	Access  string    `json:"access"`
	Refresh string    `json:"refresh,omitempty"`
	Expires time.Time `json:"expires,omitzero"`
}

func parseTokens(s string) tokens {
	var t tokens
	if json.Unmarshal([]byte(s), &t) == nil && t.Access != "" {
		return t
	}
	return tokens{Access: s}
}

func (t tokens) String() string {
	b, _ := json.Marshal(t)
	return string(b)
}

// stale is whether the access token has expired, or is about to.
func (t tokens) stale(now time.Time) bool {
	return t.Refresh != "" && !t.Expires.IsZero() && now.Add(5*time.Minute).After(t.Expires)
}

// login is a sign-in under way: its PKCE verifier, its state, and where
// it redirects to.
type login struct{ verifier, state, redirect string }

func newLogin(redirect string) login {
	return login{verifier: random(), state: random(), redirect: redirect}
}

func random() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// URL is the page the user signs in on.
func (l login) URL() string {
	sum := sha256.Sum256([]byte(l.verifier))
	q := url.Values{
		"code":                  {"true"},
		"client_id":             {oauthClient},
		"response_type":         {"code"},
		"redirect_uri":          {l.redirect},
		"scope":                 {oauthScope},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
		"state":                 {l.state},
	}
	return authorizeURL + "?" + q.Encode()
}

// codeFrom finds the code in where the sign-in ended: the redirect's
// URL (…/callback?code=…&state=…), or what its page shows to paste
// ("code#state"). ok is false when there's none, or it's another
// sign-in's: its state is this one's.
func (l login) codeFrom(s string) (code string, ok bool) {
	s = strings.TrimSpace(s)
	state := ""
	if strings.HasPrefix(s, l.redirect) {
		u, err := url.Parse(s)
		if err != nil {
			return "", false
		}
		code, state = u.Query().Get("code"), u.Query().Get("state")
	} else {
		code, state, _ = strings.Cut(cleanToken(s), "#")
	}
	if code == "" || state != l.state {
		return "", false
	}
	return code, true
}

// signedIn is what a sign-in gets: the tokens, and whose they are.
type signedIn struct {
	tokens
	Email, Org string
}

// exchange trades the code for tokens. Not on the main thread.
func (l login) exchange(code string) (signedIn, error) {
	var r tokenReply
	err := postJSON(tokenURL, map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"redirect_uri":  l.redirect,
		"client_id":     oauthClient,
		"code_verifier": l.verifier,
		"state":         l.state,
	}, &r)
	if err != nil {
		return signedIn{}, err
	}
	return signedIn{tokens: r.tokens(""), Email: r.Account.Email, Org: r.Organization.Name}, nil
}

// refreshed is t with a new access token. Not on the main thread.
func refreshed(t tokens) (tokens, error) {
	var r tokenReply
	err := postJSON(tokenURL, map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": t.Refresh,
		"client_id":     oauthClient,
		"scope":         oauthScope,
	}, &r)
	if err != nil {
		return t, err
	}
	return r.tokens(t.Refresh), nil
}

type tokenReply struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"` // seconds
	Account      struct {
		Email string `json:"email_address"`
	} `json:"account"`
	Organization struct {
		Name string `json:"name"`
	} `json:"organization"`
}

// tokens are the reply's; a refresh may keep the refresh token it had.
func (r tokenReply) tokens(refresh string) tokens {
	t := tokens{Access: r.AccessToken, Refresh: r.RefreshToken}
	if t.Refresh == "" {
		t.Refresh = refresh
	}
	if r.ExpiresIn > 0 {
		t.Expires = time.Now().Add(time.Duration(r.ExpiresIn) * time.Second)
	}
	return t
}

func postJSON(url string, body any, v any) error {
	b, _ := json.Marshal(body)
	res, err := client.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return apiError(res)
	}
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		return err
	}
	return nil
}

// unauthorized is whether err is the API refusing the token.
func unauthorized(err error) bool {
	var e *httpError
	return errors.As(err, &e) && e.Status == http.StatusUnauthorized
}

// loopback is a server on localhost that the sign-in redirects to: it
// takes the code, and tells the browser to go back to the app.
type loopback struct {
	URL   string      // the redirect: http://localhost:port/callback
	codes chan string // what it redirected to, with the code
	done  chan struct{}
	once  sync.Once
	srv   *http.Server
}

// listen starts a loopback server on a free port, on IPv4's and, where
// it can, IPv6's loopback (a browser may try either for localhost).
func listen() (*loopback, error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	l := &loopback{URL: fmt.Sprintf("http://localhost:%d/callback", port), codes: make(chan string, 1), done: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		select {
		case l.codes <- l.URL + "?" + r.URL.RawQuery:
		default:
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><meta name="viewport" content="width=device-width"><body style="font:17px -apple-system,system-ui,sans-serif;text-align:center;padding:64px 24px">Signed in. You can go back to the app.</body>`)
	})
	l.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go l.srv.Serve(ln)
	if ln6, err := net.Listen("tcp6", fmt.Sprintf("[::1]:%d", port)); err == nil {
		go l.srv.Serve(ln6)
	}
	return l, nil
}

// Close stops the server; done closes.
func (l *loopback) Close() {
	l.once.Do(func() {
		l.srv.Close()
		close(l.done)
	})
}
