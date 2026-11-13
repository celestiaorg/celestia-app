package encoding_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	evidencetypes "cosmossdk.io/x/evidence/types"
	"cosmossdk.io/x/feegrant"
	upgradetypes "cosmossdk.io/x/upgrade/types"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/go-square/v4/share"
	squaretx "github.com/celestiaorg/go-square/v4/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	gogoproto "github.com/cosmos/gogoproto/proto"
	ibctm "github.com/cosmos/ibc-go/v8/modules/light-clients/07-tendermint"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

const (
	msgSendURL = "/cosmos.bank.v1beta1.MsgSend"
	msgExecURL = "/cosmos.authz.v1beta1.MsgExec"
	// nonCriticalField has bit 11 set, so the SDK decoder lets it through in
	// TxBody and in the messages inside it.
	nonCriticalField = 1025
)

var (
	testFrom = sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	testTo   = sdk.AccAddress(bytes.Repeat([]byte{2}, 20)).String()
)

func appendBytesField(b []byte, num protowire.Number, v []byte) []byte {
	b = protowire.AppendTag(b, num, protowire.BytesType)
	return protowire.AppendBytes(b, v)
}

func appendVarintField(b []byte, num protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(b, num, protowire.VarintType), v)
}

func anyBytes(typeURL string, value []byte) []byte {
	b := appendBytesField(nil, 1, []byte(typeURL))
	if len(value) > 0 {
		b = appendBytesField(b, 2, value)
	}
	return b
}

// coinBytes encodes a Coin; amount nil leaves the amount field out.
func coinBytes(denom string, amount []byte) []byte {
	b := appendBytesField(nil, 1, []byte(denom))
	if amount != nil {
		b = appendBytesField(b, 2, amount)
	}
	return b
}

func msgSendBytes(from, to string, coin []byte) []byte {
	b := appendBytesField(nil, 1, []byte(from))
	b = appendBytesField(b, 2, []byte(to))
	return appendBytesField(b, 3, coin)
}

func msgExecBytes(msgAnys ...[]byte) []byte {
	b := appendBytesField(nil, 1, []byte(testFrom))
	for _, a := range msgAnys {
		b = appendBytesField(b, 2, a)
	}
	return b
}

func bodyBytes(memo string, msgAnys ...[]byte) []byte {
	var b []byte
	for _, a := range msgAnys {
		b = appendBytesField(b, 1, a)
	}
	if memo != "" {
		b = appendBytesField(b, 2, []byte(memo))
	}
	return b
}

func pubKeyAny(t *testing.T) []byte {
	t.Helper()
	pk, err := codectypes.NewAnyWithValue(secp256k1.GenPrivKeyFromSecret([]byte("canonical")).PubKey())
	require.NoError(t, err)
	return anyBytes(pk.TypeUrl, pk.Value)
}

// singleModeInfo encodes ModeInfo{Single{mode}}.
func singleModeInfo(mode uint64) []byte {
	return appendBytesField(nil, 1, appendVarintField(nil, 1, mode))
}

func signerInfoBytes(pubKey, modeInfo []byte, sequence uint64) []byte {
	b := appendBytesField(nil, 1, pubKey)
	b = appendBytesField(b, 2, modeInfo)
	if sequence != 0 {
		b = appendVarintField(b, 3, sequence)
	}
	return b
}

func authInfoRaw(signerInfo, feeCoin []byte) []byte {
	fee := appendBytesField(nil, 1, feeCoin)
	fee = appendVarintField(fee, 2, 100_000)
	b := appendBytesField(nil, 1, signerInfo)
	return appendBytesField(b, 2, fee)
}

// authInfoBytes encodes an AuthInfo with one direct-mode signer whose public
// key Any is pubKey, and a fee of one coin.
func authInfoBytes(pubKey, feeCoin []byte) []byte {
	return authInfoRaw(signerInfoBytes(pubKey, singleModeInfo(uint64(signingtypes.SignMode_SIGN_MODE_DIRECT)), 1), feeCoin)
}

