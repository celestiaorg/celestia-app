package expected

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/testchain"
)

func TestReader(t *testing.T) {
	c := testchain.New(t, 8)
	var sb strings.Builder
	sb.WriteString(Header("verify", testchain.ChainID, "anchor=9:AB") + "\n")
	for _, h := range []int64{3, 4, 6, 7} { // a gap at 5
		sb.WriteString(FromHeader(c.Header(h+1)).String() + "\n")
	}
	p := filepath.Join(t.TempDir(), "expected.tsv")
	require.NoError(t, os.WriteFile(p, []byte(sb.String()), 0o644))

	_, err := Open(p, "other-chain")
	require.ErrorContains(t, err, "not a verified fshdr header")
	r, err := Open(p, testchain.ChainID)
	require.NoError(t, err)
	defer r.Close()
	for _, tc := range []struct {
		h  int64
		ok bool
	}{{2, false}, {3, true}, {3, true}, {5, false}, {6, true}, {7, true}, {8, false}, {9, false}} {
		got, ok, err := r.AppHash(tc.h)
		require.NoError(t, err, tc.h)
		require.Equal(t, tc.ok, ok, tc.h)
		if ok {
			require.Equal(t, testchain.Hash("app", tc.h+1), got)
		}
	}
	_, _, err = r.AppHash(4)
	require.ErrorContains(t, err, "asked after")
}
