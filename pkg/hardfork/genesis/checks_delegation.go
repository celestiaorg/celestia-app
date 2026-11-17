package genesis

import (
	"fmt"
	"math/big"

	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/codec"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// checkDelegations requires unique (delegator, validator) pairs with positive shares in a listed validator,
// and the shares of every validator to sum exactly to its delegator_shares.
func checkDelegations(cdc codec.JSONCodec, g *Genesis) error {
	const module, records = stakingtypes.ModuleName, "delegations"
	var gs stakingtypes.GenesisState
	if err := decodeModule(cdc, g, module, &gs); err != nil {
		return err
	}
	vals := make(map[string]int, len(gs.Validators)) // operator -> index in validators
	for i, v := range gs.Validators {
		if _, err := decodeAddr(valCodec, vals, module, "validators", i, "operator_address", v.OperatorAddress); err != nil {
			return err
		}
	}
	// Sums are scaled integers, so adding never overflows or panics.
	sums := make([]*big.Int, len(gs.Validators))
	for i := range sums {
		sums[i] = new(big.Int)
	}
	seen := make(map[[2]string]int, len(gs.Delegations))
	for i, d := range gs.Delegations {
		field := fmt.Sprintf("%s[%d]", records, i)
		del, err := decodeAddr(accCodec, nil, module, records, i, "delegator_address", d.DelegatorAddress)
		if err != nil {
			return err
		}
		val, err := decodeAddr(valCodec, nil, module, records, i, "validator_address", d.ValidatorAddress)
		if err != nil {
			return err
		}
		if j, ok := seen[[2]string{del, val}]; ok {
			return duplicateError(module, records, i, j, d.DelegatorAddress)
		}
		seen[[2]string{del, val}] = i
		if d.Shares.IsNil() || !d.Shares.IsPositive() {
			return &CheckError{Step: stepRecords, Module: module, Field: field + ".shares", Want: "> 0", Got: d.Shares.String()}
		}
		j, ok := vals[val]
		if !ok {
			return &CheckError{Step: stepRecords, Module: module, Field: field + ".validator_address", Want: "a validator in validators", Got: d.ValidatorAddress}
		}
		sums[j].Add(sums[j], d.Shares.BigInt())
	}
	for i, v := range gs.Validators {
		if v.DelegatorShares.IsNil() || v.DelegatorShares.BigInt().Cmp(sums[i]) != 0 {
			want := math.LegacyNewDecFromBigIntWithPrec(sums[i], math.LegacyPrecision).String()
			return &CheckError{Step: stepConservation, Module: module, Field: fmt.Sprintf("validators[%d].delegator_shares", i), Want: want, Got: v.DelegatorShares.String()}
		}
	}
	return nil
}
