package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/katachinosekai/pzlauncher/internal/update"
)

func TestVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	pk := base64.StdEncoding.EncodeToString(pub)
	data := []byte(`{"launcher_version":"9.9.9"}`)
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)) + "\n")
	if !verify(pk, data, sig) {
		t.Fatal("valid signature rejected")
	}
	if verify(pk, []byte(`{"launcher_version":"9.9.8"}`), sig) {
		t.Fatal("tampered file accepted")
	}
	if verify(update.ReleasePublicKey, data, sig) {
		t.Fatal("signature from another key accepted")
	}
	if verify(pk, data, []byte("not base64")) {
		t.Fatal("garbage signature accepted")
	}
}
