package fibre

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d"
	"github.com/celestiaorg/celestia-app/v9/pkg/rsema1d/field"
	"github.com/klauspost/reedsolomon"
)

var (
	// ErrBlobTooLarge is returned when the blob size exceeds BlobConfig.MaxDataSize.
	ErrBlobTooLarge = errors.New("blob size exceeds maximum allowed size")
	// ErrBlobCommitmentMismatch is returned when the reconstructed commitment doesn't match the expected one.
	ErrBlobCommitmentMismatch = errors.New("commitment mismatch: reconstructed data doesn't match expected commitment")
	// ErrBlobConsumed is returned when a blob is reused after being uploaded.
	ErrBlobConsumed = errors.New("blob cannot be reused after upload")
)

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

	// Coder is a cached Reed-Solomon encoder/decoder for encoding blobs.
	Coder *rsema1d.Coder
	// Assembler builds pooled row layouts for encoding blobs.
	Assembler *RowAssembler
}

// defaultBlobConfigV0 is the shared default config, created at init time.
var defaultBlobConfigV0 = func() BlobConfig {
	cfg, err := NewBlobConfigFromParams(0, DefaultProtocolParams)
	if err != nil {
		panic(fmt.Sprintf("creating default blob config v0: %v", err))
	}
	return cfg
}()

// DefaultBlobConfigV0 returns a [BlobConfig] with default values for version 0.
// The config is created once at init and shared across all callers.
func DefaultBlobConfigV0() BlobConfig {
	return defaultBlobConfigV0
}

// BlobConfigForVersion returns the [BlobConfig] for the given blob version.
// Returns an error if the version is not supported.
func BlobConfigForVersion(version uint8) (BlobConfig, error) {
	switch version {
	case 0:
		return DefaultBlobConfigV0(), nil
	default:
		return BlobConfig{}, fmt.Errorf("unsupported blob version: %d", version)
	}
}

// NewBlobConfigFromParams creates a [BlobConfig] with values derived from the given [ProtocolParams].
// Use this when you need a config with non-default protocol parameters (e.g., for testing).
func NewBlobConfigFromParams(blobVersion uint8, params ProtocolParams) (BlobConfig, error) {
	if blobVersion != 0 {
		return BlobConfig{}, fmt.Errorf("unsupported blob version: %d", blobVersion)
	}

	workers := runtime.GOMAXPROCS(0)
	codecCfg := &rsema1d.Config{
		K:           params.Rows,
		N:           params.ParityRows(),
		WorkerCount: workers,
	}

	coder, err := rsema1d.NewCoder(codecCfg)
	if err != nil {
		return BlobConfig{}, fmt.Errorf("creating rsema1d coder: %w", err)
	}

	allocCfg := DefaultRowAssemblerConfig()
	allocCfg.MaxRowSize = params.MaxRowSize(blobVersion)
	assembler, err := NewRowAssembler(codecCfg, allocCfg)
	if err != nil {
		return BlobConfig{}, fmt.Errorf("creating row assembler: %w", err)
	}

	return BlobConfig{
		BlobVersion:  blobVersion,
		OriginalRows: params.Rows,
		ParityRows:   params.ParityRows(),
		RowSize: func(dataLen int) int {
			return params.RowSize(blobVersion, dataLen+blobHeaderLen)
		},
		MaxDataSize:   params.MaxBlobSize - blobHeaderLen,
		CodingWorkers: workers,
		Coder:         coder,
		Assembler:     assembler,
	}, nil
}

