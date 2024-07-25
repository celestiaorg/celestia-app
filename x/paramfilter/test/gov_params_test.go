package test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	bsmoduletypes "github.com/celestiaorg/celestia-app/v3/x/blobstream/types"
	minfeetypes "github.com/celestiaorg/celestia-app/v3/x/minfee"
	"github.com/celestiaorg/celestia-app/v3/x/paramfilter"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	icahosttypes "github.com/cosmos/ibc-go/v6/modules/apps/27-interchain-accounts/host/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v6/modules/apps/transfer/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	ibcconnectiontypes "github.com/cosmos/ibc-go/v6/modules/core/03-connection/types"
	"github.com/stretchr/testify/suite"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

type GovParamsTestSuite struct {
	suite.Suite

	app        *app.App
	ctx        sdk.Context
	govHandler v1beta1.Handler
}

func (suite *GovParamsTestSuite) SetupTest() {
	suite.app, _ = testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	suite.ctx = suite.app.BaseApp.NewContext(false, tmproto.Header{})
	suite.govHandler = paramfilter.NewParamBlockList(suite.app.BlockedParams()...).GovHandler(suite.app.ParamsKeeper)
}

func TestGovParamsTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping gov params test suite in short mode.")
	}
	suite.Run(t, new(GovParamsTestSuite))
}

