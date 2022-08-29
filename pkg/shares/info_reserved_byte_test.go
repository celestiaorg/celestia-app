package shares

import "testing"

func TestInfoReservedByte(t *testing.T) {
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
		irb, err := NewInfoReservedByte(test.version, test.isMessageStart)
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

func TestInfoReservedByteErrors(t *testing.T) {
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
		_, err := NewInfoReservedByte(test.version, false)
		if err == nil {
			t.Errorf("got nil but want error when version > 127")
		}
	}
}

func FuzzNewInfoReservedByte(f *testing.F) {
	f.Fuzz(func(t *testing.T, version uint8, isMessageStart bool) {
		if version > 127 {
			t.Skip()
		}
		_, err := NewInfoReservedByte(version, isMessageStart)
		if err != nil {
			t.Errorf("got nil but want error when version > 127")
		}
	})
}
