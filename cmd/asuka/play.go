package main

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Google Play's Developer API: an edit with the bundle uploaded (in
// resumable chunks), a release of it on a track, committed.

const playScope = "https://www.googleapis.com/auth/androidpublisher"

// serviceAccount is a Google Cloud service account's JSON key.
type serviceAccount struct {
	Type         string `json:"type"`
	ClientEmail  string `json:"client_email"`
	PrivateKey   string `json:"private_key"`
	PrivateKeyID string `json:"private_key_id"`
	TokenURI     string `json:"token_uri"`
	key          *rsa.PrivateKey
}

const playKeyHelp = `publishing to Google Play takes a service account: in Google Cloud, turn on the Google Play Android Developer API, make a service account and a JSON key for it (IAM › Service accounts › Keys); in the Play Console, invite its email (Users and permissions) with the rights to release; then name its key in asuka.json
  "android": {"serviceAccount": "play-service-account.json"}
or in the environment: ASUKA_PLAY_SERVICE_ACCOUNT (the key's path, or its JSON). Keep the key out of your repository`

// loadServiceAccount finds the app's Play service account: in the
// environment, else asuka.json, else GOOGLE_APPLICATION_CREDENTIALS.
func loadServiceAccount(cfg *appConfig) (*serviceAccount, error) {
	src := firstOf(os.Getenv("ASUKA_PLAY_SERVICE_ACCOUNT"), cfg.Android.ServiceAccount, os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"))
	if src == "" {
		return nil, errors.New(playKeyHelp)
	}
	data := []byte(src)
	if !strings.HasPrefix(strings.TrimSpace(src), "{") {
		var err error
		if data, err = os.ReadFile(src); err != nil {
			return nil, fmt.Errorf("the Play service account: %v", err)
		}
	}
	sa, err := parseServiceAccount(data)
	if err != nil {
		return nil, fmt.Errorf("the Play service account: %v", err)
	}
	return sa, nil
}

func parseServiceAccount(data []byte) (*serviceAccount, error) {
	sa := &serviceAccount{}
	if err := json.Unmarshal(data, sa); err != nil {
		return nil, err
	}
	if sa.Type != "service_account" || sa.ClientEmail == "" || sa.PrivateKey == "" {
		return nil, fmt.Errorf("not a service account's JSON key (type %q)", sa.Type)
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return nil, errors.New("its private_key isn't PEM")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if k, err = x509.ParsePKCS1PrivateKey(block.Bytes); err != nil {
			return nil, err
		}
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("its private_key is a %T, not RSA", k)
	}
	sa.key = rk
	return sa, nil
}

type playClient struct {
	storeHTTP
	base    string
	sa      *serviceAccount
	token   string
	expires time.Time
	now     func() time.Time
	// chunk is the resumable upload's chunk: a multiple of 256 KiB.
	chunk int64
}

func newPlay(sa *serviceAccount, verbose bool) *playClient {
	return &playClient{storeHTTP: newStoreHTTP(verbose), base: "https://androidpublisher.googleapis.com",
		sa: sa, now: time.Now, chunk: 8 << 20}
}

// playError is Google's answer to a request it refused.
type playError struct {
	status int
	Err    struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
	// The token endpoint's: {"error": "invalid_grant", "error_description": ...}.
	OAuth, OAuthDescription string
}

func (e *playError) Error() string {
	if e.OAuth != "" {
		return fmt.Sprintf("Google: %s: %s", e.OAuth, e.OAuthDescription)
	}
	msg := firstOf(e.Err.Message, http.StatusText(e.status))
	return fmt.Sprintf("Google Play: %d %s", e.status, msg)
}

func (e *playError) contains(s string) bool {
	return strings.Contains(strings.ToLower(e.Err.Message), strings.ToLower(s))
}

func readPlayError(resp *http.Response, raw []byte) *playError {
	e := &playError{status: resp.StatusCode}
	if json.Unmarshal(raw, e) != nil || (e.Err.Message == "" && e.status >= 400) {
		// The token endpoint's form of error.
		var o struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if json.Unmarshal(raw, &o) == nil && o.Error != "" {
			e.OAuth, e.OAuthDescription = o.Error, o.ErrorDescription
		} else if e.Err.Message == "" {
			e.Err.Message = strings.TrimSpace(string(raw))
		}
	}
	return e
}

// bearer is an access token for the API, from the service account's
// signed assertion; a new one when it has 5 minutes left.
func (p *playClient) bearer() (string, error) {
	now := p.now()
	if p.token != "" && now.Before(p.expires.Add(-5*time.Minute)) {
		return p.token, nil
	}
	assertion, err := signJWT(map[string]any{"alg": "RS256", "typ": "JWT", "kid": p.sa.PrivateKeyID}, map[string]any{
		"iss": p.sa.ClientEmail, "scope": playScope, "aud": p.sa.TokenURI,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}, p.sa.key)
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}.Encode()
	var tok struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	err = p.retry(func() error {
		resp, err := p.client.Post(p.sa.TokenURI, "application/x-www-form-urlencoded", strings.NewReader(form))
		if err != nil {
			if transient(err) {
				return &errRetry{err: err}
			}
			return err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			e := readPlayError(resp, raw)
			if retryable(resp.StatusCode, true) {
				return &errRetry{err: e, wait: retryAfter(resp.Header)}
			}
			return e
		}
		return json.Unmarshal(raw, &tok)
	})
	if err != nil {
		return "", fmt.Errorf("signing in as %s: %w", p.sa.ClientEmail, err)
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("signing in as %s: no access token", p.sa.ClientEmail)
	}
	p.token, p.expires = tok.AccessToken, now.Add(time.Duration(max(tok.ExpiresIn, 60))*time.Second)
	return p.token, nil
}

