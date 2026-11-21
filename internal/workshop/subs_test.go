package workshop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	if got["100"].Size != 500 || got["100"].Updated != 1700000000 || got["100"].Dir != filepath.Join(content, "100") {
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

func TestReadLocalItem(t *testing.T) {
	item := t.TempDir()
	write := func(rel, text string) {
		p := filepath.Join(item, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("mods/Fences/42/mod.info", "name=Viewpoint - 3D Fences\nid=Fences\ndescription=3D   fences.\nposter=../common/poster.png\nicon=../common/icon.png\n")
	write("mods/Fences/common/poster.png", "png")
	write("mods/Alpha/common/mod.info", "id=Alpha\nposter=missing.png\nrequire=\\tsarlib,Fences\n")
	write("mods/NoInfo/readme.txt", "x")
	got := ReadLocalItem(item)
	want := LocalItem{Title: "Alpha, Viewpoint - 3D Fences", Image: filepath.Join(item, "mods/Fences/42/../common/poster.png"), Description: "3D fences."}
	if got.Title != want.Title || filepath.Clean(got.Image) != filepath.Clean(want.Image) || got.Description != want.Description {
		t.Fatalf("%+v", got)
	}
	if strings.Join(got.ModIDs, ",") != "Alpha,Fences" || len(got.Requires) == 0 {
		t.Fatalf("ids/requires: %+v", got)
	}
	os.Remove(filepath.Join(item, "mods/Fences/common/poster.png"))
	write("mods/Fences/common/icon.png", "png")
	if got := ReadLocalItem(item); filepath.Base(got.Image) != "icon.png" {
		t.Fatalf("icon fallback: %+v", got)
	}
	if got := ReadLocalItem(filepath.Join(item, "nope")); !reflect.DeepEqual(got, LocalItem{}) {
		t.Fatalf("%+v", got)
	}
	if got := ReadLocalItem(""); !reflect.DeepEqual(got, LocalItem{}) {
		t.Fatalf("%+v", got)
	}
}
