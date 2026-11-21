package webview

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"slices"
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
		{"http://steamcommunity.com/", true, Block},
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
	WriteCommand(&buf, Command{Op: OpSignIn, Mode: ModeQR})
	buf.WriteString("garbage\n\n")
	WriteCommand(&buf, Command{Op: OpSignIn, Mode: "browser"})
	WriteCommand(&buf, Command{Op: "navigate"})
	WriteCommand(&buf, Command{Op: OpSubscribe, ID: "12a"})
	WriteCommand(&buf, Command{Op: OpSubscribe, ID: "123", Req: 4, Unsubscribe: true, Deps: true})
	buf.WriteString(`{"op":"state","extra":1}` + "\n")
	WriteCommand(&buf, Command{Op: OpCancelSignIn})
	WriteCommand(&buf, Command{Op: OpSignOut})
	WriteCommand(&buf, Command{Op: OpQuit})
	var got []Command
	if err := ReadCommands(&buf, func(c Command) { got = append(got, c) }); err != nil {
		t.Fatal(err)
	}
	want := []Command{{Op: OpSignIn, Mode: ModeQR}, {Op: OpSubscribe, ID: "123", Req: 4, Unsubscribe: true, Deps: true}, {Op: OpState}, {Op: OpCancelSignIn}, {Op: OpSignOut}, {Op: OpQuit}}
	if !slices.Equal(got, want) {
		t.Fatalf("%+v", got)
	}
	var ev bytes.Buffer
	WriteEvent(&ev, Event{Event: EvSession, SignedIn: true, Name: "kata"})
	WriteEvent(&ev, Event{Event: EvSubResult, ID: "5", Req: 2, Result: ResultFailed, Error: "x"})
	ev.WriteString("{}\n")
	WriteEvent(&ev, Event{Event: EvQR, Image: "data:image/png;base64,AA", Challenge: "https://s.team/q/1/2"})
	var evs []Event
	ReadEvents(&ev, func(e Event) { evs = append(evs, e) })
	if len(evs) != 3 || evs[0].Name != "kata" || !evs[0].SignedIn || evs[1].Req != 2 || evs[1].Result != ResultFailed || evs[2].Challenge != "https://s.team/q/1/2" {
		t.Fatalf("%+v", evs)
	}
	if ItemURL("42") != "https://steamcommunity.com/sharedfiles/filedetails/?id=42" || !Allowed(LoginURL) || !Allowed(StatusURL) {
		t.Fatal("urls")
	}
}

func TestScriptMessage(t *testing.T) {
	m, ok := ParseScriptMessage(`{"kind":"page","url":"https://steamcommunity.com/x","signedIn":true,"name":"  kata \n  chan ","item":"77"}`)
	if !ok || !m.SignedIn || m.Name != "kata chan" || m.ItemID != "77" {
		t.Fatalf("%+v", m)
	}
	m, ok = ParseScriptMessage(`{"kind":"qr","image":"data:image/png;base64,iVBOR","challenge":"https://s.team/q/1/2345"}`)
	if !ok || m.Image == "" || m.Challenge != "https://s.team/q/1/2345" {
		t.Fatalf("%+v", m)
	}
	m, ok = ParseScriptMessage(`{"kind":"qr","image":"javascript:x","challenge":"https://evil.example/q/1/2"}`)
	if ok {
		t.Fatalf("%+v", m)
	}
	if _, ok := ParseScriptMessage(`{"kind":"qr","challenge":"https://s.team/q/1/2"}`); !ok {
		t.Fatal("challenge alone is enough")
	}
	m, ok = ParseScriptMessage(`{"kind":"result","id":"9","result":"failed","error":"  You do not have\npermission "}`)
	if !ok || m.Error != "You do not have permission" {
		t.Fatalf("%+v", m)
	}
	for _, bad := range []string{`bad`, `{"kind":"result","id":"9","result":"maybe"}`, `{"kind":"result","id":"x","result":"done"}`, `{"kind":"page","item":"1;x"}`, `{"kind":"other"}`} {
		if _, ok := ParseScriptMessage(bad); ok {
			t.Error(bad)
		}
	}
	for _, js := range []string{PageScript("p"), SubscribeScript("12", false, true, "p")} {
		if strings.Count(js, "(") != strings.Count(js, ")") || strings.Count(js, "{") != strings.Count(js, "}") {
			t.Fatalf("unbalanced script: %s", js)
		}
	}
	if !strings.Contains(SubscribeScript("12", true, false, "p"), `"want":false`) {
		t.Fatal("unsubscribe must want false")
	}
}

