package main

import (
	"archive/zip"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// verifyES256 checks a JWT's ES256 signature.
func verifyES256(token string, pub *ecdsa.PublicKey) bool {
	i := strings.LastIndexByte(token, '.')
	if i < 0 {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(token[i+1:])
	if err != nil || len(sig) != 64 {
		return false
	}
	sum := sha256.Sum256([]byte(token[:i]))
	return ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:]))
}

// jwtPart decodes a JWT's header (0) or claims (1).
func jwtPart(t *testing.T, token string, i int) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("not a JWT: %q", token)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[i])
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func newP8(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return k, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

func TestASCToken(t *testing.T) {
	k, _ := newP8(t)
	now := time.Unix(1_800_000_000, 0)
	c := newASC(&ascKey{id: "KEY123", issuer: "issuer-1", key: k}, false)
	c.now = func() time.Time { return now }
	tok, err := c.bearer()
	if err != nil {
		t.Fatal(err)
	}
	if !verifyES256(tok, &k.PublicKey) {
		t.Fatal("the token's signature doesn't verify")
	}
	h, claims := jwtPart(t, tok, 0), jwtPart(t, tok, 1)
	if h["alg"] != "ES256" || h["kid"] != "KEY123" || h["typ"] != "JWT" {
		t.Fatalf("header %v", h)
	}
	if claims["iss"] != "issuer-1" || claims["aud"] != "appstoreconnect-v1" || claims["sub"] != nil {
		t.Fatalf("claims %v", claims)
	}
	if exp, iat := claims["exp"].(float64), claims["iat"].(float64); exp-iat > 20*60 || iat != float64(now.Unix()) {
		t.Fatalf("a token for %vs: Apple takes at most 20 minutes", exp-iat)
	}
	// The same token for a while, a new one after.
	now = now.Add(9 * time.Minute)
	if again, _ := c.bearer(); again != tok {
		t.Fatal("a new token too soon")
	}
	now = now.Add(2 * time.Minute)
	if again, _ := c.bearer(); again == tok {
		t.Fatal("the token wasn't renewed")
	}
	// An individual key: no issuer, the subject "user".
	c = newASC(&ascKey{id: "KEY123", key: k}, false)
	tok, _ = c.bearer()
	if claims := jwtPart(t, tok, 1); claims["sub"] != "user" || claims["iss"] != nil {
		t.Fatalf("individual key's claims %v", claims)
	}
}

func TestLoadASCKey(t *testing.T) {
	_, p8 := newP8(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, k := range []string{"RYUI_ASC_KEY_ID", "RYUI_ASC_ISSUER_ID", "RYUI_ASC_KEY", "API_PRIVATE_KEYS_DIR"} {
		t.Setenv(k, "")
	}
	cfg := &appConfig{dir: t.TempDir()}
	if _, err := loadASCKey(cfg); err == nil || !strings.Contains(err.Error(), "Users and Access") {
		t.Fatalf("no key: %v", err)
	}
	// Found where altool looks.
	cfg.IOS.AppStoreConnect.KeyID, cfg.IOS.AppStoreConnect.IssuerID = "ABC", "iss"
	if _, err := loadASCKey(cfg); err == nil || !strings.Contains(err.Error(), "AuthKey_ABC.p8") {
		t.Fatalf("no file: %v", err)
	}
	dir := filepath.Join(home, ".appstoreconnect", "private_keys")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "AuthKey_ABC.p8"), p8, 0o600)
	k, err := loadASCKey(cfg)
	if err != nil || k.id != "ABC" || k.issuer != "iss" {
		t.Fatalf("%+v %v", k, err)
	}
	// ryui.json's path, made absolute by loadConfig.
	app := t.TempDir()
	os.WriteFile(filepath.Join(app, "key.p8"), p8, 0o600)
	os.WriteFile(filepath.Join(app, "ryui.json"), []byte(`{"ios":{"appStoreConnect":{"keyId":"DEF","issuerId":"i2","key":"key.p8"}}}`), 0o644)
	cfg, err = loadConfig(app)
	if err != nil {
		t.Fatal(err)
	}
	if k, err = loadASCKey(cfg); err != nil || k.id != "DEF" {
		t.Fatalf("%+v %v", k, err)
	}
	// The environment's, with the key's text.
	t.Setenv("RYUI_ASC_KEY_ID", "ENV")
	t.Setenv("RYUI_ASC_KEY", string(p8))
	if k, err = loadASCKey(cfg); err != nil || k.id != "ENV" || k.issuer != "i2" {
		t.Fatalf("%+v %v", k, err)
	}
	t.Setenv("RYUI_ASC_KEY", "-----BEGIN PRIVATE KEY-----\nnope\n-----END PRIVATE KEY-----")
	if _, err = loadASCKey(cfg); err == nil {
		t.Fatal("a bad key loaded")
	}
}

