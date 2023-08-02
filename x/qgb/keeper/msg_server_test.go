package keeper_test

import (
	"testing"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestRegisterEVMAddress(t *testing.T) {
	input, sdkCtx := testutil.SetupFiveValChain(t)
	k := input.QgbKeeper
	vals := input.StakingKeeper.GetValidators(sdkCtx, 100)
	require.GreaterOrEqual(t, len(vals), 1)
	val := vals[0]
	_, exists := k.GetEVMAddress(sdkCtx, val.OperatorAddress)
	require.False(t, exists)

	msg, err := types.NewMsgRegisterEVMAddress(val.OperatorAddress, val.EvmAddress)
	require.NoError(t, err)
	_, err = k.RegisterEVMAddress(sdkCtx, msg)
	require.NoError(t, err)

	_, exists = k.GetEVMAddress(sdkCtx, val.OperatorAddress)
	require.True(t, exists)

	// test again with an address that is not the validator
	valAddr, err := sdk.ValAddressFromBech32("celestiavaloper1xcy3els9ua75kdm783c3qu0rfa2eplestc6sqc")
	require.NoError(t, err)
	msg, err = types.NewMsgRegisterEVMAddress(valAddr.String(), val.EvmAddress)
	require.NoError(t, err)

	_, err = k.RegisterEVMAddress(sdkCtx, msg)
	require.Error(t, err)

	// overide the previous EVM address with a new one
	evmAddr := common.BytesToAddress([]byte("evm_address")).String()
	msg, err = types.NewMsgRegisterEVMAddress(val.OperatorAddress, evmAddr)
	_, err = k.RegisterEVMAddress(sdkCtx, msg)
	require.NoError(t, err)

	addr, _ := k.GetEVMAddress(sdkCtx, val.OperatorAddress)
	require.Equal(t, evmAddr, addr)

}
