package play

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"time"
)

var raknetMagic = []byte{0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe, 0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78}

var ErrNoReply = errors.New("no reply")

func Ping(ip string, port int, timeout time.Duration) (time.Duration, error) {
	conn, err := net.DialTimeout("udp", net.JoinHostPort(ip, strconv.Itoa(port)), timeout)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	var pkt bytes.Buffer
	pkt.WriteByte(0x01)
	binary.Write(&pkt, binary.BigEndian, time.Now().UnixMilli())
	pkt.Write(raknetMagic)
	binary.Write(&pkt, binary.BigEndian, int64(0))
	deadline := time.Now().Add(timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return 0, err
	}
	buf := make([]byte, 1500)
	for attempt := 0; attempt < 3 && time.Now().Before(deadline); attempt++ {
		start := time.Now()
		if _, err := conn.Write(pkt.Bytes()); err != nil {
			return 0, err
		}
		_ = conn.SetReadDeadline(minTime(deadline, start.Add(timeout/3)))
		n, err := conn.Read(buf)
		if err == nil && n >= 1 && buf[0] == 0x1c {
			return time.Since(start), nil
		}
	}
	return 0, ErrNoReply
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
