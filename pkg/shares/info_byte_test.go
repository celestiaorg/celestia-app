package shares

import "testing"

func TestInfoByte(t *testing.T) {
	blobStart := true
	notBlobStart := false

	type testCase struct {
		version         uint8
		isSequenceStart bool
	}
	tests := []testCase{
		{0, blobStart},
		{1, blobStart},
		{2, blobStart},
		{127, blobStart},

		{0, notBlobStart},
		{1, notBlobStart},
		{2, notBlobStart},
		{127, notBlobStart},
	}

	for _, test := range tests {
		irb, err := NewInfoByte(test.version, test.isSequenceStart)
		if err != nil {
			t.Errorf("got %v want no error", err)
		}
		if got := irb.Version(); got != test.version {
			t.Errorf("got version %v want %v", got, test.version)
		}
		if got := irb.IsSequenceStart(); got != test.isSequenceStart {
			t.Errorf("got IsSequenceStart %v want %v", got, test.isSequenceStart)
		}
	}
}

func TestInfoByteErrors(t *testing.T) {
	blobStart := true
	notBlobStart := false

	type testCase struct {
		version         uint8
		isSequenceStart bool
	}

	tests := []testCase{
		{128, notBlobStart},
		{255, notBlobStart},
		{128, blobStart},
		{255, blobStart},
	}

	for _, test := range tests {
		_, err := NewInfoByte(test.version, false)
		if err == nil {
			t.Errorf("got nil but want error when version > 127")
		}
	}
}

func FuzzNewInfoByte(f *testing.F) {
	f.Fuzz(func(t *testing.T, version uint8, isSequenceStart bool) {
		if version > 127 {
			t.Skip()
		}
		_, err := NewInfoByte(version, isSequenceStart)
		if err != nil {
			t.Errorf("got nil but want error when version > 127")
		}
	})
}

func TestParseInfoByte(t *testing.T) {
	type testCase struct {
		b                   byte
		wantVersion         uint8
		wantisSequenceStart bool
	}

	tests := []testCase{
		{0b00000000, 0, false},
		{0b00000001, 0, true},
		{0b00000010, 1, false},
		{0b00000011, 1, true},
		{0b00000101, 2, true},
		{0b11111111, 127, true},
	}

	for _, test := range tests {
		got, err := ParseInfoByte(test.b)
		if err != nil {
			t.Errorf("got %v want no error", err)
		}
		if got.Version() != test.wantVersion {
			t.Errorf("got version %v want %v", got.Version(), test.wantVersion)
		}
		if got.IsSequenceStart() != test.wantisSequenceStart {
			t.Errorf("got IsSequenceStart %v want %v", got.IsSequenceStart(), test.wantisSequenceStart)
		}
	}
}
