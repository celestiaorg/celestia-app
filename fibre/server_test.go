package fibre_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v8/fibre"
	"github.com/celestiaorg/celestia-app/v8/fibre/state"
	"github.com/celestiaorg/celestia-app/v8/fibre/validator"
	"github.com/cometbft/cometbft/crypto"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// makeTestServer creates a server with all necessary test infrastructure.
func makeTestServer(t *testing.T) (*fibre.Server, validator.Set, *core.Validator) {
	t.Helper()

	// create validator set (use enough validators for good distribution)
	validators, privKeys := makeTestValidators(t, 100)
	valSet := validator.Set{
		ValidatorSet: core.NewValidatorSet(validators),
		Height:       100,
	}

	// use first validator as the server's identity
	privVal := newTestPrivValidator(privKeys[0])

	// find the server validator in the ValidatorSet by matching the address
	// Note: core.NewValidatorSet may reorder validators, so we can't assume validators[0] == privKeys[0]
	serverPubKey, err := privVal.GetPubKey()
	require.NoError(t, err)
	serverAddress := serverPubKey.Address()

	serverValidator, found := valSet.GetByAddress(serverAddress)
	require.True(t, found, "server validator not found in validator set")
	require.NotNil(t, serverValidator, "server validator is nil")

	valSetGetter := &mockValidatorSetGetter{set: valSet}

	cfg := fibre.DefaultServerConfig()
	cfg.ServerListenAddress = "127.0.0.1:0"
	cfg.StateClientFn = func() (state.Client, error) {
		return &mockStateClient{
			chainID:   "celestia",
			SetGetter: valSetGetter,
		}, nil
	}
	cfg.SignerFn = func(_ string) (core.PrivValidator, error) {
		return privVal, nil
	}

	cfg.StoreFn = func(scfg fibre.StoreConfig) (*fibre.Store, error) {
		return fibre.NewMemoryStore(scfg), nil
	}
	server, err := fibre.NewServer(cfg)
	require.NoError(t, err)

	require.NoError(t, server.Start(t.Context()))
	t.Cleanup(func() {
		require.NoError(t, server.Stop(context.Background()))
	})

	return server, valSet, serverValidator
}

// mockStateClient implements state.Client for testing.
type mockStateClient struct {
	validator.SetGetter
	validator.HostRegistry
	chainID string
}

func (m *mockStateClient) Start(context.Context) error { return nil }
func (m *mockStateClient) Stop(context.Context) error  { return nil }
func (m *mockStateClient) ChainID() string             { return m.chainID }

func (m *mockStateClient) VerifyPromise(_ context.Context, promise *state.PaymentPromise) (state.VerifiedPromise, error) {
	expirationTime := promise.CreationTimestamp.Add(1 * time.Hour)
	if time.Now().After(expirationTime) || time.Now().Equal(expirationTime) {
		return state.VerifiedPromise{}, fmt.Errorf("payment promise expired: creation_timestamp %v + timeout %v = %v",
			promise.CreationTimestamp, 1*time.Hour, expirationTime)
	}
	return state.VerifiedPromise{ExpiresAt: expirationTime}, nil
}

// testPrivValidator is a simple mock PrivValidator for testing.
type testPrivValidator struct {
	privKey crypto.PrivKey
}

func newTestPrivValidator(privKey crypto.PrivKey) *testPrivValidator {
	return &testPrivValidator{privKey: privKey}
}

func (m *testPrivValidator) GetPubKey() (crypto.PubKey, error) {
	return m.privKey.PubKey(), nil
}

func (m *testPrivValidator) SignRawBytes(chainID, uniqueID string, rawBytes []byte) ([]byte, error) {
	signBytes, err := core.RawBytesMessageSignBytes(chainID, uniqueID, rawBytes)
	if err != nil {
		return nil, err
	}
	return m.privKey.Sign(signBytes)
}

func (m *testPrivValidator) SignVote(chainID string, vote *cmtproto.Vote) error {
	return nil
}

func (m *testPrivValidator) SignProposal(chainID string, proposal *cmtproto.Proposal) error {
	return nil
}

func (m *testPrivValidator) GetAddress() core.Address {
	return m.privKey.PubKey().Address()
}
