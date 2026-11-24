// Package scripts tests fastsync.sh and segment.sh with stub tools.
package scripts

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func hash(tag string, h int) string {
	return strings.Repeat(fmt.Sprintf("%02X", (len(tag)*31+h)%256), 32)
}

// row is an expected.tsv row for height h.
func row(h int) string {
	return fmt.Sprintf("%d\t%s\t%s\t%s\t%s\t%s\t3", h, hash("app", h), hash("hdr", h), hash("res", h), hash("val", h), hash("val", h))
}

// stub writes an executable bash script.
func stub(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte("#!/usr/bin/env bash\nset -euo pipefail\n"+body), 0o755))
	return p
}

// stubs returns env entries pointing fastsync.sh at stub tools. The applier
// writes the applier-interface lines; it reports HANDOVER past handoverAt and
// exits 2 (app hash mismatch) when asked to apply up to failAt.
func stubs(t *testing.T, handoverAt, failAt int) []string {
	bin := t.TempDir()
	rows := ""
	for h := 1; h <= 40; h++ {
		rows += row(h) + `\n`
	}
	fshdr := stub(t, bin, "fshdr", `out=""; from=0; to=0
while [ $# -gt 0 ]; do case $1 in -out) out=$2; shift;; -from) from=$2; shift;; -to) to=$2; shift;; esac; shift; done
printf '# fshdr stub\n'"`+rows+`" | awk -F'\t' -v a="$from" -v b="$to" '/^#/ || ($1>=a && $1<=b)' > "$out"
`)
	applier := func(name string, handover bool) string {
		h := "false"
		if handover {
			h = "true"
		}
		return stub(t, bin, name, fmt.Sprintf(`to=0
while [ $# -gt 0 ]; do case $1 in -to) to=$2; shift;; esac; shift; done
echo "%s apply to $to" >> "$(dirname "$0")/calls"
if [ "$to" -ge %d ]; then echo "APP HASH MISMATCH at height $to"; exit 2; fi
if %s && [ "$to" -gt %d ]; then echo "HANDOVER at %d"; exit 0; fi
echo "done: applied to height $to, verified $to, recorded hash only: 0"
`, name, failAt, h, handoverAt, handoverAt+1))
	}
	fsboot := stub(t, bin, "fsboot", `echo "fsboot $*" >> "$(dirname "$0")/calls"; echo "[fsboot 00:00:00] DONE"`)
	appd := stub(t, bin, "appd", `if [ "$1" = init ]; then home=${@: -1}; mkdir -p "$home/config" "$home/data"; fi`)
	return []string{"FSHDR=" + fshdr, "FSBOOT=" + fsboot, "APPD=" + appd,
		"APPLIER=" + applier("applier46", true), "APPLIER50=" + applier("applier50", false), "CALLS=" + filepath.Join(bin, "calls")}
}

func newHome(t *testing.T) string {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(home, "data", "application.db"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "config.toml"), []byte("db_backend = \"pebbledb\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "app.toml"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "genesis.json"), []byte(`{"genesis_time":"x","chain_id": "test-chain","initial_height":"1"}`), 0o644))
	return home
}

func runScript(t *testing.T, env []string, script string, args ...string) (string, error) {
	t.Helper()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func envValue(env []string, key string) string {
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, key+"="); ok {
			return v
		}
	}
	return ""
}

var baseArgs = []string{"--changesets", "/cs", "--rpc", "http://rpc-a,http://rpc-b", "--trust-height", "100", "--trust-hash", strings.Repeat("AB", 32), "--to", "30"}

