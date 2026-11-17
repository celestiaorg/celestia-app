package genesis

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	minttypes "github.com/celestiaorg/celestia-app/v11/x/mint/types"
	cryptoenc "github.com/cometbft/cometbft/crypto/encoding"
	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/x/authz"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
)

const (
	stepOptions = "options"
	// sameChainIDGap is the minimum initial height above E for a kept chain-id.
	sameChainIDGap = 11
	// maxHeightGap is the maximum initial height above E.
	maxHeightGap = 1_000_000
	// maxInitialHeight leaves headroom for the heights core derives from it.
	maxInitialHeight = math.MaxInt64 - 1_000_000
	// legacyMochaChainID is classified as a legacy genesis by the multiplexer.
	legacyMochaChainID = "mocha-4"
)

// NewCandidate checks opts and returns a copy of exp with the three outer fields set.
func NewCandidate(cdc codec.JSONCodec, exp *Export, opts Options) (*Genesis, error) {
	if opts.ExportBlockTime.IsZero() {
		return nil, &CheckError{Step: stepOptions, Field: "export-block-time", Want: "set", Got: "missing"}
	}
	if opts.GenesisTime.IsZero() {
		return nil, &CheckError{Step: stepOptions, Field: "genesis-time", Want: "set", Got: "missing"}
	}
	blockTime, genesisTime := opts.ExportBlockTime.UTC(), opts.GenesisTime.UTC()
	if genesisTime.Before(blockTime) {
		return nil, &CheckError{Step: stepOptions, Field: "genesis-time", Want: ">= " + formatTime(blockTime), Got: formatTime(genesisTime)}
	}
	if err := checkMintTime(cdc, &exp.Genesis, genesisTime); err != nil {
		return nil, err
	}
	if err := checkAuthz(cdc, &exp.Genesis, genesisTime); err != nil {
		return nil, err
	}

	chainID := opts.ChainID
	if chainID == "" {
		chainID = exp.ChainID
	}
	// JSON encoding would replace invalid bytes and change the chain-id.
	if !utf8.ValidString(chainID) {
		return nil, &CheckError{Step: stepOptions, Field: "chain-id", Want: "valid UTF-8", Got: chainID}
	}
	// Fibre payment promises carry the chain-id and must match it.
	if len(chainID) > fibretypes.MaxChainIDSize || chainID == legacyMochaChainID {
		return nil, &CheckError{Step: stepOptions, Field: "chain-id", Want: fmt.Sprintf("at most %d bytes and not %s", fibretypes.MaxChainIDSize, legacyMochaChainID), Got: chainID}
	}
	knownNetwork := slices.Contains(appconsts.PublicNetworks, chainID) || chainID == appconsts.CortoChainID
	if knownNetwork && chainID != exp.ChainID {
		return nil, &CheckError{Step: stepOptions, Field: "chain-id", Want: "no renaming into a known network", Got: chainID}
	}

	// LoadExport guarantees InitialHeight >= 2, so E >= 1.
	exportHeight := exp.InitialHeight - 1
	gap := int64(1)
	if chainID == exp.ChainID {
		gap = sameChainIDGap
	}
	if exportHeight > math.MaxInt64-gap {
		return nil, &CheckError{Step: stepOptions, Field: "initial-height", Want: fmt.Sprintf(">= %d+%d", exportHeight, gap), Got: "beyond int64"}
	}
	if minHeight := exportHeight + gap; opts.InitialHeight < minHeight {
		return nil, &CheckError{Step: stepOptions, Field: "initial-height", Want: fmt.Sprintf(">= %d", minHeight), Got: fmt.Sprint(opts.InitialHeight)}
	}
	if exportHeight <= math.MaxInt64-maxHeightGap && opts.InitialHeight > exportHeight+maxHeightGap {
		return nil, &CheckError{Step: stepOptions, Field: "initial-height", Want: fmt.Sprintf("<= %d", exportHeight+maxHeightGap), Got: fmt.Sprint(opts.InitialHeight)}
	}
	if opts.InitialHeight > maxInitialHeight {
		return nil, &CheckError{Step: stepOptions, Field: "initial-height", Want: fmt.Sprintf("<= %d", int64(maxInitialHeight)), Got: fmt.Sprint(opts.InitialHeight)}
	}

	cand := exp.Genesis
	cand.GenesisTime, cand.ChainID, cand.InitialHeight = genesisTime, chainID, opts.InitialHeight
	cand.Modules = slices.Clone(exp.Modules)
	params := *exp.Consensus.Params
	params.Validator.PubKeyTypes = slices.Clone(params.Validator.PubKeyTypes)
	vals := make([]cmttypes.GenesisValidator, len(exp.Consensus.Validators))
	for i, v := range exp.Consensus.Validators {
		// The proto round trip copies the key bytes.
		proto, err := cryptoenc.PubKeyToProto(v.PubKey)
		if err == nil {
			v.PubKey, err = cryptoenc.PubKeyFromProto(proto)
		}
		if err != nil {
			return nil, &CheckError{Step: stepOptions, Field: fmt.Sprintf("consensus.validators[%d].pub_key", i), Got: err.Error()}
		}
		v.Address = bytes.Clone(v.Address)
		vals[i] = v
	}
	cand.Consensus = &genutiltypes.ConsensusGenesis{Validators: vals, Params: &params}
	return &cand, nil
}

