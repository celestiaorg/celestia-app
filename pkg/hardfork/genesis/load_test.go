package genesis_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/hardfork/genesis"
	"github.com/celestiaorg/celestia-app/v11/test/util"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cosmos/cosmos-sdk/codec"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"
)

// fixture is a real in-process export at E = 1.
type fixture struct {
	cdc     codec.Codec
	modules []string
	file    []byte
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	testApp, _ := util.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	exported, err := testApp.ExportAppStateAndValidators(false, nil, nil)
	require.NoError(t, err)

	// Wrapped as the SDK export command does, with non-empty app metadata.
	g := &genutiltypes.AppGenesis{
		AppName:       "celestia-appd",
		AppVersion:    "v0.0.0-test",
		GenesisTime:   util.GenesisTime,
		ChainID:       util.ChainID,
		InitialHeight: exported.Height,
		AppState:      exported.AppState,
		Consensus:     genutiltypes.NewConsensusGenesis(exported.ConsensusParams, exported.Validators),
	}
	return fixture{cdc: testApp.AppCodec(), modules: genesis.ExportedModules(testApp.ModuleManager), file: encode(t, g)}
}

func (f fixture) decode(t *testing.T) *genutiltypes.AppGenesis {
	t.Helper()
	var g genutiltypes.AppGenesis
	require.NoError(t, json.Unmarshal(f.file, &g))
	return &g
}

func (f fixture) options() genesis.Options {
	sum := sha256.Sum256(f.file)
	return genesis.Options{
		ExportSHA256:    hex.EncodeToString(sum[:]),
		ExportHeight:    1,
		ExportBlockTime: util.GenesisTime,
		GenesisTime:     util.GenesisTime.Add(time.Hour),
		InitialHeight:   12,
	}
}

func encode(t *testing.T, g *genutiltypes.AppGenesis) []byte {
	t.Helper()
	bz, err := json.Marshal(g)
	require.NoError(t, err)
	return append(bz, '\n')
}

// modules returns the app_state entries sorted by name.
func modules(t *testing.T, g *genutiltypes.AppGenesis) []genesis.Module {
	t.Helper()
	var state map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(g.AppState, &state))
	mods := make([]genesis.Module, 0, len(state))
	for name, raw := range state {
		mods = append(mods, genesis.Module{Name: name, Raw: raw})
	}
	slices.SortFunc(mods, func(a, b genesis.Module) int { return strings.Compare(a.Name, b.Name) })
	return mods
}

// setModules writes mods into app_state in the given order.
func setModules(t *testing.T, g *genutiltypes.AppGenesis, mods []genesis.Module) {
	t.Helper()
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range mods {
		if i > 0 {
			b.WriteByte(',')
		}
		name, err := json.Marshal(m.Name)
		require.NoError(t, err)
		b.Write(name)
		b.WriteByte(':')
		b.Write(m.Raw)
	}
	b.WriteByte('}')
	g.AppState = b.Bytes()
}

// nested returns a JSON array value of the given depth.
func nested(depth int) string {
	return strings.Repeat("[", depth) + strings.Repeat("]", depth)
}

// withVesting replaces the empty vesting payload with raw.
func (f fixture) withVesting(t *testing.T, raw string) []byte {
	t.Helper()
	require.Equal(t, 1, bytes.Count(f.file, []byte(`"vesting":{}`)))
	return bytes.Replace(f.file, []byte(`"vesting":{}`), []byte(`"vesting":`+raw), 1)
}

// withConsensusField adds an unknown field to consensus, so its depth is the value's + 1.
func (f fixture) withConsensusField(t *testing.T, raw string) []byte {
	t.Helper()
	old := []byte(`"consensus":{"validators":`)
	require.Equal(t, 1, bytes.Count(f.file, old))
	return bytes.Replace(f.file, old, []byte(`"consensus":{"x":`+raw+`,"validators":`), 1)
}

// replace replaces the single occurrence of old in the export file.
func (f fixture) replace(t *testing.T, old, repl string) []byte {
	t.Helper()
	require.Equal(t, 1, bytes.Count(f.file, []byte(old)))
	return bytes.Replace(f.file, []byte(old), []byte(repl), 1)
}

// errAtEnd returns the last bytes of data together with err, then EOF.
type errAtEnd struct {
	data []byte
	err  error
}

