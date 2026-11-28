package main

// Keys and signatures: PEM PKCS#8 / PKIX ed25519 keys as openssl writes them
// (`openssl genpkey -algorithm ed25519`, `openssl pkey -pubout`) and detached base64 signatures
// over the exact manifest bytes.

import (
	"bufio"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// maxManifestBytes bounds how much of a manifest file is read.
const maxManifestBytes = 64 << 20

// readLimited reads at most limit bytes of p and fails if the file is larger.
func readLimited(p string, limit int64) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: larger than %d bytes", p, limit)
	}
	return b, nil
}

func readPEM(p, wantType string) ([]byte, error) {
	b, err := readLimited(p, 1<<16)
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
