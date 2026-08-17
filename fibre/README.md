# Fibre

Fibre is Celestia's data availability protocol served by validator-operated fibre servers. It uses verifiable erasure coding to disseminate blob data across the validator set, so blobs can be retrieved under honest-majority assumptions without full replication — and without riding the data square.

This package is the Go client. Depending on what you want to do:

- **Publish and retrieve blobs** — this page.
- **Run a fibre server (validators)** — [cmd/README.md](./cmd/README.md).
- **On-chain payment module reference** — [x/fibre/README.md](../x/fibre/README.md).
- **Protocol specification** — [specs/src/fibre.md](../specs/src/fibre.md).

## Requirements

- A network running app version 10 or later, with bonded validators that have [registered fibre hosts](../x/valaddr/README.md) — the client discovers servers through that registry.
- gRPC access to a `celestia-appd` node (default `127.0.0.1:9090`).
- A funded account in a local keyring, plus a funded fibre **escrow account** for that key (see [Escrow](#escrow)).

## Quickstart

Upload a blob with `Put`, then fetch it back by its `BlobID`:

```go
package main

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	ctx := context.Background()
	const (
		keyName     = "mykey"
		grpcAddress = "127.0.0.1:9090"
	)

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr, err := keyring.New(app.Name, keyring.BackendTest, "/path/to/keyring-dir", nil, encCfg.Codec)
	if err != nil {
		panic(err)
	}

	// The fibre client: discovers validators' fibre servers via the app node
	// and handles shard upload/download.
	cfg := fibre.DefaultClientConfig()
	cfg.StateAddress = grpcAddress
	cfg.DefaultKeyName = keyName

	client, err := fibre.NewClient(kr, cfg)
	if err != nil {
		panic(err)
	}
	if err := client.Start(ctx); err != nil {
		panic(err)
	}
	defer client.Stop(ctx)

	// The tx client: signs and broadcasts the MsgPayForFibre that settles payment.
	conn, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	txClient, err := user.SetupTxClient(ctx, kr, conn, encCfg, user.WithDefaultAccount(keyName))
	if err != nil {
		panic(err)
	}

	// Upload: encodes the data, uploads shards to the assigned validators,
	// collects their endorsements, and submits the PayForFibre transaction.
	ns := share.MustNewV0Namespace([]byte("example"))
	result, err := fibre.Put(ctx, client, txClient, ns, []byte("hello fibre"))
	if err != nil {
		panic(err)
	}
	fmt.Printf("published: blob_id=%s tx=%s height=%d\n", result.BlobID, result.TxHash, result.Height)

	// Download: retrieves enough shards from the validator set to reconstruct
	// the blob, verifying against the commitment in the BlobID.
	blob, err := client.Download(ctx, result.BlobID)
	if err != nil {
		panic(err)
	}
	fmt.Printf("retrieved %d bytes\n", len(blob.Data()))
}
```

Anyone who knows the `BlobID` can download the blob the same way — only uploading requires a key and escrow.

## Escrow

Fibre payments settle against a prepaid escrow account, not against your regular balance. By default the client does **not** fund it for you; deposit before the first upload:

```shell
celestia-appd tx fibre deposit-to-escrow 1000000utia --from mykey
```

The charge per blob is `650,000 + 45,000 × ⌈blob_size / 256 KiB⌉` utia (see [x/fibre](../x/fibre/README.md#abstract)). Alternatively, set `cfg.Escrow.AutoFund = true` to let the client track a local budget and broadcast deposit transactions automatically as needed — opt-in, because it spends from your account on its own.

## Notes

- `Put` is the convenience path: one blob, one `MsgPayForFibre`, submitted through the provided tx client. For custom transaction handling — fee grants, batching several blobs into a single PFF — use `Client.Upload` and submit the message yourself.
- Uploaded data is retained by fibre servers for a limited window (the `shard_retention` chain parameter, plus whatever servers keep voluntarily). Fibre is not archival storage: download soon after publishing, or persist the data elsewhere.
- The full API and configuration reference (`ClientConfig`, thresholds, timeouts, retries) is specified in [specs/src/fibre_client.md](../specs/src/fibre_client.md).
