package zbsetup

import (
	"strings"
	"testing"
)

func TestMallocEdits(t *testing.T) {
	cases := []struct {
		name, in, add, rm string
	}{
		{"empty", "", "MALLOC_ARENA_MAX=2 %command%", ""},
		{"user real", "gamemoderun %command% -javaagent:ZombieBuddy.jar -- -", "MALLOC_ARENA_MAX=2 gamemoderun %command% -javaagent:ZombieBuddy.jar -- -", "gamemoderun %command% -javaagent:ZombieBuddy.jar -- -"},
		{"already on", "MALLOC_ARENA_MAX=2 gamemoderun %command% -", "MALLOC_ARENA_MAX=2 gamemoderun %command% -", "gamemoderun %command% -"},
		{"bare command", "%command%", "MALLOC_ARENA_MAX=2 %command%", ""},
		{"no command, game args", "-debug", "MALLOC_ARENA_MAX=2 %command% -debug", "%command% -debug"},
		{"no command, zombiebuddy", "-javaagent:ZombieBuddy.jar -- -nosound", "MALLOC_ARENA_MAX=2 %command% -javaagent:ZombieBuddy.jar -- -nosound", "%command% -javaagent:ZombieBuddy.jar -- -nosound"},
		{"other env kept", "FOO=1 gamemoderun %command%", "MALLOC_ARENA_MAX=2 FOO=1 gamemoderun %command%", "FOO=1 gamemoderun %command%"},
		{"gamescope", "gamescope -f -- gamemoderun %command%", "gamescope -f -- env MALLOC_ARENA_MAX=2 gamemoderun %command%", "gamescope -f -- gamemoderun %command%"},
		{"gamescope env other", "gamescope -f -- env FOO=1 %command%", "gamescope -f -- env MALLOC_ARENA_MAX=2 env FOO=1 %command%", "gamescope -f -- env FOO=1 %command%"},
		{"gamescope path", "/usr/bin/gamescope -W 1920 -- %command%", "/usr/bin/gamescope -W 1920 -- env MALLOC_ARENA_MAX=2 %command%", "/usr/bin/gamescope -W 1920 -- %command%"},
		{"escaped quotes", `X=\"a b\" %command%`, `MALLOC_ARENA_MAX=2 X=\"a b\" %command%`, `X=\"a b\" %command%`},
	}
	for _, c := range cases {
		got, _ := AddMalloc(c.in)
		if got != c.add {
			t.Errorf("%s add: %q want %q", c.name, got, c.add)
		}
		if val, ok := MallocArena(got); !ok || val != MallocValue {
			t.Errorf("%s: not detected after add: %q", c.name, got)
		}
		if again, changed := AddMalloc(got); changed || again != got {
			t.Errorf("%s: add not idempotent: %q", c.name, again)
		}
		rm, _ := RemoveMalloc(got)
		if rm != c.rm {
			t.Errorf("%s rm: %q want %q", c.name, rm, c.rm)
		}
		if _, ok := MallocArena(rm); ok {
			t.Errorf("%s: still set after remove: %q", c.name, rm)
		}
	}
}

func TestMallocUserValue(t *testing.T) {
	v := "MALLOC_ARENA_MAX=4 gamemoderun %command%"
	if val, ok := MallocArena(v); !ok || val != "4" {
		t.Fatal(val, ok)
	}
	if got, ch := AddMalloc(v); ch || got != v {
		t.Fatal("user value overwritten:", got)
	}
	if got, ch := RemoveMalloc(v); ch || got != v {
		t.Fatal("user value removed:", got)
	}
	on, other := MallocStatus([]string{"-nosound", v, "MALLOC_ARENA_MAX=2 %command%"})
	if !on || other != "4" {
		t.Fatal(on, other)
	}
	if on, other := MallocStatus([]string{"gamemoderun %command%", ""}); on || other != "" {
		t.Fatal(on, other)
	}
}

func TestMallocOutsideGamescopeIgnored(t *testing.T) {
	v := "MALLOC_ARENA_MAX=2 gamescope -f -- %command%"
	if _, ok := MallocArena(v); ok {
		t.Fatal("gamescope strips the variable; it must not count")
	}
	got, _ := AddMalloc(v)
	if got != "MALLOC_ARENA_MAX=2 gamescope -f -- env MALLOC_ARENA_MAX=2 %command%" {
		t.Fatal(got)
	}
}

