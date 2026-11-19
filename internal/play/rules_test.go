package play

import (
	"bytes"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

func rulesPayload(pairs ...string) []byte {
	b := []byte{0xff, 0xff, 0xff, 0xff, 'E'}
	b = binary.LittleEndian.AppendUint16(b, uint16(len(pairs)/2))
	for _, s := range pairs {
		b = append(append(b, s...), 0)
	}
	return b
}

var realRules = []string{
	"description", "",
	"lastWipe", "1794832700",
	"modCount", "339",
	"mods:1/1", "ZombieBuddy;PZArrayListCompat;EngineLoadTimeFixes;MapVisitedSaveFix;zdk;ModLoadOrderSorter_b42;ModManager;ProjectALifeNPCs;ETO_",
	"open", "1",
	"public", "0",
	"pvp", "1",
	"version", "42.22",
}

func TestParseA2SRules(t *testing.T) {
	r, err := parseA2SRules(rulesPayload(realRules...)[4:])
	if err != nil {
		t.Fatal(err)
	}
	if r["version"] != "42.22" || r["modCount"] != "339" || r["open"] != "1" || r["description"] != "" || len(r) != 8 {
		t.Fatalf("got %v", r)
	}
	ids, truncated := ServerModsPrefix(r)
	if !truncated || len(ids) != 8 || ids[0] != "ZombieBuddy" || ids[7] != "ProjectALifeNPCs" {
		t.Fatalf("mods prefix %v %v", ids, truncated)
	}
	short, trunc := ServerModsPrefix(map[string]string{"mods": "A;B"})
	if trunc || strings.Join(short, ",") != "A,B" {
		t.Fatalf("short list %v %v", short, trunc)
	}
	if _, err := parseA2SRules([]byte("I")); err == nil {
		t.Fatal("wrong header must fail")
	}
	r, err = parseA2SRules(rulesPayload("a", "1", "b")[4:])
	if err != nil || r["a"] != "1" || len(r) != 1 {
		t.Fatalf("cut reply: %v %v", r, err)
	}
}

func splitPackets(payload []byte, id uint32, size int) [][]byte {
	var chunks [][]byte
	for len(payload) > 0 {
		n := min(size, len(payload))
		chunks = append(chunks, payload[:n])
		payload = payload[n:]
	}
	var out [][]byte
	for i, c := range chunks {
		p := append([]byte{}, splitHeader...)
		p = binary.LittleEndian.AppendUint32(p, id)
		p = append(p, byte(len(chunks)), byte(i))
		p = binary.LittleEndian.AppendUint16(p, uint16(size))
		out = append(out, append(p, c...))
	}
	return out
}

type fakeA2S struct {
	pc        net.PacketConn
	challenge []byte
	split     int
	reverse   bool
	info      []byte
}

func (f *fakeA2S) serve() {
	buf := make([]byte, 1500)
	for {
		n, addr, err := f.pc.ReadFrom(buf)
		if err != nil {
			return
		}
		req := append([]byte{}, buf[:n]...)
		switch {
		case n >= 5 && req[4] == 'T':
			f.pc.WriteTo(f.info, addr)
		case n == 9 && req[4] == 'V' && !bytes.Equal(req[5:9], f.challenge):
			f.pc.WriteTo(append([]byte{0xff, 0xff, 0xff, 0xff, 'A'}, f.challenge...), addr)
		case n == 9 && req[4] == 'V':
			payload := rulesPayload(realRules...)
			if f.split == 0 {
				f.pc.WriteTo(payload, addr)
				continue
			}
			pk := splitPackets(payload, 0x1234, f.split)
			if f.reverse {
				for i, j := 0, len(pk)-1; i < j; i, j = i+1, j-1 {
					pk[i], pk[j] = pk[j], pk[i]
				}
			}
			for _, p := range pk {
				f.pc.WriteTo(p, addr)
			}
		}
	}
}

func startFake(t *testing.T, f *fakeA2S) int {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	f.pc = pc
	go f.serve()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

func TestQueryRulesChallenge(t *testing.T) {
	for name, f := range map[string]*fakeA2S{
		"single":         {challenge: []byte{9, 8, 7, 6}},
		"split":          {challenge: []byte{1, 2, 3, 4}, split: 64},
		"split reversed": {challenge: []byte{1, 2, 3, 4}, split: 50, reverse: true},
	} {
		port := startFake(t, f)
		r, err := QueryRules("127.0.0.1", port, time.Second)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if r["version"] != "42.22" || r["modCount"] != "339" || len(r) != 8 {
			t.Fatalf("%s: %v", name, r)
		}
	}
}

func TestSplitPacketRejects(t *testing.T) {
	p := splitPackets([]byte("xxxx"), 0x80000001, 4)[0]
	if _, err := parseSplitPacket(p); err == nil || !strings.Contains(err.Error(), "compressed") {
		t.Fatalf("compressed: %v", err)
	}
	if _, err := parseSplitPacket([]byte{0xfe, 0xff, 0xff, 0xff, 1}); err == nil {
		t.Fatal("short split packet must fail")
	}
}

func realInfo() []byte {
	b := []byte{0xff, 0xff, 0xff, 0xff, 'I', 0x11}
	for _, s := range []string{"Kata's PZ Server", "vehicle_interior_arcadia_realus", "zomboid", "Project Zomboid"} {
		b = append(append(b, s...), 0)
	}
	b = append(b, 0, 0, 0, 8, 0, 'd', 'w', 1, 1)
	b = append(append(b, "1.0.0.0"...), 0)
	b = append(b, 0xb1, 0x85, 0x3f)
	b = append(b, 0x14, 0xa0, 0xe6, 0x6e, 0x61, 0xcd, 0x40, 0x01)
	b = append(append(b, "hidden;modded;pvp;VERSION:42.22"...), 0)
	return append(b, 0x38, 0xa8, 0x01, 0, 0, 0, 0, 0)
}

func TestParseA2SInfoExtended(t *testing.T) {
	st, err := parseA2SInfo(realInfo(), time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if st.Name != "Kata's PZ Server" || st.Players != 0 || st.MaxPlayers != 8 || !st.Password || st.Keywords != "hidden;modded;pvp;VERSION:42.22" {
		t.Fatalf("got %+v", st)
	}
	if st.Version() != "42.22" || !st.Open() {
		t.Fatalf("version/open from keywords: %q %v", st.Version(), st.Open())
	}
	st, err = parseA2SInfo(a2sReply("x", 1, 2), time.Millisecond)
	if err != nil || st.Password || st.Keywords != "" {
		t.Fatalf("plain reply: %+v %v", st, err)
	}
	if (ServerStatus{Keywords: "closed;pvp"}).Open() {
		t.Fatal("closed keyword")
	}
}

func TestQueryServerWithRules(t *testing.T) {
	f := &fakeA2S{challenge: []byte{5, 5, 5, 5}, info: realInfo()}
	port := startFake(t, f)
	st := QueryServer("127.0.0.1", port, time.Second)
	if !st.Online || !st.Password || st.Rules["version"] != "42.22" {
		t.Fatalf("got %+v", st)
	}
}
