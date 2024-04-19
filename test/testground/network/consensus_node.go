package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/cmd/celestia-appd/cmd"
	"github.com/celestiaorg/celestia-app/test/util/genesis"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	srvtypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	tmconfig "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto/ed25519"
	cmtos "github.com/tendermint/tendermint/libs/os"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/pex"
	"github.com/tendermint/tendermint/privval"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

// ConsensusNode is the node type used by testground instances to run a
// celestia-app full node. It can optionally be configured to be a validator,
// and has methods to bootstrap a network, initialize itself, start, and stop.
type ConsensusNode struct {
	Name string
	// NetworkKey is the key used for signing gossiped messages.
	networkKey ed25519.PrivKey
	// ConsensusKey is the key used for signing votes.
	consensusKey ed25519.PrivKey

	kr   keyring.Keyring
	ecfg encoding.Config

	params    *Params
	CmtConfig *tmconfig.Config
	AppConfig *srvconfig.Config
	baseDir   string

	cctx testnode.Context

	stopFuncs []func() error
	// AppOptions are the application options of the test node.
	AppOptions *testnode.KVAppOptions
	// AppCreator is used to create the application for the testnode.
	AppCreator srvtypes.AppCreator
	// SuppressLogs in testnode. This should be set to true when running
	// testground tests unless debugging.
	SuppressLogs bool

	cmtNode *node.Node
}

// Bootstrap is the first function called in a test by each node. It is
// responsible for initializing the node and creating a gentx if this node is a
// validator.
func (cn *ConsensusNode) Bootstrap(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) ([]PeerPacket, error) {
	cn.ecfg = encoding.MakeConfig(app.ModuleBasics)

	ip, err := initCtx.NetClient.GetDataNetworkIP()
	if err != nil {
		return nil, err
	}

	params, err := ParseParams(cn.ecfg, runenv)
	if err != nil {
		return nil, err
	}
	cn.params = params

	nodeID := NodeID(initCtx.GlobalSeq)
	cn.Name = nodeID

	kr, addrs := testnode.NewKeyring(nodeID, TxSimAccountName)
	cn.kr = kr

	val := genesis.NewDefaultValidator(nodeID)
	ckey, ok := val.ConsensusKey.(ed25519.PrivKey)
	if !ok {
		return nil, errors.New("invalid consensus key type")
	}
	cn.consensusKey = ckey
	nkey, ok := val.NetworkKey.(ed25519.PrivKey)
	if !ok {
		return nil, errors.New("invalid network key type")
	}
	cn.networkKey = nkey

	var bz []byte
	if runenv.TestGroupID == ValidatorGroupID {
		gentx, err := val.GenTx(cn.ecfg, cn.kr, cn.params.ChainID)
		if err != nil {
			return nil, err
		}
		bz, err = cn.ecfg.TxConfig.TxJSONEncoder()(gentx)
		if err != nil {
			return nil, err
		}
	}

	pubKs, err := getPublicKeys(cn.kr, nodeID, TxSimAccountName)
	if err != nil {
		return nil, err
	}

	pp := PeerPacket{
		PeerID:          peerID(ip.String(), cn.networkKey),
		GroupID:         runenv.TestGroupID,
		GlobalSequence:  initCtx.GlobalSeq,
		GenesisAccounts: addrsToStrings(addrs...),
		GenesisPubKeys:  pubKs,
		GenTx:           json.RawMessage(bz),
	}

	_, err = initCtx.SyncClient.Publish(ctx, PeerPacketTopic, pp)
	if err != nil {
		return nil, err
	}

	packets, err := DownloadSync(ctx, initCtx, PeerPacketTopic, PeerPacket{}, runenv.TestInstanceCount)
	if err != nil {
		return nil, err
	}

	return packets, nil
}

// Init creates the files required by tendermint and celestia-app using the data
// downloaded from the Leader node.
func (cn *ConsensusNode) Init(baseDir string, genesis json.RawMessage, mcfg RoleConfig) error {
	cn.CmtConfig = mcfg.CmtConfig
	cn.AppConfig = mcfg.AppConfig
	cn.AppCreator = cmd.NewAppServer
	cn.SuppressLogs = true

	// manually set the protocol version to the one used by the testground
	appOpts := testnode.DefaultAppOptions()
	cn.AppOptions = appOpts

	baseDir = filepath.Join(baseDir, ".celestia-app")
	cn.baseDir = baseDir

	cn.CmtConfig.SetRoot(baseDir)

	// save the genesis file
	configPath := filepath.Join(baseDir, "config")
	err := os.MkdirAll(configPath, os.ModePerm)
	if err != nil {
		return err
	}
	// save the genesis file as configured
	err = cmtos.WriteFile(cn.CmtConfig.GenesisFile(), genesis, 0o644)
	if err != nil {
		return err
	}
	pvStateFile := cn.CmtConfig.PrivValidatorStateFile()
	if err := cmtos.EnsureDir(filepath.Dir(pvStateFile), 0o777); err != nil {
		return err
	}
	pvKeyFile := cn.CmtConfig.PrivValidatorKeyFile()
	if err := cmtos.EnsureDir(filepath.Dir(pvKeyFile), 0o777); err != nil {
		return err
	}
	filePV := privval.NewFilePV(cn.consensusKey, pvKeyFile, pvStateFile)
	filePV.Save()

	nodeKeyFile := cn.CmtConfig.NodeKeyFile()
	if err := cmtos.EnsureDir(filepath.Dir(nodeKeyFile), 0o777); err != nil {
		return err
	}
	nodeKey := &p2p.NodeKey{
		PrivKey: cn.networkKey,
	}
	if err := nodeKey.SaveAs(nodeKeyFile); err != nil {
		return err
	}

	return nil
}

