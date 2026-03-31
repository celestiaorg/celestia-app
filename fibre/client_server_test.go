package fibre_test

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/celestiaorg/celestia-app/v8/fibre"
	grpcfibre "github.com/celestiaorg/celestia-app/v8/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v8/fibre/state"
	"github.com/celestiaorg/celestia-app/v8/fibre/validator"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

// TestClientServerUploadDownload validates end-to-end download flow with various blob sizes and configurations.
func TestClientServerUploadDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping TestClientServerUploadDownload in short mode")
	}

	tests := []struct {
		name           string
		numValidators  int
		numClients     int
		blobsPerClient int
		blobSize       int
		duplicate      int // upload same blob multiple times at different heights
	}{
		{
			name:           "MaxBlobSize",
			numValidators:  2,
			numClients:     1,
			blobsPerClient: 1,
			blobSize:       fibre.DefaultBlobConfigV0().MaxDataSize,
			duplicate:      2,
		},
		{
			name:           "MinBlobSize",
			numValidators:  3,
			numClients:     2,
			blobsPerClient: 1,
			blobSize:       1,
			duplicate:      1,
		},
		{
			name:           "ManyClientsSingleServerManyBlobs",
			numValidators:  1,
			numClients:     10,
			blobsPerClient: 5,
			blobSize:       128 * 1024, // 128 KiB
			duplicate:      2,
		},
		{
			name:           "ManyClientsManyServersManyBlobs",
			numValidators:  10,
			numClients:     10,
			blobsPerClient: 5,
			blobSize:       128 * 1024, // 128 KiB
			duplicate:      2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := makeTestEnv(t, tt.numValidators, tt.numClients, nil, nil)
			defer env.Close()

			totalBlobs := tt.numClients * tt.blobsPerClient
			allBlobIDs := make([]fibre.BlobID, totalBlobs)
			allPromiseHashes := make([][][]byte, totalBlobs)
			allData := make([][]byte, totalBlobs)

			// upload blobs
			err := env.ForEachClient(t.Context(), func(ctx context.Context, client *fibre.Client, clientIdx int) error {
				for blobIdx := range tt.blobsPerClient {
					data := make([]byte, tt.blobSize)
					if _, err := cryptorand.Read(data); err != nil {
						return fmt.Errorf("generating random data for blob %d: %w", blobIdx, err)
					}

					blob, err := fibre.NewBlob(data, fibre.DefaultBlobConfigV0())
					if err != nil {
						return fmt.Errorf("creating blob %d: %w", blobIdx, err)
					}

					slotIdx := clientIdx*tt.blobsPerClient + blobIdx
					allPromiseHashes[slotIdx] = make([][]byte, 0, tt.duplicate)

					// upload blob (possibly multiple times at different heights)
					for uploadIdx := range tt.duplicate {
						if tt.duplicate > 1 {
							env.SetHeight(uint64(100 + uploadIdx*100))
						}
						signedPromise, err := client.Upload(ctx, testNamespace, blob)
						if err != nil {
							return fmt.Errorf("uploading blob %d (upload %d): %w", blobIdx, uploadIdx, err)
						}
						// store all promise hashes for this blob
						promiseHash, err := signedPromise.Hash()
						if err != nil {
							return fmt.Errorf("getting promise hash for blob %d: %w", blobIdx, err)
						}
						allPromiseHashes[slotIdx] = append(allPromiseHashes[slotIdx], promiseHash)
					}

					allBlobIDs[slotIdx] = blob.ID()
					allData[slotIdx] = data
				}

				client.Await() // wait for all background uploads to complete
				return nil
			})
			require.NoError(t, err)

			// verify storage: all stores should have valid data and payment promises
			// collect row indices per store for duplicate detection (map[storeIdx]map[commitmentStr][]rowIndex)
			rowIndicesByStore := make([]map[string][]uint32, len(env.stores))
			for i := range rowIndicesByStore {
				rowIndicesByStore[i] = make(map[string][]uint32)
			}
			var rowIndicesMu sync.Mutex

			err = env.ForEachStore(t.Context(), func(ctx context.Context, store *fibre.Store, storeIdx int) error {
				for i, id := range allBlobIDs {
					rows, err := store.Get(ctx, id.Commitment())
					if err != nil {
						return fmt.Errorf("store %d missing rows for blob %s: %w", storeIdx, id.String(), err)
					}

					// verify rows are not empty
					if len(rows.Rows) == 0 {
						return fmt.Errorf("store %d has empty rows for blob %s", storeIdx, id.String())
					}

					// verify RLC root is set
					if rows.GetRoot() == nil || len(rows.GetRoot()) != 32 {
						return fmt.Errorf("store %d has invalid RLC root for blob %s", storeIdx, id.String())
					}

					// collect row indices for duplicate detection
					indices := make([]uint32, len(rows.Rows))
					for j, row := range rows.Rows {
						indices[j] = row.Index
					}
					rowIndicesMu.Lock()
					rowIndicesByStore[storeIdx][id.String()] = indices
					rowIndicesMu.Unlock()

					// verify all payment promises are stored (one per duplicate upload)
					for j, promiseHash := range allPromiseHashes[i] {
						promise, err := store.GetPaymentPromise(ctx, promiseHash)
						if err != nil {
							return fmt.Errorf("store %d missing payment promise %d for hash %x: %w", storeIdx, j, promiseHash, err)
						}

						// verify payment promise commitment matches the BlobID's commitment
						if promise.Commitment != id.Commitment() {
							return fmt.Errorf("store %d payment promise %d commitment mismatch: got %x, expected %x",
								storeIdx, j, promise.Commitment[:], id.Commitment())
						}
					}
				}
				return nil
			})
			require.NoError(t, err)

			// verify no duplicate rows across stores (sequential check after concurrent collection)
			for _, id := range allBlobIDs {
				seen := make(map[uint32]int) // row index -> store index
				for storeIdx, storeRows := range rowIndicesByStore {
					for _, rowIdx := range storeRows[id.String()] {
						if existingStore, exists := seen[rowIdx]; exists {
							t.Fatalf("duplicate row index %d for blob %s: found in store %d and store %d",
								rowIdx, id.String(), existingStore, storeIdx)
						}
						seen[rowIdx] = storeIdx
					}
				}
			}

			// download and validate
			err = env.ForEachClient(t.Context(), func(ctx context.Context, client *fibre.Client, clientIdx int) error {
				for blobIdx := range tt.blobsPerClient {
					slotIdx := clientIdx*tt.blobsPerClient + blobIdx
					id := allBlobIDs[slotIdx]
					originalData := allData[slotIdx]

					blob, err := client.Download(ctx, id)
					if err != nil {
						return fmt.Errorf("downloading blob %s: %w", id.String(), err)
					}
					if !bytes.Equal(blob.Data(), originalData) {
						return fmt.Errorf("data mismatch for %s: downloaded %d bytes, expected %d bytes",
							id.String(), len(blob.Data()), len(originalData))
					}
					if !blob.ID().Equals(id) {
						return fmt.Errorf("blob ID mismatch: got %s, expected %s",
							blob.ID().String(), id.String())
					}
				}
				return nil
			})
			require.NoError(t, err)
		})
	}
}