func TestFastsyncDryRun(t *testing.T) {
	home := newHome(t)
	env := stubs(t, 1000, 1000)
	out, err := runScript(t, env, "fastsync.sh", append([]string{"--home", home, "--dry-run", "--checkpoints", "10,20,99"}, baseArgs...)...)
	require.NoError(t, err, out)
	var cmds []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "+ ") && (strings.Contains(l, " verify ") || strings.Contains(l, " apply ") || strings.Contains(l, " bootstrap ")) {
			cmds = append(cmds, l)
		}
	}
	require.Len(t, cmds, 7, out)
	require.Contains(t, cmds[0], "verify -chain-id test-chain -from 1 -to 30 -out "+home+"/fastsync/expected.tsv -rpc http://rpc-a,http://rpc-b -anchor-height 100 -trust-height 100")
	require.Contains(t, cmds[1], "apply -db "+home+"/data -backend pebbledb -changesets /cs -expected "+home+"/fastsync/expected.tsv -require-trusted -to 10 -parallel")
	require.Contains(t, cmds[2], "bootstrap -home "+home+"/fastsync/checkpoints/10 -height 10")
	require.Contains(t, cmds[3], "-to 20")
	require.Contains(t, cmds[4], "bootstrap -home "+home+"/fastsync/checkpoints/20 -height 20")
	require.Contains(t, cmds[5], "-to 30")
	require.Contains(t, cmds[6], "bootstrap -home "+home+" -height 30")
	require.Contains(t, out, "skip checkpoint 99")
	require.Contains(t, out, "DONE")
	_, err = os.Stat(filepath.Join(home, "fastsync"))
	require.True(t, os.IsNotExist(err), "dry run writes nothing")
}

func TestFastsyncRunWithHandoverAndCheckpoints(t *testing.T) {
	home := newHome(t)
	env := stubs(t, 15, 1000)
	out, err := runScript(t, env, "fastsync.sh", append([]string{"--home", home, "--checkpoints", "10,20"}, baseArgs...)...)
	require.NoError(t, err, out)
	require.Contains(t, out, "handover to the v0.50 applier")
	calls, err := os.ReadFile(envValue(env, "CALLS"))
	require.NoError(t, err)
	require.Equal(t, []string{
		"applier46 apply to 10",
		"fsboot bootstrap -home " + home + "/fastsync/checkpoints/10 -height 10 -expected " + home + "/fastsync/expected.tsv -rpc http://rpc-a,http://rpc-b",
		"applier46 apply to 20", "applier50 apply to 20",
		"fsboot bootstrap -home " + home + "/fastsync/checkpoints/20 -height 20 -expected " + home + "/fastsync/expected.tsv -rpc http://rpc-a,http://rpc-b",
		"applier50 apply to 30",
		"fsboot bootstrap -home " + home + " -height 30 -expected " + home + "/fastsync/expected.tsv -rpc http://rpc-a,http://rpc-b",
	}, strings.Split(strings.TrimSpace(string(calls)), "\n"))
	ck, err := os.ReadFile(filepath.Join(home, "fastsync", "checkpoints", "20", "CHECKPOINT"))
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("height\t20\napp_hash_after\t%s\nnext_header_hash\t%s\n", hash("app", 20), hash("hdr", 20)), string(ck))
	marker, err := os.ReadFile(filepath.Join(home, "fastsync", "applied-to"))
	require.NoError(t, err)
	require.Equal(t, "30\n", string(marker))
}

func TestFastsyncMismatchPoisonsDB(t *testing.T) {
	home := newHome(t)
	env := stubs(t, 1000, 30)
	out, err := runScript(t, env, "fastsync.sh", append([]string{"--home", home}, baseArgs...)...)
	require.Error(t, err)
	require.Contains(t, out, "APP HASH MISMATCH during apply")
	require.FileExists(t, filepath.Join(home, "fastsync", "POISONED"))
	calls, _ := os.ReadFile(envValue(env, "CALLS"))
	require.NotContains(t, string(calls), "fsboot", "nothing is bootstrapped after a mismatch")

	out, err = runScript(t, env, "fastsync.sh", append([]string{"--home", home}, baseArgs...)...)
	require.Error(t, err)
	require.Contains(t, out, "unverified height")
}