type fakeEngine struct {
	log    []string
	posted []func()
}

func (f *fakeEngine) Load(u string)         { f.log = append(f.log, "load "+u) }
func (f *fakeEngine) Show()                 { f.log = append(f.log, "show") }
func (f *fakeEngine) Hide()                 { f.log = append(f.log, "hide") }
func (f *fakeEngine) Quit()                 { f.log = append(f.log, "quit") }
func (f *fakeEngine) Post(fn func())        { f.posted = append(f.posted, fn) }
func (f *fakeEngine) ClearData(done func()) { f.log = append(f.log, "clear"); done() }
func (f *fakeEngine) Eval(js string) {
	f.log = append(f.log, "eval "+js[:min(len(js), 20)])
}

func (f *fakeEngine) take() []string {
	l := f.log
	f.log = nil
	return l
}

func page(signed bool, name, item string) string {
	return fmt.Sprintf(`{"kind":"page","signedIn":%v,"name":%q,"item":%q}`, signed, name, item)
}

func events(b *bytes.Buffer) []Event {
	var evs []Event
	ReadEvents(b, func(e Event) { evs = append(evs, e) })
	return evs
}

func TestHelperSignIn(t *testing.T) {
	var out bytes.Buffer
	h := NewHelper(&out, nil)
	e := &fakeEngine{}
	h.Start(e)
	h.ScriptMessage(page(false, "", ""))
	if l := e.take(); !slices.Equal(l, []string{"load " + StatusURL}) {
		t.Fatal(l)
	}
	h.Apply(Command{Op: OpSignIn, Mode: ModeWindow})
	if l := e.take(); !slices.Equal(l, []string{"load " + LoginURL, "show"}) {
		t.Fatal(l)
	}
	h.ScriptMessage(page(false, "", ""))
	h.ScriptMessage(page(true, " kata ", ""))
	if l := e.take(); !slices.Equal(l, []string{"hide"}) {
		t.Fatal(l)
	}
	evs := events(&out)
	if len(evs) != 2 || evs[0].SignedIn || !evs[1].SignedIn || evs[1].Name != "kata" {
		t.Fatalf("%+v", evs)
	}
	h.ScriptMessage(page(true, "", ""))
	if len(events(&out)) != 0 {
		t.Fatal("unchanged session must not be re-sent")
	}
	h.Apply(Command{Op: OpState})
	h.ScriptMessage(page(true, "kata", ""))
	h.Apply(Command{Op: OpSignOut})
	evs = events(&out)
	if len(evs) != 1 || evs[0].SignedIn {
		t.Fatalf("%+v", evs)
	}
	if l := e.take(); !slices.Equal(l, []string{"load " + StatusURL, "clear", "load " + StatusURL}) {
		t.Fatal(l)
	}

	h.Apply(Command{Op: OpSignIn, Mode: ModeQR})
	h.ScriptMessage(`{"kind":"qr","image":"data:image/png;base64,AAA"}`)
	h.ScriptMessage(`{"kind":"qr","image":"data:image/png;base64,AAA"}`)
	h.ScriptMessage(`{"kind":"qr","image":"data:image/png;base64,BBB","challenge":"https://s.team/q/1/9"}`)
	evs = events(&out)
	if len(evs) != 2 || evs[0].Image != "data:image/png;base64,AAA" || evs[1].Challenge != "https://s.team/q/1/9" {
		t.Fatalf("rotation: %+v", evs)
	}
	if l := e.take(); !slices.Equal(l, []string{"hide", "load " + LoginURL}) {
		t.Fatal(l)
	}
	h.Apply(Command{Op: OpCancelSignIn})
	h.ScriptMessage(`{"kind":"qr","image":"data:image/png;base64,CCC"}`)
	if len(events(&out)) != 0 {
		t.Fatal("no QR after cancel")
	}
	h.Apply(Command{Op: OpSignIn, Mode: ModeWindow})
	e.take()
	h.WindowClosed()
	evs = events(&out)
	if len(evs) != 1 || evs[0].Event != EvSignInClosed {
		t.Fatalf("%+v", evs)
	}
	if l := e.take(); !slices.Equal(l, []string{"hide", "load " + StatusURL}) {
		t.Fatal(l)
	}
}

