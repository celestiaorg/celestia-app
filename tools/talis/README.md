# talis

## Prerequisite - DigitalOcean setup

### DigitalOcean Account

- If you're part of the Celestia engineering team ask for access to Celestia's DigitalOcean account or alternatively use a personal account.
- **Generate the API token:** Go to Settings → API → Generate New Token.
- Save the token somewhere that's easily accessible.

### SSH Key

- For quick and easy testing, create a new SSH key without a passphrase:

```sh
ssh-keygen -t ed25519 -f ~/.ssh/id_ed25519_no_passphrase -N ""
```

- Upload the SSH key to DigitalOcean:
- Navigate to Settings → Security → SSH Keys.
- Click "Add SSH Key".
- Paste your public key.

```sh
cat ~/.ssh/id_ed25519_no_passphrase.pub
```

- Add your name into the name for quick and easy access we'll need this later. Now your key should appear in "SSH Keys" list.

## Running talis

You have two options when it comes to running talis. You can run it on your local machine which has high RAM requirements or you can run it inside of a DigitalOcean droplet. The guide for this will be at the end of the file.

## Install

```sh
go install ./tools/talis/
```

All binaries used by nodes in the network are compiled on the user's local machine. Either change the target when compiling celestia-app, or use the docker image to ensure complete compatibility.

```sh
make build-talis-bins
```

Note that this doesn't install binaries in the `$GOPATH/bin`, so you must specify the path when creating the payload with the `genesis` subcommand and `-a` (`--app-binary` path) and `-t` (`--txsim-binary` path) flags. See `genesis` subcommand usage below.

## Usage

If the relevant binaries are installed via go, and the celestia-app repo is
downloaded, then the talis defaults should work. Your `$GOPATH` is used to copy the scripts from this repo to the payload, along with default locations for the binaries.

### init

```sh
# initializes the repo w/ editable scripts and configs
talis init -c <chain-id> -e <experiment>
```

This will initialize the directory that contains directory structure used for conducting an experiment.

```
.
├── app.toml
├── config.json
├── config.toml
├── data/
├── payload/
└── scripts/
```

the celestia-app configs (config.toml and app.toml) can be manually edited here, and they will be copied to each node. `config.json` is the talis specific configuration file that contains all info related to spinning up the network. This is updated after the nodes have been spun up. Basic defaults are set, but the relevant fields can either be edited after generation or via using a flag. At this point, it looks something like this:

```json
{
  "validators": [],
  "chain_id": "talis-test-3",
  "experiment": "test-3",
  "ssh_pub_key_path": "/home/HOSTNAME/.ssh/id_ed25519.pub",
  "ssh_key_name": "HOSTNAME",
  "digitalocean_token": "pulled from env var if available",
  "s3_config": {
    "region": "pulled from AWS_DEFAULT_REGION env var if available",
    "access_key_id": "pulled from AWS_ACCESS_KEY_ID env var if available",
    "secret_access_key": "pulled from AWS_SECRET_ACCESS_KEY env var if available",
    "bucket_name": "pulled from AWS_S3_BUCKET env var if available",
    "endpoint": "pulled from AWS_S3_ENDPOINT env var if available. Can be left empty if targeting an AWS S3 bucket"
  }
}
```

Notes:

- The AWS config supports any S3-compatible bucket. So it can be used with Digital Ocean and other cloud providers.
- Example: The S3 endpoint for Digital Ocean is: `https://<region>.digitaloceanspaces.com/`.

### add

```sh
# adds specific nodes to the config (see flags for further configuration)
talis add  -t <node-type> -c <count>
```

If we call:

```sh
talis add -t validator -c 1
```

we will see the config updated to:

```json
{
  "validators": [
    {
      "node_type": "validator",
      "public_ip": "TBD",
      "private_ip": "TBD",
      "provider": "digitalocean",
      "slug": "c2-16vcpu-32gb",
      "region": "nyc3", // randomly determined unless specified.
      "name": "validator-0",
      "tags": [
        "talis",
        "validator",
        "validator-0",
        "chainID"
      ]
    }
  ],
  ...
  "chain_id": "talis-test",
  "experiment": "test",
  "ssh_pub_key_path": "/home/HOSTNAME/.ssh/id_ed25519.pub",
  "ssh_key_name": "HOSTNAME",
  ...
}
```

### Export env vars

```sh
export DIGITALOCEAN_TOKEN="your_api_token_here"
export TALIS_SSH_KEY_PATH="your_ssh_key_path_here"
```

### up

`up` uses the configuration to spin up the cloud instances. Note that this doesn't start the network!

```sh
# uses the config to spin up nodes on the relevant cloud services
talis up

# use more workers for faster instance creation. DigitalOcean has a 5000 requests/hour rate limit per API token.
# For droplet creation, each worker makes ~3-5 API calls per droplet, so ~20 workers should be safe for most use cases.
talis up --workers 20
```

### genesis

Before we can start the network, we need to create a payload that contains everything each instance needs to actually start the network. This includes all the required keys, configs, genesis.json, and startup scripts. The `--square-size` flag will change the `GovMaxSquareSize`. By default, the binaries in the $GOPATH/bin will be used, however if specific binaries are needed (likely unless you are running some flavor of debian), use the -a (-a, --app-binary) and -t (-t, --txsim-binary) flags.

