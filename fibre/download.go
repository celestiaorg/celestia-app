package fibre

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"

	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/rlc"
)

// download is the per-download session/state for fetching and reconstructing a blob.
// It dispatches workers across a selected validator set, hands shards to an
// [rsema1d.Reconstructor] for verification and decoding, and lazily acquires
// a pool slab to back the K original-row slots once the wire rowSize is known
// — small blobs don't pay for a max-sized allocation.
type download struct {
	cfg BlobConfig

	id            BlobID
	selected      []validator.SelectedValidator
	reconstructor *rsema1d.Reconstructor

	mu         sync.Mutex
	inflightWg sync.WaitGroup // ensures we await requests to finish before freeing memory
	inflight   int            // rows reserved by in-flight workers
	cursor     int            // next validator to dispatch
	sigCh      chan struct{}  // state change wakeup channel

	slabOnce sync.Once
	slab     []byte // K*rowSize contiguous pool region; nil until first Add and after Free

	rows [][]byte // K+N; rows[:K] become slab-backed after the first Add
}

func newDownload(
	blobCfg BlobConfig,
	id BlobID,
	selected []validator.SelectedValidator,
) (*download, error) {
	rec, err := blobCfg.Coder.NewReconstructor(rsema1d.Commitment(id.Commitment()))
	if err != nil {
		return nil, err
	}

	return &download{
		cfg:           blobCfg,
		id:            id,
		selected:      selected,
		reconstructor: rec,
		rows:          make([][]byte, blobCfg.TotalRows()),
		sigCh:         make(chan struct{}, 1),
	}, nil
}

// ShardSources yields one SelectedValidator per dispatchable worker slot until
// the download is final or ctx is cancelled. A slot is dispatchable when the
// selected list isn't exhausted and the K-row budget has spare reservations;
// otherwise the iterator parks until a worker frees capacity. After the range
// exits, call [download.Blob] with the same ctx for the outcome.
func (s *download) ShardSources(ctx context.Context) iter.Seq[validator.SelectedValidator] {
	return func(yield func(validator.SelectedValidator) bool) {
		for {
			if s.ready() {
				return
			}

			if from, ok := s.pick(); ok {
				if yield(from) {
					continue
				}
				return
			}

			select {
			case <-s.sigCh:
			case <-ctx.Done():
				return
			}
		}
	}
}

// pick reserves the next validator's row budget. Returns ok=false when the
// selected list is exhausted or the K-row budget is fully reserved.
func (s *download) pick() (validator.SelectedValidator, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cursor >= len(s.selected) {
		return validator.SelectedValidator{}, false
	}
	if s.reconstructor.Want() <= s.inflight {
		return validator.SelectedValidator{}, false
	}

	from := s.selected[s.cursor]
	s.cursor++
	s.inflight += from.ExpectedRows
	s.inflightWg.Add(1)
	return from, true
}

// ready reports whether dispatch can stop. The inflight==0 gate ensures every
// dispatched worker has completed its store (release decrements inflight only
// after store) before [download.Blob] reads s.rows for Reconstruct.
func (s *download) ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight > 0 {
		return false
	}

	return s.reconstructor.Want() == 0 || s.cursor >= len(s.selected)
}

// AddShard verifies a worker's shard and stores its novel rows. On the first
// successful shard it also pre-backs the K original-row slots from DataPool
// using the wire rowSize. Concurrent calls return disjoint Index sets, so
// [download.store] writes to disjoint slots without further locking. Callers
// must invoke [download.SkipShard] on error; successful adds release the
// reservation internally.
func (s *download) AddShard(from validator.SelectedValidator, proofs []*rsema1d.RowProof, rlc rlc.Vector) error {
	novel, err := s.reconstructor.Add(proofs, rlc)
	if err != nil {
		return err
	}
	if len(novel) > 0 {
		rowLn := len(novel[0].Row)
		// Reject rows larger than the reader's configured maximum before they
		// reach the DataPool, which is sized for MaxRowSize and would otherwise
		// panic. A malicious or custom uploader can serve an oversized row whose
		// proof still verifies against the (attacker-chosen) commitment.
		if s.cfg.MaxRowSize > 0 && rowLn > s.cfg.MaxRowSize {
			return fmt.Errorf("row size %d exceeds maximum %d", rowLn, s.cfg.MaxRowSize)
		}
		s.acquireSlab(rowLn)
		s.store(novel)
	}
	s.release(from)
	return nil
}

