package namespace

import (
	"bytes"
	"math"
	"reflect"
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

// TestRepeatNonMutability ensures that the output of Repeat method is not mutated when the original namespace is mutated.
func TestRepeatNonMutability(t *testing.T) {
	n := 10
	namespace := Namespace{Version: NamespaceVersionMax, ID: []byte{1, 2, 3, 4}}
	repeated := namespace.Repeat(n)
	// mutate the original namespace
	namespace.ID[0] = 5
	// ensure the repeated namespaces are not mutated
	for i := 0; i < n; i++ {
		assert.NotEqual(t, repeated[i], namespace)
	}
}

func TestNewV0(t *testing.T) {
	type testCase struct {
		name    string
		subID   []byte
		want    Namespace
		wantErr bool
	}

	testCases := []testCase{
		{
			name:  "valid namespace",
			subID: bytes.Repeat([]byte{1}, NamespaceVersionZeroIDSize),
			want: Namespace{
				Version: NamespaceVersionZero,
				ID:      append(NamespaceVersionZeroPrefix, bytes.Repeat([]byte{1}, NamespaceVersionZeroIDSize)...),
			},
			wantErr: false,
		},
		{
			name:  "left pads subID if too short",
			subID: []byte{1, 2, 3, 4},
			want: Namespace{
				Version: NamespaceVersionZero,
				ID:      append(NamespaceVersionZeroPrefix, []byte{0, 0, 0, 0, 0, 0, 1, 2, 3, 4}...),
			},
			wantErr: false,
		},
		{
			name:    "invalid namespace because subID is too long",
			subID:   bytes.Repeat([]byte{1}, NamespaceVersionZeroIDSize+1),
			want:    Namespace{},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		got, err := NewV0(tc.subID)
		if tc.wantErr {
			assert.Error(t, err)
			return
		}
		assert.NoError(t, err)
		assert.Equal(t, tc.want, got)
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

func TestLeftPad(t *testing.T) {
	tests := []struct {
		input    []byte
		size     int
		expected []byte
	}{
		// input smaller than pad size
		{[]byte{1, 2, 3}, 10, []byte{0, 0, 0, 0, 0, 0, 0, 1, 2, 3}},
		{[]byte{1}, 5, []byte{0, 0, 0, 0, 1}},
		{[]byte{1, 2}, 4, []byte{0, 0, 1, 2}},

		// input equal to pad size
		{[]byte{1, 2, 3}, 3, []byte{1, 2, 3}},
		{[]byte{1, 2, 3, 4}, 4, []byte{1, 2, 3, 4}},

		// input larger than pad size
		{[]byte{1, 2, 3, 4, 5}, 4, []byte{1, 2, 3, 4, 5}},
		{[]byte{1, 2, 3, 4, 5, 6, 7}, 3, []byte{1, 2, 3, 4, 5, 6, 7}},

		// input size 0
		{[]byte{}, 8, []byte{0, 0, 0, 0, 0, 0, 0, 0}},
		{[]byte{}, 0, []byte{}},
	}

	for _, test := range tests {
		result := leftPad(test.input, test.size)
		assert.True(t, reflect.DeepEqual(result, test.expected))
	}
}

func TestIsReserved(t *testing.T) {
	type testCase struct {
		ns   Namespace
		want bool
	}
	testCases := []testCase{
		{
			ns:   MustNewV0(bytes.Repeat([]byte{1}, NamespaceVersionZeroIDSize)),
			want: false,
		},
		{
			ns:   TxNamespace,
			want: true,
		},
		{
			ns:   IntermediateStateRootsNamespace,
			want: true,
		},
		{
			ns:   PayForBlobNamespace,
			want: true,
		},
		{
			ns:   PrimaryReservedPaddingNamespace,
			want: true,
		},
		{
			ns:   MaxPrimaryReservedNamespace,
			want: true,
		},
		{
			ns:   MinSecondaryReservedNamespace,
			want: true,
		},
		{
			ns:   TailPaddingNamespace,
			want: true,
		},
		{
			ns:   ParitySharesNamespace,
			want: true,
		},
		{
			ns: Namespace{
				Version: math.MaxUint8,
				ID:      append(bytes.Repeat([]byte{0xFF}, NamespaceIDSize-1), 1),
			},
			want: true,
		},
	}

	for _, tc := range testCases {
		got := tc.ns.IsReserved()
		assert.Equal(t, tc.want, got)
	}
}