func TestHelperSubscribe(t *testing.T) {
	var out bytes.Buffer
	h := NewHelper(&out, nil)
	var fire []func()
	h.afterFunc = func(d time.Duration, f func()) func() { fire = append(fire, f); return func() {} }
	e := &fakeEngine{}
	h.Start(e)
	e.take()
	h.Apply(Command{Op: OpSubscribe, ID: "1", Req: 1})
	h.Apply(Command{Op: OpSubscribe, ID: "2", Req: 2, Unsubscribe: true})
	if l := e.take(); !slices.Equal(l, []string{"load " + ItemURL("1")}) {
		t.Fatal(l)
	}
	h.ScriptMessage(page(true, "kata", "3"))
	h.ScriptMessage(page(true, "kata", "1"))
	if l := e.take(); len(l) != 1 || !strings.HasPrefix(l[0], "eval ") {
		t.Fatal(l)
	}
	h.ScriptMessage(`{"kind":"result","id":"7","result":"done"}`)
	h.ScriptMessage(`{"kind":"result","id":"1","result":"done"}`)
	if l := e.take(); !slices.Equal(l, []string{"load " + ItemURL("2")}) {
		t.Fatal(l)
	}
	h.ScriptMessage(page(true, "kata", "2"))
	e.take()
	h.ScriptMessage(`{"kind":"result","id":"2","result":"already"}`)
	evs := events(&out)
	if len(evs) != 3 || evs[1].Req != 1 || evs[1].Result != ResultDone || evs[2].Req != 2 || evs[2].Result != ResultAlready {
		t.Fatalf("%+v", evs)
	}

	h.Apply(Command{Op: OpSubscribe, ID: "3", Req: 3})
	h.Apply(Command{Op: OpSignIn, Mode: ModeWindow})
	if l := e.take(); !slices.Equal(l, []string{"load " + ItemURL("3"), "load " + LoginURL, "show"}) {
		t.Fatal(l)
	}
	h.ScriptMessage(page(true, "kata", ""))
	if l := e.take(); !slices.Equal(l, []string{"hide", "load " + ItemURL("3")}) {
		t.Fatalf("job must continue after sign-in: %q", l)
	}
	events(&out)
	fire[len(fire)-2]()
	fire[len(fire)-1]()
	if len(e.posted) != 2 {
		t.Fatal(len(e.posted))
	}
	e.posted[0]()
	if len(events(&out)) != 0 {
		t.Fatal("stale timer must not fail the job")
	}
	e.posted[1]()
	evs = events(&out)
	if len(evs) != 1 || evs[0].Result != ResultFailed || evs[0].Req != 3 {
		t.Fatalf("%+v", evs)
	}
	h.Apply(Command{Op: OpSubscribe, ID: "4", Req: 4})
	h.ScriptMessage(`{"kind":"result","id":"4","result":"not_signed_in"}`)
	evs = events(&out)
	if len(evs) != 2 || evs[0].Result != ResultNotSignedIn || evs[1].Event != EvSession || evs[1].SignedIn {
		t.Fatalf("%+v", evs)
	}
}