func txRawBytes(body, authInfo []byte) []byte {
	b := appendBytesField(nil, 1, body)
	b = appendBytesField(b, 2, authInfo)
	return appendBytesField(b, 3, bytes.Repeat([]byte{7}, 64))
}

// nestedUpgradeAny returns an Any holding a MsgSoftwareUpgrade whose plan
// holds, in the deprecated upgraded_client_state Any that the SDK does not
// unpack, another MsgSoftwareUpgrade, n times. Each level adds three messages
// to the nesting depth: the Plan, the Any and the message in it.
func nestedUpgradeAny(t *testing.T, n int) []byte {
	t.Helper()
	var inner *codectypes.Any
	for range n + 1 {
		msg := &upgradetypes.MsgSoftwareUpgrade{
			Authority: testFrom,
			Plan:      upgradetypes.Plan{Name: "v12", Height: 10, UpgradedClientState: inner}, //nolint:staticcheck // the deprecated field still decodes
		}
		bz, err := msg.Marshal()
		require.NoError(t, err)
		inner = &codectypes.Any{TypeUrl: "/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade", Value: bz}
	}
	return anyBytes(inner.TypeUrl, inner.Value)
}

// fixture holds canonical pieces of a tx shared by the tests below.
type fixture struct {
	decode, sdkDecode sdk.TxDecoder
	send, sendAny     []byte
	pubKey, feeCoin   []byte
	authInfo, body    []byte
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	msg := banktypes.NewMsgSend(sdk.MustAccAddressFromBech32(testFrom), sdk.MustAccAddressFromBech32(testTo), sdk.NewCoins(sdk.NewInt64Coin("utia", 1)))
	send, err := msg.Marshal()
	require.NoError(t, err)
	require.Equal(t, msgSendBytes(testFrom, testTo, coinBytes("utia", []byte("1"))), send)
	f := fixture{
		decode:    config.TxConfig.TxDecoder(),
		sdkDecode: authtx.DefaultTxDecoder(config.Codec),
		send:      send,
		sendAny:   anyBytes(msgSendURL, send),
		pubKey:    pubKeyAny(t),
		feeCoin:   coinBytes("utia", []byte("1000")),
	}
	f.authInfo = authInfoBytes(f.pubKey, f.feeCoin)
	f.body = bodyBytes("memo", f.sendAny)

	// The hand-built encodings match what gogoproto writes.
	var body txtypes.TxBody
	require.NoError(t, config.Codec.Unmarshal(f.body, &body))
	bz, err := body.Marshal()
	require.NoError(t, err)
	require.Equal(t, f.body, bz)
	var authInfo txtypes.AuthInfo
	require.NoError(t, config.Codec.Unmarshal(f.authInfo, &authInfo))
	bz, err = authInfo.Marshal()
	require.NoError(t, err)
	require.Equal(t, f.authInfo, bz)
	exec := authz.NewMsgExec(sdk.MustAccAddressFromBech32(testFrom), []sdk.Msg{&banktypes.MsgSend{}})
	exec.Msgs[0] = &codectypes.Any{TypeUrl: msgSendURL, Value: send}
	bz, err = exec.Marshal()
	require.NoError(t, err)
	require.Equal(t, msgExecBytes(f.sendAny), bz)
	return f
}

