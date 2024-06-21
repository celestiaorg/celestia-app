package blobfactory

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	coretypes "github.com/tendermint/tendermint/types"
)

func DefaultTxOpts() []user.TxOption {
	return FeeTxOpts(10_000_000)
}

func FeeTxOpts(gas uint64) []user.TxOption {
	fee := uint64(float64(gas)*appconsts.DefaultMinGasPrice) + 1
	return []user.TxOption{
		user.SetFee(fee),
		user.SetGasLimit(gas),
	}
}

func GenerateManyRawSendTxs(txConfig client.TxConfig, count int) []coretypes.Tx {
	const acc = "signer"
	kr := testfactory.GenerateKeyring(acc)
	signer := blobtypes.NewKeyringSigner(kr, acc, "chainid")
	txs := make([]coretypes.Tx, count)
	for i := 0; i < count; i++ {
		txs[i] = GenerateRawSendTx(txConfig, signer, 100)
	}
	return txs
}

// GenerateRawSendTx creates send transactions meant to help test encoding/prepare/process
// proposal, they are not meant to actually be executed by the state machine. If
// we want that, we have to update nonce, and send funds to someone other than
// the same account signing the transaction.
func GenerateRawSendTx(txConfig client.TxConfig, signer *blobtypes.KeyringSigner, amount int64) (rawTx []byte) {
	feeCoin := sdk.Coin{
		Denom:  appconsts.BondDenom,
		Amount: sdk.NewInt(1),
	}

	opts := []blobtypes.TxBuilderOption{
		blobtypes.SetFeeAmount(sdk.NewCoins(feeCoin)),
		blobtypes.SetGasLimit(1000000000),
	}

	amountCoin := sdk.Coin{
		Denom:  appconsts.BondDenom,
		Amount: sdk.NewInt(amount),
	}

	addr, err := signer.GetSignerInfo().GetAddress()
	if err != nil {
		panic(err)
	}

	msg := banktypes.NewMsgSend(addr, addr, sdk.NewCoins(amountCoin))

	return CreateRawTx(txConfig, msg, signer, opts...)
}

func CreateRawTx(txConfig client.TxConfig, msg sdk.Msg, signer *blobtypes.KeyringSigner, opts ...blobtypes.TxBuilderOption) []byte {
	builder := signer.NewTxBuilder(opts...)
	tx, err := signer.BuildSignedTx(builder, msg)
	if err != nil {
		panic(err)
	}

	// encode the tx
	rawTx, err := txConfig.TxEncoder()(tx)
	if err != nil {
		panic(err)
	}

	return rawTx
}

// GenerateRandomAmount generates a random amount for a Send transaction.
func GenerateRandomAmount(rand *tmrand.Rand) int64 {
	n := rand.Int64()
	if n < 0 {
		return -n
	}
	return n
}

// GenerateRandomRawSendTx generates a random raw send tx.
func GenerateRandomRawSendTx(txConfig client.TxConfig, rand *tmrand.Rand, signer *blobtypes.KeyringSigner) (rawTx []byte) {
	amount := GenerateRandomAmount(rand)
	return GenerateRawSendTx(txConfig, signer, amount)
}

// GenerateManyRandomRawSendTxsSameSigner  generates count many random raw send txs.
func GenerateManyRandomRawSendTxsSameSigner(txConfig client.TxConfig, rand *tmrand.Rand, signer *blobtypes.KeyringSigner, count int) []coretypes.Tx {
	txs := make([]coretypes.Tx, count)
	for i := 0; i < count; i++ {
		txs[i] = GenerateRandomRawSendTx(txConfig, rand, signer)
	}
	return txs
}
