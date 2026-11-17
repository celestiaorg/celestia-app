package genesis

import (
	"bytes"
	"fmt"

	cryptoenc "github.com/cometbft/cometbft/crypto/encoding"
	cmttypes "github.com/cometbft/cometbft/types"
)

// checkValidators checks each consensus key and that CometBFT accepts the set.
func checkValidators(params *cmttypes.ConsensusParams, validators []cmttypes.GenesisValidator) error {
	vals := make([]*cmttypes.Validator, 0, len(validators))
	for i, v := range validators {
		field := fmt.Sprintf("consensus.validators[%d]", i)
		// The proto round trip rejects nil, unsupported and wrong-size keys.
		proto, err := cryptoenc.PubKeyToProto(v.PubKey)
		if err == nil {
			_, err = cryptoenc.PubKeyFromProto(proto)
		}
		if err != nil {
			return &CheckError{Step: stepInput, Field: field + ".pub_key", Want: "a valid public key", Got: err.Error()}
		}
		// Same check as celestia-core state/execution.go validateValidatorUpdates.
		if !cmttypes.IsValidPubkeyType(params.Validator, v.PubKey.Type()) {
			return &CheckError{Step: stepInput, Field: field + ".pub_key", Want: fmt.Sprintf("one of %v", params.Validator.PubKeyTypes), Got: v.PubKey.Type()}
		}
		if len(v.Address) > 0 && !bytes.Equal(v.Address, v.PubKey.Address()) {
			return &CheckError{Step: stepInput, Field: field + ".address", Want: v.PubKey.Address().String(), Got: v.Address.String()}
		}
		vals = append(vals, cmttypes.NewValidator(v.PubKey, v.Power))
	}
	// Rejects duplicate keys and power above MaxTotalVotingPower with an error.
	if err := (&cmttypes.ValidatorSet{}).UpdateWithChangeSet(vals); err != nil {
		return &CheckError{Step: stepInput, Field: "consensus.validators", Want: "a valid validator set", Got: err.Error()}
	}
	return nil
}