// call sends a request to the API: body (unless nil) as JSON, the
// response's JSON into out (unless nil). idempotent: tried again if the
// server or the network fails, not only when rate limited.
func (p *playClient) call(method, path string, q url.Values, body, out any, idempotent bool) error {
	var data []byte
	if body != nil {
		var err error
		if data, err = json.Marshal(body); err != nil {
			return err
		}
	}
	u := p.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return p.retry(func() error {
		tok, err := p.bearer()
		if err != nil {
			return err
		}
		var rd io.Reader
		if data != nil {
			rd = bytes.NewReader(data)
		}
		req, err := http.NewRequest(method, u, rd)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if data != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		if p.verbose {
			fmt.Println("asuka:", method, u)
		}
		resp, err := p.client.Do(req)
		if err != nil {
			if idempotent && transient(err) {
				return &errRetry{err: err}
			}
			return err
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			if idempotent {
				return &errRetry{err: err}
			}
			return err
		}
		if resp.StatusCode >= 300 {
			e := readPlayError(resp, raw)
			if retryable(resp.StatusCode, idempotent) {
				return &errRetry{err: e, wait: retryAfter(resp.Header)}
			}
			return e
		}
		if out != nil && len(raw) > 0 {
			return json.Unmarshal(raw, out)
		}
		return nil
	})
}

func (p *playClient) edits(pkg string) string {
	return "/androidpublisher/v3/applications/" + url.PathEscape(pkg) + "/edits"
}

// insertEdit opens an edit: the app's changes, applied together on
// commit.
func (p *playClient) insertEdit(pkg string) (string, error) {
	var e struct {
		ID string `json:"id"`
	}
	// A second edit, if the first was made and its answer lost, is
	// harmless: it's never committed.
	if err := p.call(http.MethodPost, p.edits(pkg), nil, map[string]any{}, &e, true); err != nil {
		return "", err
	}
	return e.ID, nil
}

func (p *playClient) deleteEdit(pkg, edit string) error {
	return p.call(http.MethodDelete, p.edits(pkg)+"/"+edit, nil, nil, nil, true)
}

// highestVersionCode is the app's highest version code: of its bundles,
// its APKs and its tracks' releases.
func (p *playClient) highestVersionCode(pkg, edit string) (int64, error) {
	var bundles struct {
		Bundles []struct {
			VersionCode int64 `json:"versionCode"`
		} `json:"bundles"`
	}
	var apks struct {
		Apks []struct {
			VersionCode int64 `json:"versionCode"`
		} `json:"apks"`
	}
	var tracks struct {
		Tracks []playTrack `json:"tracks"`
	}
	path := p.edits(pkg) + "/" + edit
	for _, r := range []struct {
		path string
		out  any
	}{{"/bundles", &bundles}, {"/apks", &apks}, {"/tracks", &tracks}} {
		if err := p.call(http.MethodGet, path+r.path, nil, nil, r.out, true); err != nil {
			return 0, err
		}
	}
	var high int64
	for _, b := range bundles.Bundles {
		high = max(high, b.VersionCode)
	}
	for _, a := range apks.Apks {
		high = max(high, a.VersionCode)
	}
	for _, t := range tracks.Tracks {
		for _, r := range t.Releases {
			for _, vc := range r.VersionCodes {
				n, _ := strconv.ParseInt(vc, 10, 64)
				high = max(high, n)
			}
		}
	}
	return high, nil
}

