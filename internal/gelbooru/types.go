package gelbooru

import (
	"encoding/json"
	"fmt"
	"time"
)

// PostsResponse is the top-level JSON envelope from Gelbooru.
// The "post" key is singular even though it's an array.
// When there are no results the key may be absent entirely.
type PostsResponse struct {
	Attributes struct {
		Limit  int `json:"limit"`
		Offset int `json:"offset"`
		Count  int `json:"count"`
	} `json:"@attributes"`
	Posts []Post `json:"post"`
}

type Post struct {
	ID              int          `json:"id"`
	CreatedAt       GelbooruTime `json:"created_at"`
	Score           int          `json:"score"`
	Width           int          `json:"width"`
	Height          int          `json:"height"`
	MD5             string       `json:"md5"`
	Rating          string       `json:"rating"`
	Source          string       `json:"source"`
	Tags            string       `json:"tags"`
	TagStringArtist string       `json:"tag_string_artist"`
	FileURL         string       `json:"file_url"`
	// Image is the bare filename (e.g. "abc123.png") used to derive extension.
	Image string `json:"image"`
}

// GelbooruTime parses Gelbooru's date format: "Tue May 26 12:00:00 +0000 2026".
type GelbooruTime struct{ time.Time }

var timeLayouts = []string{
	"Mon Jan 02 15:04:05 -0700 2006",
	"Mon Jan  2 15:04:05 -0700 2006",
	time.RFC3339,
}

func (t *GelbooruTime) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	for _, layout := range timeLayouts {
		if parsed, err := time.Parse(layout, s); err == nil {
			t.Time = parsed
			return nil
		}
	}
	return fmt.Errorf("gelbooru: cannot parse time %q", s)
}
