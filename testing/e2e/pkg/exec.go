package e2e

import (
	"fmt"
	osexec "os/exec"
	"path/filepath"
)

// execute executes a shell command.
func exec(args ...string) error {
	cmd := osexec.Command(args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	switch err := err.(type) {
	case nil:
		return nil
	case *osexec.ExitError:
		return fmt.Errorf("failed to run %q:\n%v", args, string(out))
	default:
		return err
	}
}

// execCompose runs a Docker Compose command for a testnet.
func execCompose(dir string, args ...string) error {
	return exec(append(
		[]string{"docker compose", "-f", filepath.Join(dir, "docker-compose.yml")},
		args...)...)
}

// execDocker runs a Docker command.
func execDocker(args ...string) error {
	return exec(append([]string{"docker"}, args...)...)
}
