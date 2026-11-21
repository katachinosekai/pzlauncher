package report

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	if err := (Report{}).Validate(); !errors.Is(err, ErrEmpty) {
		t.Fatal(err)
	}
	if err := (Report{Doing: strings.Repeat("x", MaxTextChars+1)}).Validate(); !errors.Is(err, ErrTooLong) {
		t.Fatal(err)
	}
	if err := (Report{Happened: "x", Logs: make([]byte, MaxLogsBytes+1)}).Validate(); !errors.Is(err, ErrLogsTooBig) {
		t.Fatal(err)
	}
	if err := (Report{Happened: "crashed"}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSend(t *testing.T) {
	var got map[string]string
	var logs string
	status := http.StatusOK
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Error(err)
		}
		got = map[string]string{}
		for k, v := range r.MultipartForm.Value {
			got[k] = v[0]
		}
		logs = ""
		if f, _, err := r.FormFile("logs"); err == nil {
			b, _ := io.ReadAll(f)
			logs = string(b)
		}
		w.WriteHeader(status)
		if status == http.StatusBadRequest {
			io.WriteString(w, `{"error":"bad field"}`)
		}
	}))
	defer srv.Close()
	c := &Client{HTTP: srv.Client(), Relay: srv.URL}
	r := Report{Doing: "loading a save", Happened: "game froze", LauncherVersion: "0.1.8", OS: "linux/amd64", Crash: true, Logs: []byte("zipdata")}
	if err := c.Send(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got["app"] != "pzlauncher" || got["doing"] != "loading a save" || got["happened"] != "game froze" || got["version"] != "0.1.8" || got["crash"] != "1" || logs != "zipdata" {
		t.Fatal(got, logs)
	}
	r.Logs = nil
	if err := c.Send(context.Background(), r); err != nil || logs != "" {
		t.Fatal(err, logs)
	}
	status = http.StatusTooManyRequests
	if err := c.Send(context.Background(), r); !errors.Is(err, ErrRateLimited) {
		t.Fatal(err)
	}
	status = http.StatusBadRequest
	if err := c.Send(context.Background(), r); err == nil || !strings.Contains(err.Error(), "bad field") {
		t.Fatal(err)
	}
	if err := (&Client{}).Send(context.Background(), r); !errors.Is(err, ErrNotConfigured) {
		t.Fatal(err)
	}
	if err := (&Client{Relay: "http://example.com/"}).Send(context.Background(), r); err == nil {
		t.Fatal("http relay accepted")
	}
}
