package manifest

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testAppHash = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
}

// newBundle creates a 3-file bundle covering heights 1..25 (files start at 1, 11, 21).
func newBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "cs-000000000001.bin.zst", strings.Repeat("a", 3000))
	writeFile(t, dir, "cs-000000000011.bin.zst", strings.Repeat("b", 2000))
	writeFile(t, dir, "cs-000000000021.bin", "tail file, not yet compressed")
	writeFile(t, dir, "expected.tsv", "25\t"+testAppHash+"\tFF\n")
	return dir
}

func buildOpts() BuildOptions {
	return BuildOptions{ChainID: "test-1", LastHeight: 25, LastAppHash: testAppHash, Extra: []string{"expected.tsv"}, Workers: 2, Now: time.Unix(0, 0)}
}

type signed struct {
	m    *Manifest
	b    []byte
	sig  []byte
	pub  ed25519.PublicKey
	priv ed25519.PrivateKey
}

func buildAndSign(t *testing.T, dir string) signed {
	t.Helper()
	m, err := Build(dir, buildOpts())
	require.NoError(t, err)
	b, err := m.Marshal()
	require.NoError(t, err)
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return signed{m, b, ed25519.Sign(priv, b), pub, priv}
}

func TestBuildManifest(t *testing.T) {
	dir := newBundle(t)
	s := buildAndSign(t, dir)
	m := s.m
	require.Equal(t, "test-1", m.ChainID)
	require.Equal(t, int64(1), m.FirstHeight)
	require.Equal(t, int64(25), m.LastHeight)
	require.Equal(t, strings.ToUpper(testAppHash), m.LastAppHash)
	require.Len(t, m.Files, 4)
	want := [][3]any{
		{"cs-000000000001.bin.zst", int64(1), int64(10)},
		{"cs-000000000011.bin.zst", int64(11), int64(20)},
		{"cs-000000000021.bin", int64(21), int64(25)},
		{"expected.tsv", int64(0), int64(0)},
	}
	for i, w := range want {
		require.Equal(t, w[0], m.Files[i].Name)
		require.Equal(t, w[1], m.Files[i].FirstHeight)
		require.Equal(t, w[2], m.Files[i].LastHeight)
		require.Len(t, m.Files[i].SHA256, 64)
	}
	require.Equal(t, int64(3000+2000+len("tail file, not yet compressed")+len("25\t"+testAppHash+"\tFF\n")), m.TotalBytes)
	// round trip
	m2, err := Parse(s.b)
	require.NoError(t, err)
	require.Equal(t, m, m2)
}

func TestVerifyOK(t *testing.T) {
	dir := newBundle(t)
	s := buildAndSign(t, dir)
	m, err := Verify(dir, s.b, s.sig, s.pub, VerifyOptions{ChainID: "test-1", Workers: 3})
	require.NoError(t, err)
	require.Equal(t, int64(25), m.LastHeight)
}

func TestVerifyTamperedByte(t *testing.T) {
	dir := newBundle(t)
	s := buildAndSign(t, dir)
	p := filepath.Join(dir, "cs-000000000011.bin.zst")
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	b[1234] ^= 0x01 // one flipped bit, same size
	require.NoError(t, os.WriteFile(p, b, 0o644))
	_, err = Verify(dir, s.b, s.sig, s.pub, VerifyOptions{})
	require.ErrorContains(t, err, "cs-000000000011.bin.zst: sha256")
}

func TestVerifyTruncated(t *testing.T) {
	dir := newBundle(t)
	s := buildAndSign(t, dir)
	require.NoError(t, os.Truncate(filepath.Join(dir, "cs-000000000001.bin.zst"), 2999))
	_, err := Verify(dir, s.b, s.sig, s.pub, VerifyOptions{})
	require.ErrorContains(t, err, "size 2999")
}

func TestVerifySignatureFailures(t *testing.T) {
	dir := newBundle(t)
	s := buildAndSign(t, dir)

	// manifest modified after signing (e.g. a sha256 swapped to match a tampered file)
	mod := []byte(strings.Replace(string(s.b), `"last_height": 25`, `"last_height": 26`, 1))
	require.NotEqual(t, s.b, mod)
	_, err := Verify(dir, mod, s.sig, s.pub, VerifyOptions{})
	require.ErrorContains(t, err, "SIGNATURE INVALID")

	// signature flipped
	bad := append([]byte(nil), s.sig...)
	bad[0] ^= 0x80
	_, err = Verify(dir, s.b, bad, s.pub, VerifyOptions{})
	require.ErrorContains(t, err, "SIGNATURE INVALID")

	// signed by another key
	otherPub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	_, err = Verify(dir, s.b, s.sig, otherPub, VerifyOptions{})
	require.ErrorContains(t, err, "SIGNATURE INVALID")
}

func TestVerifyChainID(t *testing.T) {
	dir := newBundle(t)
	s := buildAndSign(t, dir)
	_, err := Verify(dir, s.b, s.sig, s.pub, VerifyOptions{ChainID: "celestia"})
	require.ErrorContains(t, err, "expected \"celestia\"")
}

