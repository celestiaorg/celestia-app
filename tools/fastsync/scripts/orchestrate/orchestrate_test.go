// Package orchestrate runs test-orchestrate.sh, the offline tests of the multi-VM orchestration scripts.
package orchestrate

import (
	"os/exec"
	"testing"
)

func TestOrchestrateScripts(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	out, err := exec.Command("bash", "test-orchestrate.sh").CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatalf("test-orchestrate.sh: %v", err)
	}
}
