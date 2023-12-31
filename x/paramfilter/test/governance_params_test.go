package test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/x/paramfilter"
	"github.com/stretchr/testify/suite"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
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
			"consensus.block.MaxGas",
			testProposal(proposal.ParamChange{
				Subspace: baseapp.Paramspace,
				Key:      string(baseapp.ParamStoreKeyBlockParams),
				Value:    `{"max_bytes": "1", "max_gas": "2"}`,
			}),
			func() {
				gotMaxGas := suite.app.BaseApp.GetConsensusParams(suite.ctx).Block.MaxGas
				wantMaxGas := int64(2)
				suite.Require().Equal(
					wantMaxGas,
					gotMaxGas)
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
