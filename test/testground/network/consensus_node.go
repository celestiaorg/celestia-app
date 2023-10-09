package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/cmd/celestia-appd/cmd"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/genesis"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	srvtypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	tmconfig "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto/ed25519"
	cmtos "github.com/tendermint/tendermint/libs/os"
	tmos "github.com/tendermint/tendermint/libs/os"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/node"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/pex"
	"github.com/tendermint/tendermint/privval"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

// ConsensusNode is the node type used by testground instances to run a
// celestia-app full node. It can optionally be configured to be a validator,
// and has methods to boostrap a network, initialize itself, start, and stop.
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
	cn.consensusKey = val.ConsensusKey
	cn.networkKey = val.NetworkKey

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

	// // manually save the packets to the address book.
	// if err := addPeersToAddressBook(homeDir, packets); err != nil {
	// 	return nil, err
	// }

	return packets, nil
}

// Init creates the files required by tendermint and celestia-app using the data
// downloaded from the Leader node.
func (cn *ConsensusNode) Init(baseDir string, genesis json.RawMessage, mcfg ConsensusNodeMetaConfig) error {
	cn.CmtConfig = mcfg.CmtConfig
	cn.CmtConfig.Instrumentation.InfluxToken = "_AHFLpgzvTD2e6cOIp1rE_ToLziwKKKq8Vk9oIq9XjBRJB7ZaJiJSc9Upr57DPc7Fz-tZbIM-mH39MB-TiE7qA=="
	cn.AppConfig = mcfg.AppConfig
	cn.AppCreator = cmd.NewAppServer
	cn.AppOptions = testnode.DefaultAppOptions()

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
	if err := tmos.EnsureDir(filepath.Dir(pvStateFile), 0o777); err != nil {
		return err
	}
	pvKeyFile := cn.CmtConfig.PrivValidatorKeyFile()
	if err := tmos.EnsureDir(filepath.Dir(pvKeyFile), 0o777); err != nil {
		return err
	}
	filePV := privval.NewFilePV(cn.consensusKey, pvKeyFile, pvStateFile)
	filePV.Save()

	nodeKeyFile := cn.CmtConfig.NodeKeyFile()
	if err := tmos.EnsureDir(filepath.Dir(nodeKeyFile), 0o777); err != nil {
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
	cctx := testnode.NewContext(ctx, cn.kr, ucfg.TmConfig, cn.params.ChainID)

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

// Stop stops the node and cleans up the data directory.
func (cn *ConsensusNode) Stop() error {
	var err error
	for _, stop := range cn.stopFuncs {
		if sterr := stop(); err != nil {
			err = sterr
		}
	}
	return err
}

// UniversalTestingConfig returns the configuration used by the testnode package.
func (cn *ConsensusNode) UniversalTestingConfig() testnode.UniversalTestingConfig {
	return testnode.UniversalTestingConfig{
		TmConfig:    cn.CmtConfig,
		AppConfig:   cn.AppConfig,
		AppOptions:  cn.AppOptions,
		AppCreator:  cn.AppCreator,
		SupressLogs: false,
	}
}

// SubmitRandomPFB will submit a single PFB using the consensus node's tx
// signing account. One blob will be included for each size provided in a single PFB.
func (c *ConsensusNode) SubmitRandomPFB(ctx context.Context, runenv *runtime.RunEnv, blobSizes ...int) (*sdk.TxResponse, error) {
	runenv.RecordMessage("attempting to get the key")
	if c.kr == nil {
		return nil, errors.New("nil keyring")
	}
	rec, err := c.kr.Key(c.Name)
	if err != nil {
		return nil, err
	}
	runenv.RecordMessage("got key")
	addr, err := rec.GetAddress()
	if err != nil {
		return nil, err
	}
	runenv.RecordMessage("got addr")
	signer, err := user.SetupSigner(ctx, c.kr, c.cctx.GRPCClient, addr, c.ecfg)
	if err != nil {
		return nil, err
	}
	runenv.RecordMessage("created signer")

	r := tmrand.NewRand()

	blobs := blobfactory.RandBlobsWithNamespace(appns.RandomBlobNamespaces(r, len(blobSizes)), blobSizes)
	runenv.RecordMessage("made blobs")
	blobSizesU := make([]uint32, 0, len(blobSizes))
	for _, size := range blobSizes {
		blobSizesU = append(blobSizesU, uint32(size))
	}

	limit := blobtypes.DefaultEstimateGas(blobSizesU)

	runenv.RecordMessage("finished prep for pfb")

	return signer.SubmitPayForBlob(ctx, blobs, user.SetGasLimitAndFee(limit, 0.1))
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
	var filePath string = fmt.Sprintf("%s/config/addrbook.json", path)

	err := os.MkdirAll(path+"/config", os.ModePerm)
	if err != nil {
		return err
	}

	f, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	addrBook := pex.NewAddrBook(filePath, false)

	for _, peer := range peers {
		id, ip, port, err := parsePeerID(peer.PeerID)
		if err != nil {
			return err
		}

		netAddr := p2p.NetAddress{
			ID:   p2p.ID(id),
			IP:   ip,
			Port: uint16(port),
		}

		fmt.Println("routable", netAddr.Routable())

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
		return "", nil, 0, fmt.Errorf("Invalid input format")
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

func getAddresses(runenv *runtime.RunEnv) {
	var filePath string = fmt.Sprintf("%s/config/addrbook.json", homeDir)

	addrBook := pex.NewAddrBook(filePath, false)

	s := addrBook.GetSelection()
	ss := make([]string, 0, len(s))
	for _, addr := range s {
		ss = append(ss, addr.String())
	}

	runenv.RecordMessage(fmt.Sprintf("addresses: %s empty: %v", ss, addrBook.Empty()))
}