// TotalRows returns the total number of rows (OriginalRows + ParityRows).
func (c BlobConfig) TotalRows() int {
	return c.OriginalRows + c.ParityRows
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
	id           BlobID
	originalID   BlobID

	// holds meta fields about the blob
	header blobHeaderV0
	// data holds the decoded original data (without header).
	data []byte

	// fields for reconstruction
	rows [][]byte

	// fields for RLC-based row verification (set via SetOrWaitVerificationContext)
	verificationCtx    *rsema1d.VerificationContext
	verificationCtxSet atomic.Bool // true once verificationCtx is ready
	ctxErr             error       // error from the Once winner (if any)
	rlcOnce            sync.Once   // ensures expensive extension runs exactly once

	consumed atomic.Bool

	// mu guards Row access against concurrent release. Row takes a read lock;
	// release takes a write lock to ensure all in-flight Row calls complete
	// before pooled memory is returned.
	mu       sync.RWMutex
	released bool
	// releaseFn returns pooled row buffers to the assembler.
	// Set when the blob was created via NewBlob with a pooled assembler.
	releaseFn func()
}

// NewBlob creates a new [Blob] instance by encoding the data.
// It takes ownership of data and may reuse it directly as backing storage for
// original rows. Callers must not modify data after calling NewBlob.
//
// The data is prefixed with a header containing the blob version and data size.
// Returns [ErrBlobTooLarge] if the data size exceeds BlobConfig.MaxDataSize.
//
// The returned blob holds pooled row buffers from the [RowAssembler].
// Pooled storage is released automatically when [Client.Upload] completes.
// Data must not be modified after ownership is transferred to the blob.
func NewBlob(data []byte, cfg BlobConfig) (*Blob, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("data cannot be empty")
	}
	if len(data) > cfg.MaxDataSize {
		return nil, fmt.Errorf("%w: data size %d exceeds maximum %d", ErrBlobTooLarge, len(data), cfg.MaxDataSize)
	}

	header := newBlobHeaderV0(len(data))
	extendedData, release, err := header.encode(data, cfg)
	if err != nil {
		return nil, err
	}

	return &Blob{
		cfg:          cfg,
		extendedData: extendedData,
		id:           NewBlobID(cfg.BlobVersion, extendedData.Commitment()),
		header:       header,
		data:         data,
		releaseFn:    release,
	}, nil
}

// NewEmptyBlob creates a new [Blob] instance for receiving and reconstructing data.
// Returns an error if the BlobID is invalid or the blob version is not supported.
func NewEmptyBlob(id BlobID) (*Blob, error) {
	if err := id.Validate(); err != nil {
		return nil, fmt.Errorf("invalid blob ID: %w", err)
	}
	cfg, err := BlobConfigForVersion(id.Version())
	if err != nil {
		return nil, err
	}
	totalRows := cfg.OriginalRows + cfg.ParityRows
	return &Blob{
		cfg:  cfg,
		id:   id,
		rows: make([][]byte, totalRows),
	}, nil
}

// ID returns the BlobID of this blob.
func (d *Blob) ID() BlobID {
	return d.id
}

// Recovered reports whether reconstruction completed with
// [WithSkipCommitmentCheck], meaning the blob was recovered from rows whose
// reconstructed commitment did not match the original on-chain BlobID.
//
// When true:
//   - [ID] returns the reconstructed blob ID derived from the recovered data
//   - [OriginalID] returns the original on-chain blob ID that was downloaded
//
// This marks recovery from an incorrect-encoding scenario where the recovered
// data is accepted as canonical even though the original commitment was invalid.
func (d *Blob) Recovered() bool {
	return d.originalID != nil
}

// OriginalID returns the original on-chain BlobID before any reconstruction overwrite.
// After recovery from an incorrect encoding (via WithSkipCommitmentCheck), this returns
// the on-chain commitment that was replaced during reconstruction.
// If no overwrite occurred, returns the current ID.
func (d *Blob) OriginalID() BlobID {
	if d.originalID != nil {
		return d.originalID
	}
	return d.id
}

// Config returns the blob's configuration.
func (d *Blob) Config() BlobConfig {
	return d.cfg
}

// RLC returns the computed random linear combination values for the original rows.
func (d *Blob) RLC() []field.GF128 {
	if d.extendedData == nil {
		return nil
	}
	return d.extendedData.RLC()
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
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.released {
		return nil, errors.New("blob row storage has been released")
	}
	if d.extendedData == nil {
		return nil, fmt.Errorf("no extended data available")
	}

	return d.extendedData.GenerateRowInclusionProof(index)
}

