package play

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type cpEntry struct {
	tag  byte
	str  string
	num  int64
	a, b uint16
}

type classMethod struct {
	name, desc string
	code       []byte
}

type classField struct {
	name, desc string
	constIdx   uint16
}

type classFile struct {
	cp      []cpEntry
	fields  []classField
	methods []classMethod
}

type byteReader struct {
	d   []byte
	p   int
	err error
}

func (r *byteReader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.p+n > len(r.d) {
		r.err = errors.New("truncated class file")
		return nil
	}
	b := r.d[r.p : r.p+n]
	r.p += n
	return b
}

func (r *byteReader) u1() byte {
	b := r.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (r *byteReader) u2() uint16 {
	b := r.take(2)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint16(b)
}

func (r *byteReader) u4() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func parseClass(d []byte) (*classFile, error) {
	r := &byteReader{d: d}
	if r.u4() != 0xCAFEBABE {
		return nil, errors.New("not a class file")
	}
	r.take(4)
	n := int(r.u2())
	c := &classFile{cp: make([]cpEntry, n)}
	for i := 1; i < n && r.err == nil; i++ {
		t := r.u1()
		e := cpEntry{tag: t}
		switch t {
		case 1:
			e.str = decodeModifiedUTF8(r.take(int(r.u2())))
		case 3:
			e.num = int64(int32(r.u4()))
		case 4:
			r.take(4)
		case 5, 6:
			e.num = int64(uint64(r.u4())<<32 | uint64(r.u4()))
			c.cp[i] = e
			i++
			continue
		case 7, 8, 16, 19, 20:
			e.a = r.u2()
		case 9, 10, 11, 12, 17, 18:
			e.a, e.b = r.u2(), r.u2()
		case 15:
			r.u1()
			e.a = r.u2()
		default:
			return nil, fmt.Errorf("unknown constant pool tag %d", t)
		}
		c.cp[i] = e
	}
	r.take(6)
	r.take(2 * int(r.u2()))
	attrs := func() map[string][]byte {
		m := map[string][]byte{}
		for k := int(r.u2()); k > 0 && r.err == nil; k-- {
			name := c.utf8(r.u2())
			m[name] = r.take(int(r.u4()))
		}
		return m
	}
	for k := int(r.u2()); k > 0 && r.err == nil; k-- {
		r.u2()
		f := classField{name: c.utf8(r.u2()), desc: c.utf8(r.u2())}
		if cv := attrs()["ConstantValue"]; len(cv) == 2 {
			f.constIdx = binary.BigEndian.Uint16(cv)
		}
		c.fields = append(c.fields, f)
	}
	for k := int(r.u2()); k > 0 && r.err == nil; k-- {
		r.u2()
		m := classMethod{name: c.utf8(r.u2()), desc: c.utf8(r.u2())}
		if code := attrs()["Code"]; len(code) >= 8 {
			l := int(binary.BigEndian.Uint32(code[4:8]))
			if 8+l <= len(code) {
				m.code = code[8 : 8+l]
			}
		}
		c.methods = append(c.methods, m)
	}
	if r.err != nil {
		return nil, r.err
	}
	return c, nil
}

func decodeModifiedUTF8(b []byte) string {
	out := make([]rune, 0, len(b))
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c < 0x80:
			out = append(out, rune(c))
			i++
		case c&0xE0 == 0xC0 && i+1 < len(b):
			out = append(out, rune(c&0x1F)<<6|rune(b[i+1]&0x3F))
			i += 2
		case c&0xF0 == 0xE0 && i+2 < len(b):
			out = append(out, rune(c&0x0F)<<12|rune(b[i+1]&0x3F)<<6|rune(b[i+2]&0x3F))
			i += 3
		default:
			out = append(out, 0xFFFD)
			i++
		}
	}
	return string(out)
}

func (c *classFile) entry(i uint16) cpEntry {
	if int(i) >= len(c.cp) {
		return cpEntry{}
	}
	return c.cp[i]
}

func (c *classFile) utf8(i uint16) string {
	e := c.entry(i)
	if e.tag != 1 {
		return ""
	}
	return e.str
}