// checkMintTime requires the exported minter and enforces mint's InitGenesis time bound.
func checkMintTime(cdc codec.JSONCodec, g *Genesis, genesisTime time.Time) error {
	raw, ok := g.Module(minttypes.ModuleName)
	if !ok {
		return &CheckError{Step: stepOptions, Module: minttypes.ModuleName, Want: "present", Got: "missing"}
	}
	var gs minttypes.GenesisState
	if err := cdc.UnmarshalJSON(raw, &gs); err != nil {
		return &CheckError{Step: stepOptions, Module: minttypes.ModuleName, Got: err.Error()}
	}
	if gs.Minter == nil || gs.GenesisTime == nil {
		return &CheckError{Step: stepOptions, Module: minttypes.ModuleName, Field: "minter and genesis_time", Want: "set", Got: "export without the mint export fix"}
	}
	if gs.GenesisTime.After(genesisTime) {
		return &CheckError{Step: stepOptions, Module: minttypes.ModuleName, Field: "genesis_time", Want: "<= " + formatTime(genesisTime), Got: formatTime(*gs.GenesisTime)}
	}
	return nil
}

// checkAuthz rejects grants that authz InitGenesis fails on: one expiring exactly
// at G, as it skips only grants expiring before G, an invalid address or a
// missing authorization.
func checkAuthz(cdc codec.JSONCodec, g *Genesis, genesisTime time.Time) error {
	var gs authz.GenesisState
	if err := decodeModule(cdc, g, authz.ModuleName, &gs); err != nil {
		return err
	}
	for i, a := range gs.Authorization {
		if a.Expiration != nil && a.Expiration.Equal(genesisTime) {
			return &CheckError{Step: stepOptions, Module: authz.ModuleName, Field: fmt.Sprintf("authorization[%d].expiration", i), Want: "not genesis-time " + formatTime(genesisTime) + "; choose another genesis-time", Got: fmt.Sprintf("granter %s, grantee %s", a.Granter, a.Grantee)}
		}
		if _, err := decodeAddr(accCodec, nil, authz.ModuleName, "authorization", i, "granter", a.Granter); err != nil {
			return err
		}
		if _, err := decodeAddr(accCodec, nil, authz.ModuleName, "authorization", i, "grantee", a.Grantee); err != nil {
			return err
		}
		if _, err := (authz.Grant{Authorization: a.Authorization}).GetAuthorization(); err != nil {
			return &CheckError{Step: stepRecords, Module: authz.ModuleName, Field: fmt.Sprintf("authorization[%d].authorization", i), Want: "an authorization", Got: err.Error()}
		}
	}
	return nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
