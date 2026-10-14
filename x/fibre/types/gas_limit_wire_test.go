package types

import (
	"testing"

	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

// TestGasLimitFromAuthInfoMatchesDecoder pins the wire read against what a
// protobuf decoder produces, including for encodings a transaction is free to
// choose. A disagreement is not cosmetic: a gas limit read as zero makes the
// pre-verification pass skip the transaction, so an encoding that reads zero
// here but non-zero to the SDK would let anyone force their certificates onto
// the sequential verification path.
func TestGasLimitFromAuthInfoMatchesDecoder(t *testing.T) {
	const (
		authInfoFee  = 2
		feeGasLimit  = 2
		feeAmountFld = 1
	)

	feeWithGas := func(gas uint64) []byte {
		var fee []byte
		fee = protowire.AppendTag(fee, feeGasLimit, protowire.VarintType)
		return protowire.AppendVarint(fee, gas)
	}
	appendFee := func(authInfo, fee []byte) []byte {
		authInfo = protowire.AppendTag(authInfo, authInfoFee, protowire.BytesType)
		return protowire.AppendBytes(authInfo, fee)
	}

	// an empty Fee message: present, but with no gas_limit field
	emptyFee := []byte{}
	// a Fee carrying only an (empty) amount, so it is non-empty but still
	// declares no gas_limit
	amountOnlyFee := protowire.AppendBytes(
		protowire.AppendTag(nil, feeAmountFld, protowire.BytesType), nil)

	cases := []struct {
		name     string
		authInfo []byte
	}{
		{"single fee", appendFee(nil, feeWithGas(1_000_000))},
		{"gas then empty fee", appendFee(appendFee(nil, feeWithGas(1_000_000)), emptyFee)},
		{"empty fee then gas", appendFee(appendFee(nil, emptyFee), feeWithGas(1_000_000))},
		{"gas then amount-only fee", appendFee(appendFee(nil, feeWithGas(1_000_000)), amountOnlyFee)},
		{"two gas limits", appendFee(appendFee(nil, feeWithGas(1_000_000)), feeWithGas(7))},
		{"repeated gas in one fee", appendFee(nil, append(feeWithGas(1_000_000), feeWithGas(7)...))},
		{"no fee at all", nil},
		{"amount-only fee", appendFee(nil, amountOnlyFee)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var authInfo cosmostx.AuthInfo
			require.NoError(t, authInfo.Unmarshal(tc.authInfo),
				"the vector must be an encoding the decoder accepts")

			var want uint64
			if authInfo.Fee != nil {
				want = authInfo.Fee.GasLimit
			}
			require.Equal(t, want, gasLimitFromAuthInfo(tc.authInfo))
		})
	}
}