func (r *errAtEnd) Read(p []byte) (int, error) {
	n := copy(p, r.data)
	r.data = r.data[n:]
	if len(r.data) > 0 {
		return n, nil
	}
	err := r.err
	r.err = io.EOF
	return n, err
}

// requireCheckError compares err with want, except that Got only has to contain got.
func requireCheckError(t *testing.T, err error, want genesis.CheckError, got string) {
	t.Helper()
	var ce *genesis.CheckError
	require.True(t, errors.As(err, &ce), "want a CheckError, got %v", err)
	require.Contains(t, ce.Got, got, ce.Error())
	require.NotContains(t, ce.Error(), "\x1b")
	require.Less(t, len(ce.Error()), 1024)
	actual := *ce
	actual.Got = ""
	require.Equal(t, want, actual, ce.Error())
}

func inputErr(field, want string) genesis.CheckError {
	return genesis.CheckError{Step: "input", Field: field, Want: want}
}

func TestLoadExportRejects(t *testing.T) {
	f := newFixture(t)
	other := sha256.Sum256([]byte("other"))
	longKey := "\x1b[31m" + strings.Repeat("a", 4096)
	appState := func(module, want string) genesis.CheckError {
		return genesis.CheckError{Step: "load", Module: module, Field: "app_state", Want: want}
	}

	runLoadCases(t, f, []loadCase{
		{
			name: "hash mismatch",
			opts: func(o *genesis.Options) { o.ExportSHA256 = hex.EncodeToString(other[:]) },
			want: genesis.CheckError{Step: "load", Field: "export-sha256", Want: hex.EncodeToString(other[:])},
		},
		{
			name: "trailing data",
			file: func(*testing.T) []byte { return append(slices.Clone(f.file), "{}"...) },
			want: genesis.CheckError{Step: "load", Want: "end of file"},
			got:  "trailing data",
		},
		{
			name: "trailing non-JSON",
			file: func(*testing.T) []byte { return append(slices.Clone(f.file), "x"...) },
			want: genesis.CheckError{Step: "load"},
			got:  "invalid character",
		},
		{
			name: "malformed JSON",
			file: func(*testing.T) []byte { return []byte(`{"chain_id":"test-app",}`) },
			want: genesis.CheckError{Step: "load"},
			got:  "invalid character",
		},
		{
			name: "long key with an escape character",
			file: func(*testing.T) []byte { return []byte(`{"` + strings.ReplaceAll(longKey, "\x1b", `\u001b`) + `":1}`) },
			want: genesis.CheckError{Step: "load", Field: longKey, Want: "an AppGenesis key"},
			got:  "unknown key",
			text: `"\x1b[31maaa`,
		},
		{
			name: "long key cut at a rune boundary",
			file: func(*testing.T) []byte { return []byte(`{"` + strings.Repeat("a", 127) + `é":1}`) },
			want: genesis.CheckError{Step: "load", Field: strings.Repeat("a", 127) + "é", Want: "an AppGenesis key"},
			got:  "unknown key",
			text: `"` + strings.Repeat("a", 127) + `"...`,
		},
		{
			name: "joined EOF error with the last bytes",
			reader: func(data []byte) io.Reader {
				return &errAtEnd{data: data, err: errors.Join(io.EOF, io.ErrUnexpectedEOF)}
			},
			want: genesis.CheckError{Step: "load"},
			got:  "unexpected EOF",
		},
		{
			name: "joined EOF error after the document",
			reader: func(data []byte) io.Reader {
				return io.MultiReader(bytes.NewReader(data), &errAtEnd{err: errors.Join(io.EOF, io.ErrUnexpectedEOF)})
			},
			want: genesis.CheckError{Step: "load"},
			got:  "unexpected EOF",
		},
		{
			name:   "plain EOF with the last bytes accepted",
			reader: func(data []byte) io.Reader { return &errAtEnd{data: data, err: io.EOF} },
		},
		{
			name: "large integer and escaped string kept byte for byte",
			file: func(t *testing.T) []byte { return f.withVesting(t, `{"n":9007199254740993,"s":"\"\\"}`) },
		},
		{
			name: "brackets in strings do not count toward depth",
			file: func(t *testing.T) []byte { return f.withVesting(t, nested(9998)[:9998]+`"\"[{\\"`+nested(9998)[9998:]) },
		},
		{
			name: "consensus depth 10000 passes the depth check",
			file: func(t *testing.T) []byte { return f.withConsensusField(t, nested(9998)) },
			want: genesis.CheckError{Step: "load", Field: `consensus."x"`, Want: "a known field"},
			got:  "unknown field",
		},
		{
			name: "consensus key in another case",
			file: func(t *testing.T) []byte {
				return f.replace(t, `"consensus":{"validators":`, `"consensus":{"Validators":`)
			},
			want: genesis.CheckError{Step: "load", Field: `consensus."Validators"`, Want: "a known field"},
			got:  "unknown field",
		},
		{
			name: "unknown nested consensus field",
			file: func(t *testing.T) []byte {
				return f.replace(t, `"params":{"block":{"max_bytes":`, `"params":{"block":{"x":"1","max_bytes":`)
			},
			want: genesis.CheckError{Step: "load", Field: `consensus."params"."block"."x"`, Want: "a known field"},
			got:  "unknown field",
		},
		{
			name: "repeated nested consensus key",
			file: func(t *testing.T) []byte {
				return f.replace(t, `"params":{"block":{"max_bytes":`, `"params":{"block":{"max_gas":"1","max_bytes":`)
			},
			want: genesis.CheckError{Step: "load", Field: `consensus."params"."block"."max_gas"`, Want: "one occurrence"},
			got:  "repeated key",
		},
		{
			name: "consensus depth 10001",
			file: func(t *testing.T) []byte { return f.withConsensusField(t, nested(9999)) },
			want: genesis.CheckError{Step: "load", Field: "consensus"},
			got:  "exceeded max depth",
		},
		{
			name: "document depth 10001",
			file: func(t *testing.T) []byte { return f.withVesting(t, nested(9999)) },
			want: genesis.CheckError{Step: "load", Field: "app_state"},
			got:  "exceeded max depth",
		},
		{
			name: "missing app_state",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.AppState = nil },
			want: genesis.CheckError{Step: "load", Field: "app_state", Want: "present"},
		},
		{
			name: "CometBFT v1 doc",
			file: func(t *testing.T) []byte {
				doc, err := f.decode(t).ToGenesisDoc()
				require.NoError(t, err)
				bz, err := cmtjson.Marshal(doc)
				require.NoError(t, err)
				return bz
			},
			want: genesis.CheckError{Step: "load", Field: "initial_height"},
			got:  "cannot unmarshal string",
		},
		{
			name: "initial_height 0",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.InitialHeight = 0 },
			want: inputErr("initial_height", ">= 2"),
		},
		{
			name: "initial_height 1",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.InitialHeight = 1 },
			want: inputErr("initial_height", ">= 2"),
		},
		{
			name: "export height mismatch",
			opts: func(o *genesis.Options) { o.ExportHeight = 2 },
			want: inputErr("export-height", "1"),
		},
		{
			name: "missing consensus",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.Consensus = nil },
			want: inputErr("consensus", "present"),
		},
		{
			name: "missing params",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.Consensus.Params = nil },
			want: inputErr("consensus.params", "present"),
		},
		{
			name: "app version 0",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.Consensus.Params.Version.App = 0 },
			want: inputErr("consensus.params.version.app", fmt.Sprint(appconsts.Version)),
		},
		{
			name: "next app version",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.Consensus.Params.Version.App = appconsts.Version + 1 },
			want: inputErr("consensus.params.version.app", fmt.Sprint(appconsts.Version)),
		},
		{
			name: "invalid consensus params",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.Consensus.Params.Evidence.MaxAgeNumBlocks = 0 },
			want: inputErr("consensus.params", "valid consensus params"),
			got:  "MaxAgeNumBlocks",
		},
		{
			name: "empty validators",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.Consensus.Validators = nil },
			want: inputErr("consensus.validators", "non-empty"),
		},
		{
			name: "zero power",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.Consensus.Validators[0].Power = 0 },
			want: inputErr("consensus.validators[0].power", "> 0"),
		},
		{
			name: "non-empty app_hash",
			edit: func(_ *testing.T, g *genutiltypes.AppGenesis) { g.AppHash = []byte{1} },
			want: inputErr("app_hash", "empty"),
		},
		{
			name: "duplicate module key",
			edit: func(t *testing.T, g *genutiltypes.AppGenesis) {
				mods := modules(t, g)
				setModules(t, g, slices.Insert(mods, 1, mods[0]))
			},
			want: appState(f.modules[0], "a key after "+f.modules[0]),
		},
		{
			name: "unknown module key",
			edit: func(t *testing.T, g *genutiltypes.AppGenesis) {
				setModules(t, g, append(modules(t, g), genesis.Module{Name: "zzz", Raw: json.RawMessage(`{}`)}))
			},
			want: appState("zzz", "a known module"),
		},
		{
			name: "missing module key",
			edit: func(t *testing.T, g *genutiltypes.AppGenesis) {
				setModules(t, g, slices.DeleteFunc(modules(t, g), func(m genesis.Module) bool { return m.Name == "mint" }))
			},
			want: appState("mint", "present"),
		},
		{
			name: "unsorted module keys",
			edit: func(t *testing.T, g *genutiltypes.AppGenesis) {
				mods := modules(t, g)
				mods[0], mods[1] = mods[1], mods[0]
				setModules(t, g, mods)
			},
			want: appState(f.modules[0], "a key after "+f.modules[1]),
		},
		{
			name: "unknown module key with the next app version",
			edit: func(t *testing.T, g *genutiltypes.AppGenesis) {
				setModules(t, g, append(modules(t, g), genesis.Module{Name: "zzz", Raw: json.RawMessage(`{}`)}))
				g.Consensus.Params.Version.App = appconsts.Version + 1
			},
			want: inputErr("consensus.params.version.app", fmt.Sprint(appconsts.Version)),
			got:  "(use the v",
		},
		{
			name: "missing module key with the next app version",
			edit: func(t *testing.T, g *genutiltypes.AppGenesis) {
				setModules(t, g, slices.DeleteFunc(modules(t, g), func(m genesis.Module) bool { return m.Name == "mint" }))
				g.Consensus.Params.Version.App = appconsts.Version + 1
			},
			want: inputErr("consensus.params.version.app", fmt.Sprint(appconsts.Version)),
			got:  "(use the v",
		},
		{
			name: "unsorted module keys with the next app version",
			edit: func(t *testing.T, g *genutiltypes.AppGenesis) {
				mods := modules(t, g)
				mods[0], mods[1] = mods[1], mods[0]
				setModules(t, g, mods)
				g.Consensus.Params.Version.App = appconsts.Version + 1
			},
			want: appState(f.modules[0], "a key after "+f.modules[1]),
		},
	})
}

