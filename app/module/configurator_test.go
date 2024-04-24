package module_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/app/module"
	"github.com/celestiaorg/celestia-app/v2/x/signal"
	signaltypes "github.com/celestiaorg/celestia-app/v2/x/signal/types"
	"github.com/cosmos/cosmos-sdk/store"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/tests/mocks"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
