package fibre_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v6/fibre"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/stretchr/testify/require"
)

// TestClientServerUpload validates end-to-end over GRPC the upload flow with various blob sizes and configurations.
func TestClientServerUpload(t *testing.T) {
	tests := []struct {
		name           string
		numValidators  int
		numClients     int
		blobsPerClient int
		blobSize       int
	}{
		{
			name:           "MaxBlobSize",
			numValidators:  1,
			numClients:     2,
			blobsPerClient: 1,
			blobSize:       fibre.DefaultBlobConfigV0().MaxBlobSize,
		},
		{
			name:           "MinBlobSize",
			numValidators:  3,
			numClients:     2,
			blobsPerClient: 1,
			blobSize:       1,
		},
		{
			name:           "ManyClientsManyServersManyBlobs",
			numValidators:  10,
			numClients:     10,
			blobsPerClient: 5,
			blobSize:       128 * 1024, // 128 KiB
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := makeTestEnv(t, tt.numValidators, tt.numClients, func(cfg *fibre.ClientConfig) {
				// ensure all validators receive rows by setting target signatures to 100%
				cfg.UploadTargetSignaturesCount.Numerator = 1
				cfg.UploadTargetSignaturesCount.Denominator = 1
			}, nil)
			defer env.Close()

			totalBlobs := tt.numClients * tt.blobsPerClient
			allCommitments := make([]fibre.Commitment, totalBlobs)
			allPromiseHashes := make([][]byte, totalBlobs)

			// for each client start uploading blobs
			err := env.ForEachClient(t.Context(), func(ctx context.Context, client *fibre.Client, clientIdx int) error {
				for blobIdx := range tt.blobsPerClient {
					data := make([]byte, tt.blobSize)
					if _, err := rand.Read(data); err != nil {
						return fmt.Errorf("generating random data for blob %d: %w", blobIdx, err)
					}

					blob, err := fibre.NewBlob(data, client.Config().BlobConfig)
					if err != nil {
						return fmt.Errorf("creating blob %d: %w", blobIdx, err)
					}

					ns := share.MustNewV0Namespace([]byte{byte(clientIdx >> 8), byte(clientIdx)})
					promise, err := client.Upload(ctx, ns, blob)
					if err != nil {
						return fmt.Errorf("uploading blob %d: %w", blobIdx, err)
					}
					promiseHash, err := promise.Hash()
					if err != nil {
						return fmt.Errorf("hashing payment promise %d: %w", blobIdx, err)
					}

					slotIdx := clientIdx*tt.blobsPerClient + blobIdx
					allCommitments[slotIdx] = blob.Commitment()
					allPromiseHashes[slotIdx] = promiseHash
				}

				return nil
			})
			require.NoError(t, err)

			// verify storage: all validators should have rows and payment promises for all uploads
			err = env.ForEachStore(t.Context(), func(ctx context.Context, store *fibre.Store, storeIdx int) error {
				for i, commitment := range allCommitments {
					rows, err := store.Get(ctx, commitment)
					if err != nil {
						return fmt.Errorf("store %d missing rows for commitment %s: %w", storeIdx, commitment.String(), err)
					}

					// verify rows are not empty
					if len(rows.Rows) == 0 {
						return fmt.Errorf("store %d has empty rows for commitment %s", storeIdx, commitment.String())
					}

					// verify RLC root is set
					if rows.GetRoot() == nil || len(rows.GetRoot()) != 32 {
						return fmt.Errorf("store %d has invalid RLC root for commitment %s", storeIdx, commitment.String())
					}

					// verify payment promise is stored
					promise, err := store.GetPaymentPromise(ctx, allPromiseHashes[i])
					if err != nil {
						return fmt.Errorf("store %d missing payment promise for hash %x: %w", storeIdx, allPromiseHashes[i], err)
					}

					// verify payment promise commitment matches
					if !promise.Commitment.Equals(commitment) {
						return fmt.Errorf("store %d payment promise commitment mismatch: got %s, expected %s",
							storeIdx, promise.Commitment.String(), commitment.String())
					}
				}

				return nil
			})
			require.NoError(t, err)
		})
	}
}
