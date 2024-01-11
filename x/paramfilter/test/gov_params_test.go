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
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v6/modules/apps/transfer/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	ibcconnectiontypes "github.com/cosmos/ibc-go/v6/modules/core/03-connection/types"
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

func (suite *GovParamsTestSuite) TestModifiableParameters() {
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
		{
			"distribution.BaseProposerReward",
			testProposal(proposal.ParamChange{
				Subspace: distributiontypes.ModuleName,
				Key:      string(distributiontypes.ParamStoreKeyBaseProposerReward),
				Value:    `"1"`,
			}),
			func() {
				gotBaseProposerReward := suite.app.DistrKeeper.GetParams(suite.ctx).BaseProposerReward
				wantBaseProposerReward := sdk.NewDec(1)
				suite.Require().Equal(
					wantBaseProposerReward,
					gotBaseProposerReward)
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
				gotBonusProposerReward := suite.app.DistrKeeper.GetParams(suite.ctx).BonusProposerReward
				wantBonusProposerReward := sdk.NewDec(1)
				suite.Require().Equal(
					wantBonusProposerReward,
					gotBonusProposerReward)
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
				gotCommunityTax := suite.app.DistrKeeper.GetParams(suite.ctx).CommunityTax
				wantCommunityTax := sdk.NewDec(1)
				suite.Require().Equal(
					wantCommunityTax,
					gotCommunityTax)
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
				gotWithdrawAddrEnabled := suite.app.DistrKeeper.GetParams(suite.ctx).WithdrawAddrEnabled
				wantWithdrawAddrEnabled := false
				suite.Require().Equal(
					wantWithdrawAddrEnabled,
					gotWithdrawAddrEnabled)
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
				suite.Require().Equal(
					wantMaxDepositPeriod,
					gotMaxDepositPeriod)

				gotMinDeposit := suite.app.GovKeeper.GetDepositParams(suite.ctx).MinDeposit
				wantMinDeposit := []sdk.Coin{{Denom: "test", Amount: sdk.NewInt(2)}}
				suite.Require().Equal(
					wantMinDeposit,
					gotMinDeposit)
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
				suite.Require().Equal(
					wantQuorum,
					gotQuroum)

				gotThreshold := suite.app.GovKeeper.GetTallyParams(suite.ctx).Threshold
				wantThreshold := "0.2"
				suite.Require().Equal(
					wantThreshold,
					gotThreshold)

				gotVetoThreshold := suite.app.GovKeeper.GetTallyParams(suite.ctx).VetoThreshold
				wantVetoThreshold := "0.3"
				suite.Require().Equal(
					wantVetoThreshold,
					gotVetoThreshold)
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
				gotVotingPeriod := *suite.app.GovKeeper.GetVotingParams(suite.ctx).VotingPeriod
				wantVotingPeriod := time.Duration(2)
				suite.Require().Equal(
					wantVotingPeriod,
					gotVotingPeriod)
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
				gotVotingPeriod := suite.app.IBCKeeper.ClientKeeper.GetParams(suite.ctx).AllowedClients
				wantVotingPeriod := []string{"01-test"}
				suite.Require().Equal(
					wantVotingPeriod,
					gotVotingPeriod)
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
				gotMaxExpectedTimePerBlock := suite.app.IBCKeeper.ConnectionKeeper.GetParams(suite.ctx).MaxExpectedTimePerBlock
				wantMaxExpectedTimePerBlock := uint64(2)
				suite.Require().Equal(
					wantMaxExpectedTimePerBlock,
					gotMaxExpectedTimePerBlock)
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
				gotReceiveEnabled := suite.app.TransferKeeper.GetParams(suite.ctx).ReceiveEnabled
				wantReceiveEnabled := false
				suite.Require().Equal(
					wantReceiveEnabled,
					gotReceiveEnabled)
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
				gotSendEnabled := suite.app.TransferKeeper.GetParams(suite.ctx).SendEnabled
				wantSendEnabled := false
				suite.Require().Equal(
					wantSendEnabled,
					gotSendEnabled)
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
				gotDowntimeJailDuration := suite.app.SlashingKeeper.GetParams(suite.ctx).DowntimeJailDuration
				wantDowntimeJailDuration := time.Duration(2)
				suite.Require().Equal(
					wantDowntimeJailDuration,
					gotDowntimeJailDuration)
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
				gotMinSignedPerWindow := suite.app.SlashingKeeper.GetParams(suite.ctx).MinSignedPerWindow
				wantMinSignedPerWindow := sdk.NewDec(1)
				suite.Require().Equal(
					wantMinSignedPerWindow,
					gotMinSignedPerWindow)
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
				gotSignedBlocksWindow := suite.app.SlashingKeeper.GetParams(suite.ctx).SignedBlocksWindow
				wantSignedBlocksWindow := int64(1)
				suite.Require().Equal(
					wantSignedBlocksWindow,
					gotSignedBlocksWindow)
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
				gotSlashFractionDoubleSign := suite.app.SlashingKeeper.GetParams(suite.ctx).SlashFractionDoubleSign
				wantSlashFractionDoubleSign := sdk.NewDec(1)
				suite.Require().Equal(
					wantSlashFractionDoubleSign,
					gotSlashFractionDoubleSign)
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
				gotSlashFractionDowntime := suite.app.SlashingKeeper.GetParams(suite.ctx).SlashFractionDowntime
				wantSlashFractionDowntime := sdk.NewDec(1)
				suite.Require().Equal(
					wantSlashFractionDowntime,
					gotSlashFractionDowntime)
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
				gotHistoricalEntries := suite.app.StakingKeeper.GetParams(suite.ctx).HistoricalEntries
				wantHistoricalEntries := uint32(1)
				suite.Require().Equal(
					wantHistoricalEntries,
					gotHistoricalEntries)
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
				gotMaxEntries := suite.app.StakingKeeper.GetParams(suite.ctx).MaxEntries
				wantMaxEntries := uint32(1)
				suite.Require().Equal(
					wantMaxEntries,
					gotMaxEntries)
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
				gotMinCommissionRate := suite.app.StakingKeeper.GetParams(suite.ctx).MinCommissionRate
				wantMinCommissionRate := sdk.NewDec(1)
				suite.Require().Equal(
					wantMinCommissionRate,
					gotMinCommissionRate)
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
