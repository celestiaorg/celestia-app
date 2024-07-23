# ADR 001: ABCI++ Adoption

## Status

Implemented

## Changelog

- 2022-03-03: Initial Commit

## Context

Among other things, ABCI++ enables arbitrary logic to be performed by the application to create and verify proposal blocks.

We need this functionality in order for block producers to:

- Pick a square size / fill the data square efficiently
- Malleate user transactions
- Separate rollup block data from the stateful portion of the transaction
- Use the appropriate signature for the selected block size
- Follow the [non-interactive default rules](https://github.com/celestiaorg/celestia-specs/blob/65d4d528beafaf8d84f7eb805db940f05f2a2c93/src/rationale/message_block_layout.md#non-interactive-default-rules) (not done here)
- Create Celestia-specific data hash by erasure-coding the block data

We also need this functionality for validators to verify that:

- For every `MsgPayForBlob` (previously `MsgPayForData`) included in the block, there is also a corresponding blob and vice versa.
- The data hash represents the properly-erasure-coded block data for the selected block size.
- The included messages are arranged in the expected locations in the square according to the non-interactive default rules (not done here)

Technically, we don’t have to use ABCI++ yet, we could still test some needed features in the upcoming testnet without it. However, these implementations would not be representative of the implementations that would actually make it to mainnet, as they would have to be significantly different from their ABCI++ counterparts. The decision to adopt ABCI++ earlier becomes easier considering that the tendermint team has already done most of the heavy lifting, and it is possible to start working on the needed features without waiting for the cosmos-sdk team to use them. We explain our plans below to do just this, by using a subset of ABCI++ (ABCI+?) using only the new methods that are necessary, finished, and easy to incorporate into the cosmos-sdk.

## Alternative Approaches

While the adoption of ABCI++ is inevitable given the already made decision by upstream to implement it, here are some alternatives to the features that we need that do not use ABCI++:

- [Alternatives for Message Inclusion.](https://github.com/celestiaorg/celestia-app/blob/92341dd68ee6e555ec6c0bb780afa3a1c8243a93/adrs/adr008:adopt-ABC%2B%2B-early.md#alternative-approaches)
- [Alternatives for Picking a square size.](https://github.com/celestiaorg/celestia-core/issues/454)

## Detailed Design

### [#631](https://github.com/celestiaorg/celestia-core/pull/631) Simplified version of ABCI++ (`celestia-core`)

Here we are adding only the two new methods that are necessary for the features that we need.

```go
// Application is an interface that enables any finite, deterministic state machine
// to be driven by a blockchain-based replication engine via the ABCI.
type Application interface {
   ...
   PrepareProposal(RequestPrepareProposal) ResponsePrepareProposal
   ProcessProposal(RequestProcessProposal) ResponseProcessProposal
   ...
}
```

It's also important to note the changes made to the request types for both methods. In upstream, they are only passing the transactions to the applications. This has been modified to pass the entire block data. This is because Celestia separates some block data that cannot modify state (messages), and the application has to have access to both normal transaction data and messages to perform the necessary processing and checks.

```protobuf
message RequestPrepareProposal {
 // block_data is an array of transactions that will be included in a block,
 // sent to the app for possible modifications.
 // applications can not exceed the size of the data passed to it.
 tendermint.types.Data block_data = 1;
 // If an application decides to populate block_data with extra information, they can not exceed this value.
 int64 block_data_size = 2;
}

message RequestProcessProposal {
 tendermint.types.Header header     = 1 [(gogoproto.nullable) = false];
 tendermint.types.Data   block_data = 2;
}

...

// Data contains the set of transactions, evidence,
// and messages to be included in the block
message Data {
 // Txs that will be applied by state @ block.Height+1.
 // NOTE: not all txs here are valid.  We're just agreeing on the order first.
 // This means that block.AppHash does not include these txs.
 repeated bytes txs = 1;

 EvidenceList           evidence                 = 3 [(gogoproto.nullable) = false];
 Messages               messages                 = 4 [(gogoproto.nullable) = false];
 uint64                 original_square_size     = 5;
 bytes                  hash                     = 6; // <-- The hash has been added so that the erasure step can be performed in the app
}
```

Here is where the new ABCI++ method is called to perform arbitrary logic in the app before creating the proposal block. `consensus/state.go`

```go
// CreateProposalBlock calls state.MakeBlock with evidence from the evpool
// and txs from the mempool.
func (blockExec *BlockExecutor) CreateProposalBlock(
   height int64,
   state State, commit *types.Commit,
   proposerAddr []byte,
) (*types.Block, *types.PartSet) {
   ...
   txs := blockExec.mempool.ReapMaxBytesMaxGas(maxDataBytes, maxGas)
   l := len(txs)
   bzs := make([][]byte, l)
   for i := 0; i < l; i++ {
       bzs[i] = txs[i]
   }

   preparedProposal, err := blockExec.proxyApp.PrepareProposalSync(
       abci.RequestPrepareProposal{
           BlockData:     &tmproto.Data{Txs: txs.ToSliceOfBytes(), Evidence: *pevdData},
           BlockDataSize: maxDataBytes},
   )
   ...
   return state.MakeBlock(
       ...
   )
}
```

Here is where arbitrary logic is performed by the app before voting on a proposed block during consensus.

```go
func (cs *State) defaultDoPrevote(height int64, round int32) {
  ...
   stateMachineValidBlock, err := cs.blockExec.ProcessProposal(cs.ProposalBlock)
   if err != nil {
       cs.Logger.Error("state machine returned an error when trying to process proposal block", "err", err)
   }

   // Vote nil if the application invalidated the block
   if !stateMachineValidBlock {
       // Consensus says we must vote nil
       logger.Error("prevote step: consensus deems this block to be mustVoteNil", "err", err)
       cs.signAddVote(tmproto.PrevoteType, nil, types.PartSetHeader{})
       return
   }

   // Prevote cs.ProposalBlock
   // NOTE: the proposal signature is validated when it is received,
   // and the proposal block parts are validated as they are received (against the merkle hash in the proposal)
   logger.Debug("prevote step: ProposalBlock is valid")
   cs.signAddVote(tmproto.PrevoteType, cs.ProposalBlock.Hash(), cs.ProposalBlockParts.Header())
}
```

For those interested in how to incorporate these methods into the cosmos-sdk and the app, we do that in the following PRs.

- celestiaorg/cosmos-sdk [#63](https://github.com/celestiaorg/cosmos-sdk/pull/63)
- celestiaorg/celestia-app [#214](https://github.com/celestiaorg/celestia-app/pull/214)

### PrepareProposal [#637](https://github.com/celestiaorg/celestia-core/pull/637) and [#224](https://github.com/celestiaorg/celestia-app/pull/224)

The way that we create proposal blocks will be refactored (previously [`PrePreprocessTxs`](https://github.com/celestiaorg/celestia-app/blob/0363f0410d9d6bf0e51ac92afcaa9be7c0d1ba07/app/abci.go#L17-L108)) to accommodate the new features.

```go
// PrepareProposal separates messages from transactions, malleates those transactions,
// estimates the square size, fills the data square, and calculates the data hash from
// the erasure data
func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
   squareSize := app.estimateSquareSize(req.BlockData)

   dataSquare, data, err := SplitShares(app.txConfig, squareSize, req.BlockData)
   if err != nil {
       panic(err)
   }

   eds, err := da.ExtendShares(squareSize, dataSquare)
   if err != nil {
       // ... log error
       panic(err)
   }

   dah := da.NewDataAvailabilityHeader(eds)
   data.Hash = dah.Hash() // <-- here we are setting the data hash before we pass the block data back to tendermint
   data.OriginalSquareSize = squareSize

   return abci.ResponsePrepareProposal{
       BlockData: data,
   }
}
```

We estimate the square size by assuming that all the malleable transactions in the block have a valid commitment for whatever square size that we end up picking, and then quickly iterating through the block data to add up the expected lengths of each message/transaction. Please see [here](https://github.com/celestiaorg/celestia-app/blob/e18d8d2301a96702e1bf684735a3620eb059b12f/app/prepare_proposal.go#L47-L130) for more details.

In order to efficiently fill the data square and ensure that each message included in the block is paid for, we progressively generate the data square using a few new types. More details can be found in [#637](https://github.com/celestiaorg/celestia-core/pull/637)

```go
// CompactShareWriter lazily merges transactions or other compact types in
// the block data into shares that will eventually be included in a data square.
// It also has methods to help progressively count how many shares the transactions
// written take-up.
type CompactShareWriter struct {
   shares       []NamespacedShare
   pendingShare NamespacedShare
   namespace    namespace.ID
}
...
// MessageShareWriter lazily merges messages into shares that will eventually be
// included in a data square. It also has methods to help progressively count
// how many shares the messages written take up.
type MessageShareWriter struct {
   shares [][]NamespacedShare
   count  int
}
```

These types are combined in a new celestia-app type, `shareSplitter`, which is responsible for atomically writing transactions and their corresponding messages to the data square and the returned block data.

```go
// shareSplitter writes a data square using provided block data. It also ensures
// that message and their corresponding txs get written to the square
// atomically.
type shareSplitter struct {
   txWriter  *coretypes.CompactShareWriter
   msgWriter *coretypes.MessageShareWriter
   ...
}
```

```go
// SplitShares uses the provided block data to create a flattened data square.
// Any MsgWirePayForBlobs are malleated, and their corresponding
// MsgPayForBlob and blob are written atomically. If there are
// transactions that will not fit in the given square size, then they are
// discarded. This is reflected in the returned block data. Note: pointers to
// block data are only used to avoid dereferencing, not because we need the block
// data to be mutable.
func SplitShares(txConf client.TxConfig, squareSize uint64, data *core.Data) ([][]byte, *core.Data, error) {
   var (
       processedTxs [][]byte
       messages     core.Messages
   )
   sqwr, err := newShareSplitter(txConf, squareSize, data)
   if err != nil {
       return nil, nil, err
   }
   for _, rawTx := range data.Txs {
       ... // decode the transaction

       // write the tx to the square if it normal
       if !hasWirePayForBlob(authTx) {
           success, err := sqwr.writeTx(rawTx)
           if err != nil {
               continue
           }
           if !success {
               // the square is full
               break
           }
           processedTxs = append(processedTxs, rawTx)
           continue
       }

       ...

       // attempt to malleate and write the resulting tx + msg to the square
       parentHash := sha256.Sum256(rawTx)
       success, malTx, message, err := sqwr.writeMalleatedTx(parentHash[:], authTx, wireMsg)
       if err != nil {
           continue
       }
       if !success {
           // the square is full, but we will attempt to continue to fill the
           // block until there are no tx left or no room for txs. While there
           // was not room for this particular tx + msg, there might be room
           // for other txs or even other smaller messages
           continue
       }
       processedTxs = append(processedTxs, malTx)
       messages.MessagesList = append(messages.MessagesList, message)
   }

   sort.Slice(messages.MessagesList, func(i, j int) bool {
       return bytes.Compare(messages.MessagesList[i].NamespaceId, messages.MessagesList[j].NamespaceId) < 0
   })

   return sqwr.export(), &core.Data{
       Txs:                    processedTxs,
       Messages:               messages,
       Evidence:               data.Evidence,
   }, nil
}

...

// writeTx marshals the tx and lazily writes it to the square. Returns true if
// the write was successful, false if there was not enough room in the square.
func (sqwr *shareSplitter) writeTx(tx []byte) (ok bool, err error) {
   delimTx, err := coretypes.Tx(tx).MarshalDelimited()
   if err != nil {
       return false, err
   }

   if !sqwr.hasRoomForTx(delimTx) {
       return false, nil
   }

   sqwr.txWriter.Write(delimTx)
   return true, nil
}

// writeMalleatedTx malleates a MsgWirePayForBlob into a MsgPayForBlob and
// its corresponding message provided that it has a MsgPayForBlob for the
// preselected square size. Returns true if the write was successful, false if
// there was not enough room in the square.
func (sqwr *shareSplitter) writeMalleatedTx(
   parentHash []byte,
   tx signing.Tx,
   wpfb *types.MsgWirePayForBlob,
) (ok bool, malleatedTx coretypes.Tx, msg *core.Message, err error) {
   ... // process the malleated tx and extract the message.

   // check if we have room for both the tx and the message it is crucial that we
   // add both atomically, otherwise the block is invalid
   if !sqwr.hasRoomForBoth(wrappedTx, coreMsg.Data) {
       return false, nil, nil, nil
   }

   ...

   sqwr.txWriter.Write(delimTx)
   sqwr.msgWriter.Write(coretypes.Message{
       NamespaceID: coreMsg.NamespaceId,
       Data:        coreMsg.Data,
   })

   return true, wrappedTx, coreMsg, nil
}
```

Lastly, the data availability header is used to create the `DataHash` in the `Header` in the application instead of in tendermint. This is done by modifying the protobuf version of the block data to retain the cached hash and setting it during `ProcessProposal`. Later, in `ProcessProposal` other full nodes check that the `DataHash` matches the block data by recomputing it. Previously, this extra check was performed inside the `ValidateBasic` method of `types.Data`, where is was computed each time it was decoded. Not only is this more efficient as it saves significant computational resources and keeps `ValidateBasic` light, it is also much more explicit. This approach does not however dramatically change any existing code in tendermint, as the code to compute the hash of the block data remains there. Ideally, we would move all of the code that computes erasure encoding to the app. This approach allows us to keep the intuitiveness of the `Hash` method for `types.Data`, along with not forcing us to change many tests in tendermint, which rely on this functionality.

### ProcessProposal [#214](https://github.com/celestiaorg/celestia-app/pull/214), [#216](https://github.com/celestiaorg/celestia-app/pull/216), and [#224](https://github.com/celestiaorg/celestia-app/pull/224)

During `ProcessProposal`, we

- quickly compare the commitments found in the transactions that are paying for messages to the commitments generated using the message data contained in the block.
- compare the data hash in the header with that generated in the block data

```go
func (app *App) ProcessProposal(req abci.RequestProcessProposal) abci.ResponseProcessProposal {
   // Check for message inclusion:
   //  - each MsgPayForBlob included in a block should have a corresponding blob also in the block data
   //  - the commitment in each PFB should match that of its corresponding blob
   //  - there should be no unpaid messages

   // extract the commitments from any MsgPayForBlobs in the block
   commitments := make(map[string]struct{})
   for _, rawTx := range req.BlockData.Txs {
       ...
       commitments[string(pfb.ShareCommitment)] = struct{}{}
       ...
   }

   // quickly compare the number of PFBs and messages, if they aren't
   // identical, then  we already know this block is invalid
   if len(commitments) != len(req.BlockData.Messages.MessagesList) {
       ... // logging and rejecting
   }

   ... // generate the data availability header

   if !bytes.Equal(dah.Hash(), req.Header.DataHash) {
       ... // logging and rejecting
   }

   return abci.ResponseProcessProposal{
       Result: abci.ResponseProcessProposal_ACCEPT,
   }
}
```

## Consequences

### Positive

- Don't have to wait for the cosmos-sdk or tendermint teams to finish ABCI++
- Get to test important features in the upcoming testnet
- We won't have to implement hacky temporary versions of important features.

### Negative

- We will still have to slightly refactor the code here after ABCI++ comes out

## References

### Issues that will be able to be closed after merging this and all implementation PRs

[#77](https://github.com/celestiaorg/celestia-core/issues/77)
[#454](https://github.com/celestiaorg/celestia-core/issues/454)
[#520](https://github.com/celestiaorg/celestia-core/issues/520)
[#626](https://github.com/celestiaorg/celestia-core/issues/626)
[#636](https://github.com/celestiaorg/celestia-core/issues/636)

[#156](https://github.com/celestiaorg/celestia-app/issues/156)
[#204](https://github.com/celestiaorg/celestia-app/issues/204)

### Open PRs in order of which need to be reviewed/merged first

[#631](https://github.com/celestiaorg/celestia-core/pull/631)
[#63](https://github.com/celestiaorg/cosmos-sdk/pull/63)
[#214](https://github.com/celestiaorg/celestia-app/pull/214)

[#216](https://github.com/celestiaorg/celestia-app/pull/216)
[#637](https://github.com/celestiaorg/celestia-core/pull/637)
[#224](https://github.com/celestiaorg/celestia-app/pull/224)

### Other related unmerged ADRs that we can close after merging this ADR

[#559](https://github.com/celestiaorg/celestia-core/pull/559)
[#157](https://github.com/celestiaorg/celestia-app/pull/157)
