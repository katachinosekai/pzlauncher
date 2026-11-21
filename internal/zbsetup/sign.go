package zbsetup

import (
	"bufio"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
)

type SigStatus int

const (
	Unsigned SigStatus = iota
	SignedOK
	SignedUnknownKey
	SignedBad
)

func (s SigStatus) String() string {
	switch s {
	case SignedOK:
		return "signature valid (Zed)"
	case SignedUnknownKey:
		return "signed by an unknown author key"
	case SignedBad:
		return "signature INVALID"
	}
	return "no .zbs signature"
}

var KnownKeys = map[string][]string{
	"76561198043849998": {"c0fe7daaa1fbd1ee54096d96669a7bbb10cd0a2ad08949d664d4804c50e34db1"},
}

type ZBS struct {
	SteamID   string
	Signature []byte
}

func ParseZBS(r io.Reader) (ZBS, error) {
	var z ZBS
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			first = false
			if line != "ZBS" {
				return z, errors.New("not a .zbs file")
			}
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "SteamID64":
			z.SteamID = strings.TrimSpace(v)
		case "Signature":
			b, err := hex.DecodeString(strings.TrimSpace(v))
			if err != nil {
				return z, errors.New("bad .zbs signature hex")
			}
			z.Signature = b
		}
	}
	if z.SteamID == "" || len(z.Signature) != ed25519.SignatureSize {
		return z, errors.New("incomplete .zbs file")
	}
	return z, nil
}

func Fingerprint(p string) (string, error) { return fileSHA256(p) }

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func VerifyZBS(jarPath, zbsPath string) SigStatus {
	f, err := os.Open(zbsPath)
	if err != nil {
		return Unsigned
	}
	z, err := ParseZBS(f)
	f.Close()
	if err != nil {
		return SignedBad
	}
	sum, err := fileSHA256(jarPath)
	if err != nil {
		return SignedBad
	}
	keys := KnownKeys[z.SteamID]
	if len(keys) == 0 {
		return SignedUnknownKey
	}
	msg := []byte("ZBS:" + z.SteamID + ":" + sum)
	for _, k := range keys {
		pk, err := hex.DecodeString(k)
		if err != nil || len(pk) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(pk, msg, z.Signature) {
			return SignedOK
		}
	}
	return SignedBad
}
