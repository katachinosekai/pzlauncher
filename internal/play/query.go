package play

import (
	"bytes"
	"errors"
	"net"
	"strconv"
	"time"
)

type ServerStatus struct {
	Online     bool
	Name       string
	Players    int
	MaxPlayers int
	Ping       time.Duration
	HasPlayers bool
	Password   bool
	Keywords   string
	Rules      map[string]string
}

func (s ServerStatus) String() string {
	if !s.Online {
		return "offline"
	}
	out := strconv.Itoa(int(s.Ping.Milliseconds())) + " ms"
	if s.Ping < time.Millisecond {
		out = "<1 ms"
	}
	if s.HasPlayers {
		out += " · " + strconv.Itoa(s.Players) + "/" + strconv.Itoa(s.MaxPlayers) + " players"
		if s.MaxPlayers > 0 && s.Players >= s.MaxPlayers {
			out += " · full"
		}
	}
	return "online · " + out
}

var a2sInfoRequest = append([]byte{0xff, 0xff, 0xff, 0xff, 'T'}, append([]byte("Source Engine Query"), 0)...)

func QueryServer(ip string, port int, timeout time.Duration) ServerStatus {
	if st, err := queryA2S(ip, port, timeout); err == nil {
		if rules, err := QueryRules(ip, port, timeout); err == nil {
			st.Rules = rules
		}
		return st
	}
	if d, err := Ping(ip, port+1, timeout); err == nil {
		return ServerStatus{Online: true, Ping: d}
	}
	return ServerStatus{}
}

func dialA2S(ip string, port int, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("udp", net.JoinHostPort(ip, strconv.Itoa(port)), timeout)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func queryA2S(ip string, port int, timeout time.Duration) (ServerStatus, error) {
	conn, err := dialA2S(ip, port, timeout)
	if err != nil {
		return ServerStatus{}, err
	}
	defer conn.Close()
	buf := make([]byte, 4096)
	start := time.Now()
	if _, err := conn.Write(a2sInfoRequest); err != nil {
		return ServerStatus{}, err
	}
	n, err := conn.Read(buf)
	if err != nil {
		return ServerStatus{}, err
	}
	rtt := time.Since(start)
	if n >= 9 && buf[4] == 'A' {
		req := append(append([]byte{}, a2sInfoRequest...), buf[5:9]...)
		start = time.Now()
		if _, err := conn.Write(req); err != nil {
			return ServerStatus{}, err
		}
		if n, err = conn.Read(buf); err != nil {
			return ServerStatus{}, err
		}
		rtt = time.Since(start)
	}
	return parseA2SInfo(buf[:n], rtt)
}

func cstring(d []byte, p int) (string, int, bool) {
	if p > len(d) {
		return "", p, false
	}
	e := bytes.IndexByte(d[p:], 0)
	if e < 0 {
		return "", p, false
	}
	return string(d[p : p+e]), p + e + 1, true
}

func parseA2SInfo(d []byte, rtt time.Duration) (ServerStatus, error) {
	if len(d) < 6 || !bytes.Equal(d[:4], []byte{0xff, 0xff, 0xff, 0xff}) || d[4] != 'I' {
		return ServerStatus{}, errors.New("not an A2S_INFO reply")
	}
	p := 6
	var fields [4]string
	for i := range fields {
		var ok bool
		if fields[i], p, ok = cstring(d, p); !ok {
			return ServerStatus{}, errors.New("short A2S_INFO reply")
		}
	}
	p += 2
	if len(d) < p+2 {
		return ServerStatus{}, errors.New("short A2S_INFO reply")
	}
	st := ServerStatus{Online: true, Name: fields[0], Players: int(d[p]), MaxPlayers: int(d[p+1]), Ping: rtt, HasPlayers: true}
	p += 5
	if p >= len(d) {
		return st, nil
	}
	st.Password = d[p] == 1
	p += 2
	_, p, ok := cstring(d, p)
	if !ok || p >= len(d) {
		return st, nil
	}
	edf := d[p]
	p++
	if edf&0x80 != 0 {
		p += 2
	}
	if edf&0x10 != 0 {
		p += 8
	}
	if edf&0x40 != 0 {
		p += 2
		if _, p, ok = cstring(d, p); !ok {
			return st, nil
		}
	}
	if edf&0x20 != 0 {
		if kw, _, ok := cstring(d, p); ok {
			st.Keywords = kw
		}
	}
	return st, nil
}
