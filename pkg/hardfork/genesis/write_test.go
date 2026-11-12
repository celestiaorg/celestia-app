package genesis_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/pkg/hardfork/genesis"
	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"
)

func (f fixture) candidate(t *testing.T, opts genesis.Options) (*genesis.Export, *genesis.Genesis) {
	t.Helper()
	exp, err := genesis.LoadExport(bytes.NewReader(f.file), f.modules, opts)
	require.NoError(t, err)
	cand, err := genesis.NewCandidate(f.cdc, exp, opts)
	require.NoError(t, err)
	return exp, cand
}

func TestWriterByteIdentity(t *testing.T) {
	f := newFixture(t)
	g := f.decode(t)
	g.Consensus.Validators[0].Name = "v<&>"
	f.file = encode(t, g)
	// Literal and escaped HTML characters in a module and in consensus, in an indented file.
	f.file = f.withVesting(t, "{\"s\":\"<>&\u2028\",\"t\":\"\\u003c\"}")
	f.file = f.replace(t, `"v\u003c\u0026\u003e"`, `"v<&>"`)
	var indented bytes.Buffer
	require.NoError(t, json.Indent(&indented, f.file, "", "  "))
	f.file = indented.Bytes()

	_, cand := f.candidate(t, f.options())
	var out bytes.Buffer
	w, err := genesis.WriteGenesis(&out, cand)
	require.NoError(t, err)

	// The reference is json.Marshal of the input with the three outer fields set.
	var want genutiltypes.AppGenesis
	require.NoError(t, json.Unmarshal(f.file, &want))
	want.ChainID, want.InitialHeight, want.GenesisTime = cand.ChainID, cand.InitialHeight, cand.GenesisTime
	wantBytes, err := json.Marshal(&want)
	require.NoError(t, err)
	require.Equal(t, string(wantBytes), out.String())
	require.Contains(t, out.String(), `"s":"\u003c\u003e\u0026\u2028","t":"\u003c"`)

	// RequestInitChain as celestia-core builds it from the written file.
	var written genutiltypes.AppGenesis
	require.NoError(t, json.Unmarshal(out.Bytes(), &written))
	vals := make([]*cmttypes.Validator, len(written.Consensus.Validators))
	for i, v := range written.Consensus.Validators {
		vals[i] = cmttypes.NewValidator(v.PubKey, v.Power)
	}
	params := written.Consensus.Params.ToProto()
	req := abci.RequestInitChain{
		Time:            written.GenesisTime,
		ChainId:         written.ChainID,
		InitialHeight:   written.InitialHeight,
		ConsensusParams: &params,
		Validators:      cmttypes.TM2PB.ValidatorUpdates(cmttypes.NewValidatorSet(vals)),
		AppStateBytes:   written.AppState,
	}
	require.Equal(t, genesis.Sizes{File: int64(out.Len()), AppState: int64(len(written.AppState)), InitChain: int64(req.Size())}, w.Sizes)
}

func TestEncodeDeterministic(t *testing.T) {
	f := newFixture(t)
	local := time.Local
	t.Cleanup(func() { time.Local = local })
	run := func(loc *time.Location) ([]byte, genesis.Written, *genesis.Report) {
		time.Local = loc
		opts := f.options()
		opts.ExportBlockTime, opts.GenesisTime = opts.ExportBlockTime.In(loc), opts.GenesisTime.In(loc)
		exp, cand := f.candidate(t, opts)
		var out bytes.Buffer
		w, err := genesis.WriteGenesis(&out, cand)
		require.NoError(t, err)
		return out.Bytes(), w, genesis.NewReport(exp, cand, opts)
	}

	a, wa, ra := run(time.UTC)
	b, wb, rb := run(time.FixedZone("UTC+3", 3*3600))
	require.Equal(t, a, b)
	require.Equal(t, wa, wb)
	require.Equal(t, ra, rb)
	require.Equal(t, sha256.Sum256(a), wa.SHA256)
}

func TestSizeLimits(t *testing.T) {
	for n, want := range map[int64]string{
		75<<20 - 1:        "",
		75 << 20:          "",
		75<<20 + 1:        "above 75 MiB",
		math.MaxInt32:     "above 75 MiB",
		math.MaxInt32 + 1: "only a native start",
	} {
		got := genesis.TransportNote(n)
		if want == "" {
			require.Empty(t, got, n)
		} else {
			require.Contains(t, got, want, n)
		}
	}
}

var errInjected = errors.New("injected")

// nth fails the nth call.
func nth(n int) func(string) error {
	calls := 0
	return func(string) error {
		if calls++; calls == n {
			return errInjected
		}
		return nil
	}
}

// hook replaces *fn with wrap(*fn) for the test.
func hook[F any](t *testing.T, fn *F, wrap func(F) F) {
	orig := *fn
	t.Cleanup(func() { *fn = orig })
	*fn = wrap(orig)
}

