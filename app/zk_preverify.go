package app

import (
	"bytes"
	"slices"

	"github.com/celestiaorg/celestia-app/v12/pkg/parallel"
	zkismtypes "github.com/celestiaorg/celestia-app/v12/x/zkism/types"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// zkismTypeURLPrefix prefixes every zkism message type URL, which a tx carries verbatim, also
// inside an authz Any, so txs without it are skipped undecoded.
var zkismTypeURLPrefix = []byte("/celestia.zkism.v1.Msg")

// zkProofMsgs returns the proof-carrying zkism messages of txs in block order, direct or one level
// inside an authz MsgExec. ICA-dispatched messages are left to verify inline.
func zkProofMsgs(decode sdk.TxDecoder, txs [][]byte) []sdk.Msg {
	perTx := make([][]sdk.Msg, len(txs))
	parallel.ForEach(len(txs), func(i int) {
		// A panic leaves the tx out; its proofs then verify inline.
		defer func() { _ = recover() }()
		if blobtx.HasBlobTxTypeID(txs[i]) || !bytes.Contains(txs[i], zkismTypeURLPrefix) {
			return
		}
		tx, err := decode(txs[i])
		if err != nil {
			return
		}
		for _, msg := range tx.GetMsgs() {
			msgs := []sdk.Msg{msg}
			if exec, ok := msg.(*authz.MsgExec); ok {
				msgs, _ = exec.GetMessages()
			}
			perTx[i] = appendZKProofMsgs(perTx[i], msgs)
		}
	})()
	return slices.Concat(perTx...)
}

// appendZKProofMsgs appends the messages of msgs that verify a ZK proof, the proof-verifying
// subset of zkProofMsgWeight.
func appendZKProofMsgs(dst, msgs []sdk.Msg) []sdk.Msg {
	for _, msg := range msgs {
		switch msg.(type) {
		case *zkismtypes.MsgUpdateInterchainSecurityModule, *zkismtypes.MsgSubmitMessages:
			dst = append(dst, msg)
		}
	}
	return dst
}
