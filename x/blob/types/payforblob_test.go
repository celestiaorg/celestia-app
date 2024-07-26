package types_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	sdkerrors "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2/inclusion"
	"github.com/celestiaorg/go-square/v2/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/crypto/merkle"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func TestMsgTypeURLParity(t *testing.T) {
	require.Equal(t, sdk.MsgTypeURL(&types.MsgPayForBlobs{}), types.URLMsgPayForBlobs)
}

func TestValidateBasic(t *testing.T) {
	type test struct {
		name    string
		msg     *types.MsgPayForBlobs
		wantErr *sdkerrors.Error
	}

	validMsg := validMsgPayForBlobs(t)

	// MsgPayForBlobs that uses parity shares namespace
	paritySharesMsg := validMsgPayForBlobs(t)
	paritySharesMsg.Namespaces[0] = share.ParitySharesNamespace.Bytes()

	// MsgPayForBlobs that uses tail padding namespace
	tailPaddingMsg := validMsgPayForBlobs(t)
	tailPaddingMsg.Namespaces[0] = share.TailPaddingNamespace.Bytes()

	// MsgPayForBlobs that uses transaction namespace
	txNamespaceMsg := validMsgPayForBlobs(t)
	txNamespaceMsg.Namespaces[0] = share.TxNamespace.Bytes()

	// MsgPayForBlobs that uses intermediateStateRoots namespace
	intermediateStateRootsNamespaceMsg := validMsgPayForBlobs(t)
	intermediateStateRootsNamespaceMsg.Namespaces[0] = share.IntermediateStateRootsNamespace.Bytes()

	// MsgPayForBlobs that uses the max primary reserved namespace
	maxReservedNamespaceMsg := validMsgPayForBlobs(t)
	maxReservedNamespaceMsg.Namespaces[0] = share.MaxPrimaryReservedNamespace.Bytes()

	// MsgPayForBlobs that has an empty share commitment
	emptyShareCommitment := validMsgPayForBlobs(t)
	emptyShareCommitment.ShareCommitments[0] = []byte{}

	// MsgPayForBlobs that has an invalid share commitment size
	invalidShareCommitmentSize := validMsgPayForBlobs(t)
	invalidShareCommitmentSize.ShareCommitments[0] = bytes.Repeat([]byte{0x1}, 31)

	// MsgPayForBlobs that has no namespaces
	noNamespaces := validMsgPayForBlobs(t)
	noNamespaces.Namespaces = [][]byte{}

	// MsgPayForBlobs that has no share versions
	noShareVersions := validMsgPayForBlobs(t)
	noShareVersions.ShareVersions = []uint32{}

	// MsgPayForBlobs that has no blob sizes
	noBlobSizes := validMsgPayForBlobs(t)
	noBlobSizes.BlobSizes = []uint32{}

	// MsgPayForBlobs that has no share commitments
	noShareCommitments := validMsgPayForBlobs(t)
	noShareCommitments.ShareCommitments = [][]byte{}

	tests := []test{
		{
			name:    "valid msg",
			msg:     validMsg,
			wantErr: nil,
		},
		{
			name:    "parity shares namespace",
			msg:     paritySharesMsg,
			wantErr: types.ErrReservedNamespace,
		},
		{
			name:    "tail padding namespace",
			msg:     tailPaddingMsg,
			wantErr: types.ErrReservedNamespace,
		},
		{
			name:    "tx namespace",
			msg:     txNamespaceMsg,
			wantErr: types.ErrReservedNamespace,
		},
		{
			name:    "intermediate state root namespace",
			msg:     intermediateStateRootsNamespaceMsg,
			wantErr: types.ErrReservedNamespace,
		},
		{
			name:    "max reserved namespace",
			msg:     maxReservedNamespaceMsg,
			wantErr: types.ErrReservedNamespace,
		},
		{
			name:    "empty share commitment",
			msg:     emptyShareCommitment,
			wantErr: types.ErrInvalidShareCommitment,
		},
		{
			name:    "incorrect hash size share commitment",
			msg:     invalidShareCommitmentSize,
			wantErr: types.ErrInvalidShareCommitment,
		},
		{
			name:    "no namespace ids",
			msg:     noNamespaces,
			wantErr: types.ErrNoNamespaces,
		},
		{
			name:    "no share versions",
			msg:     noShareVersions,
			wantErr: types.ErrNoShareVersions,
		},
		{
			name:    "no blob sizes",
			msg:     noBlobSizes,
			wantErr: types.ErrNoBlobSizes,
		},
		{
			name:    "no share commitments",
			msg:     noShareCommitments,
			wantErr: types.ErrNoShareCommitments,
		},
		{
			name:    "invalid namespace version",
			msg:     invalidNamespaceVersionMsgPayForBlobs(t),
			wantErr: types.ErrInvalidNamespaceVersion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.msg.ValidateBasic()
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr.Error())
				space, code, log := sdkerrors.ABCIInfo(err, false)
				assert.Equal(t, tt.wantErr.Codespace(), space)
				assert.Equal(t, tt.wantErr.ABCICode(), code)
				t.Log(log)
			}
		})
	}
}

