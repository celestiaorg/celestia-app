// Package codec adds resource preflight to application gRPC decoding.
package codec

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/app/grpc/tx"
	"github.com/cometbft/cometbft/libs/protoresource"
	"github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc/encoding"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// A canonical batch has at most 20 hex-encoded 32-byte IDs. Leave room for
// encoding overhead and small unknown fields without using the server-wide cap.
const maxBatchBytes = 4096

type boundedCodec struct {
	base  encoding.Codec
	batch *protoresource.Profile
}

// New retains the SDK codec and checks batch cardinality before it runs.
func New(base encoding.Codec) (encoding.Codec, error) {
	if base == nil {
		return nil, fmt.Errorf("nil application gRPC codec")
	}
	profile, err := protoresource.New(proto.HybridResolver, protoreflect.FullName(proto.MessageName(&tx.TxStatusBatchRequest{})), protoresource.Limits{
		Bytes: maxBatchBytes, Fields: maxBatchBytes, Messages: maxBatchBytes, Elements: maxBatchBytes, CopyBytes: maxBatchBytes, Depth: 100, AllocationUnits: 1 << 20,
	}, protoresource.FieldLimits{"celestia.core.v1.tx.TxStatusBatchRequest.tx_ids": tx.MaxTxStatusBatchIDs})
	if err != nil {
		return nil, err
	}
	return &boundedCodec{base: base, batch: profile}, nil
}

func (c *boundedCodec) Name() string                  { return c.base.Name() }
func (c *boundedCodec) Marshal(v any) ([]byte, error) { return c.base.Marshal(v) }
func (c *boundedCodec) Unmarshal(data []byte, v any) error {
	if _, ok := v.(*tx.TxStatusBatchRequest); ok {
		if err := c.batch.Check(data); err != nil {
			return err
		}
	}
	return c.base.Unmarshal(data, v)
}
