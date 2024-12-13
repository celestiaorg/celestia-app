package testnet

import "k8s.io/apimachinery/pkg/api/resource"

var DefaultResources = Resources{
	MemoryRequest: resource.MustParse("4Gi"),
	MemoryLimit:   resource.MustParse("4Gi"),
	CPU:           resource.MustParse("1"),
	Volume:        resource.MustParse("1Gi"),
}

const (
	TxsimVersion = "8e573bb"
	MB           = 1000 * 1000
	GB           = 1000 * MB
	MiB          = 1024 * 1024
	GiB          = 1024 * MiB
)
