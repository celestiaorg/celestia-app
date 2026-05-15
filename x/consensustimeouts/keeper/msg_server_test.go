package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v9/x/consensustimeouts/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
)

// TestUpdateParams_AuthorityRejected asserts a wrong-authority message fails
// with ErrUnauthorized and leaves stored params untouched.
func TestUpdateParams_AuthorityRejected(t *testing.T) {
	f := newTestFixture(t)
	original := f.keeper.GetParams(f.ctx)

	msg := &types.MsgUpdateParams{
		Authority: "celestia1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq", // not the gov authority
		Params:    modifiedParams(),
	}

	_, err := f.keeper.UpdateParams(f.ctx, msg)
	require.Error(t, err)
	require.ErrorIs(t, err, sdkerrors.ErrUnauthorized)

	// State must not have changed.
	require.Equal(t, original, f.keeper.GetParams(f.ctx))
}

// TestUpdateParams_ValidationRejected asserts out-of-range params (TimeoutCommit=0)
// fail with ErrInvalidRequest and the error message references the offending
// field.
func TestUpdateParams_ValidationRejected(t *testing.T) {
	f := newTestFixture(t)
	original := f.keeper.GetParams(f.ctx)

	bad := types.DefaultParams()
	bad.TimeoutCommit = 0

	msg := &types.MsgUpdateParams{
		Authority: f.authority,
		Params:    bad,
	}

	_, err := f.keeper.UpdateParams(f.ctx, msg)
	require.Error(t, err)
	require.ErrorIs(t, err, sdkerrors.ErrInvalidRequest)
	require.Contains(t, err.Error(), "timeout_commit")

	// State must not have changed.
	require.Equal(t, original, f.keeper.GetParams(f.ctx))
}

// TestUpdateParams_HappyPath_EventEmitted asserts a valid update persists the
// new params and emits exactly one EventUpdateParams carrying the authority and
// params.
func TestUpdateParams_HappyPath_EventEmitted(t *testing.T) {
	f := newTestFixture(t)
	// Use a context with a fresh event manager so we only see this call's events.
	ctx := f.ctx.WithEventManager(sdk.NewEventManager())

	want := modifiedParams()
	msg := &types.MsgUpdateParams{
		Authority: f.authority,
		Params:    want,
	}

	resp, err := f.keeper.UpdateParams(ctx, msg)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// State was updated.
	require.Equal(t, want, f.keeper.GetParams(ctx))

	// Exactly one EventUpdateParams was emitted.
	eventType := proto.MessageName(&types.EventUpdateParams{})
	abciEvents := ctx.EventManager().ABCIEvents()
	var emitted []abci.Event
	for _, e := range abciEvents {
		if e.Type == eventType {
			emitted = append(emitted, e)
		}
	}
	require.Len(t, emitted, 1, "expected exactly one %s event", eventType)

	// Decode the event back into the typed struct to verify its contents.
	typed, err := sdk.ParseTypedEvent(emitted[0])
	require.NoError(t, err)
	got, ok := typed.(*types.EventUpdateParams)
	require.True(t, ok, "decoded event has unexpected type %T", typed)
	require.Equal(t, f.authority, got.Authority)
	require.Equal(t, want, got.Params)
}
