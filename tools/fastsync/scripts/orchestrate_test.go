package scripts

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests of orchestrate/*.sh with stub tools: gcloud is backed by a local directory, the appliers, fsboot,
// celestia-appd and fshdr are the stubs of scripts_test.go (plus an fshdr headers stub), and segment.sh is real.

const testPeer = "0123456789abcdef0123456789abcdef01234567@10.0.0.2:26656"

// orchEnv returns the stub environment plus a gcloud stub whose gs://NAME/... objects live under the
// returned bucket root, an fshdr headers stub that serves rows of the "node block store" file nodeRows,
// and a manifest stub.
func orchEnv(t *testing.T, handoverAt, failAt int) (env []string, bucketRoot, nodeRows string) {
	env = stubs(t, handoverAt, failAt)
	bin := t.TempDir()
	bucketRoot = t.TempDir()
	nodeRows = filepath.Join(bin, "node-rows.tsv")
	var b strings.Builder
	for h := 1; h <= 60; h++ {
		b.WriteString(row(h) + "\n")
	}
	require.NoError(t, os.WriteFile(nodeRows, []byte(b.String()), 0o644))
	gcloud := stub(t, bin, "gcloud", `map() { case $1 in gs://*) echo "`+bucketRoot+`/${1#gs://}";; *) echo "$1";; esac; }
[ "$1" = storage ] || exit 64
case $2 in
  ls) [ -e "$(map "$3")" ];;
  cp) shift 2; args=("$@"); dst=$(map "${args[-1]}"); unset 'args[-1]'
      for s in "${args[@]}"; do
        src=$(map "$s"); [ -f "$src" ] || { echo "no such object $s" >&2; exit 1; }
        if [[ $dst == */ ]]; then mkdir -p "$dst"; cp "$src" "$dst"; else mkdir -p "$(dirname "$dst")"; cp "$src" "$dst"; fi
      done;;
  *) exit 64;;
esac
`)
	fshdr := stub(t, bin, "fshdr", `[ "$1" = headers ] || exit 64
