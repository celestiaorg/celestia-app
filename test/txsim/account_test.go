package txsim

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// TestNewAccountManagerSurfacesBalanceError checks that the underlying balance
// query error is returned when no master account can be selected.
func TestNewAccountManagerSurfacesBalanceError(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr := keyring.NewInMemory(encCfg.Codec)
	_, err := kr.NewAccount(testfactory.TestAccName, testfactory.TestAccMnemo, "", "", hd.Secp256k1)
	require.NoError(t, err)

	// nothing listens on this address so every balance query fails
	conn, err := grpc.NewClient("127.0.0.1:1", grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	_, err = NewAccountManager(t.Context(), kr, encCfg, "", conn, time.Second, false, false, 0)
	require.Error(t, err)
	require.ErrorContains(t, err, "no suitable master account found")
	require.ErrorContains(t, err, "error getting balance for")
}
