package testnet

var DefaultResources = Resources{
	MemoryRequest: "500Mi",
	MemoryLimit:   "500Mi",
	CPU:           "300m",
	Volume:        "1Gi",
}

const (
	TxsimVersion = "pr-3541"
	MB           = 1000 * 1000
	GB           = 1000 * MB
	MiB          = 1024 * 1024
	GiB          = 1024 * MiB
)