// totalBlobSize subtracts the delimiter size from the desired total size. this
// is useful for testing for blobs that occupy exactly so many shares.
func totalBlobSize(size int) int {
	return size - delimLen(uint64(size))
}

func delimLen(size uint64) int {
	lenBuf := make([]byte, binary.MaxVarintLen64)
	return binary.PutUvarint(lenBuf, size)
}

func validMsgPayForBlobs(t *testing.T) *types.MsgPayForBlobs {
	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)
	ns1 := share.NamespaceVersionZeroPrefix
	ns1 = append(ns1, bytes.Repeat([]byte{0x01}, share.NamespaceVersionZeroIDSize)...)
	ns := share.MustNewNamespace(share.NamespaceVersionZero, ns1)
	data := bytes.Repeat([]byte{2}, totalBlobSize(appconsts.ContinuationSparseShareContentSize*12))

	blob, err := share.NewV0Blob(ns, data)
	require.NoError(t, err)

	addr := signer.Account(testfactory.TestAccName).Address()
	pfb, err := types.NewMsgPayForBlobs(addr.String(), appconsts.LatestVersion, blob)
	assert.NoError(t, err)

	return pfb
}

func invalidNamespaceVersionMsgPayForBlobs(t *testing.T) *types.MsgPayForBlobs {
	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)
	ns1 := share.NamespaceVersionZeroPrefix
	ns1 = append(ns1, bytes.Repeat([]byte{0x01}, share.NamespaceVersionZeroIDSize)...)
	ns := share.MustNewNamespace(255, ns1)
	data := bytes.Repeat([]byte{2}, totalBlobSize(appconsts.ContinuationSparseShareContentSize*12))

	blob, err := share.NewV0Blob(ns, data)
	require.NoError(t, err)
	blobs := []*share.Blob{blob}

	commitments, err := inclusion.CreateCommitments(blobs, merkle.HashFromByteSlices, appconsts.DefaultSubtreeRootThreshold)
	require.NoError(t, err)

	namespaces, sizes, shareVersions := types.ExtractBlobComponents(blobs)

	namespacesBytes := make([][]byte, len(namespaces))
	for idx, namespace := range namespaces {
		namespacesBytes[idx] = namespace.Bytes()
	}

	addr := signer.Account(testfactory.TestAccName).Address()
	pfb := &types.MsgPayForBlobs{
		Signer:           addr.String(),
		Namespaces:       namespacesBytes,
		ShareCommitments: commitments,
		BlobSizes:        sizes,
		ShareVersions:    shareVersions,
	}

	return pfb
}

