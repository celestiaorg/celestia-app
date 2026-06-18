// Package version contains a regression test for scripts/version.sh, the shell
// helper that the Makefile uses to resolve the semantic version embedded into
// the celestia-appd binary. There is no production Go code here; the test
// guards the version-resolution logic, which has regressed several times (see
// issues #3977, #4631 and PR #5894).
package version
