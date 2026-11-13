package main

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakePlay is Google's token endpoint and the Play Developer API, as far
// as asuka publish uses them.
type fakePlay struct {
	t      *testing.T
	mu     sync.Mutex
	srv    *httptest.Server
	pub    *rsa.PublicKey
	log    []string
	edits  int
	tokens int
	// the upload
	size     int64
	received []byte
	failOnce bool // the second chunk fails once, with a 503
	// the track and the commit
	draftApp    bool
	holdReview  bool
	tracks      []map[string]any
	commitQuery []string
}

func newFakePlay(t *testing.T, pub *rsa.PublicKey) *fakePlay {
	f := &fakePlay{t: t, pub: pub, failOnce: true}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakePlay) json(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func (f *fakePlay) fail(w http.ResponseWriter, status int, msg string) {
	f.json(w, status, map[string]any{"error": map[string]any{"code": status, "message": msg, "status": "FAILED_PRECONDITION"}})
}

func (f *fakePlay) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	if r.URL.Path == "/token" {
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ParseForm()
		a := r.PostForm.Get("assertion")
		parts := strings.Split(a, ".")
		if r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || len(parts) != 3 {
			f.json(w, 400, map[string]any{"error": "invalid_request"})
			return
		}
		sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
		sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if rsa.VerifyPKCS1v15(f.pub, crypto.SHA256, sum[:], sig) != nil {
			f.json(w, 400, map[string]any{"error": "invalid_grant", "error_description": "Invalid JWT Signature."})
			return
		}
		claims := jwtPart(f.t, a, 1)
		if claims["scope"] != playScope || claims["iss"] != "publisher@x.iam.gserviceaccount.com" || claims["aud"] != f.srv.URL+"/token" {
			f.t.Errorf("claims %v", claims)
		}
		f.tokens++
		f.json(w, 200, map[string]any{"access_token": "tok", "expires_in": 3600, "token_type": "Bearer"})
		return
	}
	if r.URL.Path != "/session/s1" && r.Header.Get("Authorization") != "Bearer tok" {
		f.fail(w, 401, "no token")
		return
	}
	const edits = "/androidpublisher/v3/applications/com.example.notes/edits"
	key := r.Method + " " + r.URL.Path
	f.log = append(f.log, key)
	if strings.Contains(r.URL.Path, "/applications/") && !strings.Contains(r.URL.Path, "/applications/com.example.notes/") {
		f.fail(w, 404, "Package not found: "+strings.Split(r.URL.Path, "/")[4]+".")
		return
	}
	switch {
	case key == "POST "+edits:
		f.edits++
		f.json(w, 200, map[string]any{"id": fmt.Sprintf("e%d", f.edits)})
	case key == "GET "+edits+"/e1/bundles":
		f.json(w, 200, map[string]any{"bundles": []map[string]any{{"versionCode": 5}}})
	case key == "GET "+edits+"/e1/apks":
		f.json(w, 200, map[string]any{}) // no APKs: no "apks"
	case key == "GET "+edits+"/e1/tracks":
		f.json(w, 200, map[string]any{"tracks": []map[string]any{
			{"track": "production", "releases": []map[string]any{{"versionCodes": []string{"12"}, "status": "completed"}}},
			{"track": "internal"},
		}})
	case key == "GET "+edits+"/e1/details":
		f.json(w, 200, map[string]any{"defaultLanguage": "en-GB"})
	case key == "DELETE "+edits+"/e1":
		w.WriteHeader(204)
	case key == "POST /upload"+edits+"/e2/bundles":
		if r.URL.Query().Get("uploadType") != "resumable" || r.Header.Get("X-Upload-Content-Type") != "application/octet-stream" {
			f.t.Errorf("upload start %v %v", r.URL.Query(), r.Header)
		}
		f.size, _ = strconv.ParseInt(r.Header.Get("X-Upload-Content-Length"), 10, 64)
		w.Header().Set("Location", f.srv.URL+"/session/s1")
		w.WriteHeader(200)
	case key == "PUT /session/s1":
		cr := r.Header.Get("Content-Range")
		if cr == fmt.Sprintf("bytes */%d", f.size) {
			// Where it got to.
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(f.received)-1))
			w.WriteHeader(308)
			return
		}
		var start, end, total int64
		if _, err := fmt.Sscanf(cr, "bytes %d-%d/%d", &start, &end, &total); err != nil || total != f.size || start != int64(len(f.received)) || end-start+1 != int64(len(body)) {
			f.t.Errorf("chunk %q of %d bytes, have %d", cr, len(body), len(f.received))
			w.WriteHeader(400)
			return
		}
		if start > 0 && f.failOnce {
			f.failOnce = false
			f.fail(w, 503, "backend error")
			return
		}
		f.received = append(f.received, body...)
		if int64(len(f.received)) < f.size {
			w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(f.received)-1))
			w.WriteHeader(308)
			return
		}
		f.json(w, 200, map[string]any{"versionCode": 13, "sha256": "x"})
	case strings.HasPrefix(key, "PUT "+edits+"/e2/tracks/"):
		t := map[string]any{}
		json.Unmarshal(body, &t)
		f.tracks = append(f.tracks, t)
		rel := t["releases"].([]any)[0].(map[string]any)
		if f.draftApp && rel["status"] != "draft" {
			f.fail(w, 400, "Only releases with status draft may be created on draft app.")
			return
		}
		f.json(w, 200, t)
	case key == "POST "+edits+"/e2:commit":
		f.commitQuery = append(f.commitQuery, r.URL.RawQuery)
		if f.holdReview && r.URL.Query().Get("changesNotSentForReview") != "true" {
			f.fail(w, 400, "Changes cannot be sent for review automatically. Please set the query parameter changesNotSentForReview to true.")
			return
		}
		f.json(w, 200, map[string]any{"id": "e2"})
	case key == "DELETE "+edits+"/e2":
		w.WriteHeader(204)
	default:
		f.t.Errorf("unexpected %s", key)
		f.fail(w, 404, "Package not found: com.example.notes.")
	}
}