func TestMallocWithZombieBuddy(t *testing.T) {
	for _, start := range []string{"", "gamemoderun %command% -", "-debug", "gamescope -f -- %command%"} {
		zbFirst, _ := AddLoader(start, "linux")
		zbFirst, _ = AddMalloc(zbFirst)
		mFirst, _ := AddMalloc(start)
		mFirst, _ = AddLoader(mFirst, "linux")
		for _, v := range []string{zbFirst, mFirst} {
			if !HasLoader(v, "linux") {
				t.Errorf("%q: loader lost in %q", start, v)
			}
			if val, ok := MallocArena(v); !ok || val != MallocValue {
				t.Errorf("%q: malloc lost in %q", start, v)
			}
			noZB, _ := RemoveLoader(v)
			if val, ok := MallocArena(noZB); !ok || val != MallocValue {
				t.Errorf("%q: ZombieBuddy removal dropped malloc: %q", start, noZB)
			}
			noM, _ := RemoveMalloc(v)
			if !HasLoader(noM, "linux") || strings.Contains(noM, MallocVar) {
				t.Errorf("%q: malloc removal broke %q -> %q", start, v, noM)
			}
		}
	}
}

func TestMallocPlans(t *testing.T) {
	configs := []ConfigFile{{Path: "a/localconfig.vdf", Text: vdfReal}}
	p := MallocPlan(configs, true)
	if len(p.Configs) != 1 || len(p.Configs[0].Edits) != 1 || p.Configs[0].Edits[0].After != "MALLOC_ARENA_MAX=2 gamemoderun %command% -javaagent:ZombieBuddy.jar -- -" {
		t.Fatalf("%+v", p)
	}
	if !strings.Contains(p.Configs[0].Text, `"9420"`+"\n\t\t\t\t\t{\n\t\t\t\t\t\t\"LaunchOptions\"\t\t\"gamemoderun %command%\"") {
		t.Fatal("other game touched")
	}
	back := MallocPlan([]ConfigFile{{Path: "a/localconfig.vdf", Text: p.Configs[0].Text}}, false)
	if len(back.Configs) != 1 || back.Configs[0].Text != vdfReal {
		t.Fatal("remove did not restore the file")
	}
	if MallocPlan(configs, false).Configs != nil {
		t.Fatal("remove changed a file without the variable")
	}

	// ZombieBuddy setup plus malloc: one edit per entry, before = original.
	off := strings.Replace(vdfReal, "gamemoderun %command% -javaagent:ZombieBuddy.jar -- -", "gamemoderun %command% -", 1)
	cf := []ConfigFile{{Path: "a/localconfig.vdf", Text: off}, {Path: "b/localconfig.vdf", Text: p.Configs[0].Text}}
	var zp Plan
	editConfigs(cf, func(v string) string { nv, _ := AddLoader(v, "linux"); return nv }, &zp)
	AddMallocToPlan(&zp, cf)
	if len(zp.Configs) != 1 || zp.Configs[0].Path != "a/localconfig.vdf" {
		t.Fatalf("%+v", zp.Configs)
	}
	e := zp.Configs[0].Edits
	if len(e) != 1 || e[0].Before != "gamemoderun %command% -" || e[0].After != "MALLOC_ARENA_MAX=2 gamemoderun %command% -javaagent:ZombieBuddy.jar -- -" {
		t.Fatalf("%+v", e)
	}
	vals, _ := ReadLaunchOptions(zp.Configs[0].Text)
	if len(vals) != 1 || vals[0] != e[0].After {
		t.Fatal(vals)
	}
	// Only malloc missing: the plan gains a config it did not have.
	var only Plan
	AddMallocToPlan(&only, []ConfigFile{{Path: "a/localconfig.vdf", Text: vdfReal}})
	if len(only.Configs) != 1 || only.Configs[0].Edits[0].Before != "gamemoderun %command% -javaagent:ZombieBuddy.jar -- -" {
		t.Fatalf("%+v", only.Configs)
	}
}
