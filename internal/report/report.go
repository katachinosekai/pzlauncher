package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

var RelayURL = ""

const App = "pzlauncher"

const (
	MaxTextChars = 4000
	MaxLogsBytes = 20 << 20
)

var (
	ErrNotConfigured = errors.New("sending reports is not set up in this launcher build")
	ErrEmpty         = errors.New("describe what you were doing or what happened")
	ErrTooLong       = fmt.Errorf("keep each answer under %d characters", MaxTextChars)
	ErrLogsTooBig    = fmt.Errorf("the logs are larger than %d MiB: save a local copy instead and share it another way", MaxLogsBytes>>20)
	ErrRateLimited   = errors.New("too many reports were sent recently: try again later")
)

type Report struct {
	Doing           string
	Happened        string
	LauncherVersion string
	OS              string
	Crash           bool
	Logs            []byte
}

func (r Report) Validate() error {
	if strings.TrimSpace(r.Doing) == "" && strings.TrimSpace(r.Happened) == "" {
		return ErrEmpty
	}
	if utf8.RuneCountInString(r.Doing) > MaxTextChars || utf8.RuneCountInString(r.Happened) > MaxTextChars {
		return ErrTooLong
	}
	if len(r.Logs) > MaxLogsBytes {
		return ErrLogsTooBig
	}
	return nil
}

func (r Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Launcher: %s\nOS: %s\n", r.LauncherVersion, r.OS)
	if r.Crash {
		b.WriteString("Offered after a game crash (Java crash dump found)\n")
	}
	fmt.Fprintf(&b, "\nWhat were you doing?\n%s\n\nWhat happened?\n%s\n", strings.TrimSpace(r.Doing), strings.TrimSpace(r.Happened))
	return b.String()
}

type Client struct {
	HTTP  *http.Client
	Relay string
}

func NewClient() *Client {
	return &Client{HTTP: &http.Client{Timeout: 2 * time.Minute}, Relay: RelayURL}
}

func (c *Client) Configured() bool { return c.Relay != "" }

func (c *Client) Send(ctx context.Context, r Report) error {
	if c.Relay == "" {
		return ErrNotConfigured
	}
	if u, err := url.Parse(c.Relay); err != nil || u.Scheme != "https" {
		return fmt.Errorf("report relay must be an https URL: %q", c.Relay)
	}
	if err := r.Validate(); err != nil {
		return err
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fields := [][2]string{{"app", App}, {"doing", r.Doing}, {"happened", r.Happened}, {"version", r.LauncherVersion}, {"os", r.OS}}
	if r.Crash {
		fields = append(fields, [2]string{"crash", "1"})
	}
	for _, f := range fields {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			return err
		}
	}
	if len(r.Logs) > 0 {
		w, err := mw.CreateFormFile("logs", "logs.zip")
		if err != nil {
			return err
		}
		if _, err := w.Write(r.Logs); err != nil {
			return err
		}
	}
	if err := mw.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Relay, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("User-Agent", "PZLauncher/"+r.LauncherVersion)
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&out)
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return ErrRateLimited
	case resp.StatusCode/100 != 2:
		if out.Error != "" {
			return fmt.Errorf("report not sent: %s", out.Error)
		}
		return fmt.Errorf("report not sent: relay answered %s", resp.Status)
	}
	return nil
}
