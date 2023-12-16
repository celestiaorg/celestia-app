package params_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	params "github.com/cosmos/cosmos-sdk/x/params"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
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
			"bank.SendEnabled",
			testProposal(proposal.ParamChange{
				Subspace: banktypes.ModuleName,
				Key:      string(banktypes.KeySendEnabled),
				Value:    `[{"denom": "test", "enabled": false}]`,
			}),
			func() {
				sendEnabledParams := suite.app.BankKeeper.GetParams(suite.ctx).SendEnabled
				suite.Require().Equal([]*banktypes.SendEnabled{banktypes.NewSendEnabled("test", false)}, sendEnabledParams)
			},
			false,
		},
		// TimeIotaMs is not in conensus.block
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
		{
			"consensus.Version.AppVersion",
			testProposal(proposal.ParamChange{
				Subspace: baseapp.Paramspace,
				Key:      string(baseapp.ParamStoreKeyVersionParams),
				Value:    `{"app_version": "3"}`,
			}),
			func() {
				versionParams := suite.app.BaseApp.GetConsensusParams(suite.ctx).Version
				suite.Require().Equal(
					tmproto.VersionParams{
						AppVersion: 3,
					},
					*versionParams)
			},
			false,
		},
		{
			"mint.MintDenom",
			testProposal(proposal.ParamChange{
				Subspace: minttypes.ModuleName,
				Key:      string(minttypes.KeyMintDenom),
				Value:    `"test"`,
			}),
			func() {
				mintParams := suite.app.MintKeeper.GetParams(suite.ctx)
				suite.Require().Equal(
					mintParams.MintDenom,
					"test")
			},
			false,
		},
		{
			"staking.BondDenom",
			testProposal(proposal.ParamChange{
				Subspace: stakingtypes.ModuleName,
				Key:      string(stakingtypes.KeyBondDenom),
				Value:    `"test"`,
			}),
			func() {
				stakingParams := suite.app.StakingKeeper.GetParams(suite.ctx)
				suite.Require().Equal(
					stakingParams.BondDenom,
					"test")
			},
			false,
		},
		{
			"staking.MaxValidators",
			testProposal(proposal.ParamChange{
				Subspace: stakingtypes.ModuleName,
				Key:      string(stakingtypes.KeyMaxValidators),
				Value:    `1`,
			}),
			func() {
				stakingParams := suite.app.StakingKeeper.GetParams(suite.ctx)
				suite.Require().Equal(
					stakingParams.MaxValidators,
					uint32(1))
			},
			false,
		},
		{
			"staking.UnbondingTime",
			testProposal(proposal.ParamChange{
				Subspace: stakingtypes.ModuleName,
				Key:      string(stakingtypes.KeyUnbondingTime),
				Value:    `"1"`,
			}),
			func() {
				stakingParams := suite.app.StakingKeeper.GetParams(suite.ctx)
				suite.Require().Equal(
					stakingParams.UnbondingTime,
					time.Duration(1))
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
				tc.onHandle()
			}
		})
	}
}
