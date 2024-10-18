# Vacuum! Spec

## Intro

Vacuum! is a high throughput, extremely efficient, and very robust blob propagation protocol. It uses highly optimized lazy gossiping and clever prioritization to distribute unique blobs over unique connections. This enables close to the most optimal theoretical performance for any given topoplogy or network load.

Validator Availability Ceritificates (VACs) allow for multi-height pipelining of block data before an arbitrary future block is created by providing a mechanism for validators to signal to proposers which block data they have. This is similar, but simpler, to DAG based protocols.

Unlike other high efficiency block propagation protocols such as Turbine, the topology of the network and the route of the block data is not known ahead of time. Instead, to acheive high throughput, the topology of the network and path of each blob is discovered on the fly as data is propagated via the pipelined lazy gossip of `VAC`s and `WantBlob`s. Beyond the simplicity benefits, using a JIT approach to routing blobs results in a more optimal path than a AOT approach. This is derived from incorporating variables that are impossible to know a head of time, such as the realtime congestion and latency between each peer.

### Assumptions

Vacuum! must be built ontop of a protocol such as QUIC or TCP, meaning it assumes there are guarantees in the order in which messages are read.

## Constants

SAME_VAC_SEND_LIMIT: the number of times a peer can send the same VAC to a peer before being removed. Default = 1

VAC_ROOT_PRUNE_WINDOW: the default number of heights that a VACRoot from a validator is stored for.

MAX_VAC_ROOTS_PER_HEIGHT: specifies the maximum number of VACRoots that are allowed from each valdiator per height. Increasing this constant reduces latency, but increases message passing overhead.

## Message Types

Here are the message types that are required by vacuum! to function.

### BlobTx

Fortunately, no contents for the existing `BlobTx` need to be changed.

```go=
// BlobTx wraps an encoded sdk.Tx with a second field to contain blobs of data.
// The raw bytes of the blobs are not signed over, instead the commitment over
// that data is verified for each blob using the relevant MsgPayForBlobs. Those
// sdk.Msgs are signed over in the encoded sdk.Tx.
type BlobTx struct {
	Tx     []byte  
	Blobs  []*Blob
}
```

