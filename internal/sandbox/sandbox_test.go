package sandbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const optsTxt = `VERSION = 1,
/* Sample mod: options
   spanning lines */
option Sample.Enabled
{
	type = boolean,
	default = true,
	page = Sample,
	translation = Sample_Enabled,
}

option Sample.Count
{type = integer, min = 1, max = 10, default = 3, page = Sample, translation = Sample_Count,}

option Sample.Speed
{
	type = double, min = 0.5, max = 2, default = 1,
	page = Sample,
}

option Sample.Mode
{type = enum, numValues = 3, default = 2, page = Sample, translation = Sample_Mode, valueTranslation = Sample_ModeValues,}

option Sample.Note
{type = string, default = , page = Sample, translation = Sample_Note,}

option Other.Flag
{type = boolean, default = false, page = Other,}

option Plain_Setting
{type = integer, min = -1, max = 100, default = -1, page = Plain,}
`

const sandboxTxt = `Sandbox_EN = {
    Sandbox_Sample = "Sample Mod",
    Sandbox_Sample_Enabled = "Enable it",
    Sandbox_Sample_Enabled_tooltip = "First line." ..
        " <LINE> Second <RGB:1,0,0> line.",
    Sandbox_Sample_Count = "How many",
}
`

const sandboxJSON = `{"Sandbox_Sample_Mode": "Mode", "Sandbox_Sample_ModeValues_option1": "Off", "Sandbox_Sample_ModeValues_option3": "High", "Sandbox_Sample_Count": "ignored, txt wins"}`

