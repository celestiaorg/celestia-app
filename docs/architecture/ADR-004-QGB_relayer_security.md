# ADR 4: QGB Relayer security design

## Changelog

- 2022-06-05: Synchronous QGB implementation 

## Context

The current QGB design requires relayers to relay everything in a perfect synchronous order, but the contracts do not.
In fact, the QGB smart contract is designed to update the data commitments as follows:  

- Receive a data commitment  
- Check that the block height (nonce) is higher than the previously committed root  
- Check if the data commitment is signed using the current valset _(this is the problematic check)_  
- Then, other checks + commit

So, if a relayer is up to date, it will submit data commitment and will pass the above checks.

Now, if the relayer is missing some data commitments or valset updates, then it will start catching up the following way:  

- Relay valset  
- Keep relaying all data commitments that were signed using that valset  
- If a new valset is found, check that: up to the block where the valset was changed, all the data commitments that happened during that period are relayed  
- Relay the next valset  
- And, so on.

The problem with this approach is that there is constant risk of this happening for any relayer. Also, a malicious relayer, can target any honest QGB relayer in normal mode, or while catching up, and submit a valset before the honest relayer submits all the data commitments that were signed with the previous valset:

- Get the latest relayed valset
- Listen for new signed valsets
- Once a new valset is signed by 2/3 of the network, submit it immediately to be included in next block

Then, this would create holes in the signatures as the honest relayer will no longer be able to submit the previous data commitments as they're not, in general, signed by the valset relayed by the malicious relayer. And the only solution is to jump to the ones that were signed with the current valset.

## Alternative Approaches

###  More  synchrony : Deploy the QGB contract with a data commitment window

When deploying the QGB  contract,  also  set the data commitment window,  ie, the number of blocks between the `beginBlock` and `endBlock` of each data  commitment confirm.

Then, update the QGB contract to check when receiving a new valset if the latest relayed data commitment height is >= new valset height - data commitment window.

This also would mean adding, for example, a `DataCommitmentWindowConfirm` representing signatures of the validator set for a certain `DataCommitmentWindow`, since this latter can be updated using gov proposals.

- Cons:
	- More complexity and state in the contract
- Pros:
	- Fix the race condition issue

### Synchronous QGB : Universal nonce approach

This approach consists of switching to a synchronous QGB design utilizing universal nonces. This means, the `ValsetConfirm`s and `DataCommitmentConfirm`s  will have the same nonce being incremented on each attestations. Then, the QGB contract will check against this universal nonce and only accept an attestations if its nonce is incremented by 1.

- Cons:
  - Unifiying the `ValsetConfirm`s and `DataCommitmentConfirm`s under the same nonce even if they represent separate concepts.
- Pros:
  - Simpler QGB smart contract

### Add more state to the contract : Store valsets and their nonce

Update the QGB contract to store the valset hashes + their nonces:

- Cons:  
		- Would make the contract more complex  
- Pros:  
		- Would make the relayer parallelizable (can submit data commitments and valsets in any order as long as the valset is committed)  
		- would allow the QGB to catchup correctly even in the existence of a malicious relayer  

### A request oriented design

Currently, the attestations that need to be signed are defined by the state machine based on `abci.endBlock()` and a `DataCommitmentWindow`. This simplifies the state machine and doesn't require  implementing new transaction types to ask for orchestrators signatures.

The request oriented design means providing users (relayers mainly) with the ability to post data commitment requests and ask orchestrators to sign them.

The main issue with this approach is spamming and state bloat. In fact, allowing attestations signatures' requests would allow anyone to spam the network with unnecessary signatures and make orchestrators do unnecessary work. This gets worse if signatures are part of the state, since this latter is costly.

A proposition to remediate the issues described above is to make the signatures part of the block data in specific namespaces. Then, we can charge per request and even make asking for attestations signatures a bit costly.

- Pros
	- Would allow anyone to ask for signatures over commitments, ie, the QGB can then be used by any team without changing anything.

- Cons
	- Makes slashing more complicated. In fact, to slash for liveness, providing the whole history of blocks, proving that a certain orchestrator didn't sign an attestation in the given period, will be hard to implement and the proofs will be big. Compared to the attestations being part of the state, which can be queried easilly.

## Decision

the **Synchronous QGB : Universal nonce approach** will be implemented as it will allow us to ship a working QGB 1.0 version faster while preserving the same security assumptions at the expense of parallelization, and custumization, as discussed under the _request oriented design_ above.

## Detailed Design

TODO [#471](https://github.com/celestiaorg/celestia-app/issues/471)

## Status

Accepted

## References

- Tracker issue for the tasks: https://github.com/celestiaorg/celestia-app/issues/467
