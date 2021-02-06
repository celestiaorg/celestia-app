package app

import (
	"os"
	"testing"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/lazyledger/lazyledger-app/x/lazyledgerapp/types"
	abci "github.com/lazyledger/lazyledger-core/abci/types"
	"github.com/lazyledger/lazyledger-core/libs/log"
	"github.com/spf13/cast"
	"github.com/stretchr/testify/assert"
	dbm "github.com/tendermint/tm-db"
)

// this test doesn't currently test anything, but it's a good starting template
func TestPreProcessTxs(t *testing.T) {
	testApp := setupApp()
	basicTx := &types.MsgWirePayForMessage{
		MessageSize:        4,
		Message:            []byte{1, 2, 3, 4},
		MessageNameSpaceId: []byte{1, 2, 3, 4},
		From:               "cosmos1tg66f06v0jh42g5wxnht9a5fqqj0lu78n37ss5",
	}
	rawBasicTx, err := testApp.txEncoder(txTest{[]sdk.Msg{basicTx}})
	if err != nil {
		t.Error(err)
	}
	tests := []struct {
		name string
		args abci.RequestPreprocessTxs
		want abci.ResponsePreprocessTxs
	}{
		{
			name: "basic",
			args: abci.RequestPreprocessTxs{
				Txs: [][]byte{rawBasicTx},
			},
		},
	}
	for _, tt := range tests {
		result := testApp.PreprocessTxs(tt.args)
		assert.Equal(t, tt.want, result, tt.name)
	}
}

func generateTxs() [][]byte {
	return [][]byte{}
}

func setupApp() *App {
	// var cache sdk.MultiStorePersistentCache
	// EmptyAppOptions is a stub implementing AppOptions
	emptyOpts := emptyAppOptions{}
	db := dbm.NewMemDB()
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stdout))

	skipUpgradeHeights := make(map[int64]bool)

	return New(
		"test-app", logger, db, nil, true, skipUpgradeHeights,
		cast.ToString(emptyOpts.Get(flags.FlagHome)),
		cast.ToUint(emptyOpts.Get(server.FlagInvCheckPeriod)),
		MakeEncodingConfig(), // Ideally, we would reuse the one created by NewRootCmd.
		emptyOpts,
	)
}

type emptyAppOptions struct{}

// Get implements AppOptions
func (ao emptyAppOptions) Get(o string) interface{} {
	return nil
}

/////////////////////////////
//	Generate Txs
/////////////////////////////

func generateRawTxs(t *testing.T, count int, txEncoder sdk.TxEncoder) [][]byte {
	output := make([][]byte, count)
	for i := 0; i < count; i++ {
		// create the tx
		tx := newTxCounter(int64(i), int64(i))
		// encode the tx
		raw, err := txEncoder(tx)
		if err != nil {
			t.Error(err)
		}
		output[i] = raw
	}
	return output
}

// Simple tx with a list of Msgs.
type txTest struct {
	Msgs []sdk.Msg
}

// Implements Tx
func (tx txTest) GetMsgs() []sdk.Msg   { return tx.Msgs }
func (tx txTest) ValidateBasic() error { return nil }

// ValidateBasic() fails on negative counters.
// Otherwise it's up to the handlers
type msgCounter struct {
	Counter       int64
	FailOnHandler bool
}

// dummy implementation of proto.Message
func (msg msgCounter) Reset()         {}
func (msg msgCounter) String() string { return "TODO" }
func (msg msgCounter) ProtoMessage()  {}

// Implements Msg
func (msg msgCounter) Route() string                { return "lazyledgerapp" }
func (msg msgCounter) Type() string                 { return types.TypeMsgPayforMessage }
func (msg msgCounter) GetSignBytes() []byte         { return nil }
func (msg msgCounter) GetSigners() []sdk.AccAddress { return nil }
func (msg msgCounter) ValidateBasic() error {
	if msg.Counter >= 0 {
		return nil
	}
	return sdkerrors.Wrap(sdkerrors.ErrInvalidSequence, "counter should be a non-negative integer")
}

func newTxCounter(counter int64, msgCounters ...int64) *txTest {
	msgs := make([]sdk.Msg, 0, len(msgCounters))
	for _, c := range msgCounters {
		msgs = append(msgs, msgCounter{c, false})
	}

	return &txTest{msgs}
}
