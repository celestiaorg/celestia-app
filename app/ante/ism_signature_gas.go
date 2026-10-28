package ante

import (
	"strings"

	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

var _ sdk.AnteDecorator = ISMSignatureGasDecorator{}

// Multisig ISM metadata is a fixed header followed by whole 65-byte
// signatures, and both multisig parsers reject any other length before a key
// is recovered. The two headers differ by 1028 bytes, which is not a multiple
// of 65, so a length matches at most one layout.
const (
	MessageIDMultisigHeaderLen  = 68
	MerkleRootMultisigHeaderLen = 1096
	ISMSignatureLen             = 65
)

// ProcessMessageSignatures returns the signatures a multisig ISM would recover
// keys for from a MsgProcessMessage, from the length of its hex metadata
// alone: no decoding and no state read, so the count is O(1) per message.
// Metadata that fits neither multisig layout, such as the fixed-size merkle
// proof a ZK ISM takes or hex of odd length, recovers nothing and weighs zero.
// Which ISM the recipient uses is not consulted, since resolving it needs state
// reads whose cost is not bounded here; a multisig ISM recovers at most one key
// per signature in a layout it accepts, so the layout alone bounds the work.
func ProcessMessageSignatures(metadata string) int {
	hexLen := len(strings.TrimPrefix(metadata, "0x"))
	if hexLen%2 != 0 {
		return 0
	}
	metadataLen := hexLen / 2
	for _, headerLen := range []int{MessageIDMultisigHeaderLen, MerkleRootMultisigHeaderLen} {
		if metadataLen >= headerLen && (metadataLen-headerLen)%ISMSignatureLen == 0 {
			return (metadataLen - headerLen) / ISMSignatureLen
		}
	}
	return 0
}

// ISMSignatureGasDecorator charges gas for every signature a Hyperlane
// MsgProcessMessage carries, at a rate that prices the secp256k1 recovery the
// ISM performs per signature. Each signature also takes a slot of the
// per-block signature budget, so a signature costs its sender what it costs
// the chain whether or not the ISM accepts it. The hyperlane module charges
// its own, much smaller, per-signature gas at execution; this charge is
// additional.
type ISMSignatureGasDecorator struct{}

func NewISMSignatureGasDecorator() ISMSignatureGasDecorator {
	return ISMSignatureGasDecorator{}
}

func (ISMSignatureGasDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if sigs := ismSignatureCount(tx.GetMsgs()); sigs > 0 {
		ctx.GasMeter().ConsumeGas(uint64(sigs)*appconsts.GasPerISMSignature, "ante: ism signature verification")
	}
	return next(ctx, tx, simulate)
}

// ismSignatureCount sums ProcessMessageSignatures over msgs and over the
// messages a MsgExec dispatches. A nested MsgExec is skipped: NestedMsgDecorator
// rejects it, so no message inside it ever executes. ICA packets are not
// walked: proposal counting fails closed on any MsgProcessMessage inside one,
// so no such tx reaches execution.
func ismSignatureCount(msgs []sdk.Msg) int {
	count := 0
	for _, msg := range msgs {
		switch msg := msg.(type) {
		case *hyperlanetypes.MsgProcessMessage:
			count += ProcessMessageSignatures(msg.Metadata)
		case *authz.MsgExec:
			nested, err := msg.GetMessages()
			if err != nil {
				continue
			}
			for _, nestedMsg := range nested {
				if process, ok := nestedMsg.(*hyperlanetypes.MsgProcessMessage); ok {
					count += ProcessMessageSignatures(process.Metadata)
				}
			}
		}
	}
	return count
}
