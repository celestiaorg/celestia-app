# Go Module Dependency Management

This document describes the dependency management setup for the celestia-app repository, which contains multiple Go modules.

## Repository Structure

The repository contains three Go modules:
- Main module: `/go.mod`
- Docker E2E tests: `/test/docker-e2e/go.mod`
- Interchain tests: `/test/interchain/go.mod`

## Dependency Update Strategies

### 1. Automated Dependency Updates (Recommended)

**Custom Workflow**: `.github/workflows/sync-go-modules.yml`
- Runs daily to check for dependency updates across all modules
- Updates all three modules in a single PR for consistency
- Uses the existing `make mod` command to ensure proper synchronization
- Can be triggered manually via GitHub Actions

**Dependabot**: `.github/dependabot.yml`
- Monitors each module for dependency updates
- Creates separate PRs for each module (limitation: cannot group across directories)
- Provides baseline monitoring even if custom workflow is preferred

### 2. Manual Dependency Management

Use the existing `make mod` command to update and tidy all modules:

```bash
make mod
```

This command:
- Runs `go mod tidy` on the main module
- Runs `go mod tidy` on `/test/interchain`
- Runs `go mod tidy` on `/test/docker-e2e`

### 3. Pre-commit Hook (Optional)

For automatic `go mod tidy` on every commit affecting Go modules:

```bash
# Install the pre-commit hook
cp scripts/pre-commit-hook .git/hooks/pre-commit
```

This ensures all modules are tidied whenever any `go.mod` file is modified.

## Recommendations

1. **Use the custom workflow** for regular dependency updates to ensure all modules stay in sync
2. **Keep dependabot enabled** as a backup monitoring system
3. **Consider the pre-commit hook** if you want automatic tidying on every commit
4. **Always run `make mod`** when manually updating dependencies

## Troubleshooting

If builds fail due to module inconsistencies:
1. Run `make mod` to synchronize all modules
2. Ensure all modules use compatible dependency versions
3. Check that shared dependencies use the same versions across modules