// fakeASC is App Store Connect, as far as ryui publish uses it.
type fakeASC struct {
	t        *testing.T
	mu       sync.Mutex
	srv      *httptest.Server
	pub      *ecdsa.PublicKey
	log      []string
	bodies   map[string][]map[string]any
	parts    map[int64][]byte
	failPart int // parts to fail once with a 500
	// state
	versions     []map[string]any // appStoreVersions
	localization []map[string]any
	uploadPolls  int
	buildPolls   int
	groupTries   int
	uploadState  string
	uploadErrors []map[string]any
}

func newFakeASC(t *testing.T, pub *ecdsa.PublicKey) *fakeASC {
	f := &fakeASC{t: t, pub: pub, bodies: map[string][]map[string]any{}, parts: map[int64][]byte{}, failPart: 1,
		versions: []map[string]any{{"type": "appStoreVersions", "id": "v11", "attributes": map[string]any{"versionString": "1.1", "appVersionState": "READY_FOR_DISTRIBUTION"}}},
		localization: []map[string]any{
			{"type": "appStoreVersionLocalizations", "id": "l-en", "attributes": map[string]any{"locale": "en-US", "whatsNew": ""}},
		},
		uploadState: "COMPLETE",
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeASC) write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		json.NewEncoder(w).Encode(v)
	}
}

func list(items ...map[string]any) map[string]any {
	if items == nil {
		items = []map[string]any{}
	}
	return map[string]any{"data": items, "links": map[string]any{"self": "x"}}
}

func one(item map[string]any) map[string]any { return map[string]any{"data": item} }

func res(typ, id string, attrs map[string]any) map[string]any {
	return map[string]any{"type": typ, "id": id, "attributes": attrs}
}

