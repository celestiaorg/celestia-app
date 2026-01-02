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

const (
	DODropletLimit = 100
)

type Client interface {
	Up(ctx context.Context, workers int) error
	Down(ctx context.Context, workers int) error
	List(ctx context.Context) error
	GetConfig() Config
}

type ClientInfo struct {
	sshKey []byte
	cfg    Config
}

type DOClient struct {
	ClientInfo
	do       *godo.Client
	doSSHKey godo.Key
}

func NewClient(cfg Config) (Client, error) {
	if cfg.DigitalOceanToken != "" {
		return NewDOClient(cfg)
	}
	if cfg.GoogleCloudProject != "" {
		return NewGCClient(cfg)
	}
	return nil, errors.New("no cloud provider credentials found")
}

func NewDOClient(cfg Config) (*DOClient, error) {
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

	return &DOClient{
		ClientInfo: ClientInfo{
			sshKey: sshKey,
			cfg:    cfg,
		},
		do:       client,
		doSSHKey: key,
	}, nil
}

func (c *DOClient) Up(ctx context.Context, workers int) error {
	insts := make([]Instance, 0)
	for _, v := range append(c.cfg.Validators, c.cfg.Metrics...) {
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

	// Check if spinning up these instances would exceed the 100-droplet limit
	currentCount, err := c.countRunningDroplets(ctx)
	if err != nil {
		log.Printf("⚠️  Warning: failed to count running droplets: %v", err)
	} else {
		totalAfterUp := currentCount + len(insts)
		if totalAfterUp > DODropletLimit {
			excess := totalAfterUp - DODropletLimit
			return fmt.Errorf("cannot spin up %d instances: would exceed DigitalOcean's %d droplet limit (currently %d running, would be %d total). Please reduce the number of instances by %d", len(insts), DODropletLimit, currentCount, totalAfterUp, excess)
		}
	}

	insts, err = CreateDroplets(ctx, c.do, insts, c.doSSHKey, workers)
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

func (c *DOClient) countRunningDroplets(ctx context.Context) (int, error) {
	opts := &godo.ListOptions{}
	count := 0
	for {
		droplets, resp, err := c.do.Droplets.List(ctx, opts)
		if err != nil {
			return 0, fmt.Errorf("failed to list droplets: %w", err)
		}

		count += len(droplets)

		if resp.Links == nil || resp.Links.IsLastPage() {
			break
		}
		page, err := resp.Links.CurrentPage()
		if err != nil {
			return 0, fmt.Errorf("failed to paginate droplets list: %w", err)
		}

		opts.Page = page + 1
	}

	return count, nil
}

func (c *DOClient) Down(ctx context.Context, workers int) error {
	insts := make([]Instance, 0)
	for _, v := range append(c.cfg.Validators, c.cfg.Metrics...) {
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

func (c *DOClient) List(ctx context.Context) error {
	opts := &godo.ListOptions{}
	cnt := 0
	for {
		droplets, resp, err := c.do.Droplets.List(ctx, opts)
		if err != nil {
			return fmt.Errorf("failed to list droplets: %w", err)
		}

		for _, droplet := range droplets {
			if hasAllTags(droplet.Tags, []string{"talis"}) {
				publicIP := ""
				privateIP := ""
				if len(droplet.Networks.V4) > 0 {
					for _, network := range droplet.Networks.V4 {
						if network.Type == "public" && publicIP == "" {
							publicIP = network.IPAddress
						}
						if network.Type == "private" && privateIP == "" {
							privateIP = network.IPAddress
						}
					}
				}

				if cnt == 0 {
					fmt.Printf("%-30s %-10s %-15s %-15s %s\n", "Name", "Status", "Region", "Public IP", "Created")
					fmt.Printf("%-30s %-10s %-15s %-15s %s\n", "----", "------", "------", "---------", "-------")
				}

				fmt.Printf("%-30s %-10s %-15s %-15s %s\n",
					droplet.Name,
					droplet.Status,
					droplet.Region.Slug,
					publicIP,
					droplet.Created)
				cnt++
			}
		}

		if resp.Links == nil || resp.Links.IsLastPage() {
			break
		}
		page, err := resp.Links.CurrentPage()
		if err != nil {
			return fmt.Errorf("failed to paginate droplets list: %w", err)
		}

		opts.Page = page + 1
	}

	fmt.Println("Total number of talis instances:", cnt)
	return nil
}

func (c *DOClient) GetConfig() Config {
	return c.cfg
}
