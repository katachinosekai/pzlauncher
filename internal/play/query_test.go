package play

import (
	"net"
	"strings"
	"testing"
	"time"
)

func a2sReply(name string, players, max byte) []byte {
	b := []byte{0xff, 0xff, 0xff, 0xff, 'I', 17}
	for _, s := range []string{name, "map", "folder", "game"} {
		b = append(append(b, s...), 0)
	}
	return append(b, 0, 0, players, max, 0)
}

func TestQueryServerA2SWithChallenge(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	go func() {
		buf := make([]byte, 1500)
		for i := 0; i < 2; i++ {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n == len(a2sInfoRequest) {
				pc.WriteTo([]byte{0xff, 0xff, 0xff, 0xff, 'A', 1, 2, 3, 4}, addr)
			} else {
				pc.WriteTo(a2sReply("Test Server", 3, 8), addr)
			}
		}
	}()
	port := pc.LocalAddr().(*net.UDPAddr).Port
	st := QueryServer("127.0.0.1", port, time.Second)
	if !st.Online || st.Name != "Test Server" || st.Players != 3 || st.MaxPlayers != 8 {
		t.Fatalf("got %+v", st)
	}
	if !strings.Contains(st.String(), "3/8 players") {
		t.Fatalf("string: %q", st.String())
	}
}

func TestQueryServerOffline(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()
	st := QueryServer("127.0.0.1", port, 300*time.Millisecond)
	if st.Online || st.String() != "offline" {
		t.Fatalf("got %+v", st)
	}
}

func TestStatusFull(t *testing.T) {
	st := ServerStatus{Online: true, Players: 8, MaxPlayers: 8, HasPlayers: true, Ping: 25 * time.Millisecond}
	if st.String() != "online · 25 ms · 8/8 players · full" {
		t.Fatalf("got %q", st.String())
	}
}
