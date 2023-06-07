## Bridging TIA from Celestia to a Rollup via Stamper

I really like the idea of using a wasm client, or some sort of middleware +
commitment, for rollups that don’t want to have to route IBC messages through
stamper to other chains. If done right, it can save rollups from having to route
messages through stamper, which is brilliant. However, the only place I don’t
see this occurring is on Celestia. I see a few downsides to this:

- Celestia would now require Stamper to be functioning for an indefinite amount
  of time
- Celestia’s state machine would have to include the wasm runtime. While we can
  definitely make it so that it requires a hardfork to change which wasm code is
  on Celestia, this is still introducing an entire VM. The main downside here is
  likely memetic, since Celestia “doesn’t have a execution environment”, but it
  does still bloat the state machine in that we have to maintain that wasm code,
  and runtime, forever.
- The option to upgrade. As Jack has stated, one of the benefits of using the
  wasm client is because it can presumably be upgraded to become proof based
  instead of relying on a committee. This is great for rollup to other chain
  IBC, but this is terrible for Celestia, since Celestia is not a settlement
  layer. Again, we could require a hardfork to upgrade the client, but the point
  being that we’re moving closer in the direction of Celestia being a settlement
  layer.
- If stamper is successful, then there could very well be tens in not hundreds
  of rollups that use it. If that's the case, and each of those rollups want
  TIA, then that will require the same number of IBC connections on Celestia’s
  state machine.

That’s not to say that TIA cannot flow through Stamper, it can, provided Stamper
connects to the rollups that use it. This has the exact same security as a
Stamper approved IBC connection between Celestia and a rollup, but none of the
downsides described above.

## Permissionless deployment

I’m not yet convinced that the scheme described by Stamper is still bulletproof
for permissionless deployment of rollups. This is because the pricing of running
a rollup and verifying the header hash is can not be bounded. 

For instance, what happens if validators are unable to run a client chain for
any reason? Are the validators slashed if they cannot? If they are slashed, then
attackers can have the validators run some arbitrary rollup node that earns more
money than it costs to pay validators to run it, ie one that requires some large
amount of BTC mining to verify a rollup block. If the validators are not
slashed, then it is not permissionless as it boils down to whichever rollup
nodes the validators choose to run.

Some sort of auction/market/permission mechanism must be used (see ICS, Axelar,
EigenLayer, Mesh Security, or Polkadot for different flavors).

## Potential Improvements

### Not using Vote Extensions

Since we’re not actually requiring consensus be reached on each rollup header
(ie a rollup has a non-determinism bug), this could be (arguably) simplified by
just using normal state transitions. Each vote for a client chain header can be
counted asynchronously, and the state remains the same. Similar privileges of
not paying gas can be provided by other mechanisms, the only difference would be
that the votes were txs they would end up in the block data.

### Not requiring validators be the ones to run rollup nodes

Validators don’t actually have to be the ones to verify the hashes. Instead,
this can be arbitrary participants. AFAICT, this actually provides onchain light
clients with the same level of security, assuming that it still requires ⅔ of
the voting power be delegated to these random participants. This is because the
only slashing for app hash violations is from social slashing. If 2 ⁄ 3 of the
voting power approves some header, then the only way to slash them is via a
hardfork from the minority.

In other words, if two thirds of the voting power of the chain deems some
committee the ultimate truth of a set of headers, then I don’t think it matters
if it's the same committee determining the ultimate truth for each header. Note
that I am not to saying that the committees chosen are of the same “quality”,
but I am saying that technically I think there is the same level of crypto
economic security. Again, could be wrong here.

As one would expect, no matter who is voting, the one counting the votes, in
this case the Stamper validator set, is the limiting factor for security.

This has the additional benefit of avoiding the scalability issues of a single
entity maintaining tens, or maybe more, of arbitrary rollup nodes.

### Not limiting payments to USDC

AFACT, the reason to limit payments to USDC was to guarantee that validators get
paid in something that will likely have value. Being paid in something valuable
is important, because Stamper is supposed to be permissionless, however as we
discussed above, that’s not actually possible. Therefore, if some other
market/auction/permission mechanism is used, then there is no reason to limit
payments to USDC.

### Using a simple “restaking” market mechanism

Combining some of the improvements discussed above, mainly not requiring the
participants verifying the headers to be validators, we can have a simple market
mechanism where Stamper stakers can first stake on stamper validator, then using
some rehypothecation mechanism, they can restake per client chain on some
participant. If that participant doesn’t submit votes or equivocates, then there
can be optional slashing.
