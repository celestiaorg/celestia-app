package namespace

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

var (
	validID         = append(NamespaceVersionZeroPrefix, bytes.Repeat([]byte{1}, NamespaceVersionZeroIDSize)...)
	tooShortID      = append(NamespaceVersionZeroPrefix, []byte{1}...)
	tooLongID       = append(NamespaceVersionZeroPrefix, bytes.Repeat([]byte{1}, NamespaceSize)...)
	invalidPrefixID = bytes.Repeat([]byte{1}, NamespaceSize)
)

func TestNew(t *testing.T) {
	type testCase struct {
		name    string
		version uint8
		id      []byte
		wantErr bool
		want    Namespace
	}

	testCases := []testCase{
		{
			name:    "valid namespace",
			version: NamespaceVersionZero,
			id:      validID,
			wantErr: false,
			want: Namespace{
				Version: NamespaceVersionZero,
				ID:      validID,
			},
		},
		{
			name:    "unsupported version",
			version: uint8(1),
			id:      validID,
			wantErr: true,
		},
		{
			name:    "unsupported id: too short",
			version: NamespaceVersionZero,
			id:      tooShortID,
			wantErr: true,
		},
		{
			name:    "unsupported id: too long",
			version: NamespaceVersionZero,
			id:      tooLongID,
			wantErr: true,
		},
		{
			name:    "unsupported id: invalid prefix",
			version: NamespaceVersionZero,
			id:      invalidPrefixID,
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := New(tc.version, tc.id)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFrom(t *testing.T) {
	type testCase struct {
		name    string
		bytes   []byte
		wantErr bool
		want    Namespace
	}
	validNamespace := []byte{}
	validNamespace = append(validNamespace, NamespaceVersionZero)
	validNamespace = append(validNamespace, NamespaceVersionZeroPrefix...)
	validNamespace = append(validNamespace, bytes.Repeat([]byte{0x1}, NamespaceVersionZeroIDSize)...)
	parityNamespace := bytes.Repeat([]byte{0xFF}, NamespaceSize)

	testCases := []testCase{
		{
			name:    "valid namespace",
			bytes:   validNamespace,
			wantErr: false,
			want: Namespace{
				Version: NamespaceVersionZero,
				ID:      validID,
			},
		},
		{
			name:    "parity namespace",
			bytes:   parityNamespace,
			wantErr: false,
			want: Namespace{
				Version: NamespaceVersionMax,
				ID:      bytes.Repeat([]byte{0xFF}, NamespaceIDSize),
			},
		},
		{
			name:    "unsupported version",
			bytes:   append([]byte{1}, append(NamespaceVersionZeroPrefix, bytes.Repeat([]byte{1}, NamespaceSize-len(NamespaceVersionZeroPrefix))...)...),
			wantErr: true,
		},
		{
			name:    "unsupported id: too short",
			bytes:   append([]byte{NamespaceVersionZero}, tooShortID...),
			wantErr: true,
		},
		{
			name:    "unsupported id: too long",
			bytes:   append([]byte{NamespaceVersionZero}, tooLongID...),
			wantErr: true,
		},
		{
			name:    "unsupported id: invalid prefix",
			bytes:   append([]byte{NamespaceVersionZero}, invalidPrefixID...),
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := From(tc.bytes)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestBytes(t *testing.T) {
	namespace, err := New(NamespaceVersionZero, validID)
	assert.NoError(t, err)

	want := append([]byte{NamespaceVersionZero}, validID...)
	got := namespace.Bytes()

	assert.Equal(t, want, got)
}
