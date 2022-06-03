# ADR 4: QGB Relayer security design

## Changelog

- {date}: {changelog}

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
-   Get the latest relayed valset
-   Listen for new signed valsets
-   Once a new valset is signed by 2/3 of the network, submit it immediately to be included in next block
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

To avoid spamming the network with requests, these signatures can be added to the block data in specific namespaces and the we can charge per request.

- Pros
	- Would allow anyone to ask for signatures over commitments, ie, the QGB can then be used by any team without changing anything.

- Cons
	- Makes slashing more complicated

## Decision

Still not made.

## Detailed Design

Still not specified.

## Status

Proposed

## References

> Are there any relevant PR comments, issues that led up to this, or articles referenced for why we made the given design choice? If so link them here!

- {reference link}
