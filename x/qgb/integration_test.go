package qgb_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	qgbtypes "github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/errors"
	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
)

func TestQGBIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping QGB integration test in short mode.")
	}
	suite.Run(t, new(QGBIntegrationSuite))
}

type QGBIntegrationSuite struct {
	suite.Suite

	accounts []string
	cctx     testnode.Context
	ecfg     encoding.Config
}

func (s *QGBIntegrationSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up integration test suite")

	s.accounts = []string{"jimmy"}

	cfg := testnode.DefaultConfig().WithAccounts(s.accounts)
	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	s.cctx = cctx
}

func (s *QGBIntegrationSuite) TestQGB() {
	t := s.T()
	type test struct {
		name                string
		msgFunc             func() (msgs []sdk.Msg, signer string)
		expectedCheckTxCode uint32
	}
	tests := []test{
		{
			name: "edit a qgb validator address",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				account := "validator"
				valAcc, err := s.cctx.Keyring.Key(account)
				require.NoError(t, err)
				valAddr, err := valAcc.GetAddress()
				require.NoError(t, err)

				rvalAddr := sdk.ValAddress(valAddr)

				msg := qgbtypes.NewMsgRegisterEVMAddress(rvalAddr, gethcommon.HexToAddress("0x95222290DD7278Aa3Ddd389Cc1E1d165CC4BAfe5"))
				require.NoError(t, err)
				return []sdk.Msg{msg}, account
			},
			expectedCheckTxCode: abci.CodeTypeOK,
		},
		{
			name: "unable to edit a qgb validator address",
			msgFunc: func() (msgs []sdk.Msg, signer string) {
				account := s.accounts[0]
				valAcc, err := s.cctx.Keyring.Key("validator")
				require.NoError(t, err)
				valAddr, err := valAcc.GetAddress()
				require.NoError(t, err)

				rvalAddr := sdk.ValAddress(valAddr)

				msg := qgbtypes.NewMsgRegisterEVMAddress(rvalAddr, gethcommon.HexToAddress("0x95222290DD7278Aa3Ddd389Cc1E1d165CC4BAfe5"))
				require.NoError(t, err)
				return []sdk.Msg{msg}, account
			},
			expectedCheckTxCode: errors.ErrInvalidPubKey.ABCICode(),
		},
	}

	// sign and submit the transactions
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs, signer := tt.msgFunc()
			res, err := testnode.SignAndBroadcastTx(s.ecfg, s.cctx.Context, signer, msgs...)
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Equal(t, tt.expectedCheckTxCode, res.Code, res.RawLog)
		})
	}
}
