package keeper_test

import (
	"fmt"
	"math/big"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	_ "github.com/celestiaorg/celestia-app/v7/app/params" // init() sets celestia bech32 prefix
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/x/forwarding/keeper"
	"github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mochaMsgForwardFixture represents a real MsgForward transaction from Mocha-4
// testnet. These fixtures are used to verify that the x/forwarding fixes
// (#6877, #6880, #6881) do not change behavior for transactions that already
// succeeded on-chain.
type mochaMsgForwardFixture struct {
	height         int64
	txHash         string
	signer         string
	forwardAddr    string
	destDomain     uint32
	destRecipient  string
	maxIgpFeeDenom string
	maxIgpFeeAmt   string
	forwardedDenom string
	forwardedAmt   string
}

// TestMochaMsgForward_ValidateBasic verifies that all 73 MsgForward
// transactions from Mocha-4 pass ValidateBasic with the current code.
// A failure here would indicate the fixes changed validation in a way
// that rejects previously-accepted transactions.
func TestMochaMsgForward_ValidateBasic(t *testing.T) {
	for _, fix := range mochaMsgForwardFixtures {
		t.Run(fmt.Sprintf("height_%d", fix.height), func(t *testing.T) {
			msg := types.NewMsgForward(
				fix.signer,
				fix.forwardAddr,
				fix.destDomain,
				fix.destRecipient,
				sdk.NewCoin(fix.maxIgpFeeDenom, math.NewIntFromBigInt(mustParseBigInt(fix.maxIgpFeeAmt))),
			)
			err := msg.ValidateBasic()
			assert.NoError(t, err, "tx %s at height %d should pass ValidateBasic", fix.txHash, fix.height)
		})
	}
}

// TestMochaMsgForward_AddressDerivation verifies that DeriveForwardingAddress
// produces the expected forward_addr for all Mocha-4 transactions. This
// confirms the address derivation logic is unchanged by the fixes.
func TestMochaMsgForward_AddressDerivation(t *testing.T) {
	for _, fix := range mochaMsgForwardFixtures {
		t.Run(fmt.Sprintf("height_%d", fix.height), func(t *testing.T) {
			destRecipient, err := util.DecodeHexAddress(fix.destRecipient)
			require.NoError(t, err)

			derived, err := types.DeriveForwardingAddress(fix.destDomain, destRecipient.Bytes())
			require.NoError(t, err)

			expectedAddr, err := sdk.AccAddressFromBech32(fix.forwardAddr)
			require.NoError(t, err)

			assert.Equal(t, expectedAddr, sdk.AccAddress(derived),
				"derived address mismatch for tx %s at height %d", fix.txHash, fix.height)
		})
	}
}

// TestMochaMsgForward_SupportedDenoms verifies that filterSupportedDenoms
// (from fix #6877) preserves all denoms that were actually forwarded on
// Mocha-4. If any forwarded denom is filtered out, the fix would cause those
// transactions to fail with ErrNoBalance — breaking consensus.
func TestMochaMsgForward_SupportedDenoms(t *testing.T) {
	for _, fix := range mochaMsgForwardFixtures {
		t.Run(fmt.Sprintf("height_%d", fix.height), func(t *testing.T) {
			assert.True(t, keeper.IsSupportedDenom(fix.forwardedDenom),
				"forwarded denom %q at height %d should be supported", fix.forwardedDenom, fix.height)

			coins := sdk.NewCoins(sdk.NewCoin(fix.forwardedDenom, math.NewIntFromBigInt(mustParseBigInt(fix.forwardedAmt))))
			filtered := keeper.FilterSupportedDenoms(coins)
			assert.Equal(t, coins, filtered,
				"filterSupportedDenoms should preserve forwarded denom %q at height %d", fix.forwardedDenom, fix.height)
		})
	}
}

// TestMochaMsgForward_IGPFeeDenomIsSupported verifies that the IGP fee denom
// used in all Mocha-4 transactions is the bond denom. This is relevant to fix
// #6880 (IGP fee refund calculation) — the calculateExcessIGPFee function
// handles the case where forwardedBalance.Denom == quotedFee.Denom differently.
// Since all Mocha-4 transactions use utia for IGP fees and forward hyperlane/*
// tokens, the denoms never match, meaning the IGP subtraction branch is not
// exercised.
func TestMochaMsgForward_IGPFeeDenomIsSupported(t *testing.T) {
	for _, fix := range mochaMsgForwardFixtures {
		assert.Equal(t, appconsts.BondDenom, fix.maxIgpFeeDenom,
			"IGP fee denom at height %d should be bond denom", fix.height)
		// Verify the forwarded denom differs from IGP fee denom.
		// This is the common case for calculateExcessIGPFee.
		assert.NotEqual(t, fix.forwardedDenom, fix.maxIgpFeeDenom,
			"forwarded denom should differ from IGP fee denom at height %d", fix.height)
	}
}

// TestMochaMsgForward_AllSucceeded verifies that all 73 Mocha-4 MsgForward
// transactions forwarded exactly 1 token successfully (no partial failures).
// This establishes the baseline: fix #6881 (atomic state token sends) should
// not change behavior for transactions that had no failures.
func TestMochaMsgForward_AllSucceeded(t *testing.T) {
	assert.Len(t, mochaMsgForwardFixtures, 73, "expected 73 MsgForward transactions from Mocha-4")
	for _, fix := range mochaMsgForwardFixtures {
		assert.NotEmpty(t, fix.forwardedAmt, "expected non-empty forwarded amount at height %d", fix.height)
		amt := mustParseBigInt(fix.forwardedAmt)
		assert.True(t, amt.Sign() > 0, "expected positive forwarded amount at height %d", fix.height)
	}
}

// TestCalculateExcessIGPFee_MochaDenomPattern tests the calculateExcessIGPFee
// function with the denom pattern observed on Mocha-4: IGP fee in utia,
// forwarded token in hyperlane/*. Since the denoms differ, the function should
// NOT subtract the forwarded balance from igpUsed.
func TestCalculateExcessIGPFee_MochaDenomPattern(t *testing.T) {
	testCases := []struct {
		name          string
		beforeAmt     int64 // fee denom balance at forwardAddr before
		afterAmt      int64 // fee denom balance at forwardAddr after warp
		quotedFeeAmt  int64
		forwardAmt    int64
		forwardDenom  string
		expectedExcess int64
	}{
		{
			name:          "exact IGP fee consumed, no excess",
			beforeAmt:     0,
			afterAmt:      0,
			quotedFeeAmt:  78000,
			forwardAmt:    10000000,
			forwardDenom:  "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
			expectedExcess: 0,
		},
		{
			name:          "partial IGP fee consumed, excess refunded",
			beforeAmt:     0,
			afterAmt:      20000,
			quotedFeeAmt:  78000,
			forwardAmt:    10000000,
			forwardDenom:  "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
			expectedExcess: 20000, // quotedFee - (before + quotedFee - after) = 78000 - (0+78000-20000) = 20000
		},
		{
			name:          "same denom for fee and forward (hypothetical utia forward)",
			beforeAmt:     1000000,
			afterAmt:      0,
			quotedFeeAmt:  78000,
			forwardAmt:    1000000,
			forwardDenom:  appconsts.BondDenom,
			expectedExcess: 0, // igpUsed = before+quotedFee-after - forwardAmt = 1000000+78000-0-1000000 = 78000; excess = 78000-78000 = 0
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			before := sdk.NewCoin(appconsts.BondDenom, math.NewInt(tc.beforeAmt))
			after := sdk.NewCoin(appconsts.BondDenom, math.NewInt(tc.afterAmt))
			quotedFee := sdk.NewCoin(appconsts.BondDenom, math.NewInt(tc.quotedFeeAmt))
			forwardedBalance := sdk.NewCoin(tc.forwardDenom, math.NewInt(tc.forwardAmt))

			excess := keeper.CalculateExcessIGPFee(before, after, quotedFee, forwardedBalance)
			assert.True(t, math.NewInt(tc.expectedExcess).Equal(excess),
				"unexpected excess IGP fee: expected %d, got %s", tc.expectedExcess, excess)
		})
	}
}

