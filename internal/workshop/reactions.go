package workshop

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
)

// Reaction is a Steam Points award an item has received (shown on its page).
type Reaction struct {
	ID    int
	Count int
	Name  string
}

// ReactionIconURL is Steam's still icon for a reaction.
func ReactionIconURL(id int) string {
	return fmt.Sprintf("https://store.fastly.steamstatic.com/public/images/loyalty/reactions/still/%d.png", id)
}

var (
	reactionRe     = regexp.MustCompile(`data-reaction="(\d{1,4})"\s+data-reactioncount="(\d{1,9})"(?:\s+data-tooltip-html="([^"]*)")?`)
	reactionNameRe = regexp.MustCompile(`reaction_award_name&quot;&gt;(.{1,80}?)&lt;`)
)

// ParseReactions reads the reactions from an item's Steam page, most given
// first as Steam lists them.
func ParseReactions(page []byte) []Reaction {
	var out []Reaction
	seen := map[int]bool{}
	for _, m := range reactionRe.FindAllSubmatch(page, -1) {
		id, _ := strconv.Atoi(string(m[1]))
		n, _ := strconv.Atoi(string(m[2]))
		if seen[id] || n <= 0 {
			continue
		}
		seen[id] = true
		r := Reaction{ID: id, Count: n}
		if nm := reactionNameRe.FindSubmatch(m[3]); nm != nil {
			r.Name = html.UnescapeString(string(nm[1]))
		}
		out = append(out, r)
	}
	return out
}

// Reactions fetches an item's public Steam page and reads its reactions.
func Reactions(ctx context.Context, client *http.Client, id string) ([]Reaction, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ItemURL(id), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := readBody(resp, 8<<20)
	if err != nil {
		return nil, err
	}
	return ParseReactions(body), nil
}
