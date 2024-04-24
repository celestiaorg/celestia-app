package module_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/app/module"
	"github.com/celestiaorg/celestia-app/v2/x/blobstream"
	blobstreamkeeper "github.com/celestiaorg/celestia-app/v2/x/blobstream/keeper"
	blobstreamtypes "github.com/celestiaorg/celestia-app/v2/x/blobstream/types"
	"github.com/celestiaorg/celestia-app/v2/x/signal"
	signaltypes "github.com/celestiaorg/celestia-app/v2/x/signal/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/store"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/tests/mocks"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/params/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

func TestConfiguratorRegistersAllMessageTypes(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)
	mockServer := mocks.NewMockServer(mockCtrl)
	mockServer.EXPECT().RegisterService(gomock.Any(), gomock.Any()).Times(2).Return()
	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	configurator := module.NewConfigurator(cdc.Codec, mockServer, mockServer)

	storeKey := sdk.NewKVStoreKey(signaltypes.StoreKey)

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	keeper := signal.NewKeeper(storeKey, nil)
	upgradeModule := signal.NewAppModule(keeper)
	mm, err := module.NewManager([]module.VersionedModule{
		{Module: upgradeModule, FromVersion: 2, ToVersion: 2},
	})
	require.NoError(t, err)
	require.NotNil(t, mm)

	mm.RegisterServices(configurator)
	acceptedMessages := configurator.GetAcceptedMessages()
	require.Equal(t, map[uint64]map[string]struct{}{
		2: {"/celestia.signal.v1.MsgSignalVersion": {}, "/celestia.signal.v1.MsgTryUpgrade": {}},
	}, acceptedMessages)

	require.NotNil(t, keeper)
}

// TestConfigurator verifies that the configurator only registers blobstream messages for version 1.
func TestConfigurator(t *testing.T) {
	mockCtrl := gomock.NewController(t)
	t.Cleanup(mockCtrl.Finish)

	mockServer := mocks.NewMockServer(mockCtrl)
	mockServer.EXPECT().RegisterService(gomock.Any(), gomock.Any()).Times(2).Return()
	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	configurator := module.NewConfigurator(config.Codec, mockServer, mockServer)

	keeper, _ := setupKeeper(t)
	blobstream := blobstream.NewAppModule(config.Codec, *keeper)

	mm, err := module.NewManager([]module.VersionedModule{{Module: blobstream, FromVersion: 1, ToVersion: 1}})
	require.NoError(t, err)
	require.NotNil(t, mm)

	mm.RegisterServices(configurator)
	acceptedMessages := configurator.GetAcceptedMessages()
	require.Equal(t, map[uint64]map[string]struct{}{
		1: {"/celestia.qgb.v1.MsgRegisterEVMAddress": {}},
	}, acceptedMessages)

	require.NotNil(t, keeper)
}

func setupKeeper(t *testing.T) (*blobstreamkeeper.Keeper, store.CommitMultiStore) {
	registry := codectypes.NewInterfaceRegistry()
	appCodec := codec.NewProtoCodec(registry)
	storeKey := sdk.NewKVStoreKey(blobstreamtypes.StoreKey)
	subspace := types.NewSubspace(appCodec, codec.NewLegacyAmino(), storeKey, storeKey, "params")

	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	stakingKeeper := newMockStakingKeeper(map[string]int64{})
	keeper := blobstreamkeeper.NewKeeper(
		appCodec,
		storeKey,
		subspace,
		stakingKeeper,
	)
	return keeper, stateStore
}

type mockStakingKeeper struct{}

func newMockStakingKeeper(_ map[string]int64) *mockStakingKeeper {
	return &mockStakingKeeper{}
}

func (m *mockStakingKeeper) GetLastValidatorPower(_ sdk.Context, _ sdk.ValAddress) int64 {
	return 0
}

func (m *mockStakingKeeper) GetValidator(_ sdk.Context, _ sdk.ValAddress) (validator stakingtypes.Validator, found bool) {
	return stakingtypes.Validator{}, false
}

func (m *mockStakingKeeper) GetBondedValidatorsByPower(_ sdk.Context) []stakingtypes.Validator {
	return []stakingtypes.Validator{}
}
