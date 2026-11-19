package workshop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowseURL(t *testing.T) {
	cases := []struct {
		q    BrowseQuery
		want map[string]string
	}{
		{BrowseQuery{}, map[string]string{"appid": "108600", "browsesort": "trend", "days": "-1", "section": "readytouseitems"}},
		{BrowseQuery{Sort: SortRecent, Text: "  hydro craft "}, map[string]string{"browsesort": "mostrecent", "searchtext": "hydro craft"}},
		{BrowseQuery{Sort: SortTopRated, Page: 3}, map[string]string{"browsesort": "toprated", "p": "3"}},
		{BrowseQuery{Sort: SortUpdated}, map[string]string{"browsesort": "lastupdated"}},
		{BrowseQuery{Sort: SortTrendWeek}, map[string]string{"browsesort": "trend", "days": "7"}},
	}
	for _, c := range cases {
		s := BrowseURL("", c.q)
		if !strings.HasPrefix(s, "https://steamcommunity.com/workshop/browse/?") {
			t.Fatal(s)
		}
		u, _ := url.Parse(s)
		for k, v := range c.want {
			if got := u.Query().Get(k); got != v {
				t.Errorf("%v: %s=%q want %q (%s)", c.q, k, got, v, s)
			}
		}
		if c.q.Page <= 1 && u.Query().Has("p") {
			t.Errorf("page 1 has p: %s", s)
		}
		if c.q.Text == "" && u.Query().Has("searchtext") {
			t.Errorf("empty search has searchtext: %s", s)
		}
	}
	if ItemURL("3825994973") != "https://steamcommunity.com/sharedfiles/filedetails/?id=3825994973" {
		t.Fatal(ItemURL("3825994973"))
	}
	if ItemURL("1&x=2") != "https://steamcommunity.com/sharedfiles/filedetails/?id=1%26x%3D2" {
		t.Fatal(ItemURL("1&x=2"))
	}
	if SortFromName("Top rated") != SortTopRated || SortFromName("nope") != SortPopular || SortRecent.String() != "Most recent" {
		t.Fatal("sort names")
	}
	if !strings.HasPrefix(LoginURL, "https://steamcommunity.com/login/home/") {
		t.Fatal(LoginURL)
	}
}

func TestParseBrowsePage(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "browse.html"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := ParseBrowsePage(b)
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != 74255 || r.Page != 1 || r.TotalPages != 1000 || len(r.Items) != 3 {
		t.Fatalf("%+v", r)
	}
	it := r.Items[0]
	if it.ID != "3840495605" || !strings.HasPrefix(it.Title, "[RKC]Abrams") || it.Size != 69670945 || it.Subscriptions != 7620 || it.Stars != 5 || it.Updated.Unix() != 1795081799 {
		t.Fatalf("%+v", it)
	}
	if !strings.HasPrefix(it.Preview, "https://images.steamusercontent.com/ugc/") || it.Description == "" || len(it.Tags) == 0 {
		t.Fatalf("%+v", it)
	}
	if r.Items[1].Title != "Farming Zone" || r.Items[1].Size != 1603836 {
		t.Fatalf("%+v", r.Items[1])
	}
	for _, bad := range []string{"", "<html>no data</html>", `<script id="valve-ssr-data">{"renderContext":{}}</script>`, `<script id="valve-ssr-data">{"renderContext":{"queryData":"{\"queries\":[]}"}}</script>`} {
		if _, err := ParseBrowsePage([]byte(bad)); err != ErrBrowseFormat {
			t.Errorf("%q: %v", bad, err)
		}
	}
	empty := `<script type="application/json" id="valve-ssr-data">{"renderContext":{"queryData":"{\"queries\":[{\"queryKey\":[\"workshop_browse\",{}],\"state\":{\"data\":{\"current_page\":1,\"total_pages\":0,\"total_count\":0,\"results\":[]}}}]}"}}</script>`
	if r, err := ParseBrowsePage([]byte(empty)); err != nil || len(r.Items) != 0 || r.Total != 0 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestBrowseHTTP(t *testing.T) {
	page, _ := os.ReadFile(filepath.Join("testdata", "browse.html"))
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		if r.Header.Get("User-Agent") == "" {
			t.Error("no user agent")
		}
		if r.URL.Query().Get("searchtext") == "fail" {
			http.Error(w, "nope", http.StatusServiceUnavailable)
			return
		}
		w.Write(page)
	}))
	defer srv.Close()
	r, err := Browse(context.Background(), srv.Client(), srv.URL+"/", BrowseQuery{Text: "farm", Sort: SortRecent, Page: 2})
	if err != nil || len(r.Items) != 3 {
		t.Fatalf("%+v %v", r, err)
	}
	if gotQuery.Get("searchtext") != "farm" || gotQuery.Get("browsesort") != "mostrecent" || gotQuery.Get("p") != "2" {
		t.Fatal(gotQuery)
	}
	if _, err := Browse(context.Background(), srv.Client(), srv.URL+"/", BrowseQuery{Text: "fail"}); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatal(err)
	}
}

func TestParseDetails(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "details.json"))
	if err != nil {
		t.Fatal(err)
	}
	list, err := ParseDetails(b)
	if err != nil || len(list) != 2 {
		t.Fatalf("%+v %v", list, err)
	}
	d := list[0]
	if d.ID != "2409333430" || d.Title != "'91 RANGE ROVER Classic" || d.Size != 18928379 || d.Subscriptions == 0 || !d.Found || d.Updated.Unix() != 1791483143 || len(d.Tags) != 5 {
		t.Fatalf("%+v", d)
	}
	if len([]rune(d.Description)) > 203 || strings.Contains(d.Description, "\n") {
		t.Fatalf("%q", d.Description)
	}
	if list[1].Found {
		t.Fatalf("missing item marked found: %+v", list[1])
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(b) }))
	defer srv.Close()
	m, err := FetchDetails(context.Background(), srv.Client(), srv.URL, []string{"2409333430", "1"})
	if err != nil || len(m) != 1 || m["2409333430"].Preview == "" {
		t.Fatalf("%+v %v", m, err)
	}
	titles, err := FetchTitles(context.Background(), srv.Client(), srv.URL, []string{"2409333430"})
	if err != nil || titles["2409333430"] != "'91 RANGE ROVER Classic" {
		t.Fatalf("%v %v", titles, err)
	}
}

func TestFlexInt(t *testing.T) {
	var v struct {
		A flexInt `json:"a"`
		B flexInt `json:"b"`
		C flexInt `json:"c"`
		D flexInt `json:"d"`
	}
	if err := jsonUnmarshal(`{"a":"12","b":34,"c":null,"d":"x"}`, &v); err != nil || v.A != 12 || v.B != 34 || v.C != 0 || v.D != 0 {
		t.Fatalf("%+v %v", v, err)
	}
}
