# rsmt2d

Two-dimensional Reed-Solomon Merkle tree data availability, maintained in
celestia-app as an independent Go module:

```go
import "github.com/celestiaorg/celestia-app/rsmt2d"
```

This module does not depend on the celestia-app application module or Cosmos SDK.
Its version is independent of the app's major version.

## Source and compatibility

Imported from [celestiaorg/rsmt2d v0.15.3](https://github.com/celestiaorg/rsmt2d/tree/fb27392a895e911649bc811bb186673508f925a8).
The dependencies, LICENSE, and NOTICE are unchanged. The runtime Go files are
unchanged except that the unexported, test-only `deepCopy` method now checks
the import error before dereferencing. The module declaration and external test
imports use the new path. All tests from that
release are retained. The original [audit](https://github.com/celestiaorg/rsmt2d/blob/fb27392a895e911649bc811bb186673508f925a8/audit/informal-systems.pdf)
and [design paper](https://arxiv.org/abs/1809.09044) remain available.

Relocating the package preserves encoding and Merkle roots but changes Go type
identity. Update imports in every package that exchanges `ExtendedDataSquare`,
`Axis`, `TreeConstructorFn`, or `BufferedTreeConstructor` values, including the
app version used by the consumer. Merely adding a `replace` for the old module
does not make values from the two import paths interchangeable.

## Development

From this directory:

```sh
GOWORK=off go test -mod=readonly ./...
GOWORK=off go test -mod=readonly -race -count=3 ./...
GOWORK=off go vet ./...
```

The explicit `GOWORK=off` verifies the library's own dependency graph. Root
`go test ./...` does not traverse nested modules; the dedicated rsmt2d CI job
runs these checks. `make test-rsmt2d` at the repository root runs the race suite.

## Releases and consumer migration

1. Merge the module import and publish a tag such as `rsmt2d/v0.15.3` on that
   commit. Go module tags for this directory require the `rsmt2d/` prefix.
   App tags such as `v11.0.0` do not release this module.
2. Update app's module requirement to the published module version. Its local
   replacement supports editing the library and app together; downstream
   consumers ignore that replacement and must resolve the published version.
3. Publish app releases containing the changed imports. Node users of app v10
   need the corresponding v10 migration; upgrading to app v11 is not required.
4. Update consumers' imports and requirements together, run `go mod tidy`, and
   verify builds, EDS repair, roots, and any stored fixture output. Review
   consumers pinned to an older rsmt2d release as dependency upgrades too.
5. Reconcile unreleased changes on standalone rsmt2d main separately. This
   import intentionally preserves v0.15.3 and does not supersede those changes.
   Deprecate the old module only after consumers have a tested migration path.
   Preserve old tags and source so historical builds remain reproducible.

For a PR preview, pin the new module to an immutable commit containing this
directory. Replace preview pins with release tags before consumer releases.
