package buildinfo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"golang.org/x/mod/semver"

	"inferencerig/config"
)

// defaultReleasesAPI lists releases newest-first rather than using
// /releases/latest, which excludes prereleases. scripts/install.sh resolves
// versions the same way, so both agree on what "newest" means.
const defaultReleasesAPI = "https://api.github.com/repos/rivt-ai/inference-rig/releases?per_page=1"

const (
	updateCheckTTL     = 24 * time.Hour
	updateCheckTimeout = 10 * time.Second
)

// updateChecker caches the newest release seen. LatestVersion never blocks on
// the network: GetInfo sits on the UI's poll path, so a slow or unreachable
// GitHub must cost the caller nothing. The refresh runs in the background and
// the next poll picks the answer up.
type updateChecker struct {
	mu        sync.Mutex
	latest    string
	checkedAt time.Time
	inFlight  bool

	apiBase string       // tests point this at an httptest server
	client  *http.Client // tests inject a client
}

var checker updateChecker

// LatestVersion returns the newest release when it is newer than the running
// binary, and "" when up to date, unknown, or checking is disabled.
func LatestVersion() string { return checker.latestVersion() }

func (c *updateChecker) latestVersion() string {
	if !c.enabled() {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.inFlight && time.Since(c.checkedAt) > updateCheckTTL {
		c.inFlight = true
		go c.refresh()
	}
	return c.latest
}

// enabled reports whether checking is allowed at all. Unstamped builds have no
// release to compare against, and the env var is the user's opt out.
func (c *updateChecker) enabled() bool {
	return Version != "dev" && os.Getenv(config.NoUpdateCheckEnv) == ""
}

// refresh fetches once and records the outcome. checkedAt advances even on
// failure, so an unreachable GitHub is retried on the next TTL boundary rather
// than on every single call.
func (c *updateChecker) refresh() {
	latest, err := c.fetch()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.inFlight = false
	c.checkedAt = time.Now()
	if err != nil {
		return // keep whatever we knew before; failures stay invisible to the UI
	}
	c.latest = latest
}

// fetch returns the newest release tag when it beats the running version, else
// "". semver.Compare rather than string comparison, so v0.10.0 sorts above
// v0.9.0 and prereleases order correctly.
func (c *updateChecker) fetch() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := c.http().Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github releases: unexpected status %d", resp.StatusCode)
	}

	var releases []struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return "", err
	}
	if len(releases) == 0 {
		return "", nil
	}
	tag := releases[0].TagName
	if semver.Compare(tag, Version) > 0 {
		return tag, nil
	}
	return "", nil
}

func (c *updateChecker) base() string {
	if c.apiBase != "" {
		return c.apiBase
	}
	return defaultReleasesAPI
}

func (c *updateChecker) http() *http.Client {
	if c.client != nil {
		return c.client
	}
	return &http.Client{Timeout: updateCheckTimeout}
}