// TestCanonicalTxDecoderAccepts covers canonical txs and encodings that real
// clients send which differ from gogoproto's but that every decoder reads
// the same way.
func TestCanonicalTxDecoderAccepts(t *testing.T) {
	f := newFixture(t)

	remoteTransfer := func(t *testing.T) []byte {
		recipient, err := util.DecodeHexAddress("0x" + strings.Repeat("ab", 32))
		require.NoError(t, err)
		msg := &warptypes.MsgRemoteTransfer{
			Sender:            testFrom,
			TokenId:           util.CreateMockHexAddress("token", 1),
			DestinationDomain: 42161,
			Recipient:         recipient,
			Amount:            sdkmath.NewInt(1_000_000),
			GasLimit:          sdkmath.NewInt(0),
			MaxFee:            sdk.NewInt64Coin("utia", 2_000_000),
		}
		bz, err := msg.Marshal()
		require.NoError(t, err)
		lower := []byte(recipient.String())
		upper := append([]byte("0x"), bytes.ToUpper(lower[2:])...)
		require.Equal(t, 1, bytes.Count(bz, lower))
		return bytes.Replace(bz, lower, upper, 1)
	}

	testCases := []struct {
		name string
		raw  func(t *testing.T) []byte
	}{
		{
			name: "canonical tx",
			raw:  func(*testing.T) []byte { return txRawBytes(f.body, f.authInfo) },
		},
		{
			name: "canonical MsgExec",
			raw: func(*testing.T) []byte {
				return txRawBytes(bodyBytes("", anyBytes(msgExecURL, msgExecBytes(f.sendAny))), f.authInfo)
			},
		},
		{
			name: "two messages",
			raw:  func(*testing.T) []byte { return txRawBytes(bodyBytes("", f.sendAny, f.sendAny), f.authInfo) },
		},
		{
			name: "explicit empty memo",
			raw: func(*testing.T) []byte {
				return txRawBytes(appendBytesField(bodyBytes("", f.sendAny), 2, nil), f.authInfo)
			},
		},
		{
			name: "explicit zero sequence",
			raw: func(*testing.T) []byte {
				signer := signerInfoBytes(f.pubKey, singleModeInfo(uint64(signingtypes.SignMode_SIGN_MODE_DIRECT)), 0)
				signer = appendVarintField(signer, 3, 0)
				return txRawBytes(f.body, authInfoRaw(signer, f.feeCoin))
			},
		},
		{
			name: "MsgTransfer without timeout_height",
			raw: func(*testing.T) []byte {
				transfer := appendBytesField(nil, 1, []byte("transfer"))
				transfer = appendBytesField(transfer, 2, []byte("channel-2"))
				transfer = appendBytesField(transfer, 3, coinBytes("utia", []byte("1000")))
				transfer = appendBytesField(transfer, 4, []byte(testFrom))
				transfer = appendBytesField(transfer, 5, []byte("osmo1receiver"))
				transfer = appendVarintField(transfer, 7, 1_794_000_000_000_000_000)
				return txRawBytes(bodyBytes("", anyBytes("/ibc.applications.transfer.v1.MsgTransfer", transfer)), f.authInfo)
			},
		},
		{
			// The innermost Plan's Timestamp is message 98 deep, counting
			// TxBody as 1; one more level puts it at 101.
			name: "Anys nested to the depth limit",
			raw:  func(t *testing.T) []byte { return txRawBytes(bodyBytes("", nestedUpgradeAny(t, 31)), f.authInfo) },
		},
		{
			name: "uppercase hex address",
			raw: func(t *testing.T) []byte {
				return txRawBytes(bodyBytes("", anyBytes("/hyperlane.warp.v1.MsgRemoteTransfer", remoteTransfer(t))), f.authInfo)
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.decode(tc.raw(t))
			require.NoError(t, err)
		})
	}
}

// requireRejected checks that the SDK decoder alone accepts raw but the app
// decoder rejects it, directly and inside a BlobTx.
func requireRejected(t *testing.T, f fixture, raw []byte) {
	t.Helper()
	_, err := f.sdkDecode(raw)
	require.NoError(t, err)

	sdkTx, err := f.decode(raw)
	require.ErrorIs(t, err, sdkerrors.ErrTxDecode)
	require.Nil(t, sdkTx)

	blob, err := share.NewV0Blob(share.MustNewV0Namespace(bytes.Repeat([]byte{2}, 10)), []byte("data"))
	require.NoError(t, err)
	blobTx, err := squaretx.MarshalBlobTx(raw, blob)
	require.NoError(t, err)
	_, err = f.decode(blobTx)
	require.ErrorIs(t, err, sdkerrors.ErrTxDecode)
}

