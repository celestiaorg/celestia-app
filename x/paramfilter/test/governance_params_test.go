package test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/x/paramfilter"
	"github.com/stretchr/testify/suite"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	bsmoduletypes "github.com/celestiaorg/celestia-app/x/blobstream/types"
	minttypes "github.com/celestiaorg/celestia-app/x/mint/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	params "github.com/cosmos/cosmos-sdk/x/params"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
)

type TestSuite struct {
	suite.Suite

	app        *app.App
	ctx        sdk.Context
	govHandler v1beta1.Handler
	pph        paramfilter.ParamBlockList
}

func (suite *TestSuite) SetupTest() {
	suite.app, _ = testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	suite.ctx = suite.app.BaseApp.NewContext(false, tmproto.Header{})
	suite.govHandler = params.NewParamChangeProposalHandler(suite.app.ParamsKeeper)
	suite.pph = paramfilter.NewParamBlockList([2]string{})

	minter := minttypes.DefaultMinter()
	suite.app.MintKeeper.SetMinter(suite.ctx, minter)
}

func TestTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping handler test suite in short mode.")
	}
	suite.Run(t, new(TestSuite))
}

func (suite *TestSuite) TestModifiableParameters() {
	testCases := []struct {
		name     string
		proposal *proposal.ParameterChangeProposal
		subTest  func()
	}{
		{
			"auth.MaxMemoCharacters",
			testProposal(proposal.ParamChange{
				Subspace: authtypes.ModuleName,
				Key:      string(authtypes.KeyMaxMemoCharacters),
				Value:    `"2"`,
			}),
			func() {
				gotMaxMemoCharacters := suite.app.AccountKeeper.GetParams(suite.ctx).MaxMemoCharacters
				wantMaxMemoCharacters := uint64(2)
				suite.Require().Equal(wantMaxMemoCharacters, gotMaxMemoCharacters)
			},
		},
		{
			"auth.SigVerifyCostED25519",
			testProposal(proposal.ParamChange{
				Subspace: authtypes.ModuleName,
				Key:      string(authtypes.KeySigVerifyCostED25519),
				Value:    `"2"`,
			}),
			func() {
				gotSigVerifyCostED25519 := suite.app.AccountKeeper.GetParams(suite.ctx).SigVerifyCostED25519
				wantSigVerifyCostED25519 := uint64(2)
				suite.Require().Equal(wantSigVerifyCostED25519, gotSigVerifyCostED25519)
			},
		},
		{
			"auth.SigVerifyCostSecp256k1",
			testProposal(proposal.ParamChange{
				Subspace: authtypes.ModuleName,
				Key:      string(authtypes.KeySigVerifyCostSecp256k1),
				Value:    `"2"`,
			}),
			func() {
				gotSigVerifyCostSecp256k1 := suite.app.AccountKeeper.GetParams(suite.ctx).SigVerifyCostSecp256k1
				wantSigVerifyCostSecp256k1 := uint64(2)
				suite.Require().Equal(wantSigVerifyCostSecp256k1, gotSigVerifyCostSecp256k1)
			},
		},
		{
			"auth.TxSigLimit",
			testProposal(proposal.ParamChange{
				Subspace: authtypes.ModuleName,
				Key:      string(authtypes.KeyTxSigLimit),
				Value:    `"2"`,
			}),
			func() {
				gotTxSigLimit := suite.app.AccountKeeper.GetParams(suite.ctx).TxSigLimit
				wantTxSigLimit := uint64(2)
				suite.Require().Equal(wantTxSigLimit, gotTxSigLimit)
			},
		},
		{
			"auth.TxSizeCostPerByte",
			testProposal(proposal.ParamChange{
				Subspace: authtypes.ModuleName,
				Key:      string(authtypes.KeyTxSizeCostPerByte),
				Value:    `"2"`,
			}),
			func() {
				gotTxSizeCostPerByte := suite.app.AccountKeeper.GetParams(suite.ctx).TxSizeCostPerByte
				wantTxSizeCostPerByte := uint64(2)
				suite.Require().Equal(wantTxSizeCostPerByte, gotTxSizeCostPerByte)
			},
		},
		{
			"blob.GasPerBlobByte",
			testProposal(proposal.ParamChange{
				Subspace: blobtypes.ModuleName,
				Key:      string(blobtypes.KeyGasPerBlobByte),
				Value:    `2`,
			}),
			func() {
				gotGasPerBlobByte := suite.app.BlobKeeper.GetParams(suite.ctx).GasPerBlobByte
				wantGasPerBlobByte := uint32(2)
				suite.Require().Equal(
					wantGasPerBlobByte,
					gotGasPerBlobByte)
			},
		},
		{
			"blob.GovMaxSquareSize",
			testProposal(proposal.ParamChange{
				Subspace: blobtypes.ModuleName,
				Key:      string(blobtypes.KeyGovMaxSquareSize),
				Value:    `"2"`,
			}),
			func() {
				gotGovMaxSquareSize := suite.app.BlobKeeper.GetParams(suite.ctx).GovMaxSquareSize
				wantGovMaxSquareSize := uint64(2)
				suite.Require().Equal(
					wantGovMaxSquareSize,
					gotGovMaxSquareSize)
			},
		},
		{
			"blobstream.DataCommitmentWindow",
			testProposal(proposal.ParamChange{
				Subspace: bsmoduletypes.ModuleName,
				Key:      string(bsmoduletypes.ParamsStoreKeyDataCommitmentWindow),
				Value:    `"100"`,
			}),
			func() {
				gotDataCommitmentWindow := suite.app.BlobstreamKeeper.GetParams(suite.ctx).DataCommitmentWindow
				wantDataCommitmentWindow := uint64(100)
				suite.Require().Equal(
					wantDataCommitmentWindow,
					gotDataCommitmentWindow)
			},
		},
		{
			"consensus.block",
			testProposal(proposal.ParamChange{
				Subspace: baseapp.Paramspace,
				Key:      string(baseapp.ParamStoreKeyBlockParams),
				Value:    `{"max_bytes": "1", "max_gas": "2"}`,
			}),
			func() {
				gotMaxBytes := suite.app.BaseApp.GetConsensusParams(suite.ctx).Block.MaxBytes
				wantMaxBytes := int64(1)
				suite.Require().Equal(
					wantMaxBytes,
					gotMaxBytes)

				gotMaxGas := suite.app.BaseApp.GetConsensusParams(suite.ctx).Block.MaxGas
				wantMaxGas := int64(2)
				suite.Require().Equal(
					wantMaxGas,
					gotMaxGas)
			},
		},
		{
			"consensus.evidence",
			testProposal(proposal.ParamChange{
				Subspace: baseapp.Paramspace,
				Key:      string(baseapp.ParamStoreKeyEvidenceParams),
				Value:    `{"max_age_duration": "1", "max_age_num_blocks": "2", "max_bytes": "3"}`,
			}),
			func() {
				gotMaxAgeDuration := suite.app.BaseApp.GetConsensusParams(suite.ctx).Evidence.MaxAgeDuration
				wantMaxAgeDuration := time.Duration(1)
				suite.Require().Equal(
					wantMaxAgeDuration,
					gotMaxAgeDuration)

				gotMaxAgeNumBlocks := suite.app.BaseApp.GetConsensusParams(suite.ctx).Evidence.MaxAgeNumBlocks
				wantMaxAgeNumBlocks := int64(2)
				suite.Require().Equal(
					wantMaxAgeNumBlocks,
					gotMaxAgeNumBlocks)

				gotMaxBytes := suite.app.BaseApp.GetConsensusParams(suite.ctx).Evidence.MaxBytes
				wantMaxBytes := int64(3)
				suite.Require().Equal(
					wantMaxBytes,
					gotMaxBytes)
			},
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			validationErr := suite.govHandler(suite.ctx, tc.proposal)
			suite.Require().NoError(validationErr)
			tc.subTest()
		})
	}
}
