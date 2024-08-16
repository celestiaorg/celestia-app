package testnet

var DefaultResources = Resources{
	MemoryRequest: "3Gi",
	MemoryLimit:   "3Gi",
	CPU:           "1000m",
	Volume:        "2Gi",
}

const (
	TxsimVersion = "pr-3541"
	MB           = 1000 * 1000
	GB           = 1000 * MB
	MiB          = 1024 * 1024
	GiB          = 1024 * MiB
)
