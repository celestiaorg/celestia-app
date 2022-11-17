package testnode

import (
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SignAndBroadcastTx signs a transaction using the provided account and keyring
// inside the client.Context, then broadcasts it synchronously.
func SignAndBroadcastTx(encCfg encoding.Config, c Context, account string, msg ...sdk.Msg) (res *sdk.TxResponse, err error) {
	opts := []types.TxBuilderOption{
		types.SetGasLimit(1000000000000000000),
		// types.SetFeeAmount(sdk.NewCoins(
		// 	sdk.NewCoin(app.BondDenom, sdk.NewInt(100)),
		// )),
	}

	// use the key for accounts[i] to create a singer used for a single PFB
	signer := types.NewKeyringSigner(c.Keyring, account, c.ChainID)

	signer.SetEncodingConfig(encCfg)

	rec := signer.GetSignerInfo()
	addr, err := rec.GetAddress()
	if err != nil {
		return nil, err
	}

	acc, seq, err := c.AccountRetriever.GetAccountNumberSequence(c.Context, addr)
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
