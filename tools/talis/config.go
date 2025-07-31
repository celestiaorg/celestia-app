package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"

	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
)

type NodeType string

const (
	// Validator represents a validator node in the network.
	Validator NodeType = "validator"
	// Bridge represents a bridge node in the network.
	Bridge NodeType = "bridge"
	// Light represents a light node in the network.
	Light NodeType = "light"
)

var (
	valCount   = atomic.Uint32{}
	nodeCount  = atomic.Uint32{}
	lightCount = atomic.Uint32{}
)

// NodeName returns the name of the node based on its type and index. The
// name is in the format "<node_type>-<index>". For example, "validator-0" or
// "bridge-1". Index is a global counter that is incremented for each node created.
func NodeName(nodeType NodeType) string {
	index := 0
	switch nodeType {
	case Validator:
		index = int(valCount.Add(1)) - 1
	case Bridge:
		index = int(nodeCount.Add(1)) - 1
	case Light:
		index = int(lightCount.Add(1)) - 1
	default:
		panic(fmt.Sprintf("unknown node type: %s", nodeType))
	}
	return fmt.Sprintf("%s-%d", nodeType, index)
}

// Provider simply marks the provider the instance config should target.
type Provider string

const (
	// DO represents DigitalOcean as a provider.
	DigitalOcean Provider = "digitalocean"
	// Linode represents Linode as a provider.
	Linode Provider = "linode"
)

// Instance represents a single instance in the network. It contains
// information about the instance such as its public and private IP address,
// provider, region, and name. It also contains a list of tags that are
// attached to the instance.
type Instance struct {
	NodeType NodeType `json:"node_type"`
	// PublicIP is the public IP address of the instance.
	PublicIP string `json:"public_ip"`
	// PrivateIP is the private IP address of the instance.
	PrivateIP string `json:"private_ip"`
	// Provider is the provider of the instance. For example, "digitalocean" or
	// "aws".
	Provider Provider `json:"provider"`
	// Slug is a provider specific string that determines what type of instance
	// the node is ran on.
	Slug string `json:"slug"`
	// Region is the region in which the instance is created. For example,
	// "nyc1" for DigitalOcean or "us-east-1" for AWS.
	Region string `json:"region"`
	// Name is the name of the instance. This is used to identify the instance
	// in the network and is also used as the hostname of the instance. It
	// therefore should be unique.
	Name string `json:"name"`
	// Tags are attached to every spun up instance. They are used to identify
	// the instance in the network, associate the instance with an experiment
	// and network, and mark as a talis instance.
	Tags []string `json:"tags"`
}

func NewBaseInstance(nodeType NodeType) Instance {
	name := NodeName(nodeType)
	return Instance{
		NodeType:  nodeType,
		PublicIP:  "TBD",
		PrivateIP: "TBD",
		Name:      name,
		Tags:      []string{appconsts.TalisChainID, string(nodeType), name},
	}
}

// Config describes the desired state of the network.
type Config struct {
	Validators []Instance `json:"validators"`
	Bridges    []Instance `json:"bridges,omitempty"`
	Lights     []Instance `json:"lights,omitempty"`

	// ChainID is the chain ID of the network. This is used to identify the
	// network and is also used as the chain ID of the network. It is
	// automatically prefixed with "talis-" by default. This is required to
	// increase the square size beyond the v4 limit of 128.
	ChainID string `json:"chain_id"`
	// Experiment is the experiment ID of the network. This is used to index which experiment
	// the network is associated with.
	Experiment string `json:"experiment"`
	// SSHPubKeyPath is the path to the SSH public key that will be added to
	// every instance.
	SSHPubKeyPath string `json:"ssh_pub_key_path"`
	// SSHKeyName is the name of the SSH key that will be used to access the
	// instances. This is used to identify the SSH key in the provider's
	// dashboard. If it's not already kept by the provider, the key will be
	// added.
	SSHKeyName string `json:"ssh_key_name"`
	// DigitalOceanToken is used to authenticate with DigitalOcean. It can be
	// provided via an env var or flag.
	DigitalOceanToken string `json:"digitalocean_token,omitempty"`
	// LinodeToken is used to authenticate with Linode. It can be provided via
	// an env var or flag.
	LinodeToken string `json:"linode_token,omitempty"`
	// S3Config is used to configure the S3 bucket that will be used to store
	// traces, logs, and other data.
	S3Config S3Config `json:"s3_config,omitempty"`
}

