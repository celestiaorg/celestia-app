# Docker

## Context

Github Actions should automatically build and publish a Docker image for each release. If Github Actions failed to build or publish the Docker image, you can manually build and publish a Docker image using this guide.

## Prerequisites

1. Navigate to <https://github.com/settings/tokens> and generate a new token with the `write:packages` scope.

## Steps

1. Verify that a Docker image with the correct tag doesn't already exist for the release you're trying to create publish on [GHCR](https://github.com/celestiaorg/celestia-app/pkgs/container/celestia-app/versions)

1. In a new terminal

    ```shell
    # Set the CR_PAT environment variable to your token
    export CR_PAT=YOUR_TOKEN
    # Login to the GitHub Container Registry
    echo $CR_PAT | docker login ghcr.io -u USERNAME --password-stdin

    # Tell docker to use buildx for the multiple platform support
    docker buildx create --use
    # Build the image, in this example the v2.2.0-mocha image
    docker buildx build --platform linux/amd64,linux/arm64 -t ghcr.io/celestiaorg/celestia-app:v2.2.0-mocha --push .
    ```

1. Verify that a Docker image with the correct tag was published on [GHCR](https://github.com/celestiaorg/celestia-app/pkgs/container/celestia-app/versions).
