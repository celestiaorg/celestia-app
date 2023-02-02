package blobfactory

import (
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/testutil/namespace"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/rand"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
	"google.golang.org/grpc"
)

var defaultSigner = testfactory.RandomAddress().String()

func RandMsgPayForBlobsWithSigner(singer string, size, blobCount int) (*blobtypes.MsgPayForBlobs, []*tmproto.Blob) {
	blobs := make([]*tmproto.Blob, blobCount)
	for i := 0; i < blobCount; i++ {
		blob, err := types.NewBlob(namespace.RandomBlobNamespace(), tmrand.Bytes(size))
		if err != nil {
			panic(err)
		}
		blobs[i] = blob
	}

	msg, err := blobtypes.NewMsgPayForBlobs(
		singer,
		blobs...,
	)
	if err != nil {
		panic(err)
	}
	return msg, blobs
}

func RandBlobsWithNamespace(namespaces [][]byte, sizes []int) []*tmproto.Blob {
	blobs := make([]*tmproto.Blob, len(namespaces))
	for i, ns := range namespaces {
		blob, err := types.NewBlob(ns, tmrand.Bytes(sizes[i]))
		if err != nil {
			panic(err)
		}
		blobs[i] = blob
	}
	return blobs
}

func RandMsgPayForBlobsWithNamespaceAndSigner(signer string, nid []byte, size int) (*blobtypes.MsgPayForBlobs, *tmproto.Blob) {
	blob, err := types.NewBlob(nid, tmrand.Bytes(size))
	if err != nil {
		panic(err)
	}
	msg, err := blobtypes.NewMsgPayForBlobs(
		signer,
		blob,
	)
	if err != nil {
		panic(err)
	}
	return msg, blob
}

func RandMsgPayForBlobs(size int) (*blobtypes.MsgPayForBlobs, *tmproto.Blob) {
	blob, err := types.NewBlob(namespace.RandomBlobNamespace(), tmrand.Bytes(size))
	if err != nil {
		panic(err)
	}
	msg, err := blobtypes.NewMsgPayForBlobs(
		defaultSigner,
		blob,
	)
	if err != nil {
		panic(err)
	}
	return msg, blob
}

func RandBlobTxsRandomlySized(enc sdk.TxEncoder, count, maxSize, maxBlobs int) []coretypes.Tx {
	const acc = "signer"
	kr := testfactory.GenerateKeyring(acc)
	signer := blobtypes.NewKeyringSigner(kr, acc, "chainid")
	addr, err := signer.GetSignerInfo().GetAddress()
	if err != nil {
		panic(err)
	}

	coin := sdk.Coin{
		Denom:  bondDenom,
		Amount: sdk.NewInt(10),
	}

	opts := []blobtypes.TxBuilderOption{
		blobtypes.SetFeeAmount(sdk.NewCoins(coin)),
		blobtypes.SetGasLimit(100000000),
	}

	txs := make([]coretypes.Tx, count)
	for i := 0; i < count; i++ {
		// pick a random non-zero size of max maxSize
		size := tmrand.Intn(maxSize)
		if size == 0 {
			size = 1
		}
		blobCount := tmrand.Intn(maxBlobs)
		if blobCount == 0 {
			blobCount = 1
		}
		msg, blobs := RandMsgPayForBlobsWithSigner(addr.String(), size, blobCount)
		builder := signer.NewTxBuilder(opts...)
		stx, err := signer.BuildSignedTx(builder, msg)
		if err != nil {
			panic(err)
		}
		rawTx, err := enc(stx)
		if err != nil {
			panic(err)
		}
		cTx, err := coretypes.MarshalBlobTx(rawTx, blobs...)
		if err != nil {
			panic(err)
		}
		txs[i] = cTx
	}

	return txs
}

