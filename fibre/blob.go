package fibre

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"runtime"

	"github.com/celestiaorg/rsema1d"
	"github.com/celestiaorg/rsema1d/field"
)

// ErrBlobTooLarge is returned when the blob size exceeds BlobConfig.MaxDataSize.
var ErrBlobTooLarge = errors.New("blob size exceeds maximum allowed size")

// Commitment is a commitment to a blob.
// TODO(@Wondertan): merge with rsema1d.Commitment once it has these methods.
type Commitment rsema1d.Commitment

// UnmarshalBinary decodes a [Commitment] from bytes.
func (c *Commitment) UnmarshalBinary(data []byte) error {
	if len(data) != 32 {
		return fmt.Errorf("commitment must be 32 bytes, got %d", len(data))
	}
	copy(c[:], data)
	return nil
}

// String returns the hex-encoded string representation of the commitment.
func (c Commitment) String() string {
	return hex.EncodeToString(c[:])
}

// CommitmentFromString decodes a [Commitment] from a hex-encoded string.
func CommitmentFromString(s string) (Commitment, error) {
	data, err := hex.DecodeString(s)
	if err != nil {
		return Commitment{}, fmt.Errorf("decoding hex: %w", err)
	}
	var c Commitment
	if err := c.UnmarshalBinary(data); err != nil {
		return Commitment{}, err
	}
	return c, nil
}

// Equals returns true if the two commitments are equal.
func (c Commitment) Equals(other Commitment) bool {
	return c == other
}

// BlobConfig contains configuration parameters for blob encoding and decoding.
type BlobConfig struct {
	// BlobVersion is the version of the row format.
	BlobVersion uint8
	// OriginalRows is the number of original rows before erasure coding (K in rsema1d).
	OriginalRows int
	// ParityRows is the number of parity rows added by erasure coding (N in rsema1d).
	// Total rows = OriginalRows + ParityRows.
	ParityRows int
	// RowSize computes row size given the data length.
	RowSize func(dataLen int) int
	// MaxDataSize is the maximum data size that can be passed to NewBlob.
	MaxDataSize int
	// CodingWorkers is the number of workers to use for encoding and decoding rsema1d.
	CodingWorkers int
}

// DefaultBlobConfigV0 returns a [BlobConfig] with default values for version 0.
func DefaultBlobConfigV0() BlobConfig {
	return NewBlobConfigFromParams(0, DefaultProtocolParams)
}

// NewBlobConfigFromParams creates a [BlobConfig] with values derived from the given [ProtocolParams].
// Use this when you need a config with non-default protocol parameters (e.g., for testing).
func NewBlobConfigFromParams(blobVersion uint8, p ProtocolParams) BlobConfig {
	if blobVersion != 0 {
		panic(fmt.Sprintf("unsupported blob version: %d", blobVersion))
	}

	return BlobConfig{
		BlobVersion:  blobVersion,
		OriginalRows: p.Rows,
		ParityRows:   p.ParityRows(),
		RowSize: func(dataLen int) int {
			return p.RowSize(blobVersion, dataLen+blobHeaderLen)
		},
		MaxDataSize:   p.MaxBlobSize - blobHeaderLen, // subtract the header overhead
		CodingWorkers: runtime.GOMAXPROCS(0),
	}
}

// UploadSize calculates size of blob data with padding and w/o parity.
// This is the size included in the [PaymentPromise] and the one actually paid for.
func (c BlobConfig) UploadSize(dataLen int) int {
	return c.RowSize(dataLen) * c.OriginalRows
}

// Blob represents encoded data with Reed-Solomon erasure coding.
// NOTE: The Blob currently embeds the versioned header. The long-term intention is to keep the Blob struct version independent,
// while the respective header+config combination is versioned and produce general Blob.
// Once the new version is introduced, we can consider restricting the Blob to be general, i.e. without keeping the header of a particular version.
type Blob struct {
	cfg BlobConfig

	extendedData *rsema1d.ExtendedData
	commitment   Commitment
	rlcCoeffs    []field.GF128

	// holds meta fields about the blob
	header blobHeaderV0
	// data holds the decoded original data (without header).
	data []byte
}

// NewBlob creates a new [Blob] instance by encoding the data.
// It takes the data and a [BlobConfig].
// The data is prefixed with a header containing the blob version and data size.
// Returns [ErrBlobTooLarge] if the data size exceeds BlobConfig.MaxDataSize.
func NewBlob(data []byte, cfg BlobConfig) (d *Blob, err error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}
	if len(data) > cfg.MaxDataSize {
		return nil, fmt.Errorf("%w: data size %d exceeds maximum %d", ErrBlobTooLarge, len(data), cfg.MaxDataSize)
	}

	d = &Blob{
		cfg:    cfg,
		header: newBlobHeaderV0(len(data)),
		data:   data,
	}

	rows := d.header.encodeToRows(data, cfg)
	d.extendedData, d.commitment, d.rlcCoeffs, err = rsema1d.Encode(rows, &rsema1d.Config{
		K:           cfg.OriginalRows,
		N:           cfg.ParityRows,
		RowSize:     len(rows[0]),
		WorkerCount: cfg.CodingWorkers,
	})
	if err != nil {
		return nil, fmt.Errorf("encoding data: %w", err)
	}

	return d, nil
}

// Commitment returns the commitment to the blob.
func (d *Blob) Commitment() Commitment {
	return d.commitment
}

// Config returns the blob's configuration.
func (d *Blob) Config() BlobConfig {
	return d.cfg
}

// RLCCoeffs returns RLC coefficients of the original data.
func (d *Blob) RLCCoeffs() []field.GF128 {
	return d.rlcCoeffs
}

