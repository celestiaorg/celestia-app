package types

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestValidateBasic(t *testing.T) {
	valAddr, err := sdk.ValAddressFromBech32("cosmosvaloper1xcy3els9ua75kdm783c3qu0rfa2eples6eavqq")
	require.NoError(t, err)
	evmAddr := common.BytesToAddress([]byte("hello")).String()

	_, err = NewMsgRegisterEVMAddress(valAddr.String(), evmAddr)
	require.NoError(t, err)
	_, err = NewMsgRegisterEVMAddress(valAddr.String(), "invalid evm address")
	require.Error(t, err)
	_, err = NewMsgRegisterEVMAddress("invalid validator address", evmAddr)
}
