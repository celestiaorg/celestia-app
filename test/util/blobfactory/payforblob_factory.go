package blobfactory

import (
	"bytes"
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
	"google.golang.org/grpc"
)

var (
	// TestMaxBlobSize is the maximum size of each blob in a blob transaction, for testing purposes
	TestMaxBlobSize = appconsts.ShareSize * 2 * appconsts.DefaultSquareSizeUpperBound
	// TestMaxBlobCount is the maximum number of blobs in a blob transaction, for testing purposes
	TestMaxBlobCount = 5
)

func RandMsgPayForBlobsWithSigner(rand *tmrand.Rand, signer string, size, blobCount int) (*blobtypes.MsgPayForBlobs, []*tmproto.Blob) {
	blobs := make([]*tmproto.Blob, blobCount)
	for i := 0; i < blobCount; i++ {
		blob, err := blobtypes.NewBlob(appns.RandomBlobNamespaceWithPRG(rand), tmrand.Bytes(size), appconsts.ShareVersionZero)
		if err != nil {
			panic(err)
		}
		blobs[i] = blob
	}

	msg, err := blobtypes.NewMsgPayForBlobs(signer, blobs...)
	if err != nil {
		panic(err)
	}
	return msg, blobs
}

func RandBlobsWithNamespace(namespaces []appns.Namespace, sizes []int) []*tmproto.Blob {
	blobs := make([]*tmproto.Blob, len(namespaces))
	for i, ns := range namespaces {
		blob, err := blobtypes.NewBlob(ns, tmrand.Bytes(sizes[i]), appconsts.ShareVersionZero)
		if err != nil {
			panic(err)
		}
		blobs[i] = blob
	}
	return blobs
}