func (f *fakeASC) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	body, _ := io.ReadAll(r.Body)
	if strings.HasPrefix(path, "/parts/") {
		// The upload's parts: presigned, no token.
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Part") != "yes" {
			f.t.Errorf("part %s: headers %v", path, r.Header)
		}
		var off int64
		fmt.Sscan(strings.TrimPrefix(path, "/parts/"), &off)
		if f.failPart < 0 {
			w.WriteHeader(http.StatusForbidden) // for good
			return
		}
		if off != 0 && f.failPart > 0 {
			f.failPart--
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		f.parts[off] = body
		return
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !verifyES256(tok, f.pub) {
		f.write(w, 401, map[string]any{"errors": []map[string]any{{"status": "401", "code": "NOT_AUTHORIZED", "title": "no"}}})
		return
	}
	key := r.Method + " " + path
	f.log = append(f.log, key)
	if len(body) > 0 {
		m := map[string]any{}
		if err := json.Unmarshal(body, &m); err != nil {
			f.t.Errorf("%s: %v", key, err)
		}
		f.bodies[key] = append(f.bodies[key], m)
	}
	q := r.URL.Query()
	switch key {
	case "GET /v1/apps":
		if q.Get("filter[bundleId]") != "com.example.notes" {
			f.write(w, 200, list())
			return
		}
		f.write(w, 200, list(res("apps", "app1", map[string]any{"bundleId": "com.example.notes", "name": "Notes", "primaryLocale": "en-US"})))
	case "GET /v1/builds":
		if q.Get("filter[version]") != "" {
			f.write(w, 200, list()) // not linked by number: the upload links it
			return
		}
		if q.Get("filter[app]") != "app1" || q.Get("sort") != "-uploadedDate" {
			f.t.Errorf("builds query %v", q)
		}
		f.write(w, 200, list(res("builds", "b-old", map[string]any{"version": "7"}), res("builds", "b-older", map[string]any{"version": "6.1"})))
	case "GET /v1/apps/app1/buildUploads":
		f.write(w, 200, list(res("buildUploads", "u-old", map[string]any{"cfBundleVersion": "8"})))
	case "GET /v1/apps/app1/appStoreVersions":
		var out []map[string]any
		for _, v := range f.versions {
			if s := q.Get("filter[versionString]"); s == "" || v["attributes"].(map[string]any)["versionString"] == s {
				out = append(out, v)
			}
		}
		f.write(w, 200, list(out...))
	case "GET /v1/apps/app1/betaGroups":
		f.write(w, 200, list(
			res("betaGroups", "g-team", map[string]any{"name": "Team", "isInternalGroup": true, "hasAccessToAllBuilds": true}),
			res("betaGroups", "g-friends", map[string]any{"name": "Friends", "isInternalGroup": false}),
		))
	case "POST /v1/buildUploads":
		f.write(w, 201, one(res("buildUploads", "up1", map[string]any{})))
	case "POST /v1/buildUploadFiles":
		size := int64(f.bodies[key][0]["data"].(map[string]any)["attributes"].(map[string]any)["fileSize"].(float64))
		var ops []map[string]any
		for off := int64(0); off < size; off += 1000 {
			n := min(1000, size-off)
			ops = append(ops, map[string]any{"method": "PUT", "url": fmt.Sprintf("%s/parts/%d", f.srv.URL, off), "offset": off, "length": n,
				"requestHeaders": []map[string]any{{"name": "X-Part", "value": "yes"}}})
		}
		f.write(w, 201, one(res("buildUploadFiles", "file1", map[string]any{"uploadOperations": ops})))
	case "DELETE /v1/buildUploads/up1":
		w.WriteHeader(204)
	case "PATCH /v1/buildUploadFiles/file1":
		f.write(w, 200, one(res("buildUploadFiles", "file1", map[string]any{})))
	case "GET /v1/buildUploads/up1":
		f.uploadPolls++
		attrs := map[string]any{"state": map[string]any{"state": "PROCESSING", "warnings": []map[string]any{{"code": "W1", "description": "a warning"}}}}
		u := res("buildUploads", "up1", attrs)
		if f.uploadPolls > 1 {
			attrs["state"] = map[string]any{"state": f.uploadState, "errors": f.uploadErrors}
			if f.uploadState == "COMPLETE" {
				u["relationships"] = map[string]any{"build": map[string]any{"data": map[string]any{"type": "builds", "id": "b1"}}}
			}
		}
		f.write(w, 200, one(u))
	case "GET /v1/builds/b1":
		f.buildPolls++
		state := "PROCESSING"
		if f.buildPolls > 1 {
			state = "VALID"
		}
		f.write(w, 200, one(res("builds", "b1", map[string]any{"version": "9", "processingState": state})))
	case "GET /v1/builds/b1/betaBuildLocalizations":
		f.write(w, 200, list())
	case "POST /v1/betaBuildLocalizations":
		f.write(w, 201, one(res("betaBuildLocalizations", "bl1", map[string]any{})))
	case "GET /v1/builds/b1/buildBetaDetail":
		f.write(w, 200, one(res("buildBetaDetails", "d1", map[string]any{"internalBuildState": "READY_FOR_BETA_TESTING", "externalBuildState": "READY_FOR_BETA_SUBMISSION"})))
	case "POST /v1/builds/b1/relationships/betaGroups":
		if f.groupTries++; f.groupTries == 1 {
			f.write(w, 404, map[string]any{"errors": []map[string]any{{"status": "404", "code": "NOT_FOUND", "title": "not yet"}}})
			return
		}
		w.WriteHeader(204)
	case "POST /v1/betaAppReviewSubmissions":
		f.write(w, 201, one(res("betaAppReviewSubmissions", "br1", map[string]any{})))
	case "POST /v1/appStoreVersions":
		v := res("appStoreVersions", "v12", map[string]any{"versionString": "1.2", "appVersionState": "PREPARE_FOR_SUBMISSION"})
		f.versions = append(f.versions, v)
		f.write(w, 201, one(v))
	case "PATCH /v1/appStoreVersions/v12/relationships/build":
		w.WriteHeader(204)
	case "GET /v1/appStoreVersions/v12/appStoreVersionLocalizations":
		f.write(w, 200, list(f.localization...))
	case "PATCH /v1/appStoreVersionLocalizations/l-en", "PATCH /v1/appStoreVersionLocalizations/l-ja":
		f.write(w, 200, one(res("appStoreVersionLocalizations", "l", map[string]any{})))
	case "GET /v1/reviewSubmissions":
		f.write(w, 200, list())
	case "POST /v1/reviewSubmissions":
		f.write(w, 201, one(res("reviewSubmissions", "s1", map[string]any{"state": "READY_FOR_REVIEW"})))
	case "GET /v1/reviewSubmissions/s1/items":
		f.write(w, 200, list())
	case "POST /v1/reviewSubmissionItems":
		f.write(w, 201, one(res("reviewSubmissionItems", "i1", map[string]any{})))
	case "PATCH /v1/reviewSubmissions/s1":
		f.write(w, 200, one(res("reviewSubmissions", "s1", map[string]any{"state": "WAITING_FOR_REVIEW"})))
	default:
		f.t.Errorf("unexpected %s", key)
		f.write(w, 404, map[string]any{"errors": []map[string]any{{"status": "404", "code": "NOT_FOUND", "title": "no " + key}}})
	}
}

