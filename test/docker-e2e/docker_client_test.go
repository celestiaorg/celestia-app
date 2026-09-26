package docker_e2e

import (
	"context"
	"errors"
	"net/netip"
	"testing"

	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"
)

type containerCreateClient struct {
	tastoratypes.TastoraDockerClient
	create func(context.Context, client.ContainerCreateOptions) (client.ContainerCreateResult, error)
}

func (c containerCreateClient) ContainerCreate(ctx context.Context, opts client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	return c.create(ctx, opts)
}

func TestDynamicPortClient(t *testing.T) {
	rpcPort := network.MustParsePort("26657/tcp")
	grpcPort := network.MustParsePort("9090/tcp")
	bindings := network.PortMap{
		rpcPort: {
			{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "40000"},
			{HostIP: netip.MustParseAddr("::1"), HostPort: "40001"},
		},
		grpcPort: {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "40002"}},
	}
	opts := client.ContainerCreateOptions{
		Name: "validator",
		Config: &container.Config{
			Image:        "celestia-app:test",
			ExposedPorts: network.PortSet{rpcPort: {}, grpcPort: {}},
		},
		HostConfig: &container.HostConfig{
			PortBindings:    bindings,
			PublishAllPorts: true,
			Binds:           []string{"data:/home/celestia"},
		},
	}
	ctx := context.Background()
	wantResult := client.ContainerCreateResult{ID: "container-id"}
	c := dynamicPortClient{containerCreateClient{create: func(gotCtx context.Context, got client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
		require.Equal(t, ctx, gotCtx)
		require.Equal(t, opts.Name, got.Name)
		require.Equal(t, opts.Config, got.Config)
		require.Equal(t, opts.HostConfig.Binds, got.HostConfig.Binds)
		require.True(t, got.HostConfig.PublishAllPorts)
		require.Len(t, got.HostConfig.PortBindings, len(bindings))
		for port, original := range bindings {
			actual := got.HostConfig.PortBindings[port]
			require.Len(t, actual, len(original))
			for i := range original {
				require.Equal(t, original[i].HostIP, actual[i].HostIP)
				require.Empty(t, actual[i].HostPort)
			}
		}
		return wantResult, nil
	}}}
	result, err := c.ContainerCreate(ctx, opts)
	require.NoError(t, err)
	require.Equal(t, wantResult, result)
	// Do not mutate the configuration owned by the caller.
	require.Equal(t, "40000", bindings[rpcPort][0].HostPort)
	require.Equal(t, "40001", bindings[rpcPort][1].HostPort)
	require.Equal(t, "40002", bindings[grpcPort][0].HostPort)
}

func TestDynamicPortClientWithoutBindings(t *testing.T) {
	for _, hostConfig := range []*container.HostConfig{nil, {}, {NetworkMode: "host"}} {
		opts := client.ContainerCreateOptions{HostConfig: hostConfig}
		wantErr := errors.New("create failed")
		c := dynamicPortClient{containerCreateClient{create: func(_ context.Context, got client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
			require.Equal(t, opts, got)
			return client.ContainerCreateResult{}, wantErr
		}}}
		_, err := c.ContainerCreate(context.Background(), opts)
		require.ErrorIs(t, err, wantErr)
	}
}
