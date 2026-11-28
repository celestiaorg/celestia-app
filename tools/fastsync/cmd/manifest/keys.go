package main

// Keys and signatures: PEM PKCS#8 / PKIX ed25519 keys (the format of `openssl genpkey -algorithm ed25519`)
// and detached base64 signatures over the exact manifest bytes.

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/manifest"
)

// GenerateKey writes a new ed25519 private key (0600) and its public key as PEM files.
func GenerateKey(privPath, pubPath string) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return err
	}
	if err := writeNew(privPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}), 0o600); err != nil {
		return err
	}
	return writeNew(pubPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}), 0o644)
}

func writeNew(p string, b []byte, mode os.FileMode) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func readPEM(p, wantType string) ([]byte, error) {
	b, err := manifest.ReadLimited(p, 1<<16)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != wantType {
		return nil, fmt.Errorf("%s: no %q PEM block", p, wantType)
	}
	return blk.Bytes, nil
}

// LoadPrivateKey reads a PKCS#8 PEM ed25519 private key.
func LoadPrivateKey(p string) (ed25519.PrivateKey, error) {
	der, err := readPEM(p, "PRIVATE KEY")
	if err != nil {
		return nil, err
	}
	k, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, err
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s: not an ed25519 key", p)
	}
	return priv, nil
}

// LoadPublicKey reads a PKIX PEM ed25519 public key.
func LoadPublicKey(p string) (ed25519.PublicKey, error) {
	der, err := readPEM(p, "PUBLIC KEY")
	if err != nil {
		return nil, err
	}
	k, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	pub, ok := k.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%s: not an ed25519 key", p)
	}
	return pub, nil
}

// EncodeSignature returns the detached signature file contents (base64 + newline).
func EncodeSignature(sig []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(sig) + "\n")
}

// DecodeSignature parses a detached signature file.
func DecodeSignature(b []byte) ([]byte, error) {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, errors.New("signature file: want base64 of 64 bytes")
	}
	return sig, nil
}

// ReadRanges parses a TSV of "name<TAB>first<TAB>last" lines ('#' comments allowed).
func ReadRanges(p string) (map[string][2]int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string][2]int64{}
	sc := bufio.NewScanner(f)
	for ln := 1; sc.Scan(); ln++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fs := strings.Fields(line)
		if len(fs) != 3 {
			return nil, fmt.Errorf("%s:%d: want name first last", p, ln)
		}
		a, err1 := strconv.ParseInt(fs[1], 10, 64)
		b, err2 := strconv.ParseInt(fs[2], 10, 64)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("%s:%d: bad height", p, ln)
		}
		out[fs[0]] = [2]int64{a, b}
	}
	return out, sc.Err()
}

// LookupExpected returns the app hash after height h from an expected-hash TSV
// ("height<TAB>app_hash_after[<TAB>...]", as written by fshdr verify).
func LookupExpected(p string, h int64) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	want := strconv.FormatInt(h, 10)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) >= 2 && fs[0] == want {
			return fs[1], nil
		}
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("%s: no entry for height %d", p, h)
}
