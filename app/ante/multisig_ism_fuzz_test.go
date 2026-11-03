package ante_test

import (
	"bytes"
	"fmt"
	"testing"

	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"
)

// ismNodeWidth is the fixed width of one message-tree node record: kind,
// validator-count choice and child count.
const ismNodeWidth = 3

// ismTree builds a message tree from fuzz records and tracks what the
// NestedMsg and MultisigISMValidators decorators must reject.
type ismTree struct {
	c          *fuzzutil.Consumer
	addr       sdk.AccAddress
	nodes      int
	oversized  bool // a multisig ISM above the validator limit, at any depth
	nestedWrap bool // a MsgExec or MsgSubmitProposal inside another one
}

var ismValidatorCounts = []int{0, 1, appconsts.MaxMultisigISMValidators, appconsts.MaxMultisigISMValidators + 1, 2 * appconsts.MaxMultisigISMValidators}

func (p *ismTree) node(depth int) sdk.Msg {
	p.nodes++
	r := p.c.Record(ismNodeWidth)
	kind, count, children := r.Byte(), int(r.Byte()), int(r.Byte()%3)
	if depth >= 3 || p.nodes > 24 {
		kind %= 3 // leaves only
	}
	switch kind % 5 {
	case 0:
		return banktypes.NewMsgSend(p.addr, p.addr, sdk.NewCoins(sdk.NewInt64Coin("utia", 1)))
	case 1, 2:
		n := ismValidatorCounts[count%len(ismValidatorCounts)]
		p.oversized = p.oversized || n > appconsts.MaxMultisigISMValidators
		validators := make([]string, n)
		for i := range validators {
			validators[i] = fmt.Sprintf("0x%040x", i)
		}
		if kind%5 == 1 {
			return &ismtypes.MsgCreateMessageIdMultisigIsm{Creator: p.addr.String(), Validators: validators, Threshold: 1}
		}
		return &ismtypes.MsgCreateMerkleRootMultisigIsm{Creator: p.addr.String(), Validators: validators, Threshold: 1}
	default: // 3: authz exec, 4: gov proposal
		if depth > 0 {
			p.nestedWrap = true
		}
		msgs := make([]sdk.Msg, 0, children)
		for range children {
			msgs = append(msgs, p.node(depth+1))
		}
		if kind%5 == 3 {
			exec := authz.NewMsgExec(p.addr, msgs)
			return &exec
		}
		proposal := &govv1.MsgSubmitProposal{Proposer: p.addr.String()}
		if err := proposal.SetMsgs(msgs); err != nil {
			panic(err)
		}
		return proposal
	}
}

// FuzzMultisigISMNestedLimit checks the ante chain's guard on multisig ISM size
// across nesting. MultisigISMValidatorsDecorator inspects only one level of
// MsgExec and MsgSubmitProposal and relies on NestedMsgDecorator, which runs
// first, to reject deeper nesting. The target builds message trees up to three
// levels deep with multisig ISMs of 0 to 200 validators, round-trips them
// through the tx encoder and decoder, and runs both decorators in chain order.
// The pair must reject exactly the txs that nest a wrapper in a wrapper or hold
// an oversized multisig ISM anywhere, so neither decorator can be relaxed
// without the other noticing.
func FuzzMultisigISMNestedLimit(f *testing.F) {
	node := func(kind, count, children byte) []byte { return []byte{kind, count, children} }
	f.Add([]byte{})
	f.Add(fuzzutil.Records(ismNodeWidth, node(1, 2, 0)))
	f.Add(fuzzutil.Records(ismNodeWidth, node(2, 3, 0)))
	f.Add(fuzzutil.Records(ismNodeWidth, node(3, 0, 1), node(1, 3, 0)))
	f.Add(fuzzutil.Records(ismNodeWidth, node(4, 0, 2), node(0, 0, 0), node(2, 4, 0)))
	f.Add(fuzzutil.Records(ismNodeWidth, node(3, 0, 1), node(4, 0, 1), node(1, 3, 0)))
	f.Add(fuzzutil.Records(ismNodeWidth, node(4, 0, 1), node(3, 0, 1), node(1, 2, 0)))

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	chain := sdk.ChainAnteDecorators(ante.NewNestedMsgDecorator(), ante.NewMultisigISMValidatorsDecorator())
	addr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	f.Fuzz(func(t *testing.T, records []byte) {
		tree := &ismTree{c: fuzzutil.New(records), addr: addr}
		var msgs []sdk.Msg
		for len(msgs) < 3 && !tree.c.Exhausted() {
			msgs = append(msgs, tree.node(0))
		}
		if len(msgs) == 0 {
			msgs = append(msgs, banktypes.NewMsgSend(addr, addr, nil))
		}

		builder := enc.TxConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msgs...))
		raw, err := enc.TxConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		tx, err := enc.TxConfig.TxDecoder()(raw)
		require.NoError(t, err)

		_, err = chain(sdk.Context{}, tx, false)
		if tree.oversized || tree.nestedWrap {
			require.Error(t, err, "oversized=%v nested=%v got past the ante guards", tree.oversized, tree.nestedWrap)
		} else {
			require.NoError(t, err)
		}
	})
}