// SetOrWaitVerificationContext validates the RLC coefficients and sets the verification
// context for stronger per-row verification.
//
// Flow:
//  1. Fast path: if context is already set, return immediately.
//  2. Cheap validation: build the RLC Merkle tree (no extension) and check that
//     SHA256(rowRoot || rlcOrigRoot) matches the blob's commitment.
//  3. Expensive work (sync.Once): extend the RLC values and create the verification context.
//     Only one goroutine performs this; others block in Once.Do until it completes.
//
// Returns an error if the RLC coefficients are inconsistent with the commitment.
func (d *Blob) SetOrWaitVerificationContext(
	rlcOrig []field.GF128,
	rowSize int,
	sampleProof *rsema1d.RowProof,
) error {
	// Fast path: context already set.
	if d.verificationCtxSet.Load() {
		return nil
	}

	// build RLC tree (no extension) and validate root against commitment.
	config := &rsema1d.Config{
		K:           d.cfg.OriginalRows,
		N:           d.cfg.ParityRows,
		RowSize:     rowSize,
		WorkerCount: d.cfg.CodingWorkers,
	}
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	rlcOrigRoot := rsema1d.BuildPaddedRLCTree(rlcOrig, config).Root()
	// TODO: it is possible to reuse rowRoot from previous calculations before we started verifyRLC
	//  but we don't need this to be super optimised due to this case being rare
	if err := rsema1d.ValidateRLCRoot(rlcOrigRoot, rsema1d.Commitment(d.id.Commitment()), sampleProof, config); err != nil {
		return fmt.Errorf("validating RLC root against commitment: %w", err)
	}

	// Expensive work: extend RLC values and create verification context (exactly once).
	d.rlcOnce.Do(func() {
		verCtx, _, err := rsema1d.CreateVerificationContext(rlcOrig, config)
		if err != nil {
			d.ctxErr = fmt.Errorf("creating verification context: %w", err)
			return
		}
		d.verificationCtx = verCtx
		d.verificationCtxSet.Store(true)
	})

	if d.verificationCtxSet.Load() {
		return nil
	}
	return d.ctxErr
}

// VerifyRow verifies a [*rsema1d.RowInclusionProof] against the blob's commitment.
// If a verification context is set (via [SetVerificationContext]), uses the stronger
// RLC-based verification that checks each row's RLC value. Otherwise falls back to
// lightweight inclusion-only verification.
// Safe to call concurrently — performs only pure computation with no shared state mutation.
func (d *Blob) VerifyRow(row *rsema1d.RowInclusionProof) error {
	if d.verificationCtx != nil {
		if err := rsema1d.VerifyRowWithContext(&row.RowProof, d.id.Commitment(), d.verificationCtx); err != nil {
			return fmt.Errorf("verifying row %d: %w", row.Index, err)
		}
		return nil
	}
	config := &rsema1d.Config{
		K:           d.cfg.OriginalRows,
		N:           d.cfg.ParityRows,
		RowSize:     len(row.Row),
		WorkerCount: d.cfg.CodingWorkers,
	}
	if err := rsema1d.VerifyRowInclusionProof(row, d.id.Commitment(), config); err != nil {
		return fmt.Errorf("verifying row %d: %w", row.Index, err)
	}
	return nil
}

// rlcVerifier coordinates RLC-based row verification across concurrent download goroutines.
// It holds the RLC verification state and an immutable copy of the commitment,
// independently from the Blob. This prevents data races between straggler download
// goroutines and the caller's Reconstruct which may mutate the Blob.
type rlcVerifier struct {
	commitment Commitment
	cfg        BlobConfig

	verificationCtx    *rsema1d.VerificationContext
	verificationCtxSet atomic.Bool
	ctxErr             error
	rlcOnce            sync.Once
}

func newRLCVerifier(commitment Commitment, cfg BlobConfig) *rlcVerifier {
	return &rlcVerifier{
		commitment: commitment,
		cfg:        cfg,
	}
}