while [ $# -gt 0 ]; do case $1 in -from) lo=$2; shift;; -to) hi=$2; shift;; esac; shift; done
echo "# fshdr headers (UNVERIFIED)"
awk -F'\t' -v lo="$lo" -v hi="$hi" '$1>=lo && $1<=hi' "`+nodeRows+`"
`)
	manifest := stub(t, bin, "manifest", `echo "manifest $*" >> "`+envValue(env, "CALLS")+`"; printf 'CHAIN_ID=test-chain\nLAST_HEIGHT=100\n'`)
	env = append(env, "GCLOUD="+gcloud, "FSHDR="+fshdr, "MANIFEST_BIN="+manifest,
		"APPD="+stub(t, bin, "appd", `while [ $# -gt 0 ]; do case $1 in --halt-height) h=$2; shift;; esac; shift; done
if [ "${STUB_APPD_FAIL:-0}" = 1 ]; then echo "CONSENSUS FAILURE!!! wrong Block.Header.AppHash"; exit 1; fi
echo "panic: halting node per configuration at height $h"; exit 2
`))
	if _, err := exec.LookPath("zstd"); err != nil { // stand-in on hosts without zstd
		zstd := stub(t, bin, "zstd", `out='' dec=0 in=''
while [ $# -gt 0 ]; do case $1 in -o) out=$2; shift;; -dc) dec=1;; -*) ;; *) in=$1;; esac; shift; done
if [ "$dec" = 1 ]; then cat "$in"; else cat > "$out"; fi
`)
		env = append(env, "PATH="+filepath.Dir(zstd)+":"+os.Getenv("PATH"))
	}
	return env, bucketRoot, nodeRows
}

func writeExpected(t *testing.T, dir string) string {
	var b strings.Builder
	b.WriteString("# fshdr verify chain=test-chain anchor=61:AB\n")
	for h := 1; h <= 60; h++ {
		b.WriteString(row(h) + "\n")
	}
	p := filepath.Join(dir, "expected.tsv")
	require.NoError(t, os.WriteFile(p, []byte(b.String()), 0o644))
	return p
}

func readKV(t *testing.T, p string) map[string]string {
	bz, err := os.ReadFile(p)
	require.NoError(t, err)
	m := map[string]string{}
	for _, l := range strings.Split(strings.TrimSpace(string(bz)), "\n") {
		k, v, _ := strings.Cut(l, "=")
		m[k] = v
	}
	return m
}

func TestPlanSegments(t *testing.T) {
	out, err := runScript(t, nil, "orchestrate/plan-segments.sh", "--to", "1000000", "--vms", "2")
	require.NoError(t, err, out)
	require.Equal(t, "# plan: heights 1..1000000, checkpoint every 250000, 2 VM(s) -> 4 segment(s), 2 wave(s)\n"+
		"seg\tstart_ckpt\tfirst_height\tend_height\tblocks\tvm\twave\n"+
		"0\tgenesis\t1\t250000\t250000\t0\t0\n"+
		"1\t250000\t250001\t500000\t250000\t1\t0\n"+
		"2\t500000\t500001\t750000\t250000\t0\t1\n"+
		"3\t750000\t750001\t1000000\t250000\t1\t1\n", out)
	out, err = runScript(t, nil, "orchestrate/plan-segments.sh", "--from", "500001", "--to", "760000")
	require.NoError(t, err, out)
	require.Contains(t, out, "1\t750000\t750001\t760000\t10000\t0\t1\n")
	out, err = runScript(t, nil, "orchestrate/plan-segments.sh", "--to", "5")
	require.NoError(t, err, out)
	require.Contains(t, out, "0\tgenesis\t1\t5\t5\t0\t0\n")
	for _, args := range [][]string{{"--to", "5", "--from", "6"}, {"--to", "10", "--vms", "0"}, {"--to", "x"},
		{"--to", "10", "--every", "0"}, {"--from", "300001", "--to", "760000"}, {"--to", "10", "--format", "tsv"}} {
		_, err := runScript(t, nil, "orchestrate/plan-segments.sh", args...)
		require.Error(t, err, args)
	}
}

func applyArgs(home, cs, exp string, extra ...string) []string {
	return append([]string{"--home", home, "--changesets", cs, "--expected", exp, "--chain-id", "test-chain",
		"--bucket", "gs://bkt/run", "--every", "10", "--to", "30"}, extra...)
}

func TestApplyCheckpointsDryRun(t *testing.T) {
	env, _, _ := orchEnv(t, 1000, 1000)
	out, err := runScript(t, env, "orchestrate/apply-checkpoints.sh", applyArgs("/h", "/cs", "/e.tsv", "--dry-run", "--manifest", "/m.json", "--pub", "/k.pub")...)
	require.NoError(t, err, out)
	applier := envValue(env, "APPLIER")
	require.Contains(t, out, "+ chmod -R a-w /cs\n")
	require.Contains(t, out, "manifest verify -dir /cs -manifest /m.json -sig /m.json.sig -pub /k.pub -chain-id test-chain -print-env\n")
	require.Contains(t, out, "+ "+applier+" apply -db /h/data -backend goleveldb -changesets /cs -expected /e.tsv -require-trusted -to 10 -parallel\n")
	require.Contains(t, out, "-require-trusted -to 30 -parallel\n")
	require.Contains(t, out, "+ tar -C /h/data -cf - application.db | zstd -q -T0 -3 -o /h/fastsync-orch/ckpt-10/application.db.tar.zst\n")
	tb := strings.Index(out, "/h/fastsync-orch/ckpt-20/application.db.tar.zst gs://bkt/run/checkpoints/20/application.db.tar.zst")
	ck := strings.Index(out, "/h/fastsync-orch/ckpt-20/CHECKPOINT gs://bkt/run/checkpoints/20/CHECKPOINT")
	require.True(t, tb > 0 && ck > tb, "CHECKPOINT is uploaded after the tarball:\n%s", out)

	for _, args := range [][]string{
		applyArgs("/h", "/cs", "/e", "--dry-run"), // no manifest and no --skip-bundle-verify
		{"--home", "/h", "--changesets", "/cs", "--expected", "/e", "--chain-id", "bad id", "--bucket", "gs://bkt", "--to", "10", "--skip-bundle-verify", "--dry-run"},
		{"--home", "/h", "--changesets", "/cs", "--expected", "/e", "--chain-id", "c", "--bucket", "s3://bkt", "--to", "10", "--skip-bundle-verify", "--dry-run"},
		{"--home", "/h", "--changesets", "/cs", "--expected", "/e", "--chain-id", "c", "--bucket", "gs://bkt", "--to", "10", "--checkpoints", "5", "--skip-bundle-verify", "--dry-run"},
	} {
		_, err := runScript(t, env, "orchestrate/apply-checkpoints.sh", args...)
		require.Error(t, err, args)
	}
}

// publishCheckpoints runs apply-checkpoints.sh (checkpoints 10, 20, 30) and returns the bucket root.
func publishCheckpoints(t *testing.T, env []string, bucketRoot string) string {
	home := newHome(t)
	require.NoError(t, os.WriteFile(filepath.Join(home, "data", "application.db", "MANIFEST"), []byte("db"), 0o644))
	cs := t.TempDir()
	t.Cleanup(func() { _ = exec.Command("chmod", "-R", "u+w", cs).Run() }) // runs before TempDir's removal
	require.NoError(t, os.WriteFile(filepath.Join(cs, "cs-000000000001.bin.zst"), []byte("cs"), 0o644))
	exp := writeExpected(t, t.TempDir())
	out, err := runScript(t, env, "orchestrate/apply-checkpoints.sh", applyArgs(home, cs, exp, "--manifest", "/m.json", "--pub", "/k.pub")...)
	require.NoError(t, err, out)
	st, err := os.Stat(filepath.Join(cs, "cs-000000000001.bin.zst"))
	require.NoError(t, err)
	require.Zero(t, st.Mode().Perm()&0o222, "the bundle is made read-only before it is verified")
	calls, err := os.ReadFile(envValue(env, "CALLS"))
	require.NoError(t, err)
	require.Contains(t, string(calls), "manifest verify -dir "+cs)

	// a rerun publishes nothing again
	out, err = runScript(t, env, "orchestrate/apply-checkpoints.sh", applyArgs(home, cs, exp, "--skip-bundle-verify")...)
	require.NoError(t, err, out)
	require.Contains(t, out, "checkpoint 10 already published")
	return bucketRoot
}

func TestApplyCheckpoints(t *testing.T) {
	env, bucketRoot, _ := orchEnv(t, 15, 1000)
	publishCheckpoints(t, env, bucketRoot)
	for _, c := range []int{10, 20, 30} {
		d := filepath.Join(bucketRoot, "bkt", "run", "checkpoints", fmt.Sprint(c))
		kv := readKV(t, filepath.Join(d, "CHECKPOINT"))
		tb, err := os.ReadFile(filepath.Join(d, "application.db.tar.zst"))
		require.NoError(t, err)
		format := "v050"
		if c == 10 {
			format = "v046" // before the HANDOVER at 16
		}
		require.Equal(t, map[string]string{
			"chain_id": "test-chain", "height": fmt.Sprint(c), "app_hash_after": hash("app", c),
			"tarball": "application.db.tar.zst", "tarball_sha256": fmt.Sprintf("%x", sha256.Sum256(tb)), "store_format": format,
		}, kv)
	}
}

func TestApplyCheckpointsMismatch(t *testing.T) {
	env, bucketRoot, _ := orchEnv(t, 1000, 20)
	home := newHome(t)
	exp := writeExpected(t, t.TempDir())
	out, err := runScript(t, env, "orchestrate/apply-checkpoints.sh", applyArgs(home, t.TempDir(), exp, "--skip-bundle-verify")...)
	require.Error(t, err)
	require.Contains(t, out, "APP HASH MISMATCH during apply")
	require.FileExists(t, filepath.Join(home, "fastsync-orch", "POISONED"))
	require.FileExists(t, filepath.Join(bucketRoot, "bkt", "run", "checkpoints", "10", "CHECKPOINT"))
	require.NoDirExists(t, filepath.Join(bucketRoot, "bkt", "run", "checkpoints", "20"))
	out, err = runScript(t, env, "orchestrate/apply-checkpoints.sh", applyArgs(home, t.TempDir(), exp, "--skip-bundle-verify")...)
	require.Error(t, err)
	require.Contains(t, out, "POISONED")

	// an unverified expected.tsv (no fshdr header) is refused
	bad := filepath.Join(t.TempDir(), "expected.tsv")
	require.NoError(t, os.WriteFile(bad, []byte(row(10)+"\n"), 0o644))
	out, err = runScript(t, env, "orchestrate/apply-checkpoints.sh", applyArgs(newHome(t), t.TempDir(), bad, "--skip-bundle-verify")...)
	require.Error(t, err)
	require.Contains(t, out, "not a verified fshdr file")
}

func segHome(t *testing.T) string {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(home, "data"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "config.toml"), []byte("db_backend = \"goleveldb\"\npersistent_peers = \"\"\n\n[blocksync]\nversion = \"v0\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "app.toml"), []byte("minimum-gas-prices = \"\"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "genesis.json"), []byte(`{"chain_id": "test-chain"}`), 0o644))
	return home
}

func segArgs(home, exp string, extra ...string) []string {
	return append([]string{"--bucket", "gs://bkt/run", "--checkpoint", "10", "--end", "30", "--home", home, "--expected", exp,
		"--chain-id", "test-chain", "--peer", testPeer, "--rpc", "http://rpc-a,http://rpc-b"}, extra...)
}

func TestSegmentRunDryRun(t *testing.T) {
	env, _, _ := orchEnv(t, 1000, 1000)
	out, err := runScript(t, env, "orchestrate/segment-run.sh", segArgs("/h", "/e.tsv", "--dry-run", "--max-requesters", "200", "--fastindex")...)
	require.NoError(t, err, out)
	for _, want := range []string{
		"storage cp gs://bkt/run/checkpoints/10/CHECKPOINT gs://bkt/run/checkpoints/10/application.db.tar.zst /h/segment/\n",
		"+ zstd -q -dc /h/segment/application.db.tar.zst | head -c 1099511627776 | tar -C /h/data --no-same-owner --no-same-permissions -xf - application.db\n",
		"+ fastindex -db /h/data -backend goleveldb\n",
		" bootstrap -home /h -height 10 -expected /e.tsv -rpc http://rpc-a,http://rpc-b\n",
		"+ set_toml /h/config/app.toml performance inter-block-cache \"sync-only\"\n",
		"+ set_toml /h/config/config.toml blocksync max_requesters 200\n",
		"segment.sh /h 30 26 " + testPeer + " /e.tsv\n",
		"would upload RESULT status=PASS to gs://bkt/run/results/11-30/",
	} {
		require.Contains(t, out, want)
	}
	out, err = runScript(t, env, "orchestrate/segment-run.sh", "--dry-run", "--bucket", "gs://bkt", "--checkpoint", "genesis", "--end", "30",
		"--home", "/h", "--expected", "/e", "--chain-id", "test-chain", "--peer", testPeer, "--inter-block-cache", "keep")
	require.NoError(t, err, out)
	require.NotContains(t, out, "checkpoints/")
	require.NotContains(t, out, "bootstrap -home")
	require.NotContains(t, out, "set_toml")

	for _, args := range [][]string{
		segArgs("/h", "/e", "--dry-run", "--end", "25"),                         // fewer than 20 blocks
		segArgs("/h", "/e", "--dry-run", "--rpc", "http://rpc-a,http://rpc-a/"), // one distinct server
		segArgs("/h", "/e", "--dry-run", "--max-requesters", "1"),
		segArgs("/h", "/e", "--dry-run", "--inter-block-cache", "on"),
		segArgs("/h", "/e", "--dry-run", "--peer", "nope"),
		segArgs("/h", "/e", "--dry-run", "--env", "X=1"),
	} {
		_, err := runScript(t, env, "orchestrate/segment-run.sh", args...)
		require.Error(t, err, args)
	}
	out, err = runScript(t, env, "orchestrate/segment-run.sh", segArgs("/h", "/e", "--dry-run", "--rpc", "http://rpc-a", "--allow-single-source")...)
	require.NoError(t, err, out)
	require.Contains(t, out, "-rpc http://rpc-a -allow-single-source")
}

// replaceCheckpoint replaces the tarball of checkpoint 10 by one built from dir (tar flags in tarArgs) and
// updates the CHECKPOINT sha256, as a malicious bucket writer could.
func replaceCheckpoint(t *testing.T, env []string, bucketRoot, dir string, tarArgs ...string) {
	d := filepath.Join(bucketRoot, "bkt", "run", "checkpoints", "10")
	tb := filepath.Join(d, "application.db.tar.zst")
	cmd := exec.Command("bash", "-c", `tar -C "$1" -cf - "${@:3}" application.db | zstd -q -o "$2" -f`, "_", dir, tb)
	cmd.Args = append(cmd.Args, tarArgs...)
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, string(out))
	bz, err := os.ReadFile(tb)
	require.NoError(t, err)
	kv := readKV(t, filepath.Join(d, "CHECKPOINT"))
	kv["tarball_sha256"] = fmt.Sprintf("%x", sha256.Sum256(bz))
	var b strings.Builder
	for _, k := range []string{"chain_id", "height", "app_hash_after", "tarball", "tarball_sha256", "store_format"} {
		b.WriteString(k + "=" + kv[k] + "\n")
	}
	require.NoError(t, os.WriteFile(filepath.Join(d, "CHECKPOINT"), []byte(b.String()), 0o644))
}

func TestSegmentRun(t *testing.T) {
	env, bucketRoot, nodeRows := orchEnv(t, 1000, 1000)
	publishCheckpoints(t, env, bucketRoot)
	exp := writeExpected(t, t.TempDir())
	result := func() map[string]string {
		return readKV(t, filepath.Join(bucketRoot, "bkt", "run", "results", "11-30", "RESULT"))
	}
	run := func(env []string, extra ...string) (string, string, error) {
		home := segHome(t)
		out, err := runScript(t, env, "orchestrate/segment-run.sh", segArgs(home, exp, extra...)...)
		return home, out, err
	}

	home, out, err := run(env, "--max-requesters", "300")
	require.NoError(t, err, out)
	require.Equal(t, "PASS", result()["status"])
	require.Contains(t, result()["reason"], "SEGMENT 10..30: OK")
	require.FileExists(t, filepath.Join(home, "data", "application.db", "MANIFEST"))
	app, err := os.ReadFile(filepath.Join(home, "config", "app.toml"))
	require.NoError(t, err)
	require.Contains(t, string(app), "[performance]\ninter-block-cache = \"sync-only\"\n")
	cfg, err := os.ReadFile(filepath.Join(home, "config", "config.toml"))
	require.NoError(t, err)
	require.Contains(t, string(cfg), "[blocksync]\nversion = \"v0\"\nmax_requesters = 300\n")
	require.Contains(t, string(cfg), `persistent_peers = "`+testPeer+`"`)
	calls, err := os.ReadFile(envValue(env, "CALLS"))
	require.NoError(t, err)
	require.Contains(t, string(calls), "fsboot bootstrap -home "+home+" -height 10 -expected "+exp+" -rpc http://rpc-a,http://rpc-b\n")

	_, out, err = run(append(env, "STUB_APPD_FAIL=1"))
	require.Error(t, err, out)
	require.Equal(t, "FAIL", result()["status"])
	require.Contains(t, result()["reason"], "FAILED")

	// the node's block store differs from the verified chain at 25
	rows, err := os.ReadFile(nodeRows)
	require.NoError(t, err)
	forked := strings.Replace(string(rows), row(25), strings.Replace(row(25), hash("app", 25), strings.Repeat("00", 32), 1), 1)
	require.NoError(t, os.WriteFile(nodeRows, []byte(forked), 0o644))
	_, out, err = run(env)
	require.Error(t, err, out)
	require.Contains(t, result()["reason"], "BOUNDARY MISMATCH")
	require.NoError(t, os.WriteFile(nodeRows, rows, 0o644))

	// over the decompression cap
	_, out, err = run(env, "--max-db-bytes", "100")
	require.Error(t, err, out)
	require.Contains(t, result()["reason"], "more than --max-db-bytes")

	// a tampered tarball
	tbPath := filepath.Join(bucketRoot, "bkt", "run", "checkpoints", "10", "application.db.tar.zst")
	orig, err := os.ReadFile(tbPath)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(tbPath, append(append([]byte(nil), orig...), 'X'), 0o644))
	_, out, err = run(env)
	require.Error(t, err, out)
	require.Contains(t, result()["reason"], "sha256 mismatch")

	// a tarball with a symlink, even with a matching CHECKPOINT
	src := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(src, "application.db"), 0o755))
	require.NoError(t, os.Symlink("/etc", filepath.Join(src, "application.db", "link")))
	replaceCheckpoint(t, env, bucketRoot, src)
	_, out, err = run(env)
	require.Error(t, err, out)
	require.Contains(t, result()["reason"], "non-regular entry: application.db/link")

	// owners and modes from the archive are not applied
	require.NoError(t, os.Remove(filepath.Join(src, "application.db", "link")))
	require.NoError(t, os.WriteFile(filepath.Join(src, "application.db", "MANIFEST"), []byte("db"), 0o4755))
	replaceCheckpoint(t, env, bucketRoot, src, "--owner=1234", "--group=1234")
	home, out, err = run(env)
	require.NoError(t, err, out)
	st, err := os.Stat(filepath.Join(home, "data", "application.db", "MANIFEST"))
	require.NoError(t, err)
	require.Zero(t, st.Mode()&os.ModeSetuid, "setuid bit from the archive")
	if os.Geteuid() == 0 {
		owner, err := exec.Command("stat", "-c", "%u", filepath.Join(home, "data", "application.db", "MANIFEST")).Output()
		require.NoError(t, err)
		require.Equal(t, "0", strings.TrimSpace(string(owner)), "owner from the archive")
	}
}
