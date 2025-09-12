# AnteHandler v6

The AnteHandler chains together several decorators to ensure the following criteria are met for app version 6:

- Panics are wrapped with the transaction string format for better error reporting.
- A gas meter is set up in the context before any gas consumption occurs.
- The tx does not contain any messages that are disabled by the circuit breaker (e.g. `MsgSoftwareUpgrade`, `MsgCancelUpgrade`, `MsgIBCSoftwareUpgrade`).
- The tx does not contain any [extension options](https://github.com/cosmos/cosmos-sdk/blob/22c28366466e64ebf0df1ce5bec8b1130523552c/proto/cosmos/tx/v1beta1/tx.proto#L119-L122).
- The tx passes `ValidateBasic()`.
- The tx's [timeout_height](https://github.com/cosmos/cosmos-sdk/blob/22c28366466e64ebf0df1ce5bec8b1130523552c/proto/cosmos/tx/v1beta1/tx.proto#L115-L117) has not been reached if one is specified.
- The tx's [memo](https://github.com/cosmos/cosmos-sdk/blob/22c28366466e64ebf0df1ce5bec8b1130523552c/proto/cosmos/tx/v1beta1/tx.proto#L110-L113) is <= the max memo characters where [`MaxMemoCharacters = 256`](<https://github.com/cosmos/cosmos-sdk/blob/a429238fc267da88a8548bfebe0ba7fb28b82a13/x/auth/README.md?plain=1#L230>).
- The tx's [gas_limit](https://github.com/cosmos/cosmos-sdk/blob/22c28366466e64ebf0df1ce5bec8b1130523552c/proto/cosmos/tx/v1beta1/tx.proto#L211-L213) is > the gas consumed based on the tx's size where [`TxSizeCostPerByte = 10`](https://github.com/celestiaorg/celestia-app/blob/6ea21f729fe88e4175c4b3084119392c4acd1957/pkg/appconsts/app_consts.go#L23).
- The tx's feepayer has enough funds to pay fees for the tx. The tx's feepayer is the feegranter (if specified) or the tx's first signer. Note the [feegrant](https://github.com/cosmos/cosmos-sdk/blob/v0.46.15/x/feegrant/README.md) module is enabled.
- The tx's gas price is >= the network minimum gas price where [`NetworkMinGasPrice = 0.000001` utia](https://github.com/celestiaorg/celestia-app/blob/6ea21f729fe88e4175c4b3084119392c4acd1957/pkg/appconsts/initial_consts.go#L24).
- Public keys are set in the context for the fee-payer and all signers.
- The tx's count of signatures <= the max number of signatures. The max number of signatures is [`TxSigLimit = 7`](https://github.com/cosmos/cosmos-sdk/blob/a429238fc267da88a8548bfebe0ba7fb28b82a13/x/auth/README.md?plain=1#L231).
- The tx's [gas_limit](https://github.com/cosmos/cosmos-sdk/blob/22c28366466e64ebf0df1ce5bec8b1130523552c/proto/cosmos/tx/v1beta1/tx.proto#L211-L213) is > the gas consumed based on the tx's signatures.
- The tx's [signatures](https://github.com/cosmos/cosmos-sdk/blob/22c28366466e64ebf0df1ce5bec8b1130523552c/types/tx/signing/signature.go#L10-L26) are valid. For each signature, ensure that the signature's sequence number (a.k.a nonce) matches the account sequence number of the signer.
- The tx does not contain a `MsgExec` with a nested `MsgExec` or `MsgPayForBlobs`.
- The tx's [gas_limit](https://github.com/cosmos/cosmos-sdk/blob/22c28366466e64ebf0df1ce5bec8b1130523552c/proto/cosmos/tx/v1beta1/tx.proto#L211-L213) is > the gas consumed based on the blob size(s). Since blobs are charged based on the number of shares they occupy, the gas consumed is calculated as follows: `gasToConsume = sharesNeeded(blob) * bytesPerShare * gasPerBlobByte`. Where `bytesPerShare` is a global constant (an alias for [`ShareSize = 512`](https://github.com/celestiaorg/go-square/blob/b3db9faa7b36decbebb4db45b1778468022a0019/share/consts.go#L10)) from the go-square package and `gasPerBlobByte` is a versioned constant that can be modified through hard forks (the [`GasPerBlobByte = 8`](https://github.com/celestiaorg/celestia-app/blob/6ea21f729fe88e4175c4b3084119392c4acd1957/pkg/appconsts/app_consts.go#L24)).
- The tx's total blob share count is <= the max blob share count. The max blob share count is derived from the maximum valid square size. The max valid square size is the minimum of: `GovMaxSquareSize` and `SquareSizeUpperBound`.
- The tx does not contain a message of type [MsgSubmitProposal](https://github.com/cosmos/cosmos-sdk/blob/d6d929843bbd331b885467475bcb3050788e30ca/proto/cosmos/gov/v1/tx.proto#L33-L43) with zero proposal messages or with a proposal message that modifies a parameter that is not governance modifiable.
- The tx is not an IBC packet or update message that has already been processed.

In addition to the above criteria, the AnteHandler also has a number of side-effects:

- Tx fees are deducted from the tx's feepayer and added to the fee collector module account.
- Tx priority is calculated based on the smallest denomination of gas price in the tx and set in context.
- The nonce of all tx signers is incremented by 1.