// StartNode uses the testnode package to start a tendermint node with
// celestia-app and the provided configuration.
func (cn *ConsensusNode) StartNode(ctx context.Context, baseDir string) error {
	ucfg := cn.UniversalTestingConfig()
	tmNode, app, err := testnode.NewCometNode(baseDir, &ucfg)
	if err != nil {
		return err
	}

	cn.cmtNode = tmNode
	cctx := testnode.NewContext(ctx, cn.kr, ucfg.TmConfig, cn.params.ChainID, ucfg.AppConfig.API.Address)

	cctx, stopNode, err := testnode.StartNode(tmNode, cctx)
	cn.stopFuncs = append(cn.stopFuncs, stopNode)
	if err != nil {
		return err
	}

	cctx, cleanupGRPC, err := testnode.StartGRPCServer(app, ucfg.AppConfig, cctx)
	cn.stopFuncs = append(cn.stopFuncs, cleanupGRPC)

	cn.cctx = cctx

	return err
}

// Stop stops the node and cleans up the data directory by calling the cleanup
// functions. It returns the last error that was not nil if any of the cleanup
// functions returned an error.
func (cn *ConsensusNode) Stop() error {
	var err error
	for _, stop := range cn.stopFuncs {
		if sterr := stop(); sterr != nil {
			err = sterr
		}
	}
	return err
}

// UniversalTestingConfig returns the configuration used by the testnode package.
func (cn *ConsensusNode) UniversalTestingConfig() testnode.UniversalTestingConfig {
	return testnode.UniversalTestingConfig{
		TmConfig:     cn.CmtConfig,
		AppConfig:    cn.AppConfig,
		AppOptions:   cn.AppOptions,
		AppCreator:   cn.AppCreator,
		SuppressLogs: cn.SuppressLogs,
	}
}

func addrsToStrings(addrs ...sdk.AccAddress) []string {
	strs := make([]string, len(addrs))
	for i, addr := range addrs {
		strs[i] = addr.String()
	}
	return strs
}

func getPublicKeys(kr keyring.Keyring, accounts ...string) ([]string, error) {
	keys := make([]string, 0, len(accounts))
	for _, acc := range accounts {
		rec, err := kr.Key(acc)
		if err != nil {
			return nil, err
		}
		pubK, err := rec.GetPubKey()
		if err != nil {
			return nil, err
		}
		keys = append(keys, SerializePublicKey(pubK))
	}
	return keys, nil
}

func addPeersToAddressBook(path string, peers []PeerPacket) error {
	err := os.MkdirAll(strings.Replace(path, "addrbook.json", "", -1), os.ModePerm)
	if err != nil {
		return err
	}

	addrBook := pex.NewAddrBook(path, false)
	err = addrBook.OnStart()
	if err != nil {
		return err
	}

	for _, peer := range peers {
		id, ip, peerPort, err := parsePeerID(peer.PeerID)
		if err != nil {
			return err
		}
		port, err := safeConvertIntToUint16(peerPort)
		if err != nil {
			return err
		}

		netAddr := p2p.NetAddress{
			ID:   p2p.ID(id),
			IP:   ip,
			Port: port,
		}

		err = addrBook.AddAddress(&netAddr, &netAddr)
		if err != nil {
			return err
		}
	}

	addrBook.Save()
	return nil
}

func parsePeerID(input string) (string, net.IP, int, error) {
	// Define a regular expression to capture the address, IP, and port.
	re := regexp.MustCompile(`^(.*?)@([\d.]+):(\d+)$`)
	match := re.FindStringSubmatch(input)

	if len(match) != 4 {
		return "", nil, 0, fmt.Errorf("invalid input format")
	}

	// Extract the components from the regex match.
	address := match[1]
	ip := net.ParseIP(match[2])
	port := match[3]

	// Convert the port to an integer.
	portInt, err := strconv.Atoi(port)
	if err != nil {
		return "", nil, 0, err
	}

	return address, ip, portInt, nil
}

func safeConvertIntToUint16(x int) (uint16, error) {
	if x > 0 && x <= math.MaxUint16 {
		return uint16(x), nil
	}
	return 0, fmt.Errorf("%v is negative or too large to convert to uint16", x)
}
