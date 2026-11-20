//go:build !windows

package spotify

import (
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	dest    = "org.mpris.MediaPlayer2.spotify"
	objPath = "/org/mpris/MediaPlayer2"
	iface   = "org.mpris.MediaPlayer2.Player"
)

func conn() (*dbus.Conn, error) { return dbus.SessionBus() }

func Get() Status {
	c, err := conn()
	if err != nil {
		return Status{}
	}
	o := c.Object(dest, objPath)
	st, err := o.GetProperty(iface + ".PlaybackStatus")
	if err != nil {
		return Status{}
	}
	s := Status{Running: true, Playing: fmt.Sprint(st.Value()) == "Playing"}
	md, err := o.GetProperty(iface + ".Metadata")
	if err != nil {
		return s
	}
	m, ok := md.Value().(map[string]dbus.Variant)
	if !ok {
		return s
	}
	s.Title, s.Artist = metaStrings(m)
	return s
}

func metaStrings(m map[string]dbus.Variant) (title, artist string) {
	if v, ok := m["xesam:title"]; ok {
		title, _ = v.Value().(string)
	}
	if v, ok := m["xesam:artist"]; ok {
		switch a := v.Value().(type) {
		case []string:
			artist = strings.Join(a, ", ")
		case string:
			artist = a
		}
	}
	return title, artist
}

func call(method string) error {
	c, err := conn()
	if err != nil {
		return err
	}
	return c.Object(dest, objPath).Call(iface+"."+method, 0).Err
}

func PlayPause() error { return call("PlayPause") }
func Next() error      { return call("Next") }
func Prev() error      { return call("Previous") }
