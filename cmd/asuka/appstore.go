package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// App Store Connect's API: uploading a build (buildUploads, as Xcode
// and altool do), waiting for Apple to process it, and handing it to
// TestFlight's groups and to App Review.

// ascKey is an App Store Connect API key: a team key (with its issuer)
// or an individual one (no issuer).
type ascKey struct {
	id, issuer string
	key        *ecdsa.PrivateKey
}

const ascKeyHelp = `publishing to App Store Connect takes an API key: make one there (Users and Access › Integrations › App Store Connect API, with the App Manager role), then name it in asuka.json
  "ios": {"appStoreConnect": {"keyId": "ABC123DEF4", "issuerId": "57246542-96fe-1a63-e053-0824d011072a", "key": "AuthKey_ABC123DEF4.p8"}}
or in the environment: ASUKA_ASC_KEY_ID, ASUKA_ASC_ISSUER_ID and ASUKA_ASC_KEY (the .p8's path, or its text). Keep the .p8 out of your repository`

// loadASCKey finds the app's App Store Connect key: in the environment,
// else asuka.json, its .p8 where they say or, as altool looks for it,
// AuthKey_<id>.p8 in a private_keys directory.
func loadASCKey(cfg *appConfig) (*ascKey, error) {
	a := cfg.IOS.AppStoreConnect
	id := firstOf(os.Getenv("ASUKA_ASC_KEY_ID"), a.KeyID)
	issuer := firstOf(os.Getenv("ASUKA_ASC_ISSUER_ID"), a.IssuerID)
	src := firstOf(os.Getenv("ASUKA_ASC_KEY"), a.Key)
	if id == "" {
		return nil, errors.New(ascKeyHelp)
	}
	var data []byte
	switch {
	case strings.Contains(src, "-----BEGIN"):
		data = []byte(src)
	case src != "":
		var err error
		if data, err = os.ReadFile(src); err != nil {
			return nil, fmt.Errorf("the App Store Connect key: %v", err)
		}
	default:
		home, _ := os.UserHomeDir()
		dirs := []string{filepath.Join(cfg.dir, "private_keys"), filepath.Join(home, "private_keys"),
			filepath.Join(home, ".private_keys"), filepath.Join(home, ".appstoreconnect", "private_keys")}
		if d := os.Getenv("API_PRIVATE_KEYS_DIR"); d != "" {
			dirs = append(dirs, d)
		}
		for _, d := range dirs {
			for _, name := range []string{"AuthKey_" + id + ".p8", "ApiKey_" + id + ".p8"} {
				if b, err := os.ReadFile(filepath.Join(d, name)); err == nil {
					data = b
					break
				}
			}
			if data != nil {
				break
			}
		}
		if data == nil {
			return nil, fmt.Errorf("no AuthKey_%s.p8 in %s; name it in asuka.json (ios.appStoreConnect.key) or ASUKA_ASC_KEY", id, strings.Join(dirs, ", "))
		}
	}
	key, err := parseP8(data)
	if err != nil {
		return nil, fmt.Errorf("the App Store Connect key %s: %v", id, err)
	}
	return &ascKey{id: id, issuer: issuer, key: key}, nil
}

// parseP8 reads an App Store Connect key: a P-256 key in PKCS #8 PEM.
func parseP8(data []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(bytes.TrimSpace(data))
	if block == nil {
		return nil, errors.New("not a .p8 key (PEM)")
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ec, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("a %T, not an App Store Connect key (ECDSA)", k)
	}
	return ec, nil
}

type ascClient struct {
	storeHTTP
	base    string
	key     *ascKey
	token   string
	tokenAt time.Time
	now     func() time.Time
	// poll is the wait between looks at a build Apple is processing.
	poll time.Duration
	// parallel is how many parts of a build upload at once.
	parallel int
}

func newASC(key *ascKey, verbose bool) *ascClient {
	return &ascClient{storeHTTP: newStoreHTTP(verbose), base: "https://api.appstoreconnect.apple.com",
		key: key, now: time.Now, poll: 30 * time.Second, parallel: 4}
}