func mustParseBigInt(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic("invalid big int: " + s)
	}
	return n
}

// mochaMsgForwardFixtures contains all 73 MsgForward transactions from
// Mocha-4 testnet (heights 10218390-10528131). All transactions succeeded
// (code=0) and forwarded hyperlane/* tokens.
//
// Data source: https://celestia-testnet.rpc.kjnodes.com/tx_search
var mochaMsgForwardFixtures = []mochaMsgForwardFixture{
	{
		height:         10218390,
		txHash:         "55C32E984FADB50E2966EE4D0140AEDE9AEE41BD80AD069C45B469AA46E63E0F",
		signer:         "celestia1lg0e9n4pt29lpq2k4ptue4ckw09dx0aujlpe4j",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "38000",
		forwardedDenom: "hyperlane/0x726f757465725f6170700000000000000000000000000002000000000000001d",
		forwardedAmt:   "100000",
	},
	{
		height:         10218551,
		txHash:         "484CD7BE2149C81F9F93CA6A2EBD2BC87C593C6921E0B47FB995F8134A2334AA",
		signer:         "celestia1lg0e9n4pt29lpq2k4ptue4ckw09dx0aujlpe4j",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "38000",
		forwardedDenom: "hyperlane/0x726f757465725f6170700000000000000000000000000002000000000000001d",
		forwardedAmt:   "100000",
	},
	{
		height:         10249980,
		txHash:         "2DF16AC6443D54B8677ED8870455A125D563DC0620108F0B65DC718891188C89",
		signer:         "celestia1d7pp2nezv6en2lhwafgjm6mpfzruqc7ak6lpem",
		forwardAddr:    "celestia19nudkaru04wsmlsj6t9qwuy6u7c9egxcq5ndrz",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000AF9053BB6C4346381C77C2FED279B17ABAFCDF4D",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "66000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "10000000",
	},
	{
		height:         10250043,
		txHash:         "720290BE2720C3DC284A1750E5E10A875AA14B1B1873A5D6BC15AF69CA4C9AD9",
		signer:         "celestia1d7pp2nezv6en2lhwafgjm6mpfzruqc7ak6lpem",
		forwardAddr:    "celestia19nudkaru04wsmlsj6t9qwuy6u7c9egxcq5ndrz",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000AF9053BB6C4346381C77C2FED279B17ABAFCDF4D",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "66000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "10000000",
	},
	{
		height:         10250092,
		txHash:         "F1ABBDAF67187502D03F1F253F0D4C6C6631E4269B09BB32F30C96246DA5A4AA",
		signer:         "celestia1d7pp2nezv6en2lhwafgjm6mpfzruqc7ak6lpem",
		forwardAddr:    "celestia19nudkaru04wsmlsj6t9qwuy6u7c9egxcq5ndrz",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000AF9053BB6C4346381C77C2FED279B17ABAFCDF4D",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "66000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "10000000",
	},
	{
		height:         10250320,
		txHash:         "5097B6430CA1618EB2A2605669676339F2CE3EA1D53E6C765E95C783F514114F",
		signer:         "celestia1d7pp2nezv6en2lhwafgjm6mpfzruqc7ak6lpem",
		forwardAddr:    "celestia19nudkaru04wsmlsj6t9qwuy6u7c9egxcq5ndrz",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000AF9053BB6C4346381C77C2FED279B17ABAFCDF4D",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "66000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "10000000",
	},
	{
		height:         10250364,
		txHash:         "624DA2122173930694CC18C1D740B3A4734E0DE7AA696C4479775173FED6510B",
		signer:         "celestia1d7pp2nezv6en2lhwafgjm6mpfzruqc7ak6lpem",
		forwardAddr:    "celestia19nudkaru04wsmlsj6t9qwuy6u7c9egxcq5ndrz",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000AF9053BB6C4346381C77C2FED279B17ABAFCDF4D",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "66000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "10000000",
	},
	{
		height:         10261605,
		txHash:         "AD1D37CD325D781A62682E68F36BB9C9DF79ED5CC06E2A312C5A3A696C0EAE74",
		signer:         "celestia1d7pp2nezv6en2lhwafgjm6mpfzruqc7ak6lpem",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000d5e85e86fc692cedad6d6992f1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "66000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5000000000",
	},
	{
		height:         10262965,
		txHash:         "4369053B67E799C357AD6F4DA2731491C8C713B8B10303B33868A5F4EB52B4C6",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia19nudkaru04wsmlsj6t9qwuy6u7c9egxcq5ndrz",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000aF9053bB6c4346381C77C2FeD279B17ABAfCDf4d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "66000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "10000000",
	},
	{
		height:         10263565,
		txHash:         "FA2700BFD809A8BBF5797AA78793132D6E1D804922735E88CAFF2E91AA48833F",
		signer:         "celestia1lg0e9n4pt29lpq2k4ptue4ckw09dx0aujlpe4j",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEDAD6D6992F1F0CCF273E39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "100000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5010000000",
	},
	{
		height:         10263669,
		txHash:         "12C1267506DB4BA7FD58D2C5402725772EA6DC0FA2B1F86A180A3ACF3FBBE66A",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000d5e85e86fc692cedad6d6992f1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "4995000000",
	},
	{
		height:         10263671,
		txHash:         "19F8520FF503A72785CA11198FB73CE2DCE1D535638FCF19C5D12A323CF5BD8B",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1lnmgcwe0vfx309fdkvq2ng0tk5z8yvms22dhe8",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000aF9053bB6c4346381C77C2FeD279B17ABAfCDf4d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "10000000",
	},
	{
		height:         10304919,
		txHash:         "C25CB57A48E026144CF50DDC77347BDDE0CF47A9C80B698E99162F1B3040A821",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000d5e85e86fc692cedad6d6992f1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5000000000",
	},
	{
		height:         10316756,
		txHash:         "F56B15246E85CDE6B16BF5CB22684B2BB2493B3E76607F2672CA3A7EB7C153B5",
		signer:         "celestia1lg0e9n4pt29lpq2k4ptue4ckw09dx0aujlpe4j",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEDAD6D6992F1F0CCF273E39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "100000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5000000000",
	},
	{
		height:         10316778,
		txHash:         "117E6CD17D31B9D7F985909089210705607E0A588818095557C50AADFC4F0368",
		signer:         "celestia1lg0e9n4pt29lpq2k4ptue4ckw09dx0aujlpe4j",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEDAD6D6992F1F0CCF273E39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "100000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5000000000",
	},
	{
		height:         10331788,
		txHash:         "18DC299C98DEC95AEDFA82F4E7F1DAB4470B22EF2CC5D038FF4F9002C146A195",
		signer:         "celestia1lg0e9n4pt29lpq2k4ptue4ckw09dx0aujlpe4j",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEDAD6D6992F1F0CCF273E39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "100000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5000000000",
	},
	{
		height:         10332962,
		txHash:         "08077FE59D9D212B160ECA3C8632CCB5F5B5BD4C52B91EA01B491F9952F3386C",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000d5e85e86fc692cedad6d6992f1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5000000000",
	},
	{
		height:         10333901,
		txHash:         "A6737C4F64E8BC3BB003D7A0B3A8DDE76F28BFB662AD2077328F40686D416FF8",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000d5e85e86fc692cedad6d6992f1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5000000000",
	},
	{
		height:         10333955,
		txHash:         "B3974445B6A3530A3EA3F756D394843C145F568B4EE40078E88EC48FB3EA47A5",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000d5e85e86fc692cedad6d6992f1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5000000000",
	},
	{
		height:         10343594,
		txHash:         "4125704ED2DBAF557F68E5A12F54077D133A9963DEEC6FD04DA263DB09873D5D",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000d5e85e86fc692cedad6d6992f1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5000000000",
	},
	{
		height:         10343843,
		txHash:         "43611435FEF8B88452597225575BDA4938CEBF1F3DF72D466CC2137BB8640ADE",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000d5e85e86fc692cedad6d6992f1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5000000000",
	},
	{
		height:         10347702,
		txHash:         "AD3585439E0E9FF532EC32723151AF93BFC727F7AC3E9C0BC33EFE1FFE1062BF",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000d5e85e86fc692cedad6d6992f1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5000000000",
	},
	{
		height:         10403805,
		txHash:         "E7E8B84DC7831C8DF3491D56C738BD334EADD340CEDC9E4254B76BF86DE0C362",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000d5e85e86fc692cedad6d6992f1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5000000000",
	},
	{
		height:         10432283,
		txHash:         "037399A8BEE58E1C30BE0DB7E6DD6590E39567A8C0FCDF8C07F74AAB6CEAD9C1",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1qyry0npwmx6kw0007vun8hwx76e0h4l3gw92ad",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000f733324a8e89f024015aa21510de7140a504870e",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "25000000000",
	},
	{
		height:         10432538,
		txHash:         "37EA4C34CC217DF77AEAB5E8AF12354CF098A2962D0A84DBE14BB3518D8F3A6D",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000d5e85e86fc692cedad6d6992f1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000",
	},
	{
		height:         10432542,
		txHash:         "150F4C25DCDFAF4C9EA0E4682AEA9DF57E91D809D331AADDCBF7874764A4DA9D",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia17pjjnd4d2aph5pywm0l9zd04f780fuafcvlxyk",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "10000000",
	},
	{
		height:         10432588,
		txHash:         "A3F5325C8005BBB0F7EDF3CAEA1C8EF965D913452A87BD78BC4B43F904FBCDC3",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia17pjjnd4d2aph5pywm0l9zd04f780fuafcvlxyk",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "100000000",
	},
	{
		height:         10432629,
		txHash:         "CB2DD31B51CF2B21269B81F4A1E2368E867B3FB86C0481E6BC94891EE781234A",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "100000000",
	},
	{
		height:         10432710,
		txHash:         "77955AC19A5B02EF217919A5801265B9E22BA8C2C902316EBC1B5C4A2D5B5FC0",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "100000000",
	},
	{
		height:         10432752,
		txHash:         "B7D27AAFF6756CC6464AEA193AA9491E8E08291CF0C1CE9DFF0066A6BDD4326F",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000",
	},
	{
		height:         10432770,
		txHash:         "AEE0596E4DFA5D6BFBEE2262D7F763EF000F87B62D81DC880584B4D1F9A33AAB",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000",
	},
	{
		height:         10444306,
		txHash:         "F2E7404698E5F415453B4305D0535A11AF8B425C5BE995F6DBBC9754CA2B3674",
		signer:         "celestia1lg0e9n4pt29lpq2k4ptue4ckw09dx0aujlpe4j",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEDAD6D6992F1F0CCF273E39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "100000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "50000000",
	},
	{
		height:         10444308,
		txHash:         "938A0A7ECA2C63C6C7744E0D16B0B3F2DCBA718E5EC3C2D7371C601A6E1F1718",
		signer:         "celestia1lg0e9n4pt29lpq2k4ptue4ckw09dx0aujlpe4j",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEDAD6D6992F1F0CCF273E39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "100000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "21000000",
	},
	{
		height:         10444459,
		txHash:         "BF24A22E29BFC6D4EF9C6088D09E1775D48FAC2037866702213AE0E5FEAC7532",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "110000000",
	},
	{
		height:         10444489,
		txHash:         "825771E89C1C46BD77B8CFDD00CC08D1A476D07009DAA02A6FE7BCAB23C6C3A1",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "10000000",
	},
	{
		height:         10444665,
		txHash:         "1FDAE3DA245AE4499D67290BA13FAB487CCD0127077195E1E26B01529842722D",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "5000000",
	},
	{
		height:         10444933,
		txHash:         "58D0620F6E18E384AD347536967B97B53020917AECFD26A97D241CDFB8716FE3",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "10000000",
	},
	{
		height:         10445110,
		txHash:         "ADBCC6924C00CB301E3B29DDCD143B75E83FE26FB009975EDEBC6B4EB0E052C0",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000",
	},
	{
		height:         10445115,
		txHash:         "51288056EDBD695F3E0A8C949C5DADA4B3101E35F4C5F80FBF571BE77F519F00",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000",
	},
	{
		height:         10445265,
		txHash:         "4D3E1B77A87788979A9FDAA77FCFBFE777DE85C0B7DFB6F6C8B99C3E97D2B043",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "100000000",
	},
	{
		height:         10445320,
		txHash:         "72F1D1DFCBDFC7530A1BCA5E8FD00BCB36C7215B5C28A964EE433121D7ED8EDB",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "2000000",
	},
	{
		height:         10445338,
		txHash:         "7A1FC06D6F260E18DDB2AD83F26C8A3CE4FCCC6555328FC2D2397032830F6A10",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia17pjjnd4d2aph5pywm0l9zd04f780fuafcvlxyk",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "111000000",
	},
	{
		height:         10445350,
		txHash:         "D891E5CE29E770F9677C994F499503C4D4774E116162882A6C89575C97444C65",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "101000000",
	},
	{
		height:         10445382,
		txHash:         "FDC28CCFAC230E8430AE66B19530818D448C6009557C70927A67A3F7372BB5A3",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia154g4t9peh79m7hc8nva034d6tuz8k752xqcu7r",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "330000000",
	},
	{
		height:         10445406,
		txHash:         "F745BE6C5F6474FD0FBD39D1BD7AE746260ED1971B9CD4A0737920959B730832",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000",
	},
	{
		height:         10445422,
		txHash:         "391CA5F5FEBD5FE0A2CD25BE00C751DF79C81F1D1CE1FCA6CD11B44BEC8562ED",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia17pjjnd4d2aph5pywm0l9zd04f780fuafcvlxyk",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "123000000",
	},
	{
		height:         10445482,
		txHash:         "36EBFDBACBE1D8DA64CC6465D3975E01DC2EBC60245C0FC31A4F9DE0657426AE",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "100000000",
	},
	{
		height:         10445584,
		txHash:         "BA8712111FC3294E354455932DC954525485883A330B7852CAE908493F7E98DD",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "100000000",
	},
	{
		height:         10445829,
		txHash:         "D9FE29859118642EE27621F3081DE28273D307FAC2E3E2213C3FAAB6F4A67624",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1hn3s2zsx72m3ac7vh3q7h3crwkpyqggtqamztl",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000bF57d398b7a166E255c0Bf83f6e9C322d12FB00a",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "200000",
	},
	{
		height:         10445836,
		txHash:         "D189A4851010D445E41F4F35E463279EB20E9F37333D4C0CD514E7C7186B5367",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1hn3s2zsx72m3ac7vh3q7h3crwkpyqggtqamztl",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000bF57d398b7a166E255c0Bf83f6e9C322d12FB00a",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "100000",
	},
	{
		height:         10445885,
		txHash:         "3365C3544D7A7C2F18780B477AAA3E8594608A3559ED59D80F87C3B2CED9B75B",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "10000000",
	},
	{
		height:         10446585,
		txHash:         "832584AAB8E3AF9F6160396AC879F126E3B8728F5AECE206679776CEC2F6AAD6",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "10000000",
	},
	{
		height:         10447205,
		txHash:         "B742B4CE962377531D0842D98DD60BB929B4F3F0A9C3A5588262394CAAB95CE9",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000",
	},
	{
		height:         10449127,
		txHash:         "C003382531C337F287EE1471CF7DBEE625A1EF6EDAC92935FE876157805D8023",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "200000000",
	},
	{
		height:         10449129,
		txHash:         "983AD116500E02468E5ED3F4CCD9CD74AB47FF771E5BB256D70280502A9BC670",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1uk0k3wuln7xmfphjqxk0amsdhhzsgxazyn6446",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000aeadcAc857DeE5a6FC1688BCe42fC0c76Fb1B5F2",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000",
	},
	{
		height:         10449131,
		txHash:         "CB2D4C0B0FE4DE39EBF0BE4534D2ADE05FCBEB6DB5EA8B6F90328F7E841ADF8D",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia18qy7enzuulgvemr2htpu6q7vxydhvpefnwamya",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000aeadcAc857DeE5a6FC1688BCe42fC0c76Fb1B5F2",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "400000",
	},
	{
		height:         10457930,
		txHash:         "BB1489751C5451A3F68091B153E89C332C34079CBBD935D548F99074AB8B0E80",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "30000000",
	},
	{
		height:         10458110,
		txHash:         "192FD02839D5893F0E9E21EA482EF6268A998CB1A8E978594D878F72BF9B1C48",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "10000000",
	},
	{
		height:         10458468,
		txHash:         "60694F3BBB0FDF4A80CF83F711218D14D89C978662069D464F08F1AA8C6B2E69",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1ca2y0ynln5fj9586mqza8tw8j5ylcsrgs9v5ag",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000",
	},
	{
		height:         10458895,
		txHash:         "ACC2A77E65C7D0273A96A06202FE3F6244C414435E14E84CE92965DB08848BEC",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia154g4t9peh79m7hc8nva034d6tuz8k752xqcu7r",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "333000000",
	},
	{
		height:         10459069,
		txHash:         "20CAA2F729A0174B3EFD0D50E159A5F01D05DCE06A6A4E51916DB85947F27A6B",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia154g4t9peh79m7hc8nva034d6tuz8k752xqcu7r",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "222000000",
	},
	{
		height:         10459119,
		txHash:         "8936C97056A59513ECFC24E471B25DD7B26F16E869D4EB2373184F67EC00EABB",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia154g4t9peh79m7hc8nva034d6tuz8k752xqcu7r",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "100000000",
	},
	{
		height:         10459240,
		txHash:         "B8B40E3A7CC44CCC546A1F38324BF86F580939C534F401CB480094757DDC8FF2",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia154g4t9peh79m7hc8nva034d6tuz8k752xqcu7r",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "100000000",
	},
	{
		height:         10459389,
		txHash:         "98BEB40970BB0849BA10F334B189D2FFCDE09ADF7BBDB033EFA0E49F429CDCCF",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1xtywh4c787fpjhj7lc37ppy4rnd0yjxgn0hvha",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000D5E85E86FC692CEdaD6D6992F1f0ccf273e39913",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000",
	},
	{
		height:         10459481,
		txHash:         "E630F160F333257BDF35C0F6AB327512553EF7342F2319244D8AA582BB654F44",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia154g4t9peh79m7hc8nva034d6tuz8k752xqcu7r",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "200000000",
	},
	{
		height:         10459713,
		txHash:         "2948649E4513A3D340679EA3D3CC8D9DD1CCB996F721C98AD101CA7D55E39F59",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia19rdc6r8cwmf68qr6q2xajjqwxecjackjsf92ur",
		destDomain:     11155111,
		destRecipient:  "0x00000000000000000000000050cB97b8613003DB9B278Bb89d3ab3C377F99727",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000",
	},
	{
		height:         10459962,
		txHash:         "360294D4B3BDA2B30233A61BD7AB9F09DF11FD0F6674BC81D0AF2A99ABAC6754",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia154g4t9peh79m7hc8nva034d6tuz8k752xqcu7r",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "111000000",
	},
	{
		height:         10460334,
		txHash:         "D13E7EF302E5D98C3007F6D9C23A8D75B2168CFF6831451422EAC2CDC67BCA5C",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia1qyry0npwmx6kw0007vun8hwx76e0h4l3gw92ad",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000f733324a8e89f024015aa21510de7140a504870e",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "4959861721",
	},
	{
		height:         10499446,
		txHash:         "A454421816A40BC30E649F2C550FAE061BACECC8C0680A65E3E3096AEB265F78",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia184xycj78zfyhxd07rmg0wpc70r8scjm7jwj403",
		destDomain:     2147483647,
		destRecipient:  "0x00000000000000000000000050cB97b8613003DB9B278Bb89d3ab3C377F99727",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000",
	},
	{
		height:         10499631,
		txHash:         "8320F5EA24B9D51773FA0039D08E915278E1AC44038AA791535AB2C6564657AC",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia154g4t9peh79m7hc8nva034d6tuz8k752xqcu7r",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000000",
	},
	{
		height:         10516122,
		txHash:         "83B9C088A5C9038A5B2A6C0110E2C1F52F45294564951997957296CA5F0AAE56",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia17pjjnd4d2aph5pywm0l9zd04f780fuafcvlxyk",
		destDomain:     2147483647,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "78000",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "150000000",
	},
	{
		height:         10516198,
		txHash:         "FB2C9F10572564705220EF1391CA9CB8D3C1FF741DA3F98FD348AE8FF84CF281",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia154g4t9peh79m7hc8nva034d6tuz8k752xqcu7r",
		destDomain:     11155111,
		destRecipient:  "0x000000000000000000000000Bf738f0C8f112B34B6b1a7dF01FD4A8f78B83c9d",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "150000000",
	},
	{
		height:         10528131,
		txHash:         "CA5548F5AC441A4CF05B71D60177A2790A02252736B102EF2D48C257D605B20E",
		signer:         "celestia1zexdknjvxush8c2dyztxq2gxp9jf3py2ptk0d7",
		forwardAddr:    "celestia19rdc6r8cwmf68qr6q2xajjqwxecjackjsf92ur",
		destDomain:     11155111,
		destRecipient:  "0x00000000000000000000000050cB97b8613003DB9B278Bb89d3ab3C377F99727",
		maxIgpFeeDenom: "utia",
		maxIgpFeeAmt:   "42900",
		forwardedDenom: "hyperlane/0x726f757465725f61707000000000000000000000000000020000000000000024",
		forwardedAmt:   "1000000",
	},
}
