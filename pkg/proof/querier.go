package proof

import (
	"bytes"
	"fmt"
	"math"
	"strconv"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/encoding/protowire"
)

const TxInclusionQueryPath = "txInclusionProof"

// maxQueryDataSize is the upper bound on the size of req.Data accepted by the
// inclusion proof query handlers. It mirrors the governance upper bound on the
// block MaxBytes parameter so any legitimate block payload fits, while
// preventing callers from triggering an unbounded square.Construct invocation
// (and the associated heap amplification) via /abci_query.
const maxQueryDataSize = appconsts.DefaultUpperBoundMaxBytes

// QueryTxInclusionProof defines the logic performed when the ABCI client uses the Query
// method with the custom query path. The index of the transaction being
// proved must be appended to the path. The marshalled bytes of the transaction
// proof (tmproto.ShareProof) are returned.
//
// example path for proving the third transaction in that block:
// custom/txInclusionProof/3
func QueryTxInclusionProof(_ sdk.Context, path []string, req *abci.RequestQuery) ([]byte, error) {
	// reject oversized inputs before doing any allocation-heavy work
	if len(req.Data) > maxQueryDataSize {
		return nil, fmt.Errorf("request data too large: %d bytes exceeds maximum of %d bytes", len(req.Data), maxQueryDataSize)
	}

	// parse the index from the path
	if len(path) != 1 {
		return nil, fmt.Errorf("expected query path length: 1 actual: %d ", len(path))
	}
	index, err := strconv.ParseInt(path[0], 10, 64)
	if err != nil {
		return nil, err
	}
	if index < 0 {
		return nil, fmt.Errorf("path[0] element: %q produced a negative value: %d", path[0], index)
	}

	// count the block's txs and check the index before decoding the block
	txCount, err := countBlockTxs(req.Data)
	if err != nil {
		return nil, fmt.Errorf("error reading block: %w", err)
	}
	if uint64(index) >= uint64(txCount) {
		return nil, fmt.Errorf("txIndex %d out of bounds", index)
	}

	// unmarshal the block data that is passed from the ABCI client
	pbb, err := unmarshalBlock(req.Data)
	if err != nil {
		return nil, err
	}

	// create and marshal the tx inclusion proof, which we return in the form of []byte
	shareProof, err := NewTxInclusionProof(pbb.Data.Txs, uint64(index), pbb.Header.Version.App)
	if err != nil {
		return nil, err
	}

	rawShareProof, err := shareProof.Marshal()
	if err != nil {
		return nil, err
	}

	return rawShareProof, nil
}

const ShareInclusionQueryPath = "shareInclusionProof"