func TestInstall(t *testing.T) {
	f := newFixture(t)
	exp, cand := f.candidate(t, f.options())
	type paths struct{ dir, in, out, rep string }
	setup := func(t *testing.T) paths {
		dir := t.TempDir()
		for _, d := range []string{"in", "out", "rep"} {
			require.NoError(t, os.Mkdir(filepath.Join(dir, d), 0o700))
		}
		p := paths{dir, filepath.Join(dir, "in", "export.json"), filepath.Join(dir, "out", "genesis.json"), filepath.Join(dir, "rep", "report.json")}
		require.NoError(t, os.WriteFile(p.in, f.file, 0o600))
		return p
	}
	entries := func(t *testing.T, dir string) []string {
		es, err := os.ReadDir(dir)
		require.NoError(t, err)
		names := make([]string, len(es))
		for i, e := range es {
			names[i] = e.Name()
		}
		return names
	}
	install := func(p paths) error {
		return genesis.Install(cand, genesis.NewReport(exp, cand, f.options()), p.in, p.out, p.rep)
	}

	t.Run("success", func(t *testing.T) {
		p := setup(t)
		hook(t, genesis.Link, func(link func(string, string) error) func(string, string) error {
			return func(tmp, dst string) error {
				// Each temp is in its destination's directory.
				require.Equal(t, filepath.Dir(dst), filepath.Dir(tmp))
				require.True(t, strings.HasPrefix(filepath.Base(tmp), ".from-export-"), tmp)
				return link(tmp, dst)
			}
		})
		rep := genesis.NewReport(exp, cand, f.options())
		require.NoError(t, genesis.Install(cand, rep, p.in, p.out, p.rep))

		bz, err := os.ReadFile(p.out)
		require.NoError(t, err)
		sum := sha256.Sum256(bz)
		require.Equal(t, hex.EncodeToString(sum[:]), rep.GenesisSHA256)
		require.Equal(t, int64(len(bz)), rep.Sizes.File)
		var got genesis.Report
		bz, err = os.ReadFile(p.rep)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(bz, &got))
		require.Equal(t, *rep, got)
		require.Contains(t, got.Status, "trial-import")
		require.Equal(t, []string{"genesis.json"}, entries(t, filepath.Dir(p.out)))
		require.Equal(t, []string{"report.json"}, entries(t, filepath.Dir(p.rep)))
	})

	refusals := []struct {
		name string
		edit func(t *testing.T, p *paths)
		want string
	}{
		{"output exists", func(t *testing.T, p *paths) { require.NoError(t, os.WriteFile(p.out, nil, 0o600)) }, "absent"},
		{"report exists", func(t *testing.T, p *paths) { require.NoError(t, os.WriteFile(p.rep, nil, 0o600)) }, "absent"},
		{"dangling symlink output", func(t *testing.T, p *paths) { require.NoError(t, os.Symlink(filepath.Join(p.dir, "none"), p.out)) }, "absent"},
		{"input is output", func(_ *testing.T, p *paths) { p.out = p.in }, "distinct input, output and report"},
		{"output is report", func(_ *testing.T, p *paths) { p.rep = p.out }, "distinct input, output and report"},
		{"output through a symlinked directory is input", func(t *testing.T, p *paths) {
			require.NoError(t, os.Symlink(filepath.Join(p.dir, "in"), filepath.Join(p.dir, "alias")))
			p.out = filepath.Join(p.dir, "alias", "export.json")
		}, "distinct input, output and report"},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			p := setup(t)
			tc.edit(t, &p)
			before := entries(t, filepath.Join(p.dir, "out"))
			err := install(p)
			var ce *genesis.CheckError
			require.True(t, errors.As(err, &ce), "want a CheckError, got %v", err)
			require.Equal(t, tc.want, ce.Want)
			require.Equal(t, before, entries(t, filepath.Join(p.dir, "out")))
			require.Empty(t, entries(t, filepath.Join(p.dir, "rep")))
		})
	}

	failures := []struct {
		name               string
		link, sync, remove func(string) error
		leftover           string
	}{
		{name: "report link", link: nth(1)},
		{name: "report dir sync", sync: nth(1)},
		{name: "genesis link", link: nth(2)},
		{name: "genesis dir sync", sync: nth(2)},
		{name: "temp unlink", remove: nth(1)},
		{name: "after the last dir sync", sync: nth(4)},
		{
			name: "failed cleanup lists the report",
			link: nth(2),
			remove: func(path string) error {
				if filepath.Base(path) == "report.json" {
					return errInjected
				}
				return nil
			},
			leftover: "report.json",
		},
	}
	for _, tc := range failures {
		t.Run(tc.name, func(t *testing.T) {
			p := setup(t)
			pass := func(fn func(string) error) func(string) error {
				if fn == nil {
					return func(string) error { return nil }
				}
				return fn
			}
			link, sync, remove := pass(tc.link), pass(tc.sync), pass(tc.remove)
			hook(t, genesis.Link, func(orig func(string, string) error) func(string, string) error {
				return func(tmp, dst string) error {
					if err := link(dst); err != nil {
						return err
					}
					return orig(tmp, dst)
				}
			})
			hook(t, genesis.SyncDir, func(orig func(string) error) func(string) error {
				return func(dir string) error { return errors.Join(sync(dir), orig(dir)) }
			})
			hook(t, genesis.Remove, func(orig func(string) error) func(string) error {
				return func(path string) error {
					if err := remove(path); err != nil {
						return err
					}
					return orig(path)
				}
			})

			err := install(p)
			require.ErrorIs(t, err, errInjected)
			_, statErr := os.Lstat(p.out)
			require.ErrorIs(t, statErr, os.ErrNotExist)
			require.Empty(t, entries(t, filepath.Join(p.dir, "out")))
			if tc.leftover == "" {
				require.Empty(t, entries(t, filepath.Join(p.dir, "rep")))
				return
			}
			require.ErrorContains(t, err, "leftover")
			require.ErrorContains(t, err, tc.leftover)
			require.Equal(t, []string{tc.leftover}, entries(t, filepath.Join(p.dir, "rep")))
		})
	}
}