// TestModifiableParams verifies that the params listed as governance modifiable
// in the specs parameters.md file are modifiable via governance.
func (suite *GovParamsTestSuite) TestModifiableParams() {
	assert := suite.Assert()

	testCases := []struct {
		name         string
		proposal     *proposal.ParameterChangeProposal
		postProposal func()
	}{
		{
			"auth.MaxMemoCharacters",
			testProposal(proposal.ParamChange{
				Subspace: authtypes.ModuleName,
				Key:      string(authtypes.KeyMaxMemoCharacters),
				Value:    `"2"`,
			}),
			func() {
				got := suite.app.AccountKeeper.GetParams(suite.ctx).MaxMemoCharacters
				want := uint64(2)
				assert.Equal(want, got)
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
				got := suite.app.AccountKeeper.GetParams(suite.ctx).SigVerifyCostED25519
				want := uint64(2)
				assert.Equal(want, got)
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
				got := suite.app.AccountKeeper.GetParams(suite.ctx).SigVerifyCostSecp256k1
				want := uint64(2)
				assert.Equal(want, got)
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
				got := suite.app.AccountKeeper.GetParams(suite.ctx).TxSigLimit
				want := uint64(2)
				assert.Equal(want, got)
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
				got := suite.app.AccountKeeper.GetParams(suite.ctx).TxSizeCostPerByte
				want := uint64(2)
				assert.Equal(want, got)
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
				got := suite.app.BlobKeeper.GetParams(suite.ctx).GasPerBlobByte
				want := uint32(2)
				assert.Equal(want, got)
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
				got := suite.app.BlobKeeper.GetParams(suite.ctx).GovMaxSquareSize
				want := uint64(2)
				assert.Equal(want, got)
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
				got := suite.app.BlobstreamKeeper.GetParams(suite.ctx).DataCommitmentWindow
				want := uint64(100)
				assert.Equal(want, got)
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
				assert.Equal(wantMaxBytes, gotMaxBytes)

				gotMaxGas := suite.app.BaseApp.GetConsensusParams(suite.ctx).Block.MaxGas
				wantMaxGas := int64(2)
				assert.Equal(wantMaxGas, gotMaxGas)
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
				assert.Equal(wantMaxAgeDuration, gotMaxAgeDuration)

				gotMaxAgeNumBlocks := suite.app.BaseApp.GetConsensusParams(suite.ctx).Evidence.MaxAgeNumBlocks
				wantMaxAgeNumBlocks := int64(2)
				assert.Equal(wantMaxAgeNumBlocks, gotMaxAgeNumBlocks)

				gotMaxBytes := suite.app.BaseApp.GetConsensusParams(suite.ctx).Evidence.MaxBytes
				wantMaxBytes := int64(3)
				assert.Equal(wantMaxBytes, gotMaxBytes)
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
				got := *suite.app.BaseApp.GetConsensusParams(suite.ctx).Version
				want := tmproto.VersionParams{AppVersion: 3}
				assert.Equal(want, got)
			},
		},
		{
			"distribution.BaseProposerReward",
			testProposal(proposal.ParamChange{
				Subspace: distributiontypes.ModuleName,
				Key:      string(distributiontypes.ParamStoreKeyBaseProposerReward),
				Value:    `"1"`,
			}),
			func() {
				got := suite.app.DistrKeeper.GetParams(suite.ctx).BaseProposerReward
				want := sdk.NewDec(1)
				assert.Equal(want, got)
			},
		},
		{
			"distribution.BonusProposerReward",
			testProposal(proposal.ParamChange{
				Subspace: distributiontypes.ModuleName,
				Key:      string(distributiontypes.ParamStoreKeyBonusProposerReward),
				Value:    `"1"`,
			}),
			func() {
				got := suite.app.DistrKeeper.GetParams(suite.ctx).BonusProposerReward
				want := sdk.NewDec(1)
				assert.Equal(want, got)
			},
		},
		{
			"distribution.CommunityTax",
			testProposal(proposal.ParamChange{
				Subspace: distributiontypes.ModuleName,
				Key:      string(distributiontypes.ParamStoreKeyCommunityTax),
				Value:    `"1"`,
			}),
			func() {
				want := suite.app.DistrKeeper.GetParams(suite.ctx).CommunityTax
				got := sdk.NewDec(1)
				assert.Equal(got, want)
			},
		},
		{
			"distribution.WithdrawAddrEnabled",
			testProposal(proposal.ParamChange{
				Subspace: distributiontypes.ModuleName,
				Key:      string(distributiontypes.ParamStoreKeyWithdrawAddrEnabled),
				Value:    `false`,
			}),
			func() {
				got := suite.app.DistrKeeper.GetParams(suite.ctx).WithdrawAddrEnabled
				want := false
				assert.Equal(want, got)
			},
		},
		{
			"gov.DepositParams",
			testProposal(proposal.ParamChange{
				Subspace: "gov",
				Key:      string(govtypes.ParamStoreKeyDepositParams),
				Value:    `{"max_deposit_period": "1", "min_deposit": [{"denom": "test", "amount": "2"}]}`,
			}),
			func() {
				gotMaxDepositPeriod := *suite.app.GovKeeper.GetDepositParams(suite.ctx).MaxDepositPeriod
				wantMaxDepositPeriod := time.Duration(1)
				assert.Equal(wantMaxDepositPeriod, gotMaxDepositPeriod)

				gotMinDeposit := suite.app.GovKeeper.GetDepositParams(suite.ctx).MinDeposit
				wantMinDeposit := []sdk.Coin{{Denom: "test", Amount: sdk.NewInt(2)}}
				assert.Equal(wantMinDeposit, gotMinDeposit)
			},
		},
		{
			"gov.TallyParams",
			testProposal(proposal.ParamChange{
				Subspace: "gov",
				Key:      string(govtypes.ParamStoreKeyTallyParams),
				Value:    `{"quorum": "0.1", "threshold": "0.2", "veto_threshold": "0.3"}`,
			}),
			func() {
				gotQuroum := suite.app.GovKeeper.GetTallyParams(suite.ctx).Quorum
				wantQuorum := "0.1"
				assert.Equal(wantQuorum, gotQuroum)

				gotThreshold := suite.app.GovKeeper.GetTallyParams(suite.ctx).Threshold
				wantThreshold := "0.2"
				assert.Equal(wantThreshold, gotThreshold)

				gotVetoThreshold := suite.app.GovKeeper.GetTallyParams(suite.ctx).VetoThreshold
				wantVetoThreshold := "0.3"
				assert.Equal(wantVetoThreshold, gotVetoThreshold)
			},
		},
		{
			"gov.VotingParams.VotingPeriod",
			testProposal(proposal.ParamChange{
				Subspace: "gov",
				Key:      string(govtypes.ParamStoreKeyVotingParams),
				Value:    `{"voting_period": "2"}`,
			}),
			func() {
				got := *suite.app.GovKeeper.GetVotingParams(suite.ctx).VotingPeriod
				want := time.Duration(2)
				assert.Equal(want, got)
			},
		},
		{
			"ibc.ClientGenesis.AllowedClients",
			testProposal(proposal.ParamChange{
				Subspace: "ibc",
				Key:      string(ibcclienttypes.KeyAllowedClients),
				Value:    `["01-test"]`,
			}),
			func() {
				got := suite.app.IBCKeeper.ClientKeeper.GetParams(suite.ctx).AllowedClients
				want := []string{"01-test"}
				assert.Equal(want, got)
			},
		},
		{
			"ibc.ConnectionGenesis.MaxExpectedTimePerBlock",
			testProposal(proposal.ParamChange{
				Subspace: "ibc",
				Key:      string(ibcconnectiontypes.KeyMaxExpectedTimePerBlock),
				Value:    `"2"`,
			}),
			func() {
				got := suite.app.IBCKeeper.ConnectionKeeper.GetParams(suite.ctx).MaxExpectedTimePerBlock
				want := uint64(2)
				assert.Equal(want, got)
			},
		},
		{
			"ibc.Transfer.ReceiveEnabled",
			testProposal(proposal.ParamChange{
				Subspace: ibctransfertypes.ModuleName,
				Key:      string(ibctransfertypes.KeyReceiveEnabled),
				Value:    `false`,
			}),
			func() {
				got := suite.app.TransferKeeper.GetParams(suite.ctx).ReceiveEnabled
				want := false
				assert.Equal(want, got)
			},
		},
		{
			"ibc.Transfer.SendEnabled",
			testProposal(proposal.ParamChange{
				Subspace: ibctransfertypes.ModuleName,
				Key:      string(ibctransfertypes.KeySendEnabled),
				Value:    `false`,
			}),
			func() {
				got := suite.app.TransferKeeper.GetParams(suite.ctx).SendEnabled
				want := false
				assert.Equal(want, got)
			},
		},
		{
			"icahost.HostEnabled",
			testProposal(proposal.ParamChange{
				Subspace: icahosttypes.SubModuleName,
				Key:      string(icahosttypes.KeyHostEnabled),
				Value:    `false`,
			}),
			func() {
				got := suite.app.ICAHostKeeper.GetParams(suite.ctx).HostEnabled
				want := false
				assert.Equal(want, got)
			},
		},
		{
			"icahost.AllowMessages",
			testProposal(proposal.ParamChange{
				Subspace: icahosttypes.SubModuleName,
				Key:      string(icahosttypes.KeyAllowMessages),
				Value:    `["foo"]`,
			}),
			func() {
				got := suite.app.ICAHostKeeper.GetParams(suite.ctx).AllowMessages
				want := []string{"foo"}
				assert.Equal(want, got)
			},
		},
		{
			"slashing.DowntimeJailDuration",
			testProposal(proposal.ParamChange{
				Subspace: slashingtypes.ModuleName,
				Key:      string(slashingtypes.KeyDowntimeJailDuration),
				Value:    `"2"`,
			}),
			func() {
				got := suite.app.SlashingKeeper.GetParams(suite.ctx).DowntimeJailDuration
				want := time.Duration(2)
				assert.Equal(want, got)
			},
		},
		{
			"slashing.MinSignedPerWindow",
			testProposal(proposal.ParamChange{
				Subspace: slashingtypes.ModuleName,
				Key:      string(slashingtypes.KeyMinSignedPerWindow),
				Value:    `"1"`,
			}),
			func() {
				got := suite.app.SlashingKeeper.GetParams(suite.ctx).MinSignedPerWindow
				want := sdk.NewDec(1)
				assert.Equal(want, got)
			},
		},
		{
			"slashing.SignedBlocksWindow",
			testProposal(proposal.ParamChange{
				Subspace: slashingtypes.ModuleName,
				Key:      string(slashingtypes.KeySignedBlocksWindow),
				Value:    `"1"`,
			}),
			func() {
				got := suite.app.SlashingKeeper.GetParams(suite.ctx).SignedBlocksWindow
				want := int64(1)
				assert.Equal(want, got)
			},
		},
		{
			"slashing.SlashFractionDoubleSign",
			testProposal(proposal.ParamChange{
				Subspace: slashingtypes.ModuleName,
				Key:      string(slashingtypes.KeySlashFractionDoubleSign),
				Value:    `"1"`,
			}),
			func() {
				got := suite.app.SlashingKeeper.GetParams(suite.ctx).SlashFractionDoubleSign
				want := sdk.NewDec(1)
				assert.Equal(want, got)
			},
		},
		{
			"slashing.SlashFractionDowntime",
			testProposal(proposal.ParamChange{
				Subspace: slashingtypes.ModuleName,
				Key:      string(slashingtypes.KeySlashFractionDowntime),
				Value:    `"1"`,
			}),
			func() {
				got := suite.app.SlashingKeeper.GetParams(suite.ctx).SlashFractionDowntime
				want := sdk.NewDec(1)
				assert.Equal(want, got)
			},
		},
		{
			"staking.HistoricalEntries",
			testProposal(proposal.ParamChange{
				Subspace: stakingtypes.ModuleName,
				Key:      string(stakingtypes.KeyHistoricalEntries),
				Value:    `1`,
			}),
			func() {
				got := suite.app.StakingKeeper.GetParams(suite.ctx).HistoricalEntries
				want := uint32(1)
				assert.Equal(want, got)
			},
		},
		{
			"staking.MaxEntries",
			testProposal(proposal.ParamChange{
				Subspace: stakingtypes.ModuleName,
				Key:      string(stakingtypes.KeyMaxEntries),
				Value:    `1`,
			}),
			func() {
				got := suite.app.StakingKeeper.GetParams(suite.ctx).MaxEntries
				want := uint32(1)
				assert.Equal(want, got)
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
				got := suite.app.StakingKeeper.GetParams(suite.ctx).MaxValidators
				want := uint32(1)
				assert.Equal(want, got)
			},
		},
		{
			"staking.MinCommissionRate",
			testProposal(proposal.ParamChange{
				Subspace: stakingtypes.ModuleName,
				Key:      string(stakingtypes.KeyMinCommissionRate),
				Value:    `"1"`,
			}),
			func() {
				got := suite.app.StakingKeeper.GetParams(suite.ctx).MinCommissionRate
				want := sdk.NewDec(1)
				assert.Equal(want, got)
			},
		},
		{
			"minfee.NetworkMinGasPrice",
			testProposal(proposal.ParamChange{
				Subspace: minfeetypes.ModuleName,
				Key:      string(minfeetypes.KeyNetworkMinGasPrice),
				Value:    `"0.1"`,
			}),
			func() {
				var got sdk.Dec
				subspace := suite.app.GetSubspace(minfeetypes.ModuleName)
				subspace.Get(suite.ctx, minfeetypes.KeyNetworkMinGasPrice, &got)

				want, err := sdk.NewDecFromStr("0.1")
				assert.NoError(err)
				assert.Equal(want, got)
			},
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := suite.govHandler(suite.ctx, tc.proposal)
			suite.Require().NoError(err)
			tc.postProposal()
		})
	}
}

