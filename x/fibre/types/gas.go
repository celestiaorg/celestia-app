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

// PaymentAmount returns the escrow payment charged for settling a Fibre blob.
// It mirrors the keeper's calculatePaymentAmount. Clients use this to size
// escrow reservations and deposits without round-tripping to the chain.
//
// Temporary network setting: a flat 1 utia regardless of blob size, so blob
// size does not gate throughput measurements. It makes fibre traffic
// effectively free and must not ship to a real network.
func PaymentAmount(_ uint32) sdk.Coin {
	return sdk.NewCoin(appconsts.BondDenom, math.OneInt())
}
