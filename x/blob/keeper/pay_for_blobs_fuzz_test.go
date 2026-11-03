package keeper_test

import (
	"bytes"
	"math/big"
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	"github.com/celestiaorg/celestia-app/v11/x/blob/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/baseapp"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

const (
	pfbHandlerMaxEdits = 8
	pfbHandlerRecord   = 8 // bytes per edit: field, blob index, then edit-specific bytes
)

// pfbHandlerSigners holds a valid signer, its upper-case form, a foreign
// prefix and an empty one.
var pfbHandlerSigners = []string{
	"celestia15drmhzw5kwgenvemy30rqqqgq52axf5wwrruf7",
	"CELESTIA15DRMHZW5KWGENVEMY30RQQQGQ52AXF5WWRRUF7",
	"cosmos1qypqxpq9qcrsszg2pvxq6rs0zqg3yyc5lzv7xu",
	"",
}

// pfbHandlerMsg builds a valid PFB over blobs of one, 600 and 4000 bytes, the
// last with share version 1.
func pfbHandlerMsg(t *testing.T) *types.MsgPayForBlobs {
	signer := pfbHandlerSigners[0]
	addr, err := sdk.AccAddressFromBech32(signer)
	require.NoError(t, err)
	blobs := make([]*share.Blob, 0, 3)
	for i, size := range []int{1, 600, 4000} {
		ns := share.MustNewV0Namespace(bytes.Repeat([]byte{byte(i + 1)}, share.NamespaceVersionZeroIDSize))
		version, signerBytes := share.ShareVersionZero, []byte(nil)
		if i == 2 {
			version, signerBytes = share.ShareVersionOne, addr.Bytes()
		}
		b, err := share.NewBlob(ns, bytes.Repeat([]byte{byte(i)}, size), version, signerBytes)
		require.NoError(t, err)
		blobs = append(blobs, b)
	}
	msg, err := types.NewMsgPayForBlobs(signer, appconsts.Version, blobs...)
	require.NoError(t, err)
	return msg
}

// editPFB applies one typed edit to msg, read from a fixed-width record.
func editPFB(msg *types.MsgPayForBlobs, r *fuzzutil.Consumer) {
	field, n := r.Byte()%6, len(msg.BlobSizes)
	i := 0
	if n > 0 {
		i = r.Intn(n)
	}
	switch field {
	case 0:
		msg.Signer = pfbHandlerSigners[r.Intn(len(pfbHandlerSigners))]
	case 1: // namespace: flip a byte, use a reserved one, or truncate
		if i >= len(msg.Namespaces) {
			return
		}
		ns := bytes.Clone(msg.Namespaces[i])
		switch r.Byte() % 3 {
		case 0:
			if len(ns) > 0 {
				ns[r.Intn(len(ns))] ^= r.Byte() | 1
			}
		case 1:
			ns = share.TxNamespace.Bytes()
		default:
			ns = ns[:r.Intn(len(ns)+1)]
		}
		msg.Namespaces[i] = ns
	case 2: // blob size: anything, including 0 and MaxUint32
		if i < n {
			msg.BlobSizes[i] = r.Uint32()
		}
	case 3: // share commitment: truncate or flip
		if i >= len(msg.ShareCommitments) {
			return
		}
		c := bytes.Clone(msg.ShareCommitments[i])
		if r.Bool() && len(c) > 0 {
			c[r.Intn(len(c))] ^= 1
		} else {
			c = c[:r.Intn(len(c)+1)]
		}
		msg.ShareCommitments[i] = c
	case 4: // share version
		if i < len(msg.ShareVersions) {
			msg.ShareVersions[i] = uint32(r.Byte() % 3)
		}
	default: // duplicate or drop a blob, in one or all of the parallel fields
		all, dup := r.Bool(), r.Bool()
		grow := func(s [][]byte) [][]byte {
			if i >= len(s) {
				return s
			}
			if dup {
				return append(s, bytes.Clone(s[i]))
			}
			return append(s[:i:i], s[i+1:]...)
		}
		if i < n {
			if dup {
				msg.BlobSizes = append(msg.BlobSizes, msg.BlobSizes[i])
			} else {
				msg.BlobSizes = append(msg.BlobSizes[:i:i], msg.BlobSizes[i+1:]...)
			}
		}
		if all {
			msg.Namespaces, msg.ShareCommitments = grow(msg.Namespaces), grow(msg.ShareCommitments)
			if i < len(msg.ShareVersions) {
				if dup {
					msg.ShareVersions = append(msg.ShareVersions, msg.ShareVersions[i])
				} else {
					msg.ShareVersions = append(msg.ShareVersions[:i:i], msg.ShareVersions[i+1:]...)
				}
			}
		}
	}
}

// modelPFBGas charges every byte of every share a blob occupies, computed
// from the share layout constants: the first sparse share also carries the
// sequence length and, for share version 1, the signer.
func modelPFBGas(msg *types.MsgPayForBlobs) *big.Int {
	shares := new(big.Int)
	for i, size := range msg.BlobSizes {
		if size == 0 {
			continue
		}
		first := share.ShareSize - share.NamespaceSize - share.ShareInfoBytes - share.SequenceLenBytes
		if msg.ShareVersions[i] == uint32(share.ShareVersionOne) {
			first -= share.SignerSize
		}
		cont := uint64(share.ShareSize - share.NamespaceSize - share.ShareInfoBytes)
		n := uint64(1)
		if uint64(size) > uint64(first) {
			n += (uint64(size) - uint64(first) + cont - 1) / cont
		}
		shares.Add(shares, new(big.Int).SetUint64(n))
	}
	return shares.Mul(shares, big.NewInt(int64(share.ShareSize)*int64(appconsts.GasPerBlobByte)))
}

// FuzzPayForBlobsHandler applies up to eight typed edits to a valid
// MsgPayForBlobs (signer, namespaces, blob sizes, share commitments and
// versions, blob count) and runs every message that passes ValidateBasic
// through the blob msg server, routed by the SDK MsgServiceRouter as
// DeliverTx does, under a fuzz-chosen gas limit. The handler must consume
// exactly the gas of a share-layout model, run out of gas exactly when that
// exceeds the limit and never panic otherwise, emit an EventPayForBlobs that
// repeats the message, and never charge more than DefaultEstimateGas.
func FuzzPayForBlobsHandler(f *testing.F) {
	f.Add(uint64(0), []byte{})
	for field := range byte(6) {
		f.Add(uint64(0), fuzzutil.Records(pfbHandlerRecord, []byte{field, 1, 1, 0, 0x40, 0, 0}))
		f.Add(uint64(500_000), fuzzutil.Records(pfbHandlerRecord, []byte{field, 2, 3, 0xff, 0xff, 0xff, 0xff}))
	}
	f.Add(uint64(30_000), fuzzutil.Records(pfbHandlerRecord, []byte{2, 0, 0, 0x10, 0, 0}))
	f.Fuzz(func(t *testing.T, gasLimit uint64, script []byte) {
		if len(script) > pfbHandlerMaxEdits*pfbHandlerRecord {
			return
		}
		fuzzPayForBlobsHandler(t, gasLimit, script)
	})
}

// fuzzPayForBlobsHandler reports whether the handler ran and succeeded.
func fuzzPayForBlobsHandler(t *testing.T, gasLimit uint64, script []byte) bool {
	msg := pfbHandlerMsg(t)
	c := fuzzutil.New(script)
	for n := 0; n < pfbHandlerMaxEdits && !c.Exhausted(); n++ {
		editPFB(msg, c.Record(pfbHandlerRecord))
	}
	if msg.ValidateBasic() != nil {
		return false
	}

	k, _, ctx := CreateKeeper(t, appconsts.Version)
	registry := codectypes.NewInterfaceRegistry()
	types.RegisterInterfaces(registry)
	router := baseapp.NewMsgServiceRouter()
	router.SetInterfaceRegistry(registry)
	types.RegisterMsgServer(router, keeper.NewMsgServerImpl(*k))
	handler := router.Handler(msg)
	require.NotNil(t, handler)

	meter := storetypes.NewInfiniteGasMeter()
	if gasLimit != 0 {
		meter = storetypes.NewGasMeter(gasLimit)
	}
	ctx = ctx.WithGasMeter(meter).WithEventManager(sdk.NewEventManager())

	want := modelPFBGas(msg)
	require.True(t, want.IsUint64(), "a validated PFB within the input bound cannot overflow")
	require.LessOrEqual(t, want.Uint64(), types.DefaultEstimateGas(msg), "the estimate is below the handler's charge")
	outOfGas := gasLimit != 0 && want.Uint64() > gasLimit

	var res *sdk.Result
	var err error
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				_, isOOG := r.(storetypes.ErrorOutOfGas)
				require.True(t, isOOG && outOfGas, "handler panicked: %v", r)
			}
		}()
		res, err = handler(ctx, msg)
	}()
	require.Equal(t, outOfGas, panicked, "handler charged %d gas under a %d limit", want, gasLimit)
	if outOfGas {
		return false
	}
	require.NoError(t, err)
	require.Equal(t, want.Uint64(), meter.GasConsumed(), "handler gas for sizes %v versions %v", msg.BlobSizes, msg.ShareVersions)

	require.NotEmpty(t, res.Events)
	parsed, err := sdk.ParseTypedEvent(res.Events[len(res.Events)-1])
	require.NoError(t, err)
	event, ok := parsed.(*types.EventPayForBlobs)
	require.True(t, ok, "last event is %T", parsed)
	require.Equal(t, msg.Signer, event.Signer)
	require.Equal(t, msg.BlobSizes, event.BlobSizes)
	require.Equal(t, msg.Namespaces, event.Namespaces)
	return true
}

// TestPayForBlobsHandlerFuzzSeeds keeps the oracle non-vacuous: the unedited
// message is handled, as is a valid edit, while an invalid edit stops at
// ValidateBasic and a tight gas limit runs out.
func TestPayForBlobsHandlerFuzzSeeds(t *testing.T) {
	require.True(t, fuzzPayForBlobsHandler(t, 0, nil), "the unedited message must be handled")
	require.True(t, fuzzPayForBlobsHandler(t, 0, fuzzutil.Records(pfbHandlerRecord, []byte{2, 0, 0, 0xff, 0xff, 0xff, 0xff})), "a MaxUint32 blob size is still handled")
	require.True(t, fuzzPayForBlobsHandler(t, 0, fuzzutil.Records(pfbHandlerRecord, []byte{0, 0, 1})), "an upper-case signer is valid")
	require.False(t, fuzzPayForBlobsHandler(t, 0, fuzzutil.Records(pfbHandlerRecord, []byte{1, 1, 1})), "a reserved namespace fails ValidateBasic")
	require.False(t, fuzzPayForBlobsHandler(t, 1000, nil), "1000 gas cannot pay for three blobs")
}
