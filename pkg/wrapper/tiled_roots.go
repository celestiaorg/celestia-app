package wrapper

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	square "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/celestiaorg/rsmt2d"
	"golang.org/x/sync/errgroup"
)

const rootTileWidth = 16

type tiledNode [2*share.NamespaceSize + sha256.Size]byte

// ComputeRoots extends a square and computes its roots, sharing leaf hashes
// between rows and columns. Nondefault trees use the original implementation.
// paritySharesNamespace is the namespace nmt collapses rather than propagates
// when it is a node's maximum. Derived from share rather than spelled out: a
// literal would silently stop matching if the namespace size ever changed, and
// the only symptom would be a different data root.
var paritySharesNamespace = share.ParitySharesNamespace.Bytes()

func init() {
	if len(paritySharesNamespace) != share.NamespaceSize {
		panic("wrapper: parity namespace is not NamespaceSize bytes")
	}
}

func (p *TreePool) ComputeRoots(data [][]byte, expectedSize uint64) (uint64, [][]byte, [][]byte, error) {
	if len(data) == 0 || len(data)&(len(data)-1) != 0 {
		return 0, nil, nil, fmt.Errorf("number of shares is not a power of 2: got %d", len(data))
	}
	n, err := square.Size(len(data))
	if err != nil {
		return 0, nil, nil, err
	}
	if p.defaultRoots && reflect.ValueOf(appconsts.NewBaseHashFunc).Pointer() == reflect.ValueOf(sha256.New).Pointer() && n >= rootTileWidth/2 && n <= 512 && n*n == len(data) && tiledInput(data, n) {
		cells, err := extendForTiledRoots(data, n)
		if err != nil {
			return 0, nil, nil, err
		}
		if uint64(n) != expectedSize {
			return uint64(n), nil, nil, nil
		}
		rows, cols := tiledRoots(cells, 2*n, p.poolSize)
		return uint64(n), rows, cols, nil
	}
	eds, err := rsmt2d.ComputeExtendedDataSquareWithBuffer(data, appconsts.DefaultCodec(), p)
	if err != nil {
		return 0, nil, nil, err
	}
	size := uint64(eds.Width() / 2)
	if size != expectedSize {
		return size, nil, nil, nil
	}
	rows, err := eds.RowRoots()
	if err != nil {
		return size, nil, nil, err
	}
	cols, err := eds.ColRoots()
	return size, rows, cols, err
}

// tiledInput establishes the leaf size and namespace order on both axes.
func tiledInput(data [][]byte, n int) bool {
	for _, cell := range data {
		if len(cell) != share.ShareSize {
			return false
		}
	}
	for r := range n {
		for c := range n {
			ns := data[r*n+c][:share.NamespaceSize]
			if c > 0 && bytes.Compare(data[r*n+c-1][:share.NamespaceSize], ns) > 0 {
				return false
			}
			if r > 0 && bytes.Compare(data[(r-1)*n+c][:share.NamespaceSize], ns) > 0 {
				return false
			}
		}
	}
	return true
}

// extendForTiledRoots follows rsmt2d's Q0-to-Q1/Q2, then Q2-to-Q3 order.
// Each worker owns disjoint output cells; no share bytes are changed.
func extendForTiledRoots(data [][]byte, n int) ([][]byte, error) {
	w := 2 * n
	cells := make([][]byte, w*w)
	for r := range n {
		copy(cells[r*w:r*w+n], data[r*n:(r+1)*n])
	}
	codec := appconsts.DefaultCodec()
	var group errgroup.Group
	group.SetLimit(max(1, runtime.NumCPU()/2))
	for i := range n {
		group.Go(func() error {
			parity, err := codec.Encode(cells[i*w : i*w+n])
			if err == nil {
				copy(cells[i*w+n:(i+1)*w], parity)
			}
			return err
		})
	}
	for i := range n {
		group.Go(func() error {
			col := make([][]byte, n)
			for r := range n {
				col[r] = cells[r*w+i]
			}
			parity, err := codec.Encode(col)
			if err == nil {
				for r := range n {
					cells[(r+n)*w+i] = parity[r]
				}
			}
			return err
		})
	}
	if err := group.Wait(); err != nil {
		return nil, err
	}
	for r := n; r < w; r++ {
		group.Go(func() error {
			parity, err := codec.Encode(cells[r*w : r*w+n])
			if err == nil {
				copy(cells[r*w+n:(r+1)*w], parity)
			}
			return err
		})
	}
	return cells, group.Wait()
}