// RandBlobTxsWithAccounts will create random blob transactions using the
// provided configuration. If no grpc connection is provided, then it will not
// update the account info. One blob transaction is generated per account
// provided.
func RandBlobTxsWithAccounts(
	enc sdk.TxEncoder,
	kr keyring.Keyring,
	conn *grpc.ClientConn,
	size int,
	blobCount int,
	randSize bool,
	chainid string,
	accounts []string,
) []coretypes.Tx {
	coin := sdk.Coin{
		Denom:  bondDenom,
		Amount: sdk.NewInt(10),
	}

	opts := []blobtypes.TxBuilderOption{
		blobtypes.SetFeeAmount(sdk.NewCoins(coin)),
		blobtypes.SetGasLimit(100000000000000),
	}

	txs := make([]coretypes.Tx, len(accounts))
	for i := 0; i < len(accounts); i++ {
		signer := blobtypes.NewKeyringSigner(kr, accounts[i], chainid)
		if conn != nil {
			err := signer.QueryAccountNumber(context.Background(), conn)
			if err != nil {
				panic(err)
			}
		}

		addr, err := signer.GetSignerInfo().GetAddress()
		if err != nil {
			panic(err)
		}

		if size <= 0 {
			panic("size should be positive")
		}
		randomizedSize := size
		if randSize {
			randomizedSize = rand.Intn(size)
			if randomizedSize == 0 {
				randomizedSize = 1
			}
		}
		if blobCount <= 0 {
			panic("blobCount should be strictly positive")
		}
		randomizedBlobCount := blobCount
		if randSize {
			randomizedBlobCount = rand.Intn(blobCount)
			if randomizedBlobCount == 0 {
				randomizedBlobCount = 1
			}
		}
		msg, blobs := RandMsgPayForBlobsWithSigner(addr.String(), randomizedSize, randomizedBlobCount)
		builder := signer.NewTxBuilder(opts...)
		stx, err := signer.BuildSignedTx(builder, msg)
		if err != nil {
			panic(err)
		}
		rawTx, err := enc(stx)
		if err != nil {
			panic(err)
		}
		cTx, err := coretypes.MarshalBlobTx(rawTx, blobs...)
		if err != nil {
			panic(err)
		}
		txs[i] = cTx
	}

	return txs
}

func RandBlobTxs(enc sdk.TxEncoder, count, size int) []coretypes.Tx {
	const acc = "signer"
	kr := testfactory.GenerateKeyring(acc)
	signer := blobtypes.NewKeyringSigner(kr, acc, "chainid")
	addr, err := signer.GetSignerInfo().GetAddress()
	if err != nil {
		panic(err)
	}

	coin := sdk.Coin{
		Denom:  bondDenom,
		Amount: sdk.NewInt(10),
	}

	opts := []blobtypes.TxBuilderOption{
		blobtypes.SetFeeAmount(sdk.NewCoins(coin)),
		blobtypes.SetGasLimit(10000000),
	}

	txs := make([]coretypes.Tx, count)
	for i := 0; i < count; i++ {
		msg, blobs := RandMsgPayForBlobsWithSigner(addr.String(), size, 1)
		builder := signer.NewTxBuilder(opts...)
		stx, err := signer.BuildSignedTx(builder, msg)
		if err != nil {
			panic(err)
		}
		rawTx, err := enc(stx)
		if err != nil {
			panic(err)
		}
		cTx, err := coretypes.MarshalBlobTx(rawTx, blobs...)
		if err != nil {
			panic(err)
		}
		txs[i] = cTx
	}

	return txs
}

func RandBlobTxsWithNamespaces(enc sdk.TxEncoder, nIds [][]byte, sizes []int) []coretypes.Tx {
	const acc = "signer"
	kr := testfactory.GenerateKeyring(acc)
	signer := blobtypes.NewKeyringSigner(kr, acc, "chainid")
	return RandBlobTxsWithNamespacesAndSigner(enc, signer, nIds, sizes)
}

// ManyMultiBlobTxSameSigner generates and returns many blob transactions with
// the possibility to add more than one blob. The sequence and account number
// are manually set, and the sequence is manually incremented when doing so.
func ManyMultiBlobTxSameSigner(
	t *testing.T,
	enc sdk.TxEncoder,
	signer *blobtypes.KeyringSigner,
	blobSizes [][]int,
	sequence, accountNum uint64,
) []coretypes.Tx {
	txs := make([]coretypes.Tx, len(blobSizes))
	for i := 0; i < len(blobSizes); i++ {
		txs[i] = MultiBlobTx(t, enc, signer, sequence+uint64(i), accountNum, ManyRandBlobs(t, blobSizes[i]...)...)
	}
	return txs
}

func ManyRandBlobsIdenticallySized(t *testing.T, count, size int) []*tmproto.Blob {
	sizes := make([]int, count)
	for i := 0; i < count; i++ {
		sizes[i] = size
	}
	return ManyRandBlobs(t, sizes...)
}

func ManyRandBlobs(t *testing.T, sizes ...int) []*tmproto.Blob {
	return ManyBlobs(t, namespace.RandomBlobNamespaces(len(sizes)), sizes)
}

func Repeat[T any](s T, count int) []T {
	ss := make([]T, count)
	for i := 0; i < count; i++ {
		ss[i] = s
	}
	return ss
}

func ManyBlobs(t *testing.T, namespaces [][]byte, sizes []int) []*tmproto.Blob {
	blobs := make([]*tmproto.Blob, len(namespaces))
	for i, ns := range namespaces {
		blob, err := blobtypes.NewBlob(ns, tmrand.Bytes(sizes[i]))
		require.NoError(t, err)
		blobs[i] = blob
	}
	return blobs
}

