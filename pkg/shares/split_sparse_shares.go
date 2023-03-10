package shares

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	coretypes "github.com/tendermint/tendermint/types"
	"golang.org/x/exp/slices"
)

// SparseShareSplitter lazily splits blobs into shares that will eventually be
// included in a data square. It also has methods to help progressively count
// how many shares the blobs written take up.
type SparseShareSplitter struct {
	shares []Share
}

func NewSparseShareSplitter() *SparseShareSplitter {
	return &SparseShareSplitter{}
}

// Write writes the provided blob to this sparse share splitter. It returns an
// error or nil if no error is encountered.
func (sss *SparseShareSplitter) Write(blob coretypes.Blob) error {
	if !slices.Contains(appconsts.SupportedShareVersions, blob.ShareVersion) {
		return fmt.Errorf("unsupported share version: %d", blob.ShareVersion)
	}

	rawData := blob.Data

	// First share
	b := NewBuilder(blob.NamespaceID, blob.ShareVersion, true)
	if err := b.WriteSequenceLen(uint32(len(rawData))); err != nil {
		return err
	}

	for rawData != nil {

		rawDataLeftOver := b.AddData(rawData)
		if rawDataLeftOver == nil {
			// Just call it on the latest share
			b.ZeroPadIfNecessary()
		}

		share, err := b.Build()
		if err != nil {
			return err
		}
		sss.shares = append(sss.shares, *share)

		b = NewBuilder(blob.NamespaceID, blob.ShareVersion, false)
		rawData = rawDataLeftOver
	}

	return nil
}

// RemoveBlob will remove a blob from the underlying blob state. If
// there is namespaced padding after the blob, then that is also removed.
func (sss *SparseShareSplitter) RemoveBlob(i int) (int, error) {
	j := 1
	initialCount := len(sss.shares)
	if len(sss.shares) > i+1 {
		sequenceLen, err := sss.shares[i+1].SequenceLen()
		if err != nil {
			return 0, err
		}
		// 0 means that there is padding after the share that we are about to
		// remove. to remove this padding, we increase j by 1
		// with the blob
		if sequenceLen == 0 {
			j++
		}
	}
	copy(sss.shares[i:], sss.shares[i+j:])
	sss.shares = sss.shares[:len(sss.shares)-j]
	newCount := len(sss.shares)
	return initialCount - newCount, nil
}

// WriteNamespacedPaddedShares adds empty shares using the namespace of the last written share.
// This is useful to follow the message layout rules. It assumes that at least
// one share has already been written, if not it panics.
func (sss *SparseShareSplitter) WriteNamespacedPaddedShares(count int) {
	if len(sss.shares) == 0 {
		panic("cannot write empty namespaced shares on an empty SparseShareSplitter")
	}
	if count < 0 {
		panic("cannot write negative namespaced shares")
	}
	if count == 0 {
		return
	}
	lastBlob := sss.shares[len(sss.shares)-1]
	sss.shares = append(sss.shares, NamespacePaddingShares(lastBlob.NamespaceID(), count)...)
}

// Export finalizes and returns the underlying shares.
func (sss *SparseShareSplitter) Export() []Share {
	return sss.shares
}

// Count returns the current number of shares that will be made if exporting.
func (sss *SparseShareSplitter) Count() int {
	return len(sss.shares)
}
