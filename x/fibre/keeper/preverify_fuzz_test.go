package keeper_test

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"
)

const (
	// maxPreverifyMutations bounds the edits applied to one batch so each
	// input does a bounded amount of work.
	maxPreverifyMutations = 8
	pffRecord             = 8 // bytes per edit: tx index, op, flags, then op-specific bytes
	pffOps                = 10
	pffAccountNumber      = 7

	pffReplace    = 1 << 0 // replace the source tx instead of appending the edited copy
	pffResign     = 1 << 1 // re-sign the promise and the tx after the edit
	pffResignCert = 1 << 2 // re-sign the validator certificate after the edit
	pffOtherKey   = 1 << 3 // re-sign the promise and the tx with another signer's key
)

var pffSignModes = []signing.SignMode{
	signing.SignMode_SIGN_MODE_DIRECT, signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
	signing.SignMode_SIGN_MODE_TEXTUAL, signing.SignMode_SIGN_MODE_UNSPECIFIED, signing.SignMode(999),
}

// pffMutator edits pay-for-fibre txs signed by the fixture's signer or by a
// second signer whose key the fixture's accounts never registered.
type pffMutator struct {
	f       *preverifyFixture
	signers []*secp256k1.PrivKey
}

// pffTx is a pay-for-fibre tx taken apart into the fields an edit changes.
type pffTx struct {
	raw  cosmostx.TxRaw
	body cosmostx.TxBody
	auth cosmostx.AuthInfo
	msg  types.MsgPayForFibre
}

// signedTx wraps msg in a tx whose envelope signer 0 signs in direct mode, so
// the envelope fields an edit changes are real ones.
func (m *pffMutator) signedTx(t *testing.T, msg *types.MsgPayForFibre) []byte {
	return m.signedTxBy(t, msg, 0)
}

// signedTxBy is signedTx with the envelope signed by signer key.
func (m *pffMutator) signedTxBy(t *testing.T, msg *types.MsgPayForFibre, key int) []byte {
	t.Helper()
	pubKey, err := codectypes.NewAnyWithValue(m.signers[key].PubKey())
	require.NoError(t, err)
	p := &pffTx{msg: *msg, auth: cosmostx.AuthInfo{Fee: &cosmostx.Fee{GasLimit: 10_000_000}, SignerInfos: []*cosmostx.SignerInfo{{
		PublicKey: pubKey,
		ModeInfo:  &cosmostx.ModeInfo{Sum: &cosmostx.ModeInfo_Single_{Single: &cosmostx.ModeInfo_Single{Mode: signing.SignMode_SIGN_MODE_DIRECT}}},
	}}}}
	raw, ok := m.encode(p, false, true, false, key, pffAccountNumber)
	require.True(t, ok)
	return raw
}

func decodePFF(raw []byte) (*pffTx, bool) {
	p := &pffTx{}
	if p.raw.Unmarshal(raw) != nil || p.body.Unmarshal(p.raw.BodyBytes) != nil || p.auth.Unmarshal(p.raw.AuthInfoBytes) != nil {
		return nil, false
	}
	if len(p.body.Messages) != 1 || p.msg.Unmarshal(p.body.Messages[0].Value) != nil {
		return nil, false
	}
	return p, true
}

// encode re-encodes p. resignPromise and resignCert re-sign the promise with
// signer key and the certificate with the validators over the edited promise;
// resignTx re-signs the envelope with signer key over the promise's chain ID.
// Without them the signatures stay as they were, so they may be stale.
func (m *pffMutator) encode(p *pffTx, resignPromise, resignTx, resignCert bool, key int, accNum uint64) ([]byte, bool) {
	if resignPromise || resignCert {
		pp := fibre.PaymentPromise{}
		if pp.FromProto(&p.msg.PaymentPromise) == nil {
			if signBytes, err := pp.SignBytes(); err == nil {
				if resignPromise {
					p.msg.PaymentPromise.Signature, _ = m.signers[key].Sign(signBytes)
				}
				if resignCert {
					p.msg.ValidatorSignatures = slices.Clone(p.msg.ValidatorSignatures)
					for i := range min(len(p.msg.ValidatorSignatures), len(m.f.valPrivKeys)) {
						p.msg.ValidatorSignatures[i], _ = m.f.valPrivKeys[i].Sign(signBytes)
					}
				}
			}
		}
	}
	value, err := p.msg.Marshal()
	if err != nil {
		return nil, false
	}
	p.body.Messages = []*codectypes.Any{{TypeUrl: types.MsgPayForFibreTypeURL, Value: value}}
	if p.raw.BodyBytes, err = p.body.Marshal(); err != nil {
		return nil, false
	}
	if p.raw.AuthInfoBytes, err = p.auth.Marshal(); err != nil {
		return nil, false
	}
	if resignTx {
		doc := cosmostx.SignDoc{BodyBytes: p.raw.BodyBytes, AuthInfoBytes: p.raw.AuthInfoBytes, ChainId: p.msg.PaymentPromise.ChainId, AccountNumber: accNum}
		signBytes, err := doc.Marshal()
		if err != nil {
			return nil, false
		}
		sig, _ := m.signers[key].Sign(signBytes)
		p.raw.Signatures = [][]byte{sig}
	}
	out, err := p.raw.Marshal()
	return out, err == nil
}

