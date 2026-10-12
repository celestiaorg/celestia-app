package types

import (
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EstimateGasForPayForFibre estimates the gas required for a PayForFibre message.
// The formula is: GasFibre = B + A × n
// where:
//
//	B = 650,000 — fixed cost per blob
//	A = 45,000 — per-chunk cost
//	n = ⌈blobSize / 262,144⌉ — number of 256 KiB chunks
//
// This formula is standalone and not dependent on GasPerBlobByte or GasPerCelestiaByte.
// It is the single source of truth for both the on-chain settlement charge (x/fibre
// keeper) and the client-side escrow accounting, so the two never disagree.
func EstimateGasForPayForFibre(blobSize uint32) uint64 {
	if blobSize == 0 {
		return appconsts.PFBFibreGasFixedCost
	}
	chunks := (uint64(blobSize) + uint64(appconsts.PFBFibreChunkSize) - 1) / uint64(appconsts.PFBFibreChunkSize)
	return appconsts.PFBFibreGasFixedCost + appconsts.PFBFibreGasPerChunk*chunks
}

// EstimateGasForPayForFibreSignatureVerification returns fixed plus per signature ante gas.
func EstimateGasForPayForFibreSignatureVerification(validatorSignatureCount uint64) uint64 {
	return appconsts.PFFibreTxGasFixedCost +
		validatorSignatureCount*appconsts.PFFibreGasPerValidatorSignature
}

// PaymentAmount returns the escrow payment for the padded upload size, rounded up
// to whole utia at the default minimum gas price.
func PaymentAmount(blobSize uint32) sdk.Coin {
	gas := math.NewIntFromUint64(EstimateGasForPayForFibre(blobSize))
	gasPrice := math.LegacyNewDecWithPrec(int64(appconsts.DefaultMinGasPrice*1_000_000), 6)
	return sdk.NewCoin(appconsts.BondDenom, gasPrice.MulInt(gas).Ceil().TruncateInt())
}