For those that want to dive further, [link to the `Blob` struct](https://github.com/celestiaorg/celestia-core/blob/a263c2ca632398271ddb57fc0966c83fa4b2299a/proto/tendermint/types/types.pb.go#L487-L496). 

#### Outbound Logic

- Before sending a BlobTx, a node MUST first receive a `WantBlob` for an existing `VAC` that commits to that `BlobTx`
- Nodes MUST send the highest priority `BlobTx` before sending other `BlobTx` to peers that have requested more than one `BlobTx`.
- ONLY IF CHUNKING IS ENABLED: The highest priority MUST be determined by first sorting requested blobs by priority, then prioritized by unique validator.
  - Example: If a node receives `WantBlob`s for 3 blobs, two for `VAC`s from validator A with reletively higher priorities than the third `WantBlob` for a `VAC` that is from validator B.
  
  ```go
  WantBlob{VAC_A_Priority_10}
  WantBlob{VAC_A_Priority_9}
  WantBlob{VAC_B_Priority_5}
  ```
  
  The order in which they are send MUST be
  
  ```go
  WantBlob{VAC_A_Priority_10}
  WantBlob{VAC_B_Priority_5}
  WantBlob{VAC_A_Priority_9}
  ```
  
If the highest priority VAC was soley determined by the prioritiy from the validator, then a malicous validator could hide the last chunk of very high priority data, which could waste a large amount of bandwidth. This is why, if chunking is enabled, different validator's must be prioritized.

- OPTIONAL: the validator's voting power can also be incorporated into the prioritization logic. This increases the cost of meaningfully spamming the network when chunking is enabled.
  
#### Inbound Logic

- Nodes MUST check the validity of the BlobTx via the `CheckTx`. `BlobTx` that fail **stateless** checks are invalid in every scenario. Therefore, blobs that fail stateless checks MUST result in removing the sending peer. `BlobTx`s that fail **stateful** checks MUST not be gossiped, but the peer should not be kicked, as this simply means the `BlobTx` that was originally asked for is no longer relevant.

### BlobChunk

```go=
// BlobChunk contains a portion of a blob that represents some number of shares.
// It also includes an nmt inclusion proof to prove that this chunk is part of
// the entire blob.
type BlobChunk struct {
	Index uint
	ShareCount uint
	Data []byte
	Proof nmt.Proof
}
```

#### Outbound Logic

- Before sending any `BlobChunk`s, a node MUST first receive a `WantBlob` for an existing `VAC` that commits to that `BlobTx`
- Nodes MUST first send the Transaction portion of the `BlobTx`
- Nodes MUST relay the chunk of data as soon as it is received to any peers that have requested the data via a `WantBlob`

#### Inbound Logic

- Nodes MUST remove peers where a `BlobChunk` fails stateless verification
- Nodes MUST verify the NMT proof to ensure that the chunk of data is in fact part of the blob that they requested.
- Nodes MUST remove peers that send blob chunks before sending the transaction portion of the `BlobTx`

### WantBlob

`WantBlob` is used to facilitate lazy gossip. When a VAC is seen that commits to an unseen BlobTx, a peer responds with a `WantBlob`.

```go=
// WantBlob is message that is sent when a peer does not have the BlobTx that is
// committed to by a VAC.
type WantBlob struct {
	// Commitment is the commitment over the blob that is being requested.
	Commitment [32]byte
	Chunks     *bits.BitArray
}
```

#### Outbound Logic

- nodes MUST only send a `WantBlob` to a peer after receiving a `VAC` from that same peer.
- nodes MUST send a `WantBlob` after receiving the first `VAC` for a given `RootVAC`
- nodes MAY request a `BlobTx` by sending a `WantBlob` to a peer after receiving a `VAC` for that `BlobTx`

> nodes MUST send a `WantBlob` after receiving the first `VAC` for a given `RootVAC`

To expand on the reasoning behind this, the first `VAC` from each validator is prioritized throughput the network to handle one of the "worst case" scenarios where all of the validator's mempools naturally differ. When prioritizing these, the network is maintains its ability to get a high throughput and prioritize the highest value txs. This is also why validators send the highest priority `VAC` to all of their peers (more on this in the Outbound Logic for VACs).

#### Inbound Logic

- nodes MUST respond to the `WantBlob` with the underlying `BlobTx` upon receiving the `BlobTx`. Optionally, if chunking is used, the node MUST relay each chunk as it receives it.

### VACs

Validator Availability Certificates (VACs) are used by validators to both communicate that they are holding onto some data for a period of time and to distribute the highest priority data in a lazy (pull based) way throughout the network. The prioritization logic that determines which VACs are sent to which peer ensure that a high throughput is acheived no matter its topology or no matter how distirbuted that data already is.

```go=
// VAC (validator availability certificate) is a claim created by a validator that they are holding some BlobTx for a
// period of time. It contains all necessary information to identify, prioritze,
// and verify that claim.
type VAC struct {
	// Commitment is commitment to the underlying BlobTx.
	Commitment []byte
	// VACRoot is the commitment over the batch of VACs that this VAC was included
	// in.
	VACRoot []byte
	// Priority is a deterministic value that nodes can use to compare the
	// priority of any two VACs.
	Priority uint
	// Size is the size of the BlobTx in bytes.
	Size uint
	// ID indicates which VAC this is.
	ID uint
	// Proof of inclusion from this VAC to the VACRoot
	Proof merkle.Proof
}
```

`VAC`s are committed to via their hash. The method below goes into the implementation details for serializing and hashing the fields.

```go=
// Hash computes a hash of the VAC struct. All fields are included in the hash.
func (v *VAC) Hash() ([]byte, error) {
	buf := new(bytes.Buffer)

	if _, err := buf.Write(v.Commitment[:]); err != nil {
		return nil, err
	}

	if _, err := buf.Write(v.Root[:]); err != nil {
		return nil, err
	}

	if err := binary.Write(buf, binary.BigEndian, v.Priority); err != nil {
		return nil, err
	}

	if err := binary.Write(buf, binary.BigEndian, v.Size); err != nil {
		return nil, err
	}

	if err := binary.Write(buf, binary.BigEndian, v.ID); err != nil {
		return nil, err
	}

	hash := sha256.Sum256(buf.Bytes())

	return hash[:], nil
}
```

#### Inbound Logic

- Nodes MUST remove peers that send a `VAC` before sending its corresponding `VACRoot`
- Nodes MUST remove peers that send a`VAC` that fails stateless verification.
- Nodes MUST remove peers that send the same `VAC` more than `SAME_VAC_SEND_LIMIT`.

#### Outbound Logic

- Nodes MUST not send a `VAC` that fails stateless or stateful verification.
- Nodes MUST not send the same `VAC` multiple times.
- Nodes MUST only send a `VAC` for a `BlobTx` that they either 1) already have 2) have requested the blob via a `WantBlob`
- Nodes MUST send `WantBlob`s for the highest priority `VAC` from a validator if it does not already have that `BlobTx`. It then MUST broadcast that `VAC` to all its receiving peers.
- Nodes MAY request the `BlobTx` for `VAC`s that they 1) have room for or 2) is of a higher priority than the `BlobTx` they already have.
- Validators signing over `VAC`s MUST keep the underlying `BlobTx` until the specified `vac.HoldHeight` height has passed.

