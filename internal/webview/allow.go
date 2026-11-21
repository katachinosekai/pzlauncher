package webview

import (
	"net/url"
	"strings"
)

type Action int

const (
	Block Action = iota
	Allow
	External
)

func (a Action) String() string {
	switch a {
	case Allow:
		return "allow"
	case External:
		return "external"
	}
	return "block"
}

var pageHosts = map[string]bool{
	"steamcommunity.com":        true,
	"store.steampowered.com":    true,
	"login.steampowered.com":    true,
	"help.steampowered.com":     true,
	"checkout.steampowered.com": true,
}

var assetHostSuffixes = []string{
	".steamstatic.com",
	".steamusercontent.com",
	".steamcontent.com",
}

var assetHosts = map[string]bool{
	"steamcdn-a.akamaihd.net":          true,
	"steamcommunity-a.akamaihd.net":    true,
	"steamuserimages-a.akamaihd.net":   true,
	"steamstore-a.akamaihd.net":        true,
	"community.akamai.steamstatic.com": true,
}

var captchaPrefixes = []string{
	"https://www.google.com/recaptcha/",
	"https://www.recaptcha.net/recaptcha/",
	"https://www.gstatic.com/recaptcha/",
}

func AllowedHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	if pageHosts[host] || assetHosts[host] {
		return true
	}
	for _, s := range assetHostSuffixes {
		if strings.HasSuffix(host, s) {
			return true
		}
	}
	return false
}

func Allowed(raw string) bool { return Decide(raw, false) == Allow }

func Decide(raw string, userGesture bool) Action {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Block
	}
	switch strings.ToLower(u.Scheme) {
	case "about", "data", "blob":
		return Allow
	case "https":
		if u.User != nil || (u.Port() != "" && u.Port() != "443") {
			break
		}
		if AllowedHost(u.Hostname()) {
			return Allow
		}
		low := strings.ToLower(raw)
		for _, p := range captchaPrefixes {
			if strings.HasPrefix(low, p) {
				return Allow
			}
		}
	case "steam":
	default:
		return Block
	}
	if userGesture {
		return External
	}
	return Block
}

func NormalizeTyped(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "://") && !strings.HasPrefix(s, "about:") {
		s = "https://" + s
	}
	return s
}
