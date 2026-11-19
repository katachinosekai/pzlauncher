package workshop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Details struct {
	ID            string
	Title         string
	Preview       string
	Description   string
	Updated       time.Time
	Size          int64
	Subscriptions int64
	Stars         int
	Votes         int64
	Tags          []string
	Found         bool
}

type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	b = bytes.Trim(b, `"`)
	if len(b) == 0 || string(b) == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return nil
	}
	*f = flexInt(n)
	return nil
}

type rawDetails struct {
	ID               string  `json:"publishedfileid"`
	Result           flexInt `json:"result"`
	Title            string  `json:"title"`
	Preview          string  `json:"preview_url"`
	Description      string  `json:"description"`
	ShortDescription string  `json:"short_description"`
	TimeUpdated      flexInt `json:"time_updated"`
	FileSize         flexInt `json:"file_size"`
	Subscriptions    flexInt `json:"subscriptions"`
	StarRating       flexInt `json:"star_rating"`
	TotalVotes       flexInt `json:"total_votes"`
	Tags             []struct {
		Tag string `json:"tag"`
	} `json:"tags"`
}

func (r rawDetails) details() Details {
	d := Details{
		ID:            r.ID,
		Title:         r.Title,
		Preview:       r.Preview,
		Description:   r.ShortDescription,
		Size:          int64(r.FileSize),
		Subscriptions: int64(r.Subscriptions),
		Stars:         int(r.StarRating),
		Votes:         int64(r.TotalVotes),
		Found:         r.Title != "" || r.Preview != "",
	}
	if d.Description == "" {
		d.Description = r.Description
	}
	d.Description = shortText(d.Description, 200)
	if r.TimeUpdated > 0 {
		d.Updated = time.Unix(int64(r.TimeUpdated), 0)
	}
	for _, t := range r.Tags {
		d.Tags = append(d.Tags, t.Tag)
	}
	return d
}

func shortText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return strings.TrimSpace(string(r[:n])) + "..."
	}
	return s
}

func ParseDetails(b []byte) ([]Details, error) {
	var body struct {
		Response struct {
			Details []rawDetails `json:"publishedfiledetails"`
		} `json:"response"`
	}
	if err := json.Unmarshal(b, &body); err != nil {
		return nil, err
	}
	out := make([]Details, 0, len(body.Response.Details))
	for _, r := range body.Response.Details {
		out = append(out, r.details())
	}
	return out, nil
}

func FetchDetails(ctx context.Context, client *http.Client, api string, ids []string) (map[string]Details, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if api == "" {
		api = DefaultDetailsAPI
	}
	out := map[string]Details{}
	for start := 0; start < len(ids); start += 100 {
		end := min(start+100, len(ids))
		form := url.Values{"itemcount": {strconv.Itoa(end - start)}}
		for i, id := range ids[start:end] {
			form.Set(fmt.Sprintf("publishedfileids[%d]", i), id)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, api, strings.NewReader(form.Encode()))
		if err != nil {
			return out, err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := client.Do(req)
		if err != nil {
			return out, err
		}
		b, err := readBody(resp, 32<<20)
		if err != nil {
			return out, fmt.Errorf("details API: %w", err)
		}
		list, err := ParseDetails(b)
		if err != nil {
			return out, err
		}
		for _, d := range list {
			if d.Found {
				out[d.ID] = d
			}
		}
	}
	return out, nil
}