// defaultLanguage is the language of the app's store listing.
func (p *playClient) defaultLanguage(pkg, edit string) (string, error) {
	var d struct {
		DefaultLanguage string `json:"defaultLanguage"`
	}
	err := p.call(http.MethodGet, p.edits(pkg)+"/"+edit+"/details", nil, nil, &d, true)
	return d.DefaultLanguage, err
}

// errSessionGone: the resumable upload's session expired; a new one
// starts over.
var errSessionGone = errors.New("the upload session expired")

// uploadBundle uploads the .aab into the edit, in chunks, each sent
// again (from where the server says it got to) if it fails: the
// bundle's version code.
func (p *playClient) uploadBundle(pkg, edit, path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	fmt.Printf("asuka: uploading %s (%s)\n", filepath.Base(path), mb(st.Size()))
	start := p.now()
	for range 3 {
		session, err := p.startUpload(pkg, edit, st.Size())
		if err != nil {
			return 0, err
		}
		vc, err := p.sendResumable(session, f, st.Size())
		if errors.Is(err, errSessionGone) {
			fmt.Println("asuka: the upload session expired; starting over")
			continue
		}
		if err == nil {
			fmt.Printf("asuka: uploaded in %.0fs\n", p.now().Sub(start).Seconds())
		}
		return vc, err
	}
	return 0, errSessionGone
}