// bearer is the request's token: a JWT good for 20 minutes, made anew
// every 10.
func (c *ascClient) bearer() (string, error) {
	now := c.now()
	if c.token != "" && now.Sub(c.tokenAt) < 10*time.Minute {
		return c.token, nil
	}
	claims := map[string]any{"aud": "appstoreconnect-v1", "iat": now.Unix(), "exp": now.Add(19 * time.Minute).Unix()}
	if c.key.issuer != "" {
		claims["iss"] = c.key.issuer
	} else {
		claims["sub"] = "user" // an individual key
	}
	t, err := signJWT(map[string]any{"alg": "ES256", "kid": c.key.id, "typ": "JWT"}, claims, c.key.key)
	if err != nil {
		return "", err
	}
	c.token, c.tokenAt = t, now
	return t, nil
}

// ascError is App Store Connect's answer to a request it refused.
type ascError struct {
	method, path string
	status       int
	Errors       []ascErrorItem `json:"errors"`
}

type ascErrorItem struct {
	Status string `json:"status"`
	Code   string `json:"code"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Meta   struct {
		// AssociatedErrors are what a submission lacks, by resource.
		AssociatedErrors map[string][]ascErrorItem `json:"associatedErrors"`
	} `json:"meta"`
}

func (e *ascError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "App Store Connect: %s %s: %d", e.method, e.path, e.status)
	var write func(items []ascErrorItem, indent string)
	write = func(items []ascErrorItem, indent string) {
		for _, it := range items {
			msg := firstOf(it.Detail, it.Title)
			fmt.Fprintf(&b, "\n%s%s (%s)", indent, msg, it.Code)
			for _, more := range it.Meta.AssociatedErrors {
				write(more, indent+"  ")
			}
		}
	}
	write(e.Errors, "  ")
	return b.String()
}

// has reports whether one of the errors' codes starts with code.
func (e *ascError) has(code string) bool {
	for _, it := range e.Errors {
		if strings.HasPrefix(it.Code, code) {
			return true
		}
	}
	return false
}

// do sends a request: body (unless nil) as JSON, the response's JSON
// into out (unless nil). A POST is tried again only when rate limited,
// as the server didn't take it; the others when the server or the
// network failed too.
func (c *ascClient) do(method, path string, body, out any) error {
	var data []byte
	if body != nil {
		var err error
		if data, err = json.Marshal(body); err != nil {
			return err
		}
	}
	u := path
	if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
		u = c.base + path
	}
	short, _, _ := strings.Cut(strings.TrimPrefix(u, c.base), "?")
	idempotent := method != http.MethodPost
	return c.retry(func() error {
		tok, err := c.bearer()
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
		if c.verbose {
			fmt.Println("asuka:", method, u)
		}
		resp, err := c.client.Do(req)
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
			e := &ascError{method: method, path: short, status: resp.StatusCode}
			json.Unmarshal(raw, e)
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

// JSON:API's documents.

type ascRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type ascRes[A any] struct {
	Type          string `json:"type"`
	ID            string `json:"id"`
	Attributes    A      `json:"attributes"`
	Relationships map[string]struct {
		Data json.RawMessage `json:"data"`
	} `json:"relationships"`
}

// related is the ID of a to-one relationship, "" if none (or not
// included).
func (r ascRes[A]) related(name string) string {
	var ref ascRef
	if rel, ok := r.Relationships[name]; ok && json.Unmarshal(rel.Data, &ref) == nil {
		return ref.ID
	}
	return ""
}

type ascOne[A any] struct {
	Data ascRes[A] `json:"data"`
}

type ascMany[A any] struct {
	Data  []ascRes[A] `json:"data"`
	Links struct {
		Next string `json:"next"`
	} `json:"links"`
}

// ascList gets a list, following its pages: all of them, or at most
// pages.
func ascList[A any](c *ascClient, path string, pages int) ([]ascRes[A], error) {
	var all []ascRes[A]
	for n := 0; path != "" && (pages == 0 || n < pages); n++ {
		var page ascMany[A]
		if err := c.do(http.MethodGet, path, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		path = page.Links.Next
	}
	return all, nil
}

// query is a URL's query from name, value pairs.
func query(kv ...string) string {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return "?" + v.Encode()
}

// data is a request's document: {"data": {"type": typ, "id": id,
// "attributes": attrs, "relationships": rels}}, without what's empty.
func data(typ, id string, attrs map[string]any, rels map[string]any) map[string]any {
	d := map[string]any{"type": typ}
	if id != "" {
		d["id"] = id
	}
	if attrs != nil {
		d["attributes"] = attrs
	}
	if rels != nil {
		d["relationships"] = rels
	}
	return map[string]any{"data": d}
}

// to is a to-one relationship.
func to(typ, id string) map[string]any { return map[string]any{"data": ascRef{typ, id}} }

// The resources' attributes.

type ascApp struct {
	BundleID      string `json:"bundleId"`
	Name          string `json:"name"`
	PrimaryLocale string `json:"primaryLocale"`
}

type ascBuild struct {
	Version         string `json:"version"`
	ProcessingState string `json:"processingState"`
}

type ascStateDetail struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

type ascBuildUpload struct {
	CFBundleVersion string `json:"cfBundleVersion"`
	State           struct {
		State    string           `json:"state"`
		Errors   []ascStateDetail `json:"errors"`
		Warnings []ascStateDetail `json:"warnings"`
	} `json:"state"`
}

type ascUploadOp struct {
	Method         string `json:"method"`
	URL            string `json:"url"`
	Offset         int64  `json:"offset"`
	Length         int64  `json:"length"`
	RequestHeaders []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"requestHeaders"`
}

