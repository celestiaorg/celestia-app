package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

// TestNonCanonicalTxRejected checks that a correctly signed tx whose body is
// not canonically encoded fails CheckTx, is dropped by PrepareProposal,
// makes ProcessProposal reject the block and fails in FinalizeBlock.
func TestNonCanonicalTxRejected(t *testing.T) {
	testCases := []struct {
		name   string
		mutate func(body []byte) []byte
	}{
		{
			name: "unknown non-critical field",
			mutate: func(body []byte) []byte {
				body = protowire.AppendTag(body, 1025, protowire.BytesType)
				return protowire.AppendBytes(body, []byte("x"))
			},
		},
		{
			name: "non-minimal varint",
			mutate: func(body []byte) []byte {
				// timeout_height = 1, encoded in two bytes.
				return append(protowire.AppendTag(body, 3, protowire.VarintType), 0x81, 0x00)
			},
		},
		{
			name: "invalid UTF-8 memo",
			mutate: func(body []byte) []byte {
				body = protowire.AppendTag(body, 2, protowire.BytesType)
				return protowire.AppendBytes(body, []byte{0xff})
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
			accounts := testfactory.GenerateAccounts(2)
			testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
			infos := queryAccountInfo(testApp, accounts, kr)
			signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
			msg := banktypes.NewMsgSend(testfactory.GetAddress(kr, accounts[0]), testfactory.GetAddress(kr, accounts[1]), sdk.NewCoins(sdk.NewInt64Coin("utia", 1)))
			canonical, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(200_000), user.SetFee(4_000))
			require.NoError(t, err)
			raw := resignWithBody(t, kr, accounts[0], infos[0].AccountNum, canonical, tc.mutate)

			// CheckTx returns decode errors both as an error and in the response.
			checkResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: raw, Type: abci.CheckTxType_New})
			require.ErrorContains(t, err, "tx parse error")
			require.NotEqual(t, abci.CodeTypeOK, checkResp.Code)
			require.Contains(t, checkResp.Log, "tx parse error")

			prepareResp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
				Txs:    [][]byte{raw},
				Height: testApp.LastBlockHeight() + 1,
				Time:   time.Now(),
			})
			require.NoError(t, err)
			require.Empty(t, prepareResp.Txs)

			processResp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, [][]byte{raw}))
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_REJECT, processResp.Status)

			finalizeResp := commitBlock(t, testApp, raw)
			require.Len(t, finalizeResp.TxResults, 1)
			require.NotEqual(t, abci.CodeTypeOK, finalizeResp.TxResults[0].Code)
			require.Contains(t, finalizeResp.TxResults[0].Log, "tx parse error")

			// The canonical tx is still accepted.
			checkResp, err = testApp.CheckTx(&abci.RequestCheckTx{Tx: canonical, Type: abci.CheckTxType_New})
			require.NoError(t, err)
			require.Equal(t, abci.CodeTypeOK, checkResp.Code, checkResp.Log)
		})
	}
}

// resignWithBody replaces the body bytes of a signed tx with mutate(body) and
// signs the result again in SIGN_MODE_DIRECT, so only the encoding is wrong.
func resignWithBody(t *testing.T, kr keyring.Keyring, account string, accountNum uint64, raw []byte, mutate func([]byte) []byte) []byte {
	t.Helper()
	var txRaw txtypes.TxRaw
	require.NoError(t, txRaw.Unmarshal(raw))
	txRaw.BodyBytes = mutate(append([]byte(nil), txRaw.BodyBytes...))
	signDoc := txtypes.SignDoc{
		BodyBytes:     txRaw.BodyBytes,
		AuthInfoBytes: txRaw.AuthInfoBytes,
		ChainId:       testutil.ChainID,
		AccountNumber: accountNum,
	}
	signBytes, err := signDoc.Marshal()
	require.NoError(t, err)
	sig, _, err := kr.Sign(account, signBytes, signingtypes.SignMode_SIGN_MODE_DIRECT)
	require.NoError(t, err)
	txRaw.Signatures = [][]byte{sig}
	out, err := txRaw.Marshal()
	require.NoError(t, err)
	return out
}
