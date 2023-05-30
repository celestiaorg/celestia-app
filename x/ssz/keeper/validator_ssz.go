package keeper

//go:generate go run sszgen --path validator_ssz.go

// https://github.com/tendermint/tendermint/blob/v0.35.9/types/validator_set.go#L347
type ValidatorSetSSZ struct {
	Validators []*ValidatorSSZ `ssz-max:"1024"`
}

// https://github.com/tendermint/tendermint/blob/v0.35.9/types/validator.go#L116
type ValidatorSSZ struct {
	PubKey      []byte `ssz-size:"32"`
	VotingPower uint64
}

// func SSZ(v *types.ValidatorSet) [32]byte {
// 	validators := make([]*ValidatorSSZ, len(v.Validators))
// 	for i, val := range v.Validators {
// 		validators[i] = &ValidatorSSZ{
// 			PubKey:      val.PubKey.Bytes(),
// 			VotingPower: uint64(val.VotingPower),
// 		}
// 	}
// 	sszStruct := ValidatorSetSSZ{
// 		Validators: validators,
// 	}

// 	root, err := sszStruct.HashTreeRoot()
// 	if err != nil {
// 		panic(err)
// 	}
// 	return root
// }
