// Command teeism-collateral assembles the Intel PCS artifacts a TDX quote must
// be verified against, so they can be carried in a transaction.
//
// A consensus machine cannot fetch collateral over the network: every validator
// has to reach the same verdict from the same bytes. So the relayer fetches it
// once, here, and ships it with the quote.
//
// Rather than re-deriving which artifacts are needed from the quote, this runs
// the real online verification path with a recording proxy in front of it and
// keeps whatever that path asked for. What gets written is then re-verified
// offline through the same function the keeper calls, so a bundle that would not
// have satisfied consensus never reaches a transaction.
package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/v10/x/teeism/types"
	"github.com/google/go-tdx-guest/abi"
	pb "github.com/google/go-tdx-guest/proto/tdx"
	"github.com/google/go-tdx-guest/verify"
	"github.com/google/go-tdx-guest/verify/trust"
)

// recordingGetter forwards to the real PCS and keeps every response.
type recordingGetter struct {
	inner trust.HTTPSGetter

	mu      sync.Mutex
	records []record
}

type record struct {
	url    string
	header map[string][]string
	body   []byte
}

func (g *recordingGetter) Get(url string) (map[string][]string, []byte, error) {
	header, body, err := g.inner.Get(url)
	if err != nil {
		return header, body, err
	}
	g.mu.Lock()
	g.records = append(g.records, record{url: url, header: header, body: body})
	g.mu.Unlock()
	return header, body, nil
}

// collateralJSON is the shape `celestia-appd tx teeism submit-attestation` reads.
type collateralJSON struct {
	PckCrl                string `json:"pck_crl"`
	PckCrlIssuerChain     string `json:"pck_crl_issuer_chain"`
	TcbInfo               string `json:"tcb_info"`
	TcbInfoIssuerChain    string `json:"tcb_info_issuer_chain"`
	QeIdentity            string `json:"qe_identity"`
	QeIdentityIssuerChain string `json:"qe_identity_issuer_chain"`
	RootCaCrl             string `json:"root_ca_crl"`
}

func main() {
	quotePath := flag.String("quote", "-", "file holding the hex-encoded TDX quote, or - for stdin")
	outPath := flag.String("out", "-", "where to write the collateral JSON, or - for stdout")
	flag.Parse()

	if err := run(*quotePath, *outPath); err != nil {
		fmt.Fprintf(os.Stderr, "teeism-collateral: %v\n", err)
		os.Exit(1)
	}
}

func run(quotePath, outPath string) error {
	quote, err := readQuote(quotePath)
	if err != nil {
		return err
	}

	parsed, err := abi.QuoteToProto(quote)
	if err != nil {
		return fmt.Errorf("parsing quote: %w", err)
	}
	quoteV4, ok := parsed.(*pb.QuoteV4)
	if !ok {
		return fmt.Errorf("only TDX quote v4 is supported")
	}

	// Verify online once, recording everything the verifier reaches for.
	getter := &recordingGetter{inner: trust.DefaultHTTPSGetter()}
	now := time.Now()
	if err := verify.TdxQuote(quoteV4, &verify.Options{
		CheckRevocations: true,
		GetCollateral:    true,
		Getter:           getter,
		Now:              now,
	}); err != nil {
		return fmt.Errorf("the quote does not verify against live Intel collateral: %w", err)
	}

	collateral, err := collect(getter.records)
	if err != nil {
		return err
	}

	// The bundle has to satisfy the same function the keeper runs, or it would
	// fail on chain after the relayer had already paid for the transaction.
	if _, _, err := types.VerifyQuote(quote, collateral, now); err != nil {
		return fmt.Errorf("collateral was assembled but does not verify offline: %w", err)
	}

	out, err := json.MarshalIndent(collateralJSON{
		PckCrl:                hexed(collateral.PckCrl),
		PckCrlIssuerChain:     hexed(collateral.PckCrlIssuerChain),
		TcbInfo:               hexed(collateral.TcbInfo),
		TcbInfoIssuerChain:    hexed(collateral.TcbInfoIssuerChain),
		QeIdentity:            hexed(collateral.QeIdentity),
		QeIdentityIssuerChain: hexed(collateral.QeIdentityIssuerChain),
		RootCaCrl:             hexed(collateral.RootCaCrl),
	}, "", "  ")
	if err != nil {
		return err
	}

	if outPath == "-" {
		fmt.Println(string(out))
		return nil
	}
	return os.WriteFile(outPath, append(out, '\n'), 0o644)
}

// collect maps recorded responses onto the fields verification will ask for,
// using the same routing the offline getter uses.
func collect(records []record) (*types.Collateral, error) {
	c := &types.Collateral{}
	for _, r := range records {
		switch {
		case strings.Contains(r.url, "/pckcrl"):
			c.PckCrl = r.body
			c.PckCrlIssuerChain = issuerChain(r.header, "Sgx-Pck-Crl-Issuer-Chain")
		case strings.Contains(r.url, "/tcb"):
			c.TcbInfo = r.body
			c.TcbInfoIssuerChain = issuerChain(r.header, "Tcb-Info-Issuer-Chain")
		case strings.Contains(r.url, "/qe/identity"):
			c.QeIdentity = r.body
			c.QeIdentityIssuerChain = issuerChain(r.header, "Sgx-Enclave-Identity-Issuer-Chain")
		default:
			c.RootCaCrl = r.body
		}
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("the online path did not yield a complete bundle: %w", err)
	}
	return c, nil
}

// issuerChain returns the header value verbatim, still URL-escaped, because that
// is the form the verifier unescapes and parses.
func issuerChain(header map[string][]string, name string) []byte {
	if v, ok := header[name]; ok && len(v) == 1 {
		return []byte(v[0])
	}
	return nil
}

func readQuote(path string) ([]byte, error) {
	var raw []byte
	var err error
	if path == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}

	text := strings.TrimSpace(string(raw))
	text = strings.TrimPrefix(text, "0x")
	quote, err := hex.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("quote must be hex: %w", err)
	}
	if len(quote) == 0 {
		return nil, fmt.Errorf("quote is empty")
	}
	return quote, nil
}

func hexed(b []byte) string { return "0x" + hex.EncodeToString(b) }