func TestParseAndTranslate(t *testing.T) {
	opts := ParseOptions(optsTxt)
	if len(opts) != 7 || opts[6].Group() != "" || opts[6].Key() != "Plain_Setting" {
		t.Fatalf("%d options: %+v", len(opts), opts)
	}
	c := opts[1]
	if c.Name != "Sample.Count" || c.Type != "integer" || c.Min != "1" || c.Max != "10" || c.Default != "3" || c.Group() != "Sample" || c.Key() != "Count" {
		t.Fatalf("%+v", c)
	}
	if opts[3].NumValues != 3 || opts[3].ValueTranslation != "Sample_ModeValues" || opts[4].Default != "" {
		t.Fatalf("%+v %+v", opts[3], opts[4])
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Sandbox_EN.txt"), []byte(sandboxTxt), 0o644)
	os.WriteFile(filepath.Join(dir, "Sandbox.json"), []byte(sandboxJSON), 0o644)
	tr := LoadTranslations(dir)
	if tr.Label(opts[0]) != "Enable it" || tr.Label(opts[1]) != "How many" || tr.Label(opts[2]) != "Sample.Speed" {
		t.Fatal(tr.Label(opts[0]), tr.Label(opts[1]), tr.Label(opts[2]))
	}
	if got := tr.Tooltip(opts[0]); got != "First line.\nSecond line." {
		t.Fatalf("%q", got)
	}
	if tr.Page("Sample") != "Sample Mod" || tr.Page("Other") != "Other" {
		t.Fatal(tr.Page("Sample"))
	}
	if v := tr.EnumValues(opts[3]); strings.Join(v, "|") != "Off|2|High" {
		t.Fatal(v)
	}
}

func TestValid(t *testing.T) {
	o := ParseOptions(optsTxt)
	for _, c := range []struct {
		i  int
		v  string
		ok bool
	}{
		{0, "true", true}, {0, "yes", false},
		{1, "10", true}, {1, "11", false}, {1, "2.5", false},
		{2, "0.5", true}, {2, "2.01", false}, {2, "x", false},
		{3, "3", true}, {3, "4", false}, {3, "0", false},
		{4, "anything, really", true}, {4, "two\nlines", false},
	} {
		if o[c.i].Valid(c.v) != c.ok {
			t.Errorf("%s %q: want %v", o[c.i].Name, c.v, c.ok)
		}
	}
}

func optMap() map[string]Option {
	m := map[string]Option{}
	for _, o := range ParseOptions(optsTxt) {
		m[o.Name] = o
	}
	return m
}

func TestPresetRoundTrip(t *testing.T) {
	in := "Version=6\r\n# Zombies\r\nZombies=4\r\n\r\n# Sample\r\nSample.Enabled=true\r\n\r\nSample.Count=3\r\nSample.Note=a=b\r\n"
	f := parseFile(Target{Kind: Preset}, in)
	if v, _ := f.Value("Sample.Note"); v != "a=b" {
		t.Fatal(v)
	}
	out, err := f.Render(map[string]string{"Sample.Count": "7", "Sample.Mode": "3", "Plain_Setting": "5"}, optMap())
	if err != nil {
		t.Fatal(err)
	}
	want := "Version=6\r\n# Zombies\r\nZombies=4\r\n\r\n# Sample\r\nSample.Enabled=true\r\n\r\nSample.Count=7\r\nSample.Note=a=b\r\n\r\nSample.Mode=3\r\n\r\nPlain_Setting=5\r\n"
	if out != want {
		t.Fatalf("got %q", out)
	}
	g := parseFile(Target{Kind: Preset}, out)
	if v, ok := g.Value("Plain_Setting"); !ok || v != "5" {
		t.Fatal("dotless option not read back", v)
	}
	if again, _ := g.Render(map[string]string{"Plain_Setting": "6"}, optMap()); strings.Count(again, "Plain_Setting=") != 1 {
		t.Fatalf("dotless option duplicated: %q", again)
	}
	if _, err := f.Render(map[string]string{"Sample.Count": "99"}, optMap()); err == nil {
		t.Fatal("out of range accepted")
	}
}

func TestServerRoundTrip(t *testing.T) {
	in := `SandboxVars = {
    VERSION = 6,
    Zombies = 4,
    Map = {
        AllowMiniMap = true,
    },
    Sample = {
        -- Enable it
        Enabled = true,
        Note = "a, \"b\"",
    },
}
`
	f := parseFile(Target{Kind: Server}, in)
	if v, _ := f.Value("Sample.Note"); v != `a, "b"` {
		t.Fatalf("%q", v)
	}
	if v, _ := f.Value("Map.AllowMiniMap"); v != "true" {
		t.Fatal(v)
	}
	out, err := f.Render(map[string]string{"Sample.Enabled": "false", "Sample.Note": `x "y"`, "Sample.Count": "5", "Other.Flag": "true", "Plain_Setting": "7"}, optMap())
	if err != nil {
		t.Fatal(err)
	}
	want := `SandboxVars = {
    VERSION = 6,
    Zombies = 4,
    Map = {
        AllowMiniMap = true,
    },
    Sample = {
        -- Enable it
        Enabled = false,
        Note = "x \"y\"",
        Count = 5,
    },
    Other = {
        Flag = true,
    },
    Plain_Setting = 7,
}
`
	if out != want {
		t.Fatalf("got\n%s", out)
	}
	g := parseFile(Target{Kind: Server}, out)
	if v, _ := g.Value("Other.Flag"); v != "true" {
		t.Fatal("new group not readable")
	}
	if v, _ := g.Value("Plain_Setting"); v != "7" {
		t.Fatal("dotless option not readable", v)
	}
	if v, _ := g.Value("Zombies"); v != "4" {
		t.Fatal("top-level value not read", v)
	}
}

func TestSaveBacksUp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Test.cfg")
	os.WriteFile(p, []byte("Version=6\nSample.Count=3\n"), 0o644)
	f, err := Open(Target{Preset, "Test", p})
	if err != nil {
		t.Fatal(err)
	}
	bak, err := f.Save(map[string]string{"Sample.Count": "4"}, optMap())
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(bak); string(b) != "Version=6\nSample.Count=3\n" {
		t.Fatal(string(b))
	}
	if b, _ := os.ReadFile(p); string(b) != "Version=6\nSample.Count=4\n" {
		t.Fatal(string(b))
	}
	if v, _ := f.Value("Sample.Count"); v != "4" {
		t.Fatal(v)
	}
}

func TestTargets(t *testing.T) {
	z := t.TempDir()
	os.MkdirAll(filepath.Join(z, "Sandbox Presets"), 0o755)
	os.MkdirAll(filepath.Join(z, "Server"), 0o755)
	os.WriteFile(filepath.Join(z, "Sandbox Presets", "Mine.cfg"), nil, 0o644)
	os.WriteFile(filepath.Join(z, "Server", "myserver_SandboxVars.lua"), nil, 0o644)
	os.WriteFile(filepath.Join(z, "Server", "myserver.ini"), nil, 0o644)
	got := Targets(z)
	if len(got) != 2 || got[0].String() != "Preset: Mine" || got[1].String() != "Server: myserver" {
		t.Fatal(got)
	}
}
