package app

import (
	"context"
	"errors"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// QuerySequence returns the committed account sequence for the provided signer.
// We bypass BaseApp's mempool-aware implementation so the application can
// ensure it queries from the latest committed state.
func (app *App) QuerySequence(_ context.Context, req *abci.RequestQuerySequence) (*abci.ResponseQuerySequence, error) {
	addr := sdk.AccAddress(req.Signer)
	// queryCtx, err := app.BaseApp.CreateQueryContext(app.LastBlockHeight(), false)
	// if err != nil {
	// 	return nil, err
	// }

	checkTxCtx, ok := app.BaseApp.CheckState()
	if !ok {
		return &abci.ResponseQuerySequence{}, errors.New("checkState not set")
	}

	sequence, err := app.AccountKeeper.GetSequence(checkTxCtx, addr)
	if err != nil {
		if errors.Is(err, sdkerrors.ErrUnknownAddress) {
			return &abci.ResponseQuerySequence{Sequence: 0}, nil
		}
		return nil, err
	}

	return &abci.ResponseQuerySequence{Sequence: sequence}, nil
}
