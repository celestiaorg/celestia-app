package fibre_test

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/fibre/validator"
	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d"
	"github.com/celestiaorg/celestia-app/v11/pkg/rsema1d/rlc"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/cometbft/cometbft/crypto"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	txsigning "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// uploadOpWidth is the fixed width of one upload mutation record: an op byte,
// a uint16 index, a uint32 argument and one extra byte.
const uploadOpWidth = 8

// Upload mutation ops. Each edits the UploadShardRequest a fibre client sends.
const (
	opRowIndex = iota
	opRowForeign
	opRowDataFlip
	opRowDataResize
	opProofFlip
	opProofShape
	opRowsShape
	opRLCFlip
	opRLCResize
	opUploadSize
	opBlobVersion
	opHeight
	opCommitmentFlip
	opChainID
	opTimestamp
	opNamespaceFlip
	opSignerKeyFlip
	opSignatureFlip
	opDropField
	opPreUpload
	numUploadOps
)

// uploadFixture holds a valid upload for the server's validator, built once per
// fuzz worker.
type uploadFixture struct {
	server   *fibre.Server
	valSet   validator.Set
	us       *core.Validator
	usPubKey crypto.PubKey
	keyring  keyring.Keyring
	signer   *secp256k1.PubKey
	blobCfg  fibre.BlobConfig
	id       fibre.BlobID
	promise  func(ts time.Time) *fibre.PaymentPromise // a fresh, unsigned promise
	rows     []*types.BlobRow                         // rows assigned to the server's validator
	foreign  []*types.BlobRow                         // rows assigned to another validator
	rlcs     []byte
	verifier *rsema1d.Verifier
}

func newUploadFixture(tb testing.TB) *uploadFixture {
	// Discard logs: the server logs every rejected upload.
	server, valSet, us := makeTestServerWithConfig(tb, func(cfg *fibre.ServerConfig) {
		cfg.Log = slog.New(slog.DiscardHandler)
	})
	blob := makeTestBlobV0(tb, 256*1024)
	kr := makeTestKeyring(tb)
	key, err := kr.Key(fibre.DefaultKeyName)
	require.NoError(tb, err)
	pk, err := key.GetPubKey()
	require.NoError(tb, err)

	cfg := fibre.DefaultServerConfig()
	blobCfg := blob.Config()
	shardMap := valSet.Assign(blob.ID().Commitment(), blobCfg.TotalRows(), blobCfg.OriginalRows, cfg.MinRowsPerValidator, cfg.LivenessThreshold)
	rowsOf := func(indices []int) []*types.BlobRow {
		var rows []*types.BlobRow
		require.NoError(tb, blob.RowProofs(indices, func(index int, row []byte, proof [][]byte) {
			rows = append(rows, &types.BlobRow{Index: uint32(index), Data: row, Proof: proof})
		}))
		return rows
	}
	f := &uploadFixture{
		server:   server,
		valSet:   valSet,
		us:       us,
		usPubKey: us.PubKey,
		keyring:  kr,
		signer:   pk.(*secp256k1.PubKey),
		blobCfg:  blobCfg,
		id:       blob.ID(),
		rows:     rowsOf(shardMap[us]),
		rlcs:     rlc.Marshal(blob.RLC()),
		promise: func(ts time.Time) *fibre.PaymentPromise {
			return &fibre.PaymentPromise{
				ChainID:           "celestia",
				Height:            100,
				Namespace:         testNamespace,
				UploadSize:        uint32(blob.UploadSize()),
				BlobVersion:       uint32(blob.ID().Version()),
				Commitment:        blob.ID().Commitment(),
				CreationTimestamp: ts,
				SignerKey:         pk.(*secp256k1.PubKey),
			}
		},
	}
	require.NotEmpty(tb, f.rows)
	for val, indices := range shardMap {
		if !bytes.Equal(val.Address, us.Address) && len(indices) > 0 {
			f.foreign = rowsOf(indices[:min(len(indices), 4)])
			break
		}
	}
	require.NotEmpty(tb, f.foreign)
	f.verifier, err = rsema1d.NewVerifier(&rsema1d.Config{K: blobCfg.OriginalRows, N: blobCfg.ParityRows, WorkerCount: 1})
	require.NoError(tb, err)
	return f
}

