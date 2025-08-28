package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/test/util/genesis"
	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	minfeetypes "github.com/celestiaorg/celestia-app/v6/x/minfee/types"
	"github.com/celestiaorg/go-square/v2/share"
	cmtconfig "github.com/cometbft/cometbft/config"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	cmtos "github.com/cometbft/cometbft/libs/os"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/p2p/pex"
	"github.com/cometbft/cometbft/privval"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
)

// NodeInfo is a struct that contains the name, IP address, and network address
// of a node.
type NodeInfo struct {
	Name           string `json:"name"`
	IP             string `json:"ip"`
	NetworkAddress string `json:"network_address"`
	Region         string `json:"region"`
}

func (n NodeInfo) PeerID() string {
	return fmt.Sprintf("%s@%s:26656", n.NetworkAddress, n.IP)
}

// Network maintains the initial state of the network. This includes the
// genesis, all relevant validators included in the genesis, and all accounts.
type Network struct {
	genesis *genesis.Genesis
	ecfg    encoding.Config

	validators map[string]NodeInfo
	accounts   []string
}

func NewNetwork(chainID string, squareSize int, mods ...genesis.Modifier) (*Network, error) {
	codec := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	blobParams := blobtypes.DefaultParams()
	blobParams.GovMaxSquareSize = uint64(squareSize)
	cparams := app.DefaultConsensusParams()
	cparams.Block.MaxBytes = int64(squareSize * squareSize * share.ContinuationSparseShareContentSize)

	mods = append(mods, genesis.ImmediateProposals(codec.Codec))
	mods = append(mods, genesis.SetBlobParams(codec.Codec, blobParams))

	g := genesis.NewDefaultGenesis().
		WithChainID(chainID).
		WithModifiers(mods...).
		WithConsensusParams(cparams)

	return &Network{
		genesis:    g,
		validators: make(map[string]NodeInfo),
		ecfg:       codec,
	}, nil
}

func SetMinFee(codec codec.Codec, minFee float64) genesis.Modifier {
	return func(state map[string]json.RawMessage) map[string]json.RawMessage {
		minFeeGenState := minfeetypes.DefaultGenesis()
		gasPrice, err := sdkmath.LegacyNewDecFromStr(fmt.Sprintf("%f", minFee))
		if err != nil {
			panic(err)
		}
		minFeeGenState.NetworkMinGasPrice = gasPrice
		state[minfeetypes.ModuleName] = codec.MustMarshalJSON(minFeeGenState)
		return state
	}
}

// AddValidator adds a validator to the network. The validator is identified by
// its name which is assigned by pulumi as hardware is allocated. An additional
// account and keyring are saved to the payload directory that can be used by
// txsim.
// if the stake is set to 0, a default value is used.
func (n *Network) AddValidator(name, ip, payLoadRoot, region string, stake int64) error {
	n.validators[name] = NodeInfo{
		Name:   name,
		IP:     ip,
		Region: region,
	}

	val := genesis.NewDefaultValidator(name)
	if stake != 0 {
		val.Stake = stake
	}
	err := n.genesis.NewValidator(val)
	if err != nil {
		return err
	}

	// add a txsim key and keyring to each validator
	kr, err := keyring.New(app.Name, keyring.BackendTest,
		filepath.Join(payLoadRoot, name), nil, n.ecfg.Codec)
	if err != nil {
		return err
	}

	// import the validator's state key to its own personal keyring
	gkr := n.genesis.Keyring()

	valPriv, err := gkr.ExportPrivKeyArmor(name, "congest")
	if err != nil {
		return err
	}

	err = kr.ImportPrivKey("validator", valPriv, "congest")
	if err != nil {
		return err
	}

	key, _, err := kr.NewMnemonic("txsim", keyring.English, "", "", hd.Secp256k1)
	if err != nil {
		return err
	}

	pk, err := key.GetPubKey()
	if err != nil {
		return err
	}

	addr, err := key.GetAddress()
	if err != nil {
		return err
	}

	fmt.Println("adding txsim account", addr.String())

	err = n.genesis.AddAccount(genesis.Account{
		PubKey:  pk,
		Balance: 9999999999999999,
		Name:    "txsim",
	})
	if err != nil {
		return err
	}

	return nil
}