func (f *fakeASC) attrs(key string, i int) map[string]any {
	f.t.Helper()
	if len(f.bodies[key]) <= i {
		f.t.Fatalf("no body %d for %s; requests: %v", i, key, f.log)
	}
	return f.bodies[key][i]["data"].(map[string]any)["attributes"].(map[string]any)
}

func (f *fakeASC) count(key string) int {
	n := 0
	for _, k := range f.log {
		if k == key {
			n++
		}
	}
	return n
}

func testASC(t *testing.T) (*ascClient, *fakeASC) {
	k, _ := newP8(t)
	f := newFakeASC(t, &k.PublicKey)
	c := newASC(&ascKey{id: "K", issuer: "I", key: k}, false)
	c.base = f.srv.URL
	c.sleep = func(time.Duration) {}
	c.poll = 0
	return c, f
}

func testApp(t *testing.T) *goApp {
	cfg := &appConfig{Name: "Notes", ID: "com.example.notes", Version: "1.2", Build: 3, dir: t.TempDir()}
	return &goApp{dir: cfg.dir, cfg: cfg}
}

// fakeBuild stands for the release build: a file of size bytes.
func fakeBuild(t *testing.T, size int, ext string, got *int) (func(*goApp, *buildOptions) (string, error), []byte) {
	content := bytes.Repeat([]byte("0123456789abcdef"), size/16+1)[:size]
	return func(app *goApp, o *buildOptions) (string, error) {
		*got = app.cfg.Build
		p := filepath.Join(t.TempDir(), "Notes"+ext)
		return p, os.WriteFile(p, content, 0o644)
	}, content
}

