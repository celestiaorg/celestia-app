package ante

import (
	"encoding/hex"
	"slices"

	"cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/cosmos/cosmos-sdk/x/auth/migrations/legacytx"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

var (
	// Simulation signature values used to estimate gas consumption.
	key                = make([]byte, secp256k1.PubKeySize)
	simSecp256k1Pubkey = &secp256k1.PubKey{Key: key}
	simSecp256k1Sig    [64]byte
)

func init() {
	// Decodes a valid hex string into a sepc256k1Pubkey for use in transaction simulation
	bz, _ := hex.DecodeString("035AD6810A47F073553FF30D2FCC7E0D3B1C0B74B61A1AAA2582344037151E143A")
	copy(key, bz)
	simSecp256k1Pubkey.Key = key
}

// ConsumeTxSizeGasDecorator will take in parameters and consume gas proportional
// to the size of tx before calling next AnteHandler. Note, the gas costs will be
// slightly overestimated due to the fact that any given signing account may need
// to be retrieved from state.
//
// CONTRACT: If simulate=true, then signatures must either be completely filled
// in or empty.
// CONTRACT: To use this decorator, signatures of transaction must be represented
// as legacytx.StdSignature otherwise simulate mode will incorrectly estimate gas cost.
//
// The code was copied from celestia's fork of the cosmos-sdk:
// https://github.com/celestiaorg/cosmos-sdk/blob/release/v0.46.x-celestia/x/auth/ante/basic.go
// In app versions v2 and below, the txSizeCostPerByte used for gas cost estimation is taken from the auth module.
// In app v3 and above, the versioned constant appconsts.TxSizeCostPerByte is used.
// In app v4 getting the account has been removed, which is in line with the cosmos-sdk v0.50.x.
type ConsumeTxSizeGasDecorator struct {
	ak ante.AccountKeeper
}

func NewConsumeGasForTxSizeDecorator(ak ante.AccountKeeper) ConsumeTxSizeGasDecorator {
	return ConsumeTxSizeGasDecorator{
		ak: ak,
	}
}

func (cgts ConsumeTxSizeGasDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	sigTx, ok := tx.(authsigning.SigVerifiableTx)
	if !ok {
		return ctx, errors.Wrap(sdkerrors.ErrTxDecode, "invalid tx type")
	}

	ctx.GasMeter().ConsumeGas(appconsts.DefaultTxSizeCostPerByte*storetypes.Gas(len(ctx.TxBytes())), "txSize")

	// simulate gas cost for signatures in simulate mode
	if simulate {
		// in simulate mode, each element should be a nil signature
		sigs, err := sigTx.GetSignaturesV2()
		if err != nil {
			return ctx, err
		}
		n := len(sigs)

		for i, signer := range sigs {
			// if signature is already filled in, no need to simulate gas cost
			if i < n && !isIncompleteSignature(sigs[i].Data) {
				continue
			}

			var pubkey cryptotypes.PubKey

			// use placeholder simSecp256k1Pubkey if sig is nil
			if signer.PubKey == nil {
				pubkey = simSecp256k1Pubkey
			} else {
				pubkey = signer.PubKey
			}

			// use stdsignature to mock the size of a full signature
			simSig := legacytx.StdSignature{ //nolint:staticcheck // this will be removed when proto is ready
				Signature: simSecp256k1Sig[:],
				PubKey:    pubkey,
			}

			sigBz := legacy.Cdc.MustMarshal(simSig)
			txBytes := storetypes.Gas(len(sigBz) + 6)

			// If the pubkey is a multi-signature pubkey, then we estimate for the maximum
			// number of signers.
			params := cgts.ak.GetParams(ctx)
			if _, ok := pubkey.(*multisig.LegacyAminoPubKey); ok {
				txBytes *= params.TxSigLimit
			}

			ctx.GasMeter().ConsumeGas(appconsts.DefaultTxSizeCostPerByte*txBytes, "txSize")
		}
	}

	return next(ctx, tx, simulate)
}

// isIncompleteSignature tests whether SignatureData is fully filled in for simulation purposes
func isIncompleteSignature(data signing.SignatureData) bool {
	if data == nil {
		return true
	}

	switch data := data.(type) {
	case *signing.SingleSignatureData:
		return len(data.Signature) == 0
	case *signing.MultiSignatureData:
		if len(data.Signatures) == 0 {
			return true
		}
		if slices.ContainsFunc(data.Signatures, isIncompleteSignature) {
			return true
		}
	}

	return false
}
