package blobstream_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	"github.com/celestiaorg/celestia-app/v3/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	blobstreamtypes "github.com/celestiaorg/celestia-app/v3/x/blobstream/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	staking "github.com/cosmos/cosmos-sdk/x/staking/types"
	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
)

func TestBlobstreamIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping Blobstream integration test in short mode.")
	}
	suite.Run(t, new(BlobstreamIntegrationSuite))
}

type BlobstreamIntegrationSuite struct {
	suite.Suite

	accounts []string
	cctx     testnode.Context
	ecfg     encoding.Config
}

func (s *BlobstreamIntegrationSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up integration test suite")

	s.accounts = []string{"jimmy"}

	cfg := testnode.DefaultConfig().
		WithFundedAccounts(s.accounts...).
		WithConsensusParams(app.DefaultInitialConsensusParams())
	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	s.cctx = cctx
}

func (s *BlobstreamIntegrationSuite) TestBlobstream() {
	t := s.T()
	type test struct {
		name           string
		msgFunc        func() (msgs []sdk.Msg, address sdk.AccAddress)
		expectedTxCode uint32
	}
	tests := []test{
		{
			name: "edit a blobstream validator address",
			msgFunc: func() ([]sdk.Msg, sdk.AccAddress) {
				addr := testfactory.GetAddress(s.cctx.Keyring, "validator")
				valAddr := sdk.ValAddress(addr)
				msg := blobstreamtypes.NewMsgRegisterEVMAddress(valAddr, gethcommon.HexToAddress("0x95222290DD7278Aa3Ddd389Cc1E1d165CC4BAfe5"))
				return []sdk.Msg{msg}, addr
			},
			expectedTxCode: abci.CodeTypeOK,
		},
		{
			name: "edit a non blobstream validator address",
			msgFunc: func() ([]sdk.Msg, sdk.AccAddress) {
				addr := testfactory.GetAddress(s.cctx.Keyring, s.accounts[0])
				valAddr := sdk.ValAddress(addr)
				msg := blobstreamtypes.NewMsgRegisterEVMAddress(valAddr, gethcommon.HexToAddress("0x95222290DD7278Aa3Ddd389Cc1E1d165CC4BAfe5"))
				return []sdk.Msg{msg}, addr
			},
			expectedTxCode: staking.ErrNoValidatorFound.ABCICode(),
		},
	}

	// sign and submit the transactions
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, _ := tt.msgFunc()
			txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg)
			require.NoError(t, err)
			res, err := txClient.SubmitTx(s.cctx.GoContext(), msgs, blobfactory.DefaultTxOpts()...)
			if tt.expectedTxCode == abci.CodeTypeOK {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.NotNil(t, res)
			require.Equal(t, tt.expectedTxCode, res.Code, res.RawLog)
		})
	}
}
