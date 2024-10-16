package groth16

import (
	"bytes"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
)

func (cs *ClientState) GetStateTransitionVerifierKey() (groth16.VerifyingKey, error) {
	vk, err := DeserializeVerifyingKey(cs.StateTransitionVerifierKey)
	if err != nil {
		return nil, err
	}
	return vk, nil
}

func (cs *ClientState) GetStateInclusionVerifierKey() (groth16.VerifyingKey, error) {
	vk, err := DeserializeVerifyingKey(cs.StateInclusionVerifierKey)
	if err != nil {
		return nil, err
	}
	return vk, nil
}

func SerializeVerifyingKey(vk groth16.VerifyingKey) ([]byte, error) {
	var buf bytes.Buffer
	_, err := vk.WriteTo(&buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func DeserializeVerifyingKey(vkProto []byte) (groth16.VerifyingKey, error) {
	vk := groth16.NewVerifyingKey(ecc.BN254)
	_, err := vk.ReadFrom(bytes.NewReader(vkProto))
	if err != nil {
		return nil, err
	}
	return vk, nil
}