func TestPublishIOS(t *testing.T) {
	c, f := testASC(t)
	app := testApp(t)
	var built int
	build, content := fakeBuild(t, 2500, ".ipa", &built)
	notes := map[string]string{"": "Faster sync."}
	o := &publishOptions{groups: "team, friends", review: true, release: "auto"}
	if err := publishIOS(c, app, o, notes, build); err != nil {
		t.Fatalf("%v\nrequests: %v", err, f.log)
	}
	// The next build number: above the in-flight upload's 8.
	if built != 9 {
		t.Fatalf("built %d, want 9", built)
	}
	if a := f.attrs("POST /v1/buildUploads", 0); a["cfBundleVersion"] != "9" || a["cfBundleShortVersionString"] != "1.2" || a["platform"] != "IOS" {
		t.Fatalf("buildUploads %v", a)
	}
	if a := f.attrs("POST /v1/buildUploadFiles", 0); a["fileName"] != "Notes.ipa" || a["fileSize"] != float64(2500) || a["uti"] != "com.apple.ipa" || a["assetType"] != "ASSET" {
		t.Fatalf("buildUploadFiles %v", a)
	}
	// Every part, once a failed one was sent again.
	var got []byte
	for off := int64(0); off < 2500; off += 1000 {
		got = append(got, f.parts[off]...)
	}
	if !bytes.Equal(got, content) || f.failPart != 0 {
		t.Fatalf("uploaded %d bytes (failures left %d)", len(got), f.failPart)
	}
	if a := f.attrs("PATCH /v1/buildUploadFiles/file1", 0); a["uploaded"] != true {
		t.Fatalf("commit %v", a)
	}
	if a := f.attrs("POST /v1/betaBuildLocalizations", 0); a["locale"] != "en-US" || a["whatsNew"] != "Faster sync." {
		t.Fatalf("What to Test %v", a)
	}
	// Friends only (Team has every build), after TestFlight found the build.
	if len(f.bodies["POST /v1/builds/b1/relationships/betaGroups"]) != 2 {
		t.Fatalf("groups tried %d times", len(f.bodies["POST /v1/builds/b1/relationships/betaGroups"]))
	}
	groups := f.bodies["POST /v1/builds/b1/relationships/betaGroups"][1]["data"].([]any)
	if len(groups) != 1 || groups[0].(map[string]any)["id"] != "g-friends" {
		t.Fatalf("groups %v", groups)
	}
	if f.count("POST /v1/betaAppReviewSubmissions") != 1 {
		t.Fatal("no beta review for the external group")
	}
	// App Review: the version made, with the build, What's New, submitted.
	if a := f.attrs("POST /v1/appStoreVersions", 0); a["versionString"] != "1.2" || a["releaseType"] != "AFTER_APPROVAL" {
		t.Fatalf("version %v", a)
	}
	if b := f.bodies["PATCH /v1/appStoreVersions/v12/relationships/build"][0]["data"].(map[string]any); b["id"] != "b1" {
		t.Fatalf("version's build %v", b)
	}
	if a := f.attrs("PATCH /v1/appStoreVersionLocalizations/l-en", 0); a["whatsNew"] != "Faster sync." {
		t.Fatalf("What's New %v", a)
	}
	if a := f.attrs("PATCH /v1/reviewSubmissions/s1", 0); a["submitted"] != true {
		t.Fatalf("submission %v", a)
	}
	if f.count("POST /v1/reviewSubmissionItems") != 1 {
		t.Fatal("the version wasn't added to the submission")
	}
}

