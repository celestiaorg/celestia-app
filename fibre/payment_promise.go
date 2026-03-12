package fibre

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	gogoproto "github.com/cosmos/gogoproto/proto"
)

// SignedPaymentPromise contains a [PaymentPromise] along with validator signatures confirming the promise.
type SignedPaymentPromise struct {
	// PaymentPromise is the payment commitment promise that was signed by validators.
	*PaymentPromise
	// ValidatorSignatures are the signatures from validators confirming they received and stored the [Blob] to be paid for.
	ValidatorSignatures [][]byte
}

// PaymentPromise is a promise to pay for a fibre [Blob].
type PaymentPromise struct {
	// SignerKey is the secp256k1 public key of the signer (escrow account owner).
	SignerKey *secp256k1.PubKey
	// ChainID is the chain identifier for domain separation.
	ChainID string
	// Namespace is the namespace the blob is associated with.
	Namespace share.Namespace
	// UploadSize is the upload size of the blob (with padding, without parity), matching [Blob.UploadSize].
	UploadSize uint32
	// BlobVersion is the version of the blob format.
	BlobVersion uint32
	// Commitment is the hash of the row root and the RLC root.
	Commitment Commitment
	// CreationTimestamp is the timestamp when this promise was created.
	CreationTimestamp time.Time
	// Signature is the signer's signature over the sign bytes returned by [PaymentPromise.SignBytes].
	Signature []byte
	// Height is the height used to determine the validator set.
	Height uint64

	// cached sign bytes and hash
	signBytesOnce sync.Once
	signBytes     []byte
	signBytesErr  error
	hashOnce      sync.Once
	hash          [32]byte
	hashErr       error
}

// MarshalBinary encodes the [PaymentPromise] using protobuf.
func (p *PaymentPromise) MarshalBinary() ([]byte, error) {
	pbMsg, err := p.ToProto()
	if err != nil {
		return nil, err
	}
	return gogoproto.Marshal(pbMsg)
}

// UnmarshalBinary decodes the [PaymentPromise] from protobuf.
func (p *PaymentPromise) UnmarshalBinary(data []byte) error {
	pbMsg := &types.PaymentPromise{}
	if err := gogoproto.Unmarshal(data, pbMsg); err != nil {
		return err
	}

	return p.FromProto(pbMsg)
}

// FromProto converts the [PaymentPromise] from its protobuf representation.
func (p *PaymentPromise) FromProto(pbMsg *types.PaymentPromise) error {
	if pbMsg == nil {
		return fmt.Errorf("nil proto spayment promise")
	}

	// parse namespace
	ns, err := share.NewNamespaceFromBytes(pbMsg.Namespace)
	if err != nil {
		return fmt.Errorf("invalid namespace: %w", err)
	}

	// parse commitment
	if len(pbMsg.Commitment) != CommitmentSize {
		return fmt.Errorf("commitment must be %d bytes, got %d", CommitmentSize, len(pbMsg.Commitment))
	}

	*p = PaymentPromise{
		ChainID:           pbMsg.ChainId,
		Height:            uint64(pbMsg.Height),
		Namespace:         ns,
		UploadSize:        pbMsg.BlobSize,
		BlobVersion:       pbMsg.BlobVersion,
		Commitment:        Commitment(pbMsg.Commitment),
		CreationTimestamp: pbMsg.CreationTimestamp,
		SignerKey:         &pbMsg.SignerPublicKey,
		Signature:         pbMsg.Signature,
	}
	return nil
}

// ToProto converts the [PaymentPromise] to its protobuf representation.
func (p *PaymentPromise) ToProto() (*types.PaymentPromise, error) {
	if p.SignerKey == nil {
		return nil, errors.New("signer key must not be nil")
	}
	return &types.PaymentPromise{
		ChainId:           p.ChainID,
		Height:            int64(p.Height),
		Namespace:         p.Namespace.Bytes(),
		BlobSize:          p.UploadSize,
		BlobVersion:       p.BlobVersion,
		Commitment:        p.Commitment[:],
		CreationTimestamp: p.CreationTimestamp,
		SignerPublicKey:   *p.SignerKey,
		Signature:         p.Signature,
	}, nil
}