// QueryShareInclusionProof defines the logic performed when querying for the
// inclusion proofs of a set of shares to the data root. The share range should
// be appended to the path. Example path for proving the set of shares [3, 5]:
// custom/shareInclusionProof/3/5
func QueryShareInclusionProof(_ sdk.Context, path []string, req *abci.RequestQuery) ([]byte, error) {
	// reject oversized inputs before doing any allocation-heavy work
	if len(req.Data) > maxQueryDataSize {
		return nil, fmt.Errorf("request data too large: %d bytes exceeds maximum of %d bytes", len(req.Data), maxQueryDataSize)
	}

	// parse the share range from the path
	if len(path) != 2 {
		return nil, fmt.Errorf("expected query path length: 2 actual: %d ", len(path))
	}
	beginShare, err := strconv.ParseInt(path[0], 10, 64)
	if err != nil {
		return nil, err
	}
	endShare, err := strconv.ParseInt(path[1], 10, 64)
	if err != nil {
		return nil, err
	}
	// reject ranges that cannot lie in any square before decoding the block
	if beginShare < 0 || endShare <= beginShare || endShare > int64(maxSquareShares) {
		return nil, fmt.Errorf("invalid share range [%d, %d): must satisfy 0 <= begin < end <= %d", beginShare, endShare, maxSquareShares)
	}

	// check the block's encoding before decoding the block
	if _, err := countBlockTxs(req.Data); err != nil {
		return nil, fmt.Errorf("error reading block: %w", err)
	}

	// unmarshal the block data that is passed from the ABCI client
	pbb, err := unmarshalBlock(req.Data)
	if err != nil {
		return nil, err
	}

	// construct the data square from the block data. As we don't have
	// access to the application's state machine we use the upper bound
	// square size instead of the square size dictated from governance
	classifiedTxs, err := fibretypes.ClassifyTxs(pbb.Data.Txs)
	if err != nil {
		return nil, err
	}
	dataSquare, err := square.Construct(classifiedTxs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	if err != nil {
		return nil, err
	}

	begin, err := safeConvertInt64ToInt(beginShare)
	if err != nil {
		return nil, err
	}
	end, err := safeConvertInt64ToInt(endShare)
	if err != nil {
		return nil, err
	}

	nID, err := ParseNamespace(dataSquare, begin, end)
	if err != nil {
		return nil, err
	}

	shareRange := share.NewRange(begin, end)
	// create and marshal the share inclusion proof, which we return in the form of []byte
	shareProof, err := NewShareInclusionProof(dataSquare, nID, shareRange)
	if err != nil {
		return nil, err
	}

	rawShareProof, err := shareProof.Marshal()
	if err != nil {
		return nil, err
	}

	return rawShareProof, nil
}

// maxSquareShares is the number of shares in the largest original data square
// the query handlers construct.
const maxSquareShares = appconsts.SquareSizeUpperBound * appconsts.SquareSizeUpperBound

// Field numbers of tmproto.Block and tmproto.Data.
const (
	blockHeaderField     protowire.Number = 1
	blockDataField       protowire.Number = 2
	blockEvidenceField   protowire.Number = 3
	blockLastCommitField protowire.Number = 4

	dataTxsField        protowire.Number = 1
	dataSquareSizeField protowire.Number = 5
	dataHashField       protowire.Number = 6
)

// blockFieldType returns the wire type of a known tmproto.Block field.
func blockFieldType(num protowire.Number) (protowire.Type, bool) {
	switch num {
	case blockHeaderField, blockDataField, blockEvidenceField, blockLastCommitField:
		return protowire.BytesType, true
	}
	return 0, false
}

// dataFieldType returns the wire type of a known tmproto.Data field.
func dataFieldType(num protowire.Number) (protowire.Type, bool) {
	switch num {
	case dataTxsField, dataHashField:
		return protowire.BytesType, true
	case dataSquareSizeField:
		return protowire.VarintType, true
	}
	return 0, false
}

// checkWireType rejects a known field encoded with the wrong wire type, as
// decoding the message would.
func checkWireType(num protowire.Number, typ protowire.Type, fieldType func(protowire.Number) (protowire.Type, bool)) error {
	if want, ok := fieldType(num); ok && typ != want {
		return fmt.Errorf("wrong wire type %d for field %d", typ, num)
	}
	return nil
}

// countBlockTxs returns the number of txs in a serialized tmproto.Block
// without decoding it, so callers can validate requests before decoding. It rejects empty txs, which no data square can hold, and
// known fields with the wrong wire type.
func countBlockTxs(bz []byte) (int, error) {
	count := 0
	for len(bz) > 0 {
		num, typ, n := protowire.ConsumeTag(bz)
		if n < 0 {
			return 0, protowire.ParseError(n)
		}
		bz = bz[n:]
		if err := checkWireType(num, typ, blockFieldType); err != nil {
			return 0, err
		}
		if num == blockDataField {
			data, n := protowire.ConsumeBytes(bz)
			if n < 0 {
				return 0, protowire.ParseError(n)
			}
			bz = bz[n:]
			var err error
			if count, err = countDataTxs(data, count); err != nil {
				return 0, err
			}
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, bz)
		if n < 0 {
			return 0, protowire.ParseError(n)
		}
		bz = bz[n:]
	}
	return count, nil
}

// countDataTxs adds the number of txs in a serialized tmproto.Data to count.
// A block may carry its data field more than once; decoding merges them.
func countDataTxs(bz []byte, count int) (int, error) {
	for len(bz) > 0 {
		num, typ, n := protowire.ConsumeTag(bz)
		if n < 0 {
			return 0, protowire.ParseError(n)
		}
		bz = bz[n:]
		if err := checkWireType(num, typ, dataFieldType); err != nil {
			return 0, err
		}
		if num == dataTxsField {
			tx, n := protowire.ConsumeBytes(bz)
			if n < 0 {
				return 0, protowire.ParseError(n)
			}
			if len(tx) == 0 {
				return 0, fmt.Errorf("empty tx at index %d", count)
			}
			bz = bz[n:]
			count++
			continue
		}
		n = protowire.ConsumeFieldValue(num, typ, bz)
		if n < 0 {
			return 0, protowire.ParseError(n)
		}
		bz = bz[n:]
	}
	return count, nil
}

// unmarshalBlock decodes a serialized tmproto.Block. It does not size the tx
// slice from countBlockTxs: decoding can still fail on a field that precedes
// the txs, and must then not have allocated for them.
func unmarshalBlock(bz []byte) (*tmproto.Block, error) {
	pbb := new(tmproto.Block)
	if err := pbb.Unmarshal(bz); err != nil {
		return nil, fmt.Errorf("error reading block: %w", err)
	}
	return pbb, nil
}

// ParseNamespace validates the share range, checks if it only contains one namespace and returns
// that namespace ID.
// The provided range, defined by startShare and endShare, is end-exclusive.
func ParseNamespace(rawShares []share.Share, startShare, endShare int) (share.Namespace, error) {
	if startShare < 0 {
		return share.Namespace{}, fmt.Errorf("start share %d should be positive", startShare)
	}

	if endShare < 0 {
		return share.Namespace{}, fmt.Errorf("end share %d should be positive", endShare)
	}

	if endShare <= startShare {
		return share.Namespace{}, fmt.Errorf("end share %d cannot be lower or equal to the starting share %d", endShare, startShare)
	}

	if endShare > len(rawShares) {
		return share.Namespace{}, fmt.Errorf("end share %d is higher than block shares %d", endShare, len(rawShares))
	}

	startShareNs := rawShares[startShare].Namespace()
	for i, sh := range rawShares[startShare:endShare] {
		ns := sh.Namespace()
		if !bytes.Equal(startShareNs.Bytes(), ns.Bytes()) {
			return share.Namespace{}, fmt.Errorf("shares range contain different namespaces at index %d: %v and %v ", i, startShareNs, ns)
		}
	}
	return startShareNs, nil
}

func safeConvertInt64ToInt(x int64) (int, error) {
	if x < math.MinInt {
		return 0, fmt.Errorf("value %d is too small to be converted to int", x)
	}
	if x > math.MaxInt {
		return 0, fmt.Errorf("value %d is too large to be converted to int", x)
	}
	return int(x), nil
}
