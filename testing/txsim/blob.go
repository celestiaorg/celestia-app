package txsim

import (
	"context"
	"fmt"
	"math/rand"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	ns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/testutil/blobfactory"
	blob "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/gogo/protobuf/grpc"
)

var _ Sequence = &BlobSequence{}

// As napkin math, this would cover the cost of 8267 4KB blobs
const fundsForGas = 1e9 // 1000 TIA

// BlobSequence defines a pattern whereby a single user repeatedly sends a pay for blob
// message roughly every height. The PFB may consist of several blobs
type BlobSequence struct {
	namespace   ns.Namespace
	sizes       Range
	blobsPerPFB Range

	account types.AccAddress
}

func NewBlobSequence(sizes, blobsPerPFB Range) *BlobSequence {
	return &BlobSequence{
		sizes:       sizes,
		blobsPerPFB: blobsPerPFB,
	}
}

// WithNamespace provides the option of fixing a predefined namespace for
// all blobs.
func (s *BlobSequence) WithNamespace(namespace ns.Namespace) *BlobSequence {
	s.namespace = namespace
	return s
}

func (s *BlobSequence) Clone(n int) []Sequence {
	sequenceGroup := make([]Sequence, n)
	for i := 0; i < n; i++ {
		sequenceGroup[i] = &BlobSequence{
			namespace:   s.namespace,
			sizes:       s.sizes,
			blobsPerPFB: s.blobsPerPFB,
		}
	}
	return sequenceGroup
}

func (s *BlobSequence) Init(_ context.Context, _ grpc.ClientConn, allocateAccounts AccountAllocator, _ *rand.Rand) {
	s.account = allocateAccounts(1, fundsForGas)[0]
}

func (s *BlobSequence) Next(ctx context.Context, querier grpc.ClientConn, rand *rand.Rand) (Operation, error) {
	numBlobs := s.blobsPerPFB.Rand(rand)
	sizes := make([]int, numBlobs)
	namespaces := make([]ns.Namespace, numBlobs)
	for i := range sizes {
		if s.namespace.ID != nil {
			namespaces[i] = s.namespace
		} else {
			// generate a random namespace for the blob
			namespace := make([]byte, ns.NamespaceVersionZeroIDSize)
			_, err := rand.Read(namespace)
			if err != nil {
				return Operation{}, fmt.Errorf("generating random namespace: %w", err)
			}
			namespaces[i] = ns.MustNewV0(namespace)
		}
		sizes[i] = s.sizes.Rand(rand)
	}
	// generate the blobs
	blobs := blobfactory.RandBlobsWithNamespace(namespaces, sizes)
	// derive the pay for blob message
	msg, err := blob.NewMsgPayForBlobs(s.account.String(), blobs...)
	if err != nil {
		return Operation{}, err
	}
	return Operation{
		Msgs:     []types.Msg{msg},
		Blobs:    blobs,
		GasLimit: EstimateGas(sizes),
	}, nil
}

type Range struct {
	Min int
	Max int
}

func NewRange(min, max int) Range {
	return Range{Min: min, Max: max}
}

// Rand returns a random number between min (inclusive) and max (exclusive).
func (r Range) Rand(rand *rand.Rand) int {
	if r.Max <= r.Min {
		return r.Min
	}
	return rand.Intn(r.Max-r.Min) + r.Min
}

const (
	perByteGasTolerance = 2
	pfbGasFixedCost     = 80000
)

// EstimateGas estimates the gas required to pay for a set of blobs in a PFB.
func EstimateGas(blobSizes []int) uint64 {
	totalByteCount := 0
	for _, size := range blobSizes {
		totalByteCount += size
	}
	variableGasAmount := (appconsts.DefaultGasPerBlobByte + perByteGasTolerance) * totalByteCount

	return uint64(variableGasAmount + pfbGasFixedCost)
}
