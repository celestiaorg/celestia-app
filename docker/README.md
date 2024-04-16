# Celestia `txsim` Docker Image Usage Guide

The `txsim` binary is a tool that can be used to simulate transactions on the Celestia network. It can be used to test the performance of the Celestia network.
This guide provides instructions on how to use the Celestia `txsim` Docker image. The `txsim` Docker image is designed to run the `txsim` binary with a variety of configurable options.

## Table of Contents

1. [Celestia `txsim` Docker Image Usage Guide](#celestia-txsim-docker-image-usage-guide)
   1. [Table of Contents](#table-of-contents)
   2. [Prerequisites](#prerequisites)
   3. [Running the Docker Image](#running-the-docker-image)
      1. [Docker Run](#docker-run)
      2. [Docker Compose](#docker-compose)
      3. [Kubernetes Deployment](#kubernetes-deployment)
   4. [Flag Breakdown](#flag-breakdown)

## Prerequisites

Before you can use the `txsim` Docker image, you must have a prefunded account set up. The `txsim` binary requires a prefunded account to function correctly. The keyring for this account should be stored in a file that can be accessed by the Docker container.

## Running the Docker Image

### Docker Run

You can run the `txsim` Docker image using the `docker run` command. Here's an example:

```bash
docker run -it -v ${HOME}/.celestia-app:/home/celestia ghcr.io/celestiaorg/txsim -k 0 -g consensus-validator-robusta-rc6.celestia-robusta.com:9090 -t 10s -b 10 -d 100 -e 10
```

In this command, the `-v` option is used to mount the `${HOME}/.celestia-app` directory from the host to the `/home/celestia` directory in the Docker container. This allows the `txsim` binary to access the keyring for the prefunded account.

### Docker Compose

You can also run the `txsim` Docker image using Docker Compose. Here's an example `docker-compose.yml` file:

```yaml
version: '3'
services:
  txsim:
    image: ghcr.io/celestiaorg/txsim
    command: >
      -k 0
      -g consensus-validator-robusta-rc6.celestia-robusta.com:9090
      -t 10s
      -b 10
      -d 100
      -e 10
    volumes:
      - /Users/txsimp/.celestia-app:/home/celestia
```

In this file, the `volumes` key is used to mount the `/Users/txsimp/.celestia-app` directory from the host to the `/home/celestia` directory in the Docker container.

### Kubernetes Deployment

Finally, you can run the `txsim` Docker image in a Kubernetes cluster. Here's an example `deployment.yaml` file:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: txsim-deployment
spec:
  replicas: 1
  selector:
    matchLabels:
      app: txsim
  template:
    metadata:
      labels:
        app: txsim
    spec:
      containers:
      - name: txsim
        image: ghcr.io/celestiaorg/txsim
        args:
        - "-k"
        - "0"
        - "-g"
        - "consensus-validator-robusta-rc6.celestia-robusta.com:9090"
        - "-t"
        - "10s"
        - "-b"
        - "10"
        - "-d"
        - "100"
        - "-e"
        - "10"
        volumeMounts:
        - name: keyring-volume
          mountPath: /home/celestia
      volumes:
      - name: keyring-volume
        hostPath:
          path: /Users/txsimp/.celestia-app
```

In this file, the `volumeMounts` and `volumes` keys are used to mount the `/Users/txsimp/.celestia-app` directory from the host to the `/home/celestia` directory in the Docker container.

## Flag Breakdown

Here's a breakdown of what each flag means:

- `-k`: Whether a new key should be created (1 for yes, 0 for no)
- `-p`: The path to the keyring for the prefunded account
- `-g`: The gRPC endpoint for the `txsim` binary
- `-t`: The poll time for the `txsim` binary
- `-b`: The number of blob sequences to run
- `-a`: The range of blobs to send per PFB in a sequence
- `-s`: The range of blob sizes to send
- `-m`: The mnemonic for the keyring
- `-d`: The seed for the random number generator
- `-e`: The number of send sequences to run
- `-i`: The amount to send from one account to another
- `-v`: The number of send iterations to run per sequence
- `-u`: The number of stake sequences to run
- `-w`: The amount of initial stake per sequence

Please replace the placeholders in the examples with the actual values you want to use.
