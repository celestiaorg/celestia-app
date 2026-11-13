package frozen_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/app/frozen"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/stretchr/testify/require"
)

// networks pins each chain's list and whitehat. They are consensus inputs:
// never change them once v11.0.0 is tagged for that network.
var networks = []struct {
	chainID, whitehat, sha256 string
	size                      int
}{
	{appconsts.MainnetChainID, "celestia1atuap9kfnt6y8ykvn3437e7j3z8qffh33pzt53", "96faa31190aec74db35efec56f3f6aaeb16137c3fb988163b7a95a8a00d8f6a3", 41_963},
	{appconsts.MochaChainID, "celestia1xk4tasrhcr3w6jyq4xzd8x6aapv9hmfuh8mcxn", "bc2841e16bd4219adab31c6b1376363037547d63aae773b51d7dac4f0d39b629", 3},
	{appconsts.CortoChainID, "celestia1yny4dz396zd2vp85fnqynnnq6pnj00lz95xv09", "c88aae7e7e0af99ae8e2e45b5dc3cae222f4af3aa2cb1e531fcb139fe456fc11", 3},
}

// listedAddrs reads a chain's address file.
func listedAddrs(t *testing.T, chainID string) []sdk.AccAddress {
	bz, err := os.ReadFile("addresses/" + chainID + ".txt")
	require.NoError(t, err)
	var out []sdk.AccAddress
	for line := range strings.SplitSeq(string(bz), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, sdk.MustAccAddressFromBech32(line))
		}
	}
	return out
}

// TestEmbeddedLists checks every list is pinned, loaded for its chain only, unique, and without its whitehat.
func TestEmbeddedLists(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	r := frozen.NewRules(enc.Codec, nil)
	require.Equal(t, frozen.MainnetWhitehat, networks[0].whitehat)
	require.Equal(t, frozen.MochaWhitehat, networks[1].whitehat)
	require.Equal(t, frozen.CortoWhitehat, networks[2].whitehat)
	files, err := os.ReadDir("addresses")
	require.NoError(t, err)
	require.Len(t, files, len(networks))
	for _, n := range networks {
		bz, err := os.ReadFile("addresses/" + n.chainID + ".txt")
		require.NoError(t, err)
		sum := sha256.Sum256(bz)
		require.Equal(t, n.sha256, hex.EncodeToString(sum[:]), n.chainID)
		seen := map[string]bool{}
		for _, addr := range listedAddrs(t, n.chainID) {
			require.True(t, r.Has(n.chainID, addr), n.chainID)
			require.False(t, r.Has("unknown-1", addr))
			require.False(t, seen[string(addr)], "duplicate %s on %s", addr, n.chainID)
			seen[string(addr)] = true
		}
		require.Len(t, seen, n.size, n.chainID)
		require.False(t, r.Has(n.chainID, sdk.MustAccAddressFromBech32(n.whitehat)), n.chainID)
	}
}

