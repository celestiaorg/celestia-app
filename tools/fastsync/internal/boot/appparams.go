package boot

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/iavl"
	idb "github.com/cosmos/iavl/db"
)

// AppParams are the consensus params the application stored at a height:
// the fields the app sets, which no header hash covers except block size and gas.
type AppParams struct {
	Block     types.BlockParams
	Evidence  types.EvidenceParams
	Validator types.ValidatorParams
	App       *uint64 // nil when the app did not store it
}

// LocalAppParams reads the consensus params of the application DB in dataDir
// at version h: x/consensus (SDK v0.50 stores, app v4+) or, for app versions
// 1 to 3, the "baseapp" subspace of x/params.
func LocalAppParams(dataDir, backend string, h int64, appVersion uint64) (*AppParams, error) {
	db, err := dbm.NewDB("application", dbm.BackendType(backend), dataDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	get := func(storeName string, keys ...string) ([][]byte, error) {
		tree := iavl.NewMutableTree(idb.NewWrapper(dbm.NewPrefixDB(db, []byte("s/k:"+storeName+"/"))), 0, true, iavl.NewNopLogger())
		it, err := tree.GetImmutable(h)
		if err != nil {
			return nil, fmt.Errorf("store %s at version %d: %w", storeName, h, err)
		}
		out := make([][]byte, len(keys))
		for i, k := range keys {
			if out[i], err = it.Get([]byte(k)); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	if appVersion >= 4 {
		v, err := get("consensus", "Consensus") // collections.Item with prefix "Consensus"
		if err != nil {
			return nil, err
		}
		if v[0] == nil {
			return nil, errors.New("x/consensus holds no params")
		}
		var pb cmtproto.ConsensusParams
		if err := pb.Unmarshal(v[0]); err != nil {
			return nil, err
		}
		p := types.ConsensusParamsFromProto(pb)
		out := &AppParams{Block: p.Block, Evidence: p.Evidence, Validator: p.Validator}
		if pb.Version != nil {
			out.App = &pb.Version.App
		}
		return out, nil
	}
	v, err := get("params", "baseapp/BlockParams", "baseapp/EvidenceParams", "baseapp/ValidatorParams", "baseapp/VersionParams")
	if err != nil {
		return nil, err
	}
	return legacyAppParams(v[0], v[1], v[2], v[3])
}

// aminoInt decodes an amino JSON integer, which is a string.
type aminoInt int64

func (a *aminoInt) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return json.Unmarshal(b, (*int64)(a))
	}
	n, err := strconv.ParseInt(s, 10, 64)
	*a = aminoInt(n)
	return err
}

// legacyAppParams decodes the amino JSON values of the x/params "baseapp" subspace.
func legacyAppParams(block, evidence, validator, ver []byte) (*AppParams, error) {
	if block == nil || evidence == nil || validator == nil {
		return nil, errors.New("x/params baseapp subspace holds no consensus params")
	}
	var b struct {
		MaxBytes aminoInt `json:"max_bytes"`
		MaxGas   aminoInt `json:"max_gas"`
	}
	var e struct {
		MaxAgeNumBlocks aminoInt `json:"max_age_num_blocks"`
		MaxAgeDuration  aminoInt `json:"max_age_duration"`
		MaxBytes        aminoInt `json:"max_bytes"`
	}
	var v struct {
		PubKeyTypes []string `json:"pub_key_types"`
	}
	var vb struct {
		App        *aminoInt `json:"app"`
		AppVersion *aminoInt `json:"app_version"`
	}
	for _, d := range []struct {
		bz  []byte
		dst any
	}{{block, &b}, {evidence, &e}, {validator, &v}, {ver, &vb}} {
		if d.bz == nil {
			continue
		}
		if err := json.Unmarshal(d.bz, d.dst); err != nil {
			return nil, fmt.Errorf("decode %s: %w", d.bz, err)
		}
	}
	out := &AppParams{
		Block:     types.BlockParams{MaxBytes: int64(b.MaxBytes), MaxGas: int64(b.MaxGas)},
		Evidence:  types.EvidenceParams{MaxAgeNumBlocks: int64(e.MaxAgeNumBlocks), MaxAgeDuration: time.Duration(e.MaxAgeDuration), MaxBytes: int64(e.MaxBytes)},
		Validator: types.ValidatorParams{PubKeyTypes: v.PubKeyTypes},
	}
	for _, a := range []*aminoInt{vb.App, vb.AppVersion} {
		if a != nil {
			app := uint64(*a)
			out.App = &app
		}
	}
	return out, nil
}

// Check reports the first field where the source's params differ from the app's.
func (a *AppParams) Check(p types.ConsensusParams) error {
	switch {
	case a.Block != p.Block:
		return fmt.Errorf("block params: app %+v, source %+v", a.Block, p.Block)
	case a.Evidence != p.Evidence:
		return fmt.Errorf("evidence params: app %+v, source %+v", a.Evidence, p.Evidence)
	case !slices.Equal(a.Validator.PubKeyTypes, p.Validator.PubKeyTypes):
		return fmt.Errorf("validator params: app %v, source %v", a.Validator.PubKeyTypes, p.Validator.PubKeyTypes)
	case a.App != nil && *a.App != p.Version.App:
		return fmt.Errorf("app version: app %d, source %d", *a.App, p.Version.App)
	}
	return nil
}
