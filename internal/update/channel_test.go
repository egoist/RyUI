package update

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

// Semantic versioning's own example of precedence (semver.org, 11.4).
func TestCompareSemverPrecedence(t *testing.T) {
	order := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0-beta.1", "1.1.0", "2.0.0"}
	for i := range order {
		for j := range order {
			want := 0
			switch {
			case i < j:
				want = -1
			case i > j:
				want = 1
			}
			if got := Compare(order[i], order[j]); got != want {
				t.Errorf("Compare(%q, %q) = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
}

func TestChannel(t *testing.T) {
	for v, want := range map[string]string{
		"1.2.0": "", "v1.2.0": "", "1.2.0+42": "", "1.3.0-beta.2": "beta", "1.3.0-Beta2": "beta",
		"2.0.0-rc.1": "rc", "1.3.0-1": "pre", "1.3.0-nightly.20260101+abc": "nightly",
	} {
		if got := Channel(v); got != want {
			t.Errorf("Channel(%q) = %q, want %q", v, got, want)
		}
	}
	for _, c := range []struct {
		version, channel string
		want             bool
	}{
		{"1.2.0", "", true}, {"1.2.0", "beta", true}, {"1.3.0-beta.1", "", false},
		{"1.3.0-beta.1", "beta", true}, {"1.3.0-rc.1", "beta", false}, {"1.3.0-rc.1", "*", true},
	} {
		if got := OnChannel(c.version, c.channel); got != c.want {
			t.Errorf("OnChannel(%q, %q) = %t", c.version, c.channel, got)
		}
	}
	if got := ManifestName("darwin-universal", ""); got != "update-darwin-universal.json" {
		t.Error(got)
	}
	if got := ManifestName("windows-arm64", "beta"); got != "update-windows-arm64-beta.json" {
		t.Error(got)
	}
}

func TestValidVersion(t *testing.T) {
	for v, want := range map[string]bool{
		"1.0": true, "1.2.3": true, "v1.2.3": true, "1.3.0-beta.1": true, "1.3.0-beta.1+42": true, "1.0.0-x-y": true,
		"": false, "1..2": false, "1.2.x": false, "1.2.3-": false, "1.2.3-beta..1": false, "1.2.3+": false, "1.2.3-b@d": false, "latest": false,
	} {
		if got := ValidVersion(v); got != want {
			t.Errorf("ValidVersion(%q) = %t, want %t", v, got, want)
		}
	}
}

// An app on a channel takes releases and that channel's prereleases, and
// reads a prerelease's channel manifest.
func TestResolveChannelFeed(t *testing.T) {
	rel := func(tag string, pre bool) string {
		return fmt.Sprintf(`{"tag_name": %q, "draft": false, "prerelease": %t}`, tag, pre)
	}
	releases := "[" + strings.Join([]string{rel("v1.4.0-rc.1", true), rel("v1.4.0-beta.3", true), rel("v1.3.1", false)}, ",") + "]"
	get := func(_ context.Context, u string) (io.ReadCloser, error) {
		if strings.HasPrefix(u, "https://api.github.com/repos/me/app/releases?") {
			return io.NopCloser(strings.NewReader(releases)), nil
		}
		return nil, fmt.Errorf("GET %s", u)
	}
	latest := "https://github.com/me/app/releases/latest/download/update-darwin-universal.json"
	for channel, want := range map[string]string{
		"beta":  "https://github.com/me/app/releases/download/v1.4.0-beta.3/update-darwin-universal-beta.json",
		"rc":    "https://github.com/me/app/releases/download/v1.4.0-rc.1/update-darwin-universal-rc.json",
		"alpha": "https://github.com/me/app/releases/download/v1.3.1/update-darwin-universal.json",
		"":      latest, // releases only: the latest release, no API
	} {
		if got, err := ResolveChannelFeed(context.Background(), latest, channel, get); err != nil || got != want {
			t.Errorf("channel %q: %s, %v; want %s", channel, got, err, want)
		}
	}
}
