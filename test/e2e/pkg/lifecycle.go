package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Start commences the testnet.
func Start(ctx context.Context, testnet *Testnet) error {
	fmt.Println("Spinning up testnet")
	nodes := testnet.NodesByStartHeight()
	for _, node := range nodes {

		if node.StartHeight != 0 {
			if err := WaitForHeight(ctx, testnet, node.StartHeight); err != nil {
				return err
			}
		}

		fmt.Printf("Starting %s at height %d on %s\n", node.Name, node.StartHeight, fmt.Sprintf("http://localhost:%d", node.ProxyPort))

		if err := execCompose(testnet.Dir, "up", "-d", node.Name); err != nil {
			return err
		}
	}

	return nil
}

// Stop stops the currently running network
func Stop(_ context.Context, testnet *Testnet) error {
	fmt.Println("Stopping testnet")
	return execCompose(testnet.Dir, "down")
}

// Cleanup removes the Docker Compose containers and testnet directory.
func Cleanup(_ context.Context, testnet *Testnet) error {
	err := cleanupDocker()
	if err != nil {
		return err
	}
	err = cleanupDir(testnet.Dir)
	if err != nil {
		return err
	}
	return nil
}

// cleanupDocker removes all E2E resources (with label e2e=True), regardless
// of testnet.
func cleanupDocker() error {
	// GNU xargs requires the -r flag to not run when input is empty, macOS
	// does this by default. Ugly, but works.
	xargsR := `$(if [[ $OSTYPE == "linux-gnu"* ]]; then echo -n "-r"; fi)`

	err := exec("bash", "-c", fmt.Sprintf(
		"docker container ls -qa --filter label=e2e | xargs %v docker container rm -f", xargsR))
	if err != nil {
		return err
	}

	err = exec("bash", "-c", fmt.Sprintf(
		"docker network ls -q --filter label=e2e | xargs %v docker network rm", xargsR))
	if err != nil {
		return err
	}

	return nil
}

// cleanupDir cleans up a testnet directory
func cleanupDir(dir string) error {
	if dir == "" {
		return errors.New("no directory set")
	}

	_, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	// On Linux, some local files in the volume will be owned by root since Tendermint
	// runs as root inside the container, so we need to clean them up from within a
	// container running as root too.
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	err = execDocker("run", "--rm", "--entrypoint", "", "-v", fmt.Sprintf("%v:/network", absDir),
		"tendermint/e2e-node", "sh", "-c", "rm -rf /network/*/")
	if err != nil {
		return err
	}

	err = os.RemoveAll(dir)
	if err != nil {
		return err
	}

	return nil
}
