package modlist

import (
	"image"
	"image/color"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"sync"
)

func LoadImage(path string, maxSide int) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	return Downscale(img, maxSide), nil
}

func Downscale(src image.Image, maxSide int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if maxSide <= 0 || (w <= maxSide && h <= maxSide) || w == 0 || h == 0 {
		return src
	}
	nw, nh := maxSide, maxSide
	if w > h {
		nh = max(1, h*maxSide/w)
	} else {
		nw = max(1, w*maxSide/h)
	}
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	for y := range nh {
		y0 := b.Min.Y + y*h/nh
		y1 := max(y0+1, b.Min.Y+(y+1)*h/nh)
		for x := range nw {
			x0 := b.Min.X + x*w/nw
			x1 := max(x0+1, b.Min.X+(x+1)*w/nw)
			var r, g, bl, a, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					c := color.NRGBAModel.Convert(src.At(xx, yy)).(color.NRGBA)
					r += uint64(c.R)
					g += uint64(c.G)
					bl += uint64(c.B)
					a += uint64(c.A)
					n++
				}
			}
			dst.SetNRGBA(x, y, color.NRGBA{uint8(r / n), uint8(g / n), uint8(bl / n), uint8(a / n)})
		}
	}
	return dst
}

type ImageCache struct {
	MaxSide int
	mu      sync.Mutex
	done    map[string]image.Image
	failed  map[string]bool
	waiting map[string][]func(image.Image)
	sem     chan struct{}
}

func NewImageCache(maxSide, workers int) *ImageCache {
	return &ImageCache{
		MaxSide: maxSide,
		done:    map[string]image.Image{},
		failed:  map[string]bool{},
		waiting: map[string][]func(image.Image){},
		sem:     make(chan struct{}, max(1, workers)),
	}
}

func (c *ImageCache) Get(path string, cb func(image.Image)) (image.Image, bool) {
	if path == "" {
		return nil, true
	}
	c.mu.Lock()
	if img, ok := c.done[path]; ok {
		c.mu.Unlock()
		return img, true
	}
	if c.failed[path] {
		c.mu.Unlock()
		return nil, true
	}
	_, pending := c.waiting[path]
	c.waiting[path] = append(c.waiting[path], cb)
	c.mu.Unlock()
	if !pending {
		go c.load(path)
	}
	return nil, false
}

func (c *ImageCache) load(path string) {
	c.sem <- struct{}{}
	img, err := LoadImage(path, c.MaxSide)
	<-c.sem
	c.mu.Lock()
	if err != nil {
		c.failed[path] = true
		img = nil
	} else {
		c.done[path] = img
	}
	cbs := c.waiting[path]
	delete(c.waiting, path)
	c.mu.Unlock()
	for _, cb := range cbs {
		if cb != nil {
			cb(img)
		}
	}
}
