package sign

import (
	"errors"
	"fmt"
	"time"

	"github.com/cometbft/cometbft/crypto"
	cmtlog "github.com/cometbft/cometbft/libs/log"
	pvm "github.com/cometbft/cometbft/privval"
	"github.com/cometbft/cometbft/types"
)

const (
	retries      = 50
	retryTimeout = 100 * time.Millisecond
)

// Remote implements [types.PrivValidator] backed by a remote signing service
// (e.g., tmkms) that dials into a listener on the given address.
// It eagerly fetches and caches the public key during construction.
type Remote struct {
	*pvm.RetrySignerClient

	endpoint *pvm.SignerListenerEndpoint
	client   *pvm.SignerClient
	pubKey   crypto.PubKey
}

var _ types.PrivValidator = (*Remote)(nil)

// NewRemote creates a [Remote] that listens on listenAddr
// (e.g., "tcp://127.0.0.1:26659") for an external signer to connect.
func NewRemote(listenAddr, chainID string) (*Remote, error) {
	endpoint, err := pvm.NewSignerListener(listenAddr, cmtlog.NewNopLogger())
	if err != nil {
		return nil, fmt.Errorf("creating privval listener on %s: %w", listenAddr, err)
	}

	client, err := pvm.NewSignerClient(endpoint, chainID)
	if err != nil {
		_ = endpoint.Stop()
		return nil, fmt.Errorf("creating privval signer client: %w", err)
	}

	retrySigner := pvm.NewRetrySignerClient(client, retries, retryTimeout)
	pubKey, err := retrySigner.GetPubKey()
	if err != nil {
		_ = errors.Join(client.Close(), endpoint.Stop())
		return nil, fmt.Errorf("getting public key from remote signer: %w", err)
	}

	return &Remote{
		RetrySignerClient: retrySigner,
		endpoint:          endpoint,
		client:            client,
		pubKey:            pubKey,
	}, nil
}

// GetPubKey returns the cached public key from the remote signer.
func (r *Remote) GetPubKey() (crypto.PubKey, error) {
	return r.pubKey, nil
}

// Close stops the remote signer connection and the underlying listener.
func (r *Remote) Close() error {
	return errors.Join(r.client.Close(), r.endpoint.Stop())
}