func TestVerifyUnexpectedAndMissingFiles(t *testing.T) {
	dir := newBundle(t)
	s := buildAndSign(t, dir)

	writeFile(t, dir, "cs-000000000031.bin", "planted")
	_, err := Verify(dir, s.b, s.sig, s.pub, VerifyOptions{})
	require.ErrorContains(t, err, "unexpected file")
	_, err = Verify(dir, s.b, s.sig, s.pub, VerifyOptions{AllowExtra: true})
	require.NoError(t, err)
	_, err = Verify(dir, s.b, s.sig, s.pub, VerifyOptions{Ignore: []string{"cs-000000000031.bin"}})
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(dir, "cs-000000000031.bin")))

	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	_, err = Verify(dir, s.b, s.sig, s.pub, VerifyOptions{})
	require.ErrorContains(t, err, "unexpected file")
	require.NoError(t, os.Remove(filepath.Join(dir, "sub")))

	require.NoError(t, os.Remove(filepath.Join(dir, "expected.tsv")))
	_, err = Verify(dir, s.b, s.sig, s.pub, VerifyOptions{})
	require.ErrorContains(t, err, "missing")
}

func TestVerifyRefusesSymlink(t *testing.T) {
	dir := newBundle(t)
	s := buildAndSign(t, dir)
	outside := filepath.Join(t.TempDir(), "x")
	require.NoError(t, os.Rename(filepath.Join(dir, "expected.tsv"), outside))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "expected.tsv")))
	_, err := Verify(dir, s.b, s.sig, s.pub, VerifyOptions{})
	require.ErrorContains(t, err, "not a regular file")
}

func TestVerifyRefusesPathTraversal(t *testing.T) {
	// A validly signed manifest that names a file outside the directory must still be refused:
	// the signer's key may be compromised or the signer careless.
	dir := newBundle(t)
	s := buildAndSign(t, dir)
	for _, name := range []string{"../secret", "/etc/passwd", "a/b", "..", ".hidden", "x..y"} {
		m := *s.m
		m.Files = append([]File(nil), s.m.Files...)
		m.Files[3].Name = name
		b, err := m.Marshal()
		require.NoError(t, err)
		_, err = Verify(dir, b, ed25519.Sign(s.priv, b), s.pub, VerifyOptions{})
		require.ErrorContains(t, err, "unsafe file name", name)
	}
}

func TestParseRejects(t *testing.T) {
	dir := newBundle(t)
	s := buildAndSign(t, dir)
	cases := map[string]string{
		"unknown field":  strings.Replace(string(s.b), `"version": 1,`, `"version": 1, "x": 1,`, 1),
		"version":        strings.Replace(string(s.b), `"version": 1,`, `"version": 2,`, 1),
		"total":          strings.Replace(string(s.b), `"total_bytes": `, `"total_bytes": 1`, 1),
		"trailing":       string(s.b) + "{}",
		"bad app hash":   strings.Replace(string(s.b), strings.ToUpper(testAppHash), "ABCD", 1),
		"negative size":  strings.Replace(string(s.b), `"size": 3000`, `"size": -3000`, 1),
		"duplicate file": strings.Replace(string(s.b), `"cs-000000000011.bin.zst"`, `"cs-000000000001.bin.zst"`, 1),
	}
	for name, c := range cases {
		_, err := Parse([]byte(c))
		require.Error(t, err, name)
	}
}

func TestBuildRejects(t *testing.T) {
	t.Run("tmp leftover", func(t *testing.T) {
		dir := newBundle(t)
		writeFile(t, dir, "cs-000000000001.bin.zst.tmp", "partial")
		_, err := Build(dir, buildOpts())
		require.ErrorContains(t, err, "unexpected file")
	})
	t.Run("bin and zst of the same file", func(t *testing.T) {
		dir := newBundle(t)
		writeFile(t, dir, "cs-000000000011.bin", "dup")
		_, err := Build(dir, buildOpts())
		require.ErrorContains(t, err, "same changeset file")
	})
	t.Run("gap", func(t *testing.T) {
		dir := newBundle(t)
		require.NoError(t, os.Remove(filepath.Join(dir, "cs-000000000001.bin.zst")))
		writeFile(t, dir, "cs-000000000005.bin.zst", "x")
		o := buildOpts()
		o.Ranges = map[string][2]int64{"cs-000000000005.bin.zst": {5, 8}}
		_, err := Build(dir, o)
		require.ErrorContains(t, err, "gap: heights 9..10")
	})
	t.Run("restart overlap needs ranges", func(t *testing.T) {
		dir := newBundle(t)
		writeFile(t, dir, "cs-000000000011-1.bin.zst", "re-recorded")
		_, err := Build(dir, buildOpts())
		require.ErrorContains(t, err, "-ranges")
		o := buildOpts()
		o.Ranges = map[string][2]int64{"cs-000000000011.bin.zst": {11, 15}, "cs-000000000011-1.bin.zst": {11, 20}}
		m, err := Build(dir, o)
		require.NoError(t, err)
		require.Len(t, m.Files, 5)
	})
	t.Run("last height beyond files", func(t *testing.T) {
		dir := newBundle(t)
		o := buildOpts()
		o.Ranges = map[string][2]int64{"cs-000000000021.bin": {21, 24}}
		_, err := Build(dir, o)
		require.ErrorContains(t, err, "last height is 25")
	})
	t.Run("subdirectory", func(t *testing.T) {
		dir := newBundle(t)
		require.NoError(t, os.Mkdir(filepath.Join(dir, "frontier"), 0o755))
		_, err := Build(dir, buildOpts())
		require.ErrorContains(t, err, "not a regular file")
	})
	t.Run("bad app hash", func(t *testing.T) {
		o := buildOpts()
		o.LastAppHash = "zz"
		_, err := Build(newBundle(t), o)
		require.Error(t, err)
	})
}
