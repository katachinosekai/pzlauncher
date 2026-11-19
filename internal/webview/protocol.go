package webview

import (
	"bufio"
	"encoding/json"
	"io"
	"regexp"
)

const (
	OpNavigate = "navigate"
	OpShow     = "show"
	OpQuit     = "quit"
)

const (
	EvReady       = "ready"
	EvUnavailable = "unavailable"
	EvSubscribed  = "subscribed"
	EvURL         = "url"
	EvExternal    = "external"
)

type Command struct {
	Op  string `json:"op"`
	URL string `json:"url,omitempty"`
}

type Event struct {
	Event      string `json:"event"`
	URL        string `json:"url,omitempty"`
	ID         string `json:"id,omitempty"`
	Subscribed bool   `json:"subscribed,omitempty"`
	Error      string `json:"error,omitempty"`
}

const maxLine = 64 << 10

func (c Command) Valid() bool {
	switch c.Op {
	case OpShow, OpQuit:
		return true
	case OpNavigate:
		return Allowed(c.URL)
	}
	return false
}

func writeLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

func WriteCommand(w io.Writer, c Command) error { return writeLine(w, c) }

func WriteEvent(w io.Writer, e Event) error { return writeLine(w, e) }

func scanLines(r io.Reader, fn func([]byte)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), maxLine)
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			fn(sc.Bytes())
		}
	}
	return sc.Err()
}

func ReadCommands(r io.Reader, fn func(Command)) error {
	return scanLines(r, func(b []byte) {
		var c Command
		if json.Unmarshal(b, &c) == nil && c.Valid() {
			fn(c)
		}
	})
}

func ReadEvents(r io.Reader, fn func(Event)) error {
	return scanLines(r, func(b []byte) {
		var e Event
		if json.Unmarshal(b, &e) == nil && e.Event != "" {
			fn(e)
		}
	})
}

var digits = regexp.MustCompile(`^[0-9]{1,20}$`)

func ParseScriptMessage(s string) (Event, bool) {
	var m struct {
		ID         string `json:"id"`
		Subscribed bool   `json:"subscribed"`
	}
	if len(s) > 1024 || json.Unmarshal([]byte(s), &m) != nil || !digits.MatchString(m.ID) {
		return Event{}, false
	}
	return Event{Event: EvSubscribed, ID: m.ID, Subscribed: m.Subscribed}, true
}

func WatchScript(post string) string {
	return `(function(){if(window.__pzlWatch)return;window.__pzlWatch=1;` +
		`var m=/[?&]id=(\d+)/.exec(location.search);` +
		`if(!m||!/\/(sharedfiles|workshop)\/filedetails/.test(location.pathname))return;` +
		`var last=null;function check(){var b=document.getElementById('SubscribeItemBtn');if(!b)return;` +
		`var s=b.classList.contains('toggled');if(s!==last){last=s;try{` + post + `(JSON.stringify({id:m[1],subscribed:s}));}catch(e){}}}` +
		`setInterval(check,1000);})();`
}
