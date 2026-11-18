package types

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	testifyrequire "github.com/stretchr/testify/require"
)

// TestCheckTdLaunch pins which launches a quote may come from. Our enclave's
// attributes are 0x0000001000000000 (SEPT_VE_DISABLE only), from the real quote
// TestVerifyRealQuote verifies end to end.
func TestCheckTdLaunch(t *testing.T) {
	ours := []byte{0x00, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, 0x00}
	with := func(byteIdx int, mask byte) []byte {
		a := bytes.Clone(ours)
		a[byteIdx] |= mask
		return a
	}
	zero48 := make([]byte, 48)
	bound := bytes.Clone(zero48)
	bound[47] = 1

	testifyrequire.NoError(t, checkTdLaunch(ours, nil), "a v4 or TD 1.0 body carries no mr_servicetd")
	testifyrequire.NoError(t, checkTdLaunch(ours, zero48), "a TD 1.5 body with no service td bound")

	testifyrequire.ErrorContains(t, checkTdLaunch(with(0, 0x01), nil), "debug")
	testifyrequire.ErrorContains(t, checkTdLaunch(with(3, 0x20), nil), "migratable")
	testifyrequire.ErrorContains(t, checkTdLaunch(ours, bound), "service td")
	testifyrequire.ErrorContains(t, checkTdLaunch(ours[:7], nil), "8 bytes")

	// Bits we take no view on, such as PKS (bit 30), still pass.
	testifyrequire.NoError(t, checkTdLaunch(with(3, 0x40), zero48))
}

// TestCheckTdxModuleStatus grades the TDX module against Intel's real TCB info
// (evaluation 20, from the real quote's collateral): TDX_01 is UpToDate from SVN
// 11 and OutOfDate below, and our enclave runs TDX_01 SVN 11.
func TestCheckTdxModuleStatus(t *testing.T) {
	raw, err := os.ReadFile("../internal/testdata/real_quote.json")
	testifyrequire.NoError(t, err)
	var fixture struct {
		Collateral struct {
			TcbInfo string `json:"tcb_info"`
		} `json:"collateral"`
	}
	testifyrequire.NoError(t, json.Unmarshal(raw, &fixture))
	tcbInfo, err := hex.DecodeString(strings.TrimPrefix(fixture.Collateral.TcbInfo, "0x"))
	testifyrequire.NoError(t, err)

	svn := func(isvsvn, major byte) []byte {
		s := make([]byte, 16)
		s[0], s[1], s[2] = isvsvn, major, 5
		return s
	}
	// withStatus marks TDX_01's level at `isvsvn` with `status`.
	withStatus := func(isvsvn uint32, status string) []byte {
		var doc map[string]any
		testifyrequire.NoError(t, json.Unmarshal(tcbInfo, &doc))
		for _, m := range doc["tcbInfo"].(map[string]any)["tdxModuleIdentities"].([]any) {
			module := m.(map[string]any)
			if module["id"] != "TDX_01" {
				continue
			}
			for _, l := range module["tcbLevels"].([]any) {
				level := l.(map[string]any)
				if uint32(level["tcb"].(map[string]any)["isvsvn"].(float64)) == isvsvn {
					level["tcbStatus"] = status
				}
			}
		}
		out, err := json.Marshal(doc)
		testifyrequire.NoError(t, err)
		return out
	}

	testifyrequire.NoError(t, checkTdxModuleStatus(tcbInfo, svn(11, 1)), "our enclave")
	testifyrequire.NoError(t, checkTdxModuleStatus(tcbInfo, svn(12, 1)), "newer than Intel lists")
	testifyrequire.NoError(t, checkTdxModuleStatus(tcbInfo, svn(0, 0)), "the default module is graded by the platform level")

	testifyrequire.ErrorContains(t, checkTdxModuleStatus(tcbInfo, svn(6, 1)), "OutOfDate", "an old module")
	testifyrequire.ErrorContains(t, checkTdxModuleStatus(withStatus(11, "OutOfDate"), svn(11, 1)), "OutOfDate", "Intel marks ours OutOfDate")
	testifyrequire.ErrorContains(t, checkTdxModuleStatus(withStatus(11, "Revoked"), svn(11, 1)), "Revoked", "Intel revokes ours")
	testifyrequire.ErrorContains(t, checkTdxModuleStatus(tcbInfo, svn(1, 1)), "below every tcb level")
	testifyrequire.ErrorContains(t, checkTdxModuleStatus(tcbInfo, svn(11, 2)), "no tdx module identity TDX_02")
	testifyrequire.Error(t, checkTdxModuleStatus(tcbInfo, svn(11, 1)[:15]))
}
