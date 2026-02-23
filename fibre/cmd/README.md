# Fibre Server

Standalone binary for the Fibre data availability server.

## Build

```sh
make build-fibre
```

The binary is output to `build/fibre`.

## Usage

### Start

```sh
fibre start
```

On first run, initializes `~/.celestia-fibre` with a default TOML config.
Subsequent runs load the existing config.

Override the home directory:

```sh
fibre start --home /path/to/fibre-home
# or
FIBRE_HOME=/path/to/fibre-home fibre start
```

Override config values with flags (flags take precedence over config file):

```sh
fibre start \
  --app-grpc-address 127.0.0.1:9090 \
  --server-listen-address 0.0.0.0:7980 \
  --signer-listen-address tcp://127.0.0.1:26659
```

### Version

```sh
fibre version
```

## Config

The config file is at `$FIBRE_HOME/server_config.toml` (default `~/.celestia-fibre/server_config.toml`).

Config precedence: **flag > config file > default**.

## Remote Signer

Fibre uses a remote PrivVal signer (e.g. [tmkms](https://github.com/iqlusioninc/tmkms)) to sign payment promises. The signer **dials into** the fibre server's listener address.

### How it works

1. Fibre opens a TCP listener on `--signer-listen-address` (default `tcp://127.0.0.1:26659`)
2. An external signer (tmkms) dials into this address
3. Fibre fetches and caches the public key from the signer on startup
4. Payment promises are signed through this connection for the server's lifetime

### Setup with tmkms

Configure tmkms to connect to the fibre server's signer address. In your tmkms `tmkms.toml`:

```toml
[[validator]]
addr = "tcp://127.0.0.1:26659"  # must match fibre's --signer-listen-address
chain_id = "celestia"
```

### Note on startup order

Fibre blocks during startup until the remote signer connects. Make sure tmkms is running and reachable before or shortly after starting fibre, otherwise startup will hang.

## Signals

- First `SIGINT`/`SIGTERM`: graceful shutdown
- Second signal: force shutdown