```sh
# creates the payload for the network. This contains all addresses, configs, binaries (from your local GOPATH if not specified), genesis.json, and startup scripts. The `--square-size` flag will change the `GovMaxSquareSize`
talis genesis -s 128 -a /home/$HOSTNAME/go/src/github.com/celestiaorg/celestia-app/build/celestia-appd -t /home/$HOSTNAME/go/src/github.com/celestiaorg/celestia-app/build/txsim
```

Keep in mind that we can still edit anything in the payload before deploying the network.

Note: When increasing the genesis square size, ensure you also increase the `SquareSizeUpperBound` constant to allow blocks to be created at the new size.

### deploy

This step is when the network is actually started. The payload is uploaded to each instance in the network directly from the user's machine. After delivering the payload, the start script is executed in a tmux session called "app" on each machine.

```sh
# sends the payload to each node and boots the network by executing the relevant startup scripts
talis deploy

# use more workers for faster deployment (when using direct upload)
talis deploy --direct-payload-upload --workers 20
```

Note: By default, the `deploy` command will upload the payload to the configured S3 bucket, and then download it in the nodes. To upload the payload directly without passing by S3, use the `--direct-payload-upload` flag. The `--workers` flag only affects the direct upload method.

### txsim

To load the network we can use `talis` to start txsim on as many validator nodes as we want for that experiment.

```sh
# start txsim on some number of the validator instances
talis txsim -i <count> -s <blob-sequences> --min-blob-size <size> --max-blob-size <size>
```

### status

Often, it's useful to quickly check if all the nodes have caught up to the tip of the chain. This can be done via the status command, which simply prints the height of each validator after querying the `Status` endpoint.

```sh
# check which height each validator is at
talis status
```

### traces

To download traces from the network, we can use `talis` to download traces from as many validator nodes as we want for that experiment.

```sh
# download some number of traces directly from nodes to your machine via sftp
talis download -n <validator-*> -t <table> [flags]

# use more workers for faster downloads from many nodes
talis download -n <validator-*> -t <table> --workers 20
```

To quickly view block times, assuming this table was being traced we can run:

```sh
talis download -n validator-0 -t consensus_block
```

or if we needed to quickly see all of the mempool traces:

```sh
talis download -n validator-* -t mempool_tx
```

or if we want to check on the logs we can call:

```sh
talis download -n validator-* -t logs
```

### Collecting all traces to an s3 bucket

At the end of the experiment, we can quickly save all of the traces to an s3 bucket assuming that we filled out the s3 config in the config.json.

```sh
talis upload-data
```

This could take a few minutes if there is a ton of trace data, but often is completed in <30s. To download this data from the s3 bucket, we can use the s3 subcommand:

```sh
talis download s3
```

### Modifying the nodes in place

Instead of shutting down all of the nodes, if we want to run a slightly modified experiment, we can simply run the [reset](#reset) command then rerun the `genesis` and `deploy` commands. This will create a new payload and restart the network without tearing down the cloud instances. This will delete any trace data.

### reset

This command allows you to stop running services and clean up files created by the `deploy` command for either specific validators or all validators in the network.

```sh
# Reset all validators in the network
talis reset

# Reset specific validators
talis reset -v validator-0,validator-1
```

### down

Finally, remember to tear down the cloud instances. This should work first try, but it's a good habit to re-run or check the webUI for large experiments to make sure nodes were shut down successfully.

```sh
# tears down the network
talis down

# use more workers for faster teardown of many instances
talis down --workers 20
```

## Running Talis inside of a DigitalOcean droplet

Create a new droplet:

- Recommended Size: 32GB RAM 16CPU
- SSH Keys: Add your SSH key

SSH into the Droplet:

```sh
ssh root@YOUR_DROPLET_IP
```

Install Deps:

```sh
# Install Go
snap install go --channel=1.24/stable --classic

# Install Docker
apt install docker.io -y
systemctl start docker
usermod -aG docker $USER

# Install misc tools
apt install git curl jq -y
```

Set up Go env:

```sh
echo 'export GOPATH="$HOME/go"' >> ~/.profile
echo 'export GOBIN="$GOPATH/bin"' >> ~/.profile
echo 'export PATH="$GOBIN:$PATH"' >> ~/.profile
source ~/.profile
```

Clone and build:

```sh
# Clone celestia-app and cd into it
git clone https://github.com/celestiaorg/celestia-app.git
cd celestia-app

# Build binaries (celestia, celestia-appd, txsim)
make build-talis-bins

# Install talis
go install ./tools/talis/
```

Set env variables:

```sh
export DIGITALOCEAN_TOKEN="your_api_token_here"
export TALIS_SSH_KEY_PATH="~/.ssh/id_ed25519_no_passphrase"
```

**Run Talis:**

Talis assumes that you're your default ssh key so if you created a new key above you need to specify it in the commands.

```sh
# Initialize
talis init -c your-chain-id -e your-experiment

# Add validators
talis add -t validator -c <count>

# Spin up talis (use more workers if creating many instances)
talis up -n <key-name> -s <path-to-ssh-key> --workers 20

# Create payload
talis genesis -s 128 -a  build/celestia-appd -t build/txsim

# Deploy (use more workers for faster direct deployment)
talis deploy -s <path-to-ssh-key> --direct-payload-upload --workers 20
```

**Save Snapshot:**

After you're done running experiments, make sure to take a snapshot of your deployment droplet and destroy the original.
