package version

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// scriptPath returns the absolute path to scripts/version.sh.
func scriptPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine caller path")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	return filepath.Join(repoRoot, "scripts", "version.sh")
}

// gitEnv isolates HOME (so the developer's global git config, e.g. one that
// forces annotated tags, does not interfere) while preserving PATH so git and
// bash resolve.
func gitEnv(dir string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dir,
		"GIT_CONFIG_NOSYSTEM=1",
	}
}

func runIn(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = gitEnv(dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitRepo initializes a throwaway git repository with a single commit.
func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runIn(t, dir, "git", "init", "-q")
	runIn(t, dir, "git", "config", "user.email", "test@test.test")
	runIn(t, dir, "git", "config", "user.name", "test")
	runIn(t, dir, "git", "commit", "-q", "--allow-empty", "-m", "commit 1")
	return dir
}

// tag creates a lightweight tag pointing at rev (HEAD if empty). Lightweight
// tags mirror tags created by GitHub releases and never require a tag message
// regardless of git config.
func tag(t *testing.T, dir, name, rev string) {
	t.Helper()
	if rev == "" {
		rev = "HEAD"
	}
	runIn(t, dir, "git", "update-ref", "refs/tags/"+name, rev)
}

// version runs scripts/version.sh inside dir and returns the trimmed output.
func version(t *testing.T, dir string) string {
	t.Helper()
	return runIn(t, dir, "bash", scriptPath(t))
}

func TestVersion_SingleBaseTag(t *testing.T) {
	dir := gitRepo(t)
	tag(t, dir, "v8.0.8", "")
	if got := version(t, dir); got != "8.0.8" {
		t.Errorf("got %q, want %q", got, "8.0.8")
	}
}

func TestVersion_SingleMochaTag(t *testing.T) {
	dir := gitRepo(t)
	tag(t, dir, "v7.0.2-mocha", "")
	if got := version(t, dir); got != "7.0.2-mocha" {
		t.Errorf("got %q, want %q", got, "7.0.2-mocha")
	}
}

// When the base mainnet tag is present alongside network tags the clean base
// version wins (this preserves the behavior added in PR #5894).
func TestVersion_BaseAndNetworkTags(t *testing.T) {
	dir := gitRepo(t)
	tag(t, dir, "v3.8.1-mocha", "")
	tag(t, dir, "v3.8.1-arabica", "")
	tag(t, dir, "v3.8.1", "")
	if got := version(t, dir); got != "3.8.1" {
		t.Errorf("got %q, want %q", got, "3.8.1")
	}
}

// Regression test for issue #4631: a commit tagged for testnets only (no base
// mainnet tag) must report the mocha suffix, not an arbitrary sibling tag or a
// stripped base version.
func TestVersion_MochaAndArabicaNoBase(t *testing.T) {
	dir := gitRepo(t)
	// Create in arabica-then-mocha order to prove ordering does not matter.
	tag(t, dir, "v7.0.2-arabica", "")
	tag(t, dir, "v7.0.2-mocha", "")
	if got := version(t, dir); got != "7.0.2-mocha" {
		t.Errorf("got %q, want %q", got, "7.0.2-mocha")
	}
}

// mocha is preferred over a release-candidate tag on the same commit.
func TestVersion_RcAndMocha(t *testing.T) {
	dir := gitRepo(t)
	tag(t, dir, "v9.0.0-rc1", "")
	tag(t, dir, "v9.0.0-mocha", "")
	if got := version(t, dir); got != "9.0.0-mocha" {
		t.Errorf("got %q, want %q", got, "9.0.0-mocha")
	}
}

func TestVersion_RcOnly(t *testing.T) {
	dir := gitRepo(t)
	tag(t, dir, "v9.0.0-rc1", "")
	if got := version(t, dir); got != "9.0.0-rc1" {
		t.Errorf("got %q, want %q", got, "9.0.0-rc1")
	}
}

// When HEAD is ahead of the latest tag (a development build, e.g. on main) the
// version falls back to git describe and includes the commit suffix.
func TestVersion_DevBuildAheadOfTag(t *testing.T) {
	dir := gitRepo(t)
	tag(t, dir, "v8.0.8", "HEAD")
	runIn(t, dir, "git", "commit", "-q", "--allow-empty", "-m", "commit 2")
	got := version(t, dir)
	if !strings.HasPrefix(got, "8.0.8-") || !strings.Contains(got, "-g") {
		t.Errorf("got %q, want a dev version like 8.0.8-1-g<hash>", got)
	}
}
