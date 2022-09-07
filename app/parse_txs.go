package app

import (
	"crypto/sha256"
	"errors"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// parsedTx is an interanl struct that keeps track of potentially valid txs and
// their wire messages if they have any.
type parsedTx struct {
	// the original raw bytes of the tx
	rawTx []byte
	// tx is the parsed sdk tx. this is nil for all txs that do not contain a
	// MsgWirePayForData, as we do not need to parse other types of of transactions
	tx signing.Tx
	// msg is the wire msg if it exists in the tx. This field is nil for all txs
	// that do not contain one.
	msg *types.MsgWirePayForData
	// malleatedTx is the transaction
	malleatedTx coretypes.Tx
}

func (p *parsedTx) originalHash() []byte {
	ogHash := sha256.Sum256(p.rawTx)
	return ogHash[:]
}

func (p *parsedTx) wrap(shareIndex uint32) (coretypes.Tx, error) {
	if p.malleatedTx == nil {
		return nil, errors.New("cannot wrap parsed tx that is not malleated")
	}
	return coretypes.WrapMalleatedTx(p.originalHash(), shareIndex, p.malleatedTx)
}

func (p *parsedTx) message() *core.Message {
	return &core.Message{
		NamespaceId: p.msg.MessageNameSpaceId,
		Data:        p.msg.Message,
	}
}

type parsedTxs []*parsedTx

// func (p parsedTxs) wrap(indexes []uint32) ([][]byte, error) {
// 	if p.countMalleated() != len(indexes) {
// 		return nil, errors.New("mismatched number of indexes and malleated txs")
// 	}
// 	exported := make([][]byte, len(p))
// 	counter := 0
// 	for i, ptx := range p {
// 		if ptx.malleatedTx == nil {
// 			exported[i] = ptx.rawTx
// 			continue
// 		}
// 		wrappedTx, err := ptx.wrap(indexes[counter])
// 		if err != nil {
// 			return nil, err
// 		}
// 		exported[i] = wrappedTx
// 		counter++
// 	}

// 	return exported, nil
// }

func (p parsedTxs) countMalleated() int {
	count := 0
	for _, pTx := range p {
		if pTx.malleatedTx != nil {
			count++
		}
	}
	return count
}

func (p parsedTxs) remove(i int) parsedTxs {
	if i >= len(p) {
		return p
	}
	copy(p[i:], p[i+1:])
	p[len(p)-1] = nil
	return p[:len(p)]
}

// parseTxs decodes raw tendermint txs along with checking if they contain any
// MsgWirePayForData txs. If a MsgWirePayForData is found in the tx, then it is
// saved in the parsedTx that is returned. It ignores invalid txs completely.
func parseTxs(conf client.TxConfig, rawTxs [][]byte) parsedTxs {
	parsedTxs := []*parsedTx{}
	for _, rawTx := range rawTxs {
		tx, err := encoding.MalleatedTxDecoder(conf.TxDecoder())(rawTx)
		if err != nil {
			continue
		}

		authTx, ok := tx.(signing.Tx)
		if !ok {
			continue
		}

		pTx := parsedTx{
			rawTx: rawTx,
		}

		wireMsg, err := types.ExtractMsgWirePayForData(authTx)
		if err != nil {
			// we catch this error because it means that there are no
			// potentially valid MsgWirePayForData messages in this tx. We still
			// want to keep this tx, so we append it to the parsed txs.
			parsedTxs = append(parsedTxs, &pTx)
			continue
		}

		// run basic validation on the message
		err = wireMsg.ValidateBasic()
		if err != nil {
			continue
		}

		// run basic validation on the transaction
		err = authTx.ValidateBasic()
		if err != nil {
			continue
		}

		pTx.tx = authTx
		pTx.msg = wireMsg
		parsedTxs = append(parsedTxs, &pTx)
	}
	return parsedTxs
}
