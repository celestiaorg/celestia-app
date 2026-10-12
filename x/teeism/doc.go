// The x/teeism module is a custom implementation of a Hyperlane Interchain Security Module (ISM).
// Its purpose is to provide a means for proving execution of a remote chain through a standardised interface, the ISM.
// Thus, we leverage the transport semantics of the Hyperlane messaging protocol, while providing a custom security mechanism.
//
// An ISM is advanced by attestations (MsgSubmitAttestation): a TDX quote from the pinned enclave,
// verified against Intel's collateral at block time, whose report data commits to the next
// trusted state and the messages it authorizes.
//
// Each ISM has an owner, the creator to begin with, who controls:
//
//   - MsgUpdateEnclaveIdentity (tx teeism update-identity): re-pin the enclave, keeping the
//     ISM's state, for a new image, a platform update or a replacement enclave.
//   - MsgUpdateOwner (tx teeism update-owner): hand the ISM and these controls to a new owner,
//     such as a multisig. There is no renouncing.
//   - MsgSetVerifierOverrides (tx teeism set-verifier-overrides): override Intel's side of
//     verification when something Intel publishes or anchors breaks: trust a different root
//     CA, accept collateral that lapsed within a grace window, or skip Intel's verdict
//     entirely (skip_dcap), in which case only the owner may submit attestations.
//
// The overrides make an ISM accept attestations Intel would not vouch for, so they extend the
// trust routers place in the owner. Each change emits an event.
package teeism
