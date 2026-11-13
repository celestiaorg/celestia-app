package app

import (
	"cosmossdk.io/x/feegrant"
	feegrantkeeper "cosmossdk.io/x/feegrant/keeper"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// FrozenWhitehatUpgrade deletes the authz grants and fee allowances granted
// by listed accounts and resets their withdraw addresses, so that nothing set
// up with an exposed key outlives the v11 activation of app/frozen.
func (app *App) FrozenWhitehatUpgrade(ctx sdk.Context) error {
	rules := app.FrozenRules

	type grantKey struct {
		granter, grantee sdk.AccAddress
		msgType          string
	}
	var grants []grantKey
	app.AuthzKeeper.IterateGrants(ctx, func(granter, grantee sdk.AccAddress, g authz.Grant) bool {
		if !rules.Has(granter) {
			return false
		}
		// A grant that can't be unpacked can't be executed either.
		if a, err := g.GetAuthorization(); err == nil {
			grants = append(grants, grantKey{granter, grantee, a.MsgTypeURL()})
		}
		return false
	})
	for _, g := range grants {
		if err := app.AuthzKeeper.DeleteGrant(ctx, g.grantee, g.granter, g.msgType); err != nil {
			return err
		}
	}

	var allowances []*feegrant.MsgRevokeAllowance
	addrCodec := app.AccountKeeper.AddressCodec()
	err := app.FeeGrantKeeper.IterateAllFeeAllowances(ctx, func(g feegrant.Grant) bool {
		if granter, err := addrCodec.StringToBytes(g.Granter); err == nil && rules.Has(granter) {
			allowances = append(allowances, &feegrant.MsgRevokeAllowance{Granter: g.Granter, Grantee: g.Grantee})
		}
		return false
	})
	if err != nil {
		return err
	}
	feegrantServer := feegrantkeeper.NewMsgServerImpl(app.FeeGrantKeeper)
	for _, msg := range allowances {
		if _, err := feegrantServer.RevokeAllowance(ctx, msg); err != nil {
			return err
		}
	}

	var withdrawers []sdk.AccAddress
	app.DistrKeeper.IterateDelegatorWithdrawAddrs(ctx, func(del, _ sdk.AccAddress) bool {
		if rules.Has(del) {
			withdrawers = append(withdrawers, del)
		}
		return false
	})
	for _, del := range withdrawers {
		if err := app.DistrKeeper.DeleteDelegatorWithdrawAddr(ctx, del, nil); err != nil {
			return err
		}
	}

	ctx.Logger().Info("frozen accounts cleaned up", "authz_grants", len(grants), "fee_allowances", len(allowances), "withdraw_addresses", len(withdrawers))
	return nil
}
