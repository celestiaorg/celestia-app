package shares

import (
	"encoding/binary"
	"errors"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
)

type Builder struct {
	namespace      appns.Namespace
	shareVersion   uint8
	isFirstShare   bool
	isCompactShare bool
	rawShareData   []byte
}

func NewEmptyBuilder() *Builder {
	return &Builder{
		rawShareData: make([]byte, 0, appconsts.ShareSize),
	}
}

// NewBuilder returns a new share builder.
func NewBuilder(ns appns.Namespace, shareVersion uint8, isFirstShare bool) (*Builder, error) {
	b := Builder{
		namespace:      ns,
		shareVersion:   shareVersion,
		isFirstShare:   isFirstShare,
		isCompactShare: isCompactShare(ns),
	}
	if err := b.init(); err != nil {
		return nil, err
	}
	return &b, nil
}

// init initializes the share builder by populating rawShareData.
func (b *Builder) init() error {
	if b.isCompactShare {
		return b.prepareCompactShare()
	}
	return b.prepareSparseShare()
}

func (b *Builder) AvailableBytes() int {
	return appconsts.ShareSize - len(b.rawShareData)
}

func (b *Builder) ImportRawShare(rawBytes []byte) *Builder {
	b.rawShareData = rawBytes
	return b
}

func (b *Builder) AddData(rawData []byte) (rawDataLeftOver []byte) {
	// find the len left in the pending share
	pendingLeft := appconsts.ShareSize - len(b.rawShareData)

	// if we can simply add the tx to the share without creating a new
	// pending share, do so and return
	if len(rawData) <= pendingLeft {
		b.rawShareData = append(b.rawShareData, rawData...)
		return nil
	}

	// if we can only add a portion of the rawData to the pending share,
	// then we add it and add the pending share to the finalized shares.
	chunk := rawData[:pendingLeft]
	b.rawShareData = append(b.rawShareData, chunk...)

	// We need to finish this share and start a new one
	// so we return the leftover to be written into a new share
	return rawData[pendingLeft:]
}

func (b *Builder) Build() (*Share, error) {
	return NewShare(b.rawShareData)
}

// IsEmptyShare returns true if no data has been written to the share
func (b *Builder) IsEmptyShare() bool {
	expectedLen := appconsts.NamespaceSize + appconsts.ShareInfoBytes
	if b.isCompactShare {
		expectedLen += appconsts.CompactShareReservedBytes
	}
	if b.isFirstShare {
		expectedLen += appconsts.SequenceLenBytes
	}
	return len(b.rawShareData) == expectedLen
}

func (b *Builder) ZeroPadIfNecessary() (bytesOfPadding int) {
	b.rawShareData, bytesOfPadding = zeroPadIfNecessary(b.rawShareData, appconsts.ShareSize)
	return bytesOfPadding
}

// isEmptyReservedBytes returns true if the reserved bytes are empty.
func (b *Builder) isEmptyReservedBytes() (bool, error) {
	indexOfReservedBytes := b.indexOfReservedBytes()
	reservedBytes, err := ParseReservedBytes(b.rawShareData[indexOfReservedBytes : indexOfReservedBytes+appconsts.CompactShareReservedBytes])
	if err != nil {
		return false, err
	}
	return reservedBytes == 0, nil
}

// indexOfReservedBytes returns the index of the reserved bytes in the share.
func (b *Builder) indexOfReservedBytes() int {
	if b.isFirstShare {
		// if the share is the first share, the reserved bytes follow the namespace, info byte, and sequence length
		return appconsts.NamespaceSize + appconsts.ShareInfoBytes + appconsts.SequenceLenBytes
	}
	// if the share is not the first share, the reserved bytes follow the namespace and info byte
	return appconsts.NamespaceSize + appconsts.ShareInfoBytes
}

// indexOfInfoBytes returns the index of the InfoBytes.
func (b *Builder) indexOfInfoBytes() int {
	// the info byte is immediately after the namespace
	return appconsts.NamespaceSize
}

// MaybeWriteReservedBytes will be a no-op if the reserved bytes
// have already been populated. If the reserved bytes are empty, it will write
// the location of the next unit of data to the reserved bytes.
func (b *Builder) MaybeWriteReservedBytes() error {
	if !b.isCompactShare {
		return errors.New("this is not a compact share")
	}

	empty, err := b.isEmptyReservedBytes()
	if err != nil {
		return err
	}
	if !empty {
		return nil
	}

	byteIndexOfNextUnit := len(b.rawShareData)
	reservedBytes, err := NewReservedBytes(uint32(byteIndexOfNextUnit))
	if err != nil {
		return err
	}

	indexOfReservedBytes := b.indexOfReservedBytes()
	// overwrite the reserved bytes of the pending share
	for i := 0; i < appconsts.CompactShareReservedBytes; i++ {
		b.rawShareData[indexOfReservedBytes+i] = reservedBytes[i]
	}
	return nil
}

// writeSequenceLen writes the sequence length to the first share.
func (b *Builder) WriteSequenceLen(sequenceLen uint32) error {
	if b == nil {
		return errors.New("the builder object is not initialized (is nil)")
	}
	if !b.isFirstShare {
		return errors.New("not the first share")
	}
	sequenceLenBuf := make([]byte, appconsts.SequenceLenBytes)
	binary.BigEndian.PutUint32(sequenceLenBuf, sequenceLen)

	for i := 0; i < appconsts.SequenceLenBytes; i++ {
		b.rawShareData[appconsts.NamespaceSize+appconsts.ShareInfoBytes+i] = sequenceLenBuf[i]
	}

	return nil
}

// FlipSequenceStart flips the sequence start indicator of the share provided
func (b *Builder) FlipSequenceStart() {
	infoByteIndex := b.indexOfInfoBytes()

	// the sequence start indicator is the last bit of the info byte so flip the
	// last bit
	b.rawShareData[infoByteIndex] = b.rawShareData[infoByteIndex] ^ 0x01
}

func (b *Builder) prepareCompactShare() error {
	shareData := make([]byte, 0, appconsts.ShareSize)
	infoByte, err := NewInfoByte(b.shareVersion, b.isFirstShare)
	if err != nil {
		return err
	}
	placeholderSequenceLen := make([]byte, appconsts.SequenceLenBytes)
	placeholderReservedBytes := make([]byte, appconsts.CompactShareReservedBytes)

	shareData = append(shareData, b.namespace.Bytes()...)
	shareData = append(shareData, byte(infoByte))

	if b.isFirstShare {
		shareData = append(shareData, placeholderSequenceLen...)
	}

	shareData = append(shareData, placeholderReservedBytes...)

	b.rawShareData = shareData

	return nil
}

func (b *Builder) prepareSparseShare() error {
	shareData := make([]byte, 0, appconsts.ShareSize)
	infoByte, err := NewInfoByte(b.shareVersion, b.isFirstShare)
	if err != nil {
		return err
	}
	placeholderSequenceLen := make([]byte, appconsts.SequenceLenBytes)

	shareData = append(shareData, b.namespace.Bytes()...)
	shareData = append(shareData, byte(infoByte))

	if b.isFirstShare {
		shareData = append(shareData, placeholderSequenceLen...)
	}

	b.rawShareData = shareData
	return nil
}

func isCompactShare(ns appns.Namespace) bool {
	return ns.IsTx() || ns.IsPayForBlob()
}