// TestCanonicalTxDecoderRejectsWireForms covers unknown fields, field order
// and repetition, and non-minimal or mismatched wire forms.
func TestCanonicalTxDecoderRejectsWireForms(t *testing.T) {
	f := newFixture(t)
	nonMinimalVarint := append(protowire.AppendTag(nil, 3, protowire.VarintType), 0x81, 0x00) // 1 in two bytes

	modeInfoMulti := func(depth int) []byte {
		// ModeInfo{Multi{bitarray: {}, mode_infos: [inner]}}, depth times.
		inner := singleModeInfo(uint64(signingtypes.SignMode_SIGN_MODE_DIRECT))
		for range depth {
			multi := appendBytesField(nil, 1, appendVarintField(nil, 1, 1))
			multi = appendBytesField(multi, 2, inner)
			inner = appendBytesField(nil, 2, multi)
		}
		return inner
	}

	testCases := []struct {
		name string
		raw  []byte
	}{
		{
			name: "non-minimal varint in TxBody",
			raw:  txRawBytes(append(bodyBytes("memo", f.sendAny), nonMinimalVarint...), f.authInfo),
		},
		{
			name: "non-minimal tag in TxRaw",
			raw: func() []byte {
				b := []byte{0x8a, 0x00} // tag of field 1, bytes, in two bytes
				b = protowire.AppendBytes(b, f.body)
				return append(b, txRawBytes(nil, f.authInfo)...)
			}(),
		},
		{
			name: "non-minimal length prefix in TxBody",
			raw: func() []byte {
				b := appendBytesField(nil, 1, f.sendAny)
				b = append(protowire.AppendTag(b, 2, protowire.BytesType), 0x84, 0x00)
				return txRawBytes(append(b, "memo"...), f.authInfo)
			}(),
		},
		{
			name: "duplicate memo in TxBody",
			raw:  txRawBytes(appendBytesField(bodyBytes("a", f.sendAny), 2, []byte("b")), f.authInfo),
		},
		{
			name: "out of order fields in TxBody",
			raw:  txRawBytes(appendBytesField(appendBytesField(nil, 2, []byte("memo")), 1, f.sendAny), f.authInfo),
		},
		{
			name: "unknown non-critical field in TxBody",
			raw:  txRawBytes(appendBytesField(f.body, nonCriticalField, []byte("x")), f.authInfo),
		},
		{
			name: "unknown non-critical field in a message Any",
			raw:  txRawBytes(bodyBytes("", appendBytesField(f.sendAny, nonCriticalField, []byte("x"))), f.authInfo),
		},
		{
			name: "unknown non-critical field in a message",
			raw:  txRawBytes(bodyBytes("", anyBytes(msgSendURL, appendBytesField(f.send, nonCriticalField, []byte("x")))), f.authInfo),
		},
		{
			name: "out of order fields in a message",
			raw: txRawBytes(bodyBytes("", anyBytes(msgSendURL, func() []byte {
				b := appendBytesField(nil, 2, []byte(testTo))
				b = appendBytesField(b, 1, []byte(testFrom))
				return appendBytesField(b, 3, coinBytes("utia", []byte("1")))
			}())), f.authInfo),
		},
		{
			name: "duplicate field in a message nested in MsgExec",
			raw: txRawBytes(bodyBytes("", anyBytes(msgExecURL, msgExecBytes(
				anyBytes(msgSendURL, appendBytesField(f.send, 1, []byte(testFrom))),
			))), f.authInfo),
		},
		{
			name: "unknown field in an Any nested in MsgExec",
			raw: txRawBytes(bodyBytes("", anyBytes(msgExecURL, msgExecBytes(
				appendBytesField(f.sendAny, nonCriticalField, []byte("x")),
			))), f.authInfo),
		},
		{
			// MsgSoftwareUpgrade does not unpack the deprecated
			// upgraded_client_state, so only this check reads inside it.
			name: "unknown field inside an Any the SDK does not unpack",
			raw: func() []byte {
				msg := &upgradetypes.MsgSoftwareUpgrade{
					Authority: testFrom,
					Plan: upgradetypes.Plan{
						Name:                "v12",
						Height:              10,
						UpgradedClientState: &codectypes.Any{TypeUrl: msgSendURL, Value: appendBytesField(f.send, nonCriticalField, []byte("x"))}, //nolint:staticcheck // the deprecated field still decodes
					},
				}
				bz, err := msg.Marshal()
				require.NoError(t, err)
				return txRawBytes(bodyBytes("", anyBytes("/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade", bz)), f.authInfo)
			}(),
		},
		{
			name: "duplicate field in a public key",
			raw: func() []byte {
				pk := secp256k1.GenPrivKeyFromSecret([]byte("canonical")).PubKey().Bytes()
				key := appendBytesField(appendBytesField(nil, 1, pk), 1, pk)
				return txRawBytes(f.body, authInfoBytes(anyBytes("/cosmos.crypto.secp256k1.PubKey", key), f.feeCoin))
			}(),
		},
		{
			name: "enum value beyond 32 bits",
			raw:  txRawBytes(f.body, authInfoRaw(signerInfoBytes(f.pubKey, singleModeInfo(1<<32|1), 1), f.feeCoin)),
		},
		{
			name: "two members of a oneof",
			raw: func() []byte {
				mode := singleModeInfo(1)
				mode = appendBytesField(mode, 2, appendBytesField(nil, 1, appendVarintField(nil, 1, 1)))
				return txRawBytes(f.body, authInfoRaw(signerInfoBytes(f.pubKey, mode, 1), f.feeCoin))
			}(),
		},
		{
			name: "bool value above one",
			raw: func() []byte {
				proposal := appendBytesField(nil, 1, f.sendAny)
				proposal = appendBytesField(proposal, 3, []byte(testFrom))
				proposal = appendVarintField(proposal, 7, 2)
				return txRawBytes(bodyBytes("", anyBytes("/cosmos.gov.v1.MsgSubmitProposal", proposal)), f.authInfo)
			}(),
		},
		{
			name: "unpacked repeated scalar",
			raw: func() []byte {
				pfb := appendBytesField(nil, 1, []byte(testFrom))
				pfb = appendVarintField(pfb, 3, 5)
				pfb = appendVarintField(pfb, 3, 6)
				return txRawBytes(bodyBytes("", anyBytes("/celestia.blob.v1.MsgPayForBlobs", pfb)), f.authInfo)
			}(),
		},
		{
			// Counts an Any and the message in it as two levels.
			name: "Anys nested deeper than the limit",
			raw:  txRawBytes(bodyBytes("", nestedUpgradeAny(t, 32)), f.authInfo),
		},
		{
			name: "messages nested deeper than the limit",
			raw:  txRawBytes(f.body, authInfoRaw(signerInfoBytes(f.pubKey, modeInfoMulti(60), 1), f.feeCoin)),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requireRejected(t, f, tc.raw)
		})
	}
}

