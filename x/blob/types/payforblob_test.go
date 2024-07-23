package types_test

import (
	"bytes"
	"testing"

	sdkerrors "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/blob"
	"github.com/celestiaorg/go-square/inclusion"
	appns "github.com/celestiaorg/go-square/namespace"
	shares "github.com/celestiaorg/go-square/shares"
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
	paritySharesMsg.Namespaces[0] = appns.ParitySharesNamespace.Bytes()

	// MsgPayForBlobs that uses tail padding namespace
	tailPaddingMsg := validMsgPayForBlobs(t)
	tailPaddingMsg.Namespaces[0] = appns.TailPaddingNamespace.Bytes()

	// MsgPayForBlobs that uses transaction namespace
	txNamespaceMsg := validMsgPayForBlobs(t)
	txNamespaceMsg.Namespaces[0] = appns.TxNamespace.Bytes()

	// MsgPayForBlobs that uses intermediateStateRoots namespace
	intermediateStateRootsNamespaceMsg := validMsgPayForBlobs(t)
	intermediateStateRootsNamespaceMsg.Namespaces[0] = appns.IntermediateStateRootsNamespace.Bytes()

	// MsgPayForBlobs that uses the max primary reserved namespace
	maxReservedNamespaceMsg := validMsgPayForBlobs(t)
	maxReservedNamespaceMsg.Namespaces[0] = appns.MaxPrimaryReservedNamespace.Bytes()

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
	return size - shares.DelimLen(uint64(size))
}

func validMsgPayForBlobs(t *testing.T) *types.MsgPayForBlobs {
	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)
	ns1 := appns.NamespaceVersionZeroPrefix
	ns1 = append(ns1, bytes.Repeat([]byte{0x01}, appns.NamespaceVersionZeroIDSize)...)
	data := bytes.Repeat([]byte{2}, totalBlobSize(appconsts.ContinuationSparseShareContentSize*12))

	pblob := &blob.Blob{
		Data:             data,
		NamespaceId:      ns1,
		NamespaceVersion: uint32(appns.NamespaceVersionZero),
		ShareVersion:     uint32(appconsts.ShareVersionZero),
	}

	addr := signer.Account(testfactory.TestAccName).Address()
	pfb, err := types.NewMsgPayForBlobs(addr.String(), appconsts.LatestVersion, pblob)
	assert.NoError(t, err)

	return pfb
}

