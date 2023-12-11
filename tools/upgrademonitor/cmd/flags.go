package cmd

// Flags
var (
	// version is the version number that should be monitored.
	version uint64
	// grpcEndpoint is the endpoint of a consensus full node.
	grpcEndpoint string
	// pollFrequency is the frequency in seconds that upgrade monitor polls the
	// GRPC endpoint.
	pollFrequency int64
	// autoTry is whether upgrademonitor will auto submit a MsgTryUpgrade if the
	// network version is upgradeable.
	autoTry bool
)

// Defaults
var (
	// defaultVersion is the value used if the version flag isn't provided. Since
	// v2 is coordinated via an upgrade-height, v3 is the first version that this
	// tool supports.
	defaultVersion = uint64(3)
	// defaultGrpcEndpoint is the value used if the grpc-endpoint flag isn't provided.
	// This endpoint is the one enabled by default when you run ./scripts/single-node.sh
	defaultGrpcEndpoint = "0.0.0.0:9090"
	// defaultPollFrequency is the value used if the poll-frequency flag isn't provided.
	// TODO (@rootulp) consider making this 10 seconds
	defaultPollFrequency = int64(1) // 1 second
	// defaultAutoTry is the value used if the auto-try flag isn't provided.
	defaultAutoTry = true
)