// Validate performs stateless validation on the [PaymentPromise].
// It verifies all field constraints and validates the [PaymentPromise.Signature] using [PaymentPromise.SignerKey].
func (p *PaymentPromise) Validate() error {
	// signer key must be valid secp256k1 public key (33 bytes)
	if p.SignerKey == nil {
		return fmt.Errorf("signer key must be %d bytes, got 0", secp256k1.PubKeySize)
	}
	if len(p.SignerKey.Key) != secp256k1.PubKeySize {
		return fmt.Errorf("signer key must be %d bytes, got %d", secp256k1.PubKeySize, len(p.SignerKey.Key))
	}

	// chain ID must not be empty and within length limit
	if p.ChainID == "" {
		return errors.New("chain id must not be empty")
	}
	if len(p.ChainID) > maxChainIDSize {
		return fmt.Errorf("chain id length %d exceeds maximum %d", len(p.ChainID), maxChainIDSize)
	}

	// upload size must be positive
	if p.UploadSize == 0 {
		return errors.New("upload size must be positive")
	}

	// commitment must be 32 bytes (enforced by type)

	// blob version is checked externally.

	// creation timestamp must be positive
	if p.CreationTimestamp.IsZero() {
		return errors.New("creation timestamp must not be zero")
	}

	// signature must be present (compact format is 64 bytes: 32 bytes r + 32 bytes s)
	if len(p.Signature) != 64 {
		return fmt.Errorf("signature must be 64 bytes, got %d", len(p.Signature))
	}

	// height must be positive
	if p.Height == 0 {
		return fmt.Errorf("height must be positive, got %d", p.Height)
	}

	// verify signature
	signBytes, err := p.SignBytes()
	if err != nil {
		return fmt.Errorf("building sign bytes: %w", err)
	}

	// verify signature using secp256k1
	if !p.SignerKey.VerifySignature(signBytes, p.Signature) {
		return errors.New("signature verification failed")
	}

	return nil
}

const (
	// MaxPaymentPromiseSize is the theoretical maximum size of all PaymentPromise fields
	// (excluding encoding overhead, like protobuf)
	MaxPaymentPromiseSize = signBytesFixedSize + signatureSize + maxChainIDSize

	// signBytesPrefix is prepended to the sign bytes to ensure the resulting signed message
	// can't be confused with a consensus message (domain separation).
	signBytesPrefix = "fibre/pp:v0"
	// signBytesFixedSize is the size of all the constant fixed size fields.
	// Format: signerPubKey(33) + namespace(29) + blobSize(4) + commitment(32) + blobVersion(4) + height(8) + timestamp(15)
	signBytesFixedSize = secp256k1.PubKeySize + share.NamespaceSize + 4 + 32 + 4 + 8 + 15

	// maxChainIDSize is the maximum allowed chain ID length.
	// Examples: "celestia" (8), "mocha-4" (7), "arabica-11" (10)
	maxChainIDSize = 20

	// signatureSize is the size of a secp256k1 signature in compact format (32 bytes r + 32 bytes s)
	signatureSize = 64
)