// testEnv holds the test environment with servers, clients, and validator set
type testEnv struct {
	valSetGetter *shufflingValidatorSetGetter
	servers      []*fibre.Server
	clients      []*fibre.Client
	stores       []*fibre.Store
}

func (e *testEnv) Close() {
	for _, srv := range e.servers {
		_ = srv.Stop(context.Background())
	}
	for _, client := range e.clients {
		_ = client.Stop(context.Background())
	}
}

// SetHeight changes the current height of the validator set getter.
// Different heights produce deterministically shuffled validator orderings.
func (e *testEnv) SetHeight(height uint64) {
	e.valSetGetter.SetHeight(height)
}

// ForEachClient runs the given function for each client concurrently and waits for completion.
// Returns the first error encountered, if any.
func (e *testEnv) ForEachClient(ctx context.Context, fn func(context.Context, *fibre.Client, int) error) error {
	g, ctx := errgroup.WithContext(ctx)

	for i, client := range e.clients {
		g.Go(func() error {
			return fn(ctx, client, i)
		})
	}

	return g.Wait()
}

// ForEachStore runs the given function for each store concurrently and waits for completion.
// Returns the first error encountered, if any.
func (e *testEnv) ForEachStore(ctx context.Context, fn func(context.Context, *fibre.Store, int) error) error {
	g, ctx := errgroup.WithContext(ctx)

	for i, store := range e.stores {
		g.Go(func() error {
			return fn(ctx, store, i)
		})
	}

	return g.Wait()
}

