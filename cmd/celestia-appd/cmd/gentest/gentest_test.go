package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util/genesis"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func TestGenTxTestBasic(t *testing.T) {
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	tempDir := t.TempDir()
	// create and save a genesis file
	val := genesis.NewDefaultValidator("test")
	// create a validator that doesn't have enough stake to require a signature
	// to produce a block for testing purposes
	val.Stake = 1_000_000

	// we purposefully don't add the validator above yet as a validator. Adding
	// only the account will generate a tx key for the validator account and
	// fund the account in the genesis file.
	g := genesis.NewDefaultGenesis().WithAccounts(val.Account)

	gDoc, err := g.Export()
	require.NoError(t, err)

	// create and save a gentx from the validator created in the first step.
	gentx, err := val.GenTx(ecfg, g.Keyring(), gDoc.ChainID)
	require.NoError(t, err)

	err = saveGenTx(ecfg, gentx, tempDir)
	require.NoError(t, err)

	err = GenTxTest(tempDir, gDoc, []sdk.Tx{gentx})
	require.NoError(t, err)
}

func saveGenTx(ecfg encoding.Config, tx sdk.Tx, dir string) error {
	txBytes, err := ecfg.TxConfig.TxEncoder()(tx)
	if err != nil {
		return err
	}
	dir = fmt.Sprintf("%s/gentxs", dir)
	// create the gentxs directory if it doesn't exist
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return err
	}
	return os.WriteFile(fmt.Sprintf("%s/%s.json", dir, tmrand.Str(6)), txBytes, 0o777)
}
