# Scripts

This directory contains a handful of scripts that may be helpful for contributors.

## build-run-single-node.sh

This script will build the project and run a single node devnet. After running this script, the text output will contain a "Home directory" that you can use as a parameter for subsequent commands.

```bash
./scripts/build-run-single-node.sh
Home directory: /var/folders/_8/ljj6hspn0kn09qf9fy8kdyh40000gn/T/celestia_app_XXXXXXXXXXXXX.XV92a3qx
--> Updating go.mod
...
```

In a new terminal tab, export the home directory:

```bash
export CELESTIA_APP_HOME=/var/folders/_8/ljj6hspn0kn09qf9fy8kdyh40000gn/T/celestia_app_XXXXXXXXXXXXX.XV92a3qx
```

In subsequent commands, pass the `--home $CELESTIA_APP_HOME` flag:

```bash
./build/celestia-appd keys list validator --home $CELESTIA_APP_HOME
- address: celestia1grvklux2yjsln7ztk6slv538396qatckqhs86z
  name: validator
  pubkey: '{"@type":"/cosmos.crypto.secp256k1.PubKey","key":"A5R27GO4uGtzu7LVOxneiA3i59Bi7SlDr6FHaGfy47mI"}'
  type: local
```

Note: this script is used in <https://github.com/celestiaorg/docs> so please update the docs repo if you make breaking changes to this script.
