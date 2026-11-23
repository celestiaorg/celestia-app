package genesis

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"

	abci "github.com/cometbft/cometbft/abci/types"
	cryptoenc "github.com/cometbft/cometbft/crypto/encoding"
	cmtprotocrypto "github.com/cometbft/cometbft/proto/tendermint/crypto"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/types/module"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
)

const stepValidate = "validate"

// Validate runs the static checks on a candidate: the SDK and CometBFT completions change nothing, each module's
// ValidateGenesis passes in module name order, and consensus.validators equals the set staking returns from InitChain.
// Run CheckState first: it rejects the gov nulls and malformed records that ValidateGenesis may panic on.
func Validate(cdc codec.JSONCodec, txCfg client.TxEncodingConfig, mbm module.BasicManager, g *Genesis) error {
	if err := checkComplete(g); err != nil {
		return err
	}
	// BasicManager.ValidateGenesis iterates a map, so the first error could differ between runs.
	for _, name := range slices.Sorted(maps.Keys(mbm)) {
		if err := validateModule(cdc, txCfg, mbm[name], g); err != nil {
			return err
		}
	}
	return checkValidatorSet(cdc, g)
}

// appGenesis returns the outer fields of g with its own validator slice; app_state is left out, as no completion reads it.
func appGenesis(g *Genesis) *genutiltypes.AppGenesis {
	return &genutiltypes.AppGenesis{
		AppName:       g.AppName,
		AppVersion:    g.AppVersion,
		GenesisTime:   g.GenesisTime,
		ChainID:       g.ChainID,
		InitialHeight: g.InitialHeight,
		AppHash:       g.AppHash,
		Consensus:     &genutiltypes.ConsensusGenesis{Validators: slices.Clone(g.Consensus.Validators), Params: g.Consensus.Params},
	}
}

// checkComplete requires AppGenesis.ValidateAndComplete and GenesisDoc.ValidateAndComplete to pass and fill nothing.
func checkComplete(g *Genesis) error {
	ag := appGenesis(g)
	err := ag.ValidateAndComplete()
	if err == nil && !reflect.DeepEqual(ag, appGenesis(g)) {
		err = errors.New("filled a default")
	}
	if err != nil {
		return &CheckError{Step: stepValidate, Field: "AppGenesis.ValidateAndComplete", Want: "pass with no change", Got: err.Error()}
	}
	doc, err := appGenesis(g).ToGenesisDoc()
	if err == nil {
		err = doc.ValidateAndComplete()
	}
	if err == nil {
		want, _ := appGenesis(g).ToGenesisDoc()
		if !reflect.DeepEqual(doc, want) {
			err = errors.New("filled a default")
		}
	}
	if err != nil {
		return &CheckError{Step: stepValidate, Field: "GenesisDoc.ValidateAndComplete", Want: "pass with no change", Got: err.Error()}
	}
	return nil
}

// validateModule runs the ValidateGenesis of one module; recover is acceptable in this offline tool, as in decodeModule.
func validateModule(cdc codec.JSONCodec, txCfg client.TxEncodingConfig, b module.AppModuleBasic, g *Genesis) (err error) {
	mod, ok := b.(module.HasGenesisBasics)
	if !ok {
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = &CheckError{Step: stepValidate, Module: b.Name(), Want: "a valid genesis", Got: "panic in ValidateGenesis"}
		}
	}()
	raw, _ := g.Module(b.Name())
	if err := mod.ValidateGenesis(cdc, txCfg, raw); err != nil {
		return &CheckError{Step: stepValidate, Module: b.Name(), Want: "a valid genesis", Got: err.Error()}
	}
	return nil
}

// checkValidatorSet compares consensus.validators with what exported staking returns from InitGenesis, as baseapp
// InitChain does: the consensus key of each last_validator_powers entry with its power.
func checkValidatorSet(cdc codec.JSONCodec, g *Genesis) error {
	const module = stakingtypes.ModuleName
	var gs stakingtypes.GenesisState
	if err := decodeModule(cdc, g, module, &gs); err != nil {
		return err
	}
	keys := make(map[string]cmtprotocrypto.PublicKey, len(gs.Validators))
	for i, v := range gs.Validators {
		op, err := decodeAddr(valCodec, nil, module, "validators", i, "operator_address", v.OperatorAddress)
		if err != nil {
			return err
		}
		if v.ConsensusPubkey == nil {
			return &CheckError{Step: stepValidate, Module: module, Field: fmt.Sprintf("validators[%d].consensus_pubkey", i), Want: "set", Got: "null"}
		}
		if keys[op], err = v.CmtConsPublicKey(); err != nil {
			return &CheckError{Step: stepValidate, Module: module, Field: fmt.Sprintf("validators[%d].consensus_pubkey", i), Want: "a consensus key", Got: err.Error()}
		}
	}
	staking := make([]abci.ValidatorUpdate, 0, len(gs.LastValidatorPowers))
	for i, lv := range gs.LastValidatorPowers {
		op, err := decodeAddr(valCodec, nil, module, "last_validator_powers", i, "address", lv.Address)
		if err != nil {
			return err
		}
		pk, ok := keys[op]
		if !ok {
			return &CheckError{Step: stepValidate, Module: module, Field: fmt.Sprintf("last_validator_powers[%d].address", i), Want: "a validator", Got: lv.Address}
		}
		staking = append(staking, abci.ValidatorUpdate{PubKey: pk, Power: lv.Power})
	}
	consensus := make([]abci.ValidatorUpdate, 0, len(g.Consensus.Validators))
	for i, v := range g.Consensus.Validators {
		pk, err := cryptoenc.PubKeyToProto(v.PubKey)
		if err != nil {
			return &CheckError{Step: stepValidate, Field: fmt.Sprintf("consensus.validators[%d].pub_key", i), Got: err.Error()}
		}
		consensus = append(consensus, abci.ValidatorUpdate{PubKey: pk, Power: v.Power})
	}
	byKey := func(a, b abci.ValidatorUpdate) int {
		return cmp.Or(a.PubKey.Compare(b.PubKey), cmp.Compare(a.Power, b.Power))
	}
	slices.SortFunc(staking, byKey)
	slices.SortFunc(consensus, byKey)
	if len(staking) != len(consensus) {
		return &CheckError{Step: stepValidate, Field: "consensus.validators", Want: fmt.Sprintf("%d validators as in staking", len(staking)), Got: fmt.Sprint(len(consensus))}
	}
	for i := range staking {
		if !proto.Equal(&staking[i], &consensus[i]) {
			return &CheckError{Step: stepValidate, Field: "consensus.validators", Want: describe(staking[i]) + " as in staking", Got: describe(consensus[i])}
		}
	}
	return nil
}

func describe(u abci.ValidatorUpdate) string {
	pk, err := cryptoenc.PubKeyFromProto(u.PubKey)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("address %s power %d", pk.Address(), u.Power)
}