// SkipShard returns a worker's reservation without contributing rows. Must be
// called on every error path from [download.AddShard].
func (s *download) SkipShard(from validator.SelectedValidator) {
	s.release(from)
}

// store writes novel proofs into the row buffer. Original-row slots (0..K)
// are slab-backed at the wire rowSize, so the write is a single memcpy; parity
// slots (K..K+N) remain nil and adopt the wire-allocated buffer by pointer.
// Concurrent stores write to disjoint indices by construction.
func (s *download) store(proofs []*rsema1d.RowProof) {
	for _, p := range proofs {
		slot := s.rows[p.Index]
		if cap(slot) == 0 {
			s.rows[p.Index] = p.Row
			continue
		}
		// grow len from 0 to rowSize so reedsolomon's len-based
		// missing-vs-present check sees this slot as present
		s.rows[p.Index] = s.rows[p.Index][:len(p.Row)]
		copy(s.rows[p.Index], p.Row)
	}
}

func (s *download) release(from validator.SelectedValidator) {
	s.mu.Lock()
	s.inflight -= from.ExpectedRows
	s.mu.Unlock()
	s.inflightWg.Done()

	s.signal()
}

// Blob runs reconstruction and decoding and returns the resulting [Blob],
// whose data aliases the download's pool slab and remains valid until
// [Blob.Free]. Call after the [download.ShardSources] range has exited. On
// error Blob releases the slab itself, so callers do not need a deferred Free
// on the error path.
func (s *download) Blob(ctx context.Context) (*Blob, error) {
	s.inflightWg.Wait()

	if err := ctx.Err(); err != nil {
		s.freeSlab()
		return nil, err
	}

	if err := s.reconstruct(); err != nil {
		s.freeSlab()
		return nil, err
	}

	var header blobHeaderV0
	data, err := header.decode(s.slab, s.cfg)
	if err != nil {
		s.freeSlab()
		return nil, err
	}
	blob := &Blob{
		cfg:       s.cfg,
		id:        s.id,
		header:    header,
		data:      data,
		releaseFn: s.freeSlab,
	}
	blob.refCount.Store(1)
	return blob, nil
}

// reconstruct classifies the terminal outcome and, when ready, runs
// Reed-Solomon recovery so s.rows[:K] holds the original rows.
//
//   - K rows received → Reconstruct
//   - 0 rows received → [ErrNotFound]
//   - 0 < rows < K → [ErrNotEnoughShards]
func (s *download) reconstruct() error {
	err := s.reconstructor.Reconstruct(s.rows)
	if !errors.Is(err, rsema1d.ErrNotEnoughRows) {
		return err
	}

	if s.reconstructor.Have() != 0 {
		return fmt.Errorf("%w: %w", ErrNotEnoughShards, err)
	}

	return fmt.Errorf("%w: %w", ErrNotFound, err)
}

// acquireSlab grabs a K*rowSize region from DataPool and points the
// original-row slots at it. Runs at most once per download; concurrent
// AddShards racing for the first successful shard converge on the same slab
// before reaching [download.store].
func (s *download) acquireSlab(rowSize int) {
	s.slabOnce.Do(func() {
		s.slab = s.cfg.DataPool.GetRegion(s.cfg.OriginalRows, rowSize)
		for i := 0; i < s.cfg.OriginalRows; i++ {
			off := i * rowSize
			s.rows[i] = s.slab[off : off : off+rowSize]
		}
	})
}

// freeSlab returns the pool slab to its pool, if one was acquired. Idempotent;
// the slice returned by [download.Blob] must not be touched after freeSlab.
func (s *download) freeSlab() {
	if s.slab == nil {
		return
	}
	s.cfg.DataPool.PutRegion(s.slab)
	s.slab = nil
}

// RowsCount returns the number of unique rows currently stored. For instrumentation.
func (s *download) RowsCount() int {
	return s.reconstructor.Have()
}

func (s *download) signal() {
	select {
	case s.sigCh <- struct{}{}:
	default:
	}
}
