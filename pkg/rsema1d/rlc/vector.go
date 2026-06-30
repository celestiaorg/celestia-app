package rlc

import "github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/field"

// Vector is the canonical type for a sequence of GF128 values flowing through
// the RLC: derived coefficients, per-row computed values, RLC slabs on the
// wire.
type Vector []field.GF128