// sign sets the client signature over the promise's sign bytes. It leaves the
// promise unsigned when the promise cannot be converted, which the server must
// reject anyway.
func (f *uploadFixture) sign(tb testing.TB, pb *types.PaymentPromise) {
	var p fibre.PaymentPromise
	if p.FromProto(pb) != nil {
		return
	}
	signBytes, err := p.SignBytes()
	if err != nil {
		return
	}
	sig, _, err := f.keyring.Sign(fibre.DefaultKeyName, signBytes, txsigning.SignMode_SIGN_MODE_DIRECT)
	require.NoError(tb, err)
	pb.Signature = sig
}

// request returns a valid, signed request whose promise is created at ts.
func (f *uploadFixture) request(tb testing.TB, ts time.Time) *types.UploadShardRequest {
	pb, err := f.promise(ts).ToProto()
	require.NoError(tb, err)
	// ToProto can alias the fixture's namespace, commitment and key; clone them
	// so in-place edits cannot leak into later inputs.
	pb.Namespace = bytes.Clone(pb.Namespace)
	pb.Commitment = bytes.Clone(pb.Commitment)
	pb.SignerPublicKey.Key = bytes.Clone(pb.SignerPublicKey.Key)
	f.sign(tb, pb)
	shard := &types.BlobShard{Rlcs: bytes.Clone(f.rlcs)}
	for _, r := range f.rows {
		shard.Rows = append(shard.Rows, cloneRow(r))
	}
	return &types.UploadShardRequest{Promise: pb, Shard: shard}
}

func cloneRow(r *types.BlobRow) *types.BlobRow {
	proof := make([][]byte, len(r.Proof))
	for i, p := range r.Proof {
		proof[i] = bytes.Clone(p)
	}
	return &types.BlobRow{Index: r.Index, Data: bytes.Clone(r.Data), Proof: proof}
}

// flip XORs a non-zero mask into b at i, when b is non-empty.
func flip(b []byte, i int, mask byte) {
	if len(b) > 0 {
		b[i%len(b)] ^= mask | 1
	}
}

// resize truncates b or extends it with copies of fill, by up to 255 bytes.
func resize(b []byte, arg uint32, fill byte) []byte {
	if arg&1 == 0 {
		return b[:int(arg>>1)%(len(b)+1)]
	}
	return append(b, bytes.Repeat([]byte{fill}, int(arg>>1)%256)...)
}