// startUpload starts a resumable upload: its session's URL.
func (p *playClient) startUpload(pkg, edit string, size int64) (string, error) {
	u := p.base + "/upload" + p.edits(pkg) + "/" + edit + "/bundles?uploadType=resumable"
	var session string
	err := p.retry(func() error {
		tok, err := p.bearer()
		if err != nil {
			return err
		}
		req, err := http.NewRequest(http.MethodPost, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("X-Upload-Content-Type", "application/octet-stream")
		req.Header.Set("X-Upload-Content-Length", strconv.FormatInt(size, 10))
		if p.verbose {
			fmt.Println("asuka: POST", u)
		}
		resp, err := p.client.Do(req)
		if err != nil {
			if transient(err) {
				return &errRetry{err: err}
			}
			return err
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			e := readPlayError(resp, raw)
			if retryable(resp.StatusCode, true) {
				return &errRetry{err: e, wait: retryAfter(resp.Header)}
			}
			return e
		}
		if session = resp.Header.Get("Location"); session == "" {
			return errors.New("Google Play started no upload session")
		}
		return nil
	})
	return session, err
}

// playBundle is an uploaded bundle.
type playBundle struct {
	VersionCode int64  `json:"versionCode"`
	SHA256      string `json:"sha256"`
}

// sendResumable sends the file to the session, chunk by chunk.
func (p *playClient) sendResumable(session string, f *os.File, size int64) (int64, error) {
	offset := int64(0)
	shown := int64(0)
	resync := false
	for {
		var done *playBundle
		err := p.retry(func() error {
			if resync {
				// Where did the server get to?
				o, b, err := p.uploadStatus(session, size)
				if err != nil {
					return err
				}
				if b != nil {
					done = b
					return nil
				}
				offset, resync = o, false
			}
			end := min(offset+p.chunk, size)
			tok, err := p.bearer()
			if err != nil {
				return err
			}
			req, err := http.NewRequest(http.MethodPut, session, io.NewSectionReader(f, offset, end-offset))
			if err != nil {
				return err
			}
			req.ContentLength = end - offset
			req.Header.Set("Authorization", "Bearer "+tok)
			req.Header.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", offset, end-1, size))
			resp, err := p.client.Do(req)
			if err != nil {
				if transient(err) {
					resync = true
					return &errRetry{err: err}
				}
				return err
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			return p.uploadAnswer(resp, raw, &offset, &done, &resync)
		})
		if err != nil {
			return 0, err
		}
		if done != nil {
			return done.VersionCode, nil
		}
		if pct := offset * 100 / size; pct/20 > shown/20 && pct < 100 {
			shown = pct
			fmt.Printf("asuka: %d%%\n", pct)
		}
	}
}

// uploadAnswer reads a resumable upload's answer: done (the bundle),
// more to send (308, and from where), or an error.
func (p *playClient) uploadAnswer(resp *http.Response, raw []byte, offset *int64, done **playBundle, resync *bool) error {
	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated:
		b := &playBundle{}
		if err := json.Unmarshal(raw, b); err != nil {
			return fmt.Errorf("Google Play's answer to the upload: %v", err)
		}
		*done = b
		return nil
	case resp.StatusCode == http.StatusPermanentRedirect: // 308: "Resume Incomplete"
		*offset = 0
		if r := resp.Header.Get("Range"); r != "" {
			// "bytes=0-1234": the next byte is 1235.
			_, last, _ := strings.Cut(r, "-")
			n, err := strconv.ParseInt(last, 10, 64)
			if err != nil {
				return fmt.Errorf("Google Play's upload range %q", r)
			}
			*offset = n + 1
		}
		return nil
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return errSessionGone
	default:
		e := readPlayError(resp, raw)
		if retryable(resp.StatusCode, true) {
			*resync = true
			return &errRetry{err: e, wait: retryAfter(resp.Header)}
		}
		return e
	}
}

// uploadStatus asks the session how much it has: the next offset, or
// the bundle if it has all.
func (p *playClient) uploadStatus(session string, size int64) (int64, *playBundle, error) {
	tok, err := p.bearer()
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(http.MethodPut, session, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Range", fmt.Sprintf("bytes */%d", size))
	resp, err := p.client.Do(req)
	if err != nil {
		if transient(err) {
			return 0, nil, &errRetry{err: err}
		}
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var offset int64
	var done *playBundle
	resync := false
	if err := p.uploadAnswer(resp, raw, &offset, &done, &resync); err != nil {
		return 0, nil, err
	}
	return offset, done, nil
}

type playText struct {
	Language string `json:"language"`
	Text     string `json:"text"`
}

type playRelease struct {
	Name         string     `json:"name,omitempty"`
	VersionCodes []string   `json:"versionCodes"`
	Status       string     `json:"status"`
	UserFraction float64    `json:"userFraction,omitempty"`
	ReleaseNotes []playText `json:"releaseNotes,omitempty"`
}

type playTrack struct {
	Track    string        `json:"track"`
	Releases []playRelease `json:"releases"`
}

// updateTrack puts the release on the track. An app not yet published
// takes only drafts: the release becomes one, said so. The release's
// status, as it went.
func (p *playClient) updateTrack(pkg, edit, track string, rel playRelease) (string, error) {
	path := p.edits(pkg) + "/" + edit + "/tracks/" + url.PathEscape(track)
	err := p.call(http.MethodPut, path, nil, playTrack{Track: track, Releases: []playRelease{rel}}, nil, true)
	var pe *playError
	if errors.As(err, &pe) && pe.contains("draft app") && rel.Status != "draft" {
		fmt.Println("asuka: Google Play takes only draft releases until the app is published: this one is a draft, to roll out in the Play Console")
		rel.Status, rel.UserFraction = "draft", 0
		err = p.call(http.MethodPut, path, nil, playTrack{Track: track, Releases: []playRelease{rel}}, nil, true)
	}
	return rel.Status, err
}

// commit applies the edit, and sends its changes for review. If Play
// won't send them by itself (the app's changes are held for review in
// the Console), they're committed without: sent false.
func (p *playClient) commit(pkg, edit string, cancelReview bool) (sent bool, err error) {
	behavior := "ERROR_IF_IN_REVIEW"
	if cancelReview {
		behavior = "CANCEL_IN_REVIEW_AND_SUBMIT"
	}
	path := p.edits(pkg) + "/" + edit + ":commit"
	err = p.call(http.MethodPost, path, url.Values{"changesInReviewBehavior": {behavior}}, nil, nil, false)
	var pe *playError
	if errors.As(err, &pe) && pe.contains("changesNotSentForReview") {
		err = p.call(http.MethodPost, path, url.Values{"changesNotSentForReview": {"true"}}, nil, nil, false)
		return false, err
	}
	if errors.As(err, &pe) && pe.contains("in review") && !cancelReview {
		return false, fmt.Errorf("%v\nthe app has changes in review: wait for it, or publish again with -cancel-review to cancel that review and send these changes with the others", err)
	}
	return err == nil, err
}

// playHint says what to do about Google Play's refusals that have a fix.
func playHint(err error, sa *serviceAccount, pkg string) error {
	var pe *playError
	if !errors.As(err, &pe) {
		return err
	}
	switch {
	case pe.OAuth != "":
		return fmt.Errorf("%v\nthe service account's key is wrong or revoked: make a new one (Google Cloud › IAM › Service accounts › Keys)", err)
	case pe.status == http.StatusNotFound && pe.contains("package"):
		return fmt.Errorf("%v\nGoogle Play has no app %s, or the service account can't see it: create it in the Play Console with that package name and upload its first .aab there by hand; Play takes the ones after through its API", err, pkg)
	case pe.status == http.StatusForbidden || pe.status == http.StatusUnauthorized:
		return fmt.Errorf("%v\ninvite %s in the Play Console (Users and permissions) with the rights to release this app, and turn on the Google Play Android Developer API in its Google Cloud project", err, sa.ClientEmail)
	}
	return err
}
