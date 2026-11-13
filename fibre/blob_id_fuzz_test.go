package fibre

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// isHexCommitment reports whether s spells 32 bytes in hex of either case.
func isHexCommitment(s string) bool {
	if len(s) != 2*CommitmentSize {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

// FuzzCommitmentAndBlobIDParsing checks the commitment and blob ID parsers
// against an independent oracle. CommitmentFromString accepts exactly 64 hex
// digits of either case and formats back to lower case; JSON decoding agrees
// with it for every string and round trips through MarshalJSON; the binary
// decoders accept exactly their fixed sizes, copy their input and round trip.
func FuzzCommitmentAndBlobIDParsing(f *testing.F) {
	lower := strings.Repeat("0a", CommitmentSize)
	f.Add(lower, []byte(`"`+lower+`"`))
	f.Add(strings.ToUpper(lower), append([]byte{1}, bytes.Repeat([]byte{0xab}, CommitmentSize)...))
	f.Add(lower[:63]+"g", []byte(`"0`+lower[1:]+`"`))
	f.Add(lower+"00", bytes.Repeat([]byte{7}, CommitmentSize))
	f.Add("", []byte(`null`))

	f.Fuzz(func(t *testing.T, s string, raw []byte) {
		c, err := CommitmentFromString(s)
		if !isHexCommitment(s) {
			require.Error(t, err, "accepted a non-canonical commitment string")
		} else {
			require.NoError(t, err)
			require.Equal(t, strings.ToLower(s), c.String())
			want, _ := hex.DecodeString(s)
			require.Equal(t, want, c[:])
		}

		quoted, err := json.Marshal(s)
		require.NoError(t, err)
		var fromJSON Commitment
		jsonErr := fromJSON.UnmarshalJSON(quoted)
		require.Equal(t, err == nil && isHexCommitment(s), jsonErr == nil, "JSON and string parsing disagree")
		if jsonErr == nil {
			require.Equal(t, c, fromJSON)
		}

		// Arbitrary JSON is accepted only when it decodes to a valid string.
		var anyJSON Commitment
		if anyJSON.UnmarshalJSON(raw) == nil {
			var str string
			require.NoError(t, json.Unmarshal(raw, &str))
			want, err := CommitmentFromString(str)
			require.NoError(t, err)
			require.Equal(t, want, anyJSON)
			out, err := anyJSON.MarshalJSON()
			require.NoError(t, err)
			require.Equal(t, `"`+anyJSON.String()+`"`, string(out))
			var back Commitment
			require.NoError(t, back.UnmarshalJSON(out))
			require.Equal(t, anyJSON, back)
		}

		var bin Commitment
		if err := bin.UnmarshalBinary(raw); len(raw) != CommitmentSize {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			out, err := bin.MarshalBinary()
			require.NoError(t, err)
			require.Equal(t, raw, out)
		}

		var id BlobID
		if err := id.UnmarshalBinary(raw); len(raw) != BlobIDSize {
			require.Error(t, err)
			return
		}
		require.NoError(t, id.Validate())
		require.Equal(t, raw, []byte(id))
		require.Equal(t, raw[0], id.Version())
		cm := id.Commitment()
		require.Equal(t, raw[1:], cm[:])
		require.True(t, NewBlobID(id.Version(), id.Commitment()).Equals(id))
		require.Equal(t, hex.EncodeToString(raw), id.String())
		saved := bytes.Clone(raw)
		raw[0]++
		require.Equal(t, saved, []byte(id), "BlobID aliases its input")
	})
}