// TestNetworkSelection checks each chain enforces its own list and whitehat, and unknown chains nothing.
func TestNetworkSelection(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	r := frozen.NewRules(enc.Codec, nil)
	send := func(chainID string, from, to sdk.AccAddress) error {
		ctx := sdk.Context{}.WithChainID(chainID).WithConsensusParams(cmtproto.ConsensusParams{Version: &cmtproto.VersionParams{App: frozen.ActivationVersion}})
		_, err := r.SendRestriction(ctx, from, to, nil)
		return err
	}
	setWithdraw := func(chainID string, del sdk.AccAddress, to string) error {
		b := enc.TxConfig.NewTxBuilder()
		require.NoError(t, b.SetMsgs(&distrtypes.MsgSetWithdrawAddress{DelegatorAddress: del.String(), WithdrawAddress: to}))
		ctx := sdk.Context{}.WithChainID(chainID).WithConsensusParams(cmtproto.ConsensusParams{Version: &cmtproto.VersionParams{App: frozen.ActivationVersion}})
		_, err := r.AnteHandle(ctx, b.GetTx(), false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil })
		return err
	}
	other := sdk.AccAddress([]byte("unlisted_address____"))
	for _, n := range networks {
		victim := listedAddrs(t, n.chainID)[0]
		require.NoError(t, setWithdraw(n.chainID, victim, n.whitehat), n.chainID)
		require.NoError(t, send(n.chainID, victim, sdk.MustAccAddressFromBech32(n.whitehat)), n.chainID)
		require.ErrorIs(t, send(n.chainID, victim, other), frozen.ErrFrozen, n.chainID)
		require.NoError(t, send("unknown-1", victim, other), n.chainID)
		for _, m := range networks {
			if m.chainID != n.chainID {
				require.ErrorIs(t, send(n.chainID, victim, sdk.MustAccAddressFromBech32(m.whitehat)), frozen.ErrFrozen, "%s whitehat on %s", m.chainID, n.chainID)
				require.ErrorIs(t, setWithdraw(n.chainID, victim, m.whitehat), frozen.ErrFrozen, "%s whitehat on %s", m.chainID, n.chainID)
			}
		}
	}
	// The per-send cost is two map lookups, with no allocation.
	var ctx context.Context = sdk.Context{}.WithChainID(appconsts.MainnetChainID).
		WithConsensusParams(cmtproto.ConsensusParams{Version: &cmtproto.VersionParams{App: frozen.ActivationVersion}})
	victim := listedAddrs(t, appconsts.MainnetChainID)[0]
	whitehat := sdk.MustAccAddressFromBech32(frozen.MainnetWhitehat)
	allocs := testing.AllocsPerRun(100, func() { _, _ = r.SendRestriction(ctx, victim, whitehat, nil) })
	require.Zero(t, allocs)
}

func TestAnteHandle(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	listedKey := secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey)
	listed := sdk.AccAddress(listedKey.Address())
	other := sdk.AccAddress([]byte("unlisted_address____"))
	r := frozen.NewRules(enc.Codec, nil)
	r.SetNetwork("test", frozen.MainnetWhitehat, listed)

	run := func(version uint64, fee sdk.Coins, msgs ...sdk.Msg) error {
		b := enc.TxConfig.NewTxBuilder()
		require.NoError(t, b.SetMsgs(msgs...))
		b.SetFeeAmount(fee)
		ctx := sdk.Context{}.WithChainID("test").WithConsensusParams(cmtproto.ConsensusParams{Version: &cmtproto.VersionParams{App: version}})
		_, err := r.AnteHandle(ctx, b.GetTx(), false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil })
		return err
	}
	fee := sdk.NewCoins(sdk.NewInt64Coin("utia", 10_000))
	send := banktypes.NewMsgSend(listed, other, fee)
	pff := &fibretypes.MsgPayForFibre{Signer: other.String(), PaymentPromise: fibretypes.PaymentPromise{SignerPublicKey: *listedKey}}
	timeout := &fibretypes.MsgPaymentPromiseTimeout{Signer: other.String(), PaymentPromise: fibretypes.PaymentPromise{SignerPublicKey: *listedKey}}

	require.NoError(t, run(frozen.ActivationVersion, fee, send))
	require.ErrorIs(t, run(frozen.ActivationVersion, fee.Add(sdk.NewInt64Coin("ibc/ABC", 1)), send), frozen.ErrFrozen)
	require.ErrorIs(t, run(frozen.ActivationVersion, fee, pff), frozen.ErrFrozen)
	require.ErrorIs(t, run(frozen.ActivationVersion, fee, timeout), frozen.ErrFrozen)
	require.NoError(t, run(frozen.ActivationVersion-1, fee, pff))
	noKey := &fibretypes.MsgPaymentPromiseTimeout{Signer: other.String()}
	require.ErrorIs(t, run(frozen.ActivationVersion, fee, noKey), sdkerrors.ErrInvalidPubKey)
}
