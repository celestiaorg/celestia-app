# Fibre TLS Identity

This page specifies version 1 of the Fibre validator-endorsed TLS identity (privval domain `celestia-fibre-tls-v1`): the format by which a Fibre server binds its ephemeral TLS keypair to its validator consensus key. It is a Celestia-specific scheme — not libp2p-TLS compatible — and binds no network location (IP, DNS name, or SNI).

## Certificate and extension

A Fibre server presents a self-signed X.509 v3 certificate over TLS 1.3 with:

- an ephemeral Ed25519 TLS key, freshly generated on every server start
- key usage `digitalSignature`; extended key usages `serverAuth` and `clientAuth`
- `NotBefore`/`NotAfter` equal, at unix-second precision, to the signed window below; the issuing server sets `notBefore = now − 5min` and `notAfter = now + 365d`, truncated to seconds
- a non-critical extension with OID `1.3.6.1.4.1.66463.1.1` (see [OID allocations](./fibre_server.md#oid-allocations))

The extension's `extnValue` OCTET STRING contains the DER encoding of:

```text
SignedIdentity ::= SEQUENCE {
    payload   OCTET STRING,  -- DER of BindingPayload: the exact signed bytes
    signature OCTET STRING   -- consensus-key endorsement (Ed25519, 64 bytes)
}

BindingPayload ::= SEQUENCE {
    version    INTEGER,      -- 1
    notBefore  INTEGER,      -- unix seconds
    notAfter   INTEGER,      -- unix seconds
    tlsPubKey  OCTET STRING  -- SubjectPublicKeyInfo DER of the TLS key
                             -- (44 bytes, algorithm OID 1.3.101.112)
}
```

DER rules apply throughout: definite lengths, minimal INTEGER encoding. The consensus public key is not embedded anywhere in the certificate: the verifier takes the expected validator key from the validator set, and a signature that verifies under that key is the proof of endorsement.

## Signed bytes

The endorsement signature is Ed25519 by the validator consensus key over exactly these bytes:

```text
raw         = "celestia-fibre-tls:" || DER(BindingPayload)
P           = protobuf(SignRawBytesRequest{
                  chain_id  = <chain ID>,
                  raw_bytes = raw,
                  unique_id = "celestia-fibre-tls-v1",
              })
signedBytes = "COMET::RAW_BYTES::SIGN" || uvarint(len(P)) || P
```

`SignRawBytesRequest` is the CometBFT privval message with fields `chain_id` (1, `string`), `raw_bytes` (2, `bytes`), `unique_id` (3, `string`), encoded in ascending field-number order with no other fields. `uvarint(len(P))` is the base-128 protobuf varint of the length of `P`.

The chain ID is the runtime chain ID; it is part of the signing envelope but not of `BindingPayload`. The verifier computes the signed bytes from the payload bytes embedded in the extension, byte-for-byte — it never re-encodes `BindingPayload`.

## Verification

Inputs: the peer's leaf certificate, the expected validator consensus public key (raw 32-byte Ed25519, from the validator set), the chain ID, and the verification time `now`. The verifier MUST reject the certificate unless all of the following hold, checked in order:

1. The certificate carries an extension with OID `1.3.6.1.4.1.66463.1.1`.
2. The extension value is at most 8192 bytes.
3. It parses as `SignedIdentity` with no trailing bytes.
4. `payload` is non-empty and at most 4096 bytes.
5. `signature` is non-empty.
6. `payload` parses as `BindingPayload` with no trailing bytes.
7. `version` equals `1`.
8. `signature` verifies under the expected consensus key over the signed bytes computed from the embedded `payload`.
9. The certificate's SubjectPublicKeyInfo DER equals `tlsPubKey` byte-for-byte (TLS 1.3 CertificateVerify proves possession of that key).
10. `notAfter > notBefore`.
11. `notAfter − notBefore` is at most 365 days + 10 minutes.
12. `notBefore − 5min ≤ now ≤ notAfter + 5min`.
13. The certificate's own `NotBefore`/`NotAfter`, as unix seconds, equal `notBefore`/`notAfter`.
14. The certificate carries the `serverAuth` extended key usage.

## Constants

Changing any of these is a protocol break:

| Constant             | Value                    |
|----------------------|--------------------------|
| Extension OID        | `1.3.6.1.4.1.66463.1.1`  |
| Privval unique ID    | `celestia-fibre-tls-v1`  |
| Payload sign prefix  | `celestia-fibre-tls:`    |
| Envelope prefix      | `COMET::RAW_BYTES::SIGN` |
| Binding version      | `1`                      |
| Max extension size   | 8192 bytes               |
| Max payload DER size | 4096 bytes               |
| Max validity window  | 365 days + 10 minutes    |
| Clock-skew tolerance | 5 minutes                |

## Golden vectors

Canonical vectors live at [`fibre/internal/tlsid/testdata/identity_vectors.json`](https://github.com/celestiaorg/celestia-app/blob/main/fibre/internal/tlsid/testdata/identity_vectors.json). The file has a `constants` object mirroring the table above and a `cases` array; all byte fields are lowercase hex. Per case:

| Field                                                      | Meaning                                                                  |
|------------------------------------------------------------|--------------------------------------------------------------------------|
| `name`, `description`                                      | case identifier and what it exercises                                    |
| `chain_id`                                                 | chain ID the producer signed under                                       |
| `consensus_priv_seed`                                      | RFC 8032 Ed25519 seed of the endorsing consensus key (test key)          |
| `consensus_pub`                                            | its raw 32-byte public key                                               |
| `tls_priv_seed`                                            | Ed25519 seed of the TLS key endorsed in the payload (test key)           |
| `tls_pub_raw`                                              | raw 32-byte TLS public key                                               |
| `tls_pub_spki_der`                                         | its SubjectPublicKeyInfo DER                                             |
| `binding`                                                  | `version`, `not_before`, `not_after` (unix seconds)                      |
| `payload_der`                                              | DER of `BindingPayload`                                                  |
| `sign_input`                                               | `"celestia-fibre-tls:" ‖ payload_der` (the `raw_bytes` field value)      |
| `signed_bytes`                                             | the full envelope the consensus key signs                                |
| `signature`                                                | the signature embedded in the extension (tampered in the tampering case) |
| `extension_der`                                            | the extension's `extnValue` contents                                     |
| `cert_serial`                                              | certificate serial (decimal)                                             |
| `cert_der`                                                 | the certificate presented to the verifier                                |
| `verifier_chain_id`, `verifier_consensus_pub`, `verify_at` | the three verifier inputs                                                |
| `expected`                                                 | `valid` (bool) and, when invalid, `error`                                |

Every MUST-reject check has at least one case. `expected.error` values map to the verification checks: `extension_missing` (1), `extension_too_large` (2), `extension_malformed` and `extension_trailing_data` (3), `payload_empty` and `payload_too_large` (4), `signature_empty` (5), `payload_malformed` and `binding_trailing_data` (6), `unsupported_version` (7), `signature_invalid` (8), `tls_key_mismatch` (9), `window_empty` (10), `window_too_long` (11), `outside_validity_window` (12), `cert_window_mismatch` (13), `eku_missing` (14).

Cases that corrupt a layer omit the fields beneath it: `binding` is absent when the payload is not a valid `BindingPayload`, and the payload/signature fields are empty when the extension is missing or not a valid `SignedIdentity`. `cert_der`, the verifier inputs, and `expected` are always present.

A verifier implementation should reproduce `payload_der` → `sign_input` → `signed_bytes` from the case inputs, verify `signature`, and check `cert_der` at `verify_at` expecting `expected`. The private seeds let producer implementations regenerate and byte-compare their own output. Vectors are only regenerated deliberately (`go test ./fibre/internal/tlsid -run TestGoldenVectors -update`), so a vendored copy stays valid until a protocol-breaking change — which by definition arrives with a new binding version and unique ID.
