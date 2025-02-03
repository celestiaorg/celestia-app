# Backwards Compatible Block Propagation

This document is an extension of ADR024.

## Intro

Changes to gossiping protocols need to be backwards compatible with the existing
mechanism to allow for seemless upgrades. This means that the gossiping
mechanisms need to be hotswapple. This can be challenging due to the consensus
reactor and state having their own propagation mechanism, and that they were not
designed to be easily modifiable.

### Compatability

Minimally invasive modularity can be added by not touching the consensus state,
and utilizing the same entry points that exist now. That is, the consenus
reactors internal message channel to the consensus state. While far from optimal
from an engineering or performance perspective, by simply adding (yet another)
syncing routine, we can sync the data from the block propagation reactor to the
consensus.

```go
// sync data periodically checks to make sure that all block parts in the data
// routine are pushed through to the state.
func (cs *State) syncData() {
	for {
		select {
		case <-cs.Quit():
			return
		case <-time.After(time.Millisecond * SyncDataInterval):
			if cs.dr == nil {
				continue
			}

			cs.mtx.RLock()
			h, r := cs.Height, cs.Round
			pparts := cs.ProposalBlockParts
			pprop := cs.Proposal
			completeProp := cs.isProposalComplete()
			cs.mtx.RUnlock()

			if completeProp {
				continue
			}

			prop, parts, _, has := cs.dr.GetProposal(h, r)

			if !has {
				continue
			}

			if prop != nil && pprop == nil {
				cs.peerMsgQueue <- msgInfo{&ProposalMessage{prop}, ""}
			}

			if pparts != nil && pparts.IsComplete() {
				continue
			}

			for i := 0; i < int(parts.Total()); i++ {
				if pparts != nil {
					if p := pparts.GetPart(i); p != nil {
						continue
					}
				}

				part := parts.GetPart(i)
				if part == nil {
					continue
				}
				cs.peerMsgQueue <- msgInfo{&BlockPartMessage{cs.Height, cs.Round, part}, ""}
			}
		}
	}
}
```

This allows for the old routine, alongside the rest of the consensus state
logic, to function as it used to for peers that have yet to migrate to newer
versions. If the peer does not indicate that they are using the new block prop
reactor during the handshake, then the old gossiping routines are spun up like
normal upon adding the peer to the consensus reactor. However, if the peer has
indicated that they are using the new consensus reactor, then the old routines
are simply not spun up. Something along the lines of the below code should
suffice.

```go
func legacyPropagation(peer p2p.Peer) (bool, error) {
	legacyblockProp := true
	ni, ok := peer.NodeInfo().(p2p.DefaultNodeInfo)
	if !ok {
		return false, errors.New("wrong NodeInfo type. Expected DefaultNodeInfo")
	}

	for _, ch := range ni.Channels {
		if ch == types.BlockPropagationChannel {
			legacyblockProp = false
			break
		}
	}

	return legacyblockProp, nil
}
```
