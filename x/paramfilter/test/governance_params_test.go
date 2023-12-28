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
	suite.Run(t, new(TestSuite))
}

func (suite *TestSuite) TestUnmodifiableParameters() {
	testCases := []struct {
		name     string
		proposal *proposal.ParameterChangeProposal
		subTest     func()
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
		},
		{
			"consensus.block.TimeIotaMs",
			testProposal(proposal.ParamChange{
				Subspace: baseapp.Paramspace,
				Key:      string(baseapp.ParamStoreKeyBlockParams),
				Value:    `{"max_bytes": "1", "max_gas": "1", "time_iota_ms": "1"}`,
			}),
			func() {
				// need to determine if ConsensusParams.Block should be BlockParams from proto/tendermint/types instead of abci/types
				blockParams := suite.app.BaseApp.GetConsensusParams(suite.ctx).Block
				suite.Require().Equal(
					tmproto.BlockParams{
						MaxBytes:   1,
						MaxGas:     1,
						TimeIotaMs: 1,
					},
					*blockParams)
			},
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
