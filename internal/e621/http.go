package e621

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const DefaultBaseURL = "https://e621.net"

type PostsResponse struct {
	Posts []Post `json:"posts"`
}

type Post struct {
	ID          int       `json:"id"`
	Description string    `json:"description"`
	Rating      string    `json:"rating"`
	FavCount    int       `json:"fav_count"`
	File        FileInfo  `json:"file"`
	Preview     Preview   `json:"preview"`
	Sample      Sample    `json:"sample"`
	Tags        Tags      `json:"tags"`
	Score       ScoreInfo `json:"score"`
	Sources     []string  `json:"sources"`
	Duration    *float64  `json:"duration"`
	CreatedAt   time.Time `json:"created_at"`
}

type FileInfo struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Ext    string `json:"ext"`
	Size   int64  `json:"size"`
	MD5    string `json:"md5"`
	URL    string `json:"url"`
}

type Preview struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URL    string `json:"url"`
	Alt    string `json:"alt"`
}

type Sample struct {
	Has    bool   `json:"has"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	URL    string `json:"url"`
	Alt    string `json:"alt"`
}

type ScoreInfo struct {
	Up    int `json:"up"`
	Down  int `json:"down"`
	Total int `json:"total"`
}

type Tags struct {
	General   []string `json:"general"`
	Artist    []string `json:"artist"`
	Copyright []string `json:"copyright"`
	Character []string `json:"character"`
	Species   []string `json:"species"`
	Meta      []string `json:"meta"`
}

// HTTPClient is a lightweight reusable client for e621.
//
// Features:
//   - configurable throttling
//   - automatic User-Agent generation
//   - optional API key auth
//   - generic request helper
//
// e621 requires a descriptive User-Agent.
// Format recommendation:
//
//	AppName/Version (by username on e621)
//
// Example:
//
//	furry-indexer/1.0 (by myuser on e621)
type HTTPClient struct {
	BaseURL   string
	Username  string
	APIKey    string
	UserAgent string

	Client  *http.Client
	Limiter *rate.Limiter
}

type Config struct {
	AppName    string
	AppVersion string

	Username string
	APIKey   string

	// RequestsPerSecond example:
	// 2 = 2 req/sec
	RequestsPerSecond float64

	// Burst size for limiter
	Burst int

	// Optional override
	UserAgent string

	// Optional custom HTTP client
	Client *http.Client

	// Optional base URL
	BaseURL string
}

func NewHTTPClient(cfg Config) *HTTPClient {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}

	httpClient := cfg.Client
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
		}
	}

	ua := cfg.UserAgent
	if ua == "" {
		ua = buildUserAgent(
			cfg.AppName,
			cfg.AppVersion,
			cfg.Username,
		)
	}

	rps := cfg.RequestsPerSecond
	if rps <= 0 {
		rps = 2
	}

	burst := cfg.Burst
	if burst <= 0 {
		burst = 2
	}

	return &HTTPClient{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		Username:  cfg.Username,
		APIKey:    cfg.APIKey,
		UserAgent: ua,
		Client:    httpClient,
		Limiter:   rate.NewLimiter(rate.Limit(rps), burst),
	}
}

func buildUserAgent(app, version, username string) string {
	if app == "" {
		app = "go-e621-client"
	}

	if version == "" {
		version = "dev"
	}

	if username == "" {
		username = "unknown"
	}

	return fmt.Sprintf(
		"%s/%s (by %s on e621)",
		app,
		version,
		username,
	)
}

// Do performs an HTTP request against e621.
func (c *HTTPClient) Do(
	ctx context.Context,
	method string,
	path string,
	query url.Values,
	body any,
	out any,
) error {
	if err := c.Limiter.Wait(ctx); err != nil {
		return fmt.Errorf("rate limiter wait failed: %w", err)
	}

	fullURL := c.BaseURL + path

	if len(query) > 0 {
		fullURL += "?" + query.Encode()
	}

	var bodyReader io.Reader

	if body != nil {
		buf := new(bytes.Buffer)

		if err := json.NewEncoder(buf).Encode(body); err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}

		bodyReader = buf
	}

	req, err := http.NewRequestWithContext(
		ctx,
		method,
		fullURL,
		bodyReader,
	)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// e621 auth
	if c.Username != "" && c.APIKey != "" {
		req.SetBasicAuth(c.Username, c.APIKey)
	}

	resp, err := c.Client.Do(req)
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

		return fmt.Errorf(
			"e621 api error: status=%d body=%s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}

	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	return nil
}
