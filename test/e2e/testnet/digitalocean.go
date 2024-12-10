package testnet

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/digitalocean/godo"
)

type DigitalOcean struct {
	client *godo.Client
	ctx    context.Context
}

type Key struct {
	DOKey      *godo.Key
	PrivateKey *rsa.PrivateKey
}

type HwNodeDo struct {
	Droplet *godo.Droplet
	Key     *Key
}

func NewDigitalOcean(token string) *DigitalOcean {
	client := godo.NewFromToken(token)
	ctx := context.TODO()
	return &DigitalOcean{
		client: client,
		ctx:    ctx,
	}
}

func (do *DigitalOcean) Client() *godo.Client {
	return do.client
}

func (do *DigitalOcean) Ctx() context.Context {
	return do.ctx
}

func (do *DigitalOcean) GetDroplet(dropletName string) (*HwNodeDo, error) {
	droplets, _, err := do.client.Droplets.List(do.ctx, &godo.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("error listing droplets: %w", err)
	}

	for _, droplet := range droplets {
		if droplet.Name == dropletName {
			return &HwNodeDo{Droplet: &droplet}, nil
		}
	}

	return nil, nil
}

func (do *DigitalOcean) DropletExists(dropletName string) (bool, error) {
	droplet, err := do.GetDroplet(dropletName)
	if err != nil {
		return false, err
	}
	return droplet != nil, nil
}

func (do *DigitalOcean) CreateKey(dropletName string) (*Key, error) {
	// Generate a new SSH key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("error generating private key: %w", err)
	}

	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("error generating public key: %w", err)
	}

	// Add the SSH key to DigitalOcean
	createKeyRequest := &godo.KeyCreateRequest{
		Name:      dropletName + "-key",
		PublicKey: string(ssh.MarshalAuthorizedKey(publicKey)),
	}

	key, _, err := do.client.Keys.Create(do.ctx, createKeyRequest)
	if err != nil {
		return nil, fmt.Errorf("error creating SSH key: %w", err)
	}

	return &Key{DOKey: key, PrivateKey: privateKey}, nil
}

func (do *DigitalOcean) DeleteKey(keyName string) error {
	keys, _, err := do.client.Keys.List(do.ctx, &godo.ListOptions{})
	if err != nil {
		return fmt.Errorf("error listing keys: %w", err)
	}

	for _, key := range keys {
		if key.Name == keyName {
			_, err := do.client.Keys.DeleteByID(do.ctx, key.ID)
			if err != nil {
				return fmt.Errorf("error deleting SSH key: %w", err)
			}
			return nil
		}
	}

	return fmt.Errorf("key with name %s not found", keyName)
}

func (do *DigitalOcean) CreateDroplet(dropletName, region, size string, key *Key) (*HwNodeDo, error) {
	// Check if the droplet already exists
	exists, err := do.DropletExists(dropletName)
	if err != nil {
		return nil, err
	}

	if exists {
		return nil, fmt.Errorf("droplet with name %s already exists", dropletName)
	}

	// Create a new droplet if it doesn't exist
	createRequest := &godo.DropletCreateRequest{
		Name:   dropletName,
		Region: region,
		Size:   size,
		Image: godo.DropletCreateImage{
			Slug: "ubuntu-20-04-x64",
		},
		SSHKeys: []godo.DropletCreateSSHKey{
			{Fingerprint: key.DOKey.Fingerprint},
		},
		Tags: []string{"knuu", dropletName},
	}

	newDroplet, _, err := do.client.Droplets.Create(do.ctx, createRequest)
	if err != nil {
		// Ensure the key is deleted if droplet creation fails
		keyErr := do.DeleteKey(dropletName)
		if keyErr != nil {
			return nil, fmt.Errorf("error creating droplet: %w; additionally, error deleting SSH key: %v", err, keyErr)
		}
		return nil, fmt.Errorf("error creating droplet: %w", err)
	}

	// Wait until new droplet is active
	activeDroplet, err := do.waitReady(newDroplet.ID)
	if err != nil {
		// Ensure the key is deleted if waiting for droplet readiness fails
		keyErr := do.DeleteKey(dropletName)
		if keyErr != nil {
			return nil, fmt.Errorf("error waiting for droplet to be ready: %w; additionally, error deleting SSH key: %v", err, keyErr)
		}
		return nil, err
	}

	return &HwNodeDo{Droplet: activeDroplet, Key: key}, nil
}

func (do *DigitalOcean) waitReady(dropletID int) (*godo.Droplet, error) {
	for {
		droplet, _, err := do.client.Droplets.Get(do.ctx, dropletID)
		if err != nil {
			return nil, errors.New("error refreshing droplet")
		}
		if droplet.Status == "active" {
			ip, err := droplet.PublicIPv4()
			if err == nil && ip != "" {
				return droplet, nil
			}
		}
		time.Sleep(1 * time.Second)
	}
}

func (do *DigitalOcean) RemoveDroplet(dropletName string) error {
	// Check if the droplet exists
	exists, err := do.DropletExists(dropletName)
	if err != nil {
		return err
	}

	if !exists {
		return fmt.Errorf("droplet with specified name not found")
	}

	// Find the droplet by name
	droplet, err := do.GetDroplet(dropletName)
	if err != nil {
		return err
	}

	// Delete the droplet
	_, err = do.client.Droplets.Delete(do.ctx, droplet.Droplet.ID)
	if err != nil {
		return fmt.Errorf("error deleting droplet: %w", err)
	}

	// Delete the associated SSH key
	err = do.DeleteKey(dropletName)
	if err != nil {
		return fmt.Errorf("error deleting SSH key: %w", err)
	}

	return nil
}

func (node *HwNodeDo) GetPublicIP() string {
	ip, err := node.Droplet.PublicIPv4()
	if err != nil {
		return ""
	}
	return ip
}

func (node *HwNodeDo) ExecuteCommandOnDroplet(user, command string) (string, error) {
	ip := node.GetPublicIP()
	if ip == "" {
		return "", errors.New("droplet has no public IP")
	}

	signer, err := ssh.NewSignerFromKey(node.Key.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("error creating signer from private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second, // Add a timeout to the SSH connection
	}

	// Retry logic for SSH connection
	var client *ssh.Client
	for i := 0; i < 3; i++ {
		client, err = ssh.Dial("tcp", ip+":22", config)
		if err == nil {
			break
		}
		time.Sleep(2 * time.Second) // Wait before retrying
	}
	if err != nil {
		return "", fmt.Errorf("error dialing SSH: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("error creating SSH session: %w", err)
	}
	defer session.Close()

	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	if err := session.Run(command); err != nil {
		return "", fmt.Errorf("error running command: %w", err)
	}

	return stdoutBuf.String(), nil
}
