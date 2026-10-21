package app_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v10/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

// TestCheckTxPrepareProposalSeeds ensures the corpus exercises accepted transactions.
func TestCheckTxPrepareProposalSeeds(t *testing.T) {
	for kind := range byte(6) {
		require.True(t, checkTxPrepareProposal(t, []byte{kind}, 512, 100_000_000, "", 0))
	}
	require.True(t, checkTxPrepareProposal(t, []byte{0, 1, 2, 3, 4}, 512, 100_000_000, "mixed", 0))
	require.True(t, checkTxPrepareProposal(t, []byte{5, 5}, 512, 100_000_000, "", 0))
	require.True(t, checkTxPrepareProposal(t, bytes.Repeat([]byte{0}, appconsts.MaxSDKMessages), 512, 100_000_000, "", 0))
}

// FuzzCheckTxPrepareProposal checks that an accepted tx fits in an otherwise empty proposal.
func FuzzCheckTxPrepareProposal(f *testing.F) {
	for kind := range byte(6) {
		f.Add([]byte{kind}, uint32(512), uint64(100_000_000), "", uint64(0))
	}
	f.Add([]byte{0, 1, 2, 3, 4}, uint32(512), uint64(100_000_000), "mixed", uint64(0))
	f.Add([]byte{5, 5}, uint32(512), uint64(100_000_000), "", uint64(0))
	f.Add([]byte{0, 1, 2, 3, 4, 5, 5}, uint32(512), uint64(100_000_000), "mixed", uint64(0))
	for _, count := range []int{appconsts.MaxSDKMessages, appconsts.MaxSDKMessages + 1} {
		f.Add(bytes.Repeat([]byte{0}, count), uint32(512), uint64(100_000_000), "", uint64(0))
	}
	for _, size := range []uint32{1, 478, 479, 482, 483, 1 << 20, uint32(appconsts.MaxTxSize)} {
		f.Add([]byte{5}, size, uint64(100_000_000), "", uint64(0))
	}
	f.Add([]byte{0}, uint32(1), uint64(1), "", uint64(0))
	f.Add([]byte{0}, uint32(1), uint64(100_000_000), "", uint64(2))
	f.Fuzz(func(t *testing.T, kinds []byte, blobSize uint32, gas uint64, memo string, timeout uint64) {
		checkTxPrepareProposal(t, kinds, blobSize, gas, memo, timeout)
	})
}

func checkTxPrepareProposal(t *testing.T, kinds []byte, blobSize uint32, gas uint64, memo string, timeout uint64) bool {
	t.Helper()
	// Bound work before allocating blobs or initializing an app.
	if len(kinds) == 0 || len(kinds) > appconsts.MaxSDKMessages+1 || len(memo) > appconsts.MaxTxSize {
		return false
	}
	var blobCount uint64
	for _, kind := range kinds {
		if kind%6 == 5 {
			blobCount++
		}
	}
	if blobCount > 0 && (blobSize == 0 || blobCount*uint64(blobSize) > uint64(appconsts.MaxTxSize)) {
		return false
	}

	// Fresh committed state keeps CheckTx's fee and sequence changes independent
	// of PrepareProposal and prevents one fuzz input from affecting another.
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), "sender")
	t.Cleanup(func() { require.NoError(t, testApp.Close()) })
	// Expiration between CheckTx and the next height is a legitimate rejection.
	height := testApp.LastBlockHeight() + 1
	if timeout != 0 && timeout < uint64(height) {
		return false
	}
	addr := testfactory.GetAddress(kr, "sender")
	account := testutil.DirectQueryAccount(testApp, addr)
	signer := createSigner(t, kr, "sender", testApp.GetTxConfig(), account.GetAccountNumber())
	validator := sdk.ValAddress(testutil.GenesisValidatorPrivateKey().PubKey().Address()).String()
	coins := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1))
	msgs := make([]sdk.Msg, 0, len(kinds))
	var blobs []*share.Blob
	for _, kind := range kinds {
		switch kind % 6 {
		case 0:
			msgs = append(msgs, banktypes.NewMsgSend(addr, addr, coins))
		case 1:
			msgs = append(msgs, &banktypes.MsgMultiSend{
				Inputs:  []banktypes.Input{{Address: addr.String(), Coins: coins}},
				Outputs: []banktypes.Output{{Address: addr.String(), Coins: coins}},
			})
		case 2:
			msgs = append(msgs, &stakingtypes.MsgDelegate{DelegatorAddress: addr.String(), ValidatorAddress: validator, Amount: coins[0]})
		case 3:
			msgs = append(msgs, &distributiontypes.MsgWithdrawDelegatorReward{DelegatorAddress: addr.String(), ValidatorAddress: validator})
		case 4:
			msgs = append(msgs, &govtypes.MsgVote{ProposalId: 1, Voter: addr.String(), Option: govtypes.OptionYes})
		case 5:
			namespace, err := share.NewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
			require.NoError(t, err)
			blob, err := share.NewBlob(namespace, bytes.Repeat([]byte{kind}, int(blobSize)), appconsts.DefaultShareVersion, nil)
			require.NoError(t, err)
			blobs = append(blobs, blob)
		}
	}
	if len(blobs) > 0 {
		pfb, err := blobtypes.NewMsgPayForBlobs(addr.String(), appconsts.Version, blobs...)
		require.NoError(t, err)
		msgs = append(msgs, pfb)
	}
	// Re-sign after mutation so fuzzing can reach past signature verification.
	rawTx, _, err := signer.CreateTx(msgs, user.SetGasLimit(gas), user.SetFee(1_000_000), user.SetMemo(memo), user.SetTimeoutHeight(timeout))
	if err != nil {
		t.Logf("signing rejected input: %v", err)
		return false
	}
	if len(blobs) > 0 {
		rawTx, err = blobtx.MarshalBlobTx(rawTx, blobs...)
		require.NoError(t, err)
	}
	check, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: rawTx, Type: abci.CheckTxType_New})
	if err != nil || check.Code != abci.CodeTypeOK {
		t.Logf("CheckTx rejected input: response=%v error=%v", check, err)
		return false
	}
	proposal, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Txs:        [][]byte{rawTx},
		Height:     height,
		Time:       testutil.GenesisTime.Add(time.Second),
		MaxTxBytes: int64(appconsts.BlockMaxBytes),
	})
	require.NoError(t, err)
	require.Len(t, proposal.Txs, 1, "CheckTx accepted a tx that PrepareProposal dropped")
	require.True(t, bytes.Equal(rawTx, proposal.Txs[0]), "PrepareProposal changed the accepted tx")
	return true
}
