package types

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestNewBlob(t *testing.T) {
	rawBlob := []byte{1}
	validBlob, err := NewBlob([]byte{1, 2, 3, 4, 5, 6, 7, 8}, rawBlob)
	require.NoError(t, err)
	require.Equal(t, validBlob.Data, rawBlob)

	_, err = NewBlob(appconsts.TxNamespaceID, rawBlob)
	require.Error(t, err)

	_, err = NewBlob([]byte{1, 2, 3, 4, 5, 6, 7, 8}, []byte{})
	require.Error(t, err)
}

func setupTests(t *testing.T) (string, sdk.Address, *KeyringSigner, encoding.Config) {
	acc := "test account"
	signer := GenerateKeyringSigner(t, acc)
	encCfg := makeBlobEncodingConfig()
	addr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)
	return acc, addr, signer, encCfg
}
