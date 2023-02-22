package testnode

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

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
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/tendermint/tendermint/config"
	tmos "github.com/tendermint/tendermint/libs/os"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
	tmtime "github.com/tendermint/tendermint/types/time"
)

// NodeEVMPrivateKey the key used to initialize the test node validator.
// Its corresponding address is: "0x9c2B12b5a07FC6D719Ed7646e5041A7E85758329".
var NodeEVMPrivateKey, _ = crypto.HexToECDSA("64a1d6f0e760a8d62b4afdde4096f16f51b401eaaecc915740f71770ea76a8ad")

func collectGenFiles(tmCfg *config.Config, encCfg encoding.Config, pubKey cryptotypes.PubKey, nodeID, chainID, rootDir string) error {
	genTime := tmtime.Now()

	gentxsDir := filepath.Join(rootDir, "gentxs")

	initCfg := genutiltypes.NewInitConfig(chainID, gentxsDir, nodeID, pubKey)

	genFile := tmCfg.GenesisFile()
	genDoc, err := types.GenesisDocFromFile(genFile)
	if err != nil {
		return err
	}

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
		GenesisTime:     genTime,
		ChainID:         chainID,
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
	codec codec.Codec,
	file,
	chainID string,
) error {
	appGenStateJSON, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	genDoc := types.GenesisDoc{
		ChainID:         chainID,
		AppState:        appGenStateJSON,
		ConsensusParams: cparams,
		Validators:      nil,
	}

	return genDoc.SaveAs(file)
}

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
	orchEVMPublicKey := NodeEVMPrivateKey.Public().(*ecdsa.PublicKey)
	evmAddr := crypto.PubkeyToAddress(*orchEVMPublicKey)

	createValMsg, err := stakingtypes.NewMsgCreateValidator(
		sdk.ValAddress(addr),
		pubKey,
		sdk.NewCoin(app.BondDenom, sdk.NewInt(100000000)),
		stakingtypes.NewDescription("test", "", "", "", ""),
		stakingtypes.NewCommissionRates(commission, sdk.OneDec(), sdk.OneDec()),
		sdk.OneInt(),
		evmAddr,
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

func initFileStructure(t *testing.T, tmCfg *config.Config) (string, error) {
	basePath := filepath.Join(t.TempDir(), ".celestia-app")
	tmCfg.SetRoot(basePath)
	configPath := filepath.Join(basePath, "config")
	err := os.MkdirAll(configPath, os.ModePerm)
	if err != nil {
		return "", err
	}
	return basePath, nil
}
