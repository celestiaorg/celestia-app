package app

import (
	"crypto/sha256"
	"errors"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// parsedTx is an internal struct that keeps track of potentially valid txs and
// their wire messages if they have any.
type parsedTx struct {
	// the original raw bytes of the tx
	rawTx []byte
	// tx is the parsed sdk tx. this is nil for all txs that do not contain a
	// MsgWirePayForBlob, as we do not need to parse other types of of transactions
	tx signing.Tx
	// msg is the wire msg if it exists in the tx. This field is nil for all txs
	// that do not contain one.
	msg *types.MsgWirePayForBlob
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

func (p *parsedTx) blob() *core.Blob {
	return &core.Blob{
		NamespaceId: p.msg.NamespaceId,
		Data:        p.msg.Blob,
	}
}

type parsedTxs []*parsedTx

func (p parsedTxs) remove(i int) parsedTxs {
	if i >= len(p) {
		return p
	}
	copy(p[i:], p[i+1:])
	p[len(p)-1] = nil
	return p
}

// parseTxs decodes raw tendermint txs along with checking if they contain any
// MsgWirePayForBlob txs. If a MsgWirePayForBlob is found in the tx, then it is
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

		wireMsg, err := types.ExtractMsgWirePayForBlob(authTx)
		if err != nil {
			// we catch this error because it means that there are no
			// potentially valid MsgWirePayForBlob in this tx. We still
			// want to keep this tx, so we append it to the parsed txs.
			parsedTxs = append(parsedTxs, &pTx)
			continue
		}

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