func newServiceAccount(t *testing.T, tokenURI string) (*serviceAccount, []byte) {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	js, _ := json.Marshal(map[string]any{
		"type": "service_account", "project_id": "p", "private_key_id": "kid1",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email": "publisher@x.iam.gserviceaccount.com", "token_uri": tokenURI,
	})
	sa, err := parseServiceAccount(js)
	if err != nil {
		t.Fatal(err)
	}
	return sa, js
}

func testPlay(t *testing.T) (*playClient, *fakePlay) {
	srv := httptest.NewUnstartedServer(nil)
	sa, _ := newServiceAccount(t, "")
	f := &fakePlay{t: t, pub: &sa.key.PublicKey, failOnce: true, srv: srv}
	srv.Config.Handler = http.HandlerFunc(f.serve)
	srv.Start()
	t.Cleanup(srv.Close)
	sa.TokenURI = srv.URL + "/token"
	p := newPlay(sa, false)
	p.base = srv.URL
	p.sleep = func(time.Duration) {}
	p.chunk = 256 << 10
	return p, f
}

func TestPublishAndroid(t *testing.T) {
	p, f := testPlay(t)
	app := testApp(t)
	var built int
	build, content := fakeBuild(t, 700<<10, ".aab", &built)
	o := &publishOptions{track: "production", rollout: 0.1}
	if err := publishAndroid(p, app, o, map[string]string{"": "Faster sync.", "de-DE": "Schneller."}, build); err != nil {
		t.Fatalf("%v\nrequests: %v", err, f.log)
	}
	// Above every version code: the bundles', the tracks'.
	if built != 13 {
		t.Fatalf("built %d, want 13", built)
	}
	if !bytes.Equal(f.received, content) {
		t.Fatalf("received %d of %d bytes", len(f.received), len(content))
	}
	if f.tokens != 1 {
		t.Fatalf("%d tokens", f.tokens)
	}
	rel := f.tracks[0]["releases"].([]any)[0].(map[string]any)
	if f.tracks[0]["track"] != "production" || rel["status"] != "inProgress" || rel["userFraction"] != 0.1 ||
		rel["name"] != "1.2 (13)" || rel["versionCodes"].([]any)[0] != "13" {
		t.Fatalf("release %v", rel)
	}
	notes := rel["releaseNotes"].([]any)
	if len(notes) != 2 || notes[0].(map[string]any)["language"] != "de-DE" || notes[1].(map[string]any)["language"] != "en-GB" || notes[1].(map[string]any)["text"] != "Faster sync." {
		t.Fatalf("notes %v", notes)
	}
	if len(f.commitQuery) != 1 || f.commitQuery[0] != "changesInReviewBehavior=ERROR_IF_IN_REVIEW" {
		t.Fatalf("commit %v", f.commitQuery)
	}
	// The first edit, only looked at, is deleted; the second committed.
	if !strings.Contains(strings.Join(f.log, "\n"), "DELETE /androidpublisher/v3/applications/com.example.notes/edits/e1") ||
		strings.Contains(strings.Join(f.log, "\n"), "edits/e2\n") {
		t.Fatalf("edits: %v", f.log)
	}
}

