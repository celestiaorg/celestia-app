package types

import (
	"bytes"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/pkg/consts"
)

const (
	testingKeyAcc = "test"
)

func TestMountainRange(t *testing.T) {
	type test struct {
		l, k     uint64
		expected []uint64
	}
	tests := []test{
		{
			l:        11,
			k:        4,
			expected: []uint64{4, 4, 2, 1},
		},
		{
			l:        2,
			k:        64,
			expected: []uint64{2},
		},
		{ //should this test throw an error? we
			l:        64,
			k:        8,
			expected: []uint64{8, 8, 8, 8, 8, 8, 8, 8},
		},
	}
	for _, tt := range tests {
		res := PowerOf2MountainRange(tt.l, tt.k)
		assert.Equal(t, tt.expected, res)
	}
}

func TestNextPowerOf2(t *testing.T) {
	type test struct {
		input    uint64
		expected uint64
	}
	tests := []test{
		{
			input:    2,
			expected: 2,
		},
		{
			input:    11,
			expected: 8,
		},
		{
			input:    511,
			expected: 256,
		},
		{
			input:    1,
			expected: 1,
		},
		{
			input:    0,
			expected: 0,
		},
	}
	for _, tt := range tests {
		res := nextPowerOf2(tt.input)
		assert.Equal(t, tt.expected, res)
	}
}

// TestCreateCommit only shows if something changed, it doesn't actually show
// the commit is being created correctly todo(evan): fix me.
func TestCreateCommitment(t *testing.T) {
	type test struct {
		k         uint64
		namespace []byte
		message   []byte
		expected  []byte
	}
	tests := []test{
		{
			k:         4,
			namespace: bytes.Repeat([]byte{0xFF}, 8),
			message:   bytes.Repeat([]byte{0xFF}, 11*256),
			expected:  []byte{0x1c, 0x57, 0x89, 0x2f, 0xbe, 0xbf, 0xa2, 0xa4, 0x4c, 0x41, 0x9e, 0x2d, 0x88, 0xd5, 0x87, 0xc0, 0xbd, 0x37, 0xc0, 0x85, 0xbd, 0x10, 0x3c, 0x36, 0xd9, 0xa2, 0x4d, 0x4e, 0x31, 0xa2, 0xf8, 0x4e},
		},
	}
	for _, tt := range tests {
		res, err := CreateCommitment(tt.k, tt.namespace, tt.message)
		assert.NoError(t, err)
		assert.Equal(t, tt.expected, res)
	}
}

// this test only tests for changes, it doesn't actually test that the result is valid.
// todo(evan): fixme
func TestGetCommitmentSignBytes(t *testing.T) {
	type test struct {
		msg      MsgWirePayForMessage
		expected []byte
	}
	tests := []test{
		{
			msg: MsgWirePayForMessage{
				MessageSize:        4,
				Message:            []byte{1, 2, 3, 4},
				MessageNameSpaceId: []byte{1, 2, 3, 4, 1, 2, 3, 4},
				Nonce:              1,
				Fee: &TransactionFee{
					BaseRateMax: 10000,
					TipRateMax:  1000,
				},
			},
			expected: []byte(`{"fee":{"base_rate_max":"10000","tip_rate_max":"1000"},"message_namespace_id":"AQIDBAECAwQ=","message_share_commitment":"Elh5P8yB1FeiPP0uWCkp67mqSsaVat6iwjH2vSMQJys=","message_size":"4","nonce":"1"}`),
		},
	}
	for _, tt := range tests {
		res, err := tt.msg.GetCommitmentSignBytes(SquareSize)
		assert.NoError(t, err)
		assert.Equal(t, tt.expected, res)
	}
}

func TestPadMessage(t *testing.T) {
	type test struct {
		input    []byte
		expected []byte
	}
	tests := []test{
		{
			input:    []byte{1},
			expected: append([]byte{1}, bytes.Repeat([]byte{0}, ShareSize-1)...),
		},
		{
			input:    []byte{},
			expected: []byte{},
		},
		{
			input:    bytes.Repeat([]byte{1}, ShareSize),
			expected: bytes.Repeat([]byte{1}, ShareSize),
		},
		{
			input:    bytes.Repeat([]byte{1}, (3*ShareSize)-10),
			expected: append(bytes.Repeat([]byte{1}, (3*ShareSize)-10), bytes.Repeat([]byte{0}, 10)...),
		},
	}
	for _, tt := range tests {
		res := PadMessage(tt.input)
		assert.Equal(t, tt.expected, res)
	}
}