func TestPublishIOSRefusals(t *testing.T) {
	var built int
	build, _ := fakeBuild(t, 10, ".ipa", &built)

	// A version on the App Store takes no more builds: no build made.
	c, f := testASC(t)
	f.versions = append(f.versions, res("appStoreVersions", "v12", map[string]any{"versionString": "1.2", "appVersionState": "READY_FOR_DISTRIBUTION"}))
	err := publishIOS(c, testApp(t), &publishOptions{}, nil, build)
	if err == nil || !strings.Contains(err.Error(), "raise ryui.json's version") || built != 0 {
		t.Fatalf("closed version: %v (built %d)", err, built)
	}

	// An unknown group, named with the app's.
	c, _ = testASC(t)
	err = publishIOS(c, testApp(t), &publishOptions{groups: "Family"}, nil, build)
	if err == nil || !strings.Contains(err.Error(), `no TestFlight group "Family"; the app's are "Team", "Friends"`) || built != 0 {
		t.Fatalf("unknown group: %v", err)
	}

	// No app with the bundle ID.
	c, _ = testASC(t)
	app := testApp(t)
	app.cfg.ID = "com.example.other"
	if err = publishIOS(c, app, &publishOptions{}, nil, build); err == nil || !strings.Contains(err.Error(), "New App") {
		t.Fatalf("no app: %v", err)
	}

	// -build at or below the store's highest.
	c, _ = testASC(t)
	if err = publishIOS(c, testApp(t), &publishOptions{build: 8}, nil, build); err == nil || !strings.Contains(err.Error(), "has build 8 already") {
		t.Fatalf("low build: %v", err)
	}

	// Apple turns the upload down: its reasons.
	c, f = testASC(t)
	f.uploadState = "FAILED"
	f.uploadErrors = []map[string]any{{"code": "90683", "description": "Missing purpose string in Info.plist"}}
	err = publishIOS(c, testApp(t), &publishOptions{}, nil, build)
	if err == nil || !strings.Contains(err.Error(), "Missing purpose string in Info.plist (90683)") {
		t.Fatalf("failed upload: %v", err)
	}

	// A part refused for good: the upload deleted, not left waiting.
	c, f = testASC(t)
	f.failPart = -1
	err = publishIOS(c, testApp(t), &publishOptions{}, nil, build)
	if err == nil || !strings.Contains(err.Error(), "403") || f.count("DELETE /v1/buildUploads/up1") != 1 || f.count("PATCH /v1/buildUploadFiles/file1") != 0 {
		t.Fatalf("failed part: %v; %v", err, f.log)
	}

	// An update needs What's New in each of its languages.
	c, f = testASC(t)
	f.localization = append(f.localization, res("appStoreVersionLocalizations", "l-ja", map[string]any{"locale": "ja", "whatsNew": ""}))
	err = publishIOS(c, testApp(t), &publishOptions{review: true}, map[string]string{"": "New"}, build)
	if err == nil || !strings.Contains(err.Error(), "no What's New in ja") || f.count("PATCH /v1/reviewSubmissions/s1") != 0 {
		t.Fatalf("missing What's New: %v", err)
	}
	// ... which a file per language gives.
	c, f = testASC(t)
	f.localization = append(f.localization, res("appStoreVersionLocalizations", "l-ja", map[string]any{"locale": "ja", "whatsNew": ""}))
	if err = publishIOS(c, testApp(t), &publishOptions{review: true}, map[string]string{"en-US": "New", "ja": "新しい"}, build); err != nil {
		t.Fatal(err)
	}
	if a := f.attrs("PATCH /v1/appStoreVersionLocalizations/l-ja", 0); a["whatsNew"] != "新しい" {
		t.Fatalf("ja %v", a)
	}
}

func TestPublishIOSFile(t *testing.T) {
	c, f := testASC(t)
	ipa := filepath.Join(t.TempDir(), "Notes.ipa")
	writeIPA(t, ipa, map[string]any{"CFBundleIdentifier": "com.example.notes", "CFBundleShortVersionString": "1.2", "CFBundleVersion": "42",
		"UIDeviceFamily": []int{1, 2}, "LSRequiresIPhoneOS": true})
	never := func(*goApp, *buildOptions) (string, error) { t.Fatal("built"); return "", nil }
	if err := publishIOS(c, testApp(t), &publishOptions{file: ipa, noWait: true}, nil, never); err != nil {
		t.Fatal(err)
	}
	if a := f.attrs("POST /v1/buildUploads", 0); a["cfBundleVersion"] != "42" {
		t.Fatalf("%v", a)
	}
	// -no-wait: nothing after the upload.
	if f.uploadPolls != 0 || f.count("GET /v1/builds") != 0 {
		t.Fatalf("waited: %v", f.log)
	}
	// Another app's .ipa.
	app := testApp(t)
	app.cfg.ID = "com.example.other"
	if err := publishIOS(c, app, &publishOptions{file: ipa}, nil, never); err == nil || !strings.Contains(err.Error(), "ryui.json's id is com.example.other") {
		t.Fatal(err)
	}
}

