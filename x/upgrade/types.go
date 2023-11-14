package upgrade

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
)

const (
	StoreKey   = upgradetypes.StoreKey
	ModuleName = upgradetypes.ModuleName
)

var _ sdk.Msg = &MsgVersionChange{}

// TypeRegister is used to register the upgrade module's types in the encoding
// config without defining an entire module.
type TypeRegister struct{}

// RegisterLegacyAminoCodec registers the upgrade types on the LegacyAmino codec.
func (TypeRegister) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(upgradetypes.Plan{}, "cosmos-sdk/Plan", nil)
	cdc.RegisterConcrete(MsgVersionChange{}, "celestia/MsgVersionChange", nil)
}

// RegisterInterfaces registers the upgrade module types.
func (TypeRegister) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&MsgVersionChange{},
	)
}

func (msg *MsgVersionChange) GetSigners() []sdk.AccAddress {
	return nil
}

func (msg *MsgVersionChange) ValidateBasic() error {
	return nil
}

// NewMsgVersionChange creates a tx in byte form used to signal to validators
// to change to a new version
func NewMsgVersionChange(txConfig client.TxConfig, version uint64) ([]byte, error) {
	builder := txConfig.NewTxBuilder()
	msg := &MsgVersionChange{
		Version: version,
	}
	if err := builder.SetMsgs(msg); err != nil {
		return nil, err
	}
	return txConfig.TxEncoder()(builder.GetTx())
}

func IsUpgradeMsg(msg []sdk.Msg) (uint64, bool) {
	if len(msg) != 1 {
		return 0, false
	}
	msgVersionChange, ok := msg[0].(*MsgVersionChange)
	if !ok {
		return 0, false
	}
	return msgVersionChange.Version, true
}
