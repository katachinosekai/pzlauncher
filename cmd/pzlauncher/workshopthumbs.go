package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kata/pzlauncher/internal/modlist"
)

const thumbSide = 128

const thumbRetry = time.Minute

type thumbCache struct {
	mu       sync.Mutex
	mem      map[string]image.Image
	order    []string
	inflight map[string][]func(image.Image)
	failed   map[string]time.Time
	sem      chan struct{}
	dir      string
	fetch    func(ctx context.Context, u string) ([]byte, error)
}

func newThumbCache(fetch func(ctx context.Context, u string) ([]byte, error)) *thumbCache {
	dir := ""
	if d, err := os.UserCacheDir(); err == nil {
		dir = filepath.Join(d, "PZLauncher", "thumbs")
	}
	if fetch == nil {
		fetch = httpFetch
	}
	return &thumbCache{mem: map[string]image.Image{}, inflight: map[string][]func(image.Image){}, failed: map[string]time.Time{}, sem: make(chan struct{}, 4), dir: dir, fetch: fetch}
}

func thumbURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.RawQuery != "" || !strings.HasSuffix(u.Hostname(), "steamusercontent.com") {
		return raw
	}
	return raw + "?imw=128&imh=128&ima=fit&impolicy=Letterbox&imcolor=%23000000&letterbox=false"
}

func httpFetch(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

func (c *thumbCache) Get(ctx context.Context, u string, done func(image.Image)) image.Image {
	if u == "" {
		return nil
	}
	c.mu.Lock()
	if im, ok := c.mem[u]; ok {
		c.mu.Unlock()
		return im
	}
	if t, ok := c.failed[u]; ok && time.Since(t) < thumbRetry {
		c.mu.Unlock()
		return nil
	}
	waiting, busy := c.inflight[u]
	c.inflight[u] = append(waiting, done)
	c.mu.Unlock()
	if !busy {
		go c.load(ctx, u)
	}
	return nil
}

func (c *thumbCache) load(ctx context.Context, u string) {
	c.sem <- struct{}{}
	im := c.loadImage(ctx, u)
	<-c.sem
	c.mu.Lock()
	cbs := c.inflight[u]
	delete(c.inflight, u)
	if im == nil {
		c.failed[u] = time.Now()
	} else {
		delete(c.failed, u)
		c.mem[u] = im
		c.order = append(c.order, u)
		if len(c.order) > 600 {
			delete(c.mem, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.mu.Unlock()
	if im == nil {
		return
	}
	for _, cb := range cbs {
		if cb != nil {
			cb(im)
		}
	}
}

func isRemote(u string) bool {
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}

func toNRGBA(im image.Image) image.Image {
	if _, ok := im.(*image.NRGBA); ok {
		return im
	}
	b := im.Bounds()
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), im, b.Min, draw.Src)
	return dst
}

func (c *thumbCache) loadImage(ctx context.Context, u string) image.Image {
	if !isRemote(u) {
		im, err := modlist.LoadImage(u, thumbSide)
		if err != nil {
			return nil
		}
		return toNRGBA(im)
	}
	var file string
	if c.dir != "" {
		sum := sha1.Sum([]byte(u))
		file = filepath.Join(c.dir, hex.EncodeToString(sum[:]))
		if b, err := os.ReadFile(file); err == nil {
			if im, _, err := image.Decode(bytes.NewReader(b)); err == nil {
				return im
			}
		}
	}
	b, err := c.fetch(ctx, thumbURL(u))
	if err != nil {
		return nil
	}
	im, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	im = toNRGBA(modlist.Downscale(im, thumbSide))
	if file != "" {
		var buf bytes.Buffer
		if png.Encode(&buf, im) == nil && os.MkdirAll(c.dir, 0o755) == nil {
			_ = os.WriteFile(file, buf.Bytes(), 0o644)
		}
	}
	return im
}
