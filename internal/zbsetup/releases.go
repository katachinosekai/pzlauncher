package zbsetup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/kata/pzlauncher/internal/update"
)

const (
	Repo       = "zed-0xff/ZombieBuddy"
	DefaultAPI = "https://api.github.com"
	cacheName  = "releases.json"
	maxList    = 8 << 20
)

var reReleaseTag = regexp.MustCompile(`^v\d+(\.\d+)*$`)

type Asset struct {
	update.Asset
	SHA256 string
}

type Release struct {
	Tag        string
	Version    string
	Published  time.Time
	Prerelease bool
	Jar        *Asset
	Native     *Asset
	ZBS        *Asset
}

type ghAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type ghRelease struct {
	Tag        string    `json:"tag_name"`
	Draft      bool      `json:"draft"`
	Prerelease bool      `json:"prerelease"`
	Published  time.Time `json:"published_at"`
	Assets     []ghAsset `json:"assets"`
}

func ParseReleases(data []byte) ([]Release, error) {
	var raw []ghRelease
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("GitHub release list: %w", err)
	}
	var out []Release
	for _, r := range raw {
		if r.Draft || !reReleaseTag.MatchString(r.Tag) {
			continue
		}
		rel := Release{Tag: r.Tag, Version: strings.TrimPrefix(r.Tag, "v"), Published: r.Published, Prerelease: r.Prerelease}
		for _, a := range r.Assets {
			x := &Asset{Asset: update.Asset{Name: a.Name, URL: a.URL, Size: a.Size}}
			if h, ok := strings.CutPrefix(strings.ToLower(a.Digest), "sha256:"); ok && len(h) == 64 {
				x.SHA256 = h
			}
			switch a.Name {
			case JarName:
				rel.Jar = x
			case NativeName:
				rel.Native = x
			case ZBSName:
				rel.ZBS = x
			}
		}
		if rel.Jar == nil {
			continue
		}
		out = append(out, rel)
	}
	sort.SliceStable(out, func(i, j int) bool { return Compare(out[i].Version, out[j].Version) > 0 })
	return out, nil
}

func Latest(rels []Release) *Release {
	for i := range rels {
		if !rels[i].Prerelease {
			return &rels[i]
		}
	}
	return nil
}

type Client struct {
	API      string
	HTTP     *http.Client
	CacheDir string
	TTL      time.Duration
	Now      func() time.Time
}

func NewClient() *Client {
	dir := ""
	if d, err := update.CacheDir(); err == nil {
		dir = filepath.Join(d, "zombiebuddy")
	}
	return &Client{API: DefaultAPI, HTTP: &http.Client{Timeout: 5 * time.Minute}, CacheDir: dir, TTL: time.Hour}
}

func (c *Client) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

type ListResult struct {
	Releases  []Release
	FromCache bool
	Stale     bool
	FetchErr  error
}

func (c *Client) Releases(ctx context.Context, force bool) (ListResult, error) {
	cache := ""
	if c.CacheDir != "" {
		cache = filepath.Join(c.CacheDir, cacheName)
	}
	var cached []byte
	var cachedAt time.Time
	if cache != "" {
		if st, err := os.Stat(cache); err == nil {
			if b, err := os.ReadFile(cache); err == nil {
				cached, cachedAt = b, st.ModTime()
			}
		}
	}
	if cached != nil && !force && c.now().Sub(cachedAt) < c.TTL {
		if rels, err := ParseReleases(cached); err == nil {
			return ListResult{Releases: rels, FromCache: true}, nil
		}
	}
	data, err := c.fetchList(ctx)
	if err == nil {
		rels, perr := ParseReleases(data)
		if perr == nil {
			if cache != "" && os.MkdirAll(c.CacheDir, 0o755) == nil {
				_ = os.WriteFile(cache, data, 0o644)
			}
			return ListResult{Releases: rels}, nil
		}
		err = perr
	}
	if cached != nil {
		if rels, perr := ParseReleases(cached); perr == nil {
			return ListResult{Releases: rels, FromCache: true, Stale: true, FetchErr: err}, nil
		}
	}
	return ListResult{FetchErr: err}, err
}

func (c *Client) fetchList(ctx context.Context) ([]byte, error) {
	api := c.API
	if api == "" {
		api = DefaultAPI
	}
	url := strings.TrimRight(api, "/") + "/repos/" + Repo + "/releases?per_page=100"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "PZLauncher")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub: %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxList))
}

type Fetched struct {
	Jar     string
	Info    JarInfo
	Sig     SigStatus
	Release Release
}

func (c *Client) Fetch(ctx context.Context, rel Release, progress update.Progress) (Fetched, error) {
	f := Fetched{Release: rel}
	if rel.Jar == nil {
		return f, errors.New("release has no ZombieBuddy.jar")
	}
	if rel.Jar.SHA256 == "" {
		return f, fmt.Errorf("GitHub gives no checksum for %s %s; download refused", rel.Tag, JarName)
	}
	if c.CacheDir == "" {
		return f, errors.New("no cache folder")
	}
	dir := filepath.Join(c.CacheDir, rel.Tag)
	dl := &update.Client{HTTP: c.httpClient()}
	jar := filepath.Join(dir, JarName)
	if !isFile(jar) || !hashIs(jar, rel.Jar.SHA256) {
		if err := dl.Download(ctx, rel.Jar.Asset, rel.Jar.SHA256, jar, progress); err != nil {
			return f, err
		}
	}
	zbs := filepath.Join(dir, ZBSName)
	if rel.ZBS != nil && rel.ZBS.SHA256 != "" && (!isFile(zbs) || !hashIs(zbs, rel.ZBS.SHA256)) {
		if err := dl.Download(ctx, rel.ZBS.Asset, rel.ZBS.SHA256, zbs, nil); err != nil {
			return f, err
		}
	}
	return checkJar(jar, zbs, f)
}

func hashIs(p, want string) bool {
	got, err := fileSHA256(p)
	return err == nil && strings.EqualFold(got, want)
}

func checkJar(jar, zbs string, f Fetched) (Fetched, error) {
	info, err := VerifyJar(jar)
	if err != nil {
		return f, err
	}
	f.Jar, f.Info = jar, info
	f.Sig = VerifyZBS(jar, zbs)
	if f.Sig == SignedBad {
		return f, fmt.Errorf("%s: %s", jar, f.Sig)
	}
	return f, nil
}

func CheckLocal(libsDir string) (Fetched, error) {
	return checkJar(filepath.Join(libsDir, JarName), filepath.Join(libsDir, ZBSName), Fetched{})
}