### VACRoot

VACRoots are a signed commitment over a batch of VACs. They are used to aggregate the signatures of a set of VACs.

```go=
// VACRoot is a signed commitment over a batch of VACs. They are used to
// aggregate the signatures of a set of VACs
type VACRoot struct {
	// Commitment is the merkle root over the hashes of each VAC included in the
	// batch of VACs that this root commits over.
	Commitment [32]byte
	// Signature is the validators signature to verify VACs
	Signature [64]byte
	// Valdiator is the original validator's hex encoded address. It indicates
	// which validator this VACRoot from.
	Validator string
	// ID idendifies this Root for nodes in the network.
	ID uint64
	// HoldHeight is the height that the validator is promising to hold all
    // committed transactions until. The only exception is if the transactions are
    // included in a block are pruned.
	HoldHeight uint64
}
```

`VAC`s are committed to via a merkle tree of the hash of each `VAC`. This root is stored in the `Commitment` field. The `VACRoot`'s committment is signed over using the validator's consensus key. The sign bytes for a `VACRoot` are in the following order:

![sign_bytes_manrope](https://hackmd.io/_uploads/S1j9PPs3A.png)

It is generated using a fixed amount of bytes for all uints. This example method goes into more details.

```go
// SignBytes generates deterministic sign bytes for the VACRoot struct.
// It excludes the Signature field, as this is part of the signed data.
func (v *VACRoot) SignBytes() ([]byte, error) {
	buf := new(bytes.Buffer)

	if _, err := buf.Write(v.Commitment[:]); err != nil {
		return nil, err
	}

	validatorBytes, err := hex.DecodeString(v.Validator)
	if err != nil {
		return nil, errors.New("invalid hex string in Validator field")
	}
	if _, err := buf.Write(validatorBytes); err != nil {
		return nil, err
	}

	// Serialize ID (uint, fixed size).
	if err := binary.Write(buf, binary.BigEndian, v.ID); err != nil {
		return nil, err
	}

	// Serialize HoldHeight (uint64, fixed size).
	if err := binary.Write(buf, binary.BigEndian, v.HoldHeight); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
```

#### Inbound Logic

- Nodes MUST remove peers that send `VACRoot`s that fail stateless validation.
- Nodes MUST store the `VACRoot` from each validator.
- Nodes MAY delete the `VACRoot` after a subjective number of heights, the VAC_ROOT_PRUNE_WINDOW.

#### Outbound Logic

- Validators in the current or next validator set MUST be the only entities capable of creating and valid VACRoot.
- Validators MUST not send multiple different `VACRoot`s with the same ID.
- Nodes MUST not gossip `VACRoot`s that fail stateful validation. This includes `VACRoot`s with an outdated `ID` and an `ID` for a height beyond +1 of the current height.

## Creating a Block

When creating a block, the proposer MUST pick the `BlobTx` that have the highest probability of being included. This can be done by first sorting the `BlobTx` that it has received by amount of voting power that has signed a VAC over. All blobs with less than 1/3 of the VAC voting power MAY be excluded. Then it MAY sort those `BlobTx` by priority. If there is still room in the block, it can include the first high priority `BlobTx` from validators `VACRoot`.

- TODO: Analyze compact block propagation and VACs to figure out the most ideal function for picking transactions that are likely to have been distributed.

### Compact Block

```go=
// Proposal defines a block proposal for the consensus.
type Proposal struct {
	...
	CompactBlock
}

// CompactBlock summarizes the contents of a block. It does this by using
// commitments to either standard transactions or blob containing transactions,
// each using their native commitments (hashes for txs, blob commitments for
// BlobTxs).
type CompactBlock struct {
	TxCommitments     [][32]byte
	BlobTxCommitments [][32]byte
}
```

#### Inbound Logic

- Nodes MUST remove peers that send `CompactBlock`s that fail stateless validation.
  - As the `CompactBlock` is part of the proposal, stateless validation includes verifying the signature of the proposer.
- Nodes MUST remove peers that send redundant `CompactBlock`s.

#### Outbound Logic

- Nodes MUST not gossip `CompactBlock`s that fail stateful validation.
- Nodes MUST not send a peer redundant `CompactBlock`s for the same height and round.
- Nodes MUST broadcast the `CompactBlock` to all peers.
- Nodes MUST send `WantBlob` messages to the peer that sent them the `CompactBlock` for the Blobs they do not have or are not already downloading.

## Gossiping Logic

### Lazy Gossip

The main efficiency of vacuum! comes lazy gossiping. Lazy meaning nodes only ever send data if their peer asks for it first. While lazy gossiping is very efficient, it comes with a meaningful penalty to speed. This is due to additional round trips that are required for peers to ask for unseen data at each hop.

![lazy vac](https://hackmd.io/_uploads/SJzCZzAh0.png)

### Speeding up Lazy Gossip

In order to maintain high throughput block propagation when the network's load far exceeds its capacity, it needs to be able to distribute blobs quickly and in a prioritized way. Before we discuss how vacuum! prioritizes blobs, we will discuss how vacuum! increases the speed of blob propagation.

Vacuum! relies heavily on two optimizations to do this. The first is the chunking of blobs. The second optimization is the pipelining of haves and wants.

#### Chunked Blobs

Breaking a blob up into chunks allows for nodes in the network to begin transferring portions of the blob instead of waiting for the entirety of it to be received before sending it to peers. We can see how in basic simulations this speeds up the distribution of data.

![Screenshot from 2024-09-10 20-57-02](https://hackmd.io/_uploads/BJFoLfCh0.png)

Its worth noting that the benefit of chunking blobs compounds each hop. Meaning for networks that don't have a lot of hops, chunking will not have as significant increase in rate of distribution. Simultaneously, networks with many hops will see massive increases in speed.

#### Pipelined Haves and Wants

`VAC`s must be gossiped eagerly if the node that receives the VAC either has the `BlobTx` that that `VAC` commits over or it ends up requesting the `VAC`. This means that upon receiving a `VAC` for the first time, a node broadcasts that same `VAC` to all of its peers. Simultaneously, if that node does not have `BlobTx` that the VAC commits over, then it checks if that is the first `VAC` for that validator. If it is, then it always requests that `VAC`. If not, then it checks to see if it has room for that `VAC`. If it does, then it requests that `VAC`. If it doesn't have room, then it checks to see if the VAC commits over a transaction that is higher in priority than any of the transactions that it currently holds. If the priority is higher, then it requests that VAC. If it is not, then it can safely ignore that VAC and it does not gossip the VAC.

![Screenshot from 2024-09-10 20-46-50](https://hackmd.io/_uploads/BySHEGRnC.png)

```go=
// allocate reserves space for a given VAC. This spot is released when the hold
// height is exceeded.
func (vp VACPool) allocate(vac VAC) {
	...
}

func requestVAC(peer p2p.Peer, vp VACPool, vac VAC) {
	vp.allocate(vac)
	peer.Send(WantBlob{[]byte(vac.Commitment)})
}


// ReceiveVAC describes the receive logic for a VAC. Notably, it only broadcasts
// the VAC if the underlying BlobTx is either already held or it is requested.
func ReceiveVAC(vp VACPool, peer p2p.Peer, vac VAC) {
	// if the peer has sent this VAC to the node before, then they are safe to kick.
	if vp.SeenFrom(peer, vac) {
		kick(peer)
		return
	}

	// if the VAC has already been seen, then all all necessary logic has been
	// performed and the node can safely ignore it.
	if vp.HasVAC(vac) {
		return
	}

	// if this node already has the transaction but has not seen this VAC
	// before, then it must proceed to gossip the VAC, but it does not need to
	// request the blob.
	if vp.Has(vac.Commitment) {
		// ensure that the underlying BlobTx is not pruned until the hold height
		// or a higher priority VAC is received.
		vp.allocate(vac)
		broadcastToAllPeers(vac)
		return
	}

	// if this VAC is the highest priority from a given validator and this node
	// has not seen it, then it must request the BlobTx and gossip the VAC
	if vac.ID == 0 {
		requestVAC(peer, vac)
		broadcastToAllPeers(vac)
		return
	}

	// At this point, this node does not have the BlobTx, but it needs to see if
	// it has room in its mempool or if the underlying BlobTx is higher priority
	// than any of the transactions in its mempool. If either is true, it must
	// request and gossip the VAC.
	if vp.LowestPriority() < vac.Priority || vp.HasRoom(vac) {
		requestVAC(peer, vac)
		broadcastToAllPeers(vac)
	}
	
	return
}
```

### Prioritized and Parallel Distribution of Blobs

#### VACRoot and VAC Creation

In vacuum!, each validator starts the cycle by reaping a set of blob transactions from their mempool. `VAC`s are created for each transaction, and then a `VACRoot` is created and signed over using the validator's consensus key.

After creating the `VACRoot`, it can be sent to all peers.

The highest priority `VAC`s are then sent to all peers. The number of high priority `VAC`s that are sent to each peer are bounded by the max transaction size. If multiple transactions can fit in the `MaxTxSize`. The remaining `VAC`s in the `VACRoot` are sent only to a subset of all peers. The default size of this subset is 1, meaning that each `VAC` that is not the highest priority is only sent to a single peer.

![vacuum step 1](https://hackmd.io/_uploads/Syr08vohR.png)

```go=
// ValidatorPropagateVACs creates a VACRoot over the highest priority BlobTxs
// that are in the VACPool. It gossips the root first to all peers, followed by
// the highest priority VAC. Then it attempts to send the remaining VACs
// exclusively to the next peer.
func (vp VACPool) ValidatorPropagateVACs(priv privval.SignerClient, peers []p2p.Peer) {
	// collect and commit to the highest priority vacs
	vacs := vp.ReapVACs(size)
	root := newVACRoot(vacs)
	priv.SignVACRoot(root)

	// broadcast the root and highest priority VAC to all peers in order
	mustBroadcastToAllPeers(root)
	mustBroadcastToAllPeers(vacs[0])

	// distribute the rest of the VACs individually to the rest of the peers 
    vacCursor := 1
    for i := 0; ; i = (i + 1) % len(peers) {
		if success := peer.TrySend(vacs[cursor]); !success {
            // try a different peer upon failure
            continue
        }
        
        vacCursor++
        if vacCursor >= len(vacs) {
            break
        }
	}

	return
}
```

#### Prioritized Gossiping

While gossiping blobs, its crucial that nodes are sending the highest priority data at any given moment. This can be done by using a sorted queue, such as the golang std library's `container/heap` module.

Blob chunks can be prioritized in a few different ways. This mainly differs if chunking is enabled. The naive way is described in the [`BlobTx Outgoing Logic`](#blobtx).

One example implementation of a sorted queue can be found [here](https://github.com/celestiaorg/celestia-core/blob/evan/pipeline-cat-hack/mempool/cat/sorted_queue.go).

## Disconnection Rules

These are the following scenarios which should never occur for honest peers. If they do, not only is it safe to disconnect from a peer, but they must have some consequence in order to prevent malicious nodes from spamming useless data.

### Out of Order Messages

Since the `VACRoot` is required to verify the `VAC`, the corresponding `VACRoot` must be sent to a peer before a given `VAC`. If a node receives a `VAC` before it has received a `VACRoot`, then it must disconnect the peer for this error. Without this, malicious instances could spam `VAC` messages without repercussion.

### Redundant Messages

If a node sends a peer the same `VACRoot` or `VAC` twice, then it must be disconnected for the same reasons as above.

### Unsolicited Data

If a node receives any part of a blobTx that it didn't ask for, then it must disconnect from that peer.

### Invalid Messages

If a peer sends a message that does not pass that method `ValidateBasic` method, then that peer must be disconnected.
