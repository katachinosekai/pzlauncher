package workshop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

const acfText = `"AppWorkshop"
{
	"appid"		"108600"
	"NeedsDownload"		"1"
	"WorkshopItemsInstalled"
	{
		"111"
		{
			"size"		"10"
			"manifest"		"2026051810389825471"
		}
	}
	"WorkshopItemDetails"
	{
		"111"
		{
			"manifest"		"2026051810389825471"
			"subscribedby"		"205024453"
		}
		"222"
		{
			"manifest"		"0"
			"subscribedby"		"205024453"
		}
		"444"
		{
			"subscribedby"		"0"
		}
	}
}
`

func TestParseACF(t *testing.T) {
	a, err := ParseACF(acfText)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Installed["111"] || a.Installed["222"] || !a.Subscribed["222"] || a.Subscribed["444"] || !a.NeedsDL {
		t.Fatalf("%+v", a)
	}
	if _, err := ParseACF(`"AppWorkshop" { "a" "b"`); err == nil {
		t.Fatal("want error on unbalanced")
	}
	if _, err := ParseACF(`"Other" { }`); err == nil {
		t.Fatal("want error without AppWorkshop")
	}
}

func fakeSteam(t *testing.T) *httptest.Server {
	cols := map[string][]map[string]any{
		"900": {{"publishedfileid": "111", "filetype": 0}, {"publishedfileid": "901", "filetype": 2}, {"publishedfileid": "222", "filetype": 0}},
		"901": {{"publishedfileid": "333", "filetype": 0}, {"publishedfileid": "444", "filetype": 0}, {"publishedfileid": "900", "filetype": 2}},
	}
	titles := map[string]string{"111": "One", "222": "Two", "333": "Three", "444": "Four"}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.URL.Path {
		case "/col":
			json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"collectiondetails": []any{map[string]any{"children": cols[r.Form.Get("publishedfileids[0]")]}}}})
		case "/det":
			var det []any
			for i := 0; ; i++ {
				id := r.Form.Get("publishedfileids[" + string(rune('0'+i)) + "]")
				if id == "" {
					break
				}
				det = append(det, map[string]any{"publishedfileid": id, "title": titles[id]})
			}
			if r.Form.Get("itemcount") != "3" {
				t.Errorf("itemcount %q", r.Form.Get("itemcount"))
			}
			json.NewEncoder(w).Encode(map[string]any{"response": map[string]any{"publishedfiledetails": det}})
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestCheck(t *testing.T) {
	srv := fakeSteam(t)
	defer srv.Close()
	lib := t.TempDir()
	content := filepath.Join(lib, "steamapps", "workshop", "content", "108600")
	os.MkdirAll(filepath.Join(content, "111", "mods"), 0o755)
	os.MkdirAll(filepath.Join(content, "222"), 0o755)
	os.WriteFile(ACFPathFor(content), []byte(acfText), 0o644)
	if ACFPathFor(content) != filepath.Join(lib, "steamapps", "workshop", "appworkshop_108600.acf") {
		t.Fatal(ACFPathFor(content))
	}
	o := Options{CollectionID: "900", CollectionAPI: srv.URL + "/col", DetailsAPI: srv.URL + "/det", ContentDir: content}
	r, err := Check(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != 4 || r.Installed != 1 || !r.ACFFound || r.TitleErr != nil {
		t.Fatalf("%+v", r)
	}
	got := map[string]Item{}
	for _, m := range r.Missing {
		got[m.ID] = m
	}
	if got["222"].State != Downloading || got["222"].Title != "Two" || got["333"].State != NotSubscribed || got["444"].State != NotSubscribed || got["333"].Title != "Three" {
		t.Fatalf("%+v", r.Missing)
	}
	if r.Missing[len(r.Missing)-1].ID != "222" {
		t.Fatalf("downloading items sort last: %+v", r.Missing)
	}
	os.Remove(ACFPathFor(content))
	r, err = Check(context.Background(), o)
	if err != nil || r.ACFFound || r.Missing[0].State != Unknown {
		t.Fatalf("%+v %v", r, err)
	}
	o.CollectionAPI = srv.URL + "/nope"
	if _, err := Check(context.Background(), o); err == nil {
		t.Fatal("want API error")
	}
}

func TestPageURL(t *testing.T) {
	if PageURL("3825994973") != "steam://url/CommunityFilePage/3825994973" {
		t.Fatal(PageURL("1"))
	}
}
