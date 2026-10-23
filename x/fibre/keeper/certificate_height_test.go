package keeper_test

import (
	"context"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/fibre/validator"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	core "github.com/cometbft/cometbft/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// TestCertificateVerifiesAgainstSigningSet pins the height the chain reads the
// certificate's validator set from. Clients sign over CometBFT's set at the
// promise height H, which staking records as historical info H-1; historical
// info H already holds the next set. A delegation in block H-1 that reorders
// two validators must not invalidate a certificate built over the set at H.
func (suite *MsgServerTestSuite) TestCertificateVerifiesAgainstSigningSet() {
	const powerReduction = 1_000_000
	powers := []int64{400, 300, 100, 99, 60, 41}
	keys := make([]ed25519.PrivKey, len(powers))
	cometVals := make([]*core.Validator, len(powers))
	for i, power := range powers {
		keys[i] = ed25519.GenPrivKey()
		cometVals[i] = core.NewValidator(keys[i].PubKey(), power)
	}
	// signingSet is what BlockAPI ValidatorSet(H) returns to the client.
	signingSet := validator.Set{ValidatorSet: core.NewValidatorSet(cometVals), Height: uint64(suite.ctx.BlockHeight())}

	historicalInfo := func(height int64, extraPower []int64) stakingtypes.HistoricalInfo {
		vals := make([]stakingtypes.Validator, len(powers))
		for i, power := range powers {
			pk, err := cryptocodec.FromCmtPubKeyInterface(keys[i].PubKey())
			suite.Require().NoError(err)
			anyPubKey, err := codectypes.NewAnyWithValue(pk)
			suite.Require().NoError(err)
			vals[i] = stakingtypes.Validator{
				OperatorAddress: sdk.ValAddress(keys[i].PubKey().Address()).String(),
				ConsensusPubkey: anyPubKey,
				Tokens:          math.NewInt((power + extraPower[i]) * powerReduction),
				Status:          stakingtypes.Bonded,
			}
		}
		return stakingtypes.NewHistoricalInfo(cmtproto.Header{Height: height},
			stakingtypes.Validators{Validators: vals}, math.NewInt(powerReduction))
	}
	height := suite.ctx.BlockHeight()
	suite.stakingKeeper.historicalInfo = map[int64]stakingtypes.HistoricalInfo{
		height - 1: historicalInfo(height-1, []int64{0, 0, 0, 0, 0, 0}),
		// validator 3 gained 2 power in block H-1 and now outranks validator 2.
		height: historicalInfo(height, []int64{0, 0, 0, 2, 0, 0}),
	}

	signerPubKey, privKey, _ := suite.newSigner()
	promise := suite.createPaymentPromise(signerPubKey, privKey)
	pp := fibre.PaymentPromise{}
	suite.Require().NoError(pp.FromProto(&promise))
	signBytes, err := pp.SignBytes()
	suite.Require().NoError(err)

	// Validator 1 is slow, so quorum reaches the reordered validators.
	sigSet := signingSet.NewSignatureSet(fibre.DefaultProtocolParams.SafetyThreshold, signBytes)
	for _, i := range []int{0, 2, 3, 4, 5} {
		val, found := signingSet.GetByAddress(keys[i].PubKey().Address())
		suite.Require().True(found)
		signature, err := keys[i].Sign(signBytes)
		suite.Require().NoError(err)
		if done, err := sigSet.Add(val, signature); suite.NoError(err) && done {
			break
		}
	}
	signatures, err := sigSet.Signatures()
	suite.Require().NoError(err)

	msg := &types.MsgPayForFibre{PaymentPromise: promise, ValidatorSignatures: signatures}
	suite.Require().NoError(suite.keeper.ValidatePayForFibreSignatures(suite.ctx, msg))
}

// TestValidatorSetAvailableReadsSigningSet checks that the ante availability check
// asks for the same historical height signature verification reads. CheckTx runs
// on the state of H-1 while a client can already see H, so only H-1 is stored.
func (suite *MsgServerTestSuite) TestValidatorSetAvailableReadsSigningSet() {
	height := suite.ctx.BlockHeight()
	suite.stakingKeeper.historicalInfo = map[int64]stakingtypes.HistoricalInfo{
		height - 1: {Header: cmtproto.Header{Height: height - 1}},
	}
	suite.stakingKeeper.GetHistoricalInfoFn = func(_ context.Context, h int64) (stakingtypes.HistoricalInfo, error) {
		info, ok := suite.stakingKeeper.historicalInfo[h]
		if !ok {
			return stakingtypes.HistoricalInfo{}, stakingtypes.ErrNoHistoricalInfo
		}
		return info, nil
	}
	defer func() { suite.stakingKeeper.GetHistoricalInfoFn = nil }()

	suite.Require().NoError(suite.keeper.ValidateValidatorSetAvailable(suite.ctx, height))
	suite.Require().Error(suite.keeper.ValidateValidatorSetAvailable(suite.ctx, height-1))
}
