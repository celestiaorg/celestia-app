package fibre_test

import (
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/stretchr/testify/require"
)

func TestPut_NoKeyring(t *testing.T) {
	client := makeTestDownloadClient(t, 1, nil)
	t.Cleanup(func() { require.NoError(t, client.Stop(context.Background())) })

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"EmptyData", nil},
		{"ValidData", []byte("blob data")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Reject missing credentials before encoding or using the transaction client.
			result, err := fibre.Put(t.Context(), client, nil, testNamespace, tc.data)
			require.ErrorIs(t, err, fibre.ErrNoKeyring)
			require.Empty(t, result)
		})
	}
}