func RandMsgPayForBlobsWithNamespaceAndSigner(signer string, ns appns.Namespace, size int) (*blobtypes.MsgPayForBlobs, *tmproto.Blob) {
	blob, err := blobtypes.NewBlob(ns, tmrand.Bytes(size), appconsts.ShareVersionZero)
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

func RandMsgPayForBlobs(rand *tmrand.Rand, size int) (*blobtypes.MsgPayForBlobs, *tmproto.Blob) {
	blob, err := blobtypes.NewBlob(appns.RandomBlobNamespaceWithPRG(rand), tmrand.Bytes(size), appconsts.ShareVersionZero)
	if err != nil {
		panic(err)
	}
	msg, err := blobtypes.NewMsgPayForBlobs(
		testfactory.TestAccAddr,
		blob,
	)
	if err != nil {
		panic(err)
	}
	return msg, blob
}

func RandBlobTxsRandomlySized(signer *user.Signer, rand *tmrand.Rand, count, maxSize, maxBlobs int) coretypes.Txs {
	addr := signer.Address()
	opts := DefaultTxOpts()
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
		_, blobs := RandMsgPayForBlobsWithSigner(rand, addr.String(), size, blobCount)
		cTx, err := signer.CreatePayForBlob(blobs, opts...)
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
	enc encoding.Config,
	rand *tmrand.Rand,
	kr keyring.Keyring,
	conn *grpc.ClientConn,
	size int,
	blobCount int,
	randSize bool,
	accounts []string,
) []coretypes.Tx {
	if conn == nil {
		panic("no grpc connection provided")
	}
	if size <= 0 {
		panic("size should be positive")
	}
	if blobCount <= 0 {
		panic("blobCount should be strictly positive")
	}

	opts := DefaultTxOpts()
	txs := make([]coretypes.Tx, len(accounts))
	for i := 0; i < len(accounts); i++ {
		addr := testfactory.GetAddress(kr, accounts[i])
		signer, err := user.SetupSigner(context.Background(), kr, conn, addr, enc)
		if err != nil {
			panic(err)
		}

		randomizedSize := size
		if randSize {
			randomizedSize = rand.Intn(size)
			if randomizedSize == 0 {
				randomizedSize = 1
			}
		}
		randomizedBlobCount := blobCount
		if randSize {
			randomizedBlobCount = rand.Intn(blobCount)
			if randomizedBlobCount == 0 {
				randomizedBlobCount = 1
			}
		}

		_, blobs := RandMsgPayForBlobsWithSigner(rand, addr.String(), randomizedSize, randomizedBlobCount)
		cTx, err := signer.CreatePayForBlob(blobs, opts...)
		if err != nil {
			panic(err)
		}
		txs[i] = cTx
	}

	return txs
}

func RandBlobTxs(signer *user.Signer, rand *tmrand.Rand, count, blobsPerTx, size int) coretypes.Txs {
	txs := make([]coretypes.Tx, count)
	for i := 0; i < count; i++ {
		_, blobs := RandMsgPayForBlobsWithSigner(rand, signer.Address().String(), size, blobsPerTx)
		tx, err := signer.CreatePayForBlob(blobs, DefaultTxOpts()...)
		if err != nil {
			panic(err)
		}
		txs[i] = tx
	}

	return txs
}

func ManyRandBlobs(t *testing.T, rand *tmrand.Rand, sizes ...int) []*tmproto.Blob {
	return ManyBlobs(t, rand, appns.RandomBlobNamespaces(rand, len(sizes)), sizes)
}

func Repeat[T any](s T, count int) []T {
	ss := make([]T, count)
	for i := 0; i < count; i++ {
		ss[i] = s
	}
	return ss
}

func ManyBlobs(t *testing.T, rand *tmrand.Rand, namespaces []appns.Namespace, sizes []int) []*tmproto.Blob {
	blobs := make([]*tmproto.Blob, len(namespaces))
	for i, ns := range namespaces {
		blob, err := blobtypes.NewBlob(ns, rand.Bytes(sizes[i]), appconsts.ShareVersionZero)
		require.NoError(t, err)
		blobs[i] = blob
	}
	return blobs
}

func NestedBlobs(t *testing.T, namespaces []appns.Namespace, sizes [][]int) [][]*tmproto.Blob {
	blobs := make([][]*tmproto.Blob, len(sizes))
	counter := 0
	for i, set := range sizes {
		for _, size := range set {
			blob, err := blobtypes.NewBlob(namespaces[counter], tmrand.Bytes(size), appconsts.ShareVersionZero)
			require.NoError(t, err)
			blobs[i] = append(blobs[i], blob)
			counter++
		}
	}
	return blobs
}

func ManyMultiBlobTx(
	t *testing.T,
	enc client.TxConfig,
	kr keyring.Keyring,
	chainid string,
	accounts []string,
	accInfos []AccountInfo,
	blobs [][]*tmproto.Blob,
) [][]byte {
	t.Helper()
	txs := make([][]byte, len(accounts))
	opts := DefaultTxOpts()
	for i, acc := range accounts {
		addr := testfactory.GetAddress(kr, acc)
		signer, err := user.NewSigner(kr, nil, addr, enc, chainid, accInfos[i].AccountNum, accInfos[i].Sequence)
		require.NoError(t, err)
		txs[i], err = signer.CreatePayForBlob(blobs[i], opts...)
		require.NoError(t, err)
	}
	return txs
}

// IndexWrappedTxWithInvalidNamespace returns an index wrapped PFB tx with an
// invalid namespace and a blob associated with that index wrapped PFB tx.
func IndexWrappedTxWithInvalidNamespace(
	t *testing.T,
	rand *tmrand.Rand,
	signer *user.Signer,
	index uint32,
) (coretypes.Tx, tmproto.Blob) {
	t.Helper()
	addr := signer.Address()
	blob := ManyRandBlobs(t, rand, 100)[0]
	msg, err := blobtypes.NewMsgPayForBlobs(addr.String(), blob)
	require.NoError(t, err)
	msg.Namespaces[0] = bytes.Repeat([]byte{1}, 33) // invalid namespace

	rawTx, err := signer.CreateTx([]sdk.Msg{msg}, DefaultTxOpts()...)
	require.NoError(t, err)

	cTx, err := coretypes.MarshalIndexWrapper(rawTx, index)
	require.NoError(t, err)

	return cTx, *blob
}

func RandBlobTxsWithNamespacesAndSigner(
	signer *user.Signer,
	namespaces []appns.Namespace,
	sizes []int,
) []coretypes.Tx {
	addr := signer.Address()
	txs := make([]coretypes.Tx, len(namespaces))
	for i := 0; i < len(namespaces); i++ {
		// TODO: this can be refactored as the signer only needs the blobs and can construct the PFB itself
		_, blob := RandMsgPayForBlobsWithNamespaceAndSigner(addr.String(), namespaces[i], sizes[i])
		cTx, err := signer.CreatePayForBlob([]*tmproto.Blob{blob}, DefaultTxOpts()...)
		if err != nil {
			panic(err)
		}
		txs[i] = cTx
	}

	return txs
}

func ComplexBlobTxWithOtherMsgs(t *testing.T, rand *tmrand.Rand, signer *user.Signer, msgs ...sdk.Msg) coretypes.Tx {
	t.Helper()
	pfb, blobs := RandMsgPayForBlobsWithSigner(rand, signer.Address().String(), 100, 1)
	msgs = append(msgs, pfb)

	rawTx, err := signer.CreateTx(msgs, DefaultTxOpts()...)
	require.NoError(t, err)

	btx, err := coretypes.MarshalBlobTx(rawTx, blobs...)
	require.NoError(t, err)
	return btx
}

func GenerateRandomBlobCount(rand *tmrand.Rand) int {
	v := rand.Intn(TestMaxBlobCount)
	if v == 0 {
		v = 1
	}
	return v
}

func GenerateRandomBlobSize(rand *tmrand.Rand) int {
	v := rand.Intn(TestMaxBlobSize)
	if v == 0 {
		v = 1
	}
	return v
}

// GenerateRandomBlobSizes returns a slice of random non-zero blob sizes.
func GenerateRandomBlobSizes(count int, rand *tmrand.Rand) []int {
	sizes := make([]int, count)
	for i := range sizes {
		sizes[i] = GenerateRandomBlobSize(rand)
	}
	return sizes
}

// RandMultiBlobTxsSameSigner returns a slice of random Blob transactions (consisting of pfbCount number of txs) each with random number of blobs and blob sizes.
func RandMultiBlobTxsSameSigner(t *testing.T, rand *tmrand.Rand, signer *user.Signer, pfbCount int) []coretypes.Tx {
	pfbTxs := make([]coretypes.Tx, pfbCount)
	var err error
	for i := 0; i < pfbCount; i++ {
		blobsPerPfb := GenerateRandomBlobCount(rand)
		blobSizes := GenerateRandomBlobSizes(blobsPerPfb, rand)
		blobs := ManyRandBlobs(t, rand, blobSizes...)
		pfbTxs[i], err = signer.CreatePayForBlob(blobs)
		require.NoError(t, err)
	}
	return pfbTxs
}
