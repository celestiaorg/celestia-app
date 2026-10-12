package types_test

import (
	"context"
	"testing"

	txv1beta1 "cosmossdk.io/api/cosmos/tx/v1beta1"
	"cosmossdk.io/x/tx/signing"
	"cosmossdk.io/x/tx/signing/aminojson"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v10/x/teeism/types"
	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// The owner messages are what a multisig or a Ledger signs, which both use Amino
// JSON. Messages that nest a types.proto message (an identity, overrides,
// collateral) only sign if types.proto's descriptors resolve when tx.proto's do.
func TestAminoJSONSigning(t *testing.T) {
	const owner = "celestia1qyqszqgpqyqszqgpqyqszqgpqyqszqgp8e7p6w"
	ismID := util.CreateMockHexAddress("teeism", 1)

	for name, msg := range map[string]gogoproto.Message{
		"MsgUpdateEnclaveIdentity": &types.MsgUpdateEnclaveIdentity{Owner: owner, Id: ismID, Identity: identityFor(1)},
		"MsgUpdateOwner":           &types.MsgUpdateOwner{Owner: owner, Id: ismID, NewOwner: owner},
		"MsgSetVerifierOverrides": &types.MsgSetVerifierOverrides{Owner: owner, Id: ismID, Overrides: &types.VerifierOverrides{
			TrustedRootCerts: [][]byte{{0x30, 0x01}},
		}},
		"MsgSubmitAttestation": &types.MsgSubmitAttestation{
			Id: ismID, Signer: owner, Quote: []byte{1}, EventLog: []byte{2}, Payload: []byte{3},
			Collateral: &types.Collateral{PckCrl: []byte{4}, RootCaCrl: []byte{5}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			msgBytes, err := gogoproto.Marshal(msg)
			require.NoError(t, err)

			body := &txv1beta1.TxBody{Messages: []*anypb.Any{{TypeUrl: "/" + gogoproto.MessageName(msg), Value: msgBytes}}}
			bodyBytes, err := proto.Marshal(body)
			require.NoError(t, err)
			authInfo := &txv1beta1.AuthInfo{Fee: &txv1beta1.Fee{GasLimit: 200000}}
			authInfoBytes, err := proto.Marshal(authInfo)
			require.NoError(t, err)

			handler := aminojson.NewSignModeHandler(aminojson.SignModeHandlerOptions{})
			signBytes, err := handler.GetSignBytes(context.Background(),
				signing.SignerData{Address: owner, ChainID: "test-chain", AccountNumber: 1},
				signing.TxData{Body: body, AuthInfo: authInfo, BodyBytes: bodyBytes, AuthInfoBytes: authInfoBytes})
			require.NoError(t, err)
			require.NotEmpty(t, signBytes)
		})
	}
}
