package namespace

import (
	"bytes"
	"fmt"
)

type Namespace struct {
	Version uint8
	ID      []byte
}

// New returns a new namespace with the provided version and id.
func New(version uint8, id []byte) (Namespace, error) {
	err := validateVersion(version)
	if err != nil {
		return Namespace{}, err
	}

	err = validateID(version, id)
	if err != nil {
		return Namespace{}, err
	}

	return Namespace{
		Version: version,
		ID:      id,
	}, nil
}

// MustNew returns a new namespace with the provided version and id. It panics
// if the provided version or id are not supported.
func MustNew(version uint8, id []byte) Namespace {
	ns, err := New(version, id)
	if err != nil {
		panic(err)
	}
	return ns
}

// MustNewV0 returns a new namespace with version 0 and the provided id. This
// function panics if the provided id is not exactly NamespaceVersionZeroIDSize bytes.
func MustNewV0(id []byte) Namespace {
	if len(id) != NamespaceVersionZeroIDSize {
		panic(fmt.Sprintf("invalid namespace id length: %v must be %v", len(id), NamespaceVersionZeroIDSize))
	}

	ns, err := New(NamespaceVersionZero, append(NamespaceVersionZeroPrefix, id...))
	if err != nil {
		panic(err)
	}
	return ns
}

// From returns a namespace from the provided byte slice.
func From(b []byte) (Namespace, error) {
	if len(b) != NamespaceSize {
		return Namespace{}, fmt.Errorf("invalid namespace length: %v must be %v", len(b), NamespaceSize)
	}
	rawVersion := b[0]
	rawNamespace := b[1:]
	return New(rawVersion, rawNamespace)
}

// Bytes returns this namespace as a byte slice.
func (n Namespace) Bytes() []byte {
	return append([]byte{n.Version}, n.ID...)
}

// ValidateBlobNamespace returns an error if this namespace is not a valid blob namespace.
func (n Namespace) ValidateBlobNamespace() error {
	if n.IsReserved() {
		return fmt.Errorf("invalid blob namespace: %v cannot use a reserved namespace ID, want > %v", n.Bytes(), MaxReservedNamespace.Bytes())
	}

	if n.IsParityShares() {
		return fmt.Errorf("invalid blob namespace: %v cannot use parity shares namespace ID", n.Bytes())
	}

	if n.IsTailPadding() {
		return fmt.Errorf("invalid blob namespace: %v cannot use tail padding namespace ID", n.Bytes())
	}

	return nil
}

// validateVersion returns an error if the version is not supported.
func validateVersion(version uint8) error {
	if version != NamespaceVersionZero && version != NamespaceVersionMax {
		return fmt.Errorf("unsupported namespace version %v", version)
	}
	return nil
}

// validateID returns an error if the provided id does not meet the requirements
// for the provided version.
func validateID(version uint8, id []byte) error {
	if len(id) != NamespaceIDSize {
		return fmt.Errorf("unsupported namespace id length: id %v must be %v bytes but it was %v bytes", id, NamespaceIDSize, len(id))
	}

	if version == NamespaceVersionZero && !bytes.HasPrefix(id, NamespaceVersionZeroPrefix) {
		return fmt.Errorf("unsupported namespace id with version %v. ID %v must start with %v leading zeros", version, id, len(NamespaceVersionZeroPrefix))
	}
	return nil
}

func (n Namespace) IsReserved() bool {
	return bytes.Compare(n.Bytes(), MaxReservedNamespace.Bytes()) < 1
}

func (n Namespace) IsParityShares() bool {
	return bytes.Equal(n.Bytes(), ParitySharesNamespace.Bytes())
}

func (n Namespace) IsTailPadding() bool {
	return bytes.Equal(n.Bytes(), TailPaddingNamespace.Bytes())
}

func (n Namespace) IsReservedPadding() bool {
	return bytes.Equal(n.Bytes(), ReservedPaddingNamespace.Bytes())
}

func (n Namespace) IsTx() bool {
	return bytes.Equal(n.Bytes(), TxNamespace.Bytes())
}

func (n Namespace) IsPayForBlob() bool {
	return bytes.Equal(n.Bytes(), PayForBlobNamespace.Bytes())
}
