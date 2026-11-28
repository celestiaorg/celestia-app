package main

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const appHash = "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"

// writeKeys writes an ed25519 key pair in the PEM formats openssl genpkey / pkey -pubout write.
func writeKeys(t *testing.T, dir string) (key, pub string) {
	t.Helper()
	pk, sk, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	skDER, err := x509.MarshalPKCS8PrivateKey(sk)
	require.NoError(t, err)
	pkDER, err := x509.MarshalPKIXPublicKey(pk)
	require.NoError(t, err)
	key, pub = filepath.Join(dir, "bundle.key"), filepath.Join(dir, "bundle.pub")
	require.NoError(t, os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: skDER}), 0o600))
	require.NoError(t, os.WriteFile(pub, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkDER}), 0o644))
	return key, pub
}

func TestKeysAndSignature(t *testing.T) {
	key, pub := writeKeys(t, t.TempDir())
	priv, err := LoadPrivateKey(key)
	require.NoError(t, err)
	pk, err := LoadPublicKey(pub)
	require.NoError(t, err)
	sig, err := DecodeSignature(EncodeSignature(ed25519.Sign(priv, []byte("m"))))
	require.NoError(t, err)
	require.True(t, ed25519.Verify(pk, []byte("m"), sig))
	_, err = DecodeSignature([]byte("not base64!"))
	require.Error(t, err)
	_, err = LoadPublicKey(key)
	require.Error(t, err, "a private key is not a public key")
}

func TestReadRanges(t *testing.T) {
	p := filepath.Join(t.TempDir(), "r.tsv")
	require.NoError(t, os.WriteFile(p, []byte("# name first last\ncs-000000000001.bin.zst\t1\t10\n"), 0o644))
	r, err := ReadRanges(p)
	require.NoError(t, err)
	require.Equal(t, [2]int64{1, 10}, r["cs-000000000001.bin.zst"])
	require.NoError(t, os.WriteFile(p, []byte("cs-1.bin 1\n"), 0o644))
	_, err = ReadRanges(p)
	require.Error(t, err)
}

// TestCLI runs build (app hash from expected.tsv), sign and verify, then flips one byte of a bundle file.
func TestCLI(t *testing.T) {
	d := t.TempDir()
	b := filepath.Join(d, "bundle")
	require.NoError(t, os.Mkdir(b, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(b, "cs-000000000001.bin.zst"), []byte(strings.Repeat("x", 4096)), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(b, "cs-000000000101.bin"), []byte("tail"), 0o644))
	h32 := strings.Repeat("AB", 32)
	exp := filepath.Join(d, "expected.tsv")
	require.NoError(t, os.WriteFile(exp, []byte("# fshdr verify chain=test-1 anchor=151\n150\t"+appHash+"\t"+h32+"\t"+h32+"\t"+h32+"\t"+h32+"\t3\n"), 0o644))
	key, pub := writeKeys(t, d)
	mf := filepath.Join(d, "manifest.json")
	require.NoError(t, cmdBuild([]string{"-dir", b, "-chain-id", "test-1", "-last-height", "150", "-expected", exp, "-out", mf}))
	data, err := os.ReadFile(mf)
	require.NoError(t, err)
	require.Contains(t, string(data), appHash)
	require.NoError(t, cmdSign([]string{"-manifest", mf, "-key", key}))
	verify := []string{"-dir", b, "-manifest", mf, "-pub", pub, "-chain-id", "test-1"}
	require.NoError(t, cmdVerify(verify))

	require.Error(t, cmdBuild([]string{"-dir", b, "-chain-id", "test-1", "-last-height", "150", "-out", mf}), "needs -app-hash or -expected")
	require.Error(t, cmdBuild([]string{"-dir", b, "-chain-id", "test-1", "-last-height", "149", "-expected", exp, "-out", mf}), "no row 149")
	require.ErrorContains(t, cmdVerify([]string{"-dir", b, "-manifest", mf, "-pub", pub, "-chain-id", "other"}), "expected \"other\"")

	p := filepath.Join(b, "cs-000000000001.bin.zst")
	data, err = os.ReadFile(p)
	require.NoError(t, err)
	data[100] ^= 1
	require.NoError(t, os.WriteFile(p, data, 0o644))
	require.ErrorContains(t, cmdVerify(verify), "BUNDLE NOT VERIFIED")
}
