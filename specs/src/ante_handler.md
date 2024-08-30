# AnteHandler

Celestia makes use of a Cosmos SDK [AnteHandler](https://github.com/cosmos/cosmos-sdk/blob/v0.46.15/x/auth/spec/03_antehandlers.md) in order to reject decodable sdk.Txs that do not meet certain criteria. The AnteHandler is invoked at multiple times during the transaction lifecycle:

1. `CheckTx` prior to the transaction entering the mempool
1. `PrepareProposal` when the block proposer includes the transaction in a block proposal
1. `ProcessProposal` when validators validate the transaction in a block proposal
1. `DeliverTx` when full nodes execute the transaction in a decided block

The AnteHandler is defined in `app/ante/ante.go`. The app version impacts AnteHandler behavior. See:

- [AnteHandler v1](./ante_handler_v1.md)
- [AnteHandler v2](./ante_handler_v2.md)
