package testnet

import "k8s.io/apimachinery/pkg/api/resource"

var DefaultResources = Resources{
	MemoryRequest: resource.MustParse("1Gi"),
	MemoryLimit:   resource.MustParse("1Gi"),
	CPU:           resource.MustParse("500m"),
	Volume:        resource.MustParse("10Gi"),
}

const (
	TxsimVersion = "v3.3.1"
	MB           = 1000 * 1000
	GB           = 1000 * MB
	MiB          = 1024 * 1024
	GiB          = 1024 * MiB
)
