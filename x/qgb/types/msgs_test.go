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
	evmAddr := common.BytesToAddress([]byte("hello"))

	msg := NewMsgRegisterEVMAddress(valAddr, evmAddr)
	require.NoError(t, msg.ValidateBasic())
	msg = &MsgRegisterEVMAddress{valAddr.String(), "invalid evm address"}
	require.Error(t, msg.ValidateBasic())
	msg = &MsgRegisterEVMAddress{"invalid validator address", evmAddr.Hex()}
	require.Error(t, msg.ValidateBasic())
}
