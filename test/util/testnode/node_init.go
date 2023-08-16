package testnode

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/genutil"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/tendermint/tendermint/config"
	tmos "github.com/tendermint/tendermint/libs/os"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
)

func collectGenFiles(tmCfg *config.Config, encCfg encoding.Config, pubKey cryptotypes.PubKey, nodeID, rootDir string) error {
	gentxsDir := filepath.Join(rootDir, "gentxs")

	genFile := tmCfg.GenesisFile()
	genDoc, err := types.GenesisDocFromFile(genFile)
	if err != nil {
		return err
	}

	initCfg := genutiltypes.NewInitConfig(genDoc.ChainID, gentxsDir, nodeID, pubKey)

	appState, err := genutil.GenAppStateFromConfig(
		encCfg.Codec,
		encCfg.TxConfig,
		tmCfg,
		initCfg,
		*genDoc,
		banktypes.GenesisBalancesIterator{},
	)
	if err != nil {
		return err
	}

	genDoc = &types.GenesisDoc{
		GenesisTime:     genDoc.GenesisTime,
		ChainID:         genDoc.ChainID,
		Validators:      nil,
		AppState:        appState,
		ConsensusParams: genDoc.ConsensusParams,
	}

	if err := genDoc.ValidateAndComplete(); err != nil {
		return err
	}

	return genDoc.SaveAs(genFile)
}

func initGenFiles(
	cparams *tmproto.ConsensusParams,
	state map[string]json.RawMessage,
	_ codec.Codec,
	file,
	chainID string,
	genTime time.Time,
) error {
	appGenStateJSON, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	genDoc := types.GenesisDoc{
		GenesisTime:     genTime,
		ChainID:         chainID,
		AppState:        appGenStateJSON,
		ConsensusParams: cparams,
		Validators:      nil,
	}

	return genDoc.SaveAs(file)
}

// createValidator creates a genesis transaction for adding a validator account.
// The transaction is stored in the `test.json` file under the 'baseDir/gentxs`.
func createValidator(
	kr keyring.Keyring,
	encCfg encoding.Config,
	pubKey cryptotypes.PubKey,
	valopAcc,
	nodeID,
	chainID,
	baseDir string,
) error {
	rec, err := kr.Key(valopAcc)
	if err != nil {
		return err
	}
	addr, err := rec.GetAddress()
	if err != nil {
		return err
	}
	p2pAddr, _, err := server.FreeTCPAddr()
	if err != nil {
		return err
	}
	p2pURL, err := url.Parse(p2pAddr)
	if err != nil {
		return err
	}
	commission, err := sdk.NewDecFromStr("0.5")
	if err != nil {
		return err
	}

	createValMsg, err := stakingtypes.NewMsgCreateValidator(
		sdk.ValAddress(addr),
		pubKey,
		sdk.NewCoin(app.BondDenom, sdk.NewInt(100000000)),
		stakingtypes.NewDescription("test", "", "", "", ""),
		stakingtypes.NewCommissionRates(commission, sdk.OneDec(), sdk.OneDec()),
		sdk.OneInt(),
	)
	if err != nil {
		return err
	}

	memo := fmt.Sprintf("%s@%s:%s", nodeID, p2pURL.Hostname(), p2pURL.Port())
	fee := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1)))
	txBuilder := encCfg.TxConfig.NewTxBuilder()
	err = txBuilder.SetMsgs(createValMsg)
	if err != nil {
		return err
	}
	txBuilder.SetFeeAmount(fee)    // Arbitrary fee
	txBuilder.SetGasLimit(1000000) // Need at least 100386
	txBuilder.SetMemo(memo)

	txFactory := tx.Factory{}
	txFactory = txFactory.
		WithChainID(chainID).
		WithMemo(memo).
		WithKeybase(kr).
		WithTxConfig(encCfg.TxConfig)

	err = tx.Sign(txFactory, valopAcc, txBuilder, true)
	if err != nil {
		return err
	}

	txBz, err := encCfg.TxConfig.TxJSONEncoder()(txBuilder.GetTx())
	if err != nil {
		return err
	}
	gentxsDir := filepath.Join(baseDir, "gentxs")
	return writeFile(fmt.Sprintf("%v.json", "test"), gentxsDir, txBz)
}

func writeFile(name string, dir string, contents []byte) error {
	writePath := filepath.Join(dir)
	file := filepath.Join(writePath, name)

	err := tmos.EnsureDir(writePath, 0o755)
	if err != nil {
		return err
	}

	err = os.WriteFile(file, contents, 0o644) // nolint: gosec
	if err != nil {
		return err
	}

	return nil
}

func initFileStructure(t testing.TB, tmCfg *config.Config) (string, error) {
	basePath := filepath.Join(t.TempDir(), ".celestia-app")
	tmCfg.SetRoot(basePath)
	configPath := filepath.Join(basePath, "config")
	err := os.MkdirAll(configPath, os.ModePerm)
	if err != nil {
		return "", err
	}
	return basePath, nil
}
