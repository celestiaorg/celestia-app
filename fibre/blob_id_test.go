package fibre

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCommitmentMarshalJSON(t *testing.T) {
	original := generateCommitment()

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded Commitment
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	require.Equal(t, original, decoded)
}

func TestCommitmentUnmarshalJSON_InvalidHex(t *testing.T) {
	var c Commitment
	err := json.Unmarshal([]byte(`"not_valid_hex!"`), &c)
	require.Error(t, err)
}

func TestCommitmentUnmarshalJSON_WrongLength(t *testing.T) {
	var c Commitment
	// valid hex but only 2 bytes instead of 32
	err := json.Unmarshal([]byte(`"aabb"`), &c)
	require.Error(t, err)
}

func TestCommitmentMarshalBinary(t *testing.T) {
	original := generateCommitment()

	data, err := original.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}

	var decoded Commitment
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)
	require.Equal(t, original, decoded)
}

func TestCommitmentUnmarshalBinary_WrongLength(t *testing.T) {
	var c Commitment
	err := c.UnmarshalBinary([]byte{1, 2, 3})
	require.Error(t, err)
}

func generateCommitment() Commitment {
	var c Commitment
	for i := range c {
		c[i] = byte(i)
	}
	return c
}
