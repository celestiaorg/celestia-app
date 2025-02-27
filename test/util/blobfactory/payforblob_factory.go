package blobfactory

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"testing"

	tmrand "cosmossdk.io/math/unsafe"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/go-square/v2/tx"

	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

var (
	// TestMaxBlobSize is the maximum size of each blob in a blob transaction, for testing purposes
	TestMaxBlobSize = share.ShareSize * 2 * appconsts.DefaultSquareSizeUpperBound
	// TestMaxBlobCount is the maximum number of blobs in a blob transaction, for testing purposes
	TestMaxBlobCount = 5
)

func RandMsgPayForBlobsWithSigner(rand *tmrand.Rand, signer string, size, blobCount int) (*blobtypes.MsgPayForBlobs, []*share.Blob) {
	blobs := make([]*share.Blob, blobCount)
	for i := 0; i < blobCount; i++ {
		blob, err := blobtypes.NewV0Blob(testfactory.RandomBlobNamespaceWithPRG(rand), tmrand.Bytes(size))
		if err != nil {
			panic(err)
		}
		blobs[i] = blob
	}

	msg, err := blobtypes.NewMsgPayForBlobs(signer, appconsts.LatestVersion, blobs...)
	if err != nil {
		panic(err)
	}
	return msg, blobs
}

func RandV0BlobsWithNamespace(namespaces []share.Namespace, sizes []int) []*share.Blob {
	blobs := make([]*share.Blob, len(namespaces))
	var err error
	for i, ns := range namespaces {
		blobs[i], err = share.NewV0Blob(ns, tmrand.Bytes(sizes[i]))
		if err != nil {
			panic(err)
		}
	}
	return blobs
}

func RandV1BlobsWithNamespace(namespaces []share.Namespace, sizes []int, signer sdk.AccAddress) []*share.Blob {
	blobs := make([]*share.Blob, len(namespaces))
	var err error
	for i, ns := range namespaces {
		blobs[i], err = share.NewV1Blob(ns, tmrand.Bytes(sizes[i]), signer)
		if err != nil {
			panic(err)
		}
	}
	return blobs
}

func RandMsgPayForBlobsWithNamespaceAndSigner(signer string, ns share.Namespace, size int) (*blobtypes.MsgPayForBlobs, *share.Blob) {
	blob, err := blobtypes.NewV0Blob(ns, tmrand.Bytes(size))
	if err != nil {
		panic(err)
	}
	msg, err := blobtypes.NewMsgPayForBlobs(
		signer,
		appconsts.LatestVersion,
		blob,
	)
	if err != nil {
		panic(err)
	}
	return msg, blob
}

func RandMsgPayForBlobs(rand *tmrand.Rand, size int) (*blobtypes.MsgPayForBlobs, *share.Blob) {
	blob, err := share.NewBlob(testfactory.RandomBlobNamespaceWithPRG(rand), tmrand.Bytes(size), share.ShareVersionZero, nil)
	if err != nil {
		panic(err)
	}
	msg, err := blobtypes.NewMsgPayForBlobs(
		testfactory.TestAccAddr,
		appconsts.LatestVersion,
		blob,
	)
	if err != nil {
		panic(err)
	}
	return msg, blob
}

func RandBlobTxsRandomlySized(signer *user.Signer, tmrand *tmrand.Rand, count, maxSize, maxBlobs int) coretypes.Txs {
	opts := DefaultTxOpts()
	txs := make([]coretypes.Tx, count)
	for i := 0; i < count; i++ {
		// pick a random non-zero size of max maxSize
		size := rand.Intn(maxSize)
		if size == 0 {
			size = 1
		}
		blobCount := rand.Intn(maxBlobs)
		if blobCount == 0 {
			blobCount = 1
		}
		_, blobs := RandMsgPayForBlobsWithSigner(tmrand, testfactory.TestAccName, size, blobCount)
		cTx, _, err := signer.CreatePayForBlobs(testfactory.TestAccName, blobs, opts...)
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
	tmrand *tmrand.Rand,
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
		client, err := user.SetupTxClient(context.Background(), kr, conn, enc)
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

		_, blobs := RandMsgPayForBlobsWithSigner(tmrand, addr.String(), randomizedSize, randomizedBlobCount)
		cTx, _, err := client.Signer().CreatePayForBlobs(accounts[i], blobs, opts...)
		if err != nil {
			panic(err)
		}
		txs[i] = cTx
	}

	return txs
}

func RandBlobTxs(signer *user.Signer, rand *tmrand.Rand, count, blobsPerTx, size int) coretypes.Txs {
	addr := signer.Account(testfactory.TestAccName).Address()
	txs := make([]coretypes.Tx, count)
	for i := 0; i < count; i++ {
		fmt.Println(addr.String())
		_, blobs := RandMsgPayForBlobsWithSigner(rand, addr.String(), size, blobsPerTx)
		tx, _, err := signer.CreatePayForBlobs(testfactory.TestAccName, blobs, DefaultTxOpts()...)
		if err != nil {
			panic(err)
		}
		txs[i] = tx
	}

	return txs
}

