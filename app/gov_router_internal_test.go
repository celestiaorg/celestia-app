package app

import (
	"testing"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/log"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

func TestGovMsgHandler(t *testing.T) {
	failing := func(sdk.Context, sdk.Msg) (*sdk.Result, error) {
		return nil, errorsmod.Wrap(govtypes.ErrNoProposalHandlerExists, "detail [file.go:1]")
	}
	panicking := func(sdk.Context, sdk.Msg) (*sdk.Result, error) {
		panic("boom")
	}
	ctx := sdk.NewContext(nil, cmtproto.Header{}, false, log.NewNopLogger())
	endBlockCtx := ctx.WithValue(govEndBlockKey{}, true)

	require.Nil(t, govMsgHandler(nil))

	// Outside the EndBlocker the handler is unchanged.
	_, err := govMsgHandler(failing)(ctx, nil)
	require.Equal(t, "detail [file.go:1]: no handler exists for proposal type", err.Error())
	require.Panics(t, func() { _, _ = govMsgHandler(panicking)(ctx, nil) })

	_, err = govMsgHandler(failing)(endBlockCtx, nil)
	require.Equal(t, "proposal message failed: codespace gov, code 9", err.Error())
	require.ErrorIs(t, err, govtypes.ErrNoProposalHandlerExists)
	codespace, code, _ := errorsmod.ABCIInfo(err, false)
	require.Equal(t, govtypes.ModuleName, codespace)
	require.Equal(t, govtypes.ErrNoProposalHandlerExists.ABCICode(), code)

	_, err = govMsgHandler(panicking)(endBlockCtx, nil)
	require.Equal(t, "proposal message failed: codespace undefined, code 111222", err.Error())
	require.ErrorIs(t, err, sdkerrors.ErrPanic)
}
