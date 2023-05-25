package testnode

import (
	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var veryLargeGasLim uint64 = uint64(appconsts.DefaultSquareSizeUpperBound*appconsts.DefaultSquareSizeUpperBound) * appconsts.ShareSize * 10

// SignAndBroadcastTx signs a transaction using the provided account and keyring
// inside the client.Context, then broadcasts it synchronously.
func SignAndBroadcastTx(encCfg encoding.Config, c client.Context, account string, msg ...sdk.Msg) (res *sdk.TxResponse, err error) {
	opts := []types.TxBuilderOption{
		types.SetGasLimit(veryLargeGasLim),
		types.SetFeeAmount(sdk.NewCoins(
			sdk.NewCoin(app.BondDenom, sdk.NewInt(1)),
		)),
	}

	// use the key for accounts[i] to create a signer used for a single PFB
	signer := types.NewKeyringSigner(c.Keyring, account, c.ChainID)

	signer.SetEncodingConfig(encCfg)

	rec := signer.GetSignerInfo()
	addr, err := rec.GetAddress()
	if err != nil {
		return nil, err
	}

	acc, seq, err := c.AccountRetriever.GetAccountNumberSequence(c, addr)
	if err != nil {
		return nil, err
	}

	signer.SetAccountNumber(acc)
	signer.SetSequence(seq)

	tx, err := signer.BuildSignedTx(signer.NewTxBuilder(opts...), msg...)
	if err != nil {
		return nil, err
	}

	rawTx, err := encCfg.TxConfig.TxEncoder()(tx)
	if err != nil {
		return nil, err
	}

	return c.BroadcastTxSync(rawTx)
}
