package shares

import (
	"context"
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	coretypes "github.com/tendermint/tendermint/types"
)

// var defaultVoteTime = time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)

// TODO: refactor into different tests
// func TestMakeShares(t *testing.T) {
// 	reservedTxNamespaceID := append(bytes.Repeat([]byte{0}, 7), 1)
// 	reservedEvidenceNamespaceID := append(bytes.Repeat([]byte{0}, 7), 3)
// 	val := coretypes.NewMockPV()
// 	blockID := makeBlockID([]byte("blockhash"), 1000, []byte("partshash"))
// 	blockID2 := makeBlockID([]byte("blockhash2"), 1000, []byte("partshash"))
// 	vote1 := makeVote(t, val, "chainID", 0, 10, 2, 1, blockID, defaultVoteTime)
// 	vote2 := makeVote(t, val, "chainID", 0, 10, 2, 1, blockID2, defaultVoteTime)
// 	testEvidence := &coretypes.DuplicateVoteEvidence{
// 		VoteA: vote1,
// 		VoteB: vote2,
// 	}
// 	protoTestEvidence, err := coretypes.EvidenceToProto(testEvidence)
// 	if err != nil {
// 		t.Error(err)
// 	}
// 	testEvidenceBytes, err := protoio.MarshalDelimited(protoTestEvidence)
// 	largeTx := coretypes.Tx(bytes.Repeat([]byte("large Tx"), 50))
// 	largeTxLenDelimited, _ := largeTx.MarshalDelimited()
// 	smolTx := coretypes.Tx("small Tx")
// 	smolTxLenDelimited, _ := smolTx.MarshalDelimited()
// 	msg1 := coretypes.Message{
// 		NamespaceID: namespace.ID("8bytesss"),
// 		Data:        []byte("some data"),
// 	}
// 	msg1Marshaled, _ := msg1.MarshalDelimited()
// 	if err != nil {
// 		t.Fatalf("Could not encode evidence: %v, error: %v\n", testEvidence, err)
// 	}

// 	type args struct {
// 		data Splitter
// 	}
// 	tests := []struct {
// 		name string
// 		args args
// 		want NamespacedShares
// 	}{
// 		{
// 			name: "evidence",
// 			args: args{
// 				data: &coretypes.EvidenceData{
// 					Evidence: []coretypes.Evidence{testEvidence},
// 				},
// 			},
// 			want: NamespacedShares{
// 				NamespacedShare{
// 					Share: append(
// 						append(reservedEvidenceNamespaceID, byte(0)),
// 						testEvidenceBytes[:appconsts.TxShareSize]...,
// 					),
// 					ID: reservedEvidenceNamespaceID,
// 				},
// 				NamespacedShare{
// 					Share: append(
// 						append(reservedEvidenceNamespaceID, byte(0)),
// 						zeroPadIfNecessary(testEvidenceBytes[appconsts.TxShareSize:], appconsts.TxShareSize)...,
// 					),
// 					ID: reservedEvidenceNamespaceID,
// 				},
// 			},
// 		},
// 		{"small LL Tx",
// 			args{
// 				data: coretypes.Txs{smolTx},
// 			},
// 			NamespacedShares{
// 				NamespacedShare{
// 					Share: append(
// 						append(reservedTxNamespaceID, byte(0)),
// 						zeroPadIfNecessary(smolTxLenDelimited, appconsts.TxShareSize)...,
// 					),
// 					ID: reservedTxNamespaceID,
// 				},
// 			},
// 		},
// 		{"one large LL Tx",
// 			args{
// 				data: coretypes.Txs{largeTx},
// 			},
// 			NamespacedShares{
// 				NamespacedShare{
// 					Share: append(
// 						append(reservedTxNamespaceID, byte(0)),
// 						largeTxLenDelimited[:appconsts.TxShareSize]...,
// 					),
// 					ID: reservedTxNamespaceID,
// 				},
// 				NamespacedShare{
// 					Share: append(
// 						append(reservedTxNamespaceID, byte(0)),
// 						zeroPadIfNecessary(largeTxLenDelimited[appconsts.TxShareSize:], appconsts.TxShareSize)...,
// 					),
// 					ID: reservedTxNamespaceID,
// 				},
// 			},
// 		},
// 		{"large then small LL Tx",
// 			args{
// 				data: coretypes.Txs{largeTx, smolTx},
// 			},
// 			NamespacedShares{
// 				NamespacedShare{
// 					Share: append(
// 						append(reservedTxNamespaceID, byte(0)),
// 						largeTxLenDelimited[:appconsts.TxShareSize]...,
// 					),
// 					ID: reservedTxNamespaceID,
// 				},
// 				NamespacedShare{
// 					Share: append(
// 						append(
// 							reservedTxNamespaceID,
// 							byte(0),
// 						),
// 						zeroPadIfNecessary(
// 							append(largeTxLenDelimited[appconsts.TxShareSize:], smolTxLenDelimited...),
// 							appconsts.TxShareSize,
// 						)...,
// 					),
// 					ID: reservedTxNamespaceID,
// 				},
// 			},
// 		},
// 		{"ll-app message",
// 			args{
// 				data: coretypes.Messages{[]coretypes.Message{msg1}},
// 			},
// 			NamespacedShares{
// 				NamespacedShare{
// 					Share: append(
// 						[]byte(msg1.NamespaceID),
// 						zeroPadIfNecessary(msg1Marshaled, appconsts.MsgShareSize)...,
// 					),
// 					ID: msg1.NamespaceID,
// 				},
// 			},
// 		},
// 	}
// 	for i, tt := range tests {
// 		tt := tt // stupid scopelint :-/
// 		i := i
// 		t.Run(tt.name, func(t *testing.T) {
// 			got := tt.args.data.SplitIntoShares()
// 			if !reflect.DeepEqual(got, tt.want) {
// 				t.Errorf("%v: makeShares() = \n%+v\nwant\n%+v\n", i, got, tt.want)
// 			}
// 		})
// 	}
// }

