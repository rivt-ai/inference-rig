package modelcatalog

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"inferencerig/platform/filedoc"
)

const defaultBaseURL = "https://huggingface.co"

type Source struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
	URL   string `json:"url"`
}

type RemoteFile struct {
	Name      string
	SizeBytes int64
}

type Variant struct {
	Name      string `json:"name"`
	Reference string `json:"reference,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
	MultiFile bool   `json:"multi_file"`
}

type Model struct {
	ID           string    `json:"id"`
	URL          string    `json:"url"`
	Downloads    int64     `json:"downloads,omitempty"`
	Likes        int64     `json:"likes,omitempty"`
	LastModified string    `json:"last_modified,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
	Variants     []Variant `json:"variants"`
}

type SearchRequest struct {
	Backend string
	Query   string
	Limit   int
}

type Result struct {
	Models   []Model `json:"models"`
	CacheHit bool    `json:"cache_hit"`
	Stale    bool    `json:"stale"`
}

type RefreshEvent struct {
	Backend string
	Query   string
	Error   string
}

type ClientOptions struct {
	HTTPClient *http.Client
	BaseURL    string
	CacheDir   string
	CacheTTL   time.Duration
}

// Client owns remote catalog transport, caching, and refresh notifications.
type Client struct {
	// refreshing holds the cache keys with a background refresh already in
	// flight, so a stale entry triggers one refresh rather than one per read.
	refreshing sync.Map
	http       *http.Client
	baseURL    string
	cache      *catalogCache
	broker     *refreshBroker
}

func NewClient(opts ClientOptions) *Client {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		http: client, baseURL: strings.TrimRight(cmp.Or(opts.BaseURL, defaultBaseURL), "/"),
		cache: newCatalogCache(opts.CacheDir, opts.CacheTTL), broker: newRefreshBroker(),
	}
}

func (c *Client) Search(ctx context.Context, req SearchRequest, policy CatalogPolicy) (Result, error) {
	req = normalizeSearch(req)
	if req.Backend == "" {
		return Result{}, errors.New("backend is required")
	}
	if entry, ok := c.cache.load(req); ok {
		entry.Result.CacheHit = true
		entry.Result.Stale = time.Since(entry.UpdatedAt) > c.cache.ttl
		if entry.Result.Stale {
			go c.refresh(req, policy)
		}
		return entry.Result, nil
	}
	return c.fetchAndStore(ctx, req, policy)
}

// fetchAndStore fetches a result and caches it, so the read path and the
// background refresh cannot diverge on whether a fetch updates the cache.
func (c *Client) fetchAndStore(ctx context.Context, req SearchRequest, policy CatalogPolicy) (Result, error) {
	result, err := c.fetch(ctx, req, policy)
	if err == nil {
		err = c.cache.store(req, result)
	}
	return result, err
}

func (c *Client) Subscribe() (<-chan RefreshEvent, func()) { return c.broker.subscribe() }

func (c *Client) refresh(req SearchRequest, policy CatalogPolicy) {
	key := c.cache.path(req)
	if _, busy := c.refreshing.LoadOrStore(key, struct{}{}); busy {
		return
	}
	defer c.refreshing.Delete(key)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	_, err := c.fetchAndStore(ctx, req, policy)
	event := RefreshEvent{Backend: req.Backend, Query: req.Query}
	if err != nil {
		event.Error = err.Error()
	}
	c.broker.publish(event)
}

type remoteModel struct {
	ID           string   `json:"id"`
	ModelID      string   `json:"modelId"`
	Downloads    int64    `json:"downloads"`
	Likes        int64    `json:"likes"`
	LastModified string   `json:"lastModified"`
	Tags         []string `json:"tags"`
	Siblings     []struct {
		Name string `json:"rfilename"`
		Size int64  `json:"size"`
		LFS  *struct {
			Size int64 `json:"size"`
		} `json:"lfs"`
	} `json:"siblings"`
}

func (c *Client) fetch(ctx context.Context, req SearchRequest, policy CatalogPolicy) (Result, error) {
	endpoint, err := url.Parse(c.baseURL + "/api/models")
	if err != nil {
		return Result{}, err
	}
	query := endpoint.Query()
	query.Set("pipeline_tag", "text-generation")
	query.Set("sort", "downloads")
	query.Set("direction", "-1")
	query.Set("limit", strconv.Itoa(req.Limit))
	if filter := policy.SearchFilter(); filter != "" {
		query.Set("filter", filter)
	}
	if req.Query != "" {
		query.Set("search", req.Query)
	}
	endpoint.RawQuery = query.Encode()
	var listed []remoteModel
	if err := c.getJSON(ctx, endpoint.String(), &listed); err != nil {
		return Result{}, err
	}
	result := Result{Models: make([]Model, 0, len(listed))}
	for _, summary := range listed {
		model, ok, err := c.catalogModel(ctx, summary, policy)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			continue
		}
		result.Models = append(result.Models, model)
	}
	sort.Slice(result.Models, func(i, j int) bool {
		if result.Models[i].Downloads != result.Models[j].Downloads {
			return result.Models[i].Downloads > result.Models[j].Downloads
		}
		return result.Models[i].ID < result.Models[j].ID
	})
	return result, nil
}

