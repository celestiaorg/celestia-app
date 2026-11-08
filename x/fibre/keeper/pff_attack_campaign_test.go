//go:build pffcampaign

package keeper_test

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	"github.com/cometbft/cometbft/crypto/ed25519"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
)

// TestPFFCertificateCampaign is a separate verifier diagnostic. The ABCI
// campaign uses Pebble; this diagnostic deliberately isolates cryptography and
// fallback bookkeeping using the existing keeper fixture's in-memory store.
func TestPFFCertificateCampaign(t *testing.T) {
	if os.Getenv("PFF_CERTIFICATE_CAMPAIGN") != "1" {
		t.Skip("explicit campaign only")
	}
	validators, err := strconv.Atoi(os.Getenv("PFF_VALIDATORS"))
	require.NoError(t, err)
	require.Contains(t, []int{100, appconsts.MaxFibreValidatorSignatures}, validators)
	f := newPreverifyFixtureWithValidators(t, validators)
	info := f.stakingKeeper.historicalInfo[f.ctx.BlockHeight()-1]
	for i := range f.valPrivKeys {
		key := ed25519.GenPrivKeyFromSecret([]byte(fmt.Sprintf("pff-certificate-%d", i)))
		f.valPrivKeys[i] = key
		pub, e := cryptocodec.FromCmtPubKeyInterface(key.PubKey())
		require.NoError(t, e)
		info.Valset[i].ConsensusPubkey, e = codectypes.NewAnyWithValue(pub)
		require.NoError(t, e)
		info.Valset[i].OperatorAddress = sdk.ValAddress(key.PubKey().Address()).String()
	}
	f.stakingKeeper.historicalInfo[f.ctx.BlockHeight()-1] = info
	f.signerPrivKey = secp256k1.GenPrivKeyFromSecret([]byte("pff-certificate-signer"))
	f.msg.PaymentPromise.SignerPublicKey = *f.signerPrivKey.PubKey().(*secp256k1.PubKey)
	f.msg.PaymentPromise.CreationTimestamp = time.Unix(1700000000, 0).UTC()
	var promise fibre.PaymentPromise
	require.NoError(t, promise.FromProto(&f.msg.PaymentPromise))
	signBytes, err := promise.SignBytes()
	require.NoError(t, err)
	f.msg.PaymentPromise.Signature, err = f.signerPrivKey.Sign(signBytes)
	require.NoError(t, err)
	count := validators
	if validators == 100 {
		count = 67
	}
	f.msg.ValidatorSignatures = make([][]byte, count)
	for i := range f.msg.ValidatorSignatures {
		f.msg.ValidatorSignatures[i], err = f.valPrivKeys[i].Sign(signBytes)
		require.NoError(t, err)
	}
	quorum := (validators*2 + 2) / 3
	variant, mode := os.Getenv("PFF_VARIANT"), os.Getenv("PFF_VERIFY_MODE")
	corrupt := -1
	switch variant {
	case "valid", "lowgas":
	case "invalid-first":
		corrupt = 0
	case "invalid-boundary":
		corrupt = min(128, quorum-1)
	case "invalid-quorum-end":
		corrupt = quorum - 1
	default:
		t.Fatal("unknown variant")
	}
	if corrupt >= 0 {
		f.msg.ValidatorSignatures[corrupt], err = f.valPrivKeys[corrupt].Sign([]byte("different promise"))
		require.NoError(t, err)
	}
	f.refreshCertKey(t)
	var raw cosmostx.TxRaw
	require.NoError(t, raw.Unmarshal(f.txBytesFor(t, f.msg)))
	gas := uint64(10_000_000)
	if variant == "lowgas" {
		gas = 0
	}
	raw.AuthInfoBytes, err = (&cosmostx.AuthInfo{Fee: &cosmostx.Fee{GasLimit: gas}}).Marshal()
	require.NoError(t, err)
	rawTx, err := raw.Marshal()
	require.NoError(t, err)
	expected := f.withFreshCache(t).keeper.ValidatePayForFibreSignatures(f.ctx, f.msg)
	type sample struct {
		Validators, Signatures, Quorum, Iteration int
		Variant, Mode, Scope, Error               string
		NS, Allocs, Bytes                         int64
	}
	var samples []sample
	for iteration := -1; iteration < 5; iteration++ {
		fresh := f.withFreshCache(t)
		ctx := campaignEvidence(f.ctx)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		var actual error
		if variant == "lowgas" {
			fresh.keeper.PreverifySignatures(ctx, [][]byte{rawTx}, keeper.PreverifyOptions{Certificates: true})
		} else {
			if mode == "prepass" {
				fresh.keeper.PreverifyPayForFibreMsg(ctx, f.msg, f.certKey)
			} else {
				require.Equal(t, "sequential", mode)
			}
			if mode == "sequential" || !fresh.cache.Has(f.certKey) {
				actual = fresh.keeper.ValidatePayForFibreSignatures(ctx, f.msg)
			}
		}
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)
		if variant != "lowgas" {
			if expected == nil {
				require.NoError(t, actual)
			} else {
				require.EqualError(t, actual, expected.Error())
			}
		}
		errorText := ""
		if actual != nil {
			errorText = actual.Error()
		}
		if iteration >= 0 {
			samples = append(samples, sample{Validators: validators, Signatures: count, Quorum: quorum, Iteration: iteration, Variant: variant, Mode: mode, Scope: "keeper crypto diagnostic; in-memory SDK fixture", Error: errorText, NS: elapsed.Nanoseconds(), Allocs: int64(after.Mallocs - before.Mallocs), Bytes: int64(after.TotalAlloc - before.TotalAlloc)})
		}
	}
	bz, err := json.MarshalIndent(samples, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(os.Getenv("PFF_OUTPUT"), bz, 0644))
}
