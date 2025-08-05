package testnode

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path"
	"sync/atomic"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/test/util/random"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// portCounter is a global atomic counter for deterministic port allocation
// Starting from 20000 to avoid conflicts with common ports
var portCounter atomic.Int64

func init() {
	portCounter.Store(20000)
}

func TestAddress() sdk.AccAddress {
	bz, err := sdk.GetFromBech32(testfactory.TestAccAddr, appconsts.MainnetChainID)
	if err != nil {
		panic(err)
	}
	return sdk.AccAddress(bz)
}

func QueryWithoutProof(clientCtx client.Context, hashHexStr string) (*rpctypes.ResultTx, error) {
	hash, err := hex.DecodeString(hashHexStr)
	if err != nil {
		return nil, err
	}

	node, err := clientCtx.GetNode()
	if err != nil {
		return nil, err
	}

	return node.Tx(context.Background(), hash, false)
}

func NewKeyring(accounts ...string) (keyring.Keyring, []sdk.AccAddress) {
	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...).Codec
	kb := keyring.NewInMemory(cdc)

	addresses := make([]sdk.AccAddress, len(accounts))
	for idx, acc := range accounts {
		rec, _, err := kb.NewMnemonic(acc, keyring.English, "", "", hd.Secp256k1)
		if err != nil {
			panic(err)
		}
		addr, err := rec.GetAddress()
		if err != nil {
			panic(err)
		}
		addresses[idx] = addr
	}
	return kb, addresses
}

func RandomAddress() sdk.Address {
	name := random.Str(6)
	_, addresses := NewKeyring(name)
	return addresses[0]
}

func FundKeyringAccounts(accounts ...string) (keyring.Keyring, []banktypes.Balance, []authtypes.GenesisAccount) {
	kr, addresses := NewKeyring(accounts...)
	genAccounts := make([]authtypes.GenesisAccount, len(accounts))
	genBalances := make([]banktypes.Balance, len(accounts))

	for i, addr := range addresses {
		balances := sdk.NewCoins(
			sdk.NewCoin(appconsts.BondDenom, math.NewInt(DefaultInitialBalance)),
		)

		genBalances[i] = banktypes.Balance{Address: addr.String(), Coins: balances.Sort()}
		genAccounts[i] = authtypes.NewBaseAccount(addr, nil, uint64(i), 0)
	}
	return kr, genBalances, genAccounts
}

func GenerateAccounts(count int) []string {
	accs := make([]string, count)
	for i := 0; i < count; i++ {
		accs[i] = random.Str(20)
	}
	return accs
}

// GetFreePort returns a free port and optionally an error.
func GetFreePort() (int, error) {
	a, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", a)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// isPortAvailable checks if a port is available by attempting to listen on it.
func isPortAvailable(port int) bool {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	defer l.Close()
	return true
}

// GetDeterministicPort returns a deterministic port using an atomic counter.
// This eliminates race conditions by ensuring each call gets a unique port.
// It checks port availability and increments until it finds an open port.
func GetDeterministicPort() int {
	for {
		port := int(portCounter.Add(1))
		if isPortAvailable(port) {
			return port
		}
		// If port is not available, the loop will continue with the next increment
	}
}

// removeDir removes the directory `rootDir`.
// The main use of this is to reduce the flakiness of the CI when it's unable to delete
// the config folder of the tendermint node.
// This will manually go over the files contained inside the provided `rootDir`
// and delete them one by one.
func removeDir(rootDir string) error {
	dir, err := os.ReadDir(rootDir)
	if err != nil {
		return err
	}
	for _, d := range dir {
		path := path.Join([]string{rootDir, d.Name()}...)
		err := os.RemoveAll(path)
		if err != nil {
			return err
		}
	}
	return os.RemoveAll(rootDir)
}
