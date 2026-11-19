package workshop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

const subsACF = `"AppWorkshop"
{
	"appid"		"108600"
	"NeedsUpdate"		"1"
	"WorkshopItemsInstalled"
	{
		"100"
		{
			"size"		"500"
			"timeupdated"		"1700000000"
			"manifest"		"11"
		}
		"200"
		{
			"size"		"600"
			"timeupdated"		"1700000000"
			"manifest"		"21"
		}
		"300"
		{
			"size"		"700"
			"timeupdated"		"1700000000"
			"manifest"		"31"
		}
		"600"
		{
			"size"		"1"
			"timeupdated"		"1700000000"
			"manifest"		"61"
		}
		"700"
		{
			"size"		"1"
			"timeupdated"		"1700000000"
			"manifest"		"71"
		}
	}
	"WorkshopItemDetails"
	{
		"100"
		{
			"manifest"		"11"
			"timeupdated"		"1700000000"
			"subscribedby"		"5"
			"latest_timeupdated"		"1700000000"
			"latest_manifest"		"11"
		}
		"200"
		{
			"manifest"		"21"
			"timeupdated"		"1700000000"
			"subscribedby"		"5"
			"latest_timeupdated"		"1700000500"
			"latest_manifest"		"22"
		}
		"300"
		{
			"manifest"		"31"
			"subscribedby"		"5"
			"latest_timeupdated"		"1700000900"
		}
		"400"
		{
			"manifest"		"0"
			"subscribedby"		"5"
		}
		"500"
		{
			"subscribedby"		"0"
		}
		"600"
		{
			"subscribedby"		"0"
		}
	}
}
`

func TestParseSubscriptions(t *testing.T) {
	content := t.TempDir()
	for _, id := range []string{"100", "200", "300", "700"} {
		os.MkdirAll(filepath.Join(content, id, "mods"), 0o755)
	}
	subs, err := ParseSubscriptions(subsACF, content)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Sub{}
	for _, s := range subs {
		got[s.ID] = s
	}
	want := map[string]SubState{"100": SubInstalled, "200": SubNeedsUpdate, "300": SubNeedsUpdate, "400": SubDownloading, "700": SubInstalled}
	if len(got) != len(want) {
		t.Fatalf("%+v", subs)
	}
	for id, st := range want {
		if got[id].State != st {
			t.Errorf("%s: %v want %v", id, got[id].State, st)
		}
	}
	if got["100"].Size != 500 || got["100"].Updated != 1700000000 {
		t.Fatalf("%+v", got["100"])
	}
	if subs[0].State != SubDownloading || subs[len(subs)-1].State != SubInstalled {
		t.Fatalf("order: %+v", subs)
	}
	os.RemoveAll(filepath.Join(content, "100"))
	subs, _ = ParseSubscriptions(subsACF, content)
	for _, s := range subs {
		if s.ID == "100" && s.State != SubDownloading {
			t.Fatalf("empty content dir must count as downloading: %+v", s)
		}
	}
	subs, _ = ParseSubscriptions(subsACF, "")
	for _, s := range subs {
		if s.ID == "100" && s.State != SubInstalled {
			t.Fatalf("no content dir check: %+v", s)
		}
	}
	if _, err := ParseSubscriptions(`"X" { }`, ""); err == nil {
		t.Fatal("want error")
	}
	if SubNeedsUpdate.String() == "" || SubDownloading.String() == SubInstalled.String() {
		t.Fatal("strings")
	}
}
