package keeper

import (
	"github.com/celestiaorg/celestia-app/v12/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v12/pkg/parallel"
	"github.com/celestiaorg/celestia-app/v12/x/zkism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// proofInputs is every input of a proof verification, so a recorded verdict applies only to the
// exact verification it came from.
type proofInputs struct {
	vkey, programVk, proof, publicValues string
}

func newProofInputs(vkey, programVk, proof, publicValues []byte) proofInputs {
	return proofInputs{string(vkey), string(programVk), string(proof), string(publicValues)}
}

// PreverifyProofs verifies the proofs of msgs across every CPU and records each verdict for the
// handlers, replacing the previous block's. It decides nothing: handlers verify misses inline.
func (k *Keeper) PreverifyProofs(ctx sdk.Context, msgs []sdk.Msg) {
	seen := make(map[proofInputs]struct{})
	var items []proofInputs
	for _, msg := range msgs {
		if len(items) == appconsts.MaxZKProofMessages {
			break
		}
		inputs, ok := k.proofInputs(ctx, msg)
		if !ok {
			continue
		}
		if _, dup := seen[inputs]; dup {
			continue
		}
		seen[inputs] = struct{}{}
		items = append(items, inputs)
	}

	errs := make([]error, len(items))
	done := make([]bool, len(items))
	parallel.ForEach(len(items), func(i int) {
		// A panic leaves the proof without a verdict; inline verification then fails the tx alone.
		defer func() { _ = recover() }()
		in := items[i]
		v, err := k.verifier([]byte(in.vkey))
		if err == nil {
			err = v.VerifyProof([]byte(in.proof), []byte(in.programVk), []byte(in.publicValues))
		}
		errs[i], done[i] = err, true
	})()

	verdicts := make(map[proofInputs]error, len(items))
	for i, in := range items {
		if done[i] {
			verdicts[in] = errs[i]
		}
	}
	k.verdicts.Store(&verdicts)
}

// proofInputs returns the verification inputs of a proof-carrying msg under its ISM's current keys.
func (k *Keeper) proofInputs(ctx sdk.Context, msg sdk.Msg) (proofInputs, bool) {
	switch msg := msg.(type) {
	case *types.MsgUpdateInterchainSecurityModule:
		ism, err := k.isms.Get(ctx, msg.Id.GetInternalId())
		if err != nil {
			return proofInputs{}, false
		}
		return newProofInputs(ism.Groth16Vkey, ism.StateTransitionVkey, msg.Proof, msg.PublicValues), true
	case *types.MsgSubmitMessages:
		ism, err := k.isms.Get(ctx, msg.Id.GetInternalId())
		if err != nil {
			return proofInputs{}, false
		}
		return newProofInputs(ism.Groth16Vkey, ism.StateMembershipVkey, msg.Proof, msg.PublicValues), true
	}
	return proofInputs{}, false
}

// verifyProof returns the verdict PreverifyProofs recorded for these exact inputs, or verifies the
// proof with v. Verification is a pure function of the inputs, so both give the same error.
func (k *Keeper) verifyProof(v *types.SP1Groth16Verifier, vkey, proof, programVk, publicValues []byte) error {
	if verdicts := k.verdicts.Load(); verdicts != nil {
		if err, ok := (*verdicts)[newProofInputs(vkey, programVk, proof, publicValues)]; ok {
			return err
		}
	}
	return v.VerifyProof(proof, programVk, publicValues)
}
