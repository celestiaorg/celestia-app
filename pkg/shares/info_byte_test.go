package shares

import "testing"

func TestInfoByte(t *testing.T) {
	messageStart := true
	notMessageStart := false

	type testCase struct {
		version        uint8
		isMessageStart bool
	}
	tests := []testCase{
		{0, messageStart},
		{1, messageStart},
		{2, messageStart},
		{127, messageStart},

		{0, notMessageStart},
		{1, notMessageStart},
		{2, notMessageStart},
		{127, notMessageStart},
	}

	for _, test := range tests {
		irb, err := NewInfoByte(test.version, test.isMessageStart)
		if err != nil {
			t.Errorf("got %v want no error", err)
		}
		if got := irb.Version(); got != test.version {
			t.Errorf("got version %v want %v", got, test.version)
		}
		if got := irb.IsMessageStart(); got != test.isMessageStart {
			t.Errorf("got isMessageStart %v want %v", got, test.isMessageStart)
		}
	}
}

func TestInfoByteErrors(t *testing.T) {
	messageStart := true
	notMessageStart := false

	type testCase struct {
		version        uint8
		isMessageStart bool
	}

	tests := []testCase{
		{128, notMessageStart},
		{255, notMessageStart},
		{128, messageStart},
		{255, messageStart},
	}

	for _, test := range tests {
		_, err := NewInfoByte(test.version, false)
		if err == nil {
			t.Errorf("got nil but want error when version > 127")
		}
	}
}

func FuzzNewInfoByte(f *testing.F) {
	f.Fuzz(func(t *testing.T, version uint8, isMessageStart bool) {
		if version > 127 {
			t.Skip()
		}
		_, err := NewInfoByte(version, isMessageStart)
		if err != nil {
			t.Errorf("got nil but want error when version > 127")
		}
	})
}

func TestParseInfoByte(t *testing.T) {
	type testCase struct {
		b                  byte
		wantVersion        uint8
		wantIsMessageStart bool
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
		if got.IsMessageStart() != test.wantIsMessageStart {
			t.Errorf("got isMessageStart %v want %v", got.IsMessageStart(), test.wantIsMessageStart)
		}
	}
}