// TestUnmodifiableParams verifies that the params listed as non governance
// modifiable in the specs parameters.md file cannot be modified via governance.
// It does not include a test case for consensus.block.TimeIotaMs because
// TimeIotaMs is not exposed to the application.
func (suite *GovParamsTestSuite) TestUnmodifiableParams() {
	assert := suite.Assert()

	// Record the initial values of all these parameters before any governance
	// proposals are submitted.
	wantSendEnabled := suite.app.BankKeeper.GetParams(suite.ctx).SendEnabled
	wantPubKeyTypes := *suite.app.BaseApp.GetConsensusParams(suite.ctx).Validator
	wantBondDenom := suite.app.StakingKeeper.GetParams(suite.ctx).BondDenom
	wantUnbondingTime := suite.app.StakingKeeper.GetParams(suite.ctx).UnbondingTime

	testCases := []struct {
		name         string
		proposal     *proposal.ParameterChangeProposal
		wantErr      error
		postProposal func()
	}{
		{
			"bank.SendEnabled",
			testProposal(proposal.ParamChange{
				Subspace: banktypes.ModuleName,
				Key:      string(banktypes.KeySendEnabled),
				Value:    `[{"denom": "test", "enabled": false}]`,
			}),
			paramfilter.ErrBlockedParameter,
			func() {
				got := suite.app.BankKeeper.GetParams(suite.ctx).SendEnabled

				proposed := []*banktypes.SendEnabled{banktypes.NewSendEnabled("test", false)}
				assert.NotEqual(proposed, got)
				assert.Equal(wantSendEnabled, got)
			},
		},
		{
			"consensus.validator.PubKeyTypes",
			testProposal(proposal.ParamChange{
				Subspace: baseapp.Paramspace,
				Key:      string(baseapp.ParamStoreKeyValidatorParams),
				Value:    `{"pub_key_types": ["secp256k1"]}`,
			}),
			paramfilter.ErrBlockedParameter,
			func() {
				got := *suite.app.BaseApp.GetConsensusParams(suite.ctx).Validator
				proposed := tmproto.ValidatorParams{
					PubKeyTypes: []string{"secp256k1"},
				}
				assert.NotEqual(proposed, got)
				assert.Equal(wantPubKeyTypes, got)
			},
		},
		{
			"staking.BondDenom",
			testProposal(proposal.ParamChange{
				Subspace: stakingtypes.ModuleName,
				Key:      string(stakingtypes.KeyBondDenom),
				Value:    `"test"`,
			}),
			paramfilter.ErrBlockedParameter,
			func() {
				got := suite.app.StakingKeeper.GetParams(suite.ctx).BondDenom
				proposed := "test"
				assert.NotEqual(proposed, got)
				assert.Equal(wantBondDenom, got)
			},
		},
		{
			"staking.UnbondingTime",
			testProposal(proposal.ParamChange{
				Subspace: stakingtypes.ModuleName,
				Key:      string(stakingtypes.KeyUnbondingTime),
				Value:    `"1"`,
			}),
			paramfilter.ErrBlockedParameter,
			func() {
				got := suite.app.StakingKeeper.GetParams(suite.ctx).UnbondingTime
				proposed := time.Duration(1)
				assert.NotEqual(proposed, got)
				assert.Equal(wantUnbondingTime, got)
			},
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := suite.govHandler(suite.ctx, tc.proposal)
			if tc.wantErr != nil {
				suite.Require().ErrorIs(err, tc.wantErr)
			} else {
				suite.Require().NoError(err)
			}
			tc.postProposal()
		})
	}
}
