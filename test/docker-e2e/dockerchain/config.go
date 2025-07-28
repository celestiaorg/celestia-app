package dockerchain

import (
	"github.com/celestiaorg/celestia-app/v5/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v5/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v5/test/util/testnode"
	"github.com/moby/moby/client"
	"os"
)

const (
	multiplexerImage   = "ghcr.io/celestiaorg/celestia-app"
	defaultCelestiaTag = "v4.0.7-mocha"
)

// Config represents the configuration for a docker Celestia setup.
type Config struct {
	*testnode.Config
	Image           string
	Tag             string
	DockerClient    *client.Client
	DockerNetworkID string
}

// DefaultConfig returns a configured instance of Config with a custom genesis and validators.
func DefaultConfig(client *client.Client, network string) *Config {
	tnCfg := testnode.DefaultConfig()
	// default + 2 extra validators.
	tnCfg.Genesis = tnCfg.Genesis.
		WithChainID(appconsts.TestChainID).
		WithValidators(
			// TODO: ensure names are in lexicographical order.
			// this is because keyrings.Records() returns them this way.
			// we should come up with a proper fix as this is a big foot gun.
			genesis.NewDefaultValidator("validator1"),
			genesis.NewDefaultValidator("validator2"),
		)

	cfg := &Config{}
	return cfg.
		WithConfig(tnCfg).
		WithImage(GetCelestiaImage()).
		WithTag(GetCelestiaTag()).
		WithDockerClient(client).
		WithDockerNetworkID(network)
}

// WithConfig sets the testnode config and returns the Config.
func (c *Config) WithConfig(config *testnode.Config) *Config {
	c.Config = config
	return c
}

// WithImage sets the docker image and returns the Config.
func (c *Config) WithImage(image string) *Config {
	c.Image = image
	return c
}

// WithTag sets the docker tag and returns the Config.
func (c *Config) WithTag(tag string) *Config {
	c.Tag = tag
	return c
}

// WithDockerClient sets the docker client and returns the Config.
func (c *Config) WithDockerClient(client *client.Client) *Config {
	c.DockerClient = client
	return c
}

// WithDockerNetworkID sets the docker network ID and returns the Config.
func (c *Config) WithDockerNetworkID(networkID string) *Config {
	c.DockerNetworkID = networkID
	return c
}

// GetCelestiaImage returns the image to use for Celestia app.
// It can be overridden by setting the CELESTIA_IMAGE environment.
func GetCelestiaImage() string {
	if image := os.Getenv("CELESTIA_IMAGE"); image != "" {
		return image
	}
	return multiplexerImage
}

// GetCelestiaTag returns the tag to use for Celestia images.
// It can be overridden by setting the CELESTIA_TAG environment.
func GetCelestiaTag() string {
	if tag := os.Getenv("CELESTIA_TAG"); tag != "" {
		return tag
	}
	return defaultCelestiaTag
}
