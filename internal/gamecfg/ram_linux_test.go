package gamecfg

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseMeminfo(t *testing.T) {
	sc := bufio.NewScanner(strings.NewReader("MemTotal:       32768000 kB\nMemFree: 1 kB\n"))
	if got := parseMeminfo(sc); got != 32000 {
		t.Fatal(got)
	}
}
