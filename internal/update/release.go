package update

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var Repo = "katachinosekai/pzlauncher"

var ReleasePublicKey = "1esjE+gIQaDPJ48Rojr/mQeNTFonUHuBPiJOrN4/7VM="

var ErrUnsigned = errors.New("the release's manifest.json is not signed with the launcher's release key; refusing to use it")

const (
	SignatureName    = "manifest.json.sig"
	ManifestName     = "manifest.json"
	WindowsBinary    = "PZLauncher-windows-amd64.exe"
	LinuxBinary      = "PZLauncher-linux-x86_64.AppImage"
	defaultAPI       = "https://api.github.com"
	maxManifestBytes = 1 << 20
)

var packName = regexp.MustCompile(`^PZ-Fixes(-[^/\\]+)?\.zip$`)

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

type Manifest struct {
	PackVersion     string            `json:"pack_version"`
	LauncherVersion string            `json:"launcher_version"`
	SHA256          map[string]string `json:"sha256"`
}

type Release struct {
	Tag      string
	Name     string
	Assets   []Asset
	Manifest Manifest
	Pack     *Asset
}

func (r *Release) Asset(name string) *Asset {
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			return &r.Assets[i]
		}
	}
	return nil
}

func (r *Release) Hash(name string) string {
	return strings.ToLower(strings.TrimSpace(r.Manifest.SHA256[name]))
}

type Client struct {
	Repo     string
	APIBase  string
	HTTP     *http.Client
	CacheDir string
}

func NewClient() *Client {
	return &Client{Repo: Repo, APIBase: defaultAPI, HTTP: &http.Client{Timeout: 0}}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) get(ctx context.Context, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "PZLauncher")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

func (c *Client) Latest(ctx context.Context) (*Release, error) {
	repo := c.Repo
	if repo == "" {
		repo = Repo
	}
	if strings.HasPrefix(repo, "OWNER/") || strings.Count(repo, "/") != 1 {
		return nil, fmt.Errorf("update repository is not configured (%q)", repo)
	}
	base := strings.TrimRight(c.APIBase, "/")
	if base == "" {
		base = defaultAPI
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := c.get(ctx, base+"/repos/"+repo+"/releases/latest", "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw struct {
		TagName string  `json:"tag_name"`
		Name    string  `json:"name"`
		Assets  []Asset `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("parsing release: %w", err)
	}
	rel := &Release{Tag: raw.TagName, Name: raw.Name, Assets: raw.Assets}
	for i := range rel.Assets {
		a := &rel.Assets[i]
		if a.Name != path.Base(a.Name) {
			return nil, fmt.Errorf("bad asset name %q", a.Name)
		}
		if packName.MatchString(a.Name) && rel.Pack == nil {
			rel.Pack = a
		}
	}
	m := rel.Asset(ManifestName)
	if m == nil {
		return nil, errors.New("release has no manifest.json")
	}
	sa := rel.Asset(SignatureName)
	if sa == nil {
		return nil, ErrUnsigned
	}
	mb, err := c.fetchSmall(ctx, m.URL, maxManifestBytes)
	if err != nil {
		return nil, err
	}
	sb, err := c.fetchSmall(ctx, sa.URL, 4096)
	if err != nil {
		return nil, err
	}
	if !VerifyManifest(mb, sb) {
		return nil, ErrUnsigned
	}
	if err := json.Unmarshal(mb, &rel.Manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest.json: %w", err)
	}
	return rel, nil
}

func BinaryName(goos, goarch string) string {
	switch {
	case goos == "windows" && goarch == "amd64":
		return WindowsBinary
	case goos == "linux" && goarch == "amd64":
		return LinuxBinary
	}
	return ""
}

func CurrentBinaryName() string { return BinaryName(runtime.GOOS, runtime.GOARCH) }

func versionParts(v string) []int {
	v = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(v), "v"), "V"))
	v, _, _ = strings.Cut(v, "+")
	v, _, _ = strings.Cut(v, "-")
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}

func Compare(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func LauncherOutdated(latest, current string) bool {
	if current == "" || current == "dev" {
		return false
	}
	return Newer(latest, current)
}

func Newer(latest, current string) bool {
	if strings.TrimSpace(latest) == "" {
		return false
	}
	if strings.TrimSpace(current) == "" {
		return true
	}
	return Compare(latest, current) > 0
}

func (c *Client) fetchSmall(ctx context.Context, url string, limit int64) ([]byte, error) {
	resp, err := c.get(ctx, url, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s is too large", path.Base(url))
	}
	return b, nil
}

func VerifyManifest(manifest, sig []byte) bool {
	pub, err := base64.StdEncoding.DecodeString(ReleasePublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return false
	}
	s, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(s) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), manifest, s)
}
