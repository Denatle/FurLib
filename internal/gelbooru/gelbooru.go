package gelbooru

import (
	"FurLib/internal/config"
	"FurLib/internal/fetcher"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

const maxPageSize = 100

type Client struct {
	log     *zap.Logger
	cfg     config.GelbooruConfig
	http    *http.Client
	limiter *rate.Limiter
}

func (c *Client) SourceName() string { return "gelbooru" }

func NewClient(log *zap.Logger, cfg config.GelbooruConfig) *Client {
	rps := cfg.RPS
	if rps <= 0 {
		rps = 3
	}
	return &Client{
		log: log,
		cfg: cfg,
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
					return (&net.Dialer{
						Timeout:   30 * time.Second,
						KeepAlive: 30 * time.Second,
					}).DialContext(ctx, "tcp", addr)
				},
			},
		},
		limiter: rate.NewLimiter(rate.Limit(rps), int(rps)),
	}
}

func (c *Client) baseQuery() url.Values {
	q := url.Values{}
	q.Set("page", "dapi")
	q.Set("s", "post")
	q.Set("q", "index")
	q.Set("json", "1")
	if c.cfg.APIKey != "" {
		q.Set("api_key", c.cfg.APIKey)
	}
	if c.cfg.UserID != "" {
		q.Set("user_id", c.cfg.UserID)
	}
	return q
}

func (c *Client) do(ctx context.Context, query url.Values, out any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}

	baseURL := c.cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://gelbooru.com"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(baseURL, "/")+"/index.php?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "FurLib/0.0.1")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("gelbooru api error: status=%d body=%s",
			resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) Search(tags []string, limit int) ([]fetcher.MetaData, error) {
	var all []fetcher.MetaData

	for page := 0; len(all) < limit; page++ {
		pageSize := limit - len(all)
		if pageSize > maxPageSize {
			pageSize = maxPageSize
		}

		q := c.baseQuery()
		q.Set("tags", strings.Join(tags, " "))
		q.Set("limit", strconv.Itoa(pageSize))
		q.Set("pid", strconv.Itoa(page))

		var resp PostsResponse
		if err := c.do(context.Background(), q, &resp); err != nil {
			return nil, err
		}

		posts := FlattenPosts(resp)
		all = append(all, posts...)

		if len(posts) < pageSize {
			break
		}
	}

	c.log.Info("search done", zap.Int("count", len(all)), zap.Strings("tags", tags))
	return all, nil
}

func (c *Client) PostByID(id string) (fetcher.MetaData, error) {
	q := c.baseQuery()
	q.Set("id", id)

	var resp PostsResponse
	if err := c.do(context.Background(), q, &resp); err != nil {
		return fetcher.MetaData{}, fmt.Errorf("fetch post: %w", err)
	}

	posts := FlattenPosts(resp)
	if len(posts) == 0 {
		return fetcher.MetaData{}, fmt.Errorf("post not found: %s", id)
	}
	return posts[0], nil
}

func normalizeRating(r string) string {
	switch r {
	case "general":
		return "safe"
	case "questionable", "sensitive":
		return "questionable"
	case "explicit":
		return "explicit"
	default:
		return r
	}
}

func FlattenPosts(resp PostsResponse) []fetcher.MetaData {
	out := make([]fetcher.MetaData, 0, len(resp.Posts))

	for _, post := range resp.Posts {
		if post.FileURL == "" {
			continue
		}

		ext := strings.TrimPrefix(filepath.Ext(post.Image), ".")
		if ext == "" {
			ext = strings.TrimPrefix(filepath.Ext(post.FileURL), ".")
		}
		ext = strings.ToLower(ext)

		animated := ext == "gif" || ext == "webm" || ext == "mp4"
		sound := ext == "webm" || ext == "mp4"

		var sources []string
		if post.Source != "" {
			sources = []string{post.Source}
		}

		out = append(out, fetcher.MetaData{
			ID:              strconv.Itoa(post.ID),
			Source:          "gelbooru",
			OriginalSources: sources,
			Tags:            strings.Fields(html.UnescapeString(post.Tags)),
			Author:          html.UnescapeString(post.TagStringArtist),
			CreatedDate:     post.CreatedAt.Time,
			Width:           uint16(post.Width),
			Height:          uint16(post.Height),
			Filetype:        ext,
			Animated:        animated,
			Sound:           sound,
			Hash:            post.MD5,
			Link:            post.FileURL,
			Score:           post.Score,
			Rating:          normalizeRating(post.Rating),
			ExtraHeaders:    map[string]string{"Referer": "https://gelbooru.com"},
		})
	}

	return out
}
