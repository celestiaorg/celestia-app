package app

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v12/app/encoding"
	zkismtypes "github.com/celestiaorg/celestia-app/v12/x/zkism/types"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/gogoproto/proto"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	"github.com/stretchr/testify/require"
)

func TestZKProofMsgs(t *testing.T) {
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	// A memo with the type URL prefix makes a tx pass the byte scan without carrying a proof msg.
	encodeTx := func(memo string, msgs ...sdk.Msg) []byte {
		builder := enc.TxConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msgs...))
		builder.SetMemo(memo)
		bz, err := enc.TxConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		return bz
	}
	prefix := string(zkismTypeURLPrefix)
	update := func(tag byte) sdk.Msg { return &zkismtypes.MsgUpdateInterchainSecurityModule{Proof: []byte{tag}} }
	submit := func(tag byte) sdk.Msg { return &zkismtypes.MsgSubmitMessages{Proof: []byte{tag}} }

	blob, err := share.NewBlob(share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize)), []byte("data"), share.ShareVersionZero, nil)
	require.NoError(t, err)
	blobTx, err := blobtx.MarshalBlobTx(encodeTx("", update(10)), blob)
	require.NoError(t, err)

	txs := [][]byte{
		encodeTx(prefix, &banktypes.MsgSend{}),
		encodeTx("", update(1), &banktypes.MsgSend{}, submit(2)),
		blobTx,
		encodeTx(prefix, icaPacket(t, enc.Codec, []proto.Message{update(11)}, icatypes.EncodingProtobuf)),
		encodeTx("", execWith(submit(3), execWith(update(12)), &zkismtypes.MsgUpdateVerifyingKeys{}, update(4))),
		append([]byte(prefix), 0xff, 0x01),
		[]byte("junk"),
		encodeTx("", update(5)),
	}

	msgs := zkProofMsgs(enc.TxConfig.TxDecoder(), txs)
	tags := make([]byte, 0, len(msgs))
	for _, msg := range msgs {
		tags = append(tags, msg.(interface{ GetProof() []byte }).GetProof()[0])
	}
	require.Equal(t, []byte{1, 2, 3, 4, 5}, tags)
}
