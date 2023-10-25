package app

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/rsmt2d"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/types"
)

type PublishFn func(*types.Header, da.DataAvailabilityHeader, *rsmt2d.ExtendedDataSquare) error

type squarePublisher struct {
	square          *rsmt2d.ExtendedDataSquare
	dah				da.DataAvailabilityHeader
	header          *types.Header
	publish         PublishFn
	txs             [][]byte
	maxSquareSizeFn func(sdk.Context) int
}

func newSquarePublisher(publishFn PublishFn) squarePublisher {
	return squarePublisher{
		publish: publishFn,
	}
}

func (p *squarePublisher) cacheSquare(header *types.Header, dah da.DataAvailabilityHeader, square *rsmt2d.ExtendedDataSquare) {
	p.header = header
	p.square = square
	p.dah = dah
}

func (p *squarePublisher) confirmHeader(h *types.Header) bool {
	has := bytes.Equal(h.DataHash, p.header.DataHash)
	p.header = h
	return has
}

func (p *squarePublisher) publishSquare() {
	if p.header == nil {
		return
	}

	if len(p.txs) > 0 {

	}

	if err := p.publish(p.header, p.square); err != nil {
		return
	}

	// reset all values
	p.square = nil
	p.header = nil
	p.txs = make([][]byte, 0)
}

func (p *squarePublisher) addTx(tx []byte) {
	if p.header != nil {
		p.txs = append(p.txs, tx)
	}
}

func (p *squarePublisher) reconstructSquare(ctx sdk.Context) error {
	dataSquare, err := square.Construct(p.txs, p.header.Version.App, p.maxSquareSizeFn(ctx))
	if err != nil {
		return err
	}

	eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
	if err != nil {
		return err
	}

	dah, 
}
