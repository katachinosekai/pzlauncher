package safeurl

import "testing"

func TestCheck(t *testing.T) {
	for _, c := range []struct {
		in string
		ok bool
	}{
		{"https://steamcommunity.com/sharedfiles/filedetails/?id=1&a=2", true},
		{"HTTPS://github.com/zed-0xff/ZombieBuddy", true},
		{"steam://url/CommunityFilePage/1", true},
		{"steam://exit", true},
		{"http://example.com/", false},
		{"file:///etc/passwd", false},
		{"javascript:alert(1)", false},
		{"mailto:someone@example.com", false},
		{"/home/user/x.sh", false},
		{"-v", false},
		{"https://", false},
		{"https://user:pw@example.com/", false},
		{"https://example.com/ x", false},
		{"https://example.com/\nx", false},
		{"", false},
	} {
		if err := Check(c.in); (err == nil) != c.ok {
			t.Errorf("%q: got %v, want ok=%v", c.in, err, c.ok)
		}
	}
}
