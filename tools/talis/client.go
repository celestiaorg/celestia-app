package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/digitalocean/godo"
	"github.com/linode/linodego"
	"golang.org/x/oauth2"
)

type Client struct {
	do           *godo.Client
	linode       *linodego.Client
	sshKey       []byte
	doSSHKey     godo.Key
	linodeSSHKey *linodego.SSHKey
	cfg          Config
}

func NewClient(cfg Config) (*Client, error) {
	var doClient *godo.Client
	var linodeClient *linodego.Client
	var doSSHKey godo.Key
	var linodeSSHKey *linodego.SSHKey
	var err error

	sshKey, err := os.ReadFile(cfg.SSHPubKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SSH public key at: %s %w", cfg.SSHPubKeyPath, err)
	}

	if cfg.DigitalOceanToken != "" {
		tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.DigitalOceanToken})
		doClient = godo.NewClient(oauth2.NewClient(context.Background(), tokenSource))
		if doClient == nil {
			return nil, errors.New("failed to create DigitalOcean client")
		}
		doSSHKey, err = GetDOSSHKeyMeta(context.Background(), doClient, string(sshKey))
		if err != nil {
			return nil, fmt.Errorf("failed to get SSH key ID: %w", err)
		}
	}

	if cfg.LinodeToken != "" {
		linodeClient = LinodeClient(cfg.LinodeToken)
		if linodeClient == nil {
			return nil, errors.New("failed to create Linode client")
		}
		linodeSSHKey, err = GetLinodeSSHKeyMeta(context.Background(), linodeClient, string(sshKey))
		if err != nil {
			return nil, fmt.Errorf("failed to get SSH key ID: %w", err)
		}
	}

	return &Client{
		do:           doClient,
		linode:       linodeClient,
		sshKey:       sshKey,
		doSSHKey:     doSSHKey,
		linodeSSHKey: linodeSSHKey,
		cfg:          cfg,
	}, nil
}

func (c *Client) Up(ctx context.Context) error {
	doInsts := make([]Instance, 0)
	linodeInsts := make([]Instance, 0)

	for _, v := range c.cfg.Validators {
		switch v.Provider {
		case DigitalOcean:
			if v.Region == "" || v.Region == RandomRegion {
				v.Region = RandomDORegion()
			}
			doInsts = append(doInsts, v)
		case Linode:
			if v.Region == "" || v.Region == RandomRegion {
				v.Region = RandomLinodeRegion()
			}
			linodeInsts = append(linodeInsts, v)
		default:
			log.Println("unexpectedly skipping instance since provider is not supported", v.Name, "in region", v.Region)
		}
	}

	if len(doInsts) > 0 {
		created, err := CreateDroplets(ctx, c.do, doInsts, c.doSSHKey)
		if err != nil {
			return fmt.Errorf("failed to create droplets: %w", err)
		}
		for _, inst := range created {
			cfg, err := c.cfg.UpdateInstance(inst.Name, inst.PublicIP, inst.PrivateIP)
			if err != nil {
				return fmt.Errorf("failed to update config with instance %s: %w", inst.Name, err)
			}
			c.cfg = cfg
		}
	}

	if len(linodeInsts) > 0 {
		created, err := CreateLinodes(ctx, c.linode, linodeInsts, c.linodeSSHKey)
		if err != nil {
			return fmt.Errorf("failed to create linodes: %w", err)
		}
		for _, inst := range created {
			cfg, err := c.cfg.UpdateInstance(inst.Name, inst.PublicIP, inst.PrivateIP)
			if err != nil {
				return fmt.Errorf("failed to update config with instance %s: %w", inst.Name, err)
			}
			c.cfg = cfg
		}
	}

	return nil
}

func (c *Client) Down(ctx context.Context) error {
	doInsts := make([]Instance, 0)
	linodeInsts := make([]Instance, 0)

	for _, v := range c.cfg.Validators {
		switch v.Provider {
		case DigitalOcean:
			doInsts = append(doInsts, v)
		case Linode:
			linodeInsts = append(linodeInsts, v)
		default:
			log.Println("unexpectedly skipping instance since provider is not supported", v.Name, "in region", v.Region)
		}
	}

	if len(doInsts) > 0 {
		if _, err := DestroyDroplets(ctx, c.do, doInsts); err != nil {
			return err
		}
	}

	if len(linodeInsts) > 0 {
		if _, err := DestroyLinodes(ctx, c.linode, linodeInsts); err != nil {
			return err
		}
	}

	return nil
}
