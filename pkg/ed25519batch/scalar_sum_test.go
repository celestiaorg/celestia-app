package ed25519batch

import (
	"crypto/rand"
	"testing"

	"github.com/oasisprotocol/curve25519-voi/curve/scalar"
	"github.com/stretchr/testify/require"
)

// referenceSum reduces each product mod the group order and adds them there,
// which is what the accumulator has to agree with.
func referenceSum(t *testing.T, coefficients []*[32]byte, values []*scalar.Scalar) *scalar.Scalar {
	t.Helper()

	total := scalar.NewFromUint64(0)
	for i, coefficient := range coefficients {
		var wide [64]byte
		copy(wide[:], coefficient[:])
		var c scalar.Scalar
		_, err := c.SetBytesModOrderWide(wide[:])
		require.NoError(t, err)

		var product scalar.Scalar
		product.Mul(&c, values[i])
		total.Add(total, &product)
	}
	return total
}

// randomScalar returns a uniformly random scalar below the group order.
func randomScalar(t *testing.T) *scalar.Scalar {
	t.Helper()

	var wide [64]byte
	_, err := rand.Read(wide[:])
	require.NoError(t, err)
	var s scalar.Scalar
	_, err = s.SetBytesModOrderWide(wide[:])
	require.NoError(t, err)
	return &s
}

// coefficient builds a 32-byte coefficient from little-endian bytes.
func coefficient(b ...byte) *[32]byte {
	var out [32]byte
	copy(out[:], b)
	return &out
}

func TestScalarSumMatchesScalarLibrary(t *testing.T) {
	maxCoefficient := coefficient()
	for i := range 16 {
		maxCoefficient[i] = 0xff
	}
	// Above 2^128: the third word the accumulator reads is only non-zero here.
	wideCoefficient := coefficient()
	for i := range 24 {
		wideCoefficient[i] = 0xff
	}

	one := scalar.NewFromUint64(1)
	largest := randomScalar(t)

	tests := map[string]struct {
		coefficients []*[32]byte
		values       []*scalar.Scalar
	}{
		"zero coefficient":       {[]*[32]byte{coefficient()}, []*scalar.Scalar{largest}},
		"coefficient of one":     {[]*[32]byte{coefficient(1)}, []*scalar.Scalar{largest}},
		"value of one":           {[]*[32]byte{maxCoefficient}, []*scalar.Scalar{one}},
		"full 2^128 coefficient": {[]*[32]byte{maxCoefficient}, []*scalar.Scalar{largest}},
		"coefficient above 2^128": {
			[]*[32]byte{wideCoefficient},
			[]*scalar.Scalar{largest},
		},
		"carry across limbs": {
			[]*[32]byte{maxCoefficient, maxCoefficient, maxCoefficient},
			[]*scalar.Scalar{largest, largest, largest},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			var sum scalarSum
			for i, c := range tc.coefficients {
				sum.addProduct(c, tc.values[i])
			}
			var got scalar.Scalar
			sum.reduce(&got)

			want := referenceSum(t, tc.coefficients, tc.values)
			require.Equal(t, 1, got.Equal(want), "accumulator and scalar library disagree")
		})
	}
}

// TestScalarSumAccumulatesManyProducts drives the accumulator over a full
// batch's worth of random products, where the carries actually propagate.
func TestScalarSumAccumulatesManyProducts(t *testing.T) {
	const products = 256

	coefficients := make([]*[32]byte, 0, products)
	values := make([]*scalar.Scalar, 0, products)
	for range products {
		var c [32]byte
		_, err := rand.Read(c[:16])
		require.NoError(t, err)
		coefficients = append(coefficients, &c)
		values = append(values, randomScalar(t))
	}

	var sum scalarSum
	for i, c := range coefficients {
		sum.addProduct(c, values[i])
	}
	var got scalar.Scalar
	sum.reduce(&got)
	require.Equal(t, 1, got.Equal(referenceSum(t, coefficients, values)))

	// A zeroed accumulator is reusable: the verifier clears it between batches
	// rather than allocating a new one.
	clear(sum[:])
	sum.addProduct(coefficients[0], values[0])
	sum.reduce(&got)
	require.Equal(t, 1, got.Equal(referenceSum(t, coefficients[:1], values[:1])))
}
