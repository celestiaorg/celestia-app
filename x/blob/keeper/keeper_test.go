package keeper

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/blob"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	proto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestPayForBlobs(t *testing.T) {
	k, stateStore := keeper(t)
	ctx := sdk.NewContext(stateStore, tmproto.Header{}, false, nil)
	signer := "celestia15drmhzw5kwgenvemy30rqqqgq52axf5wwrruf7"
	namespace := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	namespaces := [][]byte{namespace.Bytes()}
	blobData := []byte("blob")
	blobSizes := []uint32{uint32(len(blobData))}

	// verify no events exist yet
	events := ctx.EventManager().Events().ToABCIEvents()
	assert.Len(t, events, 0)

	// emit an event by submitting a PayForBlob
	msg := createMsgPayForBlob(t, signer, namespace, blobData)
	k.PayForBlobs(ctx, msg)

	// verify that an event was emitted
	events = ctx.EventManager().Events().ToABCIEvents()
	assert.Len(t, events, 1)
	parsedEvent, err := sdk.ParseTypedEvent(events[0])
	require.NoError(t, err)
	event, err := ConvertToEventPayForBlobs(parsedEvent)
	require.NoError(t, err)

	// verify the attributes of the event
	assert.Equal(t, signer, event.Signer)
	assert.Equal(t, namespaces, event.Namespaces)
	assert.Equal(t, blobSizes, event.BlobSizes)
}

func ConvertToEventPayForBlobs(message proto.Message) (*types.EventPayForBlobs, error) {
	// Type assertion to convert proto.Message to *EventPayForBlobs
	if event, ok := message.(*types.EventPayForBlobs); ok {
		return event, nil
	}
	return nil, fmt.Errorf("message is not of type EventPayForBlobs")
}

func createMsgPayForBlob(t *testing.T, signer string, namespace appns.Namespace, blobData []byte) *types.MsgPayForBlobs {
	blob := blob.New(namespace, blobData, appconsts.ShareVersionZero)
	msg, err := types.NewMsgPayForBlobs(signer, blob)
	require.NoError(t, err)
	return msg
}
