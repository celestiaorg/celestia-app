package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"

	"github.com/digitalocean/godo"
	"golang.org/x/oauth2"
)

type Client struct {
	do       *godo.Client
	sshKey   []byte
	doSSHKey godo.Key
	cfg      Config
}

func NewClient(cfg Config) (*Client, error) {
	if cfg.DigitalOceanToken == "" {
		return nil, errors.New("DigitalOcean token is required")
	}

	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.DigitalOceanToken})
	client := godo.NewClient(oauth2.NewClient(context.Background(), tokenSource))

	if client == nil {
		return nil, errors.New("failed to create DigitalOcean client")
	}

	sshKey, err := os.ReadFile(cfg.SSHPubKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read SSH public key at: %s %w", cfg.SSHPubKeyPath, err)
	}

	key, err := GetDOSSHKeyMeta(context.Background(), client, string(sshKey))
	if err != nil {
		return nil, fmt.Errorf("failed to get SSH key ID: %w", err)
	}

	return &Client{
		do:       client,
		sshKey:   sshKey,
		doSSHKey: key,
		cfg:      cfg,
	}, nil
}

func (c *Client) Up(ctx context.Context, workers int) error {
	insts := make([]Instance, 0)
	for _, v := range c.cfg.Validators {
		if v.Provider != DigitalOcean {
			log.Println("unexpectedly skipping instance since only DO is supported", v.Name, "in region", v.Region)
			continue
		}

		if v.Region == "" || v.Region == RandomRegion {
			v.Region = RandomDORegion()
		}

		insts = append(insts, v)
	}

	if len(insts) == 0 {
		return fmt.Errorf("no instances to create")
	}

	insts, err := CreateDroplets(ctx, c.do, insts, c.doSSHKey, workers)
	if err != nil {
		return fmt.Errorf("failed to create droplets: %w", err)
	}

	for _, inst := range insts {
		cfg, err := c.cfg.UpdateInstance(inst.Name, inst.PublicIP, inst.PrivateIP)
		if err != nil {
			return fmt.Errorf("failed to update config with instance %s: %w", inst.Name, err)
		}
		c.cfg = cfg
	}

	return err
}

func (c *Client) Down(ctx context.Context, workers int) error {
	insts := make([]Instance, 0)
	for _, v := range c.cfg.Validators {
		if v.Provider != DigitalOcean {
			log.Println("unexpectedly skipping instance since only DO is supported", v.Name, "in region", v.Region)
			continue
		}
		if v.Region == "" || v.Region == RandomRegion {
			v.Region = RandomDORegion()
		}
		insts = append(insts, v)
	}

	if len(insts) == 0 {
		return fmt.Errorf("no instances to destroy")
	}

	_, err := DestroyDroplets(ctx, c.do, insts, workers)
	return err
}