type ascUploadFile struct {
	UploadOperations []ascUploadOp `json:"uploadOperations"`
}

type ascBetaGroup struct {
	Name                 string `json:"name"`
	IsInternalGroup      bool   `json:"isInternalGroup"`
	HasAccessToAllBuilds bool   `json:"hasAccessToAllBuilds"`
}

type ascLocalization struct {
	Locale   string `json:"locale"`
	WhatsNew string `json:"whatsNew"`
}

type ascBetaDetail struct {
	InternalBuildState string `json:"internalBuildState"`
	ExternalBuildState string `json:"externalBuildState"`
}

type ascVersion struct {
	VersionString   string `json:"versionString"`
	AppVersionState string `json:"appVersionState"`
}

type ascSubmission struct {
	State string `json:"state"`
}

// The publishing steps.

// findApp is the app with the bundle ID.
func (c *ascClient) findApp(bundleID string) (ascRes[ascApp], error) {
	apps, err := ascList[ascApp](c, "/v1/apps"+query("filter[bundleId]", bundleID, "fields[apps]", "bundleId,name,primaryLocale", "limit", "200"), 0)
	if err != nil {
		return ascRes[ascApp]{}, err
	}
	for _, a := range apps {
		if a.Attributes.BundleID == bundleID {
			return a, nil
		}
	}
	return ascRes[ascApp]{}, fmt.Errorf("App Store Connect has no app %s (or the API key can't see it): add it (Apps › + › New App) with that bundle ID, then publish again", bundleID)
}

