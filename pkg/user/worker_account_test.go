package user_test

import (
	"context"
	"net"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/pkg/user"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

// testSetup holds common test infrastructure
type testSetup struct {
	encCfg encoding.Config
	kr     keyring.Keyring
	signer *user.Signer
	conn   *grpc.ClientConn
}

// newTestSetup creates common test infrastructure with the specified accounts
func newTestSetup(t *testing.T, accountNames []string) *testSetup {
	t.Helper()

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr := keyring.NewInMemory(encCfg.Codec)

	accounts := make([]*user.Account, len(accountNames))
	for i, name := range accountNames {
		path := hd.CreateHDPath(sdktypes.CoinType, 0, uint32(i)).String()
		_, _, err := kr.NewMnemonic(name, keyring.English, path, keyring.DefaultBIP39Passphrase, hd.Secp256k1)
		require.NoError(t, err)
		accounts[i] = user.NewAccount(name, uint64(i+1), 0)
	}

	signer, err := user.NewSigner(kr, encCfg.TxConfig, "test-chain", accounts...)
	require.NoError(t, err)

	// Create mock GRPC connection
	listener := bufconn.Listen(1024 * 1024)
	conn, err := grpc.NewClient("", grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
		return listener.Dial()
	}), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	return &testSetup{
		encCfg: encCfg,
		kr:     kr,
		signer: signer,
		conn:   conn,
	}
}

// Close cleans up test resources
func (ts *testSetup) Close() {
	ts.conn.Close()
}

// newTxClientWithWorkers creates a TxClient with parallel workers using the specified default account
func (ts *testSetup) newTxClientWithWorkers(t *testing.T, numWorkers int, defaultAccount string) *user.TxClient {
	t.Helper()

	txWorkersOpt := user.WithTxWorkers(numWorkers)
	client, err := user.NewTxClient(ts.encCfg.Codec, ts.signer, ts.conn, ts.encCfg.InterfaceRegistry,
		user.WithDefaultAccount(defaultAccount), txWorkersOpt)
	require.NoError(t, err)

	return client
}

func TestWorkerZeroAlwaysUsesMainAccount(t *testing.T) {
	t.Parallel()

	customAccountName := "my-custom-main-account"
	setup := newTestSetup(t, []string{customAccountName})
	defer setup.Close()

	// Test with different numbers of workers - worker 0 should always use main account
	testCases := []struct {
		name       string
		numWorkers int
	}{
		{"Single Worker", 1},
		{"Two Workers", 2},
		{"Five Workers", 5},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := setup.newTxClientWithWorkers(t, tc.numWorkers, customAccountName)

			// Verify client uses our custom account as default
			require.Equal(t, customAccountName, client.DefaultAccountName())

			// Verify tx queue exists
			require.True(t, client.TxQueueWorkerCount() > 0)

			// Get all workers
			require.Equal(t, tc.numWorkers, client.TxQueueWorkerCount())

			// CRITICAL TEST: Worker 0 must ALWAYS use the main account passed to TxClient
			worker0Name := client.TxQueueWorkerAccountName(0)
			require.Equal(t, customAccountName, worker0Name,
				"Worker 0 must always use the main account passed to TxClient")
			require.Equal(t, client.DefaultAccountName(), worker0Name,
				"Worker 0 must use the same account as client.DefaultAccountName()")

			// For multiple workers, verify additional workers use generated names
			if tc.numWorkers > 1 {
				for i := 1; i < tc.numWorkers; i++ {
					expectedName := "parallel-worker-" + string(rune('0'+i))
					require.Equal(t, expectedName, client.TxQueueWorkerAccountName(i),
						"Additional workers should use generated names")
					require.NotEqual(t, customAccountName, client.TxQueueWorkerAccountName(i),
						"Additional workers should NOT use the main account")
				}
			}
		})
	}
}

func TestWorkerZeroWithDifferentDefaultAccounts(t *testing.T) {
	t.Parallel()

	accountNames := []string{"account-alpha", "account-beta", "account-gamma"}
	setup := newTestSetup(t, accountNames)
	defer setup.Close()

	// Test that worker 0 uses whichever account is set as default
	for _, defaultAccount := range accountNames {
		t.Run("DefaultAccount_"+defaultAccount, func(t *testing.T) {
			client := setup.newTxClientWithWorkers(t, 3, defaultAccount)

			// Verify the client uses the specified default account
			require.Equal(t, defaultAccount, client.DefaultAccountName())

			// Get workers
			require.Equal(t, 3, client.TxQueueWorkerCount())

			// CRITICAL: Worker 0 must use whatever account is configured as default
			require.Equal(t, defaultAccount, client.TxQueueWorkerAccountName(0),
				"Worker 0 must always use the configured default account (%s)", defaultAccount)

			// Other workers use generated names
			require.Equal(t, "parallel-worker-1", client.TxQueueWorkerAccountName(1))
			require.Equal(t, "parallel-worker-2", client.TxQueueWorkerAccountName(2))
		})
	}
}
