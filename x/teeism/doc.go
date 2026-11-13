// The x/teeism module is a custom implementation of a Hyperlane Interchain Security Module (ISM).
// Its purpose is to provide a means for proving execution of a remote chain through a standardised interface, the ISM.
// Thus, we leverage the transport semantics of the Hyperlane messaging protocol, while providing a custom security mechanism.
//
// An ISM pins a set of 1 to 8 enclave identities, any one of which may advance it (1-of-N): for
// redundancy, and so a rotation never refuses an attestation (pin {old, new}, move the relayer,
// pin {new}). The trusted state carries the set's digest (types.IdentitySetDigest); a set of one
// carries its member's digest.
//
// An ISM is advanced by attestations (MsgSubmitAttestation): a TDX quote from one of the pinned
// enclaves, verified against Intel's collateral at block time, whose report data commits to the
// next trusted state and the origin merkle tree root at it. Every root ever attested stays
// accepted.
//
// A message is delivered with a merkle branch in its metadata (types.DeliveryMetadata), which
// places it under one of those roots. So an attestation is the same size whatever the origin
// dispatched, a message dispatched while the relayer was away is deliverable against any later
// root, and someone else advancing the ISM first only gives a message a newer root to prove
// against.
//
// Each ISM has an owner, the creator to begin with, who controls:
//
//   - MsgSetEnclaveIdentities (tx teeism set-identities): replace the set of enclaves, keeping
//     the ISM's state, for a new image, a platform update, an added or a retired enclave.
//   - MsgUpdateOwner (tx teeism update-owner): hand the ISM and these controls to a new owner,
//     such as a multisig. There is no renouncing.
//   - MsgSetVerifierOverrides (tx teeism set-verifier-overrides): replace the Intel root CA
//     quotes must chain to, for when Intel rotates it.
//
// None of these relaxes verification: every attestation must carry a quote that fully
// verifies, with unexpired collateral, against a trusted root. Each change emits an event.
package teeism
