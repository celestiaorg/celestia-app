package orchestrator

import "errors"

var (
	ErrValsetNotFound                  = errors.New("valset not found")
	ErrDataCommitmentNotFound          = errors.New("data commitment not found")
	ErrUnmarshallValset                = errors.New("couldn't unmarsall valset")
	ErrUnmarshallDataCommitment        = errors.New("couldn't unmarsall data commitment")
	ErrConfirmSignatureNotFound        = errors.New("confirm signature not found")
	ErrNotEnoughValsetConfirms         = errors.New("couldn't find enough valset confirms")
	ErrNotEnoughDataCommitmentConfirms = errors.New("couldn't find enough data commitment confirms")
	ErrUnknownAttestationType          = errors.New("unknown attestation type")
	ErrFailedBroadcast                 = errors.New("failed to broadcast transaction")
)
