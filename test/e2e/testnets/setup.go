package testnets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/pex"
	"github.com/tendermint/tendermint/pkg/trace"
	"github.com/tendermint/tendermint/pkg/trace/schema"
)

func MakeConfig(node *Node) (*config.Config, error) {
	cfg := config.DefaultConfig()
	cfg.Moniker = node.Name
	cfg.RPC.ListenAddress = "tcp://0.0.0.0:26657"
	cfg.P2P.ExternalAddress = fmt.Sprintf("tcp://%v", node.AddressP2P(false))
	cfg.P2P.PersistentPeers = strings.Join(node.InitialPeers, ",")
	cfg.P2P.SendRate = 5 * 1024 * 1024 // 5MiB/s
	cfg.P2P.RecvRate = 5 * 1024 * 1024 // 5MiB/s
	cfg.Consensus.TimeoutPropose = 1 * time.Second
	cfg.Consensus.TimeoutCommit = 1 * time.Second
	cfg.Instrumentation.Prometheus = true
	cfg.Instrumentation.TraceType = "local"
	cfg.Instrumentation.TraceBufferSize = 2500
	cfg.Instrumentation.TracingTables = strings.Join(schema.AllTables(), ",")
	cfg.Instrumentation.TracePullAddress = ""
	cfg.Instrumentation.TracePushConfig = "s3.json"
	return cfg, nil
}

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

func MakeAppConfig(_ *Node) (*serverconfig.Config, error) {
	srvCfg := serverconfig.DefaultConfig()
	srvCfg.MinGasPrices = fmt.Sprintf("0.001%s", app.BondDenom)
	return srvCfg, srvCfg.ValidateBasic()
}

func MakeTracePushConfig(configPath string) error {
	traceConfigFile, err := os.OpenFile(filepath.Join(configPath, "s3.json"), os.O_CREATE|os.O_RDWR, 0777)
	if err != nil {
		return err
	}
	defer traceConfigFile.Close()
	traceConfig := trace.S3Config{
		BucketName: "block-prop-traces-ef",
		AccessKey:  GetAccessKeyEnvVar(),
		SecretKey:  GetSecretKeyEnvVar(),
		Region:     "us-east-2",
		PushDelay:  200,
	}
	err = json.NewEncoder(traceConfigFile).Encode(traceConfig)
	if err != nil {
		return err
	}
	traceConfigFile.Close()
	return nil
}

// GetAccessKeyEnvVar returns the AWS s3 bucket access key ID from the
// environment.
func GetAccessKeyEnvVar() string {
	return "AKIAZ2OBUXMTQUDOS3WM" // dgaf key cause minio is down
}

// GetSecretKeyEnvVar returns the AWS s3 bucket secret access key from the
func GetSecretKeyEnvVar() string {
	return "whaQb/Jww98DQMsZ588Nq9ylZtrl09lM/u95lo5a" // dgaf key cause minio is down
}
