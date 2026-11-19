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
		return st
	}
	if d, err := Ping(ip, port+1, timeout); err == nil {
		return ServerStatus{Online: true, Ping: d}
	}
	return ServerStatus{}
}

func queryA2S(ip string, port int, timeout time.Duration) (ServerStatus, error) {
	conn, err := net.DialTimeout("udp", net.JoinHostPort(ip, strconv.Itoa(port)), timeout)
	if err != nil {
		return ServerStatus{}, err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return ServerStatus{}, err
	}
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

func parseA2SInfo(d []byte, rtt time.Duration) (ServerStatus, error) {
	if len(d) < 6 || !bytes.Equal(d[:4], []byte{0xff, 0xff, 0xff, 0xff}) || d[4] != 'I' {
		return ServerStatus{}, errors.New("not an A2S_INFO reply")
	}
	p := 6
	var fields [4]string
	for i := range fields {
		e := bytes.IndexByte(d[p:], 0)
		if e < 0 {
			return ServerStatus{}, errors.New("short A2S_INFO reply")
		}
		fields[i] = string(d[p : p+e])
		p += e + 1
	}
	p += 2
	if len(d) < p+2 {
		return ServerStatus{}, errors.New("short A2S_INFO reply")
	}
	return ServerStatus{Online: true, Name: fields[0], Players: int(d[p]), MaxPlayers: int(d[p+1]), Ping: rtt, HasPlayers: true}, nil
}