func ManyRandBlobs(rand *tmrand.Rand, sizes ...int) []*share.Blob {
	return ManyBlobs(rand, testfactory.RandomBlobNamespaces(rand, len(sizes)), sizes)
}

func Repeat[T any](s T, count int) []T {
	ss := make([]T, count)
	for i := 0; i < count; i++ {
		ss[i] = s
	}
	return ss
}

func ManyBlobs(rand *tmrand.Rand, namespaces []share.Namespace, sizes []int) []*share.Blob {
	blobs := make([]*share.Blob, len(namespaces))
	for i, ns := range namespaces {
		blob, err := share.NewBlob(ns, rand.Bytes(sizes[i]), share.ShareVersionZero, nil)
		if err != nil {
			panic(err)
		}
		blobs[i] = blob
	}
	return blobs
}

func NestedBlobs(t *testing.T, namespaces []share.Namespace, sizes [][]int) [][]*share.Blob {
	blobs := make([][]*share.Blob, len(sizes))
	counter := 0
	for i, set := range sizes {
		for _, size := range set {
			blob, err := blobtypes.NewV0Blob(namespaces[counter], tmrand.Bytes(size))
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
	blobs [][]*share.Blob,
) [][]byte {
	t.Helper()
	txs := make([][]byte, len(accounts))
	for i, acc := range accounts {
		signer, err := user.NewSigner(kr, enc, chainid, appconsts.LatestVersion, user.NewAccount(acc, accInfos[i].AccountNum, accInfos[i].Sequence))
		require.NoError(t, err)
		txs[i], _, err = signer.CreatePayForBlobs(acc, blobs[i], DefaultTxOpts()...)
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
) (coretypes.Tx, *share.Blob) {
	t.Helper()
	blob := ManyRandBlobs(rand, 100)[0]
	acc := signer.Accounts()[0]
	require.NotNil(t, acc)
	msg, err := blobtypes.NewMsgPayForBlobs(acc.Address().String(), appconsts.LatestVersion, blob)
	require.NoError(t, err)
	msg.Namespaces[0] = bytes.Repeat([]byte{1}, 33) // invalid namespace

	rawTx, _, err := signer.CreateTx([]sdk.Msg{msg}, DefaultTxOpts()...)
	require.NoError(t, err)

	require.NoError(t, err)

	cTx, err := coretypes.MarshalIndexWrapper(rawTx, index)
	require.NoError(t, err)

	return cTx, blob
}

func RandBlobTxsWithNamespacesAndSigner(
	signer *user.Signer,
	namespaces []share.Namespace,
	sizes []int,
) []coretypes.Tx {
	txs := make([]coretypes.Tx, len(namespaces))
	for i := 0; i < len(namespaces); i++ {
		// take the first account the signer has
		acc := signer.Accounts()[0]
		_, b := RandMsgPayForBlobsWithNamespaceAndSigner(acc.Address().String(), namespaces[i], sizes[i])
		cTx, _, err := signer.CreatePayForBlobs(acc.Name(), []*share.Blob{b}, DefaultTxOpts()...)
		if err != nil {
			panic(err)
		}
		txs[i] = cTx
	}

	return txs
}

func ComplexBlobTxWithOtherMsgs(t *testing.T, rand *tmrand.Rand, signer *user.Signer, msgs ...sdk.Msg) coretypes.Tx {
	t.Helper()
	addr := signer.Accounts()[0].Address().String()

	pfb, blobs := RandMsgPayForBlobsWithSigner(rand, addr, 100, 1)
	msgs = append(msgs, pfb)

	rawTx, _, err := signer.CreateTx(msgs, DefaultTxOpts()...)
	require.NoError(t, err)

	require.NoError(t, err)

	btx, err := tx.MarshalBlobTx(rawTx, blobs...)
	require.NoError(t, err)
	return btx
}

func GenerateRandomBlobCount() int {
	v := rand.Intn(TestMaxBlobCount)
	if v == 0 {
		v = 1
	}
	return v
}

func GenerateRandomBlobSize() int {
	v := rand.Intn(TestMaxBlobSize)
	if v == 0 {
		v = 1
	}
	return v
}

// GenerateRandomBlobSizes returns a slice of random non-zero blob sizes.
func GenerateRandomBlobSizes(count int) []int {
	sizes := make([]int, count)
	for i := range sizes {
		sizes[i] = GenerateRandomBlobSize()
	}
	return sizes
}

// RandMultiBlobTxsSameSigner returns a slice of random Blob transactions (consisting of pfbCount number of txs) each with random number of blobs and blob sizes.
func RandMultiBlobTxsSameSigner(t *testing.T, rand *tmrand.Rand, signer *user.Signer, pfbCount int) []coretypes.Tx {
	pfbTxs := make([]coretypes.Tx, pfbCount)
	var err error
	for i := 0; i < pfbCount; i++ {
		blobsPerPfb := GenerateRandomBlobCount()
		blobSizes := GenerateRandomBlobSizes(blobsPerPfb)
		blobs := ManyRandBlobs(rand, blobSizes...)
		pfbTxs[i], _, err = signer.CreatePayForBlobs(testfactory.TestAccName, blobs)
		require.NoError(t, err)
	}
	return pfbTxs
}