func TestPublishAndroidDraftApp(t *testing.T) {
	p, f := testPlay(t)
	f.draftApp, f.holdReview = true, true
	var built int
	build, _ := fakeBuild(t, 1000, ".aab", &built)
	if err := publishAndroid(p, testApp(t), &publishOptions{track: "internal"}, nil, build); err != nil {
		t.Fatalf("%v\nrequests: %v", err, f.log)
	}
	if len(f.tracks) != 2 || f.tracks[1]["releases"].([]any)[0].(map[string]any)["status"] != "draft" {
		t.Fatalf("tracks %v", f.tracks)
	}
	if len(f.commitQuery) != 2 || f.commitQuery[1] != "changesNotSentForReview=true" {
		t.Fatalf("commits %v", f.commitQuery)
	}
}

func TestPublishAndroidRefusals(t *testing.T) {
	// A Play that knows no such app: what to do.
	p, f := testPlay(t)
	app := testApp(t)
	app.cfg.ID = "com.example.other"
	var built int
	build, _ := fakeBuild(t, 10, ".aab", &built)
	err := publishAndroid(p, app, &publishOptions{track: "internal"}, nil, build)
	if err == nil || !strings.Contains(err.Error(), "upload its first .aab there by hand") || built != 0 {
		t.Fatalf("%v (built %d)", err, built)
	}
	if len(f.log) != 1 {
		t.Fatalf("requests %v", f.log)
	}
	// Notes over Play's 500 characters, before the build.
	p, _ = testPlay(t)
	err = publishAndroid(p, testApp(t), &publishOptions{track: "internal"}, map[string]string{"": strings.Repeat("é", 501)}, build)
	if err == nil || !strings.Contains(err.Error(), "501 characters") || built != 0 {
		t.Fatalf("%v (built %d)", err, built)
	}
	// A bad key.
	p, _ = testPlay(t)
	other, _ := newServiceAccount(t, p.sa.TokenURI)
	p.sa.key = other.key
	err = publishAndroid(p, testApp(t), &publishOptions{track: "internal"}, nil, build)
	if err == nil || !strings.Contains(err.Error(), "Invalid JWT Signature") || !strings.Contains(err.Error(), "make a new one") {
		t.Fatalf("%v", err)
	}
}

func TestLoadServiceAccount(t *testing.T) {
	for _, k := range []string{"ASUKA_PLAY_SERVICE_ACCOUNT", "GOOGLE_APPLICATION_CREDENTIALS"} {
		t.Setenv(k, "")
	}
	cfg := &appConfig{}
	if _, err := loadServiceAccount(cfg); err == nil || !strings.Contains(err.Error(), "Users and permissions") {
		t.Fatal(err)
	}
	_, js := newServiceAccount(t, "")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "play.json"), js, 0o600)
	os.WriteFile(filepath.Join(dir, "asuka.json"), []byte(`{"android":{"serviceAccount":"play.json"}}`), 0o644)
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	sa, err := loadServiceAccount(cfg)
	if err != nil || sa.ClientEmail != "publisher@x.iam.gserviceaccount.com" || sa.TokenURI != "https://oauth2.googleapis.com/token" {
		t.Fatalf("%+v %v", sa, err)
	}
	// The JSON itself, in the environment (CI's secret).
	t.Setenv("ASUKA_PLAY_SERVICE_ACCOUNT", string(js))
	if _, err := loadServiceAccount(&appConfig{}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASUKA_PLAY_SERVICE_ACCOUNT", `{"type":"authorized_user"}`)
	if _, err := loadServiceAccount(&appConfig{}); err == nil || !strings.Contains(err.Error(), "not a service account") {
		t.Fatal(err)
	}
}

