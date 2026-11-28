package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pruningtypes "github.com/cosmos/cosmos-sdk/pruning/types"
	"github.com/cosmos/cosmos-sdk/store/changeset"
	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	"github.com/cosmos/cosmos-sdk/store/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/expected"
)

const (
	testChain = "cstool-test"
	nBlocks   = 30
)

// record commits nBlocks heights of a small two-store app with the changeset
// recorder on and returns the changeset dir and the app hash of every height.
func record(t *testing.T) (string, map[int64][]byte) {
	dir := t.TempDir()
	rec, err := changeset.New(dir, changeset.Options{BlocksPerFile: 10, StoreInfos: true, Zstd: true})
	require.NoError(t, err)
	rs := rootmulti.NewStore(dbm.NewMemDB(), log.NewNopLogger())
	rs.SetChangesetRecorder(rec)
	rs.SetPruning(pruningtypes.NewPruningOptions(pruningtypes.PruningEverything))
	keys := []*types.KVStoreKey{types.NewKVStoreKey("acc"), types.NewKVStoreKey("bank")}
	for _, k := range keys {
		rs.MountStoreWithDB(k, types.StoreTypeIAVL, nil)
	}
	require.NoError(t, rs.LoadLatestVersion())
	hashes := map[int64][]byte{}
	for h := int64(1); h <= nBlocks; h++ {
		ms := rs.CacheMultiStore()
		for i, k := range keys {
			kv := ms.GetKVStore(k)
			kv.Set(fmt.Appendf(nil, "key-%d", h), fmt.Appendf(nil, "value-%d-%d", h, i))
			kv.Set([]byte("counter"), fmt.Appendf(nil, "%d", h*int64(i+1)))
			kv.Delete(fmt.Appendf(nil, "key-%d", h-3))
		}
		ms.Write()
		id := rs.Commit()
		hashes[h] = id.Hash
	}
	require.NoError(t, rec.Close())
	return dir, hashes
}

// writeExpected writes a verified-looking expected.tsv with the given rows.
func writeExpected(t *testing.T, hashes map[int64][]byte, skip int64) string {
	var sb strings.Builder
	sb.WriteString(expected.Header("verify", testChain, "anchor=31:AB") + "\n")
	for h := int64(1); h <= nBlocks; h++ {
		if h == skip {
			continue
		}
		r := expected.Row{Height: h, AppHash: hashes[h], NextHeaderHash: bytes.Repeat([]byte{byte(h)}, 32)}
		sb.WriteString(r.String() + "\n")
	}
	p := filepath.Join(t.TempDir(), "expected.tsv")
	require.NoError(t, os.WriteFile(p, []byte(sb.String()), 0o644))
	return p
}

// rewrite writes the blocks of dir into a new uncompressed log, after mut.
func rewrite(t *testing.T, dir string, mut func(b *changeset.Block)) string {
	out := t.TempDir()
	fw, err := changeset.CreateFile(filepath.Join(out, "cs-000000000001.bin"))
	require.NoError(t, err)
	require.NoError(t, changeset.Stream(dir, 0, func(b *changeset.Block) error {
		mut(b)
		return fw.Write(b)
	}))
	require.NoError(t, fw.Close())
	return out
}

func apply(t *testing.T, args ...string) (string, int) {
	var out, errOut bytes.Buffer
	code := exitCode(cmdApply(append([]string{"-chain-id", testChain, "-progress", "0"}, args...), &out), &errOut)
	return out.String() + errOut.String(), code
}

func latest(t *testing.T, dir, backend string) int64 {
	db, err := openDB(dir, backend)
	require.NoError(t, err)
	defer db.Close()
	return rootmulti.GetLatestVersion(db)
}