// mutate applies one upload op to req and reports whether the promise should
// be re-signed and whether the clean request should be uploaded first.
func (f *uploadFixture) mutate(req *types.UploadShardRequest, op byte, idx int, arg uint32, extra byte) (resign, preUpload bool) {
	resign = true
	pb, shard := req.Promise, req.Shard
	var row *types.BlobRow
	if shard != nil && len(shard.Rows) > 0 {
		row = shard.Rows[idx%len(shard.Rows)]
	}
	switch op % numUploadOps {
	case opRowIndex:
		if row != nil {
			row.Index = arg
		}
	case opRowForeign:
		if row != nil {
			*row = *cloneRow(f.foreign[int(arg)%len(f.foreign)])
		}
	case opRowDataFlip:
		if row != nil {
			flip(row.Data, int(arg), extra)
		}
	case opRowDataResize:
		if row != nil {
			row.Data = resize(row.Data, arg, extra)
		}
	case opProofFlip:
		if row != nil && len(row.Proof) > 0 {
			flip(row.Proof[int(arg>>8)%len(row.Proof)], int(arg), extra)
		}
	case opProofShape:
		if row == nil {
			break
		}
		switch extra % 4 {
		case 0: // drop a segment
			if len(row.Proof) > 0 {
				i := int(arg) % len(row.Proof)
				row.Proof = append(row.Proof[:i], row.Proof[i+1:]...)
			}
		case 1: // append a segment of arbitrary length
			row.Proof = append(row.Proof, bytes.Repeat([]byte{byte(arg)}, int(arg>>8)%65))
		case 2: // resize a segment
			if len(row.Proof) > 0 {
				i := int(arg>>16) % len(row.Proof)
				row.Proof[i] = resize(row.Proof[i], arg&0xffff, extra)
			}
		case 3: // drop the whole proof
			row.Proof = nil
		}
	case opRowsShape:
		if shard == nil || len(shard.Rows) == 0 {
			break
		}
		i, j := idx%len(shard.Rows), int(arg)%len(shard.Rows)
		switch extra % 4 {
		case 0:
			shard.Rows = append(shard.Rows[:i], shard.Rows[i+1:]...)
		case 1:
			shard.Rows = append(shard.Rows, cloneRow(shard.Rows[i]))
		case 2:
			shard.Rows[i], shard.Rows[j] = shard.Rows[j], shard.Rows[i]
		case 3:
			shard.Rows = shard.Rows[:i]
		}
	case opRLCFlip:
		if shard != nil {
			flip(shard.Rlcs, int(arg), extra)
		}
	case opRLCResize:
		if shard != nil {
			shard.Rlcs = resize(shard.Rlcs, arg, extra)
		}
	case opUploadSize:
		if pb != nil {
			pb.BlobSize = arg
		}
	case opBlobVersion:
		if pb != nil {
			pb.BlobVersion = arg
		}
	case opHeight:
		if pb != nil {
			pb.Height = int64(int32(arg))
		}
	case opCommitmentFlip:
		if pb != nil {
			pb.Commitment = resize(pb.Commitment, arg&0xff, extra)
			flip(pb.Commitment, int(arg>>8), extra)
		}
	case opChainID:
		if pb != nil {
			pb.ChainId = []string{"", "celestia ", "Celestia", "celestia\x00", string(bytes.Repeat([]byte{'c'}, 64))}[arg%5]
		}
	case opTimestamp:
		if pb != nil {
			pb.CreationTimestamp = pb.CreationTimestamp.Add(time.Duration(int32(arg)) * time.Second)
		}
	case opNamespaceFlip:
		if pb != nil {
			pb.Namespace = resize(pb.Namespace, arg&0xff, extra)
			flip(pb.Namespace, int(arg>>8), extra)
		}
	case opSignerKeyFlip:
		if pb != nil {
			pb.SignerPublicKey.Key = bytes.Clone(pb.SignerPublicKey.Key)
			flip(pb.SignerPublicKey.Key, int(arg), extra)
		}
	case opSignatureFlip:
		resign = false
		if pb != nil {
			flip(pb.Signature, int(arg), extra)
		}
	case opDropField:
		switch arg % 4 {
		case 0:
			req.Promise = nil
		case 1:
			req.Shard = nil
		case 2:
			if shard != nil {
				shard.Rows = nil
			}
		case 3:
			if pb != nil {
				pb.Signature = nil
			}
			resign = false
		}
	case opPreUpload:
		preUpload = true
	}
	return resign, preUpload
}

