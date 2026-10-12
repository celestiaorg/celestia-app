package types

import (
	"crypto/x509"
)

const (
	// MaxTrustedRootCerts and MaxTrustedRootCertBytes bound what an override may
	// store in state.
	MaxTrustedRootCerts     = 4
	MaxTrustedRootCertBytes = 4096
)

// IsEmpty reports whether the overrides change nothing, in which case they are
// not stored at all.
func (o *VerifierOverrides) IsEmpty() bool {
	return o == nil || len(o.TrustedRootCerts) == 0
}

// Validate rejects overrides the verifier could not apply. Nil overrides are valid
// and mean none.
func (o *VerifierOverrides) Validate() error {
	if o == nil {
		return nil
	}
	if len(o.TrustedRootCerts) > MaxTrustedRootCerts {
		return ErrInvalidVerifierOverrides.Wrapf("at most %d trusted root certs, got %d", MaxTrustedRootCerts, len(o.TrustedRootCerts))
	}
	for i, der := range o.TrustedRootCerts {
		if len(der) > MaxTrustedRootCertBytes {
			return ErrInvalidVerifierOverrides.Wrapf("trusted root cert %d is %d bytes, limit is %d", i, len(der), MaxTrustedRootCertBytes)
		}
		if _, err := x509.ParseCertificate(der); err != nil {
			return ErrInvalidVerifierOverrides.Wrapf("trusted root cert %d is not a DER certificate: %s", i, err)
		}
	}
	return nil
}

// trustedRoots returns the roots that replace the verifier's embedded Intel root,
// or nil to keep it.
func (o *VerifierOverrides) trustedRoots() (*x509.CertPool, error) {
	if o == nil || len(o.TrustedRootCerts) == 0 {
		return nil, nil
	}
	pool := x509.NewCertPool()
	for i, der := range o.TrustedRootCerts {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, ErrInvalidVerifierOverrides.Wrapf("trusted root cert %d: %s", i, err)
		}
		pool.AddCert(cert)
	}
	return pool, nil
}
