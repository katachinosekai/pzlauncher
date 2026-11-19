package webview

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDecide(t *testing.T) {
	cases := []struct {
		url     string
		gesture bool
		want    Action
	}{
		{"https://steamcommunity.com/sharedfiles/filedetails/?id=1", false, Allow},
		{"https://STEAMCOMMUNITY.COM./login/home/", false, Allow},
		{"https://login.steampowered.com/jwt/finalizelogin", false, Allow},
		{"https://store.steampowered.com/login/", false, Allow},
		{"https://help.steampowered.com/en/", false, Allow},
		{"https://shared.fastly.steamstatic.com/x.js", false, Allow},
		{"https://images.steamusercontent.com/ugc/1/2/", false, Allow},
		{"https://www.google.com/recaptcha/api2/anchor", false, Allow},
		{"about:blank", false, Allow},
		{"http://steamcommunity.com/", false, Block},
		{"http://steamcommunity.com/", true, External},
		{"https://steamcommunity.com:8443/", true, External},
		{"https://user@steamcommunity.com/", false, Block},
		{"https://steamcommunity.com.evil.example/", false, Block},
		{"https://evilsteamcommunity.com/", true, External},
		{"https://www.youtube.com/embed/x", false, Block},
		{"https://www.google.com/search?q=x", true, External},
		{"steam://url/CommunityFilePage/1", true, External},
		{"steam://url/CommunityFilePage/1", false, Block},
		{"file:///etc/passwd", true, Block},
		{"javascript:alert(1)", true, Block},
		{"::bad", true, Block},
	}
	for _, c := range cases {
		if got := Decide(c.url, c.gesture); got != c.want {
			t.Errorf("Decide(%q, %v) = %v, want %v", c.url, c.gesture, got, c.want)
		}
	}
	if NormalizeTyped(" steamcommunity.com/app/108600 ") != "https://steamcommunity.com/app/108600" || NormalizeTyped("") != "" || NormalizeTyped("about:blank") != "about:blank" {
		t.Fatal("NormalizeTyped")
	}
}

func TestProtocol(t *testing.T) {
	var buf bytes.Buffer
	WriteCommand(&buf, Command{Op: OpNavigate, URL: "https://steamcommunity.com/a"})
	buf.WriteString("garbage\n\n")
	WriteCommand(&buf, Command{Op: OpNavigate, URL: "https://evil.example/"})
	WriteCommand(&buf, Command{Op: "rm"})
	WriteCommand(&buf, Command{Op: OpShow})
	buf.WriteString(`{"op":"navigate","url":"https://steamcommunity.com/b","extra":1}` + "\n")
	WriteCommand(&buf, Command{Op: OpQuit})
	var got []Command
	if err := ReadCommands(&buf, func(c Command) { got = append(got, c) }); err != nil {
		t.Fatal(err)
	}
	want := []Command{{OpNavigate, "https://steamcommunity.com/a"}, {OpShow, ""}, {OpNavigate, "https://steamcommunity.com/b"}, {OpQuit, ""}}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%d: %+v", i, got[i])
		}
	}
	buf.Reset()
	WriteEvent(&buf, Event{Event: EvSubscribed, ID: "5", Subscribed: true})
	buf.WriteString("{}\nnot json\n")
	WriteEvent(&buf, Event{Event: EvReady})
	var evs []Event
	ReadEvents(&buf, func(e Event) { evs = append(evs, e) })
	if len(evs) != 2 || evs[0] != (Event{Event: EvSubscribed, ID: "5", Subscribed: true}) || evs[1].Event != EvReady {
		t.Fatalf("%+v", evs)
	}
	if !strings.Contains(buf.String(), "") || strings.Contains(string(mustLine(Command{Op: OpShow})), "url") {
		t.Fatal("omitempty")
	}
}

func mustLine(c Command) []byte {
	var b bytes.Buffer
	WriteCommand(&b, c)
	return b.Bytes()
}