// mutate applies up to maxPreverifyMutations edits to txs, each read from a
// fixed-width record. Op 0 flips a bit of the encoding, so malformed encodings
// stay covered, and op 9 drops a tx; the others decode the tx, change one
// typed field and re-encode it, re-signing it if the flags ask. An edited copy
// is appended unless the flags ask to replace its source, so the batch
// usually holds a valid tx next to a variant whose signatures may still hit
// its cache entries.
func (m *pffMutator) mutate(txs [][]byte, script []byte) [][]byte {
	txs = slices.Clone(txs)
	c := fuzzutil.New(script)
	for n := 0; n < maxPreverifyMutations && !c.Exhausted() && len(txs) > 0; n++ {
		r := c.Record(pffRecord)
		i := r.Intn(len(txs))
		op, flags := r.Byte()%pffOps, r.Byte()
		var edited []byte
		switch op {
		case 0:
			edited = bytes.Clone(txs[i])
			at, bit := r.Uint16(), r.Byte()
			if len(edited) > 0 {
				edited[int(at)%len(edited)] ^= 1 << (bit % 8)
			}
		case 9:
			txs = slices.Delete(txs, i, i+1)
			continue
		default:
			var ok bool
			if edited, ok = m.editFields(txs[i], txs[(i+1)%len(txs)], op, flags, r); !ok {
				continue
			}
		}
		if flags&pffReplace != 0 {
			txs[i] = edited
		} else {
			txs = append(txs, edited)
		}
	}
	return txs
}

