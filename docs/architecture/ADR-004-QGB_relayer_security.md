# ADR 4: QGB Relayer security design

## Changelog

- {date}: {changelog}

## Context

Currently, the QGB  smart contract is designed to update the data commitments as follows:  
- Receive a data commitment  
- Check that the block height (nonce) is  higher than the previously committed root  
- Check if the data commitment is signed using the current valset _(this is the problematic check)_  
- Then, other checks + commit
So, if a relayer is up to date, it will submit data commitment and will pass the above checks.

Now, if the relayer is missing some data commitments or valset updates, then it will start catching up. This will happen at least one time, as we will not launch the network with the QGB, but it will be added through an upgrade.
Thus, the relayer should catchup the following way:  
- Relay first valset  
- Keep relaying all data commitments that were signed using that valset  
- If a new valset is found, check that: up to the block where the valset was changed, all the data commitments that happened during that period are relayed  
- Relay second valset  
- And, so on.
The problem with this approach is that a malicious relayer, can target any honest QGB relayer while catching up, and submit a valset before the honest relayer submits all the data commitments that were signed with the previous valset.  
Then, that would create holes in the signatures.

Also, a malicious relayer can create the above issue in any QGB bridge:  
-   Get the latest relayed valset
-   Listen for new signed valsets
-   Once a new valset is signed by 2/3 of the network, submit it immediately to be included in next block

If the honest relayer was slow in submitting the data commitments that were signed by the previous valset, then the bridge will have data commitment signatures holes. Because, the honest relayer will no longer be able to submit the previous data commitments as they're not, in general, signed by the valset relayed by the malicious relayer. And theonly solution is to jump to the ones that were signed with the current valset.

## Alternative Approaches

### Catchup solutions
#### Start mainnet with QGB deployed
Already discussed not to be the case

#### Start the QGB only from a certain height
This would  save us the catchup from genesis issue.

### Race condition solutions

####  More  synchrony : Deploy the QGB contract with a data commitment window
When deploying the QGB  contract,  also  set the data commitment window,  ie, the number of blocks between the `beginBlock` and `endBlock` of each data  commitment confirm.

Then, update the QGB contract to check when receiving a new valset if the latest relayed data commitment height is >= new valset height - data commitment window.

This also would mean adding, for example, a `DataCommitmentWindowConfirm` representing signatures of the validator set for a certain `DataCommitmentWindow`, since this latter can be updated using gov proposals.

- Cons:
	- More complexity and state in the contract
- Pros:
	- Fix the race condition issue

#### Add more state to the contract : Store valsets and their nonce
Update the QGB contract to store the valset hashes + their nonces:
- Cons:  
		- Would make the contract more complex  
- Pros:  
		- Would make the relayer parallelizable (can submit data commitments and valsets in any order as long as the valset is committed)  
		- would allow the QGB to catchup correctly even in the existence of a malicious relayer  

#### A request oriented design
Currently, the attestations that need to be signed are defined by the state machine based on `abci.endBlock()` and a `DataCommitmentWindow`. This simplifies the state machine and doesn't require  implementing new transaction types to ask for orchestrators signatures.

The request oriented design means providing users (relayers mainly) with the ability to post data commitment requests and ask orchestrators to sign them.

To avoid spamming the network with requests, these signatures can be added to the block data in specific namespaces and the we can charge per request.

- Pros
	- Would fix the relayer issue described above
	- Would allow anyone to ask for signatures over commitments, ie, the QGB can then be used by any team without changing anything.

- Cons
	- Makes slashing more complicated

## Decision

Still not made.

## Detailed Design

Still not specified.

## Status

Proposed

## Consequences

Discussed above

## References

> Are there any relevant PR comments, issues that led up to this, or articles referenced for why we made the given design choice? If so link them here!

- {reference link}
