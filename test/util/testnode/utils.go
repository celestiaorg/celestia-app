package testnode

import (
	"context"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/test/util/random"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

func TestAddress() sdk.AccAddress {
	bz, err := sdk.GetFromBech32(testfactory.TestAccAddr, "celestia")
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

// mustGetFreePort returns a free port. Panics if no free ports are available or
// an error is encountered.
func mustGetFreePort() int {
	port, err := GetFreePort()
	if err != nil {
		panic(err)
	}
	return port
}

// IsPortAvailable checks if a port is available on localhost.
func IsPortAvailable(port int) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
	if err != nil {
		// Port is available if we can't connect to it
		return true
	}
	conn.Close()
	return false
}

// KillProcessOnPort attempts to kill processes listening on the specified port.
// It returns an error if the port cleanup fails.
func KillProcessOnPort(port int) error {
	// Use lsof to find processes listening on the port
	cmd := exec.Command("lsof", "-ti", fmt.Sprintf(":%d", port))
	output, err := cmd.Output()
	if err != nil {
		// No processes found on the port, which is good
		return nil
	}

	pids := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, pidStr := range pids {
		if pidStr == "" {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		
		// Kill the process
		if err := exec.Command("kill", "-9", fmt.Sprintf("%d", pid)).Run(); err != nil {
			return fmt.Errorf("failed to kill process %d on port %d: %w", pid, port, err)
		}
	}
	
	// Wait a moment for the port to be freed
	time.Sleep(100 * time.Millisecond)
	return nil
}

// EnsurePortAvailable ensures a port is available, optionally killing processes using it.
// If killProcesses is false, it only checks availability.
// If killProcesses is true, it attempts to kill processes using the port.
func EnsurePortAvailable(port int, killProcesses bool) error {
	if IsPortAvailable(port) {
		return nil
	}
	
	if !killProcesses {
		return fmt.Errorf("port %d is not available", port)
	}
	
	if err := KillProcessOnPort(port); err != nil {
		return fmt.Errorf("failed to free port %d: %w", port, err)
	}
	
	// Double-check that the port is now available
	if !IsPortAvailable(port) {
		return fmt.Errorf("port %d is still not available after cleanup", port)
	}
	
	return nil
}

// GetFreePortWithReservation returns a free port and a function to release the reservation.
// The port is kept reserved until the release function is called.
func GetFreePortWithReservation() (int, func(), error) {
	a, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, nil, err
	}

	l, err := net.ListenTCP("tcp", a)
	if err != nil {
		return 0, nil, err
	}
	
	port := l.Addr().(*net.TCPAddr).Port
	release := func() {
		l.Close()
	}
	
	return port, release, nil
}

// GetAvailablePortWithRetry gets an available port with retry logic.
// It tries to get a free port and verifies it's still available before returning.
func GetAvailablePortWithRetry(maxRetries int) (int, error) {
	for i := 0; i < maxRetries; i++ {
		port, release, err := GetFreePortWithReservation()
		if err != nil {
			continue
		}
		
		// Immediately release the reservation and check if port is still available
		release()
		
		// Small delay to reduce race condition window
		time.Sleep(time.Duration(i+1) * 5 * time.Millisecond)
		
		// Verify the port is still available
		if IsPortAvailable(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("failed to get available port after %d retries", maxRetries)
}

// GetReservedPortForServer gets a port with reservation that should be released when server starts.
// This is more robust for server startup scenarios.
func GetReservedPortForServer() (int, func(), error) {
	const maxRetries = 10
	for i := 0; i < maxRetries; i++ {
		port, release, err := GetFreePortWithReservation()
		if err != nil {
			continue
		}
		return port, release, nil
	}
	return 0, nil, fmt.Errorf("failed to get reserved port after %d retries", maxRetries)
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
