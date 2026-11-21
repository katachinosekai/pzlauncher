package safeurl

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

var ErrRefused = errors.New("only https:// and steam:// links can be opened")

func Check(raw string) error {
	if raw == "" || strings.IndexFunc(raw, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return fmt.Errorf("%w: %q", ErrRefused, raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRefused, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		if u.Host == "" || u.User != nil {
			return fmt.Errorf("%w: %q", ErrRefused, raw)
		}
		return nil
	case "steam":
		return nil
	}
	return fmt.Errorf("%w: %q", ErrRefused, raw)
}