// SignBytes returns the bytes that should be signed for this [PaymentPromise].
// Actual signing must be done by the caller.
// The sign bytes are computed once and cached for subsequent calls.
// Format: prefix || chainID || signer_bytes || namespace || blob_size_bytes ||
//
//	commitment || blob_version_bytes || height_bytes || creation_timestamp_bytes
//
// SignBytes caches the result of the computation for subsequent calls,
// so its not allowed to change the promise after signing.
func (p *PaymentPromise) SignBytes() ([]byte, error) {
	if p.SignerKey == nil {
		return nil, errors.New("signer key must not be nil")
	}
	p.signBytesOnce.Do(func() {
		// use MarshalBinary for timestamp
		timestampBytes, err := p.CreationTimestamp.UTC().MarshalBinary() // this must be UTC
		if err != nil {
			p.signBytesErr = fmt.Errorf("marshalling timestamp: %w", err)
			return
		}

		// calculate total size including the prefix
		totalSize := len(signBytesPrefix) + len(p.ChainID) + signBytesFixedSize
		buf := make([]byte, 0, totalSize)

		// prepend domain separation prefix
		buf = append(buf, []byte(signBytesPrefix)...)
		// append chainID
		buf = append(buf, []byte(p.ChainID)...)
		// append signer_bytes (33 bytes - compressed public key)
		buf = append(buf, p.SignerKey.Bytes()...)
		// append namespace (29 bytes)
		buf = append(buf, p.Namespace.Bytes()...)
		// append blob_size (4 bytes, big-endian)
		buf = binary.BigEndian.AppendUint32(buf, p.UploadSize)
		// append commitment (32 bytes)
		buf = append(buf, p.Commitment[:]...)
		// append blob_version (4 bytes, big-endian)
		buf = binary.BigEndian.AppendUint32(buf, p.BlobVersion)
		// append height (8 bytes, big-endian)
		buf = binary.BigEndian.AppendUint64(buf, p.Height)
		// append timestamp bytes
		buf = append(buf, timestampBytes...)

		p.signBytes = buf
	})

	if p.signBytesErr != nil {
		return nil, p.signBytesErr
	}
	return p.signBytes, nil
}

// Hash returns the SHA256 hash of the [PaymentPromise] including the signature.
// The hash is computed once and cached for subsequent calls.
func (p *PaymentPromise) Hash() ([]byte, error) {
	// validate signature is present
	if len(p.Signature) == 0 {
		return nil, fmt.Errorf("signature must be set before computing hash")
	}

	p.hashOnce.Do(func() {
		// get sign bytes
		signBytes, err := p.SignBytes()
		if err != nil {
			p.hashErr = fmt.Errorf("getting sign bytes: %w", err)
			return
		}

		// hash signBytes + signature
		hasher := sha256.New()
		hasher.Write(signBytes)
		hasher.Write(p.Signature)
		copy(p.hash[:], hasher.Sum(nil))
	})

	if p.hashErr != nil {
		return nil, p.hashErr
	}
	return p.hash[:], nil
}

// SignBytesValidator returns the [PaymentPromise] bytes for validators to sign.
// This wraps the [PaymentPromise.SignBytes] with domain separation using the chain ID and signBytesPrefix.
//
// NOTE: This method encapsulates Comet's quirk which enforces a particular signing domain separation format.
// This goes on top of native [PaymentPromise] domain separation.
func (p *PaymentPromise) SignBytesValidator() ([]byte, error) {
	signBytes, err := p.SignBytes()
	if err != nil {
		return nil, fmt.Errorf("getting sign bytes: %w", err)
	}
	return core.RawBytesMessageSignBytes(p.ChainID, signBytesPrefix, signBytes)
}

// SignPaymentPromiseValidator signs the [PaymentPromise] using validator's private key behind [core.PrivValidator].
func SignPaymentPromiseValidator(promise *PaymentPromise, privVal core.PrivValidator) ([]byte, error) {
	signBytes, err := promise.SignBytes()
	if err != nil {
		return nil, fmt.Errorf("getting sign bytes: %w", err)
	}

	signature, err := privVal.SignRawBytes(promise.ChainID, signBytesPrefix, signBytes)
	if err != nil {
		return nil, fmt.Errorf("signing payment promise: %w", err)
	}

	if len(signature) != ed25519.SignatureSize {
		return nil, fmt.Errorf("invalid signature length: expected %d, got %d", ed25519.SignatureSize, len(signature))
	}

	return signature, nil
}