// TestCanonicalTxDecoderRejectsStringsAndDecimals covers invalid UTF-8 and
// math.Int / LegacyDec values that are missing or not in decimal form.
func TestCanonicalTxDecoderRejectsStringsAndDecimals(t *testing.T) {
	f := newFixture(t)
	sendWithAmount := func(amount []byte) []byte {
		return txRawBytes(bodyBytes("", anyBytes(msgSendURL, msgSendBytes(testFrom, testTo, coinBytes("utia", amount)))), f.authInfo)
	}
	testCases := []struct {
		name string
		raw  []byte
	}{
		{"invalid UTF-8 memo", txRawBytes(bodyBytes("\xff", f.sendAny), f.authInfo)},
		{"invalid UTF-8 string in a message", txRawBytes(bodyBytes("", anyBytes(msgSendURL, msgSendBytes(testFrom, testTo+"\xff", coinBytes("utia", []byte("1"))))), f.authInfo)},
		{"invalid UTF-8 in a message nested in MsgExec", txRawBytes(bodyBytes("", anyBytes(msgExecURL, msgExecBytes(
			anyBytes(msgSendURL, msgSendBytes(testFrom, testTo, coinBytes("utia\xff", []byte("1")))),
		))), f.authInfo)},
		{"coin amount left out of a message", sendWithAmount(nil)},
		{"empty fee amount", txRawBytes(f.body, authInfoBytes(f.pubKey, coinBytes("utia", []byte{})))},
		{"fee amount with a plus sign", txRawBytes(f.body, authInfoBytes(f.pubKey, coinBytes("utia", []byte("+1000"))))},
		{"hexadecimal coin amount", sendWithAmount([]byte("0x10"))},
		{"coin amount with an underscore", sendWithAmount([]byte("1_0"))},
		{"coin amount with a leading zero", sendWithAmount([]byte("01"))},
		{"negative zero coin amount", sendWithAmount([]byte("-0"))},
		{"empty coin amount nested in MsgExec", txRawBytes(bodyBytes("", anyBytes(msgExecURL, msgExecBytes(
			anyBytes(msgSendURL, msgSendBytes(testFrom, testTo, coinBytes("utia", []byte{}))),
		))), f.authInfo)},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			requireRejected(t, f, tc.raw)
		})
	}
}