func (n *Network) Peers() []string {
	var peers []string //nolint:prealloc
	for _, v := range n.validators {
		if v.IP == "" {
			continue
		}
		peers = append(peers, v.PeerID())
	}
	return peers
}

func (n *Network) InitNodes(rootDir string) error {
	if len(n.accounts) != 0 {
		n.genesis.WithKeyringAccounts(genesis.NewKeyringAccounts(genesis.DefaultInitialBalance, n.accounts...)...)
	}

	// save the genesis file
	genesisPath := filepath.Join(rootDir, "genesis.json")

	genDoc, err := n.genesis.Export()
	if err != nil {
		return err
	}

	genBytes, err := cmtjson.MarshalIndent(genDoc, "", "  ")
	if err != nil {
		return err
	}

	// save the genesis file as configured
	err = cmtos.WriteFile(genesisPath, genBytes, 0o644)
	if err != nil {
		return err
	}

	fmt.Println("genesis file saved to", genesisPath, "with", len(n.validators), "validators")

	vals := n.genesis.Validators()
	for _, v := range vals {
		valPath := filepath.Join(rootDir, v.Name)
		nodeKeyFile := filepath.Join(valPath, "node_key.json")
		if err := cmtos.EnsureDir(filepath.Dir(nodeKeyFile), 0o777); err != nil {
			return err
		}

		// add the network key assigned by the genesis to that validator's payload
		nodeKey := &p2p.NodeKey{
			PrivKey: v.NetworkKey,
		}
		if err := nodeKey.SaveAs(nodeKeyFile); err != nil {
			return err
		}
		ninfo, has := n.validators[v.Name]
		if !has {
			return fmt.Errorf("no validator found %s", v.Name)
		}
		ninfo.NetworkAddress = string(nodeKey.ID())
		n.validators[v.Name] = ninfo

		// generate remaining private key file using the assigned consensus key
		pvStateFile := filepath.Join(valPath, "priv_validator_state.json")
		if err := cmtos.EnsureDir(filepath.Dir(pvStateFile), 0o777); err != nil {
			return err
		}
		pvKeyFile := filepath.Join(valPath, "priv_validator_key.json")
		if err := cmtos.EnsureDir(filepath.Dir(pvKeyFile), 0o777); err != nil {
			return err
		}
		filePV := privval.NewFilePV(v.ConsensusKey, pvKeyFile, pvStateFile)
		filePV.Save()

		cmtcfg := cmtconfig.DefaultConfig()
		cmtconfig.WriteConfigFile(filepath.Join(rootDir, v.Name, "config.toml"), cmtcfg)

		appcfg := app.DefaultAppConfig()
		serverconfig.WriteConfigFile(filepath.Join(rootDir, v.Name, "app.toml"), appcfg)
	}

	return nil
}

// SaveValidatorsToFile saves the validators map as a JSON to the given file.
func (n *Network) SaveValidatorsToFile(filename string) error {
	// Open the file for writing. Create it if it doesn't exist.
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// Encode the validators map to JSON and write it to the file.
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ") // Optional: format the JSON with indentation
	err = encoder.Encode(n.validators)
	if err != nil {
		return err
	}

	return nil
}

func (n *Network) SaveAddressBook(payloadRoot string, peers []string) error {
	addrBookFile := filepath.Join(payloadRoot, "addrbook.json")
	return WriteAddressBook(peers, addrBookFile)
}

type Option func(*cmtconfig.Config)

func WriteAddressBook(peers []string, file string) error {
	book := pex.NewAddrBook(file, false)
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

type Regions struct {
	DigitalOcean map[string]int
	Linode       map[string]int
}
