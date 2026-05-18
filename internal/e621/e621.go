package e621

import (
	"FurLibrarer/internal/fetcher"
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

func init() {
	viper.SetDefault("e621.api_key", "")
	viper.SetDefault("e621.username", "")
	viper.SetDefault("e621.rate_limit", 2)
	viper.SetDefault("e621.base_url", "https://e621.net/")
}

type Client struct {
	log      *zap.Logger
	e6Client *HTTPClient
}

func (c *Client) SourceName() string {
	return "e621"
}

func NewClient(log *zap.Logger) *Client {
	return &Client{log: log, e6Client: NewHTTPClient(Config{
		AppName:           "FurLib",
		AppVersion:        "0.0.1",
		Username:          viper.GetString("e621.username"),
		APIKey:            viper.GetString("e621.api_key"),
		RequestsPerSecond: 2,
		Burst:             2,
		BaseURL:           viper.GetString("e621.base_url"),
	})}
}

func (c *Client) Search(tags []string, limit int) ([]fetcher.MetaData, error) {
	query := url.Values{}
	query.Add("tags", strings.Join(tags, " "))
	query.Add("limit", strconv.Itoa(limit))

	var resp PostsResponse
	err := c.e6Client.Do(context.Background(), http.MethodGet, "/posts.json", query, nil, &resp)
	if err != nil {
		return nil, err
	}

	posts := FlattenPosts(resp)

	c.log.Info("posts", zap.Any("posts", posts))

	return posts, nil
}

func (c *Client) PostByID(id string) (fetcher.MetaData, error) {
	panic("implement me")
}

// TODO: Change this
func FlattenPosts(resp PostsResponse) []fetcher.MetaData {
	out := make([]fetcher.MetaData, 0, len(resp.Posts))

	for _, post := range resp.Posts {
		var tags []string

		tags = append(tags, post.Tags.General...)
		tags = append(tags, post.Tags.Artist...)
		tags = append(tags, post.Tags.Copyright...)
		tags = append(tags, post.Tags.Character...)
		tags = append(tags, post.Tags.Species...)
		tags = append(tags, post.Tags.Meta...)

		var source string
		if len(post.Sources) > 0 {
			source = post.Sources[0]
		}

		var duration time.Duration
		if post.Duration != nil {
			duration = 0
		}

		animated := false

		switch strings.ToLower(post.File.Ext) {
		case "gif", "webm", "mp4":
			animated = true
		}

		sound := false

		switch strings.ToLower(post.File.Ext) {
		case "webm", "mp4":
			sound = true
		}

		out = append(out, fetcher.MetaData{
			ID:          strconv.Itoa(post.ID),
			Source:      source,
			Tags:        tags,
			CreatedDate: post.CreatedAt,
			Size:        uint64(post.File.Size),
			Width:       uint16(post.File.Width),
			Height:      uint16(post.File.Height),
			Filetype:    post.File.Ext,
			Animated:    animated,
			Duration:    duration,
			Sound:       sound,
			Hash:        post.File.MD5,
			Link:        post.File.URL,
		})
	}

	return out
}