// setOrWaitVerificationContext validates the RLC coefficients and sets the verification
// context for stronger per-row verification.
//
// Flow:
//  1. Fast path: if context is already set, return immediately.
//  2. Cheap validation: build the RLC Merkle tree (no extension) and check that
//     SHA256(rowRoot || rlcOrigRoot) matches the blob's commitment.
//  3. Expensive work (sync.Once): extend the RLC values and create the verification context.
//     Only one goroutine performs this; others block in Once.Do until it completes.
//
// Returns an error if the RLC coefficients are inconsistent with the commitment.
func (v *rlcVerifier) setOrWaitVerificationContext(
	rlcOrig []field.GF128,
	rowSize int,
	sampleProof *rsema1d.RowProof,
) error {
	if v.verificationCtxSet.Load() {
		return nil
	}

	config := &rsema1d.Config{
		K:           v.cfg.OriginalRows,
		N:           v.cfg.ParityRows,
		RowSize:     rowSize,
		WorkerCount: v.cfg.CodingWorkers,
	}
	if err := config.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	rlcOrigRoot := rsema1d.BuildPaddedRLCTree(rlcOrig, config).Root()
	// TODO: it is possible to reuse rowRoot from previous calculations before we started verifyRLC
	//  but we don't need this to be super optimised due to this case being rare
	if err := rsema1d.ValidateRLCRoot(rlcOrigRoot, rsema1d.Commitment(v.commitment), sampleProof, config); err != nil {
		return fmt.Errorf("validating RLC root against commitment: %w", err)
	}

	v.rlcOnce.Do(func() {
		verCtx, _, err := rsema1d.CreateVerificationContext(rlcOrig, config)
		if err != nil {
			v.ctxErr = fmt.Errorf("creating verification context: %w", err)
			return
		}
		v.verificationCtx = verCtx
		v.verificationCtxSet.Store(true)
	})

	if v.verificationCtxSet.Load() {
		return nil
	}
	return v.ctxErr
}

// verifyRow verifies a [*rsema1d.RowInclusionProof] against the commitment
// using the prepared RLC verification context.
// Safe to call concurrently.
func (v *rlcVerifier) verifyRow(row *rsema1d.RowInclusionProof) error {
	if err := rsema1d.VerifyRowWithContext(&row.RowProof, v.commitment, v.verificationCtx); err != nil {
		return fmt.Errorf("verifying row %d: %w", row.Index, err)
	}
	return nil
}

// SetRow assigns a verified [*rsema1d.RowInclusionProof] to the blob.
// Returns true when the row is new, false when the row was already set (duplicate).
// The row must be verified with [VerifyRow] before calling this method.
// It is not safe to call this method concurrently.
func (d *Blob) SetRow(row *rsema1d.RowInclusionProof) bool {
	if d.rows[row.Index] != nil {
		return false
	}
	d.rows[row.Index] = row.Row
	return true
}

// ReconstructOption configures the behavior of [Blob.Reconstruct].
type ReconstructOption func(*reconstructOptions)

type reconstructOptions struct {
	skipCommitmentCheck bool
}

// WithSkipCommitmentCheck skips the commitment verification after reconstruction
// and updates the blob's ID to the real commitment derived from the reconstructed data.
// Use this when recovering from an incorrect encoding where the on-chain commitment
// differs from the commitment derived from the correct data rows.
func WithSkipCommitmentCheck() ReconstructOption {
	return func(o *reconstructOptions) {
		o.skipCommitmentCheck = true
	}
}

