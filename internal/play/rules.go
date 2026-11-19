package play

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	singleHeader = []byte{0xff, 0xff, 0xff, 0xff}
	splitHeader  = []byte{0xfe, 0xff, 0xff, 0xff}
)

func rulesRequest(challenge []byte) []byte {
	return append([]byte{0xff, 0xff, 0xff, 0xff, 'V'}, challenge...)
}

func QueryRules(ip string, port int, timeout time.Duration) (map[string]string, error) {
	conn, err := dialA2S(ip, port, timeout)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return queryRulesConn(conn)
}

func queryRulesConn(conn net.Conn) (map[string]string, error) {
	challenge := []byte{0xff, 0xff, 0xff, 0xff}
	for attempt := 0; attempt < 3; attempt++ {
		if _, err := conn.Write(rulesRequest(challenge)); err != nil {
			return nil, err
		}
		payload, err := readA2SPayload(conn)
		if err != nil {
			return nil, err
		}
		if len(payload) >= 5 && payload[0] == 'A' {
			challenge = append([]byte{}, payload[1:5]...)
			continue
		}
		return parseA2SRules(payload)
	}
	return nil, errors.New("server kept sending challenges")
}

type splitPart struct {
	id     uint32
	total  int
	number int
	data   []byte
}

func parseSplitPacket(d []byte) (splitPart, error) {
	if len(d) < 12 || !bytes.Equal(d[:4], splitHeader) {
		return splitPart{}, errors.New("not a split packet")
	}
	p := splitPart{id: binary.LittleEndian.Uint32(d[4:8]), total: int(d[8]), number: int(d[9])}
	if p.id&0x80000000 != 0 {
		return splitPart{}, errors.New("compressed split replies are not supported")
	}
	if p.total == 0 || p.number >= p.total {
		return splitPart{}, errors.New("bad split packet numbering")
	}
	p.data = d[12:]
	return p, nil
}

func readA2SPayload(conn net.Conn) ([]byte, error) {
	buf := make([]byte, 65536)
	var parts map[int][]byte
	var id uint32
	total := 0
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		d := buf[:n]
		if bytes.Equal(d[:min(4, n)], singleHeader) {
			if parts != nil {
				continue
			}
			return append([]byte{}, d[4:]...), nil
		}
		sp, err := parseSplitPacket(d)
		if err != nil {
			return nil, err
		}
		if parts == nil {
			parts, id, total = map[int][]byte{}, sp.id, sp.total
		}
		if sp.id != id || sp.total != total {
			continue
		}
		parts[sp.number] = append([]byte{}, sp.data...)
		if len(parts) < total {
			continue
		}
		var all []byte
		for i := 0; i < total; i++ {
			all = append(all, parts[i]...)
		}
		if !bytes.Equal(all[:min(4, len(all))], singleHeader) {
			return nil, errors.New("bad reassembled reply")
		}
		return all[4:], nil
	}
}

func parseA2SRules(payload []byte) (map[string]string, error) {
	if len(payload) < 3 || payload[0] != 'E' {
		return nil, errors.New("not an A2S_RULES reply")
	}
	count := int(binary.LittleEndian.Uint16(payload[1:3]))
	rules := map[string]string{}
	p := 3
	for i := 0; i < count; i++ {
		k, np, ok := cstring(payload, p)
		if !ok {
			break
		}
		v, np2, ok := cstring(payload, np)
		if !ok {
			break
		}
		rules[k] = v
		p = np2
	}
	return rules, nil
}

const steamValueLimit = 127

func ServerModsPrefix(rules map[string]string) (ids []string, truncated bool) {
	type chunk struct {
		i   int
		val string
	}
	var chunks []chunk
	for k, v := range rules {
		switch {
		case k == "mods":
			chunks = append(chunks, chunk{0, v})
		case strings.HasPrefix(k, "mods:"):
			num, _, _ := strings.Cut(strings.TrimPrefix(k, "mods:"), "/")
			n, err := strconv.Atoi(num)
			if err == nil {
				chunks = append(chunks, chunk{n, v})
			}
		}
	}
	sort.Slice(chunks, func(a, b int) bool { return chunks[a].i < chunks[b].i })
	var all string
	for _, c := range chunks {
		all += c.val
		if len(c.val) >= steamValueLimit {
			truncated = true
		}
	}
	for _, id := range strings.Split(all, ";") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if truncated && len(ids) > 0 {
		ids = ids[:len(ids)-1]
	}
	return ids, truncated
}
