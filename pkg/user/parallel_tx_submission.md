# Parallel Tx Submission Implementation Implementation

## Concept

We want to be able to submit transactions in parallel. This is done by managing multiple accounts that have feegrants from the main account, and keeping transactions submitted by the user in a queue. It should otherwise be a very similar experience. The only different being that the responses to each tx is passed via a channel that can be retrieved from the TxClient.

## Initiallization

Use the `type Option func(client *TxClient)` to modify the txClient with any options. This includes but is not limited to:
- number of "worker" accounts submitting txs on behalf of the main accounts
- specifying the main account (defaults to the account with the most funds)
- add the entire LocalTxPool struct
we could

## Existing Code

The TxClient already has a txTracker, which acts like a pool of transactions. we should reuse this for our purposes. If its cleaner, please separate out the txTracker into a different struct and add all relevant methods onto that struct. If we don't need that then don't do that.

```go
type TxClient struct {
	mtx      sync.Mutex
	cdc      codec.Codec
	signer   *Signer
	registry codectypes.InterfaceRegistry
	// list of core endpoints for tx submission (primary + additional)
	conns []*grpc.ClientConn
	// how often to poll the network for confirmation of a transaction
	pollTime time.Duration
	// sets the default account with which to submit transactions
	defaultAccount string
	defaultAddress sdktypes.AccAddress
	// txTracker maps the tx hash to the Sequence and signer of the transaction
	// that was submitted to the chain
	txTracker           map[string]txInfo
	gasEstimationClient gasestimation.GasEstimatorClient
}
```

The Signer already has multiple different accounts / account management. We need to reuse that as well

```
type Signer struct {
	keys         keyring.Keyring
	enc          client.TxConfig
	addressCodec address.Codec
	chainID      string
	// accounts is a map from accountName to account. The signer can manage these accounts. They should match the keys on the keyring.
	accounts            map[string]*Account
	addressToAccountMap map[string]string
	signMode            signing.SignMode
}
```
## New Code

### Entrypoint

#### PFBs

similar to the existing mechanism for blindly submitting PFBs
```go
// SubmitPayForBlob forms a transaction from the provided blobs, signs it, and submits it to the chain.
// TxOptions may be provided to set the fee and gas limit.
func (client *TxClient) SubmitPayForBlob(ctx context.Context, blobs []*share.Blob, opts ...TxOption) (*TxResponse, error) {
	resp, err := client.BroadcastPayForBlob(ctx, blobs, opts...)
	if err != nil {
		return nil, err
	}

	return client.ConfirmTx(ctx, resp.TxHash)
}
```

but instead

```
// SubmitPayForBlob forms a transaction from the provided blobs, signs it, and submits it to the chain.
// TxOptions may be provided to set the fee and gas limit.
func (client *TxClient) SubmitPayForBlobParallel(ctx context.Context, blobs []*share.Blob, opts ...TxOption) (id string, error) {
	// create job and jobID for that tx (jobID should be the hash)
	// likely need to modify the

	return client.ConfirmTx(ctx, resp.TxHash)
}

func (client *TxClient) BroadcastPayForBlobWithAccount(ctx context.Context, accountName string, blobs []*share.Blob, opts ...TxOption) (*sdktypes.TxResponse, error) {
	client.mtx.Lock()
	defer client.mtx.Unlock()
	if err := client.checkAccountLoaded(ctx, accountName); err != nil {
		return nil, err
	}
	acc, exists := client.signer.accounts[accountName]
	if !exists {
		return nil, fmt.Errorf("account %s not found", accountName)
	}
	signer := acc.Address().String()
	msg, err := blobtypes.NewMsgPayForBlobs(signer, 0, blobs...)
	if err != nil {
		return nil, err
	}
	gasLimit := uint64(float64(blobtypes.DefaultEstimateGas(msg)))
	fee := uint64(math.Ceil(appconsts.DefaultMinGasPrice * float64(gasLimit)))
	// prepend calculated params, so it can be overwritten in case the user has specified it.
	opts = append([]TxOption{SetGasLimit(gasLimit), SetFee(fee)}, opts...)

	txBytes, _, err := client.signer.CreatePayForBlobs(accountName, blobs, opts...)
	if err != nil {
		return nil, err
	}

	if len(client.conns) > 1 {
		return client.broadcastMulti(ctx, txBytes, accountName)
	}
	return client.broadcastTxAndIncrementSequence(ctx, client.conns[0], txBytes, accountName)
}
```

we likely need to break BroadcastPayForBlobWithAccount into two so that we can get the tx hash for the jobID. if so, then be sure not to repeat code.

### Parallel Implementation

We should use a standard fanout approach with jobs and workers. The worker has access to the TxClient, Signer (although it only uses a single account). It needs to use the standard pipeline for submitting transactions, which includes the gas estimation etc. This should be handled by existing methods to the extent that we can.

### Testing

#### Integration

We need a testnode integration test to check the basic functionality and lifecycle. This could probably be added to an existing txclient integration test that uses testnode, but it could also be its own test. Be sure to add all new tests to the exception in the makefile to not run during the racedetector. you'll see a bunch of other tests there to add to.

#### Unit

Ofc, we need unit tests for all functionality. We should strive for complete coverage of all basics.

On a side node, I don't think we're testing multiple different connections. Try to unit test that, although I don't think we can integration test that as we can't run multiple different consensus nodes.


# Refactor

We just implemented the first iteration but now we need these changes.

// SubmissionResult contains the result of a parallel transaction submission
type SubmissionResult struct {
	TxHash string
	Error  error
}

needs the TxResp

## FeeGranter and account init

We need a method that is called after TxClient Initialization that will submit a single transaction that creates all worker accounts by sending a single utia to each account from the main account and then setting up fee grant. Look how txsim does this for insperation.

This needs to be done before trying to submit blobs if the account doesn't exist. We also need to ensure that all accounts that are asked for are created and included in the account management. do this in a clean and maintainable way.

Change the e2e test to also rely on this mechanism for account creation.
