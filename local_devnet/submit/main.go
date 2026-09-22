package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	blobtypes "github.com/celestiaorg/celestia-app/v10/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	kind := flag.String("kind", "pfb", "pfb or pff")
	count := flag.Int("count", 1, "number of transactions (one blob each)")
	size := flag.Int("size", 1024, "blob size in bytes (1..2097152)")
	interval := flag.Duration("interval", 0, "delay between confirmed transactions")
	timeout := flag.Duration("timeout", time.Minute, "timeout per transaction, including download")
	verify := flag.Bool("verify", false, "download each PFF blob and compare its bytes")
	flag.Parse()
	if flag.NArg() != 0 || (*kind != "pfb" && *kind != "pff") || *count < 1 || *size < 1 || *size > 2<<20 || *interval < 0 || *timeout <= 0 || (*verify && *kind != "pff") {
		return fmt.Errorf("invalid arguments; see --help")
	}
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	kr, err := keyring.New(app.Name, keyring.BackendTest, "/data/test", nil, enc.Codec)
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient("validator-1:9090", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	tx, err := user.SetupTxClient(ctx, kr, conn, enc, user.WithDefaultAccount("test"))
	if err != nil {
		return err
	}
	var client *fibre.Client
	if *kind == "pff" {
		params := fibre.DefaultProtocolParams
		params.MaxValidatorCount = 2
		cfg := fibre.NewClientConfigFromParams(params)
		cfg.StateAddress, cfg.DefaultKeyName = "validator-1:9090", "test"
		client, err = fibre.NewClient(kr, cfg)
		if err != nil {
			return err
		}
		if err = client.Start(ctx); err != nil {
			return err
		}
		defer func() {
			stopCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			_ = client.Stop(stopCtx)
		}()
	}
	ns, err := share.NewV0Namespace([]byte("localdev"))
	if err != nil {
		return err
	}
	for i := 0; i < *count; i++ {
		data := make([]byte, *size)
		if _, err = rand.Read(data); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		err = submit(ctx, tx, client, ns, data, *verify)
		cancel()
		if err != nil {
			return fmt.Errorf("%s %d/%d: %w", *kind, i+1, *count, err)
		}
		if i+1 < *count {
			time.Sleep(*interval)
		}
	}
	return nil
}

func submit(ctx context.Context, tx *user.TxClient, client *fibre.Client, ns share.Namespace, data []byte, verify bool) error {
	if client == nil {
		blob, err := blobtypes.NewV0Blob(ns, data)
		if err != nil {
			return err
		}
		result, err := tx.SubmitPayForBlob(ctx, []*share.Blob{blob})
		if err != nil {
			return err
		}
		fmt.Printf("PFB committed height=%d tx=%s bytes=%d\n", result.Height, result.TxHash, len(data))
		return nil
	}
	result, err := fibre.Put(ctx, client, tx, ns, data)
	if err != nil {
		return err
	}
	fmt.Printf("PFF committed height=%d tx=%s bytes=%d blob=%s\n", result.Height, result.TxHash, len(data), result.BlobID)
	if verify {
		blob, err := client.Download(ctx, result.BlobID, fibre.WithHeight(result.Height))
		if err != nil {
			return err
		}
		defer blob.Free()
		if !bytes.Equal(data, blob.Data()) {
			return fmt.Errorf("downloaded data does not match")
		}
		fmt.Println("PFF download verified")
	}
	return nil
}
