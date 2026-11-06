package types

import "github.com/cosmos/gogoproto/proto"

// This file ensures types.proto is registered before every file that imports it.
//
// Go runs init() functions in file name order. events.pb.go, genesis.pb.go and
// tx.pb.go all sort before types.pb.go, so each would register its proto file
// while types.proto is not yet in the registry, and the messages they nest from it
// (EnclaveIdentity, VerifierOverrides, Collateral, InterchainSecurityModule)
// become placeholders with no fields. Amino JSON signing then rejects every field
// of those messages as unknown, so a Ledger or multisig could not sign
// MsgUpdateEnclaveIdentity, MsgSetVerifierOverrides or MsgSubmitAttestation.
//
// This file ("descriptor_init.go") sorts before all of them, so its init() runs
// first and registers types.proto. The same fix as hyperlane-cosmos'
// x/core/01_interchain_security/types/proto_init.go, named to also precede
// events.pb.go.
//
// fileDescriptor_dd6bd2e8c12c1b66 is the package-level []byte var in types.pb.go.
// Go initializes all package-level variables before any init() functions, so it
// is available here.
func init() {
	proto.RegisterFile("celestia/teeism/v1/types.proto", fileDescriptor_dd6bd2e8c12c1b66)
}
