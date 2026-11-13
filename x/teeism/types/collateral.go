package types

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

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

// IssuedAt is when the collateral was issued: the oldest of the TCB info's and
// QE identity's issueDate and the PCK CRL's thisUpdate. The root CA CRL is left
// out; Intel issues it about once a year.
//
// Read before verification and without checking signatures, so it decides only
// whether the submission is too old to be looked at; an artifact whose date was
// forged still has to carry Intel's signature over that date to verify.
func (c *Collateral) IssuedAt() (time.Time, error) {
	tcb, err := signedIssueDate(c.TcbInfo, "tcbInfo")
	if err != nil {
		return time.Time{}, ErrInvalidCollateral.Wrapf("tcb_info: %s", err)
	}
	qe, err := signedIssueDate(c.QeIdentity, "enclaveIdentity")
	if err != nil {
		return time.Time{}, ErrInvalidCollateral.Wrapf("qe_identity: %s", err)
	}
	crl, err := x509.ParseRevocationList(c.PckCrl)
	if err != nil {
		return time.Time{}, ErrInvalidCollateral.Wrapf("pck_crl: %s", err)
	}
	oldest := tcb
	for _, t := range []time.Time{qe, crl.ThisUpdate} {
		if t.Before(oldest) {
			oldest = t
		}
	}
	return oldest, nil
}

// signedIssueDate reads the issueDate of the signed object under key.
func signedIssueDate(body []byte, key string) (time.Time, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return time.Time{}, err
	}
	var signed struct {
		IssueDate string `json:"issueDate"`
	}
	if err := json.Unmarshal(doc[key], &signed); err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", key, err)
	}
	t, err := time.Parse(time.RFC3339, signed.IssueDate)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s.issueDate: %w", key, err)
	}
	return t, nil
}

// CheckCollateralFreshness is the rule every submission's collateral must meet:
// issued no more than MaxCollateralAgeSecs before block time, and no more than
// MaxCollateralRegressionSecs older than the newest collateral the ism has
// accepted (newest, in unix seconds; zero before the first attestation).
func CheckCollateralFreshness(issued time.Time, blockTime time.Time, newest uint64) error {
	if blockTime.Sub(issued) > MaxCollateralAgeSecs*time.Second {
		return ErrInvalidCollateral.Wrapf("collateral issued at %s is more than %ds old at block time %s; fetch current collateral",
			issued.UTC().Format(time.RFC3339), MaxCollateralAgeSecs, blockTime.UTC().Format(time.RFC3339))
	}
	if issued.Unix() < 0 {
		return ErrInvalidCollateral.Wrap("collateral issue time precedes 1970")
	}
	if uint64(issued.Unix())+MaxCollateralRegressionSecs < newest {
		return ErrInvalidCollateral.Wrapf("collateral issued at %s is older than what this ism last accepted (%s); fetch current collateral",
			issued.UTC().Format(time.RFC3339), time.Unix(int64(newest), 0).UTC().Format(time.RFC3339))
	}
	return nil
}

// PcsURLs reports the URLs an offline getter is expected to answer, so the
// tooling that assembles collateral fetches exactly what verification will ask
// for.
func PcsURLs(ca, fmspc string) (pckCrl, tcbInfo, qeIdentity string) {
	return pcs.PckCrlURL(ca), pcs.TcbInfoURL(fmspc), pcs.QeIdentityURL()
}
