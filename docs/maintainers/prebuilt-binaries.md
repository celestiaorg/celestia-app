# Prebuilt binaries

Prebuilt binaries are attached to each release via [GoReleaser](https://goreleaser.com/) which runs in Github Actions. If GoReleaser failed to attach prebuilt binaries, you may want to attach them manually by following the steps below.

## Prerequisites

1. Create a Github token (classic) that has `repo:public_repo` scope via: <https://github.com/settings/tokens/new>

    ```shell
    export GORELEASER_ACCESS_TOKEN=<your-github-token>
    echo "GITHUB_TOKEN=${GORELEASER_ACCESS_TOKEN}" >> .release-env
    ```

## Steps

1. [Optional] If you need to make any code changes to fix the issue that occurred when CI tried to generate and attach the prebuilt binaries, then you likely need to skip validation when running GoReleaser locally. To skip validation, modify the Makefile command like so:

    ```diff
    ## prebuilt-binary: Create prebuilt binaries and attach them to GitHub release. Requires Docker.
    prebuilt-binary:
        @if [ ! -f ".release-env" ]; then \
            echo "A .release-env file was not found but is required to create prebuilt binaries. This command is expected to be run in CI where a .release-env file exists. If you need to run this command locally to attach binaries to a release, you need to create a .release-env file with a Github token (classic) that has repo:public_repo scope."; \
            exit 1;\
        fi
        docker run \
            --rm \
            -e CGO_ENABLED=1 \
            --env-file .release-env \
            -v /var/run/docker.sock:/var/run/docker.sock \
            -v `pwd`:/go/src/$(PACKAGE_NAME) \
            -w /go/src/$(PACKAGE_NAME) \
            ghcr.io/goreleaser/goreleaser-cross:${GOLANG_CROSS_VERSION} \
    -       release --clean
    +       release --clean --skip=validate
    .PHONY: prebuilt-binary
    ```

1. Before proceeding, test your change by running `make prebuilt-binary` and verifying that prebuilt binaries are attached to a release on your Github fork.
1. Modify `.goreleaser.yaml` so that you can upload assets to the main repository:

    ```diff
    release:
    +  github:
    +    owner: celestiaorg
    +    name: celestia-app
    ```

1. Run `make prebuilt-binary` to generate and attach the prebuilt binaries.
1. Verify the assets were attached to the release on the main repository.
