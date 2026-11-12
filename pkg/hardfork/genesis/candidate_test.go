package genesis_test

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/pkg/hardfork/genesis"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	minttypes "github.com/celestiaorg/celestia-app/v11/x/mint/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"
)

func TestOptionsRejects(t *testing.T) {
	f := newFixture(t)
	exportTime := util.GenesisTime
	options := func(field, want string) genesis.CheckError {
		return genesis.CheckError{Step: "options", Field: field, Want: want}
	}
	chainIDWant := "at most 50 bytes and not mocha-4"
	utc3 := time.FixedZone("UTC+3", 3*3600)
	editMint := func(fn func(gs *minttypes.GenesisState)) func(t *testing.T, g *genutiltypes.AppGenesis) {
		return func(t *testing.T, g *genutiltypes.AppGenesis) {
			var state map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(g.AppState, &state))
			var mint minttypes.GenesisState
			require.NoError(t, f.cdc.UnmarshalJSON(state[minttypes.ModuleName], &mint))
			fn(&mint)
			var err error
			state[minttypes.ModuleName], err = f.cdc.MarshalJSON(&mint)
			require.NoError(t, err)
			g.AppState, err = json.Marshal(state)
			require.NoError(t, err)
		}
	}

	testCases := []struct {
		name string
		edit func(t *testing.T, g *genutiltypes.AppGenesis)
		opts func(o *genesis.Options)
		want *genesis.CheckError // nil means accepted
	}{
		{
			name: "G before T",
			opts: func(o *genesis.Options) { o.GenesisTime = exportTime.Add(-time.Nanosecond) },
			want: &genesis.CheckError{Step: "options", Field: "genesis-time", Want: ">= " + exportTime.Format(time.RFC3339Nano)},
		},
		{
			name: "G equal to T accepted",
			opts: func(o *genesis.Options) { o.GenesisTime = exportTime },
		},
		{
			name: "same chain-id at E+11 with a non-UTC G accepted",
			opts: func(o *genesis.Options) { o.GenesisTime = exportTime.Add(time.Hour).In(utc3) },
		},
		{
			name: "new chain-id at E+1 with a non-UTC G accepted",
			opts: func(o *genesis.Options) {
				o.ChainID = "other"
				o.InitialHeight = 2
				o.GenesisTime = exportTime.Add(time.Hour).In(utc3)
			},
		},
		{
			name: "mint without minter and genesis_time",
			edit: editMint(func(gs *minttypes.GenesisState) { gs.Minter, gs.GenesisTime = nil, nil }),
			want: &genesis.CheckError{Step: "options", Module: "mint", Field: "minter and genesis_time", Want: "set"},
		},
		{
			name: "mint genesis_time after G",
			edit: editMint(func(gs *minttypes.GenesisState) {
				later := exportTime.Add(time.Hour + time.Nanosecond)
				gs.GenesisTime = &later
			}),
			want: &genesis.CheckError{Step: "options", Module: "mint", Field: "genesis_time", Want: "<= " + exportTime.Add(time.Hour).Format(time.RFC3339Nano)},
		},
		{
			name: "initial height below E+1",
			opts: func(o *genesis.Options) {
				o.ChainID = "other"
				o.InitialHeight = 1
			},
			want: new(options("initial-height", ">= 2")),
		},
		{
			name: "same chain-id at E+10",
			opts: func(o *genesis.Options) { o.InitialHeight = 11 },
			want: new(options("initial-height", ">= 12")),
		},
		{
			name: "renaming into a known network",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.ChainID = "x" },
			opts: func(o *genesis.Options) {
				o.ChainID = "celestia"
				o.InitialHeight = 2
			},
			want: new(options("chain-id", "no renaming into a known network")),
		},
		{
			name: "known network kept at E+11 accepted",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.ChainID = "celestia" },
			opts: func(o *genesis.Options) { o.ChainID = "celestia" },
		},
		{
			name: "initial height at E+1_000_000 accepted",
			opts: func(o *genesis.Options) { o.InitialHeight = 1 + 1_000_000 },
		},
		{
			name: "initial height at E+1_000_001",
			opts: func(o *genesis.Options) { o.InitialHeight = 1 + 1_000_001 },
			want: new(options("initial-height", "<= 1000001")),
		},
		{
			name: "E = MaxInt64-5 with the same chain-id",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.InitialHeight = math.MaxInt64 - 4 },
			opts: func(o *genesis.Options) {
				o.ExportHeight = math.MaxInt64 - 5
				o.InitialHeight = math.MaxInt64
			},
			want: new(options("initial-height", fmt.Sprintf(">= %d+11", int64(math.MaxInt64-5)))),
		},
		{
			name: "E = MaxInt64-11 at MaxInt64",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.InitialHeight = math.MaxInt64 - 10 },
			opts: func(o *genesis.Options) {
				o.ExportHeight = math.MaxInt64 - 11
				o.InitialHeight = math.MaxInt64
			},
			want: new(options("initial-height", fmt.Sprintf("<= %d", int64(math.MaxInt64-1_000_000)))),
		},
		{
			name: "long chain-id",
			opts: func(o *genesis.Options) { o.ChainID = strings.Repeat("a", 51) },
			want: new(options("chain-id", chainIDWant)),
		},
		{
			name: "invalid UTF-8 chain-id that encodes as the export's",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.ChainID = "\uFFFD" },
			opts: func(o *genesis.Options) {
				o.ChainID = "\xff"
				o.InitialHeight = 2
			},
			want: new(options("chain-id", "valid UTF-8")),
		},
		{
			name: "invalid UTF-8 chain-id that encodes longer than 50 bytes",
			opts: func(o *genesis.Options) {
				o.ChainID = strings.Repeat("\xff", 17)
				o.InitialHeight = 2
			},
			want: new(options("chain-id", "valid UTF-8")),
		},
		{
			name: "mocha-4",
			opts: func(o *genesis.Options) {
				o.ChainID = "mocha-4"
				o.InitialHeight = 2
			},
			want: new(options("chain-id", chainIDWant)),
		},
		{
			name: "missing T",
			opts: func(o *genesis.Options) { o.ExportBlockTime = time.Time{} },
			want: new(options("export-block-time", "set")),
		},
		{
			name: "missing G",
			opts: func(o *genesis.Options) { o.GenesisTime = time.Time{} },
			want: new(options("genesis-time", "set")),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			file := f
			if tc.edit != nil {
				g := f.decode(t)
				tc.edit(t, g)
				file.file = encode(t, g)
			}
			opts := file.options()
			if tc.opts != nil {
				tc.opts(&opts)
			}
			exp, err := genesis.LoadExport(bytes.NewReader(file.file), f.modules, opts)
			require.NoError(t, err)

			cand, err := genesis.NewCandidate(f.cdc, exp, opts)
			if tc.want != nil {
				requireCheckError(t, err, *tc.want, "")
				return
			}
			require.NoError(t, err)
			require.Equal(t, time.UTC, cand.GenesisTime.Location())
			require.True(t, cand.GenesisTime.Equal(opts.GenesisTime))
			// Only the three outer fields differ; module payloads are byte-equal.
			want := exp.Genesis
			want.ChainID, want.InitialHeight, want.GenesisTime = cmp.Or(opts.ChainID, exp.ChainID), opts.InitialHeight, cand.GenesisTime
			require.Equal(t, want, *cand)

			// Mutating the candidate's consensus leaves the export unchanged.
			v := cand.Consensus.Validators[0]
			v.Address[0] ^= 1
			v.PubKey.Bytes()[0] ^= 1
			cand.Consensus.Params.Validator.PubKeyTypes[0] = "x"
			require.Equal(t, file.decode(t).Consensus, exp.Consensus)
		})
	}
}
