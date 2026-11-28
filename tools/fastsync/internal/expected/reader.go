package expected

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// Reader returns the trusted app hashes of an expected.tsv in ascending
// height order, holding one row in memory. The file must be fshdr output
// verified for the chain (see CheckHeader).
type Reader struct {
	f   *os.File
	sc  *bufio.Scanner
	cur Row
	eof bool
}

// Open opens path and checks that fshdr verified it for chainID.
func Open(path, chainID string) (*Reader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 4096), 1<<16)
	if !sc.Scan() {
		f.Close()
		return nil, errors.Join(fmt.Errorf("%s: empty file", path), sc.Err())
	}
	if err := CheckHeader(sc.Text(), chainID); err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Reader{f: f, sc: sc, cur: Row{Height: -1}}, nil
}

// AppHash returns the app hash of the state after height h, and false when
// the file has no row for h. Calls must not go back to a lower height than
// the previous call.
func (r *Reader) AppHash(h int64) ([]byte, bool, error) {
	if h < r.cur.Height {
		return nil, false, fmt.Errorf("expected.tsv: height %d asked after %d", h, r.cur.Height)
	}
	for r.cur.Height < h && !r.eof {
		if !r.sc.Scan() {
			if err := r.sc.Err(); err != nil {
				return nil, false, err
			}
			r.eof = true
			break
		}
		row, err := ParseRow(r.sc.Text())
		if err != nil {
			return nil, false, fmt.Errorf("expected.tsv: %w", err)
		}
		if row.Height <= r.cur.Height {
			return nil, false, fmt.Errorf("expected.tsv: row %d after row %d (rows must ascend)", row.Height, r.cur.Height)
		}
		r.cur = row
	}
	if r.cur.Height != h {
		return nil, false, nil
	}
	return r.cur.AppHash, true, nil
}

// Close closes the file.
func (r *Reader) Close() error { return r.f.Close() }

// GenesisChainID returns the chain_id of a genesis.json.
func GenesisChainID(genesis []byte) (string, error) {
	var g struct {
		ChainID string `json:"chain_id"`
	}
	if err := json.Unmarshal(genesis, &g); err != nil {
		return "", fmt.Errorf("genesis: %w", err)
	}
	if g.ChainID == "" {
		return "", errors.New("genesis has no chain_id")
	}
	return g.ChainID, nil
}
