package webview

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
)

func DecodeQR(dataURL string) (image.Image, error) {
	i := strings.Index(dataURL, ";base64,")
	if !strings.HasPrefix(dataURL, "data:image/") || i < 0 {
		return nil, errors.New("not an image data URL")
	}
	b, err := base64.StdEncoding.DecodeString(dataURL[i+8:])
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	if r := img.Bounds(); r.Dx() < 21 || r.Dy() < 21 {
		return nil, errors.New("QR image too small")
	}
	return img, nil
}
