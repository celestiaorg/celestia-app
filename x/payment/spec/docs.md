## Abstract

The payment module is responsible for paying for arbitrary data that will be added to the Celestia blockchain. While the data being submitted can be arbitrary, the exact placement of that data is important for the transaction to be valid. This is why the payment module utilizes a malleated transaction scheme. Malleated transactions allow for users to create a single transaction, that can later be malleated by the block producer to create a variety of different valid transactions that are still signed over by the user. To accomplish this, users to create a single `MsgWirePayForMessage` transaction, which is composed of metadata and signatures for multiple variations of the transaction that will be included onchain. After the transaction is submitted to the network, the block producer selects the appropriate signature and creates a valid `MsgPayForMessage` transaction depending on the square size for that block. This new malleated `MsgPayForMessage` transaction is what ends up onchain. 

## State
The only state that is modified is the sender’s account balance, via the bank keeper’s `Burn` method.

## Messages
- [`MsgWirePayForMessage`](https://github.com/celestiaorg/celestia-app/blob/b4c8ebdf35db200a9b99d295a13de01110802af4/x/payment/types/tx.pb.go#L32-L40)

While this transaction is created and signed by the user, it never actually ends up onchain. Instead, it is used to create a new “malleated” transaction that does get included onchain.
- [`MsgPayForMessage`](https://github.com/celestiaorg/celestia-app/blob/b4c8ebdf35db200a9b99d295a13de01110802af4/x/payment/types/tx.pb.go#L208-L216)

The malleated transaction that is created from metadata contained in the original`MsgWirePayFormessage`. It also burns some of the sender’s funds.

## PreProcessTxs
The malleation process occurs during the PreProcessTxs step.

## Events
TODO after events are added.

## Parameters
There are no parameters yet, but we might add
- BaseFee
- SquareSize
- ShareSize

Further reading: [Message Block Layout](https://github.com/celestiaorg/celestia-specs/blob/master/src/rationale/message_block_layout.md)

### Usage 
`celestia-app tx payment payForMessage <hex encoded namespace> <hex encoded data> [flags]`

### Programmatic Usage
There are tools to programmatically create, sign, and broadcast `MsgWirePayForMessages`
```go
// use a keyring to sign messages programmatically 
keyringSigner, err := NewKeyringSigner(keyring, "keyring account name", "chain-id-1")
if err != nil {
    return err
}

// Query for account information necessary to sign a valid tx
err = keyringSigner.QueryAccount(ctx, grpcClientConn)
if err != nil {
    return err
}

// create the raw WirePayForMessage transaction
wpfmMsg, err := apptypes.NewWirePayForMessage(block.Header.NamespaceId, message, 16, 32, 64, 128)
if err != nil {
    return err
}

// create  and sign the commitments to the data for all the the square sizes 
gasLimOption := types.SetGasLimit(200000)
err = pfmMsg.SignShareCommitments(keyringSigner, gasLimOption)
if err != nil {
    return err
}

signedTx, err := keyringSigner.BuildSignedTx(wpfmMsg, gasLimOption)
if err != nil {
    return err
}
```

### How the commitments are generated
1) create the final version of the message by adding the length delimiter, the namespace, and then the message together into a single string of bytes
```
finalMessage = [length delimiter] + [namespace] + [message]
```
2) chunk the finalMessage into shares of size `consts.ShareSize`
3) pad until number of shares is a power of two
4) create the commitment by aranging the shares into a merkle mountain range
5) create a merkle root of the subtree roots