// buildNumber reads a build number: its first number ("12" of "12" or
// "12.3"). A greater first number is a greater build, which is all the
// next build needs.
func buildNumber(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// highestBuild is the app's highest build number, of its builds and of
// the uploads not yet builds.
func (c *ascClient) highestBuild(appID string) (int, error) {
	builds, err := ascList[ascBuild](c, "/v1/builds"+query("filter[app]", appID, "sort", "-uploadedDate", "limit", "200", "fields[builds]", "version"), 1)
	if err != nil {
		return 0, err
	}
	uploads, err := ascList[ascBuildUpload](c, "/v1/apps/"+appID+"/buildUploads"+query("sort", "-uploadedDate", "limit", "200", "fields[buildUploads]", "cfBundleVersion"), 1)
	if err != nil {
		return 0, err
	}
	high := 0
	for _, b := range builds {
		high = max(high, buildNumber(b.Attributes.Version))
	}
	for _, u := range uploads {
		high = max(high, buildNumber(u.Attributes.CFBundleVersion))
	}
	return high, nil
}

// closedStates are a version's states that take no more builds: approved,
// or released.
var closedStates = []string{"ACCEPTED", "PENDING_APPLE_RELEASE", "PENDING_DEVELOPER_RELEASE",
	"PROCESSING_FOR_DISTRIBUTION", "READY_FOR_DISTRIBUTION", "REPLACED_WITH_NEW_VERSION"}

func stateText(s string) string { return strings.ToLower(strings.ReplaceAll(s, "_", " ")) }

// checkVersionOpen fails if the version takes no more builds, before a
// build is made for it.
func (c *ascClient) checkVersionOpen(appID, version string) error {
	vs, err := ascList[ascVersion](c, "/v1/apps/"+appID+"/appStoreVersions"+query("filter[platform]", "IOS",
		"filter[versionString]", version, "fields[appStoreVersions]", "versionString,appVersionState"), 0)
	if err != nil {
		return err
	}
	for _, v := range vs {
		if slices.Contains(closedStates, v.Attributes.AppVersionState) {
			return fmt.Errorf("version %s is %s on App Store Connect, which takes no more builds of it: raise asuka.json's version",
				version, stateText(v.Attributes.AppVersionState))
		}
	}
	return nil
}

// betaGroups are the app's TestFlight groups of these names.
func (c *ascClient) betaGroups(appID string, names []string) ([]ascRes[ascBetaGroup], error) {
	if len(names) == 0 {
		return nil, nil
	}
	all, err := ascList[ascBetaGroup](c, "/v1/apps/"+appID+"/betaGroups"+query("limit", "200",
		"fields[betaGroups]", "name,isInternalGroup,hasAccessToAllBuilds"), 0)
	if err != nil {
		return nil, err
	}
	var have []string
	for _, g := range all {
		have = append(have, fmt.Sprintf("%q", g.Attributes.Name))
	}
	var out []ascRes[ascBetaGroup]
	for _, name := range names {
		var found []ascRes[ascBetaGroup]
		for _, g := range all {
			if strings.EqualFold(strings.TrimSpace(g.Attributes.Name), name) {
				found = append(found, g)
			}
		}
		switch len(found) {
		case 0:
			msg := fmt.Sprintf("no TestFlight group %q", name)
			if len(have) > 0 {
				return nil, fmt.Errorf("%s; the app's are %s", msg, strings.Join(have, ", "))
			}
			return nil, fmt.Errorf("%s: the app has none yet; make one in App Store Connect (TestFlight)", msg)
		case 1:
			out = append(out, found[0])
		default:
			return nil, fmt.Errorf("%d TestFlight groups are named %q: rename one", len(found), name)
		}
	}
	return out, nil
}

// upload sends the .ipa to App Store Connect, which processes it into a
// build: the upload's ID.
func (c *ascClient) upload(appID, path, version, build string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	var up ascOne[ascBuildUpload]
	if err := c.do(http.MethodPost, "/v1/buildUploads", data("buildUploads", "", map[string]any{
		"cfBundleShortVersionString": version, "cfBundleVersion": build, "platform": "IOS",
	}, map[string]any{"app": to("apps", appID)}), &up); err != nil {
		return "", err
	}
	var file ascOne[ascUploadFile]
	if err := c.do(http.MethodPost, "/v1/buildUploadFiles", data("buildUploadFiles", "", map[string]any{
		"fileName": filepath.Base(path), "fileSize": st.Size(), "uti": "com.apple.ipa", "assetType": "ASSET",
	}, map[string]any{"buildUpload": to("buildUploads", up.Data.ID)}), &file); err != nil {
		return "", err
	}
	fmt.Printf("asuka: uploading %s (%s)\n", filepath.Base(path), mb(st.Size()))
	start := c.now()
	err = c.sendParts(f, st.Size(), file.Data.Attributes.UploadOperations)
	if err == nil {
		err = c.do(http.MethodPatch, "/v1/buildUploadFiles/"+file.Data.ID, data("buildUploadFiles", file.Data.ID,
			map[string]any{"uploaded": true}, nil), nil)
		if err != nil {
			// Taken, with the answer lost? The upload says.
			var u ascOne[ascBuildUpload]
			if c.do(http.MethodGet, "/v1/buildUploads/"+up.Data.ID, nil, &u) == nil &&
				(u.Data.Attributes.State.State == "PROCESSING" || u.Data.Attributes.State.State == "COMPLETE") {
				err = nil
			}
		}
	}
	if err != nil {
		// Not left waiting for the rest of a file that won't come.
		c.do(http.MethodDelete, "/v1/buildUploads/"+up.Data.ID, nil, nil)
		return "", err
	}
	fmt.Printf("asuka: uploaded in %.0fs\n", c.now().Sub(start).Seconds())
	return up.Data.ID, nil
}

// sendParts sends the file's parts where App Store Connect said, a few
// at a time, each tried again if it fails.
func (c *ascClient) sendParts(f *os.File, size int64, ops []ascUploadOp) error {
	if len(ops) == 0 {
		return errors.New("App Store Connect gave nowhere to upload the build")
	}
	var total int64
	for i, op := range ops {
		if op.URL == "" || op.Offset < 0 || op.Length <= 0 || op.Offset+op.Length > size {
			return fmt.Errorf("App Store Connect's upload part %d (%d bytes at %d) isn't of the file (%d bytes)", i, op.Length, op.Offset, size)
		}
		total += op.Length
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var (
		mu      sync.Mutex
		sent    int64
		shown   int64
		firstEr error
		wg      sync.WaitGroup
	)
	jobs := make(chan ascUploadOp)
	for range min(max(c.parallel, 1), len(ops)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for op := range jobs {
				err := c.sendPart(ctx, f, op)
				mu.Lock()
				if err != nil {
					if firstEr == nil {
						firstEr = err
						cancel()
					}
				} else {
					sent += op.Length
					// Each fifth.
					if pct := sent * 100 / total; pct/20 > shown/20 && pct < 100 {
						shown = pct
						fmt.Printf("asuka: %d%%\n", pct)
					}
				}
				mu.Unlock()
			}
		}()
	}
	for _, op := range ops {
		select {
		case jobs <- op:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	return firstEr
}

func (c *ascClient) sendPart(ctx context.Context, f *os.File, op ascUploadOp) error {
	method := strings.ToUpper(firstOf(op.Method, http.MethodPut))
	return c.retry(func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		// A stalled connection fails the part, which is tried again.
		pctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		req, err := http.NewRequestWithContext(pctx, method, op.URL, io.NewSectionReader(f, op.Offset, op.Length))
		if err != nil {
			return err
		}
		req.ContentLength = op.Length
		for _, h := range op.RequestHeaders {
			req.Header.Set(h.Name, h.Value)
		}
		resp, err := c.client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if transient(err) || errors.Is(err, context.DeadlineExceeded) {
				return &errRetry{err: fmt.Errorf("uploading the part at %d: %v", op.Offset, err)}
			}
			return err
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 300 {
			err := fmt.Errorf("uploading the part at %d: %s", op.Offset, resp.Status)
			if retryable(resp.StatusCode, true) {
				return &errRetry{err: err, wait: retryAfter(resp.Header)}
			}
			return err
		}
		return nil
	})
}

// details lists App Store Connect's errors or warnings of an upload.
func details(ds []ascStateDetail) string {
	var b strings.Builder
	for _, d := range ds {
		fmt.Fprintf(&b, "\n  %s", d.Description)
		if d.Code != "" {
			fmt.Fprintf(&b, " (%s)", d.Code)
		}
	}
	return b.String()
}

// processed waits for Apple to process the upload into a build, and for
// the build to be valid: its ID.
func (c *ascClient) processed(appID, uploadID, version, build string) (string, error) {
	start := c.now()
	report := start
	waiting := func() {
		if now := c.now(); now.Sub(report) >= 5*time.Minute {
			report = now
			fmt.Printf("asuka: still processing (%.0f minutes)\n", now.Sub(start).Minutes())
		}
		c.sleep(c.poll)
	}
	fmt.Println("asuka: App Store Connect is processing the build (minutes, usually; at times an hour or more)")
	warned := false
	buildID := ""
	for buildID == "" {
		var up ascOne[ascBuildUpload]
		if err := c.do(http.MethodGet, "/v1/buildUploads/"+uploadID+query("include", "build"), nil, &up); err != nil {
			return "", err
		}
		s := up.Data.Attributes.State
		if len(s.Warnings) > 0 && !warned {
			warned = true
			fmt.Printf("asuka: App Store Connect warns:%s\n", details(s.Warnings))
		}
		switch s.State {
		case "FAILED":
			return "", fmt.Errorf("App Store Connect turned the build down:%s", details(s.Errors))
		case "COMPLETE":
			buildID = up.Data.related("build")
		}
		if buildID == "" {
			// The build, by its numbers, before the upload links it.
			bs, err := ascList[ascBuild](c, "/v1/builds"+query("filter[app]", appID, "filter[version]", build,
				"filter[preReleaseVersion.version]", version, "filter[preReleaseVersion.platform]", "IOS", "fields[builds]", "version,processingState"), 1)
			if err != nil {
				return "", err
			}
			if len(bs) > 0 {
				buildID = bs[0].ID
			}
		}
		if buildID == "" {
			waiting()
		}
	}
	for {
		var b ascOne[ascBuild]
		if err := c.do(http.MethodGet, "/v1/builds/"+buildID+query("fields[builds]", "version,processingState"), nil, &b); err != nil {
			return "", err
		}
		switch b.Data.Attributes.ProcessingState {
		case "VALID":
			fmt.Printf("asuka: processed in %.0f minutes\n", c.now().Sub(start).Minutes())
			return buildID, nil
		case "FAILED", "INVALID":
			return "", fmt.Errorf("App Store Connect found build %s %s; Apple emails the account holder why", build, stateText(b.Data.Attributes.ProcessingState))
		}
		waiting()
	}
}

// localeList lists the localizations' locales.
func localeList[A any](ls []ascRes[A], locale func(A) string) string {
	var out []string
	for _, l := range ls {
		out = append(out, locale(l.Attributes))
	}
	return strings.Join(out, ", ")
}

// betaNotes sets the build's What to Test, by locale.
func (c *ascClient) betaNotes(buildID string, notes map[string]string) error {
	have, err := ascList[ascLocalization](c, "/v1/builds/"+buildID+"/betaBuildLocalizations"+query("limit", "50"), 0)
	if err != nil {
		return err
	}
	for _, locale := range slices.Sorted(maps.Keys(notes)) {
		text := notes[locale]
		i := slices.IndexFunc(have, func(l ascRes[ascLocalization]) bool { return strings.EqualFold(l.Attributes.Locale, locale) })
		if i >= 0 {
			err = c.do(http.MethodPatch, "/v1/betaBuildLocalizations/"+have[i].ID, data("betaBuildLocalizations", have[i].ID,
				map[string]any{"whatsNew": text}, nil), nil)
		} else {
			err = c.do(http.MethodPost, "/v1/betaBuildLocalizations", data("betaBuildLocalizations", "",
				map[string]any{"locale": locale, "whatsNew": text}, map[string]any{"build": to("builds", buildID)}), nil)
		}
		if err != nil {
			return fmt.Errorf("TestFlight's What to Test (%s): %v", locale, err)
		}
	}
	return nil
}

// betaDetail is the build's TestFlight states.
func (c *ascClient) betaDetail(buildID string) (ascBetaDetail, error) {
	var d ascOne[ascBetaDetail]
	err := c.do(http.MethodGet, "/v1/builds/"+buildID+"/buildBetaDetail", nil, &d)
	return d.Data.Attributes, err
}

// testFlight gives the build to the groups, and sends it to beta review
// if one is external (testers outside the team).
func (c *ascClient) testFlight(buildID string, groups []ascRes[ascBetaGroup]) error {
	if len(groups) == 0 {
		return nil
	}
	d, err := c.betaDetail(buildID)
	if err != nil {
		return err
	}
	if d.InternalBuildState == "MISSING_EXPORT_COMPLIANCE" {
		return errors.New(`the build has no export compliance: answer for it in App Store Connect (TestFlight), or say in asuka.json what the app's encryption is ("ios": {"nonExemptEncryption": "no"}), then publish again`)
	}
	var add []ascRef
	var names []string
	external := false
	for _, g := range groups {
		if g.Attributes.IsInternalGroup && g.Attributes.HasAccessToAllBuilds {
			fmt.Printf("asuka: %q has every build already\n", g.Attributes.Name)
			continue
		}
		add = append(add, ascRef{"betaGroups", g.ID})
		names = append(names, fmt.Sprintf("%q", g.Attributes.Name))
		external = external || !g.Attributes.IsInternalGroup
	}
	if len(add) > 0 {
		// A build just processed can take a moment to reach TestFlight:
		// "not found" a few times over.
		for i := 1; ; i++ {
			err = c.do(http.MethodPost, "/v1/builds/"+buildID+"/relationships/betaGroups", map[string]any{"data": add}, nil)
			var ae *ascError
			if err == nil || !errors.As(err, &ae) || ae.status != http.StatusNotFound || i == 5 {
				break
			}
			c.sleep(c.poll)
		}
		if err != nil {
			return fmt.Errorf("giving the build to %s: %v", strings.Join(names, ", "), err)
		}
		fmt.Printf("asuka: TestFlight: the build is %s's\n", strings.Join(names, ", "))
	}
	if !external {
		return nil
	}
	if d, err = c.betaDetail(buildID); err != nil {
		return err
	}
	if d.ExternalBuildState != "READY_FOR_BETA_SUBMISSION" {
		fmt.Printf("asuka: TestFlight: for external testers, the build is %s\n", stateText(d.ExternalBuildState))
		return nil
	}
	if err := c.do(http.MethodPost, "/v1/betaAppReviewSubmissions", data("betaAppReviewSubmissions", "", nil,
		map[string]any{"build": to("builds", buildID)}), nil); err != nil {
		return fmt.Errorf("sending the build to TestFlight's beta review (its test information, in App Store Connect › TestFlight, must be filled in): %v", err)
	}
	fmt.Println("asuka: TestFlight: sent to beta review, for the external groups")
	return nil
}

// editableStates are a version's states it can be changed and submitted in.
var editableStates = []string{"PREPARE_FOR_SUBMISSION", "DEVELOPER_REJECTED", "REJECTED", "METADATA_REJECTED", "INVALID_BINARY", "READY_FOR_REVIEW"}

// submitReview makes (or finds) the App Store version, gives it the
// build, its What's New and release, and submits it for review.
func (c *ascClient) submitReview(appID, buildID, version string, notes map[string]string, release string) error {
	all, err := ascList[ascVersion](c, "/v1/apps/"+appID+"/appStoreVersions"+query("filter[platform]", "IOS",
		"fields[appStoreVersions]", "versionString,appVersionState", "limit", "200"), 0)
	if err != nil {
		return err
	}
	first := true
	var v *ascRes[ascVersion]
	for i, x := range all {
		switch {
		case x.Attributes.VersionString == version:
			v = &all[i]
		case slices.Contains([]string{"READY_FOR_DISTRIBUTION", "REPLACED_WITH_NEW_VERSION", "PROCESSING_FOR_DISTRIBUTION"}, x.Attributes.AppVersionState):
			first = false
		}
	}
	attrs := map[string]any{}
	if release != "" {
		attrs["releaseType"] = release
	}
	if v == nil {
		var made ascOne[ascVersion]
		attrs["platform"], attrs["versionString"] = "IOS", version
		if err := c.do(http.MethodPost, "/v1/appStoreVersions", data("appStoreVersions", "", attrs,
			map[string]any{"app": to("apps", appID)}), &made); err != nil {
			return fmt.Errorf("making version %s on the App Store: %v", version, err)
		}
		v = &made.Data
		fmt.Printf("asuka: App Store: version %s made\n", version)
	} else {
		switch s := v.Attributes.AppVersionState; {
		case s == "WAITING_FOR_REVIEW" || s == "IN_REVIEW":
			return fmt.Errorf("version %s is %s already: to send another build, cancel its review in App Store Connect first", version, stateText(s))
		case !slices.Contains(editableStates, s):
			return fmt.Errorf("version %s is %s: raise asuka.json's version", version, stateText(s))
		}
		if len(attrs) > 0 {
			if err := c.do(http.MethodPatch, "/v1/appStoreVersions/"+v.ID, data("appStoreVersions", v.ID, attrs, nil), nil); err != nil {
				return err
			}
		}
	}
	if err := c.do(http.MethodPatch, "/v1/appStoreVersions/"+v.ID+"/relationships/build", to("builds", buildID), nil); err != nil {
		return fmt.Errorf("giving version %s the build: %v", version, err)
	}
	// What's New: an update's, not the first version's.
	if !first {
		locs, err := ascList[ascLocalization](c, "/v1/appStoreVersions/"+v.ID+"/appStoreVersionLocalizations"+query("limit", "50",
			"fields[appStoreVersionLocalizations]", "locale,whatsNew"), 0)
		if err != nil {
			return err
		}
		for _, locale := range slices.Sorted(maps.Keys(notes)) {
			i := slices.IndexFunc(locs, func(l ascRes[ascLocalization]) bool { return strings.EqualFold(l.Attributes.Locale, locale) })
			if i < 0 {
				return fmt.Errorf("version %s has no %s localization for --notes; it has %s", version, locale,
					localeList(locs, func(l ascLocalization) string { return l.Locale }))
			}
			if err := c.do(http.MethodPatch, "/v1/appStoreVersionLocalizations/"+locs[i].ID, data("appStoreVersionLocalizations", locs[i].ID,
				map[string]any{"whatsNew": notes[locale]}, nil), nil); err != nil {
				return fmt.Errorf("the App Store's What's New (%s): %v", locale, err)
			}
			locs[i].Attributes.WhatsNew = notes[locale]
		}
		var empty []string
		for _, l := range locs {
			if strings.TrimSpace(l.Attributes.WhatsNew) == "" {
				empty = append(empty, l.Attributes.Locale)
			}
		}
		if len(empty) > 0 {
			return fmt.Errorf("version %s has no What's New in %s, which an update needs: give it with --notes (@dir: a file per language, as %s.txt)",
				version, strings.Join(empty, ", "), empty[0])
		}
	} else if len(notes) > 0 {
		fmt.Println("asuka: App Store: a first version has no What's New; --notes went to TestFlight only")
	}
	// The submission: the draft one, or the one with issues to answer,
	// else a new one.
	subs, err := ascList[ascSubmission](c, "/v1/reviewSubmissions"+query("filter[app]", appID, "filter[platform]", "IOS",
		"filter[state]", "READY_FOR_REVIEW,UNRESOLVED_ISSUES"), 0)
	if err != nil {
		return err
	}
	var subID string
	if len(subs) > 0 {
		subID = subs[0].ID
	} else {
		var s ascOne[ascSubmission]
		if err := c.do(http.MethodPost, "/v1/reviewSubmissions", data("reviewSubmissions", "", map[string]any{"platform": "IOS"},
			map[string]any{"app": to("apps", appID)}), &s); err != nil {
			return err
		}
		subID = s.Data.ID
	}
	items, err := ascList[struct{}](c, "/v1/reviewSubmissions/"+subID+"/items"+query("include", "appStoreVersion", "limit", "200"), 0)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(items, func(it ascRes[struct{}]) bool { return it.related("appStoreVersion") == v.ID }) {
		if err := c.do(http.MethodPost, "/v1/reviewSubmissionItems", data("reviewSubmissionItems", "", nil, map[string]any{
			"reviewSubmission": to("reviewSubmissions", subID), "appStoreVersion": to("appStoreVersions", v.ID),
		}), nil); err != nil {
			return fmt.Errorf("adding version %s to the review submission: %v", version, err)
		}
	}
	if err := c.do(http.MethodPatch, "/v1/reviewSubmissions/"+subID, data("reviewSubmissions", subID,
		map[string]any{"submitted": true}, nil), nil); err != nil {
		return fmt.Errorf("submitting version %s for review: %v\nthe build is the version's: fill in what it lacks in App Store Connect, and submit it there", version, err)
	}
	fmt.Printf("asuka: App Store: version %s submitted for review\n", version)
	return nil
}
