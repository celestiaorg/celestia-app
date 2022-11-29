package fork

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/testutil/testnode"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

var t = coretypes.ABCIPubKeyTypeEd25519

func init() {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)
	cfg.SetBech32PrefixForConsensusNode(app.Bech32PrefixConsAddr, app.Bech32PrefixConsPub)
	cfg.Seal()
}

func TestLoadGenState(t *testing.T) {
	// change these if you would like to test the genesis locally
	srcPath := "/home/evan/mamaki-326472.json"
	dstPath := "/home/evan/.testytest/config/genesis.json"
	newstate, kr, err := Fork(srcPath, dstPath, "validator")
	require.NoError(t, err)

	// try to run a testnode w/ the modified genesis
	tmNode, _, cctx, err := testnode.New(
		t,
		testnode.DefaultParams(),
		testnode.DefaultTendermintConfig(),
		false,
		newstate,
		kr,
	)
	require.NoError(t, err)

	cctx, stopNode, err := testnode.StartNode(tmNode, cctx)

	err = cctx.WaitForNextBlock()
	err = cctx.WaitForNextBlock()
	err = cctx.WaitForNextBlock()
	err = cctx.WaitForNextBlock()
	err = cctx.WaitForNextBlock()
	require.NoError(t, err)

	stopNode()
}

func TestFork(t *testing.T) {
	srcPath := "/home/evan/mamaki-326472.json"
	dstPath := "/home/evan/.testytest/config/genesis.json"
	_, _, err := Fork(srcPath, dstPath)
	require.NoError(t, err)
}
