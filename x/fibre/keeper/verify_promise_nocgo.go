//go:build !cgo || gofuzz

package keeper

import "github.com/celestiaorg/celestia-app/v10/fibre"

func verifyPromise(p *fibre.PaymentPromise) bool {
	return p.Validate() == nil
}
