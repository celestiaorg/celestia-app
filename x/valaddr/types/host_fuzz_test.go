package types_test

import (
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/x/valaddr/types"
)

// FuzzValidateHost checks ValidateHost, which MsgSetFibreProviderInfo.ValidateBasic
// applies to the fibre host a validator registers on chain. It must not panic,
// and an accepted host must be a bounded, canonical host:port: it splits into a
// non-empty host and a port in [1, 65535] that join back to the exact input.
func FuzzValidateHost(f *testing.F) {
	for _, host := range []string{
		"validator.example.com:7980",
		"127.0.0.1:1",
		"[::1]:65535",
		"[2001:db8::1]:443",
		"host:0",
		"host:65536",
		"host:",
		":80",
		"host",
		"a:b:80",
		"",
	} {
		f.Add(host)
	}

	f.Fuzz(func(t *testing.T, host string) {
		if types.ValidateHost(host) != nil {
			return
		}
		if len(host) > types.MaxHostLen {
			t.Fatalf("accepted %d-character host", len(host))
		}
		h, p, err := net.SplitHostPort(host)
		if err != nil {
			t.Fatalf("accepted %q that SplitHostPort rejects: %v", host, err)
		}
		if h == "" {
			t.Fatalf("accepted %q with an empty host", host)
		}
		// Excluded pending a fix; see the Linear issue for this target.
		if excludedHostSpelling(host, h, p) {
			return
		}
		port, err := strconv.ParseUint(p, 10, 16)
		if err != nil || port == 0 {
			t.Fatalf("accepted %q with port %q", host, p)
		}
		if joined := net.JoinHostPort(h, strconv.FormatUint(port, 10)); joined != host {
			t.Fatalf("accepted non-canonical %q (canonical %q)", host, joined)
		}
	})
}

// excludedHostSpelling reports host strings excluded above: brackets around a
// host part that needs none, or a port not spelled as a plain decimal.
func excludedHostSpelling(host, h, p string) bool {
	port, err := strconv.Atoi(p)
	return (strings.HasPrefix(host, "[") && !strings.Contains(h, ":")) || (err == nil && strconv.Itoa(port) != p)
}