func TestPublishFlags(t *testing.T) {
	for _, c := range []struct {
		args []string
		err  string
	}{
		{[]string{}, "usage"},
		{[]string{"-platform", "mac"}, "one of ios, android"},
		{[]string{"-platform", "ios", "-track", "beta"}, "-track is for -platform android"},
		{[]string{"-platform", "android", "-groups", "x"}, "-groups is for -platform ios"},
		{[]string{"-platform", "ios", "-file", "a.ipa", "-build", "3"}, "-file's number"},
		{[]string{"-platform", "ios", "-release", "later"}, "auto or manual"},
		{[]string{"-platform", "ios", "-release", "auto"}, "goes with -review"},
		{[]string{"-platform", "ios", "-no-wait", "-groups", "x"}, "-no-wait"},
		{[]string{"-platform", "android", "-rollout", "1"}, "above 0 and below 1"},
		{[]string{"-platform", "android", "-rollout", "0.2", "-draft"}, "a draft isn't rolled out"},
		{[]string{"-platform", "ios", "-groups", "a", "-review", "-release", "manual"}, ""},
		{[]string{"-platform", "android", "-track", "beta", "-rollout", "0.5"}, ""},
	} {
		var o publishOptions
		fs := publishFlags(&o)
		fs.SetOutput(io.Discard)
		err := fs.Parse(c.args)
		if err == nil {
			set := map[string]bool{}
			fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
			err = o.check(set)
		}
		if (err == nil) != (c.err == "") || (err != nil && !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%v: %v, want %q", c.args, err, c.err)
		}
	}
}

func TestReadNotes(t *testing.T) {
	if n, err := readNotes(""); n != nil || err != nil {
		t.Fatal(n, err)
	}
	if n, _ := readNotes("  Bug fixes. "); n[""] != "Bug fixes." {
		t.Fatal(n)
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("From a file\n"), 0o644)
	if n, _ := readNotes("@" + filepath.Join(dir, "notes.txt")); n[""] != "From a file" {
		t.Fatal(n)
	}
	langs := filepath.Join(dir, "langs")
	os.MkdirAll(langs, 0o755)
	os.WriteFile(filepath.Join(langs, "en-US.txt"), []byte("Hello"), 0o644)
	os.WriteFile(filepath.Join(langs, "ja.txt"), []byte("こんにちは"), 0o644)
	os.WriteFile(filepath.Join(langs, "empty.txt"), []byte("\n"), 0o644)
	os.WriteFile(filepath.Join(langs, "README.md"), []byte("not notes"), 0o644)
	n, err := readNotes("@" + langs)
	if err != nil || len(n) != 2 || n["en-US"] != "Hello" || n["ja"] != "こんにちは" {
		t.Fatal(n, err)
	}
	if _, err := readNotes("@" + filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing file")
	}
}

func TestNextBuild(t *testing.T) {
	for _, c := range []struct{ high, cfg, want, out int }{
		{0, 1, 0, 1},   // a first upload: asuka.json's
		{7, 3, 0, 8},   // after the store's
		{7, 20, 0, 20}, // asuka.json's, if higher
		{7, 3, 10, 10},
	} {
		if n, err := nextBuild(c.high, c.cfg, c.want); err != nil || n != c.out {
			t.Errorf("%+v: %d %v", c, n, err)
		}
	}
	if _, err := nextBuild(7, 1, 7); err == nil {
		t.Error("-build at the store's highest")
	}
	for s, n := range map[string]int{"12": 12, "12.3": 12, "": 0, "x": 0, "2024.1.5": 2024} {
		if buildNumber(s) != n {
			t.Errorf("buildNumber(%q) = %d", s, buildNumber(s))
		}
	}
}