func TestApplyVerifiedAndResume(t *testing.T) {
	cs, hashes := record(t)
	exp := writeExpected(t, hashes, 0)
	for _, backend := range []string{"goleveldb", "pebbledb"} {
		t.Run(backend, func(t *testing.T) {
			db := t.TempDir()
			out, code := apply(t, "-db", db, "-backend", backend, "-changesets", cs, "-expected", exp, "-to", "20", "-parallel")
			require.Equal(t, 0, code, out)
			require.Contains(t, out, "done: applied 20 blocks (to height 20)")
			require.Contains(t, out, "verified against trusted headers: 20, recorded hash only: 0")
			out, code = apply(t, "-db", db, "-backend", backend, "-changesets", cs, "-expected", exp, "-to", "30")
			require.Equal(t, 0, code, out)
			require.Contains(t, out, "done: applied 10 blocks (to height 30)")
			out, code = apply(t, "-db", db, "-backend", backend, "-changesets", cs, "-expected", exp, "-to", "30")
			require.Equal(t, 0, code, out)
			require.Contains(t, out, "done: applied 0 blocks (to height 30)")
		})
	}
}

// One flipped byte in a recorded value (framing valid, recorded app hash left
// as is) is caught at that height: exit 2, the height is not kept.
func TestTamperedValueExits2(t *testing.T) {
	cs, hashes := record(t)
	tampered := rewrite(t, cs, func(b *changeset.Block) {
		if b.Height != 15 {
			return
		}
		op := &b.Stores[0].Ops[0]
		require.Equal(t, changeset.OpSet, op.Kind)
		op.Value = append([]byte(nil), op.Value...)
		op.Value[len(op.Value)-1] ^= 1
	})
	db := t.TempDir()
	out, code := apply(t, "-db", db, "-changesets", tampered, "-expected", writeExpected(t, hashes, 0), "-to", "30")
	require.Equal(t, 2, code, out)
	require.Contains(t, out, "APP HASH MISMATCH")
	require.Contains(t, out, "height 15")
	require.EqualValues(t, 14, latest(t, db, "goleveldb"))
}

func TestStopsWithoutTrustedHash(t *testing.T) {
	cs, hashes := record(t)
	db := t.TempDir()
	out, code := apply(t, "-db", db, "-changesets", cs, "-expected", writeExpected(t, hashes, 25), "-to", "30")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "done: applied 24 blocks (to height 24)")
	require.Contains(t, out, "stopped before height 25")
}

func TestHandover(t *testing.T) {
	cs, hashes := record(t)
	v1 := rewrite(t, cs, func(b *changeset.Block) {
		if b.Height > 20 {
			b.StoreV1 = true
		}
	})
	out, code := apply(t, "-db", t.TempDir(), "-changesets", v1, "-expected", writeExpected(t, hashes, 0), "-to", "30")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "done: applied 20 blocks (to height 20)")
	require.Contains(t, out, "\nHANDOVER: block 21")
}

func TestExpectedMustBeVerified(t *testing.T) {
	cs, hashes := record(t)
	exp := writeExpected(t, hashes, 0)
	bz, err := os.ReadFile(exp)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(exp, append([]byte("# fshdr headers (UNVERIFIED)\n"), bz[bytes.IndexByte(bz, '\n')+1:]...), 0o644))
	out, code := apply(t, "-db", t.TempDir(), "-changesets", cs, "-expected", exp)
	require.Equal(t, 1, code)
	require.Contains(t, out, "not a verified fshdr header")

	out, code = apply(t, "-db", t.TempDir(), "-changesets", cs)
	require.Equal(t, 1, code)
	require.Contains(t, out, "need -db, -changesets and -expected")
}

func TestAllowUnverified(t *testing.T) {
	cs, _ := record(t)
	out, code := apply(t, "-db", t.TempDir(), "-changesets", cs, "-allow-unverified")
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "WARNING: -allow-unverified")
	require.Contains(t, out, "recorded hash only: 30")
}

func TestChainIDFromGenesis(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, "config", "genesis.json"), []byte(`{"chain_id":"from-genesis"}`), 0o644))
	id, err := chainID("", filepath.Join(home, "data"))
	require.NoError(t, err)
	require.Equal(t, "from-genesis", id)
	_, err = chainID("", t.TempDir())
	require.ErrorContains(t, err, "no -chain-id")
}