func writeIPA(t *testing.T, path string, info map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("Payload/Notes.app/RyUIApp")
	w.Write([]byte("binary"))
	w, _ = zw.Create("Payload/Notes.app/PlugIns/Share.appex/Info.plist")
	w.Write([]byte(plist(map[string]any{"CFBundleIdentifier": "com.example.notes.share"})))
	w, _ = zw.Create("Payload/Notes.app/Info.plist")
	w.Write([]byte(plist(info)))
	zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadIPAInfo(t *testing.T) {
	ipa := filepath.Join(t.TempDir(), "a.ipa")
	writeIPA(t, ipa, map[string]any{"CFBundleIdentifier": "com.x.y", "CFBundleShortVersionString": "2.0", "CFBundleVersion": "17",
		"CFBundleIcons": map[string]any{"CFBundlePrimaryIcon": map[string]any{"CFBundleIconName": "AppIcon"}}, "Z": "z"})
	info, err := readIPAInfo(ipa)
	if err != nil || info != (ipaInfo{"com.x.y", "2.0", "17"}) {
		t.Fatalf("%+v %v", info, err)
	}
	writeIPA(t, ipa, map[string]any{"CFBundleIdentifier": "com.x.y"})
	if _, err := readIPAInfo(ipa); err == nil {
		t.Fatal("an .ipa without its version")
	}
}

func TestASCRetries(t *testing.T) {
	c, _ := testASC(t)
	tries := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tries++
		if r.Method == http.MethodPost || tries < 3 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(map[bool]int{true: 503, false: 429}[tries%2 == 1])
			w.Write([]byte(`{"errors":[{"status":"503","code":"UNAVAILABLE","title":"Down","detail":"try later"}]}`))
			return
		}
		w.Write([]byte(`{"data":{"type":"apps","id":"a","attributes":{"name":"N"}}}`))
	}))
	defer srv.Close()
	c.base = srv.URL
	var waits []time.Duration
	c.sleep = func(d time.Duration) { waits = append(waits, d) }
	var a ascOne[ascApp]
	if err := c.do(http.MethodGet, "/v1/apps/a", nil, &a); err != nil || a.Data.Attributes.Name != "N" || tries != 3 {
		t.Fatalf("%v %+v after %d tries", err, a, tries)
	}
	if len(waits) != 2 || waits[0] != 7*time.Second {
		t.Fatalf("waits %v", waits)
	}
	// A POST isn't sent again after the server failed (503), only when
	// rate limited (429).
	tries = 0
	err := c.do(http.MethodPost, "/v1/x", map[string]any{}, nil)
	if err == nil || tries != 1 || !strings.Contains(err.Error(), "try later (UNAVAILABLE)") {
		t.Fatalf("%v after %d tries", err, tries)
	}
}

func TestASCErrorText(t *testing.T) {
	e := &ascError{method: "PATCH", path: "/v1/reviewSubmissions/s1", status: 409}
	json.Unmarshal([]byte(`{"errors":[{"status":"409","code":"STATE_ERROR.ENTITY_STATE_INVALID","title":"t","detail":"The submission isn't complete.",
		"meta":{"associatedErrors":{"/v1/appStoreVersions/v1":[{"code":"ENTITY_ERROR.ATTRIBUTE.REQUIRED","detail":"Screenshots are required."}]}}}]}`), e)
	s := e.Error()
	for _, want := range []string{"409", "The submission isn't complete. (STATE_ERROR.ENTITY_STATE_INVALID)", "Screenshots are required. (ENTITY_ERROR.ATTRIBUTE.REQUIRED)"} {
		if !strings.Contains(s, want) {
			t.Fatalf("%q lacks %q", s, want)
		}
	}
	if !e.has("STATE_ERROR") || e.has("ENTITY_ERROR") {
		t.Fatal("has")
	}
}