func TestSignShareCommitments(t *testing.T) {
	type test struct {
		accName string
		msg     *MsgWirePayForMessage
	}

	kb := generateKeyring(t, "test")

	// create the first PFM for the first test
	firstPubKey, err := kb.Key("test")
	if err != nil {
		t.Error(err)
	}
	firstNs := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	firstMsg := bytes.Repeat([]byte{1}, ShareSize)
	firstPFM, err := NewMsgWirePayForMessage(
		firstNs,
		firstMsg,
		firstPubKey.GetPubKey().Bytes(),
		&TransactionFee{},
		SquareSize,
	)
	if err != nil {
		t.Error(err)
	}

	tests := []test{
		{
			accName: "test",
			msg:     firstPFM,
		},
	}

	for _, tt := range tests {
		err := tt.msg.SignShareCommitments(tt.accName, kb)
		// there should be no error
		assert.NoError(t, err)
		// the signature should exist
		assert.Equal(t, len(tt.msg.MessageShareCommitment[0].Signature), 64)
	}
}

func generateKeyring(t *testing.T, accts ...string) keyring.Keyring {
	kb := keyring.NewInMemory()

	for _, acc := range accts {
		_, _, err := kb.NewMnemonic(acc, keyring.English, "", "", hd.Secp256k1)
		if err != nil {
			t.Error(err)
		}
	}

	return kb
}

func TestMsgWirePayForMessage_ValidateBasic(t *testing.T) {
	type test struct {
		name      string
		msg       *MsgWirePayForMessage
		expectErr bool
		errStr    string
	}

	kr := newKeyring()

	// valid pfm
	validMsg := validMsgWirePayForMessage(kr)

	// pfm with bad ns id
	badIDMsg := validMsgWirePayForMessage(kr)
	badIDMsg.MessageNameSpaceId = []byte{1, 2, 3, 4, 5, 6, 7}

	// pfm that uses reserved ns id
	reservedMsg := validMsgWirePayForMessage(kr)
	reservedMsg.MessageNameSpaceId = []byte{0, 0, 0, 0, 0, 0, 0, 100}

	// pfm that has a wrong msg size
	invalidMsgSizeMsg := validMsgWirePayForMessage(kr)
	invalidMsgSizeMsg.Message = bytes.Repeat([]byte{1}, consts.ShareSize-20)

	// pfm that has a wrong msg size
	invalidDeclaredMsgSizeMsg := validMsgWirePayForMessage(kr)
	invalidDeclaredMsgSizeMsg.MessageSize = 999

	// pfm with bad sig
	badSigMsg := validMsgWirePayForMessage(kr)
	badSigMsg.MessageShareCommitment[0].Signature = []byte{1, 2, 3, 4}

	// pfm with bad commitment
	badCommitMsg := validMsgWirePayForMessage(kr)
	badCommitMsg.MessageShareCommitment[0].ShareCommitment = []byte{1, 2, 3, 4}

	tests := []test{
		{
			name: "valid msg",
			msg:  validMsg,
		},
		{
			name:      "bad ns ID",
			msg:       badIDMsg,
			expectErr: true,
			errStr:    "invalid namespace length",
		},
		{
			name:      "reserved ns id",
			msg:       reservedMsg,
			expectErr: true,
			errStr:    "uses a reserved namesapce ID",
		},
		{
			name:      "invalid msg size",
			msg:       invalidMsgSizeMsg,
			expectErr: true,
			errStr:    "Share message must be divisible",
		},
		{
			name:      "bad declared message size",
			msg:       invalidDeclaredMsgSizeMsg,
			expectErr: true,
			errStr:    "Declared Message size does not match actual Message size",
		},
		{
			name:      "bad sig",
			msg:       badSigMsg,
			expectErr: true,
			errStr:    "invalid signature for share commitment",
		},
		{
			name:      "bad commitment",
			msg:       badCommitMsg,
			expectErr: true,
			errStr:    "invalid commit for square size",
		},
	}

	for _, tt := range tests {
		err := tt.msg.ValidateBasic()
		if tt.expectErr {
			require.NotNil(t, err, tt.name)
			require.Contains(t, err.Error(), tt.errStr, tt.name)
			continue
		}
		require.NoError(t, err, tt.name)
	}
}

func validMsgWirePayForMessage(keyring keyring.Keyring) *MsgWirePayForMessage {
	info, err := keyring.Key(testingKeyAcc)
	if err != nil {
		panic(err)
	}
	msg, err := NewMsgWirePayForMessage(
		[]byte{1, 2, 3, 4, 5, 6, 7, 8},
		bytes.Repeat([]byte{1}, 1000),
		info.GetPubKey().Bytes(),
		&TransactionFee{},
		16, 32, 64,
	)
	if err != nil {
		panic(err)
	}
	err = msg.SignShareCommitments(testingKeyAcc, keyring)
	if err != nil {
		panic(err)
	}
	return msg
}

func newKeyring() keyring.Keyring {
	kb := keyring.NewInMemory()
	_, _, err := kb.NewMnemonic(testingKeyAcc, keyring.English, "", "", hd.Secp256k1)
	if err != nil {
		panic(err)
	}
	return kb
}
