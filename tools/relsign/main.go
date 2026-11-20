package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: relsign keygen <private-key-file> | relsign sign <private-key-file> <file>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "keygen":
		if _, err := os.Stat(os.Args[2]); err == nil {
			fail(fmt.Errorf("%s already exists", os.Args[2]))
		}
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fail(err)
		}
		if err := os.WriteFile(os.Args[2], []byte(base64.StdEncoding.EncodeToString(priv.Seed())+"\n"), 0o600); err != nil {
			fail(err)
		}
		fmt.Println(base64.StdEncoding.EncodeToString(pub))
	case "sign":
		if len(os.Args) != 4 {
			fail(fmt.Errorf("sign needs <private-key-file> <file>"))
		}
		k, err := os.ReadFile(os.Args[2])
		if err != nil {
			fail(err)
		}
		seed, err := base64.StdEncoding.DecodeString(string(trim(k)))
		if err != nil || len(seed) != ed25519.SeedSize {
			fail(fmt.Errorf("bad private key file"))
		}
		data, err := os.ReadFile(os.Args[3])
		if err != nil {
			fail(err)
		}
		sig := ed25519.Sign(ed25519.NewKeyFromSeed(seed), data)
		if err := os.WriteFile(os.Args[3]+".sig", []byte(base64.StdEncoding.EncodeToString(sig)+"\n"), 0o644); err != nil {
			fail(err)
		}
	default:
		fail(fmt.Errorf("unknown command %q", os.Args[1]))
	}
}

func trim(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "relsign:", err)
	os.Exit(1)
}
