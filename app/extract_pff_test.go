package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	apperr "github.com/celestiaorg/celestia-app/v10/app/errors"
	"github.com/celestiaorg/celestia-app/v10/test/util/blobfactory"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestExtractPayForFibre(t *testing.T) {
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	txConfig := enc.TxConfig

	normalTx := decodeTxForPayForFibreTest(t, txConfig, newNormalTx(t, txConfig))
	pffTx := decodeTxForPayForFibreTest(t, txConfig, blobfactory.UnsignedPayForFibreTx(t, txConfig))
	mixedTx := decodeTxForPayForFibreTest(t, txConfig, newMixedPayForFibreTx(t, txConfig))
	multiTx := decodeTxForPayForFibreTest(t, txConfig, newMultiPayForFibreTx(t, txConfig))

	got, err := extractPayForFibre(normalTx)
	require.NoError(t, err)
	require.Nil(t, got)

	got, err = extractPayForFibre(pffTx)
	require.NoError(t, err)
	require.NotNil(t, got)

	_, err = extractPayForFibre(mixedTx)
	require.ErrorIs(t, err, apperr.ErrInvalidPayForFibreTx)

	_, err = extractPayForFibre(multiTx)
	require.ErrorIs(t, err, apperr.ErrInvalidPayForFibreTx)
}

func decodeTxForPayForFibreTest(t *testing.T, txConfig client.TxConfig, txBytes []byte) sdk.Tx {
	t.Helper()
	tx, err := txConfig.TxDecoder()(txBytes)
	require.NoError(t, err)
	return tx
}
