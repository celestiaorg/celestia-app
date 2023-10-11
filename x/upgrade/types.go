package upgrade

import (
	fmt "fmt"

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

func (s Schedule) ValidateBasic() error {
	lastHeight := 0
	lastVersion := uint64(0)
	for idx, plan := range s {
		if err := plan.ValidateBasic(); err != nil {
			return fmt.Errorf("plan %d: %w", idx, err)
		}
		if plan.Start <= int64(lastHeight) {
			return fmt.Errorf("plan %d: start height must be greater than %d, got %d", idx, lastHeight, plan.Start)
		}
		if plan.Version <= lastVersion {
			return fmt.Errorf("plan %d: version must be greater than %d, got %d", idx, lastVersion, plan.Version)
		}
		lastHeight = int(plan.End)
		lastVersion = plan.Version
	}
	return nil
}

// ValidateVersions checks if all plan versions are covered by all the app versions
// that the state machine supports.
func (s Schedule) ValidateVersions(appVersions []uint64) error {
	versionMap := make(map[uint64]struct{})
	for _, version := range appVersions {
		versionMap[version] = struct{}{}
	}
	for _, plan := range s {
		if _, ok := versionMap[plan.Version]; !ok {
			return fmt.Errorf("plan version %d not found in app versions %v", plan.Version, appVersions)
		}
	}
	return nil
}

func (s Schedule) ShouldProposeUpgrade(height int64) (uint64, bool) {
	for _, plan := range s {
		if height >= plan.Start-1 && height < plan.End {
			return plan.Version, true
		}
	}
	return 0, false
}

func (p Plan) ValidateBasic() error {
	if p.Start < 1 {
		return fmt.Errorf("plan start height cannot be negative or zero: %d", p.Start)
	}
	if p.End < 1 {
		return fmt.Errorf("plan end height cannot be negative or zero: %d", p.End)
	}
	if p.Start > p.End {
		return fmt.Errorf("plan end height must be greater or equal than start height: %d >= %d", p.Start, p.End)
	}
	if p.Version == 0 {
		return fmt.Errorf("plan version cannot be zero")
	}
	return nil
}

func NewSchedule(plans ...Plan) Schedule {
	return plans
}

func NewPlan(startHeight, endHeight int64, version uint64) Plan {
	return Plan{
		Start:   startHeight,
		End:     endHeight,
		Version: version,
	}
}
