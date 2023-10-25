package app

import (
	"bytes"
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/rsmt2d"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/types"
)

type PublishFn func(context.Context, *types.Header, *da.DataAvailabilityHeader, *rsmt2d.ExtendedDataSquare)

const PublishFnLabel = "PublishFn"

type squarePublisher struct {
	square          *rsmt2d.ExtendedDataSquare
	dah             *da.DataAvailabilityHeader
	header          *types.Header
	publish         PublishFn
	txs             [][]byte
	maxSquareSizeFn func(sdk.Context) int
}

func newSquarePublisher(publishFn PublishFn, maxSquareSizeFn func(sdk.Context) int) squarePublisher {
	return squarePublisher{
		publish:         publishFn,
		maxSquareSizeFn: maxSquareSizeFn,
	}
}

func (p *squarePublisher) cacheSquare(header *types.Header, dah da.DataAvailabilityHeader, square *rsmt2d.ExtendedDataSquare) {
	if p.publish == nil {
		return
	}
	p.header = header
	p.square = square
	p.dah = &dah
}

func (p *squarePublisher) confirmHeader(h *types.Header) bool {
	has := false
	if p.header != nil {
		has = bytes.Equal(h.DataHash, p.header.DataHash)
	}
	p.header = h
	return has
}

func (p *squarePublisher) publishSquare(ctx sdk.Context) {
	if p.header == nil || p.publish == nil {
		return
	}

	if len(p.txs) > 0 {
		if err := p.reconstructSquare(ctx); err != nil {
			panic(err)
		}
	}

	// don't block on publishing
	go p.publish(context.TODO(), p.header, p.dah, p.square)

	// reset all values
	p.square = nil
	p.header = nil
	p.txs = make([][]byte, 0)
}

func (p *squarePublisher) addTx(tx []byte) {
	if p.header != nil && p.publish != nil {
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

	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		return err
	}

	if !bytes.Equal(dah.Hash(), p.header.DataHash) {
		return fmt.Errorf("data availability header hash does not match the one in the header (%X != %X)", dah.Hash(), p.header.DataHash)
	}

	return nil
}