func NestedBlobs(t *testing.T, nids [][]byte, sizes [][]int) [][]*tmproto.Blob {
	blobs := make([][]*tmproto.Blob, len(sizes))
	counter := 0
	for i, set := range sizes {
		for _, size := range set {
			blob, err := blobtypes.NewBlob(nids[counter], tmrand.Bytes(size))
			require.NoError(t, err)
			blobs[i] = append(blobs[i], blob)
			counter++
		}
	}
	return blobs
}

func ManyMultiBlobTx(
	t *testing.T,
	enc sdk.TxEncoder,
	kr keyring.Keyring,
	chainid string,
	accounts []string,
	accInfos []AccountInfo,
	blobs [][]*tmproto.Blob,
) [][]byte {
	txs := make([][]byte, len(accounts))
	for i, acc := range accounts {
		signer := blobtypes.NewKeyringSigner(kr, acc, chainid)
		txs[i] = MultiBlobTx(t, enc, signer, accInfos[i].Sequence, accInfos[i].AccountNum, blobs[i]...)
	}
	return txs
}

func MultiBlobTx(
	t *testing.T,
	enc sdk.TxEncoder,
	signer *blobtypes.KeyringSigner,
	sequence, accountNum uint64,
	blobs ...*tmproto.Blob,
) coretypes.Tx {
	addr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)

	coin := sdk.Coin{
		Denom:  bondDenom,
		Amount: sdk.NewInt(10),
	}
	opts := []blobtypes.TxBuilderOption{
		blobtypes.SetFeeAmount(sdk.NewCoins(coin)),
		blobtypes.SetGasLimit(10000000),
	}
	msg, err := blobtypes.NewMsgPayForBlobs(addr.String(), blobs...)
	require.NoError(t, err)

	signer.SetAccountNumber(accountNum)
	signer.SetSequence(sequence)

	builder := signer.NewTxBuilder(opts...)
	stx, err := signer.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	rawTx, err := enc(stx)
	require.NoError(t, err)

	cTx, err := coretypes.MarshalBlobTx(rawTx, blobs...)
	require.NoError(t, err)

	return cTx
}

func RandBlobTxsWithNamespacesAndSigner(
	enc sdk.TxEncoder,
	signer *blobtypes.KeyringSigner,
	nIds [][]byte,
	sizes []int,
) []coretypes.Tx {
	addr, err := signer.GetSignerInfo().GetAddress()
	if err != nil {
		panic(err)
	}

	coin := sdk.Coin{
		Denom:  bondDenom,
		Amount: sdk.NewInt(10),
	}

	opts := []blobtypes.TxBuilderOption{
		blobtypes.SetFeeAmount(sdk.NewCoins(coin)),
		blobtypes.SetGasLimit(10000000),
	}

	txs := make([]coretypes.Tx, len(nIds))
	for i := 0; i < len(nIds); i++ {
		msg, blob := RandMsgPayForBlobsWithNamespaceAndSigner(addr.String(), nIds[i], sizes[i])
		builder := signer.NewTxBuilder(opts...)
		stx, err := signer.BuildSignedTx(builder, msg)
		if err != nil {
			panic(err)
		}
		rawTx, err := enc(stx)
		if err != nil {
			panic(err)
		}
		cTx, err := coretypes.MarshalBlobTx(rawTx, blob)
		if err != nil {
			panic(err)
		}
		txs[i] = cTx
	}

	return txs
}

func ComplexBlobTxWithOtherMsgs(t *testing.T, kr keyring.Keyring, enc sdk.TxEncoder, chainid, account string, msgs ...sdk.Msg) coretypes.Tx {
	signer := blobtypes.NewKeyringSigner(kr, account, chainid)
	signerAddr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)

	pfb, blobs := RandMsgPayForBlobsWithSigner(signerAddr.String(), 100, 1)

	opts := []blobtypes.TxBuilderOption{
		blobtypes.SetFeeAmount(sdk.NewCoins(sdk.NewCoin(bondDenom, sdk.NewInt(10)))),
		blobtypes.SetGasLimit(100000000000000),
	}

	msgs = append(msgs, pfb)

	sdkTx, err := signer.BuildSignedTx(signer.NewTxBuilder(opts...), msgs...)
	require.NoError(t, err)
	rawTx, err := enc(sdkTx)
	require.NoError(t, err)

	btx, err := coretypes.MarshalBlobTx(rawTx, blobs...)
	require.NoError(t, err)
	return btx
}
