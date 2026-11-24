// Package expected produces and reads expected.tsv, the trusted per-height
// values the appliers and fsboot check against. Row h holds values of header
// h+1, which describe the state after block h.
package expected

import (
	"bufio"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/cometbft/cometbft/types"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/hdrchain"
)

// Row is one line of expected.tsv.
type Row struct {
	Height             int64 // the row describes the state after Height
	AppHash            []byte
	NextHeaderHash     []byte // hash of header Height+1
	LastResultsHash    []byte
	ValidatorsHash     []byte
	NextValidatorsHash []byte
	AppVersion         uint64
}

// FromHeader returns the row header h yields (row h.Height-1). h must have
// been verified.
func FromHeader(h *types.Header) Row {
	return Row{
		Height:             h.Height - 1,
		AppHash:            h.AppHash,
		NextHeaderHash:     h.Hash(),
		LastResultsHash:    h.LastResultsHash,
		ValidatorsHash:     h.ValidatorsHash,
		NextValidatorsHash: h.NextValidatorsHash,
		AppVersion:         h.Version.App,
	}
}

// String formats the row as a TSV line without the newline.
func (r Row) String() string {
	return fmt.Sprintf("%d\t%X\t%X\t%X\t%X\t%X\t%d", r.Height, r.AppHash, r.NextHeaderHash,
		r.LastResultsHash, r.ValidatorsHash, r.NextValidatorsHash, r.AppVersion)
}

// ParseRow parses a line written by Row.String.
func ParseRow(line string) (Row, error) {
	cols := strings.Split(strings.TrimRight(line, "\r\n"), "\t")
	if len(cols) != 7 {
		return Row{}, fmt.Errorf("want 7 columns, got %d", len(cols))
	}
	var r Row
	var err error
	if r.Height, err = strconv.ParseInt(cols[0], 10, 64); err != nil {
		return Row{}, fmt.Errorf("height: %w", err)
	}
	for i, dst := range []*[]byte{&r.AppHash, &r.NextHeaderHash, &r.LastResultsHash, &r.ValidatorsHash, &r.NextValidatorsHash} {
		if *dst, err = hex.DecodeString(cols[i+1]); err != nil {
			return Row{}, fmt.Errorf("column %d: %w", i+2, err)
		}
	}
	if len(r.NextHeaderHash) != 32 {
		return Row{}, fmt.Errorf("header hash must be 32 bytes, got %d", len(r.NextHeaderHash))
	}
	if r.AppVersion, err = strconv.ParseUint(cols[6], 10, 64); err != nil {
		return Row{}, fmt.Errorf("app version: %w", err)
	}
	return r, nil
}

// Find returns row h of the expected.tsv at path.
func Find(path string, h int64) (Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return Row{}, err
	}
	defer f.Close()
	return find(f, h)
}

func find(r io.Reader, h int64) (Row, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), 1<<16)
	key := strconv.FormatInt(h, 10) + "\t"
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, key) {
			row, err := ParseRow(line)
			if err != nil {
				return Row{}, fmt.Errorf("row %d: %w", h, err)
			}
			return row, nil
		}
	}
	if err := sc.Err(); err != nil {
		return Row{}, err
	}
	return Row{}, fmt.Errorf("no row for height %d", h)
}

// Params selects the range Walk verifies.
type Params struct {
	ChainID      string
	AnchorHeight int64  // height of the trusted header; must be > To
	AnchorHash   []byte // trusted hash of header AnchorHeight
	From, To     int64  // rows wanted: the states after From..To
	Window       int64  // headers fetched per source call (memory bound)
}

func (p Params) validate() error {
	switch {
	case p.ChainID == "":
		return errors.New("empty chain id")
	case p.From < 1 || p.To < p.From:
		return fmt.Errorf("need 1 <= from (%d) <= to (%d)", p.From, p.To)
	case p.AnchorHeight <= p.To:
		return fmt.Errorf("anchor height %d must be > to (%d)", p.AnchorHeight, p.To)
	case len(p.AnchorHash) != 32:
		return fmt.Errorf("anchor hash must be 32 bytes, got %d", len(p.AnchorHash))
	case p.Window < 1:
		return errors.New("window must be positive")
	}
	return nil
}

// Walk verifies headers AnchorHeight down to From+1 with a backward hash
// chain (hdrchain.VerifyBackward) and calls emit once per window with that
// window's rows within From..To in ascending order. Windows are emitted from
// the highest to the lowest.
func Walk(ctx context.Context, p Params, src hdrchain.RangeSource, emit func([]Row) error) error {
	if err := p.validate(); err != nil {
		return err
	}
	expect := p.AnchorHash
	for hi := p.AnchorHeight; hi > p.From; {
		lo := max(hi-p.Window+1, p.From+1)
		hs, err := src.Headers(ctx, lo, hi)
		if err != nil {
			return fmt.Errorf("headers %d..%d: %w", lo, hi, err)
		}
		byHeight := make(map[int64]*types.Header, hi-lo+1)
		for _, h := range hs {
			if h == nil || h.Height < lo || h.Height > hi {
				return fmt.Errorf("headers %d..%d: source returned a header outside the range", lo, hi)
			}
			byHeight[h.Height] = h
		}
		desc := make([]*types.Header, 0, hi-lo+1)
		for height := hi; height >= lo; height-- {
			h, ok := byHeight[height]
			if !ok {
				return fmt.Errorf("source returned no header for height %d", height)
			}
			desc = append(desc, h)
		}
		if expect, err = hdrchain.VerifyBackward(p.ChainID, expect, desc); err != nil {
			return err
		}
		var rows []Row
		for i := len(desc) - 1; i >= 0; i-- { // ascending
			if desc[i].Height-1 <= p.To { // headers above to+1 only link the chain
				rows = append(rows, FromHeader(desc[i]))
			}
		}
		if err := emit(rows); err != nil {
			return err
		}
		hi = lo - 1
	}
	return nil
}