// TestCanonicalTxDecoderAcceptsEncoderOutput checks that a tx the SDK's own
// tx builder encodes, holding any registered message with every field set
// (Anys and oneofs included where a value fits), decodes to the same tx as
// under the SDK decoder alone.
func TestCanonicalTxDecoderAcceptsEncoderOutput(t *testing.T) {
	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	decode := config.TxConfig.TxDecoder()
	sdkDecode := authtx.DefaultTxDecoder(config.Codec)
	anys := candidateAnys(t)
	checked, withAny := 0, 0
	for _, url := range config.InterfaceRegistry.ListImplementations(sdk.MsgInterfaceProtoName) {
		// Try each Any value until the SDK decoder accepts the tx.
		accepted := false
		for _, anyValue := range anys {
			msg, err := config.InterfaceRegistry.Resolve(url)
			require.NoError(t, err)
			filled := fillFields(reflect.ValueOf(msg).Elem(), anyValue, 0)
			raw, ok := encodeTx(t, config, msg)
			if !ok {
				t.Logf("%s: cannot be set", url)
				break
			}
			sdkTx, sdkErr := sdkDecode(raw)
			appTx, err := decode(raw)
			if sdkErr != nil {
				require.Error(t, err, url)
				if filled {
					continue
				}
				break
			}
			require.NoError(t, err, url)
			require.Equal(t, protoTxBytes(t, sdkTx), protoTxBytes(t, appTx), url)
			checked++
			if filled {
				withAny++
			}
			accepted = true
			break
		}
		if !accepted {
			t.Logf("%s: the SDK decoder rejects every filled form", url)
		}
	}
	t.Logf("checked %d message types, %d with an Any set", checked, withAny)
	require.Greater(t, checked, 100)
	require.Greater(t, withAny, 10)
}

func encodeTx(t *testing.T, config encoding.Config, msg sdk.Msg) ([]byte, bool) {
	t.Helper()
	builder := config.TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		return nil, false
	}
	builder.SetMemo("memo")
	builder.SetGasLimit(100_000)
	builder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("utia", 1)))
	raw, err := config.TxConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return raw, true
}

func protoTxBytes(t *testing.T, tx sdk.Tx) []byte {
	t.Helper()
	protoTx, ok := tx.(interface{ GetProtoTx() *txtypes.Tx })
	require.True(t, ok)
	bz, err := protoTx.GetProtoTx().Marshal()
	require.NoError(t, err)
	return bz
}

