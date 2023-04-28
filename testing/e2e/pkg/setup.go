package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	sdk "github.com/cosmos/cosmos-sdk/types"
	auth "github.com/cosmos/cosmos-sdk/x/auth/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	slashing "github.com/cosmos/cosmos-sdk/x/slashing/types"
	staking "github.com/cosmos/cosmos-sdk/x/staking/types"
	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/ethereum/go-ethereum/common"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/pex"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/types"
)

func Setup(ctx context.Context, testnet *Testnet) error {
	// Ensure that all the requisite images are available
	if err := SetupImages(ctx, testnet); err != nil {
		return fmt.Errorf("setting up images: %w", err)
	}

	fmt.Printf("Setting up network %s\n", testnet.Name)

	_, err := os.Stat(testnet.Dir)
	if err == nil {
		return errors.New("testnet directory already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("checking if testnet directory exists: %w", err)
	}

	// Create the directory for the testnet
	if err := os.MkdirAll(testnet.Dir, os.ModePerm); err != nil {
		return err
	}
	cleanup := func() { os.RemoveAll(testnet.Dir) }

	// Create the docker compose file
	if err := WriteDockerCompose(testnet, filepath.Join(testnet.Dir, "docker-compose.yml")); err != nil {
		cleanup()
		return fmt.Errorf("setting up docker compose: %w", err)
	}

	// Make the genesis file for the testnet
	genesis, err := MakeGenesis(testnet)
	if err != nil {
		cleanup()
		return fmt.Errorf("making genesis: %w", err)
	}

	// Initialize the file system and configs for each node
	for name, node := range testnet.Nodes {
		err := InitNode(node, genesis, testnet.Dir)
		if err != nil {
			cleanup()
			return fmt.Errorf("initializing node %s: %w", name, err)
		}
	}

	return nil
}

// SetupImages ensures that all the requisite docker images for each
// used celestia consensus version.
func SetupImages(ctx context.Context, testnet *Testnet) error {
	c, err := client.NewClientWithOpts()
	if err != nil {
		return fmt.Errorf("establishing docker client: %v", err)
	}

	versions := testnet.GetAllVersions()

	for _, v := range versions {
		if v == "current" {
			// we assume that the user has locally downloaded
			// the current docker image
			continue
		}
		refStr := dockerSrcURL + ":" + v
		fmt.Printf("Pulling in docker image: %s\n", refStr)
		rc, err := c.ImagePull(ctx, refStr, dockertypes.ImagePullOptions{})
		if err != nil {
			return fmt.Errorf("error pulling image %s: %w", refStr, err)
		}
		_, _ = io.Copy(io.Discard, rc)
		_ = rc.Close()
	}

	return nil
}

func MakeGenesis(testnet *Testnet) (types.GenesisDoc, error) {
	encCdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	appGenState := app.ModuleBasics.DefaultGenesis(encCdc.Codec)
	bankGenesis := bank.DefaultGenesisState()
	stakingGenesis := staking.DefaultGenesisState()
	slashingGenesis := slashing.DefaultGenesisState()
	genAccs := []auth.GenesisAccount{}
	stakingGenesis.Params.BondDenom = app.BondDenom
	delegations := make([]staking.Delegation, 0, len(testnet.Nodes))
	valInfo := make([]slashing.SigningInfo, 0, len(testnet.Nodes))
	balances := make([]bank.Balance, 0, len(testnet.Accounts)+1)
	var (
		validators  staking.Validators
		totalBonded int64
	)

	// setup the validator information on the state machine
	for name, node := range testnet.Nodes {
		if !node.IsValidator() || node.StartHeight != 0 {
			continue
		}

		addr := node.AccountKey.PubKey().Address()
		pk, err := cryptocodec.FromTmPubKeyInterface(node.SignerKey.PubKey())
		if err != nil {
			return types.GenesisDoc{}, fmt.Errorf("converting public key for node %s: %w", node.Name, err)
		}
		pkAny, err := codectypes.NewAnyWithValue(pk)
		if err != nil {
			return types.GenesisDoc{}, err
		}
		evmAddress := common.HexToAddress(crypto.CRandHex(common.AddressLength))

		validators = append(validators, staking.Validator{
			OperatorAddress: sdk.ValAddress(addr).String(),
			ConsensusPubkey: pkAny,
			Description: staking.Description{
				Moniker: name,
			},
			Status:          staking.Bonded,
			Tokens:          sdk.NewInt(node.SelfDelegation),
			DelegatorShares: sdk.OneDec(),
			// 5% commission
			Commission:        staking.NewCommission(sdk.NewDecWithPrec(5, 2), sdk.OneDec(), sdk.OneDec()),
			MinSelfDelegation: sdk.ZeroInt(),
			EvmAddress:        evmAddress.Hex(),
		})
		totalBonded += node.SelfDelegation
		consensusAddr := pk.Address()
		delegations = append(delegations, staking.NewDelegation(sdk.AccAddress(addr), sdk.ValAddress(addr), sdk.OneDec()))
		valInfo = append(valInfo, slashing.SigningInfo{
			Address:              sdk.ConsAddress(consensusAddr).String(),
			ValidatorSigningInfo: slashing.NewValidatorSigningInfo(sdk.ConsAddress(consensusAddr), 1, 0, time.Unix(0, 0), false, 0),
		})
	}
	stakingGenesis.Delegations = delegations
	stakingGenesis.Validators = validators
	slashingGenesis.SigningInfos = valInfo

	accountNumber := uint64(0)
	for _, account := range testnet.Accounts {
		pk, err := cryptocodec.FromTmPubKeyInterface(account.Key.PubKey())
		if err != nil {
			return types.GenesisDoc{}, fmt.Errorf("converting public key for account %s: %w", account.Name, err)
		}

		addr := pk.Address()
		acc := auth.NewBaseAccount(addr.Bytes(), pk, accountNumber, 0)
		genAccs = append(genAccs, acc)
		balances = append(balances, bank.Balance{
			Address: sdk.AccAddress(addr).String(),
			Coins: sdk.NewCoins(
				sdk.NewCoin(app.BondDenom, sdk.NewInt(account.Tokens)),
			),
		})
	}
	// add bonded amount to bonded pool module account
	balances = append(balances, bank.Balance{
		Address: auth.NewModuleAddress(staking.BondedPoolName).String(),
		Coins:   sdk.Coins{sdk.NewCoin(app.BondDenom, sdk.NewInt(totalBonded))},
	})
	bankGenesis.Balances = bank.SanitizeGenesisBalances(balances)
	authGenesis := auth.NewGenesisState(auth.DefaultParams(), genAccs)

	// update the original genesis state
	appGenState[bank.ModuleName] = encCdc.Codec.MustMarshalJSON(bankGenesis)
	appGenState[auth.ModuleName] = encCdc.Codec.MustMarshalJSON(authGenesis)
	appGenState[staking.ModuleName] = encCdc.Codec.MustMarshalJSON(stakingGenesis)
	appGenState[slashing.ModuleName] = encCdc.Codec.MustMarshalJSON(slashingGenesis)

	if err := app.ModuleBasics.ValidateGenesis(encCdc.Codec, encCdc.TxConfig, appGenState); err != nil {
		return types.GenesisDoc{}, fmt.Errorf("validating genesis: %w", err)
	}

	appState, err := json.MarshalIndent(appGenState, "", " ")
	if err != nil {
		return types.GenesisDoc{}, fmt.Errorf("marshaling app state: %w", err)
	}

	// Validator set and app hash are set in InitChain
	return types.GenesisDoc{
		ChainID:         testnet.Name,
		GenesisTime:     time.Now().UTC(),
		ConsensusParams: types.DefaultConsensusParams(),
		AppState:        appState,
		// AppHash is not provided but computed after InitChain
	}, nil
}

func MakeConfig(node *Node) (*config.Config, error) {
	cfg := config.DefaultConfig()
	cfg.Moniker = node.Name
	cfg.RPC.ListenAddress = "tcp://0.0.0.0:26657"
	cfg.P2P.ExternalAddress = fmt.Sprintf("tcp://%v", node.AddressP2P(false))
	cfg.P2P.PersistentPeers = strings.Join(node.Peers, ",")

	// TODO: when we use adaptive timeouts, add a parameter in the testnet manifest
	// to set block times
	// FIXME: This values get overridden by the timeout consts in the app package.
	// We should modify this if we want to quicken the time of the blocks.
	cfg.Consensus.TimeoutPropose = 1000 * time.Millisecond
	cfg.Consensus.TimeoutCommit = 300 * time.Millisecond
	return cfg, nil
}

func InitNode(node *Node, genesis types.GenesisDoc, rootDir string) error {
	// Initialize file directories
	nodeDir := filepath.Join(rootDir, node.Name)
	for _, dir := range []string{
		filepath.Join(nodeDir, "config"),
		filepath.Join(nodeDir, "data"),
	} {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return fmt.Errorf("error creating directory %s: %w", dir, err)
		}
	}

	// Create and write the config file
	cfg, err := MakeConfig(node)
	if err != nil {
		return fmt.Errorf("making config: %w", err)
	}
	config.WriteConfigFile(filepath.Join(nodeDir, "config", "config.toml"), cfg)

	// Store the genesis file
	err = genesis.SaveAs(filepath.Join(nodeDir, "config", "genesis.json"))
	if err != nil {
		return fmt.Errorf("saving genesis: %w", err)
	}

	// Create the app.toml file
	appConfig, err := MakeAppConfig(node)
	if err != nil {
		return fmt.Errorf("making app config: %w", err)
	}
	serverconfig.WriteConfigFile(filepath.Join(nodeDir, "config", "app.toml"), appConfig)

	// Store the node key for the p2p handshake
	err = (&p2p.NodeKey{PrivKey: node.NetworkKey}).SaveAs(filepath.Join(nodeDir, "config", "node_key.json"))
	if err != nil {
		return err
	}

	// Store the validator signer key for consensus
	(privval.NewFilePV(node.SignerKey,
		filepath.Join(nodeDir, "config", "priv_validator_key.json"),
		filepath.Join(nodeDir, "data", "priv_validator_state.json"),
	)).Save()

	return nil
}