func TestLoadExportFile(t *testing.T) {
	f := newFixture(t)
	path := filepath.Join(t.TempDir(), "export.json")
	require.NoError(t, os.WriteFile(path, f.file, 0o600))
	exp, err := genesis.LoadExportFile(path, f.modules, f.options())
	require.NoError(t, err)
	want, err := genesis.LoadExport(bytes.NewReader(f.file), f.modules, f.options())
	require.NoError(t, err)
	require.Equal(t, want, exp)

	// A non-JSON file fails on the hash, so it is never parsed.
	require.NoError(t, os.WriteFile(path, []byte("not JSON"), 0o600))
	_, err = genesis.LoadExportFile(path, f.modules, f.options())
	requireCheckError(t, err, genesis.CheckError{Step: "load", Field: "export-sha256", Want: f.options().ExportSHA256}, "")
}

// loadCase is an export edit that LoadExport must reject with want, or accept when want is zero.
type loadCase struct {
	name   string
	file   func(t *testing.T) []byte
	edit   func(t *testing.T, g *genutiltypes.AppGenesis)
	reader func(data []byte) io.Reader
	opts   func(o *genesis.Options)
	want   genesis.CheckError
	got    string
	text   string
}

func runLoadCases(t *testing.T, f fixture, testCases []loadCase) {
	t.Helper()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			file := f
			switch {
			case tc.file != nil:
				file.file = tc.file(t)
			case tc.edit != nil:
				g := f.decode(t)
				tc.edit(t, g)
				file.file = encode(t, g)
			}
			opts := file.options()
			if tc.opts != nil {
				tc.opts(&opts)
			}

			var r io.Reader = bytes.NewReader(file.file)
			if tc.reader != nil {
				r = tc.reader(file.file)
			}
			exp, err := genesis.LoadExport(r, f.modules, opts)
			if tc.want == (genesis.CheckError{}) {
				require.NoError(t, err)
				// The payloads equal the source bytes extracted without LoadExport.
				require.Equal(t, modules(t, file.decode(t)), exp.Modules)
				return
			}
			requireCheckError(t, err, tc.want, tc.got)
			require.Contains(t, err.Error(), tc.text)
		})
	}
}