func (c *classFile) memberRef(i uint16) (owner, name, desc string) {
	e := c.entry(i)
	if e.tag != 9 && e.tag != 10 && e.tag != 11 {
		return "", "", ""
	}
	owner = c.utf8(c.entry(e.a).a)
	nt := c.entry(e.b)
	return owner, c.utf8(nt.a), c.utf8(nt.b)
}

func (c *classFile) method(name string) *classMethod {
	for i := range c.methods {
		if c.methods[i].name == name {
			return &c.methods[i]
		}
	}
	return nil
}

func (c *classFile) intField(name string) (int, bool) {
	for _, f := range c.fields {
		if f.name == name && f.constIdx != 0 {
			if e := c.entry(f.constIdx); e.tag == 3 {
				return int(e.num), true
			}
		}
	}
	return 0, false
}

var fixedOpLen = func() [256]int8 {
	var t [256]int8
	for i := range t {
		t[i] = 1
	}
	set := func(n int8, ops ...int) {
		for _, o := range ops {
			t[o] = n
		}
	}
	set(2, 0x10, 0x12, 0x15, 0x16, 0x17, 0x18, 0x19, 0x36, 0x37, 0x38, 0x39, 0x3a, 0xa9, 0xbc)
	set(3, 0x11, 0x13, 0x14, 0x84, 0xb2, 0xb3, 0xb4, 0xb5, 0xb6, 0xb7, 0xb8, 0xbb, 0xbd, 0xc0, 0xc1, 0xc6, 0xc7)
	for o := 0x99; o <= 0xa8; o++ {
		t[o] = 3
	}
	set(4, 0xc5)
	set(5, 0xb9, 0xba, 0xc8, 0xc9)
	set(-1, 0xaa, 0xab, 0xc4)
	return t
}()

func instrLen(code []byte, pc int) int {
	op := code[pc]
	if n := fixedOpLen[op]; n > 0 {
		return int(n)
	}
	switch op {
	case 0xc4:
		if pc+1 < len(code) && code[pc+1] == 0x84 {
			return 6
		}
		return 4
	case 0xaa, 0xab:
		p := (pc + 4) &^ 3
		if p+12 > len(code) {
			return len(code) - pc
		}
		if op == 0xaa {
			lo := int32(binary.BigEndian.Uint32(code[p+4:]))
			hi := int32(binary.BigEndian.Uint32(code[p+8:]))
			return p + 12 + 4*int(hi-lo+1) - pc
		}
		np := int(int32(binary.BigEndian.Uint32(code[p+4:])))
		return p + 8 + 8*np - pc
	}
	return 1
}

type constVal struct {
	isInt bool
	i     int
	s     string
}

func (c *classFile) pushedConst(code []byte, pc int) (constVal, bool) {
	op := code[pc]
	switch {
	case op >= 0x02 && op <= 0x08:
		return constVal{isInt: true, i: int(op) - 3}, true
	case op == 0x10 && pc+1 < len(code):
		return constVal{isInt: true, i: int(int8(code[pc+1]))}, true
	case op == 0x11 && pc+2 < len(code):
		return constVal{isInt: true, i: int(int16(binary.BigEndian.Uint16(code[pc+1:])))}, true
	case (op == 0x12 && pc+1 < len(code)) || (op == 0x13 && pc+2 < len(code)):
		idx := uint16(code[pc+1])
		if op == 0x13 {
			idx = binary.BigEndian.Uint16(code[pc+1:])
		}
		e := c.entry(idx)
		switch e.tag {
		case 3:
			return constVal{isInt: true, i: int(e.num)}, true
		case 8:
			return constVal{s: c.utf8(e.a)}, true
		}
	}
	return constVal{}, false
}

func (c *classFile) walk(code []byte, fn func(pc int, op byte) bool) {
	for pc := 0; pc < len(code); {
		if !fn(pc, code[pc]) {
			return
		}
		n := instrLen(code, pc)
		if n <= 0 {
			return
		}
		pc += n
	}
}