func NewConfig(experiment, chainID string) Config {
	return Config{
		Validators: []Instance{},
		Bridges:    []Instance{},
		Lights:     []Instance{},
		Experiment: experiment,
		ChainID:    TalisChainID(chainID),
		S3Config: S3Config{
			AccessKeyID:     os.Getenv(EnvVarAWSAccessKeyID),
			SecretAccessKey: os.Getenv(EnvVarAWSSecretAccessKey),
			BucketName:      os.Getenv(EnvVarS3Bucket),
			Region:          os.Getenv(EnvVarAWSRegion),
			Endpoint:        os.Getenv(EnvVarS3Endpoint),
		},
	}
}

func (cfg Config) WithSSHPubKeyPath(path string) Config {
	cfg.SSHPubKeyPath = path
	return cfg
}

func (cfg Config) WithSSHKeyName(name string) Config {
	cfg.SSHKeyName = name
	return cfg
}

func (cfg Config) WithDigitalOceanToken(token string) Config {
	cfg.DigitalOceanToken = token
	return cfg
}

func (cfg Config) WithLinodeToken(token string) Config {
	cfg.LinodeToken = token
	return cfg
}

func (cfg Config) WithS3Config(s3 S3Config) Config {
	cfg.S3Config = s3
	return cfg
}

func (cfg Config) WithDigitalOceanValidator(region string) Config {
	i := NewDigitalOceanValidator(region)
	cfg.Validators = append(cfg.Validators, i)
	return cfg
}

func (cfg Config) WithChainID(chainID string) Config {
	cfg.ChainID = TalisChainID(chainID)
	return cfg
}

func (c Config) Save(root string) error {
	// Create the directory if it doesn't exist
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}

	// Create the config file path
	configFilePath := filepath.Join(root, "config.json")

	cfgFile, err := os.OpenFile(configFilePath, os.O_RDWR|os.O_CREATE|os.O_SYNC, 0o755)
	if err != nil {
		return err
	}
	defer cfgFile.Close()

	// Write the config to the file
	encoder := json.NewEncoder(cfgFile)
	encoder.SetIndent("", "  ")
	return encoder.Encode(c)
}

// LoadConfig loads the config from the specified path.
func LoadConfig(rootDir string) (Config, error) {
	cfgFile, err := os.Open(filepath.Join(rootDir, "config.json"))
	if err != nil {
		return Config{}, err
	}
	defer cfgFile.Close()

	var cfg Config
	decoder := json.NewDecoder(cfgFile)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func TalisChainID(chainID string) string {
	return "talis-" + chainID
}

func (c Config) UpdateInstance(name, publicIP, privateIP string) (Config, error) {
	for i := range c.Validators {
		if c.Validators[i].Name == name {
			c.Validators[i].PublicIP = publicIP
			c.Validators[i].PrivateIP = privateIP
			return c, nil
		}
	}
	for i := range c.Bridges {
		if c.Bridges[i].Name == name {
			c.Bridges[i].PublicIP = publicIP
			c.Bridges[i].PrivateIP = privateIP
			return c, nil
		}
	}
	for i := range c.Lights {
		if c.Lights[i].Name == name {
			c.Lights[i].PublicIP = publicIP
			c.Lights[i].PrivateIP = privateIP
			return c, nil
		}
	}
	return c, fmt.Errorf("instance %s not found", name)
}
