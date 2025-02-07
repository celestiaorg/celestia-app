//go:build !race

// known race in testnode
// ref: https://github.com/celestiaorg/celestia-app/issues/1369
package txsim_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	v2 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v2"
	v3 "github.com/celestiaorg/celestia-app/v4/pkg/appconsts/v4"
	"github.com/celestiaorg/celestia-app/v4/test/txsim"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	blob "github.com/celestiaorg/celestia-app/v4/x/blob/types"
	signaltypes "github.com/celestiaorg/celestia-app/v4/x/signal/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	distribution "github.com/cosmos/cosmos-sdk/x/distribution/types"
	staking "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
)

func TestTxSimulator(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestTxSimulator in short mode.")
	}
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testCases := []struct {
		name        string
		sequences   []txsim.Sequence
		expMessages map[string]int64
		useFeegrant bool
	}{
		{
			name:      "send sequence",
			sequences: []txsim.Sequence{txsim.NewSendSequence(2, 1000, 100)},
			// we expect at least 5 bank send messages within 30 seconds
			expMessages: map[string]int64{sdk.MsgTypeURL(&bank.MsgSend{}): 5},
		},
		{
			name:      "stake sequence",
			sequences: []txsim.Sequence{txsim.NewStakeSequence(1000)},
			expMessages: map[string]int64{
				sdk.MsgTypeURL(&staking.MsgDelegate{}):                     1,
				sdk.MsgTypeURL(&distribution.MsgWithdrawDelegatorReward{}): 5,
				// NOTE: this sequence also makes redelegations but because the
				// testnet has only one validator, this never happens
			},
		},
		{
			name: "blob sequence",
			sequences: []txsim.Sequence{
				txsim.NewBlobSequence(
					txsim.NewRange(100, 1000),
					txsim.NewRange(1, 3)),
			},
			expMessages: map[string]int64{sdk.MsgTypeURL(&blob.MsgPayForBlobs{}): 10},
		},
		{
			name: "multi blob sequence",
			sequences: txsim.NewBlobSequence(
				txsim.NewRange(1000, 1000),
				txsim.NewRange(3, 3),
			).Clone(4),
			expMessages: map[string]int64{sdk.MsgTypeURL(&blob.MsgPayForBlobs{}): 20},
		},
		{
			name: "multi mixed sequence",
			sequences: append(append(
				txsim.NewSendSequence(2, 1000, 100).Clone(3),
				txsim.NewStakeSequence(1000).Clone(3)...),
				txsim.NewBlobSequence(txsim.NewRange(1000, 1000), txsim.NewRange(1, 3)).Clone(3)...),
			expMessages: map[string]int64{
				sdk.MsgTypeURL(&bank.MsgSend{}):                            15,
				sdk.MsgTypeURL(&staking.MsgDelegate{}):                     2,
				sdk.MsgTypeURL(&distribution.MsgWithdrawDelegatorReward{}): 10,
				sdk.MsgTypeURL(&blob.MsgPayForBlobs{}):                     10,
			},
		},
		{
			name: "multi mixed sequence using feegrant",
			sequences: append(append(
				txsim.NewSendSequence(2, 1000, 100).Clone(3),
				txsim.NewStakeSequence(1000).Clone(3)...),
				txsim.NewBlobSequence(txsim.NewRange(1000, 1000), txsim.NewRange(1, 3)).Clone(3)...),
			expMessages: map[string]int64{
				sdk.MsgTypeURL(&bank.MsgSend{}):                            15,
				sdk.MsgTypeURL(&staking.MsgDelegate{}):                     2,
				sdk.MsgTypeURL(&distribution.MsgWithdrawDelegatorReward{}): 10,
				sdk.MsgTypeURL(&blob.MsgPayForBlobs{}):                     10,
			},
			useFeegrant: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			keyring, rpcAddr, grpcAddr := Setup(t)

			opts := txsim.DefaultOptions().
				SuppressLogs().
				WithPollTime(time.Millisecond * 100)
			if tc.useFeegrant {
				opts.UseFeeGrant()
			}

			err := txsim.Run(
				ctx,
				grpcAddr,
				keyring,
				encCfg,
				opts,
				tc.sequences...,
			)
			// Expect all sequences to run for at least 30 seconds without error
			require.True(t, errors.Is(err, context.DeadlineExceeded), err.Error())

			blocks, err := testnode.ReadBlockchain(context.Background(), rpcAddr)
			require.NoError(t, err)
			for _, block := range blocks {
				txs, err := testnode.DecodeBlockData(block.Data)
				require.NoError(t, err, block.Height)
				for _, tx := range txs {
					for _, msg := range tx.GetMsgs() {
						if _, ok := tc.expMessages[sdk.MsgTypeURL(msg)]; ok {
							tc.expMessages[sdk.MsgTypeURL(msg)]--
						}
					}
				}
			}
			for msg, count := range tc.expMessages {
				if count > 0 {
					t.Errorf("missing %d messages of type %s (blocks: %d)", count, msg, len(blocks))
				}
			}
		})
	}
}

func Setup(t testing.TB) (keyring.Keyring, string, string) {
	t.Helper()

	cfg := testnode.DefaultConfig().WithTimeoutCommit(300 * time.Millisecond).WithFundedAccounts("txsim-master")

	cctx, rpcAddr, grpcAddr := testnode.NewNetwork(t, cfg)

	return cctx.Keyring, rpcAddr, grpcAddr
}

func TestTxSimUpgrade(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestTxSimUpgrade in short mode.")
	}
	cp := app.DefaultConsensusParams()
	cp.Version.App = v2.Version
	cfg := testnode.DefaultConfig().
		WithTimeoutCommit(300 * time.Millisecond).
		WithConsensusParams(cp).
		WithFundedAccounts("txsim-master")
	cctx, _, grpcAddr := testnode.NewNetwork(t, cfg)

	require.NoError(t, cctx.WaitForNextBlock())

	// upgrade to v3 at height 20
	sequences := []txsim.Sequence{
		txsim.NewUpgradeSequence(v3.Version, 20),
	}

	opts := txsim.DefaultOptions().
		// SuppressLogs().
		WithPollTime(time.Millisecond * 100)

	err := txsim.Run(
		cctx.GoContext(),
		grpcAddr,
		cctx.Keyring,
		encoding.MakeConfig(app.ModuleEncodingRegisters...),
		opts,
		sequences...,
	)
	require.NoError(t, err)

	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	querier := signaltypes.NewQueryClient(conn)

	// We can't check that the upgrade was successful because the upgrade height is thousands of blocks away
	// and even at 300 millisecond block times, it would take too long. Instead we just want to assert
	// that the upgrade is ready to be performed
	require.Eventually(t, func() bool {
		upgradePlan, err := querier.GetUpgrade(cctx.GoContext(), &signaltypes.QueryGetUpgradeRequest{})
		require.NoError(t, err)
		return upgradePlan.Upgrade != nil && upgradePlan.Upgrade.AppVersion == v3.Version
	}, time.Second*20, time.Millisecond*100)
}
