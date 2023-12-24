package test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/x/paramfilter"
	"github.com/stretchr/testify/suite"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	testutil "github.com/celestiaorg/celestia-app/test/util"
	minttypes "github.com/celestiaorg/celestia-app/x/mint/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	params "github.com/cosmos/cosmos-sdk/x/params"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

type HandlerTestSuite struct {
	suite.Suite

	app        *app.App
	ctx        sdk.Context
	govHandler v1beta1.Handler
	pph        paramfilter.ParamBlockList
}

func (suite *HandlerTestSuite) SetupTest() {
	suite.app, _ = testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	suite.ctx = suite.app.BaseApp.NewContext(false, tmproto.Header{})
	suite.govHandler = params.NewParamChangeProposalHandler(suite.app.ParamsKeeper)
	suite.pph = paramfilter.NewParamBlockList([2]string{})

	minter := minttypes.DefaultMinter()
	suite.app.MintKeeper.SetMinter(suite.ctx, minter)
}

func TestHandlerTestSuite(t *testing.T) {
	suite.Run(t, new(HandlerTestSuite))
}

func (suite *HandlerTestSuite) TestUnmodifiableParameters() {
	testCases := []struct {
		name     string
		proposal *proposal.ParameterChangeProposal
		onHandle func()
		expErr   bool
	}{
		// The tests below show the parameters as modifiable, block in App.BlockedParams().
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
		// TimeIotaMs is not in conensus.block, can not submit a governance proposal to modify it.
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
		// {
		// 	"mint.BondDenom",
		// 	testProposal(proposal.ParamChange{
		// 		Subspace: minttypes.ModuleName,
		// 		Key:      string(minttypes.KeyMinter),
		// 		Value:    `{"inflation_rate": "1", "annual_provisions": "3", "PreviousBlockTime": "1", "bond_denom": "test"}`,
		// 	}),
		// 	func() {
		// 		mintParams := suite.app.MintKeeper.GetMinter(suite.ctx)
		// 		suite.Require().Equal(
		// 			mintParams.BondDenom,
		// 			"test")
		// 	},
		// 	false,
		// },
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
