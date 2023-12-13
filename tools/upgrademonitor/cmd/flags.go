package cmd

// Flags
var (
	// grpcEndpoint is the endpoint of a consensus full node.
	grpcEndpoint string
	// pollFrequency is the frequency in seconds that upgrade monitor polls the
	// GRPC endpoint.
	pollFrequency int64
	// pathToTransaction is the file path to a signed transaction that will be
	// auto-published when the network is upgradeable.
	pathToTransaction string
)

// Defaults
var (
	// defaultGrpcEndpoint is the value used if the grpc-endpoint flag isn't provided.
	// This endpoint is the one enabled by default when you run ./scripts/single-node.sh
	defaultGrpcEndpoint = "0.0.0.0:9090"
	// defaultPollFrequency is the value used if the poll-frequency flag isn't provided.
	defaultPollFrequency = int64(10) // 10 seconds
	// defaultPathToTransaction is the value used if the auto-publish flag isn't provided.
	defaultPathToTransaction = ""
)
