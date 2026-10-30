package ante

import (
	"strings"

	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

var _ sdk.AnteDecorator = ISMSignatureGasDecorator{}

// Multisig ISM metadata is a header followed by whole 65-byte signatures; any
// other length is rejected before a key is recovered.
const (
	MessageIDMultisigHeaderLen  = 68
	MerkleRootMultisigHeaderLen = 1096
	ISMSignatureLen             = 65
)

// ProcessMessageSignatures bounds the keys a multisig ISM recovers for a
// MsgProcessMessage from its hex metadata length alone. Metadata that fits
// neither multisig layout, such as a ZK ISM merkle proof, weighs zero.
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

// ISMSignatureGasDecorator charges GasPerISMSignature for every signature a
// MsgProcessMessage carries, in addition to the hyperlane module's own charge.
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

// ismSignatureCount sums ProcessMessageSignatures over msgs and one level of
// MsgExec. Nested MsgExec and ICA packets never execute a MsgProcessMessage.
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