// FuzzServerUploadShard drives Server.UploadShard with requests derived from a
// valid upload by typed edits to the payment promise, rows, proofs and RLCs.
// The promise is re-signed after edits unless an op corrupts the signature, so
// edits reach shard verification rather than stopping at signature checks.
//
// The oracle: UploadShard never panics and rejects with InvalidArgument; when
// it accepts, the request independently verifies (client signature, row
// assignment, sizes and rsema1d proofs against the commitment) unless the same
// promise was already stored, the returned validator signature verifies, and
// DownloadShard serves a shard that verifies against the commitment.
func FuzzServerUploadShard(f *testing.F) {
	op := func(o int, idx uint16, arg uint32, extra byte) []byte {
		return []byte{byte(o), byte(idx >> 8), byte(idx), byte(arg >> 24), byte(arg >> 16), byte(arg >> 8), byte(arg), extra}
	}
	f.Add([]byte{})
	f.Add(fuzzutil.Records(uploadOpWidth, op(opPreUpload, 0, 0, 0)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opPreUpload, 0, 0, 0), op(opRowDataFlip, 0, 3, 1)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opRowIndex, 0, 1<<31, 0)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opRowForeign, 0, 0, 0)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opRowDataResize, 1, 3, 7)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opProofFlip, 0, 0x0102, 9)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opProofShape, 0, 0x2000, 1)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opProofShape, 0, 0x00010041, 2)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opRowsShape, 0, 1, 1)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opRowsShape, 0, 1, 2)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opRLCFlip, 0, 5, 1)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opRLCResize, 0, 32, 0)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opUploadSize, 0, 1<<20, 0)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opBlobVersion, 0, 1, 0)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opHeight, 0, 0xffffffff, 0)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opCommitmentFlip, 0, 0x0100, 1)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opChainID, 0, 3, 0)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opTimestamp, 0, 7200, 0)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opNamespaceFlip, 0, 0x0300, 4)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opSignerKeyFlip, 0, 2, 1)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opSignatureFlip, 0, 9, 1)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opDropField, 0, 1, 0)))
	f.Add(fuzzutil.Records(uploadOpWidth, op(opPreUpload, 0, 0, 0), op(opDropField, 0, 1, 0)))

	fx := newUploadFixture(f)
	f.Fuzz(func(t *testing.T, ops []byte) {
		// A fresh creation time per input gives each input its own promise
		// hash, so earlier inputs' stored shards cannot change this outcome.
		ts := time.Now()
		req := fx.request(t, ts)
		resign, preUpload := true, false
		c := fuzzutil.New(ops)
		for n := 0; n < 32 && !c.Exhausted(); n++ {
			r := c.Record(uploadOpWidth)
			rs, pu := fx.mutate(req, r.Byte(), int(r.Uint16()), r.Uint32(), r.Byte())
			resign = resign && rs
			preUpload = preUpload || pu
		}
		if resign && req.Promise != nil {
			fx.sign(t, req.Promise)
		}

		var preHash []byte
		if preUpload {
			clean := fx.request(t, ts)
			_, err := fx.server.UploadShard(t.Context(), clean)
			require.NoError(t, err, "valid upload rejected")
			var p fibre.PaymentPromise
			require.NoError(t, p.FromProto(clean.Promise))
			preHash, err = p.Hash()
			require.NoError(t, err)
		}

		// Send the request through the wire encoding, so the handler only sees
		// requests a peer can produce.
		wire, err := proto.Marshal(req)
		require.NoError(t, err)
		var decoded types.UploadShardRequest
		require.NoError(t, proto.Unmarshal(wire, &decoded))

		resp, err := fx.server.UploadShard(t.Context(), &decoded)
		if len(ops) == 0 {
			require.NoError(t, err, "valid upload rejected")
		}
		if err != nil {
			require.Equal(t, codes.InvalidArgument, status.Code(err), "unexpected rejection: %v", err)
			return
		}
		fx.checkAccepted(t, &decoded, resp, preHash)
	})
}