func invalidNamespaceVersionMsgPayForBlobs(t *testing.T) *types.MsgPayForBlobs {
	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)
	ns1 := appns.NamespaceVersionZeroPrefix
	ns1 = append(ns1, bytes.Repeat([]byte{0x01}, appns.NamespaceVersionZeroIDSize)...)
	data := bytes.Repeat([]byte{2}, totalBlobSize(appconsts.ContinuationSparseShareContentSize*12))

	pblob := &blob.Blob{
		Data:             data,
		NamespaceId:      ns1,
		NamespaceVersion: uint32(255),
		ShareVersion:     uint32(appconsts.ShareVersionZero),
	}

	blobs := []*blob.Blob{pblob}

	commitments, err := inclusion.CreateCommitments(blobs, merkle.HashFromByteSlices, appconsts.DefaultSubtreeRootThreshold)
	require.NoError(t, err)

	namespaceVersions, namespaceIDs, sizes, shareVersions := types.ExtractBlobComponents(blobs)
	namespaces := []appns.Namespace{}
	for i := range namespaceVersions {
		namespace, err := appns.New(uint8(namespaceVersions[i]), namespaceIDs[i])
		require.NoError(t, err)
		namespaces = append(namespaces, namespace)
	}

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
		blobs       []*blob.Blob
		expectedErr bool
	}
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	ns2 := appns.MustNewV0(bytes.Repeat([]byte{2}, appns.NamespaceVersionZeroIDSize))

	testCases := []testCase{
		{
			name:   "valid msg PFB with small blob",
			signer: testfactory.TestAccAddr,
			blobs: []*blob.Blob{
				{
					NamespaceVersion: uint32(ns1.Version),
					NamespaceId:      ns1.ID,
					Data:             []byte{1},
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				},
			},
			expectedErr: false,
		},
		{
			name:   "valid msg PFB with large blob",
			signer: testfactory.TestAccAddr,
			blobs: []*blob.Blob{
				{
					NamespaceVersion: uint32(ns1.Version),
					NamespaceId:      ns1.ID,
					Data:             tmrand.Bytes(1000000),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				},
			},
			expectedErr: false,
		},
		{
			name:   "valid msg PFB with two blobs",
			signer: testfactory.TestAccAddr,
			blobs: []*blob.Blob{
				{
					NamespaceVersion: uint32(ns1.Version),
					NamespaceId:      ns1.ID,
					Data:             []byte{1},
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				},
				{
					NamespaceVersion: uint32(ns2.Version),
					NamespaceId:      ns2.ID,
					Data:             []byte{2},
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				},
			},
		},
		{
			name:   "unsupported share version returns an error",
			signer: testfactory.TestAccAddr,
			blobs: []*blob.Blob{
				{
					NamespaceVersion: uint32(ns1.Version),
					NamespaceId:      ns1.ID,
					Data:             tmrand.Bytes(1000000),
					ShareVersion:     uint32(10), // unsupported share version
				},
			},
			expectedErr: true,
		},
		{
			name:   "msg PFB with tx namespace returns an error",
			signer: testfactory.TestAccAddr,
			blobs: []*blob.Blob{
				{
					NamespaceVersion: uint32(appns.TxNamespace.Version),
					NamespaceId:      appns.TxNamespace.ID,
					Data:             tmrand.Bytes(1000000),
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				},
			},
			expectedErr: true,
		},
		{
			name:   "msg PFB with invalid signer returns an error",
			signer: testfactory.TestAccAddr[:10],
			blobs: []*blob.Blob{
				{
					NamespaceVersion: uint32(ns1.Version),
					NamespaceId:      ns1.ID,
					Data:             []byte{1},
					ShareVersion:     uint32(appconsts.ShareVersionZero),
				},
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
				assert.Equal(t, uint32(len(blob.Data)), msgPFB.BlobSizes[i])
				ns, err := appns.From(msgPFB.Namespaces[i])
				assert.NoError(t, err)
				assert.Equal(t, ns.ID, blob.NamespaceId)
				assert.Equal(t, uint32(ns.Version), blob.NamespaceVersion)

				expectedCommitment, err := inclusion.CreateCommitment(blob, merkle.HashFromByteSlices, appconsts.DefaultSubtreeRootThreshold)
				require.NoError(t, err)
				assert.Equal(t, expectedCommitment, msgPFB.ShareCommitments[i])
			}
		})
	}
}

func TestValidateBlobs(t *testing.T) {
	type test struct {
		name        string
		blob        *blob.Blob
		expectError bool
	}

	tests := []test{
		{
			name: "valid blob",
			blob: &blob.Blob{
				Data:             []byte{1},
				NamespaceId:      appns.RandomBlobNamespace().ID,
				ShareVersion:     uint32(appconsts.DefaultShareVersion),
				NamespaceVersion: uint32(appns.NamespaceVersionZero),
			},
			expectError: false,
		},
		{
			name: "invalid share version",
			blob: &blob.Blob{
				Data:             []byte{1},
				NamespaceId:      appns.RandomBlobNamespace().ID,
				ShareVersion:     uint32(10000),
				NamespaceVersion: uint32(appns.NamespaceVersionZero),
			},
			expectError: true,
		},
		{
			name: "empty blob",
			blob: &blob.Blob{
				Data:             []byte{},
				NamespaceId:      appns.RandomBlobNamespace().ID,
				ShareVersion:     uint32(appconsts.DefaultShareVersion),
				NamespaceVersion: uint32(appns.NamespaceVersionZero),
			},
			expectError: true,
		},
		{
			name: "invalid namespace",
			blob: &blob.Blob{
				Data:             []byte{1},
				NamespaceId:      appns.TxNamespace.ID,
				ShareVersion:     uint32(appconsts.DefaultShareVersion),
				NamespaceVersion: uint32(appns.NamespaceVersionZero),
			},
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
