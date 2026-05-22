package ethereum

import (
	"fmt"
	"math/big"

	sdkmath "cosmossdk.io/math"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

const (
	nativeDecimals = 6
	evmDecimals    = 18
)

var evmUnitMultiplier = new(big.Int).Exp(big.NewInt(10), big.NewInt(evmDecimals-nativeDecimals), nil)

// IdentityResolver resolves observed Ethereum addresses to canonical Celestia
// account addresses.
type IdentityResolver interface {
	Resolve(ctx sdk.Context, ethAddr []byte) (sdk.AccAddress, bool)
}

// ValidateValueTransfer verifies that a translated Celestia transaction matches
// the preserved Ethereum value-transfer envelope.
func ValidateValueTransfer(ctx sdk.Context, resolver IdentityResolver, signerData txsigning.SignerData, txData txsigning.TxData, signature []byte) error {
	auth, err := AuthorizationFromTxData(signerData, txData)
	if err != nil {
		return err
	}
	if !equalBytes(signature, auth.Signature) {
		return fmt.Errorf("Ethereum transaction signature does not match preserved envelope signature")
	}
	if resolver == nil {
		return fmt.Errorf("missing Ethereum identity resolver")
	}

	fromCelestia, found := resolver.Resolve(ctx, auth.From.Bytes())
	if !found {
		return fmt.Errorf("unresolved Ethereum sender %s", auth.From.Hex())
	}
	if fromCelestia.String() != signerData.Address {
		return fmt.Errorf("Ethereum sender %s resolves to %s, expected signer %s", auth.From.Hex(), fromCelestia.String(), signerData.Address)
	}

	toCelestia, found := resolver.Resolve(ctx, auth.To.Bytes())
	if !found {
		return fmt.Errorf("unresolved Ethereum recipient %s", auth.To.Hex())
	}

	if auth.Tx.Nonce() != signerData.Sequence {
		return fmt.Errorf("Ethereum nonce %d does not match Cosmos sequence %d", auth.Tx.Nonce(), signerData.Sequence)
	}

	if txData.AuthInfo == nil || txData.AuthInfo.Fee == nil {
		return fmt.Errorf("missing tx auth info fee")
	}
	if txData.AuthInfo.Fee.Payer != "" {
		return fmt.Errorf("fee payer is not supported for Ethereum transaction authorization")
	}
	if txData.AuthInfo.Fee.Granter != "" {
		return fmt.Errorf("fee granter is not supported for Ethereum transaction authorization")
	}
	if txData.AuthInfo.Fee.GasLimit != auth.Tx.Gas() {
		return fmt.Errorf("tx gas limit %d does not match Ethereum gas limit %d", txData.AuthInfo.Fee.GasLimit, auth.Tx.Gas())
	}

	msg, err := singleMsgSend(txData)
	if err != nil {
		return err
	}
	if msg.FromAddress != signerData.Address {
		return fmt.Errorf("MsgSend from address %s does not match signer %s", msg.FromAddress, signerData.Address)
	}
	if msg.ToAddress != toCelestia.String() {
		return fmt.Errorf("MsgSend to address %s does not match resolved Ethereum recipient %s", msg.ToAddress, toCelestia.String())
	}

	amount, err := fromEVMUnits(auth.Tx.Value())
	if err != nil {
		return fmt.Errorf("convert Ethereum value to native amount: %w", err)
	}
	if !amount.IsPositive() {
		return fmt.Errorf("native transfer amount must be positive")
	}
	if !msg.Amount.Equal(sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, amount))) {
		return fmt.Errorf("MsgSend amount %s does not match Ethereum value %s", msg.Amount.String(), sdk.NewCoin(appconsts.BondDenom, amount).String())
	}

	feeAmount, err := ethereumFeeToNative(auth.Tx.GasFeeCap(), auth.Tx.Gas())
	if err != nil {
		return fmt.Errorf("convert Ethereum fee cap to native fee amount: %w", err)
	}
	if len(txData.AuthInfo.Fee.Amount) != 1 ||
		txData.AuthInfo.Fee.Amount[0].Denom != appconsts.BondDenom ||
		txData.AuthInfo.Fee.Amount[0].Amount != feeAmount.String() {
		return fmt.Errorf("tx fee amount does not match Ethereum fee cap total %s", sdk.NewCoin(appconsts.BondDenom, feeAmount).String())
	}

	return nil
}

func singleMsgSend(txData txsigning.TxData) (*banktypes.MsgSend, error) {
	if txData.Body == nil {
		return nil, fmt.Errorf("missing tx body")
	}
	if len(txData.Body.Messages) != 1 {
		return nil, fmt.Errorf("Ethereum transaction authorization supports exactly one message")
	}

	msgAny := txData.Body.Messages[0]
	if msgAny.TypeUrl != "/cosmos.bank.v1beta1.MsgSend" {
		return nil, fmt.Errorf("unsupported Ethereum transaction message type %s", msgAny.TypeUrl)
	}

	var msg banktypes.MsgSend
	if err := msg.Unmarshal(msgAny.Value); err != nil {
		return nil, fmt.Errorf("decode MsgSend: %w", err)
	}
	return &msg, nil
}

func fromEVMUnits(evmAmount *big.Int) (sdkmath.Int, error) {
	if evmAmount == nil || evmAmount.Sign() < 0 {
		return sdkmath.Int{}, fmt.Errorf("amount must be non-negative")
	}
	nativeAmount, remainder := new(big.Int).QuoRem(evmAmount, evmUnitMultiplier, new(big.Int))
	if remainder.Sign() != 0 {
		return sdkmath.Int{}, fmt.Errorf("amount %s is not divisible by %s", evmAmount, evmUnitMultiplier)
	}
	return sdkmath.NewIntFromBigInt(nativeAmount), nil
}

func ethereumFeeToNative(gasFeeCap *big.Int, gasLimit uint64) (sdkmath.Int, error) {
	if gasFeeCap == nil || gasFeeCap.Sign() < 0 {
		return sdkmath.Int{}, fmt.Errorf("gas fee cap must be non-negative")
	}
	totalFee := new(big.Int).Mul(gasFeeCap, new(big.Int).SetUint64(gasLimit))
	return fromEVMUnits(totalFee)
}

func equalBytes(left []byte, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
