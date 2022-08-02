# Abstract

The payment module is responsible for paying for arbitrary data that will be added to the Celestia blockchain. While the data being submitted can be arbitrary, the exact placement of that data is important for the transaction to be valid. This is why the payment module utilizes a malleated transaction scheme. Malleated transactions allow for users to create a single transaction, that can later be malleated by the block producer to create a variety of different valid transactions that are still signed over by the user. To accomplish this, users create a single `MsgWirePayForData` transaction, which is composed of metadata and signatures for multiple variations of the transaction that will be included onchain. After the transaction is submitted to the network, the block producer selects the appropriate signature and creates a valid `MsgPayForData` transaction depending on the square size for that block. This new malleated `MsgPayForData` transaction is what ends up onchain.

Further reading: [Message Block Layout](https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md)

## State

- The sender’s account balance, via the bank keeper’s [`Burn`](https://github.com/cosmos/cosmos-sdk/blob/531bf5084516425e8e3d24bae637601b4d36a191/x/bank/spec/01_state.md) method.
- The standard incrememnt of the sender's account number via the [auth module](https://github.com/cosmos/cosmos-sdk/blob/531bf5084516425e8e3d24bae637601b4d36a191/x/auth/spec/02_state.md).

## Messages

- [`MsgWirePayForData`](https://github.com/celestiaorg/celestia-app/blob/b4c8ebdf35db200a9b99d295a13de01110802af4/x/payment/types/tx.pb.go#L32-L40)

While this transaction is created and signed by the user, it never actually ends up onchain. Instead, it is used to create a new "malleated" transaction that does get included onchain.

- [`MsgPayForData`](https://github.com/celestiaorg/celestia-app/blob/b4c8ebdf35db200a9b99d295a13de01110802af4/x/payment/types/tx.pb.go#L208-L216)

The malleated transaction that is created from metadata contained in the original `MsgWirePayForData`. It also burns some of the sender’s funds.

## PrepareProposal

The malleation process occurs during the PrepareProposal step.

<!-- markdownlint-disable MD010 -->
```go
// ProcessWirePayForData will perform the processing required by PrepareProposal.
// It parses the MsgWirePayForData to produce the components needed to create a
// single  MsgPayForData
func ProcessWirePayForData(msg *MsgWirePayForData, squareSize uint64) (*tmproto.Message, *MsgPayForData, []byte, error) {
	// make sure that a ShareCommitAndSignature of the correct size is
	// included in the message
	var shareCommit *ShareCommitAndSignature
	for _, commit := range msg.MessageShareCommitment {
		if commit.K == squareSize {
			shareCommit = &commit
		}
	}
	if shareCommit == nil {
		return nil,
			nil,
			nil,
			fmt.Errorf("message does not commit to current square size: %d", squareSize)
	}

	// add the message to the list of core message to be returned to ll-core
	coreMsg := tmproto.Message{
		NamespaceId: msg.GetMessageNameSpaceId(),
		Data:        msg.GetMessage(),
	}

	// wrap the signed transaction data
	pfd, err := msg.unsignedPayForData(squareSize)
	if err != nil {
		return nil, nil, nil, err
	}

	return &coreMsg, pfd, shareCommit.Signature, nil
}

// PrepareProposal fullfills the celestia-core version of the ACBI interface by
// preparing the proposal block data. The square size is determined by first
// estimating it via the size of the passed block data. Then the included
// MsgWirePayForData messages are malleated into MsgPayForData messages by
// separating the message and transaction that pays for that message. Lastly,
// this method generates the data root for the proposal block and passes it the
// blockdata.
func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	squareSize := app.estimateSquareSize(req.BlockData)

	dataSquare, data := SplitShares(app.txConfig, squareSize, req.BlockData)

	eds, err := da.ExtendShares(squareSize, dataSquare)
	if err != nil {
		app.Logger().Error(
			"failure to erasure the data square while creating a proposal block",
			"error",
			err.Error(),
		)
		panic(err)
	}

	dah := da.NewDataAvailabilityHeader(eds)
	data.Hash = dah.Hash()
	data.OriginalSquareSize = squareSize

	return abci.ResponsePrepareProposal{
		BlockData: data,
	}
}
```
<!-- markdownlint-enable MD010 -->

## Events

- [`NewPayForDataEvent`](https://github.com/celestiaorg/celestia-app/pull/213/files#diff-1ce55bda42cf160deca2e5ea1f4382b65f3b689c7e00c88085d7ce219e77303dR17-R21)
Emit an event that has the signer's address and size of the message that is paid for.

## Parameters

There are no parameters yet, but we might add

- BaseFee
- SquareSize
- ShareSize

### Usage

`celestia-app tx payment payForData <hex encoded namespace> <hex encoded data> [flags]`

### Programmatic Usage

There are tools to programmatically create, sign, and broadcast `MsgWirePayForDatas`

<!-- markdownlint-disable MD010 -->
```go
// create the raw WirePayForData transaction
wpfdMsg, err := apptypes.NewWirePayForData(block.Header.NamespaceId, message, 16, 32, 64, 128)
if err != nil {
    return err
}

// we need to create a signature for each `MsgPayForData`s that
// could be generated by the block producer
// to do this, we create a custom `KeyringSigner` to sign messages programmatically
// which uses the standard cosmos-sdk `Keyring` to sign each `MsgPayForData`
keyringSigner, err := NewKeyringSigner(keyring, "keyring account name", "chain-id-1")
if err != nil {
    return err
}

// query for account information necessary to sign a valid tx
err = keyringSigner.QueryAccount(ctx, grpcClientConn)
if err != nil {
    return err
}

// generate the signatures for each `MsgPayForData` using the `KeyringSigner`,
// then set the gas limit for the tx
gasLimOption := types.SetGasLimit(200000)
err = pfdMsg.SignShareCommitments(keyringSigner, gasLimOption)
if err != nil {
    return err
}

// Build and sign the final `WirePayForData` tx that now contians the signatures
// for potential `MsgPayForData`s
signedTx, err := keyringSigner.BuildSignedTx(
    gasLimOption(signer.NewTxBuilder()),
    wpfdMsg,
)
if err != nil {
    return err
}
```
<!-- markdownlint-enable MD010 -->

### How the commitments are generated

1. create the final version of the message by adding the length delimiter, the namespace, and then the message together into a single string of bytes

    ```python
    finalMessage = [length delimiter] + [namespace] + [message]
    ```

2. chunk the finalMessage into shares of size `consts.ShareSize`
3. pad until number of shares is a power of two
4. create the commitment by aranging the shares into a merkle mountain range
5. create a merkle root of the subtree roots
