package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre"
)

// fibreNetworkFiles validates every selected host before any remote changes.
func fibreNetworkFiles(dir string, instances []Instance) ([]string, error) {
	if dir == "" {
		return nil, nil
	}
	paths := make([]string, len(instances))
	for i, inst := range instances {
		if inst.Name == "" || filepath.Base(inst.Name) != inst.Name || strings.ContainsAny(inst.Name, `/\\`) {
			return nil, fmt.Errorf("invalid instance name %q", inst.Name)
		}
		path, err := filepath.Abs(filepath.Join(dir, inst.Name+".json"))
		if err != nil {
			return nil, err
		}
		if _, err := fibre.LoadNetworkConfig(path); err != nil {
			return nil, fmt.Errorf("network config for %s: %w", inst.Name, err)
		}
		paths[i] = path
	}
	return paths, nil
}

func stageFibreNetworkConfigs(ctx context.Context, dir string, instances []Instance, sshKeyPath, session string) (string, error) {
	paths, err := fibreNetworkFiles(dir, instances)
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", nil
	}
	// Only fixed session names enter the remote shell/SCP destination.
	if session != FibreTxSimSessionName && session != FibreReaderSessionName {
		return "", fmt.Errorf("invalid fibre session %q", session)
	}
	remotePath := "/root/talis-" + session + "-network.json"
	for i, inst := range instances {
		copyCtx, cancel := context.WithTimeout(ctx, time.Minute)
		err := scpFile(copyCtx, paths[i], inst.PublicIP, remotePath, sshKeyPath)
		cancel()
		if err != nil {
			return "", fmt.Errorf("stage network config for %s: %w", inst.Name, err)
		}
	}
	return " --network-config '" + remotePath + "'", nil
}
