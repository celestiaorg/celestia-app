package e2e

var defaultResources = Resources{
	MemoryRequest: "200Mi",
	MemoryLimit:   "200Mi",
	CPU:           "300m",
	Volume:        "1Gi",
}

var maxValidatorResources = Resources{
	MemoryRequest: "12Gi",
	MemoryLimit:   "20Gi",
	CPU:           "8",
	Volume:        "20Gi",
}

var maxTxsimResources = Resources{
	MemoryRequest: "1Gi",
	MemoryLimit:   "3Gi",
	CPU:           "2",
	Volume:        "1Gi",
}