func worldVersionFromClass(c *classFile) (int, error) {
	if m := c.method("getWorldVersion"); m != nil && len(m.code) > 0 {
		if v, ok := c.pushedConst(m.code, 0); ok && v.isInt {
			if n := instrLen(m.code, 0); n < len(m.code) && m.code[n] == 0xac {
				return v.i, nil
			}
		}
	}
	if v, ok := c.intField("WorldVersion"); ok {
		return v, nil
	}
	return 0, errors.New("IsoWorld.getWorldVersion not found")
}

func gameVersionFromCore(c *classFile) (GameVersion, error) {
	m := c.method("<clinit>")
	if m == nil {
		return GameVersion{}, errors.New("Core.<clinit> not found")
	}
	var consts []constVal
	var found *GameVersion
	pending := false
	c.walk(m.code, func(pc int, op byte) bool {
		if v, ok := c.pushedConst(m.code, pc); ok {
			consts = append(consts, v)
			pending = false
			return true
		}
		if pc+2 >= len(m.code) {
			return true
		}
		idx := binary.BigEndian.Uint16(m.code[pc+1:])
		switch op {
		case 0xb7:
			owner, name, desc := c.memberRef(idx)
			pending = false
			if owner == "zombie/core/GameVersion" && name == "<init>" && desc == "(IILjava/lang/String;)V" && len(consts) >= 3 {
				a, b, s := consts[len(consts)-3], consts[len(consts)-2], consts[len(consts)-1]
				if a.isInt && b.isInt && !s.isInt {
					pending = true
					gv := GameVersion{Major: a.i, Minor: b.i, Suffix: s.s}
					found = &gv
				}
			}
		case 0xb3:
			owner, name, _ := c.memberRef(idx)
			if pending && owner == "zombie/core/Core" && name == "gameVersion" {
				return false
			}
			pending = false
			found = nil
		}
		return true
	})
	if found == nil || !pending {
		return GameVersion{}, errors.New("Core.gameVersion not found")
	}
	return *found, nil
}

type GameInfo struct {
	WorldVersion int
	Version      GameVersion
}

type gameInfoKey struct {
	path string
	size int64
	mod  time.Time
}

var (
	gameInfoMu    sync.Mutex
	gameInfoCache = map[gameInfoKey]GameInfo{}
)

func readZipEntry(zr *zip.Reader, name string) ([]byte, error) {
	f, err := zr.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, math.MaxInt32))
}

func ReadGameInfo(gameDir string) (GameInfo, error) {
	jar := filepath.Join(gameDir, "projectzomboid.jar")
	st, err := os.Stat(jar)
	if err != nil {
		return GameInfo{}, err
	}
	key := gameInfoKey{jar, st.Size(), st.ModTime()}
	gameInfoMu.Lock()
	gi, ok := gameInfoCache[key]
	gameInfoMu.Unlock()
	if ok {
		return gi, nil
	}
	gi, err = readGameInfoJar(jar)
	if err != nil {
		return GameInfo{}, err
	}
	gameInfoMu.Lock()
	gameInfoCache[key] = gi
	gameInfoMu.Unlock()
	return gi, nil
}

func readGameInfoJar(jar string) (GameInfo, error) {
	zr, err := zip.OpenReader(jar)
	if err != nil {
		return GameInfo{}, err
	}
	defer zr.Close()
	var gi GameInfo
	d, err := readZipEntry(&zr.Reader, "zombie/iso/IsoWorld.class")
	if err != nil {
		return GameInfo{}, err
	}
	c, err := parseClass(d)
	if err != nil {
		return GameInfo{}, fmt.Errorf("IsoWorld.class: %w", err)
	}
	if gi.WorldVersion, err = worldVersionFromClass(c); err != nil {
		return GameInfo{}, err
	}
	if d, err = readZipEntry(&zr.Reader, "zombie/core/Core.class"); err != nil {
		return GameInfo{}, err
	}
	if c, err = parseClass(d); err != nil {
		return GameInfo{}, fmt.Errorf("Core.class: %w", err)
	}
	if gi.Version, err = gameVersionFromCore(c); err != nil {
		return GameInfo{}, err
	}
	return gi, nil
}
