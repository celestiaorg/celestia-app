package app

import (
	"github.com/cosmos/cosmos-sdk/baseapp"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func BlockedParamsV1() [][2]string {
	return [][2]string{
		// bank.SendEnabled
		{banktypes.ModuleName, string(banktypes.KeySendEnabled)},
		// staking.UnbondingTime
		{stakingtypes.ModuleName, string(stakingtypes.KeyUnbondingTime)},
		// staking.BondDenom
		{stakingtypes.ModuleName, string(stakingtypes.KeyBondDenom)},
		// consensus.validator.ValidatorParams
		{baseapp.Paramspace, string(baseapp.ParamStoreKeyValidatorParams)},
	}
}

func BlockedParamsV2() [][2]string {
	result := BlockedParamsV1()
	result = append(result, [][2]string{
		// consensus.evidence.EvidenceParams
		{baseapp.Paramspace, string(baseapp.ParamStoreKeyEvidenceParams)},
	}...)
	return result
}