// RowSize returns the size of each row in bytes.
// Returns 0 if no original data available to determine row size.
func (d *Blob) RowSize() int {
	return d.cfg.RowSize(len(d.data))
}

// DataSize returns the size of the original data (without header) by reading from the blob header.
// Returns 0 if no original data available to determine its size.
func (d *Blob) DataSize() int {
	return len(d.data)
}

// UploadSize calculates size of the [Blob] data with padding and w/o parity.
// This is the size included in the [PaymentPromise] and the one actually paid for.
func (d *Blob) UploadSize() int {
	return d.cfg.UploadSize(d.DataSize())
}

// Data returns the cached original data (without header).
// Returns nil if the data has not been decoded yet (call Reconstruct first for received blobs).
func (d *Blob) Data() []byte {
	return d.data
}

// Row returns the [rsema1d.RowInclusionProof] for the given index from the extended data.
func (d *Blob) Row(index int) (*rsema1d.RowInclusionProof, error) {
	if d.extendedData == nil {
		return nil, fmt.Errorf("no extended data available")
	}

	return d.extendedData.GenerateRowInclusionProof(index)
}

const (
	// blobVersionLen is the length of the version field in bytes.
	blobVersionLen = 1
	// blobDataSizeLen is the length of the data size field in bytes.
	blobDataSizeLen = 4
	// blobHeaderLen is the total length of the blob header in bytes.
	// Format: 1 byte version + 4 bytes data size
	blobHeaderLen = blobVersionLen + blobDataSizeLen
)

// blobHeaderV0 represents the version 0 blob header at the start of the first row.
// Format: 1 byte version (uint8, always 0) + 4 bytes data size (uint32)
type blobHeaderV0 struct {
	dataSize uint32
}

// newBlobHeaderV0 creates a new version 0 blob header with the given data size.
// The version field is implicitly 0 for this header type.
func newBlobHeaderV0(dataSize int) blobHeaderV0 {
	return blobHeaderV0{
		dataSize: uint32(dataSize),
	}
}

// encodeToRows encodes the data into rows with version 0 header format.
// Returns OriginalRows rows of calculated rowSize bytes each, padding with zeros as needed.
// The first row contains the header followed by data.
func (h blobHeaderV0) encodeToRows(data []byte, cfg BlobConfig) [][]byte {
	rowSize := cfg.RowSize(len(data))
	rows := make([][]byte, cfg.OriginalRows)

	// First row: allocate and write header + beginning of data
	rows[0] = make([]byte, rowSize)
	h.encode(rows[0])

	// Copy as much data as fits in the first row after the header
	firstRowDataSize := rowSize - blobHeaderLen
	if firstRowDataSize > len(data) {
		firstRowDataSize = len(data)
	}
	copy(rows[0][blobHeaderLen:], data[:firstRowDataSize])

	// Remaining rows: use slices from data (offset by what we already used)
	dataOffset := firstRowDataSize
	for i := 1; i < cfg.OriginalRows; i++ {
		start := dataOffset
		end := start + rowSize
		dataOffset += rowSize

		if end <= len(data) {
			// Full row available in data - use slice directly
			rows[i] = data[start:end:end]
			continue
		}
		// Some or no data left - allocate zero-filled padded row
		rows[i] = make([]byte, rowSize)
		if start < len(data) {
			// Partial row - insert the remaining data into the row
			copy(rows[i], data[start:])
		}
	}

	return rows
}

// decodeFromRows decodes the data from rows with version 0 header format.
// Decodes the header from the first row, validates it, then extracts the original data.
// Returns error if rows are invalid, header cannot be decoded, or data cannot be extracted.
func (h *blobHeaderV0) decodeFromRows(rows [][]byte, cfg BlobConfig) ([]byte, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("no rows to decode")
	}

	if len(rows[0]) < blobHeaderLen {
		return nil, fmt.Errorf("first row too small: need at least %d bytes for header, got %d", blobHeaderLen, len(rows[0]))
	}

	// decode header from first row
	if err := h.decode(rows[0]); err != nil {
		return nil, fmt.Errorf("decoding header: %w", err)
	}

	// validate blob size is within reasonable bounds
	if h.dataSize == 0 {
		return nil, fmt.Errorf("invalid blob size in header: must be greater than 0")
	}
	if int(h.dataSize) > cfg.MaxDataSize {
		return nil, fmt.Errorf("blob size in header (%d bytes) exceeds maximum allowed size (%d bytes)", h.dataSize, cfg.MaxDataSize)
	}

	dataSize := int(h.dataSize)

	// pre-allocate only the data size (excluding header)
	data := make([]byte, dataSize)
	offset := 0
	for i := 0; i < cfg.OriginalRows && offset < dataSize; i++ {
		// skip header in first row
		row := rows[i]
		if i == 0 {
			row = row[blobHeaderLen:]
		}

		offset += copy(data[offset:], row)
	}

	if offset != dataSize {
		return nil, fmt.Errorf("data size mismatch: copied %d bytes, expected %d", offset, dataSize)
	}

	return data, nil
}

// encode writes the version 0 blob header into the provided buffer.
// The buffer must be at least blobHeaderLen bytes long.
// Always writes version byte as 0.
func (h blobHeaderV0) encode(buf []byte) {
	buf[0] = 0 // version 0
	binary.BigEndian.PutUint32(buf[blobVersionLen:blobHeaderLen], h.dataSize)
}

// decode reads the blob header from the provided buffer.
// The buffer must be at least blobHeaderLen bytes long.
// Returns an error if the version byte is not 0.
func (h *blobHeaderV0) decode(buf []byte) error {
	if buf[0] != 0 {
		return fmt.Errorf("invalid blob version: expected 0, got %d", buf[0])
	}
	h.dataSize = binary.BigEndian.Uint32(buf[blobVersionLen:blobHeaderLen])
	return nil
}