// makeTestEnv creates a complete test environment with validators, servers, and clients.
func makeTestEnv(
	t *testing.T,
	numValidators int,
	numClients int,
	modifyClientConfig func(*fibre.ClientConfig),
	modifyServerConfig func(*fibre.ServerConfig),
) *testEnv {
	t.Helper()

	validators, privKeys := makeTestValidators(t, numValidators)

	valSetGetter := newShufflingValidatorSetGetter(validators, 100)
	testParams := fibre.DefaultProtocolParams
	testParams.MaxValidatorCount = numValidators

	servers, stores, addresses := makeTestServers(t, validators, privKeys, testParams, valSetGetter, modifyServerConfig)
	clients := make([]*fibre.Client, numClients)
	for i := range numClients {
		clientCfg := fibre.NewClientConfigFromParams(testParams)

		// create logger with unique client identifier
		clientCfg.Log = slog.Default().With("client_idx", i)

		// apply optional client config modifications
		if modifyClientConfig != nil {
			modifyClientConfig(&clientCfg)
		}
		clientCfg.NewClientFn = grpcfibre.DefaultNewClientFn(&testHostRegistry{addresses: addresses}, clientCfg.MaxMessageSize)
		clientCfg.StateClientFn = func() (state.Client, error) {
			return &mockStateClient{SetGetter: valSetGetter, chainID: "celestia"}, nil
		}

		client, err := fibre.NewClient(makeTestKeyring(t), clientCfg)
		require.NoError(t, err)
		require.NoError(t, client.Start(t.Context()))
		clients[i] = client
	}

	return &testEnv{
		valSetGetter: valSetGetter,
		servers:      servers,
		clients:      clients,
		stores:       stores,
	}
}

// makeTestServers creates and starts fibre servers for each validator.
func makeTestServers(
	t *testing.T,
	validators []*core.Validator,
	privKeys []cmted25519.PrivKey,
	params fibre.ProtocolParams,
	valSetGetter validator.SetGetter,
	modifyServerConfig func(*fibre.ServerConfig),
) ([]*fibre.Server, []*fibre.Store, map[string]string) {
	t.Helper()

	servers := make([]*fibre.Server, len(validators))
	stores := make([]*fibre.Store, len(validators))
	addresses := make(map[string]string)

	for i, val := range validators {
		privVal := newTestPrivValidator(privKeys[i])

		serverCfg := fibre.NewServerConfigFromParams(params)
		serverCfg.ServerListenAddress = "127.0.0.1:0"
		serverCfg.StateClientFn = func() (state.Client, error) {
			return &mockStateClient{
				chainID:   "celestia",
				SetGetter: valSetGetter,
			}, nil
		}
		serverCfg.SignerFn = func(_ string) (core.PrivValidator, error) {
			return privVal, nil
		}

		// create logger with unique server identifier
		serverCfg.Log = slog.Default().With(
			"server_idx", i,
			"validator_addr", val.Address.String(),
		)

		// apply optional server config modifications
		if modifyServerConfig != nil {
			modifyServerConfig(&serverCfg)
		}

		serverCfg.StoreFn = func(scfg fibre.StoreConfig) (*fibre.Store, error) {
			return fibre.NewMemoryStore(scfg), nil
		}
		srv, err := fibre.NewServer(serverCfg)
		require.NoError(t, err)

		require.NoError(t, srv.Start(t.Context()))

		servers[i] = srv
		stores[i] = srv.Store()
		addresses[val.Address.String()] = srv.ListenAddress()
	}

	return servers, stores, addresses
}

// testHostRegistry implements validator.HostRegistry for testing
type testHostRegistry struct {
	addresses map[string]string
}

func (r *testHostRegistry) GetHost(ctx context.Context, val *core.Validator) (validator.Host, error) {
	addr, ok := r.addresses[val.Address.String()]
	if !ok {
		return "", fmt.Errorf("no address for validator %s", val.Address.String())
	}
	return validator.Host(addr), nil
}

// shufflingValidatorSetGetter returns deterministically shuffled validator sets based on height.
// Each height produces a different but deterministic ordering using height as the random seed.
type shufflingValidatorSetGetter struct {
	validators []*core.Validator
	height     atomic.Uint64
}

func newShufflingValidatorSetGetter(validators []*core.Validator, initialHeight uint64) *shufflingValidatorSetGetter {
	g := &shufflingValidatorSetGetter{validators: validators}
	g.height.Store(initialHeight)
	return g
}

func (g *shufflingValidatorSetGetter) Head(ctx context.Context) (validator.Set, error) {
	return g.setForHeight(g.height.Load()), nil
}

func (g *shufflingValidatorSetGetter) GetByHeight(ctx context.Context, height uint64) (validator.Set, error) {
	return g.setForHeight(height), nil
}

func (g *shufflingValidatorSetGetter) SetHeight(height uint64) {
	g.height.Store(height)
}

func (g *shufflingValidatorSetGetter) setForHeight(height uint64) validator.Set {
	shuffled := make([]*core.Validator, len(g.validators))
	copy(shuffled, g.validators)

	r := rand.New(rand.NewSource(int64(height)))
	r.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})

	return validator.Set{
		ValidatorSet: core.NewValidatorSet(shuffled),
		Height:       height,
	}
}