func tiledParent(left, right *tiledNode) (out tiledNode) {
	var input [1 + 2*len(tiledNode{})]byte
	input[0] = 1
	copy(input[1:], left[:])
	copy(input[1+len(left):], right[:])
	digest := sha256.Sum256(input[:])
	copy(out[:share.NamespaceSize], left[:share.NamespaceSize])
	maximum := right[share.NamespaceSize : 2*share.NamespaceSize]
	if bytes.Equal(right[:share.NamespaceSize], paritySharesNamespace) {
		maximum = left[share.NamespaceSize : 2*share.NamespaceSize]
	}
	copy(out[share.NamespaceSize:], maximum)
	copy(out[2*share.NamespaceSize:], digest[:])
	return out
}

func reduceTiledNodes(nodes []tiledNode) tiledNode {
	for len(nodes) > 1 {
		for i := 0; i < len(nodes); i += 2 {
			nodes[i/2] = tiledParent(&nodes[i], &nodes[i+1])
		}
		nodes = nodes[:len(nodes)/2]
	}
	return nodes[0]
}

func tiledRoots(cells [][]byte, width, concurrency int) ([][]byte, [][]byte) {
	count := width / rootTileWidth
	rows := make([]tiledNode, width*count)
	cols := make([]tiledNode, width*count)
	parityNS := share.ParitySharesNamespace.Bytes()
	var next atomic.Int64
	var workers sync.WaitGroup
	for range min(runtime.NumCPU(), concurrency, count*count) {
		workers.Go(func() {
			var tile [rootTileWidth * rootTileWidth]tiledNode
			var axis [rootTileWidth]tiledNode
			var input [1 + share.NamespaceSize + share.ShareSize]byte
			for {
				job := int(next.Add(1)) - 1
				if job >= count*count {
					return
				}
				tr, tc := job/count, job%count
				for r := range rootTileWidth {
					for c := range rootTileWidth {
						row, col := tr*rootTileWidth+r, tc*rootTileWidth+c
						cell := cells[row*width+col]
						ns := parityNS
						if row < width/2 && col < width/2 {
							ns = cell[:share.NamespaceSize]
						}
						copy(input[1:], ns)
						copy(input[1+share.NamespaceSize:], cell)
						hash := sha256.Sum256(input[:])
						leaf := &tile[r*rootTileWidth+c]
						copy(leaf[:share.NamespaceSize], ns)
						copy(leaf[share.NamespaceSize:], ns)
						copy(leaf[2*share.NamespaceSize:], hash[:])
					}
				}
				for r := range rootTileWidth {
					copy(axis[:], tile[r*rootTileWidth:(r+1)*rootTileWidth])
					rows[(tr*rootTileWidth+r)*count+tc] = reduceTiledNodes(axis[:])
				}
				for c := range rootTileWidth {
					for r := range rootTileWidth {
						axis[r] = tile[r*rootTileWidth+c]
					}
					cols[(tc*rootTileWidth+c)*count+tr] = reduceTiledNodes(axis[:])
				}
			}
		})
	}
	workers.Wait()
	rowRoots, colRoots := make([][]byte, width), make([][]byte, width)
	for i := range width {
		row := reduceTiledNodes(rows[i*count : (i+1)*count])
		col := reduceTiledNodes(cols[i*count : (i+1)*count])
		rowRoots[i], colRoots[i] = bytes.Clone(row[:]), bytes.Clone(col[:])
	}
	return rowRoots, colRoots
}