// editFields applies one typed edit to raw. other supplies signatures from a
// different tx. It reports false if raw does not decode into its parts.
func (m *pffMutator) editFields(raw, other []byte, op, flags byte, r *fuzzutil.Consumer) ([]byte, bool) {
	p, ok := decodePFF(raw)
	if !ok {
		return nil, false
	}
	o, hasOther := decodePFF(other)
	pp, sigs := &p.msg.PaymentPromise, p.msg.ValidatorSignatures
	key := 0
	if flags&pffOtherKey != 0 {
		key = 1
	}
	accNum := uint64(pffAccountNumber)
	resign, resignTx, resignCert := flags&pffResign != 0, flags&pffResign != 0, flags&pffResignCert != 0

	switch op {
	case 1: // envelope sequence
		for _, si := range p.auth.SignerInfos {
			si.Sequence += uint64(int64(int8(r.Byte())))
		}
	case 2: // account number, which only the envelope sign bytes carry
		accNum += uint64(int64(int8(r.Byte())))
		resignTx = true
	case 3: // chain ID, which the promise and envelope sign bytes carry
		pp.ChainId = []string{"", "test-chain-fork", "other-chain", "test-chain", strings.Repeat("c", 200)}[r.Intn(5)]
	case 4: // envelope sign mode
		mode := pffSignModes[r.Intn(len(pffSignModes))]
		for _, si := range p.auth.SignerInfos {
			si.ModeInfo = &cosmostx.ModeInfo{Sum: &cosmostx.ModeInfo_Single_{Single: &cosmostx.ModeInfo_Single{Mode: mode}}}
		}
	case 5: // signature bytes, never re-signed
		resign, resignTx, resignCert = false, false, false
		switch r.Byte() % 5 {
		case 0: // flip a promise signature bit
			pp.Signature = bytes.Clone(pp.Signature)
			if len(pp.Signature) > 0 {
				pp.Signature[r.Intn(len(pp.Signature))] ^= 1 << (r.Byte() % 8)
			}
		case 1: // truncate the promise signature
			pp.Signature = pp.Signature[:r.Intn(len(pp.Signature)+1)]
		case 2: // the other tx's promise signature
			if hasOther {
				pp.Signature = o.msg.PaymentPromise.Signature
			}
		case 3: // flip a bit of, or empty, one validator signature
			if len(sigs) == 0 {
				return nil, false
			}
			sigs = slices.Clone(sigs)
			k := r.Intn(len(sigs))
			if r.Bool() || len(sigs[k]) == 0 {
				sigs[k] = nil
			} else {
				sigs[k] = bytes.Clone(sigs[k])
				sigs[k][r.Intn(len(sigs[k]))] ^= 1 << (r.Byte() % 8)
			}
			p.msg.ValidatorSignatures = sigs
		default: // the envelope signature
			if len(p.raw.Signatures) > 0 && len(p.raw.Signatures[0]) > 0 {
				sig := bytes.Clone(p.raw.Signatures[0])
				sig[r.Intn(len(sig))] ^= 1
				p.raw.Signatures[0] = sig
			}
		}
	case 6: // public key: either signer's, truncated, or the envelope's
		switch r.Byte() % 3 {
		case 0:
			pp.SignerPublicKey = *m.signers[r.Intn(len(m.signers))].PubKey().(*secp256k1.PubKey)
		case 1:
			pp.SignerPublicKey.Key = pp.SignerPublicKey.Key[:r.Intn(len(pp.SignerPublicKey.Key)+1)]
		default:
			if len(p.auth.SignerInfos) > 0 {
				pk, err := codectypes.NewAnyWithValue(m.signers[r.Intn(len(m.signers))].PubKey())
				if err != nil {
					return nil, false
				}
				p.auth.SignerInfos[0].PublicKey = pk
			}
		}
	case 7: // another payment promise field
		switch r.Byte() % 7 {
		case 0:
			pp.Height += int64(int8(r.Byte()))
		case 1:
			pp.Namespace = bytes.Clone(pp.Namespace)
			if len(pp.Namespace) > 0 {
				pp.Namespace[r.Intn(len(pp.Namespace))] ^= 1 << (r.Byte() % 8)
			}
		case 2:
			pp.BlobSize += uint32(int32(int16(r.Uint16())))
		case 3:
			pp.BlobVersion = uint32(r.Byte())
		case 4:
			pp.Commitment = bytes.Clone(pp.Commitment)
			if len(pp.Commitment) > 0 {
				pp.Commitment[r.Intn(len(pp.Commitment))] ^= 1 << (r.Byte() % 8)
			}
		case 5:
			pp.Commitment = pp.Commitment[:r.Intn(len(pp.Commitment))]
		default:
			if s := int16(r.Uint16()); s == 0 {
				pp.CreationTimestamp = time.Time{}
			} else {
				pp.CreationTimestamp = pp.CreationTimestamp.Add(time.Duration(s) * time.Second)
			}
		}
	case 8: // certificate shape: resize, or swap two signatures
		sigs = slices.Clone(sigs)
		if r.Bool() {
			n := r.Intn(len(m.f.valPrivKeys) + 2)
			for len(sigs) < n {
				sigs = append(sigs, nil)
			}
			sigs = sigs[:n]
		} else if len(sigs) > 1 {
			a, b := r.Intn(len(sigs)), r.Intn(len(sigs))
			sigs[a], sigs[b] = sigs[b], sigs[a]
		}
		p.msg.ValidatorSignatures = sigs
	}
	return m.encode(p, resign, resignTx, resignCert, key, accNum)
}

// fuzzPreverify runs the signature pre-pass on a block of mutated
// pay-for-fibre txs and checks that every cache entry it leaves is one the
// sequential verifier would also accept. A cache entry skips that verifier on
// this node only, so an unsound entry lets nodes with different caches vote
// differently on the same block. It returns the number of cache entries.
func fuzzPreverify(t *testing.T, script []byte, certificates, stopOnFirstFailure bool) int {
	t.Helper()
	f := newPreverifyFixture(t)
	m := &pffMutator{f: f, signers: []*secp256k1.PrivKey{f.signerPrivKey, secp256k1.GenPrivKey()}}
	txs := m.mutate([][]byte{m.signedTx(t, f.msg), m.signedTx(t, f.signedMessage(t, 1))}, script)

	prepass := f.withFreshCache(t)
	prepass.keeper.PreverifySignatures(prepass.ctx, txs, keeper.PreverifyOptions{
		Certificates:       certificates,
		StopOnFirstFailure: stopOnFirstFailure,
	})

	accounted := make(map[sigcache.Key]struct{})
	for _, raw := range txs {
		d := keeper.DecodePayForFibre(raw)
		if d == nil {
			continue
		}
		if d.PromiseKeyed() && prepass.cache.Has(d.PromiseKey) {
			pp := fibre.PaymentPromise{}
			require.NoError(t, pp.FromProto(&d.Msg.PaymentPromise), "a recorded promise must convert")
			require.NoError(t, f.withFreshCache(t).keeper.ValidatePromiseStateless(&pp),
				"the pre-pass recorded a promise the sequential verifier rejects")
			accounted[d.PromiseKey] = struct{}{}
		}
		if d.CertKeyed && prepass.cache.Has(d.CertKey) {
			require.True(t, certificates, "certificates were recorded although not requested")
			reference := f.withFreshCache(t)
			require.NoError(t, reference.keeper.ValidatePayForFibreSignatures(reference.ctx, d.Msg),
				"the pre-pass recorded a certificate the sequential verifier rejects")
			accounted[d.CertKey] = struct{}{}
		}
	}
	require.Equal(t, len(accounted), prepass.cache.Len(), "the cache holds an entry no tx in the block accounts for")
	return prepass.cache.Len()
}

