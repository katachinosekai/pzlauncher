package workshop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	BrowseBase        = "https://steamcommunity.com/workshop/browse/"
	LoginURL          = "https://steamcommunity.com/login/home/?goto=workshop%2Fbrowse%2F%3Fappid%3D108600"
	itemPageBase      = "https://steamcommunity.com/sharedfiles/filedetails/?id="
	browserUserAgent  = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36"
	maxBrowsePageSize = 16 << 20
)

type Sort int

const (
	SortPopular Sort = iota
	SortRecent
	SortTopRated
	SortUpdated
	SortTrendWeek
)

var SortNames = []string{"Most popular", "Most recent", "Top rated", "Recently updated"}

func (s Sort) String() string {
	if s == SortTrendWeek {
		return "Popular this week"
	}
	if int(s) >= 0 && int(s) < len(SortNames) {
		return SortNames[s]
	}
	return "?"
}

func SortFromName(n string) Sort {
	for i, s := range SortNames {
		if s == n {
			return Sort(i)
		}
	}
	return SortPopular
}

type BrowseQuery struct {
	Text string
	Sort Sort
	Page int
}

func ItemURL(id string) string { return itemPageBase + url.QueryEscape(id) }

func BrowseURL(base string, q BrowseQuery) string {
	if base == "" {
		base = BrowseBase
	}
	v := url.Values{}
	v.Set("appid", "108600")
	v.Set("section", "readytouseitems")
	switch q.Sort {
	case SortRecent:
		v.Set("browsesort", "mostrecent")
	case SortTopRated:
		v.Set("browsesort", "toprated")
	case SortUpdated:
		v.Set("browsesort", "lastupdated")
	case SortTrendWeek:
		v.Set("browsesort", "trend")
		v.Set("days", "7")
	default:
		v.Set("browsesort", "trend")
		v.Set("days", "-1")
	}
	if t := strings.TrimSpace(q.Text); t != "" {
		v.Set("searchtext", t)
	}
	if q.Page > 1 {
		v.Set("p", strconv.Itoa(q.Page))
	}
	return base + "?" + v.Encode()
}

type BrowseResult struct {
	Items      []Details
	Page       int
	TotalPages int
	Total      int
}

var ssrData = regexp.MustCompile(`(?s)<script[^>]*\bid="valve-ssr-data"[^>]*>(.*?)</script>`)

var ErrBrowseFormat = errors.New("Steam's Workshop page layout changed; use \"Open in browser\" instead")

func ParseBrowsePage(page []byte) (*BrowseResult, error) {
	m := ssrData.FindSubmatch(page)
	if m == nil {
		return nil, ErrBrowseFormat
	}
	raw := m[1]
	if !json.Valid(raw) {
		raw = []byte(html.UnescapeString(string(raw)))
	}
	var ssr struct {
		RenderContext struct {
			QueryData string `json:"queryData"`
		} `json:"renderContext"`
	}
	if err := json.Unmarshal(raw, &ssr); err != nil || ssr.RenderContext.QueryData == "" {
		return nil, ErrBrowseFormat
	}
	var qd struct {
		Queries []struct {
			QueryKey []json.RawMessage `json:"queryKey"`
			State    struct {
				Data json.RawMessage `json:"data"`
			} `json:"state"`
		} `json:"queries"`
	}
	if err := json.Unmarshal([]byte(ssr.RenderContext.QueryData), &qd); err != nil {
		return nil, ErrBrowseFormat
	}
	for _, q := range qd.Queries {
		if len(q.QueryKey) == 0 {
			continue
		}
		var name string
		if json.Unmarshal(q.QueryKey[0], &name) != nil || name != "workshop_browse" {
			continue
		}
		var data struct {
			CurrentPage flexInt      `json:"current_page"`
			TotalPages  flexInt      `json:"total_pages"`
			TotalCount  flexInt      `json:"total_count"`
			Results     []rawDetails `json:"results"`
		}
		if err := json.Unmarshal(q.State.Data, &data); err != nil {
			return nil, ErrBrowseFormat
		}
		r := &BrowseResult{Page: int(data.CurrentPage), TotalPages: int(data.TotalPages), Total: int(data.TotalCount)}
		for _, it := range data.Results {
			if it.ID == "" {
				continue
			}
			r.Items = append(r.Items, it.details())
		}
		return r, nil
	}
	return nil, ErrBrowseFormat
}

func Browse(ctx context.Context, client *http.Client, base string, q BrowseQuery) (*BrowseResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BrowseURL(base, q), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", browserUserAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	b, err := readBody(resp, maxBrowsePageSize)
	if err != nil {
		return nil, fmt.Errorf("Workshop page: %w", err)
	}
	return ParseBrowsePage(b)
}

func readBody(resp *http.Response, limit int64) ([]byte, error) {
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}
