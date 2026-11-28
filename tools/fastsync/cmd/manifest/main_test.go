package main

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const appHash = "0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF"

func TestKeysAndSignature(t *testing.T) {
	d := t.TempDir()
	key, pub := filepath.Join(d, "k.pem"), filepath.Join(d, "k.pub")
	require.NoError(t, GenerateKey(key, pub))
	require.Error(t, GenerateKey(key, pub), "keygen must not overwrite a key")
	st, err := os.Stat(key)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), st.Mode().Perm())
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

func TestReadRangesAndLookupExpected(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "r.tsv")
	require.NoError(t, os.WriteFile(p, []byte("# name first last\ncs-000000000001.bin.zst\t1\t10\n"), 0o644))
	r, err := ReadRanges(p)
	require.NoError(t, err)
	require.Equal(t, [2]int64{1, 10}, r["cs-000000000001.bin.zst"])
	require.NoError(t, os.WriteFile(p, []byte("cs-1.bin 1\n"), 0o644))
	_, err = ReadRanges(p)
	require.Error(t, err)

	e := filepath.Join(d, "expected.tsv")
	require.NoError(t, os.WriteFile(e, []byte("9\tAA\n10\t"+appHash+"\tFF\n"), 0o644))
	h, err := LookupExpected(e, 10)
	require.NoError(t, err)
	require.Equal(t, appHash, h)
	_, err = LookupExpected(e, 11)
	require.Error(t, err)
}

// TestCLI runs keygen, build, sign and verify, then flips one byte of a bundle file.
func TestCLI(t *testing.T) {
	d := t.TempDir()
	b := filepath.Join(d, "bundle")
	require.NoError(t, os.Mkdir(b, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(b, "cs-000000000001.bin.zst"), []byte(strings.Repeat("x", 4096)), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(b, "cs-000000000101.bin"), []byte("tail"), 0o644))
	key, pub, mf := filepath.Join(d, "k.pem"), filepath.Join(d, "k.pub"), filepath.Join(d, "manifest.json")
	require.NoError(t, cmdKeygen([]string{"-key", key, "-pub", pub}))
	require.NoError(t, cmdBuild([]string{"-dir", b, "-chain-id", "test-1", "-last-height", "150", "-app-hash", appHash, "-out", mf}))
	require.NoError(t, cmdSign([]string{"-manifest", mf, "-key", key}))
	verify := []string{"-dir", b, "-manifest", mf, "-pub", pub, "-chain-id", "test-1"}
	require.NoError(t, cmdVerify(verify))

	require.Error(t, cmdBuild([]string{"-dir", b, "-chain-id", "test-1", "-last-height", "150", "-out", mf}), "needs -app-hash or -expected")
	require.ErrorContains(t, cmdVerify([]string{"-dir", b, "-manifest", mf, "-pub", pub, "-chain-id", "other"}), "expected \"other\"")

	p := filepath.Join(b, "cs-000000000001.bin.zst")
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	data[100] ^= 1
	require.NoError(t, os.WriteFile(p, data, 0o644))
	require.ErrorContains(t, cmdVerify(verify), "BUNDLE NOT VERIFIED")
}