// TestPreverifyFuzzSeeds keeps the fuzz seeds meaningful: the unmodified
// block warms every key, an edit with stale signatures records nothing new, a
// re-signed edit records its own entries, envelope edits leave the entries
// alone, and every single edit runs without a failure.
func TestPreverifyFuzzSeeds(t *testing.T) {
	f := newPreverifyFixture(t)
	f.keeper.PreverifySignatures(f.ctx, [][]byte{f.txBytes(t)}, keeper.PreverifyOptions{Certificates: true})
	require.Equal(t, 2, f.cache.Len(), "the unmodified tx must warm its promise and certificate")

	blobSize := func(flags byte) []byte { return fuzzutil.Records(pffRecord, []byte{0, 7, flags, 2, 0, 1}) }
	for _, tc := range []struct {
		name         string
		script       []byte
		certificates bool
		entries      int
	}{
		{"fixture", nil, true, 4},
		{"fixture, promises only", nil, false, 2},
		{"stale signatures", blobSize(0), true, 4},
		{"stale signatures, promises only", blobSize(0), false, 2},
		{"re-signed promise, stale certificate", blobSize(pffResign), true, 4},
		{"re-signed promise, promises only", blobSize(pffResign), false, 3},
		{"re-signed promise and certificate", blobSize(pffResign | pffResignCert), true, 6},
		{"signed by a key the promise does not name", blobSize(pffResign | pffResignCert | pffOtherKey), true, 4},
		{"signed by the key the promise names", fuzzutil.Records(pffRecord, []byte{0, 6, pffResign | pffResignCert | pffOtherKey, 0, 1}), true, 6},
		{"envelope edits", fuzzutil.Records(pffRecord, []byte{0, 1, 0, 1}, []byte{1, 2, 0, 1}, []byte{0, 4, pffResign, 1}), true, 4},
		{"dropped tx", fuzzutil.Records(pffRecord, []byte{1, 9}), true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.entries, fuzzPreverify(t, tc.script, tc.certificates, false))
		})
	}

	for op := range byte(pffOps) {
		for txIndex := range byte(2) {
			t.Run(fmt.Sprintf("op=%d/tx=%d", op, txIndex), func(t *testing.T) {
				fuzzPreverify(t, fuzzutil.Records(pffRecord, []byte{txIndex, op, 0, 1, 0, 40}), true, false)
				fuzzPreverify(t, fuzzutil.Records(pffRecord, []byte{txIndex, op, pffResign | pffResignCert, 2, 0, 1}), true, false)
			})
		}
	}
}

// FuzzPreverifySignatures edits a block of signed pay-for-fibre txs, mostly
// by changing typed fields with or without re-signing, and runs the parallel
// signature pre-pass that PrepareProposal, ProcessProposal and FinalizeBlock
// use. The PreBlocker call runs without a panic recovery, so any panic halts
// the node; every cache entry must be one the sequential verifier accepts,
// and nothing else may enter the cache.
func FuzzPreverifySignatures(f *testing.F) {
	f.Add([]byte{}, true, false)
	f.Add([]byte{}, false, false)
	for op := range byte(pffOps) {
		for txIndex := range byte(2) {
			f.Add(fuzzutil.Records(pffRecord, []byte{txIndex, op, 0, 1, 0, 7}), true, op%2 == 0)
			f.Add(fuzzutil.Records(pffRecord, []byte{txIndex, op, pffResign | pffResignCert, 2, 0, 120}), true, op%2 == 1)
			f.Add(fuzzutil.Records(pffRecord, []byte{txIndex, op, pffResign | pffReplace, 3, 1}, []byte{txIndex + 1, 5, 0, 2}), false, false)
		}
	}
	// Duplicate a tx, then corrupt one validator signature of the copy.
	f.Add(fuzzutil.Records(pffRecord, []byte{0, 1}, []byte{2, 5, pffReplace, 3, 0, 1, 0}), true, true)
	f.Fuzz(func(t *testing.T, script []byte, certificates, stopOnFirstFailure bool) {
		if len(script) > maxPreverifyMutations*pffRecord {
			return
		}
		fuzzPreverify(t, script, certificates, stopOnFirstFailure)
	})
}
