package v2

import (
	"context"

	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/pkg/user"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc"
)

// TxClient is a v2 wrapper around the original TxClient that
// converts all user.TxResponse to sdktypes.TxResponse including the signer info.
type TxClient struct {
	// Embed the underlying client to automatically delegate all methods
	*user.TxClient
}

// NewTxClient creates a new v2 TxClient by wrapping the original NewTxClient function.
func NewTxClient(
	cdc codec.Codec,
	signer *user.Signer,
	conn *grpc.ClientConn,
	registry codectypes.InterfaceRegistry,
	options ...user.Option,
) (*TxClient, error) {
	v1Client, err := user.NewTxClient(cdc, signer, conn, registry, options...)
	if err != nil {
		return nil, err
	}

	return &TxClient{TxClient: v1Client}, nil
}

// SetupTxClient creates and initializes a new v2 TxClient by wrapping the original setupTxClient method.
func SetupTxClient(
	ctx context.Context,
	keys keyring.Keyring,
	conn *grpc.ClientConn,
	encCfg encoding.Config,
	options ...user.Option,
) (*TxClient, error) {
	v1Client, err := user.SetupTxClient(ctx, keys, conn, encCfg, options...)
	if err != nil {
		return nil, err
	}

	return &TxClient{TxClient: v1Client}, nil
}

// Wrapv1TxClient wraps a v1 TxClient and returns a v2 TxClient.
func Wrapv1TxClient(v1Client *user.TxClient) *TxClient {
	return &TxClient{TxClient: v1Client}
}

func (c *TxClient) buildSDKTxResponse(legacyResp *user.TxResponse) *sdktypes.TxResponse {
	return &sdktypes.TxResponse{
		Height:    legacyResp.Height,
		TxHash:    legacyResp.TxHash,
		Code:      legacyResp.Code,
		Codespace: legacyResp.Codespace,
		GasWanted: legacyResp.GasWanted,
		GasUsed:   legacyResp.GasUsed,
		Signers:   legacyResp.Signers,
	}
}

// Override only the methods that have breaking changes from the original TxClient.

// SubmitPayForBlob calls the original SubmitPayForBlob method and returns a v2 sdk.TxResponse.
func (c *TxClient) SubmitPayForBlob(ctx context.Context, blobs []*share.Blob, opts ...user.TxOption) (*sdktypes.TxResponse, error) {
	legacyResp, err := c.TxClient.SubmitPayForBlob(ctx, blobs, opts...)
	if err != nil {
		return nil, err
	}

	return c.buildSDKTxResponse(legacyResp), nil
}

// SubmitPayForBlobWithAccount calls the original SubmitPayForBlobWithAccount method and returns a v2 sdk.TxResponse.
func (c *TxClient) SubmitPayForBlobWithAccount(ctx context.Context, accountName string, blobs []*share.Blob, opts ...user.TxOption) (*sdktypes.TxResponse, error) {
	legacyResp, err := c.TxClient.SubmitPayForBlobWithAccount(ctx, accountName, blobs, opts...)
	if err != nil {
		return nil, err
	}

	return c.buildSDKTxResponse(legacyResp), nil
}

// SubmitTx calls the original SubmitTx method and returns a v2 sdk.TxResponse.
func (c *TxClient) SubmitTx(ctx context.Context, msgs []sdktypes.Msg, opts ...user.TxOption) (*sdktypes.TxResponse, error) {
	legacyResp, err := c.TxClient.SubmitTx(ctx, msgs, opts...)
	if err != nil {
		return nil, err
	}

	return c.buildSDKTxResponse(legacyResp), nil
}

// SubmitPayForBlobToQueue calls the original SubmitPayForBlobToQueue method and returns a v2 sdk.TxResponse.
func (c *TxClient) SubmitPayForBlobToQueue(ctx context.Context, blobs []*share.Blob, opts ...user.TxOption) (*sdktypes.TxResponse, error) {
	legacyResp, err := c.TxClient.SubmitPayForBlobToQueue(ctx, blobs, opts...)
	if err != nil {
		return nil, err
	}

	return c.buildSDKTxResponse(legacyResp), nil
}

// ConfirmTx calls the original ConfirmTx method and returns a v2 sdk.TxResponse.
func (c *TxClient) ConfirmTx(ctx context.Context, txHash string) (*sdktypes.TxResponse, error) {
	legacyResp, err := c.TxClient.ConfirmTx(ctx, txHash)
	if err != nil {
		return nil, err
	}

	return c.buildSDKTxResponse(legacyResp), nil
}
