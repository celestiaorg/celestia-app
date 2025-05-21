# talis

## Usage

if the relevant binaries are installed via go, and the celestia-app repo is
downloaded, then the talis defaults should work. Make sure your `$GOPATH` is
exported.

```sh
# initializes the repo w/ editable scripts and configs
talis init -c <chain-id> -e <experiment>
# adds specific nodes to the config (see flags for further configuration)
talis add  -t <node-type> -c <count>
# uses the config to spin up nodes on the relevant cloud services
talis up
# creates the payload for the network. This contains all addresses, configs, binaries (from your local machine if not specified), genesis, and startup scripts.  
talis genesis
# sends the payload to each node and boots the network by executing the relevant startup scripts
talis deploy
# tears down the network
talis down
```
