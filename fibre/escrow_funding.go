package fibre

import (
	"context"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// txDepositor implements [Depositor] by broadcasting MsgDepositToEscrow through
// the tx client that owns the escrow account, then confirming inclusion.
type txDepositor struct{ tx *user.TxClient }

func (d txDepositor) DepositToEscrow(ctx context.Context, signer string, amount math.Int) error {
	msg := &fibretypes.MsgDepositToEscrow{
		Signer: signer,
		Amount: sdk.NewCoin(appconsts.BondDenom, amount),
	}
	resp, err := d.tx.BroadcastTx(ctx, []sdk.Msg{msg})
	if err != nil {
		return err
	}
	if _, err := d.tx.ConfirmTx(ctx, resp.TxHash); err != nil {
		return err
	}
	return nil
}

// txEscrowQuerier implements [EscrowQuerier] via the x/fibre Query/EscrowAccount
// RPC over the tx client's gRPC connection.
type txEscrowQuerier struct{ q fibretypes.QueryClient }

func newTxEscrowQuerier(tx *user.TxClient) txEscrowQuerier {
	return txEscrowQuerier{q: fibretypes.NewQueryClient(tx.GRPCConn())}
}

func (e txEscrowQuerier) EscrowBalance(ctx context.Context, signer string) (math.Int, error) {
	resp, err := e.q.EscrowAccount(ctx, &fibretypes.QueryEscrowAccountRequest{Signer: signer})
	if err != nil {
		return math.ZeroInt(), err
	}
	// The query reports a missing account via Found=false (not an error), with a
	// zero-value EscrowAccount whose Balance.Amount is nil.
	if !resp.Found || resp.EscrowAccount == nil {
		return math.ZeroInt(), nil
	}
	if bal := resp.EscrowAccount.Balance.Amount; !bal.IsNil() {
		return bal, nil
	}
	return math.ZeroInt(), nil
}
