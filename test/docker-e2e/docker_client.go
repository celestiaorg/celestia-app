package docker_e2e

import (
	"context"
	"slices"

	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// dynamicPortClient lets Docker allocate host ports for the test containers.
// Tastora v0.21.1 chooses ports using temporary listeners, then closes them
// before starting the container. Another process can claim a port in that gap.
// The tests discover published ports through container inspection, so they do
// not need the host ports selected by Tastora.
type dynamicPortClient struct {
	tastoratypes.TastoraDockerClient
}

func (c dynamicPortClient) ContainerCreate(ctx context.Context, opts client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	if opts.HostConfig != nil && len(opts.HostConfig.PortBindings) > 0 {
		hostConfig := *opts.HostConfig
		hostConfig.PortBindings = make(network.PortMap, len(opts.HostConfig.PortBindings))
		for port, bindings := range opts.HostConfig.PortBindings {
			bindings = slices.Clone(bindings)
			for i := range bindings {
				// An empty host port asks Docker to allocate an available port.
				// Keep HostIP so localhost bindings remain localhost-only.
				bindings[i].HostPort = ""
			}
			hostConfig.PortBindings[port] = bindings
		}
		opts.HostConfig = &hostConfig
	}
	return c.TastoraDockerClient.ContainerCreate(ctx, opts)
}
