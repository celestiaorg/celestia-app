package keeper

import (
	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/celestiaorg/celestia-app/x/mint/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// NewQuerier returns a minting Querier handler.
func NewQuerier(k Keeper, legacyQuerierCdc *codec.LegacyAmino) sdk.Querier {
	return func(ctx sdk.Context, path []string, _ abci.RequestQuery) ([]byte, error) {
		switch path[0] {
		case types.QueryInflationRate:
			return queryInflationRate(ctx, k, legacyQuerierCdc)

		case types.QueryAnnualProvisions:
			return queryAnnualProvisions(ctx, k, legacyQuerierCdc)

		case types.QueryGenesisTime:
			return queryGenesisTime(ctx, k, legacyQuerierCdc)

		default:
			return nil, errors.Wrapf(sdkerrors.ErrUnknownRequest, "unknown query path: %s", path[0])
		}
	}
}

func queryInflationRate(ctx sdk.Context, k Keeper, legacyQuerierCdc *codec.LegacyAmino) ([]byte, error) {
	minter := k.GetMinter(ctx)

	res, err := codec.MarshalJSONIndent(legacyQuerierCdc, minter.InflationRate)
	if err != nil {
		return nil, errors.Wrap(sdkerrors.ErrJSONMarshal, err.Error())
	}

	return res, nil
}

func queryAnnualProvisions(ctx sdk.Context, k Keeper, legacyQuerierCdc *codec.LegacyAmino) ([]byte, error) {
	minter := k.GetMinter(ctx)

	res, err := codec.MarshalJSONIndent(legacyQuerierCdc, minter.AnnualProvisions)
	if err != nil {
		return nil, errors.Wrap(sdkerrors.ErrJSONMarshal, err.Error())
	}

	return res, nil
}

func queryGenesisTime(ctx sdk.Context, k Keeper, legacyQuerierCdc *codec.LegacyAmino) ([]byte, error) {
	genesisTime := k.GetGenesisTime(ctx).GenesisTime

	res, err := codec.MarshalJSONIndent(legacyQuerierCdc, genesisTime)
	if err != nil {
		return nil, errors.Wrap(sdkerrors.ErrJSONMarshal, err.Error())
	}

	return res, nil
}
