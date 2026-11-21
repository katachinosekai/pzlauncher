package workshop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fixture shaped like Steam's item page markup (values made up)
const reactionPage = `<div class="review_award_ctn">
<div role="button" class="review_award tooltip purchaseable" onClick="PublishedFileAward( '100', 0, 34 )" data-reaction="34" data-reactioncount="2" data-tooltip-html="&lt;div class=&quot;reaction_award_name&quot;&gt;Pure Gold&lt;/div&gt;">
<img class="review_award_icon" src="https://store.fastly.steamstatic.com/public/images/loyalty/reactions/still/34.png?v=5"/><span class="review_award_count ">2</span></div>
<div role="button" class="review_award tooltip purchaseable" onClick="PublishedFileAward( '100', 0, 24 )" data-reaction="24" data-reactioncount="1" data-tooltip-html="&lt;div class=&quot;reaction_award_name&quot;&gt;Mind Blown &amp; Wow&lt;/div&gt;">
<span class="review_award_count hidden">1</span></div>
<div class="review_award" data-reaction="7" data-reactioncount="15"></div>
<div class="review_award" data-reaction="34" data-reactioncount="2"></div>
<div class="review_award" data-reaction="9" data-reactioncount="0"></div>
<div role="button" class="review_award more_btn tooltip" onClick="PublishedFile_ShowMoreAwards( this )" data-count="62"></div>
</div>`

func TestParseReactions(t *testing.T) {
	got := ParseReactions([]byte(reactionPage))
	want := []Reaction{{34, 2, "Pure Gold"}, {24, 1, "Mind Blown & Wow"}, {7, 15, ""}}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %+v want %+v", i, got[i], want[i])
		}
	}
	if ParseReactions([]byte("<html>no awards</html>")) != nil {
		t.Error("a page without awards has no reactions")
	}
	if ReactionIconURL(34) != "https://store.fastly.steamstatic.com/public/images/loyalty/reactions/still/34.png" {
		t.Error(ReactionIconURL(34))
	}
}

func TestReactionsFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(reactionPage))
	}))
	defer srv.Close()
	client := srv.Client()
	client.Transport = rewrite{srv.URL, client.Transport}
	got, err := Reactions(context.Background(), client, "100")
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %+v", err, got)
	}
}

// rewrite sends every request to the test server.
type rewrite struct {
	to   string
	next http.RoundTripper
}

func (r rewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *req.URL
	host := strings.TrimPrefix(r.to, "http://")
	u.Scheme, u.Host = "http", host
	req2 := req.Clone(req.Context())
	req2.URL = &u
	req2.Host = host
	return r.next.RoundTrip(req2)
}
