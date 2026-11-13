package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	iupdate "github.com/egoist/asuka/internal/update"
)

// check reads the manifest of target on channel from src, and returns its
// version when it's newer than current.
func check(ctx context.Context, src Source, key, current, target, channel string) (*Release, error) {
	body, err := src.Manifest(ctx, target, channel)
	if err != nil {
		return nil, fmt.Errorf("update: checking for updates: %w", err)
	}
	if body == nil {
		return nil, nil
	}
	defer body.Close()
	var m iupdate.Manifest
	if err := json.NewDecoder(io.LimitReader(body, iupdate.MaxManifestSize)).Decode(&m); err != nil {
		return nil, fmt.Errorf("update: checking for updates: %w", err)
	}
	if m.Version == "" || m.URL == "" || m.Signature == "" || m.Size <= 0 {
		return nil, errors.New("update: checking for updates: the manifest is incomplete")
	}
	if iupdate.Compare(m.Version, current) <= 0 || !iupdate.OnChannel(m.Version, channel) {
		return nil, nil
	}
	r := &Release{Version: m.Version, Notes: m.Notes, Size: m.Size, manifest: m, source: src, key: key, current: current}
	r.Date, _ = time.Parse(time.RFC3339, m.Date)
	if d := m.Delta(current); d != nil && d.Size > 0 {
		r.Size = d.Size
	}
	return r, nil
}

// feedSource is the feed asuka build builds in (buildFeed):
//
//   - the URL of a target's manifest, update-<target>.json in a directory
//     (asuka.json's updates.url: an S3 bucket, a CDN): a channel's
//     prereleases are its update-<target>-<channel>.json beside it, and
//     the newer of the two is the answer;
//   - a feed of GitHub releases (updates.github): the latest release's
//     manifest, or with a channel the newest release or prerelease of it
//     the GitHub API lists (internal/update.ResolveChannelFeed);
//   - a custom server's URL (updates.feed), with {target}, {version} (the
//     running one) and {channel} filled in: it answers with a manifest,
//     or 204 No Content (or 404) when there's nothing newer.
type feedSource string

func (f feedSource) Manifest(ctx context.Context, target, channel string) (io.ReadCloser, error) {
	feed := string(f)
	switch {
	case strings.HasPrefix(feed, "https://github.com/"):
		u, err := iupdate.ResolveChannelFeed(ctx, feed, channel, httpGet)
		if err != nil {
			return nil, err
		}
		return httpGet(ctx, u)
	case strings.Contains(feed, "{target}") || strings.Contains(feed, "{version}") || strings.Contains(feed, "{channel}"):
		u := strings.NewReplacer("{target}", url.QueryEscape(target), "{version}", url.QueryEscape(Version()), "{channel}", url.QueryEscape(channel)).Replace(feed)
		body, err := httpGet(ctx, u)
		if errors.Is(err, errNoContent) || errors.Is(err, errNotFound) {
			return nil, nil
		}
		return body, err
	}
	if channel == "" {
		return httpGet(ctx, feed)
	}
	// The release's manifest and the channel's: the newer.
	base, _ := strings.CutSuffix(feed, ".json")
	var best *iupdate.Manifest
	var bestBody []byte
	for i, u := range []string{feed, base + "-" + channel + ".json"} {
		if channel == "*" && i == 1 {
			break // no manifest of every channel: the release's
		}
		body, err := httpGet(ctx, u)
		if i == 1 && errors.Is(err, errNotFound) {
			break // no prerelease on that channel yet
		}
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(body, iupdate.MaxManifestSize))
		body.Close()
		if err != nil {
			return nil, err
		}
		var m iupdate.Manifest
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, fmt.Errorf("reading %s: %w", u, err)
		}
		if best == nil || iupdate.Compare(m.Version, best.Version) > 0 {
			best, bestBody = &m, b
		}
	}
	return io.NopCloser(strings.NewReader(string(bestBody))), nil
}

func (f feedSource) Open(ctx context.Context, u string) (io.ReadCloser, error) {
	return httpGet(ctx, u)
}

var (
	errNotFound  = errors.New("not found")
	errNoContent = errors.New("no content")
)

// httpGet fetches a URL over HTTPS (or plain HTTP from the loopback
// interface, for tests and a server on this machine), through the
// environment's proxy (HTTPS_PROXY). A failure a moment may mend (a
// gateway timing out in front of a release's assets, a rate limit) is
// tried again (httpRetries).
func httpGet(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback(u.Hostname())) {
		return nil, fmt.Errorf("refusing to download %s: updates need HTTPS", rawURL)
	}
	for try := 0; ; try++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		// Not the app's name: GitHub's release assets come from a store
		// that refuses a User-Agent with bytes outside ASCII.
		req.Header.Set("User-Agent", "asuka-updater")
		resp, err := httpClient.Do(req)
		var wait time.Duration
		switch {
		case err != nil:
			if ctx.Err() != nil || try >= len(httpRetries) {
				return nil, err
			}
		case resp.StatusCode == http.StatusOK:
			return resp.Body, nil
		case resp.StatusCode == http.StatusNoContent:
			resp.Body.Close()
			return nil, errNoContent
		case resp.StatusCode == http.StatusNotFound:
			resp.Body.Close()
			return nil, fmt.Errorf("GET %s: %w", rawURL, errNotFound)
		default:
			resp.Body.Close()
			if !transientStatus(resp.StatusCode) || try >= len(httpRetries) {
				return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
			}
			wait = retryAfter(resp.Header.Get("Retry-After"))
		}
		wait = max(wait, httpRetries[try])
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
}

var httpClient = &http.Client{Transport: &http.Transport{
	Proxy:               http.ProxyFromEnvironment,
	TLSHandshakeTimeout: 30 * time.Second,
	IdleConnTimeout:     90 * time.Second,
}}

// httpRetries are how long httpGet waits before each try after the first
// of a request that failed for a while: three tries in all.
var httpRetries = []time.Duration{time.Second, 3 * time.Second}

// transientStatus reports whether a response of status code may succeed
// when asked again.
func transientStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryAfter is the wait of a Retry-After header in seconds, up to 30
// seconds; 0 for none, or for a date.
func retryAfter(h string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(h))
	if err != nil || n <= 0 {
		return 0
	}
	return min(time.Duration(n)*time.Second, 30*time.Second)
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
