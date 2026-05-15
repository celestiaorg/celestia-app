package types

const (
	ModuleName = "consensustimeouts"
	// StoreKey is the multistore key for the module. It cannot share a prefix
	// with another store key (the cosmos-sdk multistore rejects prefix
	// collisions), so we pick a value distinct from "consensus" (the sdk
	// x/consensus store key).
	StoreKey  = "ctimeouts"
	RouterKey = ModuleName
)

var ParamsKey = []byte("params")
