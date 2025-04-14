package blobfactory

import (
	"math/rand"

	"cosmossdk.io/math"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
)

func DefaultTxOpts() []user.TxOption {
	return FeeTxOpts(10_000_000_000)
}

func FeeTxOpts(gas uint64) []user.TxOption {
	fee := uint64(float64(gas)*appconsts.DefaultMinGasPrice) + 1
	return []user.TxOption{
		user.SetFee(fee),
		user.SetGasLimit(gas),
	}
}

func GenerateManyRawSendTxs(signer *user.Signer, count int) []coretypes.Tx {
	txs := make([]coretypes.Tx, count)
	for i := 0; i < count; i++ {
		txs[i] = GenerateRawSendTx(signer, 100)
	}
	return txs
}

// GenerateRawSendTx creates send transactions meant to help test encoding/prepare/process
// proposal, they are not meant to actually be executed by the state machine. If
// we want that, we have to update nonce, and send funds to someone other than
// the same account signing the transaction.
func GenerateRawSendTx(signer *user.Signer, amount int64) []byte {
	opts := DefaultTxOpts()

	amountCoin := sdk.Coin{
		Denom:  appconsts.BondDenom,
		Amount: math.NewInt(amount),
	}

	addr := signer.Account(testfactory.TestAccName).Address()
	msg := banktypes.NewMsgSend(addr, addr, sdk.NewCoins(amountCoin))

	tx, _, err := signer.CreateTx([]sdk.Msg{msg}, opts...)
	if err != nil {
		panic(err)
	}

	return tx
}

// GenerateRandomAmount generates a random amount for a Send transaction.
func GenerateRandomAmount(r *rand.Rand) int64 {
	n := r.Int63()
	if n < 0 {
		return -n
	}
	return n
}

// GenerateRandomRawSendTx generates a random raw send tx.
func GenerateRandomRawSendTx(rand *rand.Rand, signer *user.Signer) (rawTx []byte) {
	amount := GenerateRandomAmount(rand)
	return GenerateRawSendTx(signer, amount)
}

// GenerateManyRandomRawSendTxsSameSigner  generates count many random raw send txs.
func GenerateManyRandomRawSendTxsSameSigner(rand *rand.Rand, signer *user.Signer, count int) []coretypes.Tx {
	txs := make([]coretypes.Tx, count)
	for i := 0; i < count; i++ {
		txs[i] = GenerateRandomRawSendTx(rand, signer)
	}
	return txs
}
