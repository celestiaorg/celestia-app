package keeper_test

import (
	"bytes"
	"fmt"
	"testing"

	"cosmossdk.io/store"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/x/blob/keeper"
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	proto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	tmversion "github.com/cometbft/cometbft/proto/tendermint/version"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
	tmdb "github.com/tendermint/tm-db"
)

// TestPayForBlobs verifies the attributes on the emitted event.
func TestPayForBlobs(t *testing.T) {
	k, _, ctx := CreateKeeper(t, appconsts.LatestVersion)
	signer := "celestia15drmhzw5kwgenvemy30rqqqgq52axf5wwrruf7"
	namespace := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	namespaces := [][]byte{namespace.Bytes()}
	blobData := []byte("blob")
	blobSizes := []uint32{uint32(len(blobData))}

	// verify no events exist yet
	events := ctx.EventManager().Events().ToABCIEvents()
	assert.Len(t, events, 0)

	// emit an event by submitting a PayForBlob
	msg := createMsgPayForBlob(t, signer, namespace, blobData)
	_, err := k.PayForBlobs(ctx, msg)
	require.NoError(t, err)

	// verify that an event was emitted
	events = ctx.EventManager().Events().ToABCIEvents()
	assert.Len(t, events, 1)
	protoEvent, err := sdk.ParseTypedEvent(events[0])
	require.NoError(t, err)
	event, err := convertToEventPayForBlobs(protoEvent)
	require.NoError(t, err)

	// verify the attributes of the event
	assert.Equal(t, signer, event.Signer)
	assert.Equal(t, namespaces, event.Namespaces)
	assert.Equal(t, blobSizes, event.BlobSizes)
}

func convertToEventPayForBlobs(message proto.Message) (*types.EventPayForBlobs, error) {
	if event, ok := message.(*types.EventPayForBlobs); ok {
		return event, nil
	}
	return nil, fmt.Errorf("message is not of type EventPayForBlobs")
}

func createMsgPayForBlob(t *testing.T, signer string, namespace share.Namespace, blobData []byte) *types.MsgPayForBlobs {
	blob, err := share.NewBlob(namespace, blobData, share.ShareVersionZero, nil)
	require.NoError(t, err)
	msg, err := types.NewMsgPayForBlobs(signer, appconsts.LatestVersion, blob)
	require.NoError(t, err)
	return msg
}

func CreateKeeper(t *testing.T, version uint64) (*keeper.Keeper, store.CommitMultiStore, sdk.Context) {
	storeKey := sdk.NewKVStoreKey(paramtypes.StoreKey)
	tStoreKey := storetypes.NewTransientStoreKey(paramtypes.TStoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(tStoreKey, storetypes.StoreTypeTransient, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)
	ctx := sdk.NewContext(stateStore, tmproto.Header{
		Version: tmversion.Consensus{
			Block: 1,
			App:   version,
		},
	}, false, nil)

	paramsSubspace := paramtypes.NewSubspace(cdc,
		testutil.MakeTestCodec(),
		storeKey,
		tStoreKey,
		types.ModuleName,
	)
	k := keeper.NewKeeper(
		cdc,
		paramsSubspace,
	)
	k.SetParams(ctx, types.DefaultParams())

	return k, stateStore, ctx
}