func TestFastsyncRejectsBadArgs(t *testing.T) {
	home := newHome(t)
	env := stubs(t, 1000, 1000)
	for _, args := range [][]string{
		{"--home", home},
		{"--home", home, "--changesets", "/cs", "--rpc", "x", "--to", "30"}, // no trust root
		append([]string{"--home", home, "--from", "40"}, baseArgs...),
		append([]string{"--home", home, "--anchor-hash", "AB"}, baseArgs...), // anchor height not above --to
	} {
		_, err := runScript(t, env, "fastsync.sh", args...)
		require.Error(t, err, args)
	}
	require.NoError(t, os.MkdirAll(filepath.Join(home, "data", "state.db", "x"), 0o755))
	out, err := runScript(t, env, "fastsync.sh", append([]string{"--home", home}, baseArgs...)...)
	require.Error(t, err)
	require.Contains(t, out, "state.db is not empty")
}

func segmentFixture(t *testing.T, appdBody string, rowsFrom func(h int) string) (ck, exp string, env []string) {
	ck = t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ck, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ck, "config", "config.toml"), []byte("db_backend = \"goleveldb\"\nladdr = \"tcp://0.0.0.0:26656\"\nladdr = \"tcp://127.0.0.1:26657\"\nproxy_app = \"tcp://127.0.0.1:26658\"\npersistent_peers = \"\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ck, "CHECKPOINT"), []byte("height\t10\n"), 0o644))
	var e strings.Builder
	for h := 1; h <= 40; h++ {
		e.WriteString(row(h) + "\n")
	}
	exp = filepath.Join(ck, "expected.tsv")
	require.NoError(t, os.WriteFile(exp, []byte(e.String()), 0o644))
	var hs strings.Builder
	hs.WriteString(`# fshdr headers (UNVERIFIED)\n`)
	for h := 11; h <= 30; h++ {
		hs.WriteString(rowsFrom(h) + `\n`)
	}
	bin := t.TempDir()
	env = []string{
		"APPD=" + stub(t, bin, "appd", appdBody),
		"FSHDR=" + stub(t, bin, "fshdr", `printf "`+hs.String()+`"`),
	}
	return ck, exp, env
}

func TestSegment(t *testing.T) {
	cleanHalt := `echo "panic: halting node per configuration at height 31"; exit 2`
	ck, exp, env := segmentFixture(t, cleanHalt, row)
	out, err := runScript(t, env, "segment.sh", ck, "30", "31", "id@127.0.0.1:26656", exp)
	require.NoError(t, err, out)
	require.Contains(t, out, "SEGMENT 10..30: OK")
	cfg, err := os.ReadFile(filepath.Join(ck, "config", "config.toml"))
	require.NoError(t, err)
	require.Contains(t, string(cfg), `laddr = "tcp://127.0.0.1:31656"`)
	require.Contains(t, string(cfg), `persistent_peers = "id@127.0.0.1:26656"`)

	bad := func(h int) string {
		if h == 25 {
			return strings.Replace(row(h), hash("app", h), strings.Repeat("00", 32), 1)
		}
		return row(h)
	}
	ck, exp, env = segmentFixture(t, cleanHalt, bad)
	out, err = runScript(t, env, "segment.sh", ck, "30", "31", "id@x:1", exp)
	require.Error(t, err)
	require.Contains(t, out, "BOUNDARY MISMATCH")

	ck, exp, env = segmentFixture(t, `echo "panic: wrong Block.Header.AppHash"; exit 2`, row)
	out, err = runScript(t, env, "segment.sh", ck, "30", "31", "id@x:1", exp)
	require.Error(t, err)
	require.Contains(t, out, "SEGMENT 10..30: FAILED")

	ck, exp, env = segmentFixture(t, cleanHalt, row)
	out, err = runScript(t, env, "segment.sh", "--dry-run", ck, "30", "31", "id@x:1", exp)
	require.NoError(t, err, out)
	require.Contains(t, out, "--halt-height 31")
	cfg, err = os.ReadFile(filepath.Join(ck, "config", "config.toml"))
	require.NoError(t, err)
	require.Contains(t, string(cfg), "26656", "dry run changes nothing")
}
