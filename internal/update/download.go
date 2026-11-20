package update

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrChecksum = errors.New("checksum mismatch")

const maxExtractBytes = 4 << 30

type Progress func(done, total int64)

type progressWriter struct {
	done, total int64
	fn          Progress
}

func (p *progressWriter) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.fn != nil {
		p.fn(p.done, p.total)
	}
	return len(b), nil
}

func (c *Client) Download(ctx context.Context, a Asset, wantSHA256, dest string, progress Progress) error {
	want := strings.ToLower(strings.TrimSpace(wantSHA256))
	if len(want) != 64 {
		return fmt.Errorf("no valid sha256 for %s in manifest", a.Name)
	}
	resp, err := c.get(ctx, a.URL, "application/octet-stream")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".part-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	total := resp.ContentLength
	if total <= 0 {
		total = a.Size
	}
	h := sha256.New()
	pw := &progressWriter{total: total, fn: progress}
	if _, err := io.Copy(io.MultiWriter(tmp, h, pw), resp.Body); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("%w for %s: expected %s, got %s", ErrChecksum, a.Name, want, got)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return err
	}
	ok = true
	return nil
}

func CacheDir() (string, error) {
	d, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "PZLauncher"), nil
}

func (c *Client) cacheDir() (string, error) {
	if c.CacheDir != "" {
		return c.CacheDir, nil
	}
	return CacheDir()
}

func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			return r
		}
		return '_'
	}, s)
}

func (c *Client) FetchPack(ctx context.Context, rel *Release, progress Progress) (string, error) {
	if rel.Pack == nil {
		return "", errors.New("release has no PZ-Fixes zip")
	}
	cache, err := c.cacheDir()
	if err != nil {
		return "", err
	}
	version := rel.Manifest.PackVersion
	if version == "" {
		version = rel.Tag
	}
	zipPath := filepath.Join(cache, "downloads", rel.Pack.Name)
	if err := c.Download(ctx, *rel.Pack, rel.Hash(rel.Pack.Name), zipPath, progress); err != nil {
		return "", err
	}
	dest := filepath.Join(cache, "packs", safeName("pack-"+version))
	if err := os.RemoveAll(dest); err != nil {
		return "", err
	}
	if err := Extract(zipPath, dest); err != nil {
		os.RemoveAll(dest)
		return "", err
	}
	os.Remove(zipPath)
	return PackRoot(dest), nil
}

func PackRoot(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() {
		return dir
	}
	return filepath.Join(dir, entries[0].Name())
}

func entryPath(dest, name string) (string, error) {
	n := strings.ReplaceAll(name, `\`, "/")
	if n == "" || strings.HasPrefix(n, "/") || filepath.IsAbs(n) || filepath.VolumeName(n) != "" || strings.Contains(n, ":") {
		return "", fmt.Errorf("unsafe path in zip: %q", name)
	}
	for _, part := range strings.Split(n, "/") {
		if part == ".." {
			return "", fmt.Errorf("unsafe path in zip: %q", name)
		}
	}
	p := filepath.Join(dest, filepath.FromSlash(n))
	rel, err := filepath.Rel(dest, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe path in zip: %q", name)
	}
	return p, nil
}

func Extract(zipPath, dest string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	dest, err = filepath.Abs(dest)
	if err != nil {
		return err
	}
	var total uint64
	for _, f := range zr.File {
		if _, err := entryPath(dest, f.Name); err != nil {
			return err
		}
		mode := f.Mode()
		if mode&os.ModeSymlink != 0 || (!mode.IsRegular() && !mode.IsDir()) {
			return fmt.Errorf("unsupported entry in zip: %q", f.Name)
		}
		total += f.UncompressedSize64
		if total > maxExtractBytes {
			return errors.New("zip is too large to extract")
		}
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, f := range zr.File {
		p, _ := entryPath(dest, f.Name)
		if f.Mode().IsDir() || strings.HasSuffix(f.Name, "/") {
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := extractFile(f, p); err != nil {
			return err
		}
	}
	return nil
}

func extractFile(f *zip.File, p string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	perm := os.FileMode(0o644)
	if f.Mode()&0o111 != 0 {
		perm = 0o755
	}
	out, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, io.LimitReader(rc, int64(f.UncompressedSize64)+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && uint64(n) != f.UncompressedSize64 {
		err = fmt.Errorf("size mismatch extracting %q", f.Name)
	}
	return err
}

func OpenLocalPack(zipPath string) (dir, version string, err error) {
	sum, err := fileSHA256(zipPath)
	if err != nil {
		return "", "", err
	}
	version = "local-" + sum[:8]
	cache, err := CacheDir()
	if err != nil {
		return "", "", err
	}
	dest := filepath.Join(cache, "packs", safeName("pack-"+version))
	if err := os.RemoveAll(dest); err != nil {
		return "", "", err
	}
	if err := Extract(zipPath, dest); err != nil {
		os.RemoveAll(dest)
		return "", "", err
	}
	root := PackRoot(dest)
	if !isPack(root) {
		os.RemoveAll(dest)
		return "", "", fmt.Errorf("%s is not a PZ-Fixes pack (no settings or payload folder)", filepath.Base(zipPath))
	}
	return root, version, nil
}

func isPack(dir string) bool {
	for _, d := range []string{"settings", "payload"} {
		if fi, err := os.Stat(filepath.Join(dir, d)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
