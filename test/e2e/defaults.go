package e2e

var defaultResources = Resources{
	memoryRequest: "200Mi",
	memoryLimit:   "200Mi",
	cpu:           "300m",
	volume:        "1Gi",
}

var maxValidatorResources = Resources{
	memoryRequest: "10Gi",
	memoryLimit:   "12Gi",
	cpu:           "6",
	volume:        "1Gi",
}

var maxTxSimResources = Resources{
	memoryRequest: "1Gi",
	memoryLimit:   "1Gi",
	cpu:           "2",
	volume:        "1Gi",
}