func (c *Client) catalogModel(ctx context.Context, summary remoteModel, policy CatalogPolicy) (Model, bool, error) {
	id := cmp.Or(summary.ID, summary.ModelID)
	source, ok := c.source(id)
	if !ok {
		return Model{}, false, nil
	}
	var detail remoteModel
	if err := c.getJSON(ctx, c.baseURL+"/api/models/"+source.Owner+"/"+source.Repo+"?blobs=true", &detail); err != nil {
		return Model{}, false, err
	}
	files := make([]RemoteFile, 0, len(detail.Siblings))
	for _, file := range detail.Siblings {
		size := file.Size
		if file.LFS != nil && file.LFS.Size > 0 {
			size = file.LFS.Size
		}
		files = append(files, RemoteFile{Name: file.Name, SizeBytes: size})
	}
	variants, err := policy.Variants(source, files)
	if err != nil || len(variants) == 0 {
		return Model{}, false, err
	}
	tags := detail.Tags
	if len(tags) == 0 {
		tags = summary.Tags
	}
	return Model{
		ID: id, URL: source.URL, Downloads: cmp.Or(detail.Downloads, summary.Downloads),
		Likes:        cmp.Or(detail.Likes, summary.Likes),
		LastModified: cmp.Or(detail.LastModified, summary.LastModified),
		Tags:         append([]string(nil), tags...), Variants: variants,
	}, true, nil
}

func (c *Client) source(id string) (Source, bool) {
	parts := strings.Split(strings.Trim(id, "/"), "/")
	if len(parts) != 2 || !safeSegment(parts[0]) || !safeSegment(parts[1]) {
		return Source{}, false
	}
	return Source{Owner: parts[0], Repo: parts[1], URL: c.baseURL + "/" + path.Join(parts...)}, true
}

func (c *Client) getJSON(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("catalog request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("catalog request: %s", response.Status)
	}
	return json.NewDecoder(response.Body).Decode(target)
}

func normalizeSearch(req SearchRequest) SearchRequest {
	req.Backend = strings.TrimSpace(req.Backend)
	req.Query = strings.TrimSpace(req.Query)
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 100 {
		req.Limit = 100
	}
	return req
}

func safeSegment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\`)
}

type cacheEntry struct {
	UpdatedAt time.Time `json:"updated_at"`
	Result    Result    `json:"result"`
}

type catalogCache struct {
	dir      string
	ttl      time.Duration
	disabled bool
}

func newCatalogCache(dir string, ttl time.Duration) *catalogCache {
	return &catalogCache{dir: filepath.Clean(dir), ttl: ttl, disabled: dir == "" || ttl <= 0}
}

func (c *catalogCache) load(req SearchRequest) (cacheEntry, bool) {
	if c.disabled {
		return cacheEntry{}, false
	}
	data, err := os.ReadFile(c.path(req))
	if err != nil {
		return cacheEntry{}, false
	}
	var entry cacheEntry
	return entry, json.Unmarshal(data, &entry) == nil
}

func (c *catalogCache) store(req SearchRequest, result Result) error {
	if c.disabled {
		return nil
	}
	entry := cacheEntry{UpdatedAt: time.Now().UTC(), Result: result}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	return filedoc.AtomicCreate(c.path(req), data, 0o600)
}

func (c *catalogCache) path(req SearchRequest) string {
	data, _ := json.Marshal(req)
	sum := sha256.Sum256(data)
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])+".json")
}

type refreshBroker struct {
	mu   sync.Mutex
	subs map[chan RefreshEvent]struct{}
}

func newRefreshBroker() *refreshBroker {
	return &refreshBroker{subs: map[chan RefreshEvent]struct{}{}}
}

func (b *refreshBroker) subscribe() (<-chan RefreshEvent, func()) {
	ch := make(chan RefreshEvent, 4)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		if _, subscribed := b.subs[ch]; !subscribed {
			return
		}
		delete(b.subs, ch)
		close(ch)
	}
}

func (b *refreshBroker) publish(event RefreshEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- event:
		default:
		}
	}
}