// candidateAnys returns values for Any fields: a message, public keys, an
// authorization, a fee allowance, a proposal content, IBC client types and
// evidence.
func candidateAnys(t *testing.T) []*codectypes.Any {
	t.Helper()
	k1 := secp256k1.GenPrivKeyFromSecret([]byte("a")).PubKey()
	k2 := secp256k1.GenPrivKeyFromSecret([]byte("b")).PubKey()
	values := []gogoproto.Message{
		banktypes.NewMsgSend(sdk.MustAccAddressFromBech32(testFrom), sdk.MustAccAddressFromBech32(testTo), sdk.NewCoins(sdk.NewInt64Coin("utia", 1))),
		k1,
		kmultisig.NewLegacyAminoPubKey(1, []cryptotypes.PubKey{k1, k2}),
		authz.NewGenericAuthorization(msgSendURL),
		&feegrant.BasicAllowance{SpendLimit: sdk.NewCoins(sdk.NewInt64Coin("utia", 1))},
		&govv1beta1.TextProposal{Title: "title", Description: "description"},
		&ibctm.ClientState{ChainId: "chain"},
		&ibctm.Header{},
		&evidencetypes.Equivocation{Height: 1, ConsensusAddress: testFrom},
	}
	out := make([]*codectypes.Any, len(values))
	for i, v := range values {
		a, err := codectypes.NewAnyWithValue(v)
		require.NoError(t, err)
		out[i] = a
	}
	return out
}

var (
	intType  = reflect.TypeFor[sdkmath.Int]()
	decType  = reflect.TypeFor[sdkmath.LegacyDec]()
	timeType = reflect.TypeFor[time.Time]()
	anyType  = reflect.TypeFor[*codectypes.Any]()
)

// fillFields sets every exported field of v to a non-default value, Any
// fields to anyValue and oneofs to their first member. It reports whether it
// set an Any.
func fillFields(v reflect.Value, anyValue *codectypes.Any, depth int) bool {
	switch v.Type() {
	case intType:
		v.Set(reflect.ValueOf(sdkmath.NewInt(7)))
		return false
	case decType:
		v.Set(reflect.ValueOf(sdkmath.LegacyNewDec(7)))
		return false
	case timeType:
		v.Set(reflect.ValueOf(time.Unix(7, 0).UTC()))
		return false
	case anyType:
		if depth > 3 {
			return false
		}
		a := *anyValue
		v.Set(reflect.ValueOf(&a))
		return true
	}
	filled := false
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Array:
		for i := range v.Len() {
			filled = fillFields(v.Index(i), anyValue, depth) || filled
		}
	case reflect.Slice:
		if depth > 3 {
			return false
		}
		s := reflect.MakeSlice(v.Type(), 2, 2)
		for i := range 2 {
			filled = fillFields(s.Index(i), anyValue, depth+1) || filled
		}
		v.Set(s)
	case reflect.Pointer:
		if depth > 3 {
			return false
		}
		p := reflect.New(v.Type().Elem())
		filled = fillFields(p.Elem(), anyValue, depth+1)
		v.Set(p)
	case reflect.Struct:
		filled = fillStruct(v, anyValue, depth)
	}
	return filled
}

// fillStruct fills the exported fields of a struct, setting each oneof to
// its first member.
func fillStruct(v reflect.Value, anyValue *codectypes.Any, depth int) bool {
	var wrappers []any
	if w, ok := reflect.TypeAssert[interface{ XXX_OneofWrappers() []any }](v.Addr()); ok {
		wrappers = w.XXX_OneofWrappers()
	}
	filled := false
	for i := range v.NumField() {
		sf := v.Type().Field(i)
		if !sf.IsExported() || strings.HasPrefix(sf.Name, "XXX_") {
			continue
		}
		if sf.Type.Kind() != reflect.Interface {
			filled = fillFields(v.Field(i), anyValue, depth) || filled
			continue
		}
		for _, w := range wrappers {
			wv := reflect.New(reflect.TypeOf(w).Elem())
			if depth <= 3 && wv.Type().Implements(sf.Type) {
				filled = fillFields(wv.Elem(), anyValue, depth+1) || filled
				v.Field(i).Set(wv)
				break
			}
		}
	}
	return filled
}
