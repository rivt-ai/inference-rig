package buildinfo

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// stubReleases serves a releases payload and counts requests, so tests can
// assert not just the answer but whether GitHub was contacted at all.
func stubReleases(t *testing.T, body string, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func releasesJSON(tag string) string { return `[{"tag_name":"` + tag + `"}]` }

// fetch is synchronous, so version comparison is tested without racing the
// background refresh.
func TestFetchComparesSemver(t *testing.T) {
	cases := []struct {
		name    string
		current string
		tag     string
		want    string
	}{
		{"newer release is offered", "v0.1.0", "v0.2.0", "v0.2.0"},
		{"same version is not an update", "v0.1.0", "v0.1.0", ""},
		{"older release is not an update", "v0.2.0", "v0.1.0", ""},
		// The case a string comparison gets wrong: "v0.9.0" > "v0.10.0" lexically.
		{"double digit minor beats single", "v0.9.0", "v0.10.0", "v0.10.0"},
		{"prerelease is older than its release", "v0.2.0", "v0.2.0-rc1", ""},
		{"release beats running prerelease", "v0.2.0-rc1", "v0.2.0", "v0.2.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := stubReleases(t, releasesJSON(tc.tag), http.StatusOK)
			restore := setVersion(t, tc.current)
			defer restore()

			c := &updateChecker{apiBase: server.URL}
			got, err := c.fetch()
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if got != tc.want {
				t.Fatalf("latest = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFetchSurvivesBadResponses(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		status int
	}{
		{"rate limited", `{"message":"API rate limit exceeded"}`, http.StatusForbidden},
		{"server error", "", http.StatusInternalServerError},
		{"malformed json", "not json at all", http.StatusOK},
		{"no releases published", "[]", http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, _ := stubReleases(t, tc.body, tc.status)
			restore := setVersion(t, "v0.1.0")
			defer restore()

			c := &updateChecker{apiBase: server.URL}
			got, _ := c.fetch()
			if got != "" {
				t.Fatalf("latest = %q, want empty on a bad response", got)
			}
		})
	}
}

// A dev build has no release to compare against, so it must not phone home.
func TestDevBuildNeverContactsGitHub(t *testing.T) {
	server, calls := stubReleases(t, releasesJSON("v9.0.0"), http.StatusOK)
	restore := setVersion(t, "dev")
	defer restore()

	c := &updateChecker{apiBase: server.URL}
	if got := c.latestVersion(); got != "" {
		t.Fatalf("latest = %q, want empty for a dev build", got)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("made %d requests, want 0", n)
	}
}

func TestOptOutEnvNeverContactsGitHub(t *testing.T) {
	server, calls := stubReleases(t, releasesJSON("v9.0.0"), http.StatusOK)
	restore := setVersion(t, "v0.1.0")
	defer restore()
	t.Setenv("INFERENCERIG_NO_UPDATE_CHECK", "1")

	c := &updateChecker{apiBase: server.URL}
	if got := c.latestVersion(); got != "" {
		t.Fatalf("latest = %q, want empty when opted out", got)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("made %d requests, want 0", n)
	}
}

// The first call returns empty because the fetch is backgrounded; the value
// lands for the next caller. This is the contract GetInfo relies on to stay off
// the network.
func TestLatestVersionRefreshesInBackground(t *testing.T) {
	server, calls := stubReleases(t, releasesJSON("v0.2.0"), http.StatusOK)
	restore := setVersion(t, "v0.1.0")
	defer restore()

	c := &updateChecker{apiBase: server.URL}
	if got := c.latestVersion(); got != "" {
		t.Fatalf("first call = %q, want empty while the fetch is in flight", got)
	}

	waitFor(t, func() bool { return c.latestVersion() == "v0.2.0" })

	// Cached: the TTL has not elapsed, so no second request goes out.
	c.latestVersion()
	if n := calls.Load(); n != 1 {
		t.Fatalf("made %d requests, want 1 within the TTL", n)
	}
}

// A failed check still advances checkedAt, so an unreachable GitHub is not
// retried on every single call.
func TestFailedCheckDoesNotRetryImmediately(t *testing.T) {
	server, calls := stubReleases(t, "", http.StatusInternalServerError)
	restore := setVersion(t, "v0.1.0")
	defer restore()

	c := &updateChecker{apiBase: server.URL}
	c.latestVersion()
	waitFor(t, func() bool { return calls.Load() == 1 })

	for range 5 {
		c.latestVersion()
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("made %d requests, want 1 after a failure within the TTL", n)
	}
}

func setVersion(t *testing.T, value string) func() {
	t.Helper()
	previous := Version
	Version = value
	return func() { Version = previous }
}

func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}
