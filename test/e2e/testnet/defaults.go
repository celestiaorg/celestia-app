package testnet

import "k8s.io/apimachinery/pkg/api/resource"

var DefaultResources = Resources{
	MemoryRequest: resource.MustParse("400Mi"),
	MemoryLimit:   resource.MustParse("400Mi"),
	CPU:           resource.MustParse("300m"),
	Volume:        resource.MustParse("1Gi"),
}

const (
	TxsimVersion = "pr-3541"
	MB           = 1000 * 1000
	GB           = 1000 * MB
	MiB          = 1024 * 1024
	GiB          = 1024 * MiB
)
