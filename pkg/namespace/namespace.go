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
	err := validateVersionSupported(version)
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

// NewV0 returns a new namespace with version 0 and the provided subID. subID
// must be <= 10 bytes. If subID is < 10 bytes, it will be left-padded with 0s
// to fill 10 bytes.
func NewV0(subID []byte) (Namespace, error) {
	if lenSubID := len(subID); lenSubID > NamespaceVersionZeroIDSize {
		return Namespace{}, fmt.Errorf("subID must be <= %v, but it was %v bytes", NamespaceVersionZeroIDSize, lenSubID)
	}

	subID = leftPad(subID, NamespaceVersionZeroIDSize)
	id := make([]byte, NamespaceIDSize)
	copy(id[NamespaceVersionZeroPrefixSize:], subID)

	ns, err := New(NamespaceVersionZero, id)
	if err != nil {
		return Namespace{}, err
	}

	return ns, nil
}

// MustNewV0 returns a new namespace with version 0 and the provided subID. This
// function panics if the provided subID would result in an invalid namespace.
func MustNewV0(subID []byte) Namespace {
	ns, err := NewV0(subID)
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

// validateVersionSupported returns an error if the version is not supported.
func validateVersionSupported(version uint8) error {
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

// IsReserved returns true if the namespace is reserved for protocol-use.
func (n Namespace) IsReserved() bool {
	return n.IsPrimaryReserved() || n.IsSecondaryReserved()
}

func (n Namespace) IsPrimaryReserved() bool {
	return n.IsLessOrEqualThan(MaxPrimaryReservedNamespace)
}

func (n Namespace) IsSecondaryReserved() bool {
	return n.IsGreaterOrEqualThan(MinSecondaryReservedNamespace)
}

func (n Namespace) IsParityShares() bool {
	return bytes.Equal(n.Bytes(), ParitySharesNamespace.Bytes())
}

func (n Namespace) IsTailPadding() bool {
	return bytes.Equal(n.Bytes(), TailPaddingNamespace.Bytes())
}

func (n Namespace) IsPrimaryReservedPadding() bool {
	return bytes.Equal(n.Bytes(), PrimaryReservedPaddingNamespace.Bytes())
}

func (n Namespace) IsTx() bool {
	return bytes.Equal(n.Bytes(), TxNamespace.Bytes())
}

func (n Namespace) IsPayForBlob() bool {
	return bytes.Equal(n.Bytes(), PayForBlobNamespace.Bytes())
}

func (n Namespace) Repeat(times int) []Namespace {
	ns := make([]Namespace, times)
	for i := 0; i < times; i++ {
		ns[i] = n.deepCopy()
	}
	return ns
}

func (n Namespace) Equals(n2 Namespace) bool {
	return bytes.Equal(n.Bytes(), n2.Bytes())
}

func (n Namespace) IsLessThan(n2 Namespace) bool {
	return bytes.Compare(n.Bytes(), n2.Bytes()) == -1
}

func (n Namespace) IsLessOrEqualThan(n2 Namespace) bool {
	return bytes.Compare(n.Bytes(), n2.Bytes()) < 1
}

func (n Namespace) IsGreaterThan(n2 Namespace) bool {
	return bytes.Compare(n.Bytes(), n2.Bytes()) == 1
}

func (n Namespace) IsGreaterOrEqualThan(n2 Namespace) bool {
	return bytes.Compare(n.Bytes(), n2.Bytes()) > -1
}

// leftPad returns a new byte slice with the provided byte slice left-padded to the provided size.
// If the provided byte slice is already larger than the provided size, the original byte slice is returned.
func leftPad(b []byte, size int) []byte {
	if len(b) >= size {
		return b
	}
	pad := make([]byte, size-len(b))
	return append(pad, b...)
}

// deepCopy returns a deep copy of the Namespace object.
func (n Namespace) deepCopy() Namespace {
	// Create a deep copy of the ID slice
	copyID := make([]byte, len(n.ID))
	copy(copyID, n.ID)

	// Create a new Namespace object with the copied fields
	copyNamespace := Namespace{
		Version: n.Version,
		ID:      copyID,
	}

	return copyNamespace
}
