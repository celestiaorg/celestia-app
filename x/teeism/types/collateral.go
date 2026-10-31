package types

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/google/go-tdx-guest/pcs"
)

// Header names Intel's PCS uses to carry issuer chains alongside each artifact.
// go-tdx-guest reads the chain from these, so an offline getter has to reproduce
// them exactly.
const (
	pckCrlIssuerChainHeader     = "Sgx-Pck-Crl-Issuer-Chain"
	tcbInfoIssuerChainHeader    = "Tcb-Info-Issuer-Chain"
	qeIdentityIssuerChainHeader = "Sgx-Enclave-Identity-Issuer-Chain"
)

// offlineGetter serves Intel PCS artifacts out of a transaction instead of over
// the network.
//
// Consensus cannot make HTTP calls: every validator has to reach the same verdict
// from the same bytes, and a network fetch is neither reproducible nor
// synchronous. Carrying the artifacts in the transaction costs nothing in
// security, because each one is signed by Intel and those signatures are checked
// during verification exactly as they would be had they arrived over the wire.
type offlineGetter struct {
	collateral *Collateral
}

// Get routes a PCS URL to the matching artifact carried in the transaction.
//
// It matches on path rather than on the full URL so that the Intel host name is
// not load-bearing, and returns an error rather than a zero value for anything it
// was not given, so a missing artifact fails verification instead of silently
// weakening it.
func (g offlineGetter) Get(url string) (map[string][]string, []byte, error) {
	c := g.collateral

	switch {
	case strings.Contains(url, "/pckcrl"):
		return issuerChainHeader(pckCrlIssuerChainHeader, c.PckCrlIssuerChain), c.PckCrl, require("pck crl", c.PckCrl)

	case strings.Contains(url, "/tcb"):
		return issuerChainHeader(tcbInfoIssuerChainHeader, c.TcbInfoIssuerChain), c.TcbInfo, require("tcb info", c.TcbInfo)

	case strings.Contains(url, "/qe/identity"):
		return issuerChainHeader(qeIdentityIssuerChainHeader, c.QeIdentityIssuerChain), c.QeIdentity, require("qe identity", c.QeIdentity)
	}

	// Anything else is the root CA CRL, whose URL comes from a CRL distribution
	// point inside the QE identity issuer's root certificate rather than from a
	// fixed PCS path.
	return nil, c.RootCaCrl, require("root ca crl", c.RootCaCrl)
}

func require(what string, b []byte) error {
	if len(b) == 0 {
		return fmt.Errorf("collateral is missing the %s", what)
	}
	return nil
}

// issuerChainHeader reproduces the response header Intel serves an artifact with.
// The value is carried verbatim, still URL-escaped, because that is the form
// go-tdx-guest unescapes and parses.
func issuerChainHeader(name string, chain []byte) map[string][]string {
	if len(chain) == 0 {
		return nil
	}
	return map[string][]string{name: {string(chain)}}
}

// Validate rejects collateral that is missing an artifact verification needs.
func (c *Collateral) Validate() error {
	if c == nil {
		return ErrInvalidCollateral.Wrap("collateral is required")
	}
	for _, f := range []struct {
		name string
		val  []byte
	}{
		{"pck_crl", c.PckCrl},
		{"pck_crl_issuer_chain", c.PckCrlIssuerChain},
		{"tcb_info", c.TcbInfo},
		{"tcb_info_issuer_chain", c.TcbInfoIssuerChain},
		{"qe_identity", c.QeIdentity},
		{"qe_identity_issuer_chain", c.QeIdentityIssuerChain},
		{"root_ca_crl", c.RootCaCrl},
	} {
		if len(f.val) == 0 {
			return ErrInvalidCollateral.Wrapf("%s must not be empty", f.name)
		}
	}
	if err := checkSignedKey(c.TcbInfo, "tcbInfo"); err != nil {
		return ErrInvalidCollateral.Wrapf("tcb_info: %s", err)
	}
	if err := checkSignedKey(c.QeIdentity, "enclaveIdentity"); err != nil {
		return ErrInvalidCollateral.Wrapf("qe_identity: %s", err)
	}
	return nil
}

// checkSignedKey requires body to be one JSON object in which signed is the
// only top-level key that matches it case-insensitively.
//
// go-tdx-guest checks Intel's signature over the value under the exact key but
// reads its verdict through a case-insensitive struct decode, so a second key
// such as "TCBINFO" would be read without ever being signed. Other keys are
// left alone, so a field Intel adds later does not stop verification.
func checkSignedKey(body []byte, signed string) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return errors.New("must be a JSON object")
	}
	found := false
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		key, _ := tok.(string)
		if strings.EqualFold(key, signed) {
			if key != signed || found {
				return fmt.Errorf("%q must appear exactly once, spelled exactly", signed)
			}
			found = true
		}
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return err
		}
	}
	if _, err := dec.Token(); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after the JSON object")
	}
	if !found {
		return fmt.Errorf("%q is missing", signed)
	}
	return nil
}

// PcsURLs reports the URLs an offline getter is expected to answer, so the
// tooling that assembles collateral fetches exactly what verification will ask
// for.
func PcsURLs(ca, fmspc string) (pckCrl, tcbInfo, qeIdentity string) {
	return pcs.PckCrlURL(ca), pcs.TcbInfoURL(fmspc), pcs.QeIdentityURL()
}
