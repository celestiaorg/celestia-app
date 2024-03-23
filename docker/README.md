# Celestia `txsim` Docker Image Usage Guide

## Table of Contents

  1. [Docker Setup](#docker-setup)
  2. [Overview of celestia-app txsim](#overview-of-celestia-app-txsim)
  3. [Prerequisites](#prerequisites)
  4. [Quick-Start](#quick-start)
  5. [Docker Compose](#docker-compose)
  6. [Kubernetes Deployments](#kubernetes-deployment)
  7. [Flag Breakdown](#flag-breakdown)

## 🐳 Docker setup

This documentation provides a step by step guide on how to start up a celestia
app using a docker image. Docker provides a seamless setup for celestia-app
in an isolated environment on your machine. With Docker,
you do not have to worry about the manual configuration of the required
dependencies, which can be a pain.

## Overview of celestia app txsim

The celestia-app `txsim` binary is a tool that can be
used to simulate transactions on the Celestia network.
The `txsim` Docker image is designed to run the `txsim` binary with a
variety of configurable options.

## Prerequisites

- [Docker Desktop for Mac or Windows](https://docs.docker.com/get-docker) or
[Docker Engine for Linux](https://docs.docker.com/engine/install/)
and a basic understanding of Docker.

- A prefunded account set up with the keyring stored in a file,
to be accessed by an instance of the docker image.

## Quick-Start

1. In your local machine, navigate to the home directory

   ```bash
   cd $HOME
   ```

2. Create a file in which the keyring would be stored.
The file would be mounted as a volume into the docker container.

   ```bash
   touch .celestia-app
   ```

3. Using a suitable text editor of your choice, open the
.celestia-app file and paste the keyring of the prefunded account.

4. We recommend that you set the necessary file permission for the
.celestia-app file. A simple read access is all that is required for the
docker container to access the content of the file.

5. You can run the txsim Docker image using the docker run command below.

   ```bash
   docker run -it \
   -v $HOME/.celestia-app:/home/celestia ghcr.io/celestiaorg/txsim \
   -k 0 \
   -r http://consensus-validator-robusta-rc6.celestia-robusta.com:26657, \
   http://consensus-full-robusta-rc6.celestia-robusta.com:26657 \
   -g consensus-validator-robusta-rc6.celestia-robusta.com: \
   -t 10s -b 10 -d 100 -e 10
   ```

6. In this command, the -v option is used to mount the
$HOME/.celestia-app directory from the host to the /home/celestia
directory in the Docker container.
This allows the txsim binary to access the keyring for the prefunded account.

Congratulations! You have successfuly set up celestia-app in Docker 😎.

## Docker Compose

You can also run the `txsim` Docker image using Docker Compose.
Here's an example `docker-compose.yml` file:

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

In this file, the `volumes` key is used to mount
the `/Users/txsimp/.celestia-app directory from the host to 
the `/home/celestia` directory in the Docker container.

## Kubernetes Deployment

Finally, you can run the `txsim` Docker image in a Kubernetes cluster.
Here's an example `deployment.yaml` file:

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

In this file, the `volumeMounts` and `volumes` keys are used to mount the 
`/Users/txsimp/.celestia-app` directory from the host to the `/home/celestia`
directory in the Docker container.

## Flag Breakdown

The table below provides a brief explanation of the
flags used in the docker run command in step 5 of the quick start instructions.

| FLAG | DESCRIPTION | DEFAULT | OPTION |
| ---- | ---- | ---- | :----: |
|`-k`|Whether a new key should be created|0|1 for yes, 0 for no|
|`-p`|Path to keyring for prefunded account|-|-|
|`-g`|gRPC endpoint|consensus-validator-robusta-rc6.celestia-robusta.com:9090||
|`-t`|Poll time for the `txsim` binary|10s|1s,2s,3s,4s,...|
|`-b`|Number of blob sequences to run|10|any integer value(1,2,3,...)|
|`-a`|Range of blobs to send per PFB in a sequence|-|-|
|`-s`|Range of blob sizes to send|-|-|
|`-m`|Mnemonic for the keyring |-|-|
|`-d`|Seed for the random number generator|100|any integer value (1,2,3,...)|
|`-e`|Number of send sequences to run|10|any integer value (1,2,3,...)|
|`-i`|Amount to send from one account to another|-|any integer value (1,2,3,...)|
|`-v`|Number of send iterations to run per sequence|-|any integer value (1,2,3,...)|
|`-u`|Number of stake sequences to run|-|any integer value (1,2,3,...)|
|`-w`|Amount of initial stake per sequence|-|any integer value (1,2,3,...)|

Kindly replace the placeholders in the example docker run
command in step 5 of the quick start instructions,
with the actual values you want to use.