func TestMain(m *testing.M) {
	switch os.Getenv("PZL_FAKE_HELPER") {
	case "ok":
		h := NewHelper(os.Stdout, nil)
		e := &fakeEngine{}
		h.Start(e)
		h.Emit(Event{Event: EvReady})
		done := make(chan struct{})
		go h.ReadLoop(os.Stdin, func(fn func()) {
			e.log = nil
			fn()
			if slices.Contains(e.log, "quit") {
				close(done)
				return
			}
			for _, l := range e.log {
				if strings.HasPrefix(l, "load "+ItemURL("")) {
					id := strings.TrimPrefix(l, "load "+ItemURL(""))
					h.ScriptMessage(fmt.Sprintf(`{"kind":"result","id":%q,"result":"done"}`, id))
				}
			}
		})
		<-done
		os.Exit(0)
	case "broken":
		os.Exit(RunHelperWith(func(*Helper, Options) error { return errors.New("no webkit here") }))
	case "real":
		os.Exit(RunHelper(Options{DataDir: os.Getenv("PZL_REAL_DIR"), CacheDir: os.Getenv("PZL_REAL_DIR") + "-cache", Ephemeral: true}))
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
	fallback []error
}

func (r *recorder) ctl(mode string, env ...string) *Controller {
	exe, _ := os.Executable()
	return &Controller{
		Exe:        exe,
		Args:       []string{"-test.run=^$"},
		Env:        append(append(os.Environ(), "PZL_FAKE_HELPER="+mode), env...),
		OnEvent:    func(e Event) { r.mu.Lock(); r.events = append(r.events, e); r.mu.Unlock() },
		OnFallback: func(err error) { r.mu.Lock(); r.fallback = append(r.fallback, err); r.mu.Unlock() },
	}
}

func (r *recorder) find(match func(Event) bool) (Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if match(e) {
			return e, true
		}
	}
	return Event{}, false
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestControllerSubscribe(t *testing.T) {
	r := &recorder{}
	c := r.ctl("ok")
	defer c.Close()
	if err := c.Send(Command{Op: OpSubscribe, ID: "42", Req: 7}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 10*time.Second, func() bool {
		_, ok := r.find(func(e Event) bool { return e.Event == EvSubResult && e.Req == 7 && e.Result == ResultDone })
		return ok
	})
	first := c.cmd.Process.Pid
	c.Send(Command{Op: OpSubscribe, ID: "43", Req: 8})
	waitFor(t, 10*time.Second, func() bool {
		_, ok := r.find(func(e Event) bool { return e.Req == 8 })
		return ok
	})
	if c.cmd.Process.Pid != first {
		t.Fatal("must reuse the helper")
	}
	if c.Send(Command{Op: "navigate"}) == nil {
		t.Fatal("invalid command accepted")
	}
	c.mu.Lock()
	c.stopLocked()
	c.mu.Unlock()
	c.Send(Command{Op: OpState})
	waitFor(t, 10*time.Second, c.Running)
	if c.cmd.Process.Pid == first || c.Fallback() {
		t.Fatal("closed helper must be restarted")
	}
}

func TestControllerFallback(t *testing.T) {
	r := &recorder{}
	c := r.ctl("broken")
	c.Send(Command{Op: OpState})
	waitFor(t, 10*time.Second, c.Fallback)
	if err := c.Send(Command{Op: OpSubscribe, ID: "1"}); err != ErrUnavailable {
		t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.fallback) != 1 || !strings.Contains(r.fallback[0].Error(), "no webkit") {
		t.Fatalf("%+v", r.fallback)
	}
}

func TestRealHelper(t *testing.T) {
	if os.Getenv("PZL_REAL_WEBVIEW") == "" {
		t.Skip("set PZL_REAL_WEBVIEW=1 (and optionally PZL_REAL_QR_OUT=<png>) to load Steam in the real hidden helper")
	}
	dir := t.TempDir()
	r := &recorder{}
	c := r.ctl("real", "PZL_REAL_DIR="+filepath.Join(dir, "data"))
	defer c.Close()
	if err := c.Send(Command{Op: OpState}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 30*time.Second, func() bool {
		_, ok := r.find(func(e Event) bool { return e.Event == EvSession })
		return ok
	})
	s, _ := r.find(func(e Event) bool { return e.Event == EvSession })
	t.Logf("session: signed in %v", s.SignedIn)
	if s.SignedIn {
		t.Fatal("a fresh profile must not be signed in")
	}
	c.Send(Command{Op: OpSignIn, Mode: ModeQR})
	waitFor(t, 30*time.Second, func() bool {
		_, ok := r.find(func(e Event) bool { return e.Event == EvQR && e.Image != "" })
		return ok
	})
	q, _ := r.find(func(e Event) bool { return e.Event == EvQR && e.Image != "" })
	t.Logf("qr image %d bytes, challenge %q", len(q.Image), q.Challenge)
	if _, err := DecodeQR(q.Image); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "data")); err == nil {
		t.Fatal("ephemeral helper must not create its profile folder")
	}
	if out := os.Getenv("PZL_REAL_QR_OUT"); out != "" {
		b, _ := base64.StdEncoding.DecodeString(q.Image[strings.Index(q.Image, ",")+1:])
		os.WriteFile(out, b, 0o644)
	}
}