func WriteDockerCompose(testnet *Testnet, file string) error {
	tmpl, err := template.New("docker-compose").Parse(`version: '2.4'

networks:
  {{ .Name }}:
    labels:
      e2e: true
    driver: bridge
    ipam:
      driver: default
      config:
      - subnet: {{ .IP }}

services:
{{- range .Nodes }}
  {{ .Name }}:
    labels:
      e2e: true
    container_name: {{ .Name }}
    image: ghcr.io/celestiaorg/celestia-app:{{ index .Versions 0 }}
    entrypoint: ["/bin/celestia-appd"]
    command: ["start"]
    init: true
    ports:
    - 26656
    - {{ if .ProxyPort }}{{ .ProxyPort }}:{{ end }}26657
    - 6060
    - 9090
    - 1317
    volumes:
    - ./{{ .Name }}:/home/celestia/.celestia-app
    networks:
      {{ $.Name }}:
        ipv4_address: {{ .IP }}

{{end}}`)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, testnet)
	if err != nil {
		return err
	}
	return os.WriteFile(file, buf.Bytes(), 0o644)
}

func WriteAddressBook(peers []string, file string) error {
	book := pex.NewAddrBook(file, true)
	for _, peer := range peers {
		addr, err := p2p.NewNetAddressString(peer)
		if err != nil {
			return fmt.Errorf("parsing peer address %s: %w", peer, err)
		}
		err = book.AddAddress(addr, addr)
		if err != nil {
			return fmt.Errorf("adding peer address %s: %w", peer, err)
		}
	}
	book.Save()
	return nil
}

func MakeAppConfig(node *Node) (*serverconfig.Config, error) {
	srvCfg := serverconfig.DefaultConfig()
	srvCfg.MinGasPrices = fmt.Sprintf("0.001%s", app.BondDenom)
	return srvCfg, srvCfg.ValidateBasic()
}
