# talis

## Install

```sh
go install ./tools/talis/
```

All binaries used by nodes in the network are compiled on the user's local machine. Either change the target when compiling celestia-app, or use the docker image to ensure complete compatibility.

```
make build-talis-bins
```

Note that this doesn't install binaries in the `$GOPATH/bin`, so you must specify the path when creating the payload with the `genesis` subcommand and `-a` (`--app-binary` path) and `-t` (`--txsim-binary` path) flags. See `genesis` subcomand usage below.

## Usage

if the relevant binaries are installed via go, and the celestia-app repo is
downloaded, then the talis defaults should work. Your `$GOPATH` is used to copy the scripts from this repo to the payload, along with default locations for the binaries.

### init

```sh
# initializes the repo w/ editable scripts and configs
talis init -c <chain-id> -e <experiment>
```

This will initiallize the directory that contains directory structure used for conducting an experiment.

```
.
├── app.toml
├── config.json
├── config.toml
├── data/
├── payload/
└── scripts/
```

the celestia-app configs (config.toml and app.toml) can be manually edited here, and they will copied to each node. `config.json` is the talis specific configuration file that contains all info related to spinning up the network. This is updated after the nodes have been spun up. Basic defaults are set, but the relevant fields can either be edited after generation or via using a flag. At this point, it looks something like this:

```json
{
  "validators": [],
  "chain_id": "talis-test-3",
  "experiment": "test-3",
  "ssh_pub_key_path": "/home/HOSTNAME/.ssh/id_ed25519.pub",
  "ssh_key_name": "HOSTNAME",
  "digitalocean_token": "pulled from env var if available",
  "s3_config": {
    "region": "pulled from env var if available",
    "access_key_id": "pulled from env var if available",
    "secret_access_key": "pulled from env var if available",
    "bucket_name": "pulled from env var if available"
  }
}
```

### add

```sh
# adds specific nodes to the config (see flags for further configuration)
talis add  -t <node-type> -c <count>
```

If we call:

```
talis add -t validator -t 1
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

### up

`up` uses the configuration to spin up the cloud instances. Note that this doesn't start the network!

```sh
# uses the config to spin up nodes on the relevant cloud services
talis up
```

### genesis

Before we can start the network, we need to create a payload that contains everything each instance needs to actually start the network. This includes all the required keys, configs, genesis.json, and startup scripts. The `--square-size` flag will change the `GovMaxSquareSize`.

```sh
# creates the payload for the network. This contains all addresses, configs, binaries (from your local GOPATH if not specified), genesis.json, and startup scripts. The `--square-size` flag will change the `GovMaxSquareSize`
talis genesis -s 128 -a /home/$HOSTNAME/go/src/github.com/celestiaorg/celestia-app/build/celestia-appd -t /home/$HOSTNAME/go/src/github.com/celestiaorg/celestia-app/build/txsim
```

Keep in mind that we can still edit anything in the payload before deploying the network. After creating it is when that would occur.

### deploy

This step is when the network is actually started. The payload is uploaded to each instance in the network directly from the user's machine. After delivering the payload, the start script is executed in a tmux session called "app" on each machine.

```sh
# sends the payload to each node and boots the network by executing the relevant startup scripts
talis deploy
```

### txsim

To load the network we can use `talis` to start txsim on as many validator nodes as we want for that experiment.

```sh
# start txsim on some number of the validator instances
talis txsim -i <count> -s <blob-sequences> --min-blob-size <size> --max-blob-size <size>
```

### status

Often, its useful to quickly check if all the nodes have caught up to the tip of the chain. This can be done via the status command, which simply prints the height of each validator after querying the `Status` endpoint.

```sh
# check which height each validator is at
talis status
```

### traces

To download traces from the network, we can use `talis` to download traces from as many validator nodes as we want for that experiment.

```sh
# download some number of traces directly from nodes to your machine via sftp
talis download -n <validator-*> -t <table> [flags]
```

To quickly view block times, assuming this table was being traced we can run:

```
talis download -n validator-0 -t consensus_block
```

or if we needed to quickly see all of the mempool traces:

```
talis download -n validator-* -t mempool_tx
```

or if we want to check on the logs we can call:

```
talis download -n validator-* -t logs
```

### Collecting all traces to an s3 bucket

At the end of the experiment, we can quickly save all of the traces to an s3 bucket assuming that we filled out the s3 config in the config.json.

```
talis upload-data
```

This could take a few minutes if there is a ton of trace data, but often is completed in <30s. To download this data from the s3 bucket, we can use the s3 subcommand:

```
talis download s3
```

### Modifying the nodes in place

Instead of shutting down all of the nodes, if we want to run a slightly modified experiment, we can simply rerun the `genesis` and `deploy` commands. This will create a new payload and restart the network without tearing down the cloud instances. This will delete any trace data.

### down

Finally, remember to tear down the cloud instances. This should work first try, but its a good habit to re-run or check the webUI for large experiments to make sure nodes were shut down successfully.

```sh
# tears down the network
talis down
```
