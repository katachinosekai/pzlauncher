package packs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxPackBytes = 512 << 20

var (
	reOwnerRepo = regexp.MustCompile(`^[A-Za-z0-9-]{1,39}/[A-Za-z0-9._-]{1,100}$`)
	reAssetName = regexp.MustCompile(`^[^/\\]+\.zip$`)
)

type Source struct {
	Repo  string
	Tag   string
	Asset string
	URL   string
}

func (s Source) String() string {
	if s.Repo != "" {
		t := s.Tag
		if t == "" {
			t = "latest release"
		}
		return s.Repo + " (" + t + ")"
	}
	return s.URL
}

func ParseSource(in string) (Source, error) {
	in = strings.TrimSpace(in)
	in = strings.TrimSuffix(in, "/")
	if reOwnerRepo.MatchString(in) {
		return Source{Repo: in}, nil
	}
	u, err := url.Parse(in)
	if err != nil || u.Host == "" {
		return Source{}, errors.New("enter a GitHub repo (owner/repo) or an https link")
	}
	if u.Scheme != "https" {
		return Source{}, errors.New("only https links are allowed")
	}
	host := strings.ToLower(u.Hostname())
	if host == "github.com" || host == "www.github.com" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 || !reOwnerRepo.MatchString(parts[0]+"/"+parts[1]) {
			return Source{}, errors.New("that GitHub link doesn't name a repo")
		}
		s := Source{Repo: parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")}
		if len(parts) >= 5 && parts[2] == "releases" && parts[3] == "tag" {
			s.Tag = parts[4]
		}
		if len(parts) >= 6 && parts[2] == "releases" && parts[3] == "download" {
			s.Tag, s.Asset = parts[4], parts[5]
		}
		if len(parts) >= 6 && parts[2] == "releases" && parts[3] == "latest" && parts[4] == "download" {
			s.Asset = parts[5]
		}
		return s, nil
	}
	if !strings.HasSuffix(strings.ToLower(u.Path), ".zip") {
		return Source{}, errors.New("a direct link must end in .zip")
	}
	return Source{URL: u.String()}, nil
}

type Client struct {
	HTTP    *http.Client
	APIBase string
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

func (c *Client) get(ctx context.Context, u, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "PZLauncher")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("not found (%s); private repos can't be downloaded", u)
		}
		return nil, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	return resp, nil
}

func (c *Client) resolve(ctx context.Context, s Source) (name, link string, err error) {
	if s.URL != "" {
		return path.Base(s.URL), s.URL, nil
	}
	base := strings.TrimRight(c.APIBase, "/")
	if base == "" {
		base = "https://api.github.com"
	}
	api := base + "/repos/" + s.Repo + "/releases/latest"
	if s.Tag != "" {
		api = base + "/repos/" + s.Repo + "/releases/tags/" + url.PathEscape(s.Tag)
	}
	resp, err := c.get(ctx, api, "application/vnd.github+json")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	var rel struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&rel); err != nil {
		return "", "", fmt.Errorf("reading the release: %w", err)
	}
	for _, a := range rel.Assets {
		if !reAssetName.MatchString(a.Name) || !strings.HasPrefix(a.URL, "https://") {
			continue
		}
		if s.Asset == "" || a.Name == s.Asset {
			return a.Name, a.URL, nil
		}
	}
	return "", "", fmt.Errorf("the release of %s has no .zip file", s.Repo)
}

func (c *Client) Fetch(ctx context.Context, s Source, dir string, progress func(done, total int64)) (string, error) {
	name, link, err := c.resolve(ctx, s)
	if err != nil {
		return "", err
	}
	if !reAssetName.MatchString(name) {
		name = "pack.zip"
	}
	resp, err := c.get(ctx, link, "application/octet-stream")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.ContentLength > maxPackBytes {
		return "", errors.New("the pack is larger than 512 MB")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, name)
	tmp, err := os.CreateTemp(dir, ".dl-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > maxPackBytes {
				tmp.Close()
				return "", errors.New("the pack is larger than 512 MB")
			}
			if _, werr := tmp.Write(buf[:n]); werr != nil {
				tmp.Close()
				return "", werr
			}
			if progress != nil {
				progress(done, resp.ContentLength)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			tmp.Close()
			return "", rerr
		}
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return "", err
	}
	return dest, nil
}