func Test_zeroPadIfNecessary(t *testing.T) {
	type args struct {
		share []byte
		width int
	}
	tests := []struct {
		name string
		args args
		want []byte
	}{
		{"pad", args{[]byte{1, 2, 3}, 6}, []byte{1, 2, 3, 0, 0, 0}},
		{"not necessary (equal to shareSize)", args{[]byte{1, 2, 3}, 3}, []byte{1, 2, 3}},
		{"not necessary (greater shareSize)", args{[]byte{1, 2, 3}, 2}, []byte{1, 2, 3}},
	}
	for _, tt := range tests {
		tt := tt // stupid scopelint :-/
		t.Run(tt.name, func(t *testing.T) {
			if got := zeroPadIfNecessary(tt.args.share, tt.args.width); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("zeroPadIfNecessary() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	type test struct {
		name     string
		txCount  int
		evdCount int
		msgCount int
		maxSize  int // max size of each tx or msg
	}

	tests := []test{
		{"one of each random small size", 1, 1, 1, 40},
		{"one of each random large size", 1, 1, 1, 400},
		{"many of each random large size", 10, 10, 10, 40},
		{"many of each random large size", 10, 10, 10, 400},
		{"only transactions", 10, 0, 0, 400},
		{"only evidence", 0, 10, 0, 400},
		{"only messages", 0, 0, 10, 400},
	}

	for _, tc := range tests {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			// generate random data
			data := generateRandomBlockData(
				tc.txCount,
				tc.evdCount,
				tc.msgCount,
				tc.maxSize,
			)
			data.OriginalSquareSize = appconsts.MaxSquareSize

			rawShares, err := Split(data)
			require.NoError(t, err)

			eds, err := rsmt2d.ComputeExtendedDataSquare(rawShares, appconsts.DefaultCodec(), rsmt2d.NewDefaultTree)
			if err != nil {
				t.Error(err)
			}

			res, err := Merge(eds)
			if err != nil {
				t.Fatal(err)
			}

			// we have to compare the evidence by string because the the
			// timestamps differ not by actual time represented, but by
			// internals see https://github.com/stretchr/testify/issues/666
			for i := 0; i < len(data.Evidence.Evidence); i++ {
				inputEvidence := data.Evidence.Evidence[i].(*coretypes.DuplicateVoteEvidence)
				resultEvidence := res.Evidence.Evidence[i].(*coretypes.DuplicateVoteEvidence)
				assert.Equal(t, inputEvidence.String(), resultEvidence.String())
			}

			// compare the original to the result w/o the evidence
			data.Evidence = coretypes.EvidenceData{}
			res.Evidence = coretypes.EvidenceData{}

			res.OriginalSquareSize = data.OriginalSquareSize

			assert.Equal(t, data, res)
		})
	}
}

func TestFuzz_Merge(t *testing.T) {
	t.Skip()
	// run random shares through processCompactShares for a minute
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			TestMerge(t)
		}
	}
}

// generateRandomBlockData returns randomly generated block data for testing purposes
func generateRandomBlockData(txCount, evdCount, msgCount, maxSize int) coretypes.Data {
	var out coretypes.Data
	out.Txs = generateRandomlySizedCompactShares(txCount, maxSize)
	out.Evidence = generateIdenticalEvidence(evdCount)
	out.Messages = generateRandomlySizedMessages(msgCount, maxSize)
	return out
}

func generateRandomlySizedCompactShares(count, max int) coretypes.Txs {
	txs := make(coretypes.Txs, count)
	for i := 0; i < count; i++ {
		size := rand.Intn(max)
		if size == 0 {
			size = 1
		}
		txs[i] = generateRandomCompactShares(1, size)[0]
	}
	return txs
}

func generateRandomCompactShares(count, size int) coretypes.Txs {
	txs := make(coretypes.Txs, count)
	for i := 0; i < count; i++ {
		tx := make([]byte, size)
		_, err := rand.Read(tx)
		if err != nil {
			panic(err)
		}
		txs[i] = tx
	}
	return txs
}

func generateIdenticalEvidence(count int) coretypes.EvidenceData {
	evidence := make([]coretypes.Evidence, count)
	for i := 0; i < count; i++ {
		ev := coretypes.NewMockDuplicateVoteEvidence(math.MaxInt64, time.Now(), "chainID")
		evidence[i] = ev
	}
	return coretypes.EvidenceData{Evidence: evidence}
}

func generateRandomlySizedMessages(count, maxMsgSize int) coretypes.Messages {
	msgs := make([]coretypes.Message, count)
	for i := 0; i < count; i++ {
		msgs[i] = generateRandomMessage(rand.Intn(maxMsgSize))
		if len(msgs[i].Data) == 0 {
			i--
		}
	}

	// this is just to let us use assert.Equal
	if count == 0 {
		msgs = nil
	}

	messages := coretypes.Messages{MessagesList: msgs}
	messages.SortMessages()
	return messages
}

func generateRandomMessage(size int) coretypes.Message {
	msg := coretypes.Message{
		NamespaceID: tmrand.Bytes(appconsts.NamespaceSize),
		Data:        tmrand.Bytes(size),
	}
	return msg
}
