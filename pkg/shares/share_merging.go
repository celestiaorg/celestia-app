package shares

import (
	"bytes"
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/celestiaorg/rsmt2d"
	"github.com/gogo/protobuf/proto"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// Merge extracts block data from an extended data square.
func Merge(eds *rsmt2d.ExtendedDataSquare) (coretypes.Data, error) {
	squareSize := eds.Width() / 2

	// sort block data shares by namespace
	var (
		sortedTxShares  [][]byte
		sortedEvdShares [][]byte
		sortedMsgShares [][]byte
	)

	// iterate over each row index
	for x := uint(0); x < squareSize; x++ {
		// iterate over each share in the original data square
		row := eds.Row(x)

		for _, share := range row[:squareSize] {
			// sort the data of that share types via namespace
			nid := share[:appconsts.NamespaceSize]
			switch {
			case bytes.Equal(appconsts.TxNamespaceID, nid):
				sortedTxShares = append(sortedTxShares, share)

			case bytes.Equal(appconsts.EvidenceNamespaceID, nid):
				sortedEvdShares = append(sortedEvdShares, share)

			case bytes.Equal(appconsts.TailPaddingNamespaceID, nid):
				continue

			// ignore unused but reserved namespaces
			case bytes.Compare(nid, appconsts.MaxReservedNamespace) < 1:
				continue

			// every other namespaceID should be a message
			default:
				sortedMsgShares = append(sortedMsgShares, share)
			}
		}
	}

	// pass the raw share data to their respective parsers
	txs, err := ParseTxs(sortedTxShares)
	if err != nil {
		return coretypes.Data{}, err
	}

	evd, err := ParseEvd(sortedEvdShares)
	if err != nil {
		return coretypes.Data{}, err
	}

	msgs, err := ParseMsgs(sortedMsgShares)
	if err != nil {
		return coretypes.Data{}, err
	}

	return coretypes.Data{
		Txs:                txs,
		Evidence:           evd,
		Messages:           msgs,
		OriginalSquareSize: uint64(squareSize),
	}, nil
}

// ParseTxs collects all of the transactions from the shares provided
func ParseTxs(shares [][]byte) (coretypes.Txs, error) {
	// parse the sharse
	rawTxs, err := parseCompactShares(shares, appconsts.SupportedShareVersions)
	if err != nil {
		return nil, err
	}

	// convert to the Tx type
	txs := make(coretypes.Txs, len(rawTxs))
	for i := 0; i < len(txs); i++ {
		txs[i] = coretypes.Tx(rawTxs[i])
	}

	return txs, nil
}

// ParseEvd collects all evidence from the shares provided.
func ParseEvd(shares [][]byte) (coretypes.EvidenceData, error) {
	// the raw data returned does not have length delimiters or namespaces and
	// is ready to be unmarshaled
	rawEvd, err := parseCompactShares(shares, appconsts.SupportedShareVersions)
	if err != nil {
		return coretypes.EvidenceData{}, err
	}

	evdList := make(coretypes.EvidenceList, len(rawEvd))

	// parse into protobuf bytes
	for i := 0; i < len(rawEvd); i++ {
		// unmarshal the evidence
		var protoEvd tmproto.Evidence
		err := proto.Unmarshal(rawEvd[i], &protoEvd)
		if err != nil {
			return coretypes.EvidenceData{}, err
		}
		evd, err := coretypes.EvidenceFromProto(&protoEvd)
		if err != nil {
			return coretypes.EvidenceData{}, err
		}

		evdList[i] = evd
	}

	return coretypes.EvidenceData{Evidence: evdList}, nil
}

// ParseMsgs collects all messages from the shares provided
func ParseMsgs(shares [][]byte) (coretypes.Messages, error) {
	msgList, err := parseSparseShares(shares, appconsts.SupportedShareVersions)
	if err != nil {
		return coretypes.Messages{}, err
	}

	return coretypes.Messages{
		MessagesList: msgList,
	}, nil
}

// ShareSequence represents a contiguous sequence of shares that are part of the
// same namespace and message. For compact shares, one share sequence exists per
// reserved namespace. For sparse shares, one share sequence exists per message.
type ShareSequence struct {
	NamespaceID namespace.ID
	Shares      []Share
}

func ParseShares(rawShares [][]byte) (result []ShareSequence, err error) {
	currentSequence := ShareSequence{}

	for _, rawShare := range rawShares {
		share, err := NewShare(rawShare)
		if err != nil {
			return result, err
		}
		infoByte, err := share.InfoByte()
		if err != nil {
			return result, err
		}
		if infoByte.IsMessageStart() {
			if len(currentSequence.Shares) > 0 {
				result = append(result, currentSequence)
			}
			currentSequence = ShareSequence{
				Shares:      []Share{share},
				NamespaceID: share.NamespaceID(),
			}
		} else {
			if !bytes.Equal(currentSequence.NamespaceID, share.NamespaceID()) {
				return result, fmt.Errorf("share sequence %v has inconsistent namespace IDs with share %v", currentSequence, share)
			}
			currentSequence.Shares = append(currentSequence.Shares, share)
		}
	}

	return result, nil
}