func TestScriptMessage(t *testing.T) {
	if e, ok := ParseScriptMessage(`{"id":"3825994973","subscribed":true}`); !ok || e.ID != "3825994973" || !e.Subscribed || e.Event != EvSubscribed {
		t.Fatalf("%+v %v", e, ok)
	}
	for _, bad := range []string{`{"id":"12a"}`, `{"id":""}`, `nope`, `{"id":"1","subscribed":"yes"}`, `{"id":"` + strings.Repeat("1", 2000) + `"}`} {
		if _, ok := ParseScriptMessage(bad); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	js := WatchScript("window.x.post")
	if !strings.Contains(js, "window.x.post(JSON.stringify") || !strings.Contains(js, "SubscribeItemBtn") {
		t.Fatal(js)
	}
}

type fakeEngine struct{ log []string }

func (f *fakeEngine) Navigate(u string) { f.log = append(f.log, "nav "+u) }
func (f *fakeEngine) Present()          { f.log = append(f.log, "present") }
func (f *fakeEngine) Quit()             { f.log = append(f.log, "quit") }

func TestHelper(t *testing.T) {
	var out bytes.Buffer
	var opened []string
	h := NewHelper(&out, func(u string) error { opened = append(opened, u); return nil })
	e := &fakeEngine{}
	h.Apply(e, Command{Op: OpNavigate, URL: "https://steamcommunity.com/x"})
	h.Apply(e, Command{Op: OpNavigate, URL: "https://evil.example/"})
	h.Apply(e, Command{Op: OpShow})
	h.Apply(e, Command{Op: OpQuit})
	if strings.Join(e.log, "|") != "nav https://steamcommunity.com/x|present|present|quit" {
		t.Fatal(e.log)
	}
	if h.Decide("https://example.com/", false) != Block || len(opened) != 0 {
		t.Fatal("non-gesture external must be blocked silently")
	}
	if h.Decide("https://example.com/", true) != External || len(opened) != 1 {
		t.Fatal(opened)
	}
	h.URLChanged("https://steamcommunity.com/x")
	h.URLChanged("https://steamcommunity.com/x")
	h.ScriptMessage(`{"id":"7","subscribed":true}`)
	h.ScriptMessage(`bad`)
	var evs []Event
	ReadEvents(&out, func(e Event) { evs = append(evs, e) })
	if len(evs) != 3 || evs[0].Event != EvExternal || evs[1].Event != EvURL || evs[2].ID != "7" {
		t.Fatalf("%+v", evs)
	}
	var posted []Command
	h.ReadLoop(strings.NewReader(`{"op":"show"}`+"\n"), func(c Command) { posted = append(posted, c) })
	if len(posted) != 2 || posted[1].Op != OpQuit {
		t.Fatalf("EOF must post quit: %+v", posted)
	}
}

func TestMain(m *testing.M) {
	switch os.Getenv("PZL_FAKE_HELPER") {
	case "ok":
		h := NewHelper(os.Stdout, nil)
		h.Emit(Event{Event: EvReady})
		e := &fakeEngine{}
		done := make(chan struct{})
		go h.ReadLoop(os.Stdin, func(c Command) {
			if c.Op == OpQuit {
				close(done)
				return
			}
			h.Apply(e, c)
			if c.Op == OpNavigate {
				h.URLChanged(c.URL)
				if strings.Contains(c.URL, "id=42") {
					h.ScriptMessage(`{"id":"42","subscribed":true}`)
				}
			}
		})
		<-done
		os.Exit(0)
	case "broken":
		os.Exit(RunHelperWith(func(*Helper, Options) error { return errors.New("no webkit here") }))
	}
	os.Exit(m.Run())
}

func RunHelperWith(run func(*Helper, Options) error) int {
	h := NewHelper(os.Stdout, nil)
	if err := run(h, Options{}); err != nil {
		h.Emit(Event{Event: EvUnavailable, Error: err.Error()})
		return ExitUnavailable
	}
	return 0
}

type recorder struct {
	mu       sync.Mutex
	events   []Event
	external []string
	fallback []error
}

func (r *recorder) ctl(mode string) *Controller {
	exe, _ := os.Executable()
	return &Controller{
		Exe:          exe,
		Args:         []string{"-test.run=^$"},
		Env:          append(os.Environ(), "PZL_FAKE_HELPER="+mode),
		OpenExternal: func(u string) error { r.mu.Lock(); r.external = append(r.external, u); r.mu.Unlock(); return nil },
		OnEvent:      func(e Event) { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() },
		OnFallback:   func(err error) { r.mu.Lock(); r.fallback = append(r.fallback, err); r.mu.Unlock() },
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestControllerReusesHelper(t *testing.T) {
	r := &recorder{}
	c := r.ctl("ok")
	defer c.Close()
	if err := c.Open("https://steamcommunity.com/sharedfiles/filedetails/?id=1"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c.Running)
	first := c.cmd.Process.Pid
	if err := c.Open("https://steamcommunity.com/sharedfiles/filedetails/?id=42"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		for _, e := range r.events {
			if e.Event == EvSubscribed && e.ID == "42" && e.Subscribed {
				return true
			}
		}
		return false
	})
	if c.cmd.Process.Pid != first {
		t.Fatal("second page must reuse the same helper")
	}
	if err := c.Open("https://example.com/"); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	ext, fb := append([]string(nil), r.external...), len(r.fallback)
	r.mu.Unlock()
	if len(ext) != 1 || ext[0] != "https://example.com/" || fb != 0 || c.Fallback() {
		t.Fatalf("%v %d", ext, fb)
	}
	c.mu.Lock()
	c.stopLocked()
	c.mu.Unlock()
	if err := c.Open("https://steamcommunity.com/sharedfiles/filedetails/?id=2"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c.Running)
	if c.cmd.Process.Pid == first {
		t.Fatal("closed helper must be restarted")
	}
}

func TestControllerFallback(t *testing.T) {
	r := &recorder{}
	c := r.ctl("broken")
	u := "https://steamcommunity.com/sharedfiles/filedetails/?id=9"
	if err := c.Open(u); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c.Fallback)
	waitFor(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.external) == 1 })
	c.Open("https://steamcommunity.com/b")
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.fallback) != 1 || !strings.Contains(r.fallback[0].Error(), "no webkit") || r.external[0] != u || len(r.external) != 2 {
		t.Fatalf("%+v %v", r.fallback, r.external)
	}
}
