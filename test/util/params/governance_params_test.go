package params_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	params "github.com/cosmos/cosmos-sdk/x/params"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
)

type HandlerTestSuite struct {
	suite.Suite

	app        *simapp.SimApp
	ctx        sdk.Context
	govHandler govv1beta1.Handler
}

func (suite *HandlerTestSuite) SetupTest() {
	suite.app = simapp.Setup(suite.T(), false)
	suite.ctx = suite.app.BaseApp.NewContext(false, tmproto.Header{})
	suite.govHandler = params.NewParamChangeProposalHandler(suite.app.ParamsKeeper)
}

func TestHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(HandlerTestSuite))
}

func testProposal(changes ...proposal.ParamChange) *proposal.ParameterChangeProposal {
	return proposal.NewParameterChangeProposal("title", "description", changes)
}

func (suite *HandlerTestSuite) TestUnmodifiableParameters() {
	testCases := []struct {
		name     string
		proposal *proposal.ParameterChangeProposal
		onHandle func()
		expErr   bool
	}{
		// This parameters below should not be modifiable, however the tests show they are.
		{
			"conensus.block",
			testProposal(proposal.ParamChange{
				Subspace: baseapp.Paramspace,
				Key:      string(baseapp.ParamStoreKeyBlockParams),
				Value:    `{"max_bytes": "100", "max_gas": "100"}`,
			}),
			func() {
				blockParams := suite.app.BaseApp.GetConsensusParams(suite.ctx).Block
				suite.Require().Equal(abci.BlockParams{
					MaxBytes: 100,
					MaxGas:   100,
				}, *blockParams)
			},
			false,
		},
		{
			"conensus.validator.PubKeyTypes",
			testProposal(proposal.ParamChange{
				Subspace: baseapp.Paramspace,
				Key:      string(baseapp.ParamStoreKeyValidatorParams),
				Value:    `{"pub_key_types": ["secp256k1"]}`,
			}),
			func() {
				validatorParams := suite.app.BaseApp.GetConsensusParams(suite.ctx).Validator
				suite.Require().Equal(
					tmproto.ValidatorParams{
						PubKeyTypes: []string{"secp256k1"},
					},
					*validatorParams)
			},
			false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		suite.Run(tc.name, func() {
			validationErr := suite.govHandler(suite.ctx, tc.proposal)
			if tc.expErr {
				suite.Require().Error(validationErr)
			} else {
				suite.Require().NoError(validationErr)

			}
		})
	}
}