func TestNewMsgPayForBlobs(t *testing.T) {
	type testCase struct {
		name        string
		signer      string
		blobs       []*share.Blob
		expectedErr bool
	}
	ns1 := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	ns2 := share.MustNewV0Namespace(bytes.Repeat([]byte{2}, share.NamespaceVersionZeroIDSize))

	testCases := []testCase{
		{
			name:   "valid msg PFB with small blob",
			signer: testfactory.TestAccAddr,
			blobs:  []*share.Blob{mustNewBlob(t, ns1, []byte{1}, appconsts.ShareVersionZero, nil)},
		},
		{
			name:   "valid msg PFB with large blob",
			signer: testfactory.TestAccAddr,
			blobs:  []*share.Blob{mustNewBlob(t, ns1, tmrand.Bytes(1000000), appconsts.ShareVersionZero, nil)},
		},
		{
			name:   "valid msg PFB with two blobs",
			signer: testfactory.TestAccAddr,
			blobs: []*share.Blob{
				mustNewBlob(t, ns1, []byte{1}, appconsts.ShareVersionZero, nil),
				mustNewBlob(t, ns2, []byte{2}, appconsts.ShareVersionZero, nil),
			},
			expectedErr: false,
		},
		{
			name:   "unsupported share version returns an error",
			signer: testfactory.TestAccAddr,
			blobs: []*share.Blob{
				mustNewBlob(t, ns1, tmrand.Bytes(1000000), 10, nil),
			},
			expectedErr: true,
		},
		{
			name:   "msg PFB with tx namespace returns an error",
			signer: testfactory.TestAccAddr,
			blobs: []*share.Blob{
				mustNewBlob(t, share.TxNamespace, tmrand.Bytes(1000000), appconsts.ShareVersionZero, nil),
			},
			expectedErr: true,
		},
		{
			name:   "msg PFB with invalid signer returns an error",
			signer: testfactory.TestAccAddr[:10],
			blobs: []*share.Blob{
				mustNewBlob(t, ns1, []byte{1}, appconsts.ShareVersionZero, nil),
			},
			expectedErr: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			msgPFB, err := types.NewMsgPayForBlobs(tc.signer, appconsts.LatestVersion, tc.blobs...)
			if tc.expectedErr {
				assert.Error(t, err)
				return
			}

			for i, blob := range tc.blobs {
				assert.Equal(t, uint32(len(blob.Data())), msgPFB.BlobSizes[i])
				ns, err := share.NewNamespaceFromBytes(msgPFB.Namespaces[i])
				assert.NoError(t, err)
				assert.Equal(t, ns, blob.Namespace())

				expectedCommitment, err := inclusion.CreateCommitment(blob, merkle.HashFromByteSlices, appconsts.DefaultSubtreeRootThreshold)
				require.NoError(t, err)
				assert.Equal(t, expectedCommitment, msgPFB.ShareCommitments[i])
			}
		})
	}
}

func mustNewBlob(t *testing.T, ns share.Namespace, data []byte, shareVersion uint8, signer []byte) *share.Blob {
	blob, err := share.NewBlob(ns, data, shareVersion, signer)
	require.NoError(t, err)
	return blob
}

func TestValidateBlobs(t *testing.T) {
	type test struct {
		name        string
		blob        *share.Blob
		expectError bool
	}

	tests := []test{
		{
			name:        "valid blob",
			blob:        mustNewBlob(t, share.RandomBlobNamespace(), []byte{1}, appconsts.DefaultShareVersion, nil),
			expectError: false,
		},
		{
			name:        "invalid share version",
			blob:        mustNewBlob(t, share.RandomBlobNamespace(), []byte{1}, 4, nil),
			expectError: true,
		},
		{
			name:        "invalid namespace",
			blob:        mustNewBlob(t, share.TxNamespace, []byte{1}, appconsts.DefaultShareVersion, nil),
			expectError: true,
		},
	}

	for _, tt := range tests {
		err := types.ValidateBlobs(tt.blob)
		if tt.expectError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}