func TestSubQueue(t *testing.T) {
	q := NewSubQueue([]QueueItem{{"1", "Alpha"}, {"2", ""}, {"3", "Gamma"}, {"4", "Delta"}})
	if q.Progress() != "Subscribing 1 of 4: Alpha..." {
		t.Fatal(q.Progress())
	}
	q.Record(ResultDone, "")
	q.Record(ResultFailed, "You do not have permission")
	if q.Progress() != "Subscribing 3 of 4: Gamma..." {
		t.Fatal(q.Progress())
	}
	q.Record(ResultAlready, "")
	q.Record(ResultDone, "")
	if !q.Finished() || q.Summary() != "Done: 2 subscribed, 1 already subscribed, 1 failed (2: You do not have permission)." {
		t.Fatal(q.Summary())
	}
	q = NewSubQueue([]QueueItem{{"1", "A"}, {"2", "B"}, {"3", "C"}})
	q.Record(ResultDone, "")
	q.Stop("stopped by you")
	q.Stop("ignored")
	if _, ok := q.Current(); ok || q.Summary() != "Stopped after 1 of 3 (stopped by you): 1 subscribed." {
		t.Fatal(q.Summary())
	}
	q = NewSubQueue([]QueueItem{{"1", "A"}, {"2", "B"}})
	q.Record(ResultNotSignedIn, "")
	q.Record(ResultDone, "")
	if q.Done != 0 || q.Summary() != "Stopped after 1 of 2 (not signed in to Steam): 0 subscribed, 1 failed (A: not signed in)." {
		t.Fatal(q.Summary())
	}
}

func TestDecodeQR(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 41, 41))
	for i := 0; i < 41; i++ {
		img.SetGray(i, i, color.Gray{255})
	}
	var b bytes.Buffer
	gif.Encode(&b, img, nil)
	u := "data:image/gif;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	got, err := DecodeQR(u)
	if err != nil || got.Bounds().Dx() != 41 {
		t.Fatal(err)
	}
	m, ok := ParseScriptMessage(fmt.Sprintf(`{"kind":"qr","image":%q}`, u))
	if !ok || m.Image != u {
		t.Fatal("gif QR must pass the message filter")
	}
	small := image.NewGray(image.Rect(0, 0, 8, 8))
	b.Reset()
	png.Encode(&b, small)
	for _, bad := range []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes()), "data:image/png;base64,!!", "https://x/qr.png", "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("nope"))} {
		if _, err := DecodeQR(bad); err == nil {
			t.Error(bad[:20])
		}
	}
}