// Reconstruct checks the accumulated rows and reconstructs the original data.
// It is not safe to call this method concurrently.
//
// Returns:
//   - [ErrBlobCommitmentMismatch] if the reconstructed commitment doesn't match the expected one
//   - Reconstruction or decoding errors if either process fails
func (d *Blob) Reconstruct(opts ...ReconstructOption) error {
	var opt reconstructOptions
	for _, o := range opts {
		o(&opt)
	}
	// TODO(@Wondertan): Move and encapsulate inside rsema1d

	// use reedsolomon decoder directly as opposed to rsema1d.Reconstruct
	// the decoder is used to reconstruct missing shards in-place which is more efficient than copying data and
	// passing indicies as a slice of integers.
	enc, err := reedsolomon.New(d.cfg.OriginalRows, d.cfg.ParityRows, reedsolomon.WithLeopardGF16(true))
	if err != nil {
		return fmt.Errorf("creating reedsolomon decoder: %w", err)
	}

	// reconstruct missing shards in-place
	if err := enc.Reconstruct(d.rows); err != nil {
		return fmt.Errorf("reconstructing rows: %w", err)
	}

	// use EncodeParity to verify the commitment and populate extendedData and rlcCoeffs
	config := &rsema1d.Config{
		K:           d.cfg.OriginalRows,
		N:           d.cfg.ParityRows,
		RowSize:     len(d.rows[0]), // NOTE: successful reconstruct must fill all rows, so if this ever panics something is really wrong
		WorkerCount: d.cfg.CodingWorkers,
	}
	extendedData, reconstructedCommitment, _, err := rsema1d.EncodeParity(d.rows, config)
	if err != nil {
		return fmt.Errorf("encoding parity: %w", err)
	}

	// verify commitment matches
	if !opt.skipCommitmentCheck && d.id.Commitment() != reconstructedCommitment {
		return fmt.Errorf("%w: expected %x, got %x",
			ErrBlobCommitmentMismatch, d.id.Commitment(), reconstructedCommitment[:])
	}

	// decode header and extract original data from the first K rows, then cache it
	originalData, err := d.header.decodeFromRows(d.rows[:d.cfg.OriginalRows], d.cfg)
	if err != nil {
		return fmt.Errorf("decoding data from rows: %w", err)
	}

	d.data = originalData
	d.extendedData = extendedData
	if opt.skipCommitmentCheck {
		d.originalID = d.id
		d.id = NewBlobID(d.cfg.BlobVersion, Commitment(reconstructedCommitment))
	}
	return nil
}

// consume marks the blob as owned by an upload. Returns false if already consumed.
func (d *Blob) consume() bool {
	return d.consumed.CompareAndSwap(false, true)
}

// release returns pooled row buffers to the assembler.
// Blocks until all in-flight Row calls complete.
func (d *Blob) release() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.released {
		return
	}
	d.released = true
	if d.releaseFn != nil {
		d.releaseFn()
	}
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

// encode assembles rows from data, writes the blob header, and produces the
// commitment via the [rsema1d.Coder]. Returns the extended data and a release
// function that returns pooled buffers to the assembler.
func (h blobHeaderV0) encode(data []byte, cfg BlobConfig) (*rsema1d.ExtendedData, func(), error) {
	rowSize := cfg.RowSize(len(data))
	rows, release := cfg.Assembler.Assemble(data, rowSize, blobHeaderLen)
	h.marshalTo(rows[0])

	extData, err := cfg.Coder.Encode(rows)
	if err != nil {
		release()
		return nil, nil, fmt.Errorf("encoding data: %w", err)
	}
	return extData, release, nil
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
	if err := h.unmarshalFrom(rows[0]); err != nil {
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

// marshalTo writes the version 0 blob header into the provided buffer.
// The buffer must be at least blobHeaderLen bytes long.
// Always writes version byte as 0.
func (h blobHeaderV0) marshalTo(buf []byte) {
	buf[0] = 0 // version 0
	binary.BigEndian.PutUint32(buf[blobVersionLen:blobHeaderLen], h.dataSize)
}

// unmarshalFrom reads the blob header from the provided buffer.
// The buffer must be at least blobHeaderLen bytes long.
// Returns an error if the version byte is not 0.
func (h *blobHeaderV0) unmarshalFrom(buf []byte) error {
	if buf[0] != 0 {
		return fmt.Errorf("invalid blob version: expected 0, got %d", buf[0])
	}
	h.dataSize = binary.BigEndian.Uint32(buf[blobVersionLen:blobHeaderLen])
	return nil
}