// checkAccepted verifies an accepted upload independently of the server.
func (f *uploadFixture) checkAccepted(t *testing.T, req *types.UploadShardRequest, resp *types.UploadShardResponse, preHash []byte) {
	require.NotNil(t, req.Promise)
	require.NoError(t, req.Promise.ValidateBasic())
	var p fibre.PaymentPromise
	require.NoError(t, p.FromProto(req.Promise))
	require.Equal(t, "celestia", p.ChainID)
	require.Zero(t, p.BlobVersion)
	signBytes, err := p.SignBytes()
	require.NoError(t, err)
	require.True(t, p.SignerKey.VerifySignature(signBytes, p.Signature), "accepted a promise with an invalid client signature")
	require.True(t, f.usPubKey.VerifySignature(signBytes, resp.ValidatorSignature), "validator signature does not verify")

	hash, err := p.Hash()
	require.NoError(t, err)
	if !bytes.Equal(hash, preHash) {
		// Not a duplicate: the shard in this request must verify on its own.
		shard := req.Shard
		require.NotNil(t, shard)
		require.NotEmpty(t, shard.Rows)
		rowSize := len(shard.Rows[0].Data)
		require.Equal(t, rowSize*f.blobCfg.OriginalRows, int(p.UploadSize))
		f.requireVerifies(t, p.Commitment, shard)
	}

	// Only verified shards are stored, so a stored shard must verify.
	got, err := f.server.DownloadShard(t.Context(), &types.DownloadShardRequest{BlobId: fibre.NewBlobID(0, p.Commitment)})
	require.NoError(t, err)
	f.requireVerifies(t, p.Commitment, got.Shard)
}

// requireVerifies checks that every row is assigned to the server's validator
// and that the rows and RLCs verify against the commitment.
func (f *uploadFixture) requireVerifies(t *testing.T, commitment rsema1d.Commitment, shard *types.BlobShard) {
	cfg := fibre.DefaultServerConfig()
	indices := make([]uint32, len(shard.Rows))
	proofs := make([]*rsema1d.RowProof, len(shard.Rows))
	for i, r := range shard.Rows {
		indices[i] = r.Index
		proofs[i] = &rsema1d.RowProof{Index: int(r.Index), Row: r.Data, RowProof: r.Proof}
	}
	shardMap := f.valSet.Assign(commitment, f.blobCfg.TotalRows(), f.blobCfg.OriginalRows, cfg.MinRowsPerValidator, cfg.LivenessThreshold)
	require.NoError(t, shardMap.Verify(f.us, indices), "rows not assigned to this validator")
	rlcs, err := rlc.Unmarshal(shard.Rlcs)
	require.NoError(t, err)
	require.NoError(t, f.verifier.Verify(commitment, proofs, rlcs), "shard does not verify against the commitment")
}

// FuzzServerDownloadShard drives Server.DownloadShard with arbitrary blob IDs
// against a server that stores one shard. It must never panic, must only
// return that shard for its own blob ID, and must reject anything else with
// InvalidArgument or NotFound.
func FuzzServerDownloadShard(f *testing.F) {
	fx := newUploadFixture(f)
	_, err := fx.server.UploadShard(f.Context(), fx.request(f, time.Now()))
	require.NoError(f, err)
	f.Add([]byte(fx.id))
	f.Add([]byte{})
	f.Add([]byte(fx.id)[:len(fx.id)-1])
	f.Add(append([]byte(fx.id), 0))
	f.Add(append([]byte{1}, []byte(fx.id)[1:]...))

	f.Fuzz(func(t *testing.T, id []byte) {
		resp, err := fx.server.DownloadShard(t.Context(), &types.DownloadShardRequest{BlobId: id})
		if err != nil {
			code := status.Code(err)
			require.Contains(t, []codes.Code{codes.InvalidArgument, codes.NotFound}, code, "unexpected error: %v", err)
			return
		}
		var got fibre.BlobID
		require.NoError(t, got.UnmarshalBinary(id))
		require.Equal(t, fx.id.Commitment(), got.Commitment(), "served a shard for a different commitment")
		require.Zero(t, got.Version())
		fx.requireVerifies(t, fx.id.Commitment(), resp.Shard)
	})
}